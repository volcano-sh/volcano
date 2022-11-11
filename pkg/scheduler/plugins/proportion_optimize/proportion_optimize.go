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

package proportion_optimize

import (
	"reflect"
	"volcano.sh/volcano/pkg/scheduler/plugins/common"
	scheutil "volcano.sh/volcano/pkg/scheduler/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/helpers"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

const (
	// PluginName indicates name of volcano scheduler plugin.
	PluginName            = "proportion_optimize"
	Guarantee             = "proportion_optimize.GuaranteeEnable"
	Elastic               = "proportion_optimize.ElasticEnable"
	PriorityType          = "proportion_optimize.PriorityType"
	PriorityTypeWeight    = "Weight"
	PriorityTypeGuarantee = "Guarantee"
)

type proportionOptimizePlugin struct {
	totalResource *api.Resource
	queueOpts     map[api.QueueID]*common.QueueAttr
	// Arguments given for the plugin
	pluginArguments          framework.Arguments
	proportionOptimizeEnable *common.GuaranteeElasticEnable
	priorityType             string
}

// New return proportion action
func New(arguments framework.Arguments) framework.Plugin {
	return &proportionOptimizePlugin{
		totalResource:            api.EmptyResource(),
		queueOpts:                map[api.QueueID]*common.QueueAttr{},
		pluginArguments:          arguments,
		proportionOptimizeEnable: &common.GuaranteeElasticEnable{},
		priorityType:             PriorityTypeWeight,
	}
}

func (pp *proportionOptimizePlugin) Name() string {
	return PluginName
}

func (pp *proportionOptimizePlugin) OnSessionOpen(ssn *framework.Session) {
	// Prepare scheduling data for this session.
	pp.totalResource.Add(ssn.TotalResource)

	klog.V(4).Infof("The total resource is <%v>", pp.totalResource)

	pp.proportionOptimizeEnable = common.EnableGuaranteeElastic(pp.pluginArguments, ssn, Guarantee, Elastic)
	priorityType, ok := pp.pluginArguments[PriorityType].(string)
	if !ok || (priorityType != PriorityTypeGuarantee || priorityType != PriorityTypeWeight) {
		pp.priorityType = PriorityTypeWeight
	}
	klog.V(4).Infof("PriorityType of proportion_optimize is <%v>", pp.totalResource)
	remaining := pp.totalResource.Clone()
	// Build attributes for Queues.
	for _, job := range ssn.Jobs {
		klog.V(4).Infof("Considering Job <%s/%s>.", job.Namespace, job.Name)
		if _, found := pp.queueOpts[job.Queue]; !found {
			common.GenerateQueueCommonInfo(ssn.Queues[job.Queue], job, pp.queueOpts, remaining, pp.proportionOptimizeEnable)
		}

		attr := pp.queueOpts[job.Queue]
		common.GenerateQueueResource(attr, job, pp.proportionOptimizeEnable)
	}

	// if PriorityType is guarantee
	// if any dimension in guarantee is beyond deserved，try to make weight of queue meet to max(dimension in guarantee/deserved)
	if pp.proportionOptimizeEnable.GuaranteeEnable && pp.priorityType == PriorityTypeGuarantee {
		pp.tryToMeetWeight(remaining)
		for _, attr := range pp.queueOpts {
			klog.V(4).Infof("After meet weight, The weight of queue <%s> in proportion_optimize: Weight <%v>",
				attr.Name, attr.Weight)
		}
	}

	// Record metrics
	common.RecordMetrics(ssn.Queues, pp.queueOpts)

	meet := map[api.QueueID]struct{}{}
	for {
		totalWeight := int32(0)
		for _, attr := range pp.queueOpts {
			if _, found := meet[attr.QueueID]; found {
				continue
			}
			totalWeight += attr.Weight
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
				attr.Name, attr.Weight, totalWeight)
			if _, found := meet[attr.QueueID]; found {
				continue
			}

			oldDeserved := attr.Deserved.Clone()
			attr.Deserved.Add(remaining.Clone().Multi(float64(attr.Weight) / float64(totalWeight)))

			if attr.RealCapability != nil {
				attr.Deserved.MinDimensionResource(attr.RealCapability, api.Infinity)
			}
			attr.Deserved.MinDimensionResource(attr.Request, api.Zero)

			// only work when PriorityType is guarantee and guarantee is enable
			// it will be no need？
			if pp.proportionOptimizeEnable.GuaranteeEnable && pp.priorityType == PriorityTypeGuarantee {
				attr.Deserved = helpers.Max(attr.Deserved, attr.Guarantee)
			}
			common.UpdateShare(attr)
			klog.V(4).Infof("Format queue <%s> deserved resource to <%v>", attr.Name, attr.Deserved)

			if attr.Request.LessEqual(attr.Deserved, api.Zero) {
				meet[attr.QueueID] = struct{}{}
				klog.V(4).Infof("queue <%s> is meet", attr.Name)
			} else if reflect.DeepEqual(attr.Deserved, oldDeserved) {
				meet[attr.QueueID] = struct{}{}
				klog.V(4).Infof("queue <%s> is meet cause of the capability", attr.Name)
			}

			klog.V(4).Infof("The attributes of queue <%s> in proportion_optimize: deserved <%v>, realCapability <%v>, allocate <%v>, request <%v>, elastic <%v>, share <%0.2f>",
				attr.Name, attr.Deserved, attr.RealCapability, attr.Allocated, attr.Request, attr.Elastic, attr.Share)

			increased, decreased := attr.Deserved.Diff(oldDeserved, api.Zero)
			increasedDeserved.Add(increased)
			decreasedDeserved.Add(decreased)

			// Record metrics
			metrics.UpdateQueueDeserved(attr.Name, attr.Deserved.MilliCPU, attr.Deserved.Memory)
		}

		remaining.Sub(increasedDeserved).Add(decreasedDeserved)
		klog.V(4).Infof("Remaining resource is  <%s>", remaining)
		if remaining.IsEmpty() || reflect.DeepEqual(remaining, oldRemaining) {
			klog.V(4).Infof("Exiting when remaining is empty or no queue has more reosurce request:  <%v>", remaining)
			break
		}
	}

	// if PriorityType is weight.
	// try to make dimension of deserved is less guarantee meet to guarantee
	if pp.proportionOptimizeEnable.GuaranteeEnable && pp.priorityType == PriorityTypeWeight {
		pp.tryToMeetGuarantee(remaining)
		for _, attr := range pp.queueOpts {
			common.UpdateShare(attr)
			klog.V(4).Infof("After meet guarantee, The attributes of queue <%s> in proportion_optimize: deserved <%v>, share <%0.2f>",
				attr.Name, attr.Deserved, attr.Share)
		}
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
		klog.V(4).Infof("Victims from proportion_optimize plugins are %+v", victims)
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
			klog.V(4).Infof("job %s MinResources is null.", job.Name)
			return util.Permit
		}
		minReq := job.GetMinResources()

		klog.V(5).Infof("job %s min resource <%s>, queue %s capability <%s> allocated <%s> inqueue <%s> elastic <%s>",
			job.Name, minReq.String(), queue.Name, attr.RealCapability.String(), attr.Allocated.String(), attr.Inqueue.String(), attr.Elastic.String())
		// The queue resource quota limit has not reached
		totalNeedResource := minReq.Add(attr.Allocated).Add(attr.Inqueue)
		inqueue := false
		if pp.proportionOptimizeEnable.ElasticEnable {
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
			metrics.UpdateQueueAllocated(attr.Name, attr.Allocated.MilliCPU, attr.Allocated.Memory)

			common.UpdateShare(attr)

			klog.V(4).Infof("proportion_optimize AllocateFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.Share)
		},
		DeallocateFunc: func(event *framework.Event) {
			job := ssn.Jobs[event.Task.Job]
			attr := pp.queueOpts[job.Queue]
			attr.Allocated.Sub(event.Task.Resreq)
			metrics.UpdateQueueAllocated(attr.Name, attr.Allocated.MilliCPU, attr.Allocated.Memory)

			common.UpdateShare(attr)

			klog.V(4).Infof("proportion_optimize EvictFunc: task <%v/%v>, resreq <%v>,  share <%v>",
				event.Task.Namespace, event.Task.Name, event.Task.Resreq, attr.Share)
		},
	})
}

