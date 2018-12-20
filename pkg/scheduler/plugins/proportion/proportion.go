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

	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api/helpers"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
)

type proportionPlugin struct {
	args          *framework.PluginArgs
	totalResource *api.Resource
	queueOpts     map[api.QueueID]*queueAttr
}

type queueAttr struct {
	queueID api.QueueID
	name    string
	weight  int32
	share   float64

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

	// Also remove the resource used by other scheduler.
	for _, task := range ssn.Others {
		pp.totalResource.Sub(task.Resreq)
	}

	glog.V(4).Infof("The total resource is <%v>", pp.totalResource)

	// Build attributes for Queues.
	for _, job := range ssn.Jobs {
		glog.V(4).Infof("Considering Job <%s/%s>.", job.Namespace, job.Name)

		if _, found := pp.queueOpts[job.Queue]; !found {
			queue := ssn.QueueIndex[job.Queue]
			attr := &queueAttr{
				queueID: queue.UID,
				name:    queue.Name,
				weight:  queue.Weight,

				deserved:  api.EmptyResource(),
				allocated: api.EmptyResource(),
				request:   api.EmptyResource(),
			}
			pp.queueOpts[job.Queue] = attr
			glog.V(4).Infof("Added Queue <%s> attributes.", job.Queue)
		}

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

	remaining := pp.totalResource.Clone()
	meet := map[api.QueueID]struct{}{}
	for {
		totalWeight := int32(0)
		for _, attr := range pp.queueOpts {
			if _, found := meet[attr.queueID]; found {
				continue
			}
			totalWeight += attr.weight
		}

		// If no queues, break
		if totalWeight == 0 {
			break
		}

		// Calculates the deserved of each Queue.
		deserved := api.EmptyResource()
		for _, attr := range pp.queueOpts {
			glog.V(4).Infof("Considering Queue <%s>: weight <%d>, total weight <%d>.",
				attr.name, attr.weight, totalWeight)
			if _, found := meet[attr.queueID]; found {
				continue
			}

			attr.deserved.Add(remaining.Clone().Multi(float64(attr.weight) / float64(totalWeight)))
			if !attr.deserved.LessEqual(attr.request) {
				attr.deserved = helpers.Min(attr.deserved, attr.request)
				meet[attr.queueID] = struct{}{}
			}
			pp.updateShare(attr)

			glog.V(4).Infof("The attributes of queue <%s> in proportion: deserved <%v>, allocate <%v>, request <%v>, share <%0.2f>",
				attr.name, attr.deserved, attr.allocated, attr.request, attr.share)

			deserved.Add(attr.deserved)
		}

		remaining.Sub(deserved)
		if remaining.IsEmpty() {
			break
		}
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

	ssn.AddReclaimableFn(func(reclaimer *api.TaskInfo, reclaimees []*api.TaskInfo) []*api.TaskInfo {
		var victims []*api.TaskInfo
		allocations := map[api.QueueID]*api.Resource{}

		for _, reclaimee := range reclaimees {
			job := ssn.JobIndex[reclaimee.Job]
			attr := pp.queueOpts[job.Queue]

			if _, found := allocations[job.Queue]; !found {
				allocations[job.Queue] = attr.allocated.Clone()
			}
			allocated := allocations[job.Queue]
			if allocated.Less(reclaimee.Resreq) {
				glog.Errorf("Failed to calculate the allocation of Task <%s/%s> in Queue <%s>.",
					reclaimee.Namespace, reclaimee.Name, job.Queue)
				continue
			}

			allocated.Sub(reclaimee.Resreq)
			if attr.deserved.LessEqual(allocated) {
				victims = append(victims, reclaimee)
			}
		}

		return victims
	})

	ssn.AddOverusedFn(func(obj interface{}) bool {
		queue := obj.(*api.QueueInfo)
		attr := pp.queueOpts[queue.UID]

		return attr.deserved.LessEqual(attr.allocated)
	})

	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			job := ssn.JobIndex[event.Task.Job]
			attr := pp.queueOpts[job.Queue]
			attr.allocated.Add(event.Task.Resreq)

			pp.updateShare(attr)

			glog.V(4).Infof("Proportion AllocateFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)
		},
		DeallocateFunc: func(event *framework.Event) {
			job := ssn.JobIndex[event.Task.Job]
			attr := pp.queueOpts[job.Queue]
			attr.allocated.Sub(event.Task.Resreq)

			pp.updateShare(attr)

			glog.V(4).Infof("Proportion EvictFunc: task <%v/%v>, resreq <%v>,  share <%v>",
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

	// TODO(k82cn): how to handle fragement issues?
	for _, rn := range api.ResourceNames() {
		share := helpers.Share(attr.allocated.Get(rn), attr.deserved.Get(rn))
		if share > res {
			res = share
		}
	}

	attr.share = res
}
