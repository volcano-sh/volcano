/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package drf

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// PluginName indicates name of volcano scheduler plugin.
const PluginName = "drf"

var shareDelta = 0.000001

// hierarchicalNode represents the node hierarchy
// and the corresponding weight and drf attribute
type hierarchicalNode struct {
	parent *hierarchicalNode
	attr   *drfAttr
	// If the node is a leaf node,
	// request represents the request of the job.
	request   *api.Resource
	weight    float64
	saturated bool
	hierarchy string
	children  map[string]*hierarchicalNode
}

func (node *hierarchicalNode) Clone(parent *hierarchicalNode) *hierarchicalNode {
	newNode := &hierarchicalNode{
		parent: parent,
		attr: &drfAttr{
			share:            node.attr.share,
			dominantResource: node.attr.dominantResource,
			allocated:        node.attr.allocated.Clone(),
		},
		request:   node.request.Clone(),
		weight:    node.weight,
		saturated: node.saturated,
		hierarchy: node.hierarchy,
		children:  nil,
	}
	if node.children != nil {
		newNode.children = map[string]*hierarchicalNode{}
		for _, child := range node.children {
			newNode.children[child.hierarchy] = child.Clone(newNode)
		}
	}
	return newNode
}

// resourceSaturated returns true if any resource of the job is saturated or the job demands fully allocated resource
func resourceSaturated(allocated *api.Resource,
	jobRequest *api.Resource, demandingResources map[v1.ResourceName]bool) bool {
	for _, rn := range allocated.ResourceNames() {
		if allocated.Get(rn) != 0 && jobRequest.Get(rn) != 0 &&
			allocated.Get(rn) >= jobRequest.Get(rn) {
			return true
		}
		if !demandingResources[rn] && jobRequest.Get(rn) != 0 {
			return true
		}
	}
	return false
}

type drfAttr struct {
	share            float64
	dominantResource string
	allocated        *api.Resource
}

func (attr *drfAttr) String() string {
	return fmt.Sprintf("dominant resource <%s>, dominant share %f, allocated %s",
		attr.dominantResource, attr.share, attr.allocated)
}

type drfPlugin struct {
	totalResource  *api.Resource
	totalAllocated *api.Resource

	// Key is Job ID
	jobAttrs map[api.JobID]*drfAttr

	// map[namespaceName]->attr
	namespaceOpts map[string]*drfAttr

	// hierarchical tree root
	hierarchicalRoot *hierarchicalNode

	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New return drf plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &drfPlugin{
		totalResource:  api.EmptyResource(),
		totalAllocated: api.EmptyResource(),
		jobAttrs:       map[api.JobID]*drfAttr{},
		namespaceOpts:  map[string]*drfAttr{},
		hierarchicalRoot: &hierarchicalNode{
			attr:      &drfAttr{allocated: api.EmptyResource()},
			request:   api.EmptyResource(),
			hierarchy: "root",
			weight:    1,
			children:  map[string]*hierarchicalNode{},
		},
		pluginArguments: arguments,
	}
}

func (drf *drfPlugin) Name() string {
	return PluginName
}

// HierarchyEnabled returns if hierarchy is enabled
func (drf *drfPlugin) HierarchyEnabled(ssn *framework.Session) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.Name != PluginName {
				continue
			}
			return plugin.EnabledHierarchy != nil && *plugin.EnabledHierarchy
		}
	}
	return false
}

// NamespaceOrderEnabled returns the NamespaceOrder for this plugin is enabled in this session or not
func (drf *drfPlugin) NamespaceOrderEnabled(ssn *framework.Session) bool {
	for _, tier := range ssn.Tiers {
		for _, plugin := range tier.Plugins {
			if plugin.Name != PluginName {
				continue
			}
			return plugin.EnabledNamespaceOrder != nil && *plugin.EnabledNamespaceOrder
		}
	}
	return false
}

func (drf *drfPlugin) compareQueues(root *hierarchicalNode, lqueue *api.QueueInfo, rqueue *api.QueueInfo) float64 {
	lnode := root
	lpaths := strings.Split(lqueue.Hierarchy, "/")
	rnode := root
	rpaths := strings.Split(rqueue.Hierarchy, "/")
	depth := 0
	if len(lpaths) < len(rpaths) {
		depth = len(lpaths)
	} else {
		depth = len(rpaths)
	}
	for i := 0; i < depth; i++ {
		// Saturated nodes have minumun prioirty,
		// so that demanding nodes will be poped first.
		if !lnode.saturated && rnode.saturated {
			return -1
		}
		if lnode.saturated && !rnode.saturated {
			return 1
		}
		if lnode.attr.share/lnode.weight == rnode.attr.share/rnode.weight {
			if i < depth-1 {
				lnode = lnode.children[lpaths[i+1]]
				rnode = rnode.children[rpaths[i+1]]
			}
		} else {
			return lnode.attr.share/lnode.weight - rnode.attr.share/rnode.weight
		}
	}
	return 0
}

