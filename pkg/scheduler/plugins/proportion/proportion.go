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

package proportion

import (
	"github.com/golang/glog"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/api"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/framework"
)

type proportionPlugin struct {
	args          *framework.PluginArgs
	totalResource *api.Resource
	queueOpts     map[api.QueueID]*queueAttr
}

type queueAttr struct {
	name   string
	weight int32
	share  float64

	deserved  *api.Resource
	allocated *api.Resource
	request   *api.Resource
}

func New(args *framework.PluginArgs) framework.Plugin {
	return &proportionPlugin{
		args:          args,
		totalResource: api.EmptyResource(),
		queueOpts:     map[api.QueueID]*queueAttr{},
	}
}

func (pp *proportionPlugin) OnSessionOpen(ssn *framework.Session) {
	// Prepare scheduling data for this session.
	for _, n := range ssn.Nodes {
		pp.totalResource.Add(n.Allocatable)
	}

	totalWeight := int32(0)
	for _, queue := range ssn.Queues {
		attr := &queueAttr{
			name:   queue.Name,
			weight: queue.Weight,

			deserved:  api.EmptyResource(),
			allocated: api.EmptyResource(),
			request:   api.EmptyResource(),
		}
		totalWeight += queue.Weight
		pp.queueOpts[queue.UID] = attr
	}

	for _, job := range ssn.Jobs {
		for status, tasks := range job.TaskStatusIndex {
			if api.AllocatedStatus(status) {
				for _, t := range tasks {
					attr := pp.queueOpts[job.Queue]
					attr.allocated.Add(t.Resreq)
					attr.request.Add(t.Resreq)
				}
			} else if status == api.Pending {
				for _, t := range tasks {
					attr := pp.queueOpts[job.Queue]
					attr.request.Add(t.Resreq)
				}
			}
		}
	}

	for _, attr := range pp.queueOpts {
		attr.deserved = pp.totalResource.Clone().Multi(float64(attr.weight) / float64(totalWeight))
		pp.updateShare(attr)

		glog.V(3).Infof("The proportion attributes of Queue <%s>: share: %0.2f, deserved: %v, allocated: %v, request: %v",
			attr.name, attr.share, attr.deserved, attr.allocated, attr.request)
	}

	ssn.AddQueueOrderFn(func(l, r interface{}) int {
		lv := l.(*api.QueueInfo)
		rv := r.(*api.QueueInfo)

		if pp.queueOpts[lv.UID].share == pp.queueOpts[rv.UID].share {
			return 0
		}

		if pp.queueOpts[lv.UID].share < pp.queueOpts[rv.UID].share {
			return -1
		}

		return 1
	})

	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			job := ssn.JobIndex[event.Task.Job]
			attr := pp.queueOpts[job.Queue]
			attr.allocated.Add(event.Task.Resreq)

			pp.updateShare(attr)

			glog.V(3).Infof("Proportion AllocateFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)
		},
		EvictFunc: func(event *framework.Event) {
			job := ssn.JobIndex[event.Task.Job]
			attr := pp.queueOpts[job.Queue]
			attr.allocated.Sub(event.Task.Resreq)

			pp.updateShare(attr)

			glog.V(3).Infof("Proportion EvictFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)
		},
	})
}

func (pp *proportionPlugin) OnSessionClose(ssn *framework.Session) {
	pp.totalResource = nil
	pp.queueOpts = nil
}

func (pp *proportionPlugin) updateShare(attr *queueAttr) {
	res := float64(0)

	deserved := api.Min(attr.deserved, attr.request)

	// TODO(k82cn): how to handle fragement issues?
	for _, rn := range api.ResourceNames() {
		share := attr.allocated.Get(rn) / deserved.Get(rn)
		if share > res {
			res = share
		}
	}

	attr.share = res
}
