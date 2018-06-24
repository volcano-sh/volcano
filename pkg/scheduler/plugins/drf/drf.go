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
	"github.com/golang/glog"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/api"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/framework"
)

type drfAttr struct {
	share            float64
	dominantResource string
	allocated        *api.Resource
	preempting       *api.Resource
}

type drfPlugin struct {
	totalResource *api.Resource

	// Key is Job ID
	jobOpts map[api.JobID]*drfAttr
}

func New() framework.Plugin {
	return &drfPlugin{
		totalResource: api.EmptyResource(),
		jobOpts:       map[api.JobID]*drfAttr{},
	}
}

func (drf *drfPlugin) Name() string {
	return "drf"
}

func (drf *drfPlugin) OnSessionOpen(ssn *framework.Session) {
	// Prepare scheduling data for this session.
	for _, n := range ssn.Nodes {
		drf.totalResource.Add(n.Allocatable)
	}

	for _, job := range ssn.Jobs {
		attr := &drfAttr{
			allocated:  api.EmptyResource(),
			preempting: api.EmptyResource(),
		}

		for status, tasks := range job.TaskStatusIndex {
			if api.OccupiedResources(status) {
				for _, t := range tasks {
					attr.allocated.Add(t.Resreq)
				}
			}
		}

		drf.jobOpts[job.UID] = attr
	}

	// Add Preemptable function.
	ssn.AddPreemptableFn(func(l interface{}, r interface{}) bool {
		// Re-calculate the share of lv when run one task
		// Re-calculate the share of rv when evict on task
		lv := l.(*api.TaskInfo)
		rv := r.(*api.TaskInfo)

		latt := drf.jobOpts[lv.Job]
		ratt := drf.jobOpts[rv.Job]

		// Also includes preempting resources.
		lalloc := latt.allocated.Clone().Add(lv.Resreq).Add(latt.preempting)
		ralloc := ratt.allocated.Clone().Sub(rv.Resreq).Add(ratt.preempting)

		ls := drf.calculateShare(lalloc, drf.totalResource)
		rs := drf.calculateShare(ralloc, drf.totalResource)

		glog.V(3).Infof("DRF PreemptableFn: preemptor <%v:%v/%v>, alloc <%v>, share <%v>; preemptee <%v:%v/%v>, alloc <%v>, share <%v>",
			lv.UID, lv.Namespace, lv.Name, lalloc, ls, rv.UID, rv.Namespace, rv.Name, ralloc, rs)

		return ls < rs
	})

	// Add Job Order function.
	ssn.AddJobOrderFn(func(l interface{}, r interface{}) int {
		lv := l.(*api.JobInfo)
		rv := r.(*api.JobInfo)

		if drf.jobOpts[lv.UID].share == drf.jobOpts[rv.UID].share {
			return 0
		}

		if drf.jobOpts[lv.UID].share < drf.jobOpts[rv.UID].share {
			return -1
		}

		return 1
	})

	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			attr := drf.jobOpts[event.Task.Job]
			attr.allocated.Add(event.Task.Resreq)

			if event.Task.Resreq.LessEqual(attr.preempting) {
				attr.preempting.Sub(event.Task.Resreq)
			}

			drf.updateShare(attr)
		},
		PipelineFunc: func(event *framework.Event) {
			attr := drf.jobOpts[event.Task.Job]
			attr.allocated.Add(event.Task.Resreq)

			if event.Task.Resreq.LessEqual(attr.preempting) {
				attr.preempting.Sub(event.Task.Resreq)
			}

			drf.updateShare(attr)
		},
		EvictFunc: func(event *framework.Event) {
			attr := drf.jobOpts[event.Task.Job]
			attr.allocated.Sub(event.Task.Resreq)

			drf.updateShare(attr)
		},
		PreemptFunc: func(event *framework.Event) {
			attr := drf.jobOpts[event.Task.Job]
			attr.preempting.Add(event.Task.Resreq)
		},
	})
}

func (drf *drfPlugin) updateShare(attr *drfAttr) {
	attr.share = drf.calculateShare(attr.allocated, drf.totalResource)
}

func (drf *drfPlugin) calculateShare(allocated, totalResource *api.Resource) float64 {
	res := float64(0)
	for _, rn := range api.ResourceNames() {
		share := allocated.Get(rn) / totalResource.Get(rn)
		if share > res {
			res = share
		}
	}

	return res
}

func (drf *drfPlugin) OnSessionClose(session *framework.Session) {
	// Clean schedule data.
	drf.totalResource = api.EmptyResource()
	drf.jobOpts = map[api.JobID]*drfAttr{}
}
