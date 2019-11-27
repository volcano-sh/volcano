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

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// PluginName indicates name of volcano scheduler plugin.
const PluginName = "drf"

var shareDelta = 0.000001

type drfAttr struct {
	share            float64
	dominantResource string
	allocated        *api.Resource
}

type drfPlugin struct {
	totalResource *api.Resource

	// Key is Job ID
	jobAttrs map[api.JobID]*drfAttr

	// map[namespaceName]->attr
	namespaceOpts map[string]*drfAttr

	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New return drf plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &drfPlugin{
		totalResource:   api.EmptyResource(),
		jobAttrs:        map[api.JobID]*drfAttr{},
		namespaceOpts:   map[string]*drfAttr{},
		pluginArguments: arguments,
	}
}

func (drf *drfPlugin) Name() string {
	return PluginName
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

	namespaceOrderEnabled := drf.NamespaceOrderEnabled(ssn)

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
		drf.updateShare(attr)

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
			drf.updateShare(nsOpts)
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

			drf.updateShare(attr)

			nsShare := -1.0
			if namespaceOrderEnabled {
				nsOpt := drf.namespaceOpts[event.Task.Namespace]
				nsOpt.allocated.Add(event.Task.Resreq)

				drf.updateShare(nsOpt)
				nsShare = nsOpt.share
			}

			klog.V(4).Infof("DRF AllocateFunc: task <%v/%v>, resreq <%v>,  share <%v>, namespace share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share, nsShare)
		},
		DeallocateFunc: func(event *framework.Event) {
			attr := drf.jobAttrs[event.Task.Job]
			attr.allocated.Sub(event.Task.Resreq)

			drf.updateShare(attr)

			nsShare := -1.0
			if namespaceOrderEnabled {
				nsOpt := drf.namespaceOpts[event.Task.Namespace]
				nsOpt.allocated.Sub(event.Task.Resreq)

				drf.updateShare(nsOpt)
				nsShare = nsOpt.share
			}

			klog.V(4).Infof("DRF EvictFunc: task <%v/%v>, resreq <%v>,  share <%v>, namespace share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share, nsShare)
		},
	})
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