func (drf *drfPlugin) OnSessionOpen(ssn *framework.Session) {
	// Prepare scheduling data for this session.
	drf.totalResource.Add(ssn.TotalResource)

	klog.V(4).Infof("Total Allocatable %s", drf.totalResource)

	namespaceOrderEnabled := drf.NamespaceOrderEnabled(ssn)
	hierarchyEnabled := drf.HierarchyEnabled(ssn)

	for _, job := range ssn.Jobs {
		attr := &drfAttr{
			allocated: api.EmptyResource(),
		}

		for status, tasks := range job.TaskStatusIndex {
			if api.AllocatedStatus(status) {
				for _, t := range tasks {
					attr.allocated.Add(t.Resreq)
				}
			}
		}

		// Calculate the init share of Job
		drf.updateJobShare(job.Namespace, job.Name, attr)

		drf.jobAttrs[job.UID] = attr

		if namespaceOrderEnabled {
			nsOpts, found := drf.namespaceOpts[job.Namespace]
			if !found {
				nsOpts = &drfAttr{
					allocated: api.EmptyResource(),
				}
				drf.namespaceOpts[job.Namespace] = nsOpts
			}
			// all task in job should have the same namespace with job
			nsOpts.allocated.Add(attr.allocated)
			drf.updateNamespaceShare(job.Namespace, nsOpts)
		}
		if hierarchyEnabled {
			queue := ssn.Queues[job.Queue]
			drf.totalAllocated.Add(attr.allocated)
			drf.UpdateHierarchicalShare(drf.hierarchicalRoot, drf.totalAllocated, job, attr, queue.Hierarchy, queue.Weights)
		}
	}

	preemptableFn := func(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		var victims []*api.TaskInfo

		addVictim := func(candidate *api.TaskInfo) {
			victims = append(victims, candidate)
		}

		if namespaceOrderEnabled {
			// apply the namespace share policy on preemptee firstly

			lWeight := ssn.NamespaceInfo[api.NamespaceName(preemptor.Namespace)].GetWeight()
			lNsAtt := drf.namespaceOpts[preemptor.Namespace]
			lNsAlloc := lNsAtt.allocated.Clone().Add(preemptor.Resreq)
			_, lNsShare := drf.calculateShare(lNsAlloc, drf.totalResource)
			lNsShareWeighted := lNsShare / float64(lWeight)

			namespaceAllocation := map[string]*api.Resource{}

			// undecidedPreemptees means this policy could not judge preemptee is preemptable or not
			// and left it to next policy
			undecidedPreemptees := []*api.TaskInfo{}

			for _, preemptee := range preemptees {
				if preemptor.Namespace == preemptee.Namespace {
					// policy is disabled when they are in the same namespace
					undecidedPreemptees = append(undecidedPreemptees, preemptee)
					continue
				}

				// compute the preemptee namespace weighted share after preemption
				nsAllocation, found := namespaceAllocation[preemptee.Namespace]
				if !found {
					rNsAtt := drf.namespaceOpts[preemptee.Namespace]
					nsAllocation = rNsAtt.allocated.Clone()
					namespaceAllocation[preemptee.Namespace] = nsAllocation
				}
				rWeight := ssn.NamespaceInfo[api.NamespaceName(preemptee.Namespace)].GetWeight()
				rNsAlloc := nsAllocation.Sub(preemptee.Resreq)
				_, rNsShare := drf.calculateShare(rNsAlloc, drf.totalResource)
				rNsShareWeighted := rNsShare / float64(rWeight)

				// to avoid ping pong actions, the preemptee namespace should
				// have the higher weighted share after preemption.
				if lNsShareWeighted < rNsShareWeighted {
					addVictim(preemptee)
					continue
				}
				if lNsShareWeighted-rNsShareWeighted > shareDelta {
					continue
				}

				// equal namespace order leads to judgement of jobOrder
				undecidedPreemptees = append(undecidedPreemptees, preemptee)
			}

			preemptees = undecidedPreemptees
		}

		latt := drf.jobAttrs[preemptor.Job]
		lalloc := latt.allocated.Clone().Add(preemptor.Resreq)
		_, ls := drf.calculateShare(lalloc, drf.totalResource)

		allocations := map[api.JobID]*api.Resource{}

		for _, preemptee := range preemptees {
			if _, found := allocations[preemptee.Job]; !found {
				ratt := drf.jobAttrs[preemptee.Job]
				allocations[preemptee.Job] = ratt.allocated.Clone()
			}
			ralloc := allocations[preemptee.Job].Sub(preemptee.Resreq)
			_, rs := drf.calculateShare(ralloc, drf.totalResource)

			if ls < rs || math.Abs(ls-rs) <= shareDelta {
				addVictim(preemptee)
			}
		}

		klog.V(4).Infof("Victims from DRF plugins are %+v", victims)

		return victims, util.Permit
	}

	ssn.AddPreemptableFn(drf.Name(), preemptableFn)

	if hierarchyEnabled {
		queueOrderFn := func(l interface{}, r interface{}) int {
			lv := l.(*api.QueueInfo)
			rv := r.(*api.QueueInfo)
			ret := drf.compareQueues(drf.hierarchicalRoot, lv, rv)
			if ret < 0 {
				return -1
			}
			if ret > 0 {
				return 1
			}
			return 0
		}
		ssn.AddQueueOrderFn(drf.Name(), queueOrderFn)

		reclaimFn := func(reclaimer *api.TaskInfo, reclaimees []*api.TaskInfo) ([]*api.TaskInfo, int) {
			var victims []*api.TaskInfo
			// clone hdrf tree
			totalAllocated := drf.totalAllocated.Clone()
			root := drf.hierarchicalRoot.Clone(nil)

			//  update reclaimer hdrf
			ljob := ssn.Jobs[reclaimer.Job]
			lqueue := ssn.Queues[ljob.Queue]
			ljob = ljob.Clone()
			attr := drf.jobAttrs[ljob.UID]
			lattr := &drfAttr{
				allocated: attr.allocated.Clone(),
			}
			lattr.allocated.Add(reclaimer.Resreq)
			totalAllocated.Add(reclaimer.Resreq)
			drf.updateShare(lattr)
			drf.UpdateHierarchicalShare(root, totalAllocated, ljob, lattr, lqueue.Hierarchy, lqueue.Weights)

			for _, preemptee := range reclaimees {
				rjob := ssn.Jobs[preemptee.Job]
				rqueue := ssn.Queues[rjob.Queue]

				// update hdrf of reclaimee job
				totalAllocated.Sub(preemptee.Resreq)
				rjob = rjob.Clone()
				attr := drf.jobAttrs[rjob.UID]
				rattr := &drfAttr{
					allocated: attr.allocated.Clone(),
				}
				rattr.allocated.Sub(preemptee.Resreq)
				drf.updateShare(rattr)
				drf.UpdateHierarchicalShare(root, totalAllocated, rjob, rattr, rqueue.Hierarchy, rqueue.Weights)

				// compare hdrf of queues
				ret := drf.compareQueues(root, lqueue, rqueue)

				// resume hdrf of reclaimee job
				totalAllocated.Add(preemptee.Resreq)
				rattr.allocated.Add(preemptee.Resreq)
				drf.updateShare(rattr)
				drf.UpdateHierarchicalShare(root, totalAllocated, rjob, rattr, rqueue.Hierarchy, rqueue.Weights)

				if ret < 0 {
					victims = append(victims, preemptee)
				}

				if ret > shareDelta {
					continue
				}
			}

			klog.V(4).Infof("Victims from HDRF plugins are %+v", victims)

			return victims, util.Permit
		}
		ssn.AddReclaimableFn(drf.Name(), reclaimFn)
	}

	jobOrderFn := func(l interface{}, r interface{}) int {
		lv := l.(*api.JobInfo)
		rv := r.(*api.JobInfo)

		klog.V(4).Infof("DRF JobOrderFn: <%v/%v> share state: %v, <%v/%v> share state: %v",
			lv.Namespace, lv.Name, drf.jobAttrs[lv.UID].share, rv.Namespace, rv.Name, drf.jobAttrs[rv.UID].share)

		if drf.jobAttrs[lv.UID].share == drf.jobAttrs[rv.UID].share {
			return 0
		}

		if drf.jobAttrs[lv.UID].share < drf.jobAttrs[rv.UID].share {
			return -1
		}

		return 1
	}

	ssn.AddJobOrderFn(drf.Name(), jobOrderFn)

	namespaceOrderFn := func(l interface{}, r interface{}) int {
		lv := l.(api.NamespaceName)
		rv := r.(api.NamespaceName)

		lOpt := drf.namespaceOpts[string(lv)]
		rOpt := drf.namespaceOpts[string(rv)]

		lWeight := ssn.NamespaceInfo[lv].GetWeight()
		rWeight := ssn.NamespaceInfo[rv].GetWeight()

		klog.V(4).Infof("DRF NamespaceOrderFn: <%v> share state: %f, weight %v, <%v> share state: %f, weight %v",
			lv, lOpt.share, lWeight, rv, rOpt.share, rWeight)

		lWeightedShare := lOpt.share / float64(lWeight)
		rWeightedShare := rOpt.share / float64(rWeight)

		metrics.UpdateNamespaceWeight(string(lv), lWeight)
		metrics.UpdateNamespaceWeight(string(rv), rWeight)
		metrics.UpdateNamespaceWeightedShare(string(lv), lWeightedShare)
		metrics.UpdateNamespaceWeightedShare(string(rv), rWeightedShare)

		if lWeightedShare == rWeightedShare {
			return 0
		}

		if lWeightedShare < rWeightedShare {
			return -1
		}

		return 1
	}

	if namespaceOrderEnabled {
		ssn.AddNamespaceOrderFn(drf.Name(), namespaceOrderFn)
	}

	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			attr := drf.jobAttrs[event.Task.Job]
			attr.allocated.Add(event.Task.Resreq)

			job := ssn.Jobs[event.Task.Job]
			drf.updateJobShare(job.Namespace, job.Name, attr)

			nsShare := -1.0
			if namespaceOrderEnabled {
				nsOpt := drf.namespaceOpts[event.Task.Namespace]
				nsOpt.allocated.Add(event.Task.Resreq)

				drf.updateNamespaceShare(event.Task.Namespace, nsOpt)
				nsShare = nsOpt.share
			}
			if hierarchyEnabled {
				queue := ssn.Queues[job.Queue]

				drf.totalAllocated.Add(event.Task.Resreq)
				drf.UpdateHierarchicalShare(drf.hierarchicalRoot, drf.totalAllocated, job, attr, queue.Hierarchy, queue.Weights)
			}

			klog.V(4).Infof("DRF AllocateFunc: task <%v/%v>, resreq <%v>,  share <%v>, namespace share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share, nsShare)
		},
		DeallocateFunc: func(event *framework.Event) {
			attr := drf.jobAttrs[event.Task.Job]
			attr.allocated.Sub(event.Task.Resreq)

			job := ssn.Jobs[event.Task.Job]
			drf.updateJobShare(job.Namespace, job.Name, attr)

			nsShare := -1.0
			if namespaceOrderEnabled {
				nsOpt := drf.namespaceOpts[event.Task.Namespace]
				nsOpt.allocated.Sub(event.Task.Resreq)

				drf.updateNamespaceShare(event.Task.Namespace, nsOpt)
				nsShare = nsOpt.share
			}

			if hierarchyEnabled {
				queue := ssn.Queues[job.Queue]
				drf.totalAllocated.Sub(event.Task.Resreq)
				drf.UpdateHierarchicalShare(drf.hierarchicalRoot, drf.totalAllocated, job, attr, queue.Hierarchy, queue.Weights)
			}

			klog.V(4).Infof("DRF EvictFunc: task <%v/%v>, resreq <%v>,  share <%v>, namespace share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share, nsShare)
		},
	})
}