func (pp *proportionOptimizePlugin) OnSessionClose(ssn *framework.Session) {
	pp.totalResource = nil
	pp.queueOpts = nil
}

func (pp *proportionOptimizePlugin) tryToMeetWeight(remaining *api.Resource) {
	totalWeight := int32(0)
	for _, attr := range pp.queueOpts {
		totalWeight += attr.Weight
	}
	for _, attr := range pp.queueOpts {
		weightDeserved := remaining.Clone().Multi(float64(attr.Weight) / float64(totalWeight))
		if weightDeserved.LessPartly(attr.Guarantee, api.Zero) {
			// need update weight
			attr.Weight = remaining.MaxWeight(attr.Guarantee)
		}
	}
}

func (pp *proportionOptimizePlugin) tryToMeetGuarantee(remaining *api.Resource) {
	// for cpu
	cpuQueue := scheutil.NewPriorityQueue(func(l interface{}, r interface{}) bool {
		lv := l.(*common.QueueAttr)
		rv := r.(*common.QueueAttr)
		return (lv.Guarantee.MilliCPU - lv.Deserved.MilliCPU) < (rv.Guarantee.MilliCPU - rv.Deserved.MilliCPU)
	})
	for _, attr := range pp.queueOpts {
		if attr.Deserved.MilliCPU < attr.Guarantee.MilliCPU {
			cpuQueue.Push(attr)
		}
	}
	if remaining.MilliCPU > 0 {
		for {
			if cpuQueue.Empty() {
				break
			}
			cpuAvg := remaining.MilliCPU / float64(cpuQueue.Len())
			attr := cpuQueue.Pop().(*common.QueueAttr)
			if attr.Guarantee.MilliCPU-attr.Deserved.MilliCPU >= cpuAvg {
				remaining.MilliCPU -= cpuAvg
				attr.Deserved.MilliCPU += cpuAvg
			} else {
				remaining.MilliCPU = remaining.MilliCPU - (attr.Guarantee.MilliCPU - attr.Deserved.MilliCPU)
				attr.Deserved.MilliCPU = attr.Guarantee.MilliCPU
			}
		}
	}
	// for memory
	memoryQueue := scheutil.NewPriorityQueue(func(l interface{}, r interface{}) bool {
		lv := l.(*common.QueueAttr)
		rv := r.(*common.QueueAttr)
		return (lv.Guarantee.Memory - lv.Deserved.Memory) < (rv.Guarantee.Memory - rv.Deserved.Memory)
	})
	for _, attr := range pp.queueOpts {
		if attr.Deserved.Memory < attr.Guarantee.Memory {
			memoryQueue.Push(attr)
		}
	}
	if remaining.Memory > 0 {
		for {
			if memoryQueue.Empty() {
				break
			}
			memoryAvg := remaining.Memory / float64(memoryQueue.Len())
			attr := memoryQueue.Pop().(*common.QueueAttr)
			if attr.Guarantee.Memory-attr.Deserved.Memory >= memoryAvg {
				remaining.Memory -= memoryAvg
				attr.Deserved.Memory += memoryAvg
			} else {
				remaining.Memory = remaining.Memory - (attr.Guarantee.Memory - attr.Deserved.Memory)
				attr.Deserved.Memory = attr.Guarantee.Memory
			}
		}
	}
	// for others
	for name, _ := range remaining.ScalarResources {
		otherQueue := scheutil.NewPriorityQueue(func(l interface{}, r interface{}) bool {
			lv := l.(*common.QueueAttr)
			rv := r.(*common.QueueAttr)
			return (lv.Guarantee.ScalarResources[name] - lv.Deserved.ScalarResources[name]) < (rv.Guarantee.ScalarResources[name] - rv.Deserved.ScalarResources[name])
		})
		for _, attr := range pp.queueOpts {
			if attr.Deserved.ScalarResources[name] < attr.Guarantee.ScalarResources[name] {
				otherQueue.Push(attr)
			}
		}
		if remaining.ScalarResources[name] > 0 {
			for {
				if otherQueue.Empty() {
					break
				}
				otherAvg := remaining.ScalarResources[name] / float64(otherQueue.Len())
				attr := otherQueue.Pop().(*common.QueueAttr)
				if attr.Guarantee.ScalarResources[name]-attr.Deserved.ScalarResources[name] >= otherAvg {
					remaining.ScalarResources[name] -= otherAvg
					attr.Deserved.ScalarResources[name] += otherAvg
				} else {
					remaining.ScalarResources[name] = remaining.ScalarResources[name] - (attr.Guarantee.ScalarResources[name] - attr.Deserved.ScalarResources[name])
					attr.Deserved.ScalarResources[name] = attr.Guarantee.ScalarResources[name]
				}
			}
		}
	}
}
