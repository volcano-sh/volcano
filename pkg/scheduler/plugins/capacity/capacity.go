/*
Copyright 2024 The Volcano Authors.

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

package capacity

import (
	"math"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	PluginName = "capacity"
)

type capacityPlugin struct {
	totalResource  *api.Resource
	totalGuarantee *api.Resource

	queueOpts map[api.QueueID]*queueAttr
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

type queueAttr struct {
	queueID api.QueueID
	name    string
	share   float64

	deserved  *api.Resource
	allocated *api.Resource
	request   *api.Resource
	// elastic represents the sum of job's elastic resource, job's elastic = job.allocated - job.minAvailable
	elastic *api.Resource
	// inqueue represents the resource request of the inqueue job
	inqueue    *api.Resource
	capability *api.Resource
	// realCapability represents the resource limit of the queue, LessEqual capability
	realCapability *api.Resource
	guarantee      *api.Resource
}

// New return capacityPlugin action
func New(arguments framework.Arguments) framework.Plugin {
	return &capacityPlugin{
		totalResource:   api.EmptyResource(),
		totalGuarantee:  api.EmptyResource(),
		queueOpts:       map[api.QueueID]*queueAttr{},
		pluginArguments: arguments,
	}
}

func (cp *capacityPlugin) Name() string {
	return PluginName
}

func (cp *capacityPlugin) OnSessionOpen(ssn *framework.Session) {
	// Prepare scheduling data for this session.
	cp.totalResource.Add(ssn.TotalResource)

	klog.V(4).Infof("The total resource is <%v>", cp.totalResource)
	for _, queue := range ssn.Queues {
		if len(queue.Queue.Spec.Guarantee.Resource) == 0 {
			continue
		}
		guarantee := api.NewResource(queue.Queue.Spec.Guarantee.Resource)
		cp.totalGuarantee.Add(guarantee)
	}
	klog.V(4).Infof("The total guarantee resource is <%v>", cp.totalGuarantee)
	// Build attributes for Queues.
	for _, job := range ssn.Jobs {
		klog.V(4).Infof("Considering Job <%s/%s>.", job.Namespace, job.Name)
		if _, found := cp.queueOpts[job.Queue]; !found {
			queue := ssn.Queues[job.Queue]
			attr := &queueAttr{
				queueID: queue.UID,
				name:    queue.Name,

				deserved:  api.NewResource(queue.Queue.Spec.Deserved),
				allocated: api.EmptyResource(),
				request:   api.EmptyResource(),
				elastic:   api.EmptyResource(),
				inqueue:   api.EmptyResource(),
				guarantee: api.EmptyResource(),
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
			if len(queue.Queue.Spec.Guarantee.Resource) != 0 {
				attr.guarantee = api.NewResource(queue.Queue.Spec.Guarantee.Resource)
			}
			realCapability := cp.totalResource.Clone().Sub(cp.totalGuarantee).Add(attr.guarantee)
			if attr.capability == nil {
				attr.realCapability = realCapability
			} else {
				realCapability.MinDimensionResource(attr.capability, api.Infinity)
				attr.realCapability = realCapability
			}
			cp.queueOpts[job.Queue] = attr
			klog.V(4).Infof("Added Queue <%s> attributes.", job.Queue)
		}

		attr := cp.queueOpts[job.Queue]
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
			// deduct the resources of scheduling gated tasks in a job when calculating inqueued resources
			// so that it will not block other jobs from being inqueued.
			attr.inqueue.Add(job.DeductSchGatedResources(job.GetMinResources()))
		}

		// calculate inqueue resource for running jobs
		// the judgement 'job.PodGroup.Status.Running >= job.PodGroup.Spec.MinMember' will work on cases such as the following condition:
		// Considering a Spark job is completed(driver pod is completed) while the podgroup keeps running, the allocated resource will be reserved again if without the judgement.
		if job.PodGroup.Status.Phase == scheduling.PodGroupRunning &&
			job.PodGroup.Spec.MinResources != nil &&
			int32(util.CalculateAllocatedTaskNum(job)) >= job.PodGroup.Spec.MinMember {
			inqueued := util.GetInqueueResource(job, job.Allocated)
			attr.inqueue.Add(job.DeductSchGatedResources(inqueued))
		}
		attr.elastic.Add(job.GetElasticResources())
		klog.V(5).Infof("Queue %s allocated <%s> request <%s> inqueue <%s> elastic <%s>",
			attr.name, attr.allocated.String(), attr.request.String(), attr.inqueue.String(), attr.elastic.String())
	}

	for _, attr := range cp.queueOpts {
		if attr.realCapability != nil {
			attr.deserved.MinDimensionResource(attr.realCapability, api.Infinity)
		}
		// When scalar resource not specified in deserved such as "pods", we should skip it and consider deserved resource as infinity.
		attr.deserved.MinDimensionResource(attr.request, api.Infinity)

		attr.deserved = helpers.Max(attr.deserved, attr.guarantee)
		cp.updateShare(attr)
		klog.V(4).Infof("The attributes of queue <%s> in capacity: deserved <%v>, realCapability <%v>, allocate <%v>, request <%v>, elastic <%v>, share <%0.2f>",
			attr.name, attr.deserved, attr.realCapability, attr.allocated, attr.request, attr.elastic, attr.share)
	}

	// Record metrics
	for queueID, queueInfo := range ssn.Queues {
		queue := ssn.Queues[queueID]
		if attr, ok := cp.queueOpts[queueID]; ok {
			metrics.UpdateQueueDeserved(attr.name, attr.deserved.MilliCPU, attr.deserved.Memory)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)
			metrics.UpdateQueueRequest(attr.name, attr.request.MilliCPU, attr.request.Memory)
			metrics.UpdateQueuePodGroupInqueueCount(attr.name, queue.Queue.Status.Inqueue)
			metrics.UpdateQueuePodGroupPendingCount(attr.name, queue.Queue.Status.Pending)
			metrics.UpdateQueuePodGroupRunningCount(attr.name, queue.Queue.Status.Running)
			metrics.UpdateQueuePodGroupUnknownCount(attr.name, queue.Queue.Status.Unknown)
			continue
		}
		deservedCPU, deservedMem := 0.0, 0.0
		if queue.Queue.Spec.Deserved != nil {
			deservedCPU = float64(queue.Queue.Spec.Deserved.Cpu().MilliValue())
			deservedMem = float64(queue.Queue.Spec.Deserved.Memory().Value())
		}
		metrics.UpdateQueueDeserved(queueInfo.Name, deservedCPU, deservedMem)
		metrics.UpdateQueueAllocated(queueInfo.Name, 0, 0)
		metrics.UpdateQueueRequest(queueInfo.Name, 0, 0)
		metrics.UpdateQueuePodGroupInqueueCount(queueInfo.Name, 0)
		metrics.UpdateQueuePodGroupPendingCount(queueInfo.Name, 0)
		metrics.UpdateQueuePodGroupRunningCount(queueInfo.Name, 0)
		metrics.UpdateQueuePodGroupUnknownCount(queueInfo.Name, 0)
	}

	ssn.AddQueueOrderFn(cp.Name(), func(l, r interface{}) int {
		lv := l.(*api.QueueInfo)
		rv := r.(*api.QueueInfo)

		if lv.Queue.Spec.Priority != rv.Queue.Spec.Priority {
			// return negative means high priority
			return int(rv.Queue.Spec.Priority) - int(lv.Queue.Spec.Priority)
		}

		if cp.queueOpts[lv.UID].share == cp.queueOpts[rv.UID].share {
			return 0
		}

		if cp.queueOpts[lv.UID].share < cp.queueOpts[rv.UID].share {
			return -1
		}

		return 1
	})

	ssn.AddReclaimableFn(cp.Name(), func(reclaimer *api.TaskInfo, reclaimees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		var victims []*api.TaskInfo
		allocations := map[api.QueueID]*api.Resource{}

		for _, reclaimee := range reclaimees {
			job := ssn.Jobs[reclaimee.Job]
			attr := cp.queueOpts[job.Queue]

			if _, found := allocations[job.Queue]; !found {
				allocations[job.Queue] = attr.allocated.Clone()
			}
			allocated := allocations[job.Queue]
			if allocated.LessPartly(reclaimer.Resreq, api.Zero) {
				klog.V(3).Infof("Failed to allocate resource for Task <%s/%s> in Queue <%s>, not enough resource.",
					reclaimee.Namespace, reclaimee.Name, job.Queue)
				continue
			}

			exceptReclaimee := allocated.Clone().Sub(reclaimee.Resreq)
			// When scalar resource not specified in deserved such as "pods", we should skip it and consider it as infinity,
			// so the following first condition will be true and the current queue will not be reclaimed.
			if allocated.LessEqual(attr.deserved, api.Infinity) || !attr.guarantee.LessEqual(exceptReclaimee, api.Zero) {
				continue
			}
			allocated.Sub(reclaimee.Resreq)
			victims = append(victims, reclaimee)
		}
		klog.V(4).InfoS("Victims from capacity plugin", "victims", victims, "reclaimer", reclaimer)
		return victims, util.Permit
	})

	ssn.AddPreemptiveFn(cp.Name(), func(obj interface{}, candidate interface{}) bool {
		queue := obj.(*api.QueueInfo)
		task := candidate.(*api.TaskInfo)
		attr := cp.queueOpts[queue.UID]

		overused := attr.deserved.LessEqualWithDimension(attr.allocated, task.InitResreq)
		metrics.UpdateQueueOverused(attr.name, overused)
		if overused {
			klog.V(3).Infof("Queue <%v> can not reclaim, deserved <%v>, allocated <%v>, share <%v>",
				queue.Name, attr.deserved, attr.allocated, attr.share)
		}

		// PreemptiveFn is the opposite of OverusedFn in proportion plugin cause as long as there is a one-dimensional
		// resource whose deserved is greater than allocated, current task can reclaim by preempt others.
		return !overused
	})

	ssn.AddAllocatableFn(cp.Name(), func(queue *api.QueueInfo, candidate *api.TaskInfo) bool {
		attr := cp.queueOpts[queue.UID]

		free, _ := attr.realCapability.Diff(attr.allocated, api.Zero)
		allocatable := candidate.Resreq.LessEqual(free, api.Zero)
		if !allocatable {
			klog.V(3).Infof("Queue <%v>: realCapability <%v>, allocated <%v>; Candidate <%v>: resource request <%v>",
				queue.Name, attr.realCapability, attr.allocated, candidate.Name, candidate.Resreq)
		}

		return allocatable
	})

	ssn.AddJobEnqueueableFn(cp.Name(), func(obj interface{}) int {
		job := obj.(*api.JobInfo)
		queueID := job.Queue
		attr := cp.queueOpts[queueID]
		queue := ssn.Queues[queueID]
		// If no capability is set, always enqueue the job.
		if attr.realCapability == nil {
			klog.V(4).Infof("Capability of queue <%s> was not set, allow job <%s/%s> to Inqueue.",
				queue.Name, job.Namespace, job.Name)
			return util.Permit
		}

		if job.PodGroup.Spec.MinResources == nil {
			klog.V(4).Infof("job %s MinResources is null.", job.Name)
			return util.Permit
		}
		minReq := job.GetMinResources()

		klog.V(5).Infof("job %s min resource <%s>, queue %s capability <%s> allocated <%s> inqueue <%s> elastic <%s>",
			job.Name, minReq.String(), queue.Name, attr.realCapability.String(), attr.allocated.String(), attr.inqueue.String(), attr.elastic.String())
		// The queue resource quota limit has not reached
		r := minReq.Add(attr.allocated).Add(attr.inqueue).Sub(attr.elastic)
		rr := attr.realCapability.Clone()

		for name := range rr.ScalarResources {
			if _, ok := r.ScalarResources[name]; !ok {
				delete(rr.ScalarResources, name)
			}
		}

		inqueue := r.LessEqual(rr, api.Infinity)
		klog.V(5).Infof("job %s inqueue %v", job.Name, inqueue)
		if inqueue {
			attr.inqueue.Add(job.DeductSchGatedResources(job.GetMinResources()))
			return util.Permit
		}
		ssn.RecordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupUnschedulableType), "queue resource quota insufficient")
		return util.Reject
	})

	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := cp.queueOpts[job.Queue]
			attr.allocated.Add(event.Task.Resreq)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)

			cp.updateShare(attr)

			klog.V(4).Infof("Capacity AllocateFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)
		},
		DeallocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := cp.queueOpts[job.Queue]
			attr.allocated.Sub(event.Task.Resreq)
			metrics.UpdateQueueAllocated(attr.name, attr.allocated.MilliCPU, attr.allocated.Memory)

			cp.updateShare(attr)

			klog.V(4).Infof("Capacity EvictFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.share)
		},
	})
}

func (cp *capacityPlugin) OnSessionClose(ssn *framework.Session) {
	cp.totalResource = nil
	cp.totalGuarantee = nil
	cp.queueOpts = nil
}

func (cp *capacityPlugin) updateShare(attr *queueAttr) {
	res := float64(0)

	for _, rn := range attr.deserved.ResourceNames() {
		share := helpers.Share(attr.allocated.Get(rn), attr.deserved.Get(rn))
		if share > res {
			res = share
		}
	}

	attr.share = res
	metrics.UpdateQueueShare(attr.name, attr.share)
}