func (drf *drfPlugin) updateNamespaceShare(namespaceName string, attr *drfAttr) {
	drf.updateShare(attr)
	metrics.UpdateNamespaceShare(namespaceName, attr.share)
}

// build hierarchy if the node does not exist
func (drf *drfPlugin) buildHierarchy(root *hierarchicalNode, job *api.JobInfo, attr *drfAttr,
	hierarchy, hierarchicalWeights string) {
	inode := root
	paths := strings.Split(hierarchy, "/")
	weights := strings.Split(hierarchicalWeights, "/")

	for i := 1; i < len(paths); i++ {
		if child, ok := inode.children[paths[i]]; ok {
			inode = child
		} else {
			fweight, _ := strconv.ParseFloat(weights[i], 64)
			if fweight < 1 {
				fweight = 1
			}
			child = &hierarchicalNode{
				weight:    fweight,
				hierarchy: paths[i],
				request:   api.EmptyResource(),
				attr: &drfAttr{
					allocated: api.EmptyResource(),
				},
				children: make(map[string]*hierarchicalNode),
			}
			klog.V(4).Infof("Node %s added to %s, weight %f",
				child.hierarchy, inode.hierarchy, fweight)
			inode.children[paths[i]] = child
			child.parent = inode
			inode = child
		}
	}

	child := &hierarchicalNode{
		weight:    1,
		attr:      attr,
		hierarchy: string(job.UID),
		request:   job.TotalRequest.Clone(),
		children:  nil,
	}
	inode.children[string(job.UID)] = child
	// update drf attribute bottom up
	klog.V(4).Infof("Job <%s/%s> added to %s, weights %s, attr %v, total request: %s",
		job.Namespace, job.Name, inode.hierarchy, hierarchicalWeights, child.attr, job.TotalRequest)
}

