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
	"math"
	"strconv"
	"strings"

	"k8s.io/api/core/v1"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// PluginName indicates name of volcano scheduler plugin.
const PluginName = "drf"

var shareDelta = 0.000001

// hierarchicalNode represents the node hierarchy
// and the corresponding weight and drf attribute
type hierarchicalNode struct {
	parent    *hierarchicalNode
	attr      *drfAttr
	saturated bool
	weight    float64
	hierarchy string
	children  map[string]*hierarchicalNode
}

type drfAttr struct {
	share            float64
	dominantResource string
	allocated        *api.Resource
}

type drfPlugin struct {
	totalResource  *api.Resource
	totalAllocated *api.Resource

	// Key is Job ID
	jobAttrs map[api.JobID]*drfAttr

	// map[namespaceName]->attr
	namespaceOpts map[string]*drfAttr

	// hierarchical tree
	hierarchicalRoot *hierarchicalNode

	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New return drf plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &drfPlugin{
		totalResource: api.EmptyResource(),
		jobAttrs:      map[api.JobID]*drfAttr{},
		namespaceOpts: map[string]*drfAttr{},
		hierarchicalRoot: &hierarchicalNode{
			attr:      &drfAttr{allocated: api.EmptyResource()},
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
			return plugin.EnableHierarchy != nil && *plugin.EnableHierarchy
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

func (drf *drfPlugin) OnSessionOpen(ssn *framework.Session) {
	// Prepare scheduling data for this session.
	for _, n := range ssn.Nodes {
		drf.totalResource.Add(n.Allocatable)
	}

	klog.V(4).Infof("Total Allocatable %s", drf.totalResource)

	namespaceOrderEnabled := drf.NamespaceOrderEnabled(ssn)
	hierarchEnabled := drf.HierarchyEnabled(ssn)

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
		if hierarchEnabled {
			drf.totalAllocated.Add(attr.allocated)
			drf.updateHierarchyShare(job, attr)
		}
	}

	preemptableFn := func(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) []*api.TaskInfo {
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

		return victims
	}

	ssn.AddPreemptableFn(drf.Name(), preemptableFn)

	jobOrderFn := func(l interface{}, r interface{}) int {
		lv := l.(*api.JobInfo)
		rv := r.(*api.JobInfo)

		if hierarchEnabled {
			lnode := drf.hierarchicalRoot
			lpaths := strings.Split(lv.Hierarchy, "/")
			rnode := drf.hierarchicalRoot
			rpaths := strings.Split(rv.Hierarchy, "/")
			depth := 0
			if len(lpaths) < len(rpaths) {
				depth = len(lpaths)
			} else {
				depth = len(rpaths)
			}
			for i := 0; i < depth; i++ {
				klog.V(4).Infof("HDRF JobOrderFn: %s share state: %f, weight %f, %s share state: %f, weight %f",
					lnode.hierarchy, lnode.attr.share, lnode.weight,
					rnode.hierarchy, rnode.attr.share, rnode.weight)
				if lnode.attr.share/lnode.weight == rnode.attr.share/rnode.weight {
					if i < depth-1 {
						lnode = lnode.children[lpaths[i+1]]
						rnode = rnode.children[rpaths[i+1]]
					}
				} else {
					if lnode.attr.share/lnode.weight < rnode.attr.share/rnode.weight {
						return -1
					}
					return 1
				}
			}
		}
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
			if hierarchEnabled {
				drf.totalAllocated.Add(event.Task.Resreq)
				drf.updateHierarchyShare(job, attr)
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

			if hierarchEnabled {
				drf.totalAllocated.Sub(event.Task.Resreq)
				drf.updateHierarchyShare(job, attr)
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

func (drf *drfPlugin) updateHierarchyShare(job *api.JobInfo, attr *drfAttr) {
	inode := drf.hierarchicalRoot
	paths := strings.Split(job.Hierarchy, "/")
	weights := strings.Split(job.Weights, "/")

	// build hierarchy if the node does not exist
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
		saturated: !job.Allocated.Less(job.TotalRequest),
		hierarchy: string(job.UID),
		children:  nil,
	}

	// update drf attribute bottom up
	klog.V(4).Infof("Job <%s/%s> added to %s, weights %s, attr %v, saturated %b",
		job.Namespace, job.Name, job.Hierarchy, job.Weights, child.attr, child.saturated)

	demandingResources := []v1.ResourceName{}
	for _, rn := range drf.totalResource.ResourceNames() {
		if drf.totalAllocated.Get(rn) < drf.totalResource.Get(rn) {
			demandingResources = append(demandingResources, rn)
		}
	}

	inode.children[string(job.UID)] = child
	for ; inode != nil; inode = inode.parent {
		var mdr float64 = math.MaxFloat64
		// get minumun dominant resource share
		for _, child := range inode.children {
			// skip empty child(job or node without tasks added) and saturated child
			if child.attr.dominantResource != "" && !child.saturated {
				_, resShare := drf.calculateShare(child.attr.allocated, drf.totalResource)
				if resShare < mdr {
					mdr = resShare
				}
			}
		}
		iattr := &drfAttr{
			allocated: api.EmptyResource(),
		}
		// scale children with minumun drf share and sum them up
		saturated := true
		for _, child := range inode.children {
			if !child.saturated {
				saturated = false
			}
			// skip empty child(job or node without tasks added)
			if child.attr.dominantResource != "" {
				// saturated child is not scaled
				if child.saturated {
					t := child.attr.allocated
					iattr.allocated.Add(t)
				} else {
					t := child.attr.allocated.Clone().Scale(mdr / child.attr.share)
					iattr.allocated.Add(t)
				}

			}
		}

		// get dominant resource and share from demanding resources
		res := 0.0
		dominantResource := ""
		for _, rn := range demandingResources {
			share := helpers.Share(iattr.allocated.Get(rn), drf.totalResource.Get(rn))
			if share > res {
				res = share
				dominantResource = string(rn)
			}

		}
		iattr.share = res
		iattr.dominantResource = dominantResource
		inode.attr = iattr
		inode.saturated = saturated

		klog.V(4).Infof("Update hierarchical node %s, share %f, dominant %s, resource %v",
			inode.hierarchy, iattr.share, iattr.dominantResource, iattr.allocated)
	}
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
	drf.jobAttrs = map[api.JobID]*drfAttr{}
}
