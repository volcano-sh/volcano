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

package capacity

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/common"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName = "capacity"
	Guarantee  = "capacity.GuaranteeEnable"
	Elastic    = "capacity.ElasticEnable"
)

type capacityPlugin struct {
	totalResource *api.Resource
	queueOpts     map[api.QueueID]*common.QueueAttr
	// Arguments given for the plugin
	pluginArguments framework.Arguments
	capacityEnable  *common.GuaranteeElasticEnable
}

type queueAttr struct {
	queueID api.QueueID
	name    string
	weight  int32
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

// New return proportion action
func New(arguments framework.Arguments) framework.Plugin {
	return &capacityPlugin{
		totalResource:   api.EmptyResource(),
		queueOpts:       map[api.QueueID]*common.QueueAttr{},
		pluginArguments: arguments,
		capacityEnable:  &common.GuaranteeElasticEnable{},
	}
}

func (pp *capacityPlugin) Name() string {
	return PluginName
}

func (pp *capacityPlugin) OnSessionOpen(ssn *framework.Session) {
	// Prepare scheduling data for this session.
	pp.totalResource.Add(ssn.TotalResource)

	klog.V(4).Infof("The total resource is <%v>", pp.totalResource)

	pp.capacityEnable = common.EnableGuaranteeElastic(pp.pluginArguments, ssn, Guarantee, Elastic)
	totalRes := pp.totalResource.Clone()
	// Build attributes for Queues.
	for _, job := range ssn.Jobs {
		klog.V(4).Infof("Considering Job <%s/%s>.", job.Namespace, job.Name)
		if _, found := pp.queueOpts[job.Queue]; !found {
			common.GenerateQueueCommonInfo(ssn.Queues[job.Queue], job, pp.queueOpts, totalRes, pp.capacityEnable)
		}

		attr := pp.queueOpts[job.Queue]
		common.GenerateQueueResource(attr, job, pp.capacityEnable)
	}

	// Record metrics
	common.RecordMetrics(ssn.Queues, pp.queueOpts)
	common.UpdateQueuePendingTaskMetric(ssn, pp.queueOpts)

	// generate deserved resource for queue
	for _, attr := range pp.queueOpts {
		attr.Deserved = attr.RealCapability.Clone()
		attr.Deserved.MinDimensionResource(attr.Request, api.Zero)
		if pp.capacityEnable.GuaranteeEnable {
			attr.Deserved = helpers.Max(attr.Deserved, attr.Guarantee)
		}
		common.UpdateShare(attr)
		klog.V(5).Infof("Queue %s capacity <%s> realCapacity <%s> allocated <%s> request <%s> deserved <%s> inqueue <%s> elastic <%s>",
			attr.Name, attr.Capability.String(), attr.RealCapability.String(), attr.Allocated.String(), attr.Request.String(),
			attr.Deserved.String(), attr.Inqueue.String(), attr.Elastic.String())
	}

	ssn.AddQueueOrderFn(pp.Name(), func(l, r interface{}) int {
		lv := l.(*api.QueueInfo)
		rv := r.(*api.QueueInfo)

		if pp.queueOpts[lv.UID].Share == pp.queueOpts[rv.UID].Share {
			return 0
		}

		if pp.queueOpts[lv.UID].Share < pp.queueOpts[rv.UID].Share {
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
				allocations[job.Queue] = attr.Allocated.Clone()
			}
			allocated := allocations[job.Queue]
			if allocated.LessPartly(reclaimer.Resreq, api.Zero) {
				klog.V(3).Infof("Failed to allocate resource for Task <%s/%s> in Queue <%s>, not enough resource.",
					reclaimee.Namespace, reclaimee.Name, job.Queue)
				continue
			}

			if !allocated.LessEqual(attr.Deserved, api.Zero) {
				allocated.Sub(reclaimee.Resreq)
				victims = append(victims, reclaimee)
			}
		}
		klog.V(4).Infof("Victims from capacity plugins are %+v", victims)
		return victims, util.Permit
	})

	ssn.AddOverusedFn(pp.Name(), func(obj interface{}) bool {
		queue := obj.(*api.QueueInfo)
		attr := pp.queueOpts[queue.UID]

		overused := attr.Deserved.LessEqual(attr.Allocated, api.Zero)
		metrics.UpdateQueueOverused(attr.Name, overused)
		if overused {
			klog.V(3).Infof("Queue <%v>: deserved <%v>, allocated <%v>, share <%v>",
				queue.Name, attr.Deserved, attr.Allocated, attr.Share)
		}

		return overused
	})

	ssn.AddAllocatableFn(pp.Name(), func(queue *api.QueueInfo, candidate *api.TaskInfo) bool {
		attr := pp.queueOpts[queue.UID]

		free, _ := attr.Deserved.Diff(attr.Allocated, api.Zero)
		allocatable := candidate.Resreq.LessEqual(free, api.Zero)
		if !allocatable {
			klog.V(3).Infof("Queue <%v>: deserved <%v>, allocated <%v>; Candidate <%v>: resource request <%v>",
				queue.Name, attr.Deserved, attr.Allocated, candidate.Name, candidate.Resreq)
		}

		return allocatable
	})

	ssn.AddJobEnqueueableFn(pp.Name(), func(obj interface{}) int {
		job := obj.(*api.JobInfo)
		queueID := job.Queue
		attr := pp.queueOpts[queueID]
		queue := ssn.Queues[queueID]
		// If no capability is set, always enqueue the job.
		if attr.RealCapability == nil {
			klog.V(4).Infof("Capability of queue <%s> was not set, allow job <%s/%s> to Inqueue.",
				queue.Name, job.Namespace, job.Name)
			return util.Permit
		}

		if job.PodGroup.Spec.MinResources == nil {
			klog.V(4).Infof("job %s MinResources is null, allow it to Inqueue.", job.Name)
			return util.Permit
		}
		minReq := job.GetMinResources()

		klog.V(5).Infof("job %s min resource <%s>, queue %s capacity <%s> realCapability <%s> allocated <%s> inqueue <%s> elastic <%s>",
			job.Name, minReq.String(), queue.Name, attr.Capability.String(), attr.RealCapability.String(), attr.Allocated.String(), attr.Inqueue.String(), attr.Elastic.String())
		// The queue resource quota limit has not reached
		totalNeedResource := minReq.Add(attr.Allocated).Add(attr.Inqueue)
		inqueue := false
		if pp.capacityEnable.ElasticEnable {
			inqueue = totalNeedResource.Sub(attr.Elastic).LessEqual(attr.RealCapability, api.Infinity)
		} else {
			inqueue = totalNeedResource.LessEqual(attr.RealCapability, api.Infinity)
		}
		klog.V(5).Infof("job %s inqueue %v", job.Name, inqueue)
		if inqueue {
			attr.Inqueue.Add(job.GetMinResources())
			return util.Permit
		}
		ssn.RecordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupUnschedulableType), "queue resource quota insufficient")
		return util.Reject
	})

	// Register event handlers.
	ssn.AddEventHandler(&framework.EventHandler{
		AllocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := pp.queueOpts[job.Queue]
			attr.Allocated.Add(event.Task.Resreq)
			metrics.UpdateQueueAllocatedMetrics(attr.Name, attr.Allocated, attr.Capability, attr.Guarantee, attr.RealCapability)

			common.UpdateShare(attr)

			common.UpdateQueuePendingTaskMetric(ssn, pp.queueOpts)

			klog.V(4).Infof("Capacity AllocateFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.Share)
		},
		DeallocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := pp.queueOpts[job.Queue]
			attr.Allocated.Sub(event.Task.Resreq)
			metrics.UpdateQueueAllocatedMetrics(attr.Name, attr.Allocated, attr.Capability, attr.Guarantee, attr.RealCapability)

			common.UpdateShare(attr)

			common.UpdateQueuePendingTaskMetric(ssn, pp.queueOpts)

			klog.V(4).Infof("Capacity EvictFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.Share)
		},
	})
}

func (pp *capacityPlugin) OnSessionClose(ssn *framework.Session) {
	pp.totalResource = nil
	pp.queueOpts = nil
	pp.capacityEnable = nil
}