// updateNamespaceShare updates the node attribute recursively
func (drf *drfPlugin) updateHierarchicalShare(node *hierarchicalNode,
	demandingResources map[v1.ResourceName]bool) {
	if node.children == nil {
		node.saturated = resourceSaturated(node.attr.allocated,
			node.request, demandingResources)
		klog.V(4).Infof("Update hierarchical node %s, share %f, dominant %s, resource %v, saturated: %t",
			node.hierarchy, node.attr.share, node.attr.dominantResource, node.attr.allocated, node.saturated)
	} else {
		var mdr float64 = 1
		// get minimun dominant resource share
		for _, child := range node.children {
			drf.updateHierarchicalShare(child, demandingResources)
			// skip empty child and saturated child
			if child.attr.share != 0 && !child.saturated {
				_, resShare := drf.calculateShare(child.attr.allocated, drf.totalResource)
				if resShare < mdr {
					mdr = resShare
				}
			}
		}

		node.attr.allocated = api.EmptyResource()
		saturated := true
		for _, child := range node.children {
			if !child.saturated {
				saturated = false
			}
			// only consider non-empty children
			if child.attr.share != 0 {
				// saturated child is not scaled
				if child.saturated {
					t := child.attr.allocated
					node.attr.allocated.Add(t)
				} else {
					t := child.attr.allocated.Clone().Multi(mdr / child.attr.share)
					node.attr.allocated.Add(t)
				}
			}
		}
		node.attr.dominantResource, node.attr.share = drf.calculateShare(
			node.attr.allocated, drf.totalResource)
		node.saturated = saturated
		klog.V(4).Infof("Update hierarchical node %s, share %f, dominant resource %s, resource %v, saturated: %t",
			node.hierarchy, node.attr.share, node.attr.dominantResource, node.attr.allocated, node.saturated)
	}
}

