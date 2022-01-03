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
	"math"
	"reflect"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// PluginName indicates name of volcano scheduler plugin.
const PluginName = "proportion"

type proportionPlugin struct {
	totalResource *api.Resource
	queueOpts     map[api.QueueID]*queueAttr
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

type queueAttr struct {
	queueID api.QueueID
	name    string
	weight  int32
	share   float64

	deserved  *api.Resource
	allocated *api.Resource
	request   *api.Resource
	// inqueue represents the resource request of the inqueue job
	inqueue    *api.Resource
	capability *api.Resource
}

// New return proportion action
func New(arguments framework.Arguments) framework.Plugin {
	return &proportionPlugin{
		totalResource:   api.EmptyResource(),
		queueOpts:       map[api.QueueID]*queueAttr{},
		pluginArguments: arguments,
	}
}

func (pp *proportionPlugin) Name() string {
	return PluginName
}

func (pp *proportionPlugin) OnSessionOpen(ssn *framework.Session) {
	// Prepare scheduling data for this session.
	pp.totalResource.Add(ssn.TotalResource)

	klog.V(4).Infof("The total resource is <%v>", pp.totalResource)

	// Build attributes for Queues.
	for _, job := range ssn.Jobs {
		klog.V(4).Infof("Considering Job <%s/%s>.", job.Namespace, job.Name)
		if _, found := pp.queueOpts[job.Queue]; !found {
			queue := ssn.Queues[job.Queue]
			attr := &queueAttr{
				queueID: queue.UID,
				name:    queue.Name,
				weight:  queue.Weight,

				deserved:  api.EmptyResource(),
				allocated: api.EmptyResource(),
				request:   api.EmptyResource(),
				inqueue:   api.EmptyResource(),
			}
			if len(queue.Queue.Spec.Capability) != 0 {
				attr.capability = api.NewResource(queue.Queue.Spec.Capability)
				if attr.capability.MilliCPU <= 0 {
					attr.capability.MilliCPU = math.MaxFloat64
				}
				if attr.capability.Memory <= 0 {
					attr.capability.Memory = math.MaxFloat64
				}
			}

			pp.queueOpts[job.Queue] = attr
			klog.V(4).Infof("Added Queue <%s> attributes.", job.Queue)
		}

		attr := pp.queueOpts[job.Queue]
		for status, tasks := range job.TaskStatusIndex {
			if api.AllocatedStatus(status) {
				for _, t := range tasks {
					attr.allocated.Add(t.Resreq)
					attr.request.Add(t.Resreq)
				}
			} else if status == api.Pending {
				for _, t := range tasks {
					attr.request.Add(t.Resreq)
				}
			}
		}

		if job.PodGroup.Status.Phase == scheduling.PodGroupInqueue {
			attr.inqueue.Add(job.GetMinResources())
		}
	}

	// Record metrics
	for _, attr := range pp.queueOpts {
		metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)
		metrics.UpdateQueueRequest(attr.name, attr.request.MilliCPU, attr.request.Memory)
		metrics.UpdateQueueWeight(attr.name, attr.weight)
		queue := ssn.Queues[attr.queueID]
		metrics.UpdateQueuePodGroupInqueueCount(attr.name, queue.Queue.Status.Inqueue)
		metrics.UpdateQueuePodGroupPendingCount(attr.name, queue.Queue.Status.Pending)
		metrics.UpdateQueuePodGroupRunningCount(attr.name, queue.Queue.Status.Running)
		metrics.UpdateQueuePodGroupUnknownCount(attr.name, queue.Queue.Status.Unknown)
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
			klog.V(4).Infof("Exiting when total weight is 0")
			break
		}

		oldRemaining := remaining.Clone()
		// Calculates the deserved of each Queue.
		// increasedDeserved is the increased value for attr.deserved of processed queues
		// decreasedDeserved is the decreased value for attr.deserved of processed queues
		increasedDeserved := api.EmptyResource()
		decreasedDeserved := api.EmptyResource()
		for _, attr := range pp.queueOpts {
			klog.V(4).Infof("Considering Queue <%s>: weight <%d>, total weight <%d>.",
				attr.name, attr.weight, totalWeight)
			if _, found := meet[attr.queueID]; found {
				continue
			}

			oldDeserved := attr.deserved.Clone()
			attr.deserved.Add(remaining.Clone().Multi(float64(attr.weight) / float64(totalWeight)))

			if attr.capability != nil {
				attr.deserved.MinDimensionResource(attr.capability, api.Infinity)
			}
			attr.deserved.MinDimensionResource(attr.request, api.Zero)
			klog.V(4).Infof("Format queue <%s> deserved resource to <%v>", attr.name, attr.deserved)

			if attr.request.LessEqual(attr.deserved, api.Zero) {
				meet[attr.queueID] = struct{}{}
				klog.V(4).Infof("queue <%s> is meet", attr.name)
			} else if reflect.DeepEqual(attr.deserved, oldDeserved) {
				meet[attr.queueID] = struct{}{}
				klog.V(4).Infof("queue <%s> is meet cause of the capability", attr.name)
			}
			pp.updateShare(attr)

			klog.V(4).Infof("The attributes of queue <%s> in proportion: deserved <%v>, allocate <%v>, request <%v>, share <%0.2f>",
				attr.name, attr.deserved, attr.allocated, attr.request, attr.share)

			increased, decreased := attr.deserved.Diff(oldDeserved, api.Zero)
			increasedDeserved.Add(increased)
			decreasedDeserved.Add(decreased)

			// Record metrics
			metrics.UpdateQueueDeserved(attr.name, attr.deserved.MilliCPU, attr.deserved.Memory)
		}

		remaining.Sub(increasedDeserved).Add(decreasedDeserved)
		klog.V(4).Infof("Remaining resource is  <%s>", remaining)
		if remaining.IsEmpty() || reflect.DeepEqual(remaining, oldRemaining) {
			klog.V(4).Infof("Exiting when remaining is empty or no queue has more reosurce request:  <%v>", remaining)
			break
		}
	}

	ssn.AddQueueOrderFn(pp.Name(), func(l, r interface{}) int {
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

	ssn.AddReclaimableFn(pp.Name(), func(reclaimer *api.TaskInfo, reclaimees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		var victims []*api.TaskInfo
		allocations := map[api.QueueID]*api.Resource{}

		for _, reclaimee := range reclaimees {
			job := ssn.Jobs[reclaimee.Job]
			attr := pp.queueOpts[job.Queue]

			if _, found := allocations[job.Queue]; !found {
				allocations[job.Queue] = attr.allocated.Clone()
			}
			allocated := allocations[job.Queue]
			if allocated.LessPartly(reclaimer.Resreq, api.Zero) {
				klog.V(3).Infof("Failed to allocate resource for Task <%s/%s> in Queue <%s>, not enough resource.",
					reclaimee.Namespace, reclaimee.Name, job.Queue)
				continue
			}

			if !allocated.LessEqual(attr.deserved, api.Zero) {
				allocated.Sub(reclaimee.Resreq)
				victims = append(victims, reclaimee)
			}
		}
		klog.V(4).Infof("Victims from proportion plugins are %+v", victims)
		return victims, util.Permit
	})

	ssn.AddOverusedFn(pp.Name(), func(obj interface{}) bool {
		queue := obj.(*api.QueueInfo)
		attr := pp.queueOpts[queue.UID]

		overused := attr.deserved.LessEqual(attr.allocated, api.Zero)
		metrics.UpdateQueueOverused(attr.name, overused)
		if overused {
			klog.V(3).Infof("Queue <%v>: deserved <%v>, allocated <%v>, share <%v>",
				queue.Name, attr.deserved, attr.allocated, attr.share)
		}

		return overused
	})

	ssn.AddUnderusedResourceFn(pp.Name(), func(queue *api.QueueInfo) api.ResourceNameList {
		underUsedResNames := api.ResourceNameList{}
		attr := pp.queueOpts[queue.UID]

		_, underUsedResource := attr.allocated.Diff(attr.deserved, api.Zero)
		if underUsedResource.MilliCPU >= api.GetMinResource() {
			underUsedResNames = append(underUsedResNames, v1.ResourceCPU)
		}
		if underUsedResource.Memory >= api.GetMinResource() {
			underUsedResNames = append(underUsedResNames, v1.ResourceMemory)
		}
		for rName := range underUsedResource.ScalarResources {
			underUsedResNames = append(underUsedResNames, rName)
		}
		klog.V(3).Infof("Queue <%v>: deserved <%v>, allocated <%v>, share <%v>, underUsedResName %v",
			queue.Name, attr.deserved, attr.allocated, attr.share, underUsedResNames)

		return underUsedResNames
	})

	ssn.AddJobEnqueueableFn(pp.Name(), func(obj interface{}) int {
		job := obj.(*api.JobInfo)
		queueID := job.Queue
		attr := pp.queueOpts[queueID]
		queue := ssn.Queues[queueID]
		// If no capability is set, always enqueue the job.
		if len(queue.Queue.Spec.Capability) == 0 {
			klog.V(4).Infof("Capability of queue <%s> was not set, allow job <%s/%s> to Inqueue.",
				queue.Name, job.Namespace, job.Name)
			return util.Permit
		}

		if job.PodGroup.Spec.MinResources == nil {
			klog.V(4).Infof("job %s MinResources is null.", job.Name)
			return util.Permit
		}
		minReq := job.GetMinResources()

		klog.V(5).Infof("job %s min resource <%s>", job.Name, minReq.String())
		klog.V(5).Infof("queue %s capability <%s>", queue.Name, api.NewResource(queue.Queue.Spec.Capability).String())
		klog.V(5).Infof("queue %s allocated <%s>", queue.Name, attr.allocated.String())
		// The queue resource quota limit has not reached
		inqueue := minReq.Add(attr.allocated).Add(attr.inqueue).LessEqual(api.NewResource(queue.Queue.Spec.Capability), api.Infinity)
		if inqueue {
			attr.inqueue.Add(job.GetMinResources())
			return util.Permit
		}
		return util.Reject
	})

	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := pp.queueOpts[job.Queue]
			attr.allocated.Add(event.Task.Resreq)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)

			pp.updateShare(attr)

			klog.V(4).Infof("Proportion AllocateFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)
		},
		DeallocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := pp.queueOpts[job.Queue]
			attr.allocated.Sub(event.Task.Resreq)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)

			pp.updateShare(attr)

			klog.V(4).Infof("Proportion EvictFunc: task <%v/%v>, resreq <%v>,  share <%v>",
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

	// TODO(k82cn): how to handle fragment issues?
	for _, rn := range attr.deserved.ResourceNames() {
		share := helpers.Share(attr.allocated.Get(rn), attr.deserved.Get(rn))
		if share > res {
			res = share
		}
	}

	attr.share = res
	metrics.UpdateQueueShare(attr.name, attr.share)
}