func (drf *drfPlugin) UpdateHierarchicalShare(root *hierarchicalNode, totalAllocated *api.Resource, job *api.JobInfo, attr *drfAttr, hierarchy, hierarchicalWeights string) {
	// filter out demanding resources
	demandingResources := map[v1.ResourceName]bool{}
	for _, rn := range drf.totalResource.ResourceNames() {
		if totalAllocated.Get(rn) < drf.totalResource.Get(rn) {
			demandingResources[rn] = true
		}
	}
	drf.buildHierarchy(root, job, attr, hierarchy, hierarchicalWeights)
	drf.updateHierarchicalShare(root, demandingResources)
}

func (drf *drfPlugin) updateJobShare(jobNs, jobName string, attr *drfAttr) {
	drf.updateShare(attr)
	metrics.UpdateJobShare(jobNs, jobName, attr.share)
}

func (drf *drfPlugin) updateShare(attr *drfAttr) {
	attr.dominantResource, attr.share = drf.calculateShare(attr.allocated, drf.totalResource)
}

func (drf *drfPlugin) calculateShare(allocated, totalResource *api.Resource) (string, float64) {
	res := float64(0)
	dominantResource := ""
	for _, rn := range totalResource.ResourceNames() {
		share := helpers.Share(allocated.Get(rn), totalResource.Get(rn))
		if share > res {
			res = share
			dominantResource = string(rn)
		}
	}

	return dominantResource, res
}

func (drf *drfPlugin) OnSessionClose(session *framework.Session) {
	// Clean schedule data.
	drf.totalResource = api.EmptyResource()
	drf.totalAllocated = api.EmptyResource()
	drf.jobAttrs = map[api.JobID]*drfAttr{}
}
