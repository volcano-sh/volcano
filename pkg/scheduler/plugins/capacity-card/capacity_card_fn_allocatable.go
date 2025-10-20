/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced gang scheduling validation with task-level validity checks
- Improved preemption logic to respect gang scheduling constraints
- Added support for job starving detection and enhanced pipeline state management

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

package capacitycard

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// AllocatableFn checks whether the task can be allocated, which does the queue-level capacity check.
// If it returns true, which will do the following aspects to resources:
// 1. Pod phase will be changed from Pending to Running.
func (p *Plugin) AllocatableFn(queue *api.QueueInfo, candidate *api.TaskInfo) bool {
	if queue.Queue.Status.State != scheduling.QueueStateOpen {
		klog.V(3).Infof(
			"Queue <%s> current state: %s, cannot allocate task <%s/%s>.",
			queue.Name, queue.Queue.Status.State, candidate.Namespace, candidate.Name,
		)
		return false
	}
	return p.isTaskAllocatable(p.queueOpts[queue.UID], candidate)
}

// isTaskAllocatable checks whether the task can be allocated in the queue according to the queue's real capability.
func (p *Plugin) isTaskAllocatable(qAttr *queueAttr, ti *api.TaskInfo) bool {
	taskReqResource, err := p.GetTaskRequestResources(ti)
	if err != nil {
		klog.V(5).Infof(
			"Get request resource for Task <%s/%s> failed, Queue <%s>, error: <%s>",
			ti.Namespace, ti.Name, qAttr.name, err.Error(),
		)
		if ti.Pod != nil {
			eventRecorder.Eventf(
				ti.Pod, v1.EventTypeWarning, EventTypeGetTaskRequestResourceFailed,
				"Get request resource failed, Queue <%s>, error: <%s>",
				qAttr.name, err.Error(),
			)
		}
		return false
	}

	var (
		queueCapability    = qAttr.capability
		totalToBeAllocated = qAttr.allocated.Clone().Add(taskReqResource)
	)
	if totalToBeAllocated == nil {
		klog.V(5).Infof(
			"Task <%s/%s>, Queue <%s> totalToBeAllocated is nil, allow it to allocate",
			ti.Namespace, ti.Name, qAttr.name,
		)
		return true
	}
	if taskReqResource == nil {
		if ok := totalToBeAllocated.LessEqual(queueCapability, api.Zero); !ok {
			klog.V(5).Infof(
				"Task <%s/%s>, Queue <%s> capability <%s> is empty, deny it to allocate",
				ti.Namespace, ti.Name, qAttr.name, queueCapability.String(),
			)
			if ti.Pod != nil {
				eventRecorder.Eventf(
					ti.Pod, v1.EventTypeWarning, EventTypeEmptyQueueCapability,
					"Queue <%s> capability <%s> is empty, deny it to allocate",
					qAttr.name, queueCapability.String(),
				)
			}
			return false
		}
		klog.V(5).Infof(
			"Task <%s/%s>, Queue <%s> request is nil, allow it to allocate",
			ti.Namespace, ti.Name, qAttr.name,
		)
		return true
	}

	// check cpu and memory
	if taskReqResource.MilliCPU > 0 && totalToBeAllocated.MilliCPU > queueCapability.MilliCPU {
		klog.V(2).Infof(
			"Task <%s/%s>, Queue <%s> has no enough CPU, request <%v>, total would be <%v>, capability <%v>",
			ti.Namespace, ti.Name, qAttr.name,
			taskReqResource.MilliCPU, totalToBeAllocated.MilliCPU, queueCapability.MilliCPU,
		)
		if ti.Pod != nil {
			eventRecorder.Eventf(
				ti.Pod, v1.EventTypeWarning, EventTypeInsufficientCPUQuota,
				"Queue <%s> has insufficient CPU quota: requested <%v>, total would be <%v>, but capability is <%v>",
				qAttr.name, taskReqResource.MilliCPU, totalToBeAllocated.MilliCPU, queueCapability.MilliCPU,
			)
		}
		klog.V(3).Infof(
			"Queue <%s> has insufficient CPU quota: requested <%v>, total would be <%v>, but capability is <%v>",
			qAttr.name, taskReqResource.MilliCPU, totalToBeAllocated.MilliCPU, queueCapability.MilliCPU,
		)
		return false
	}
	if taskReqResource.Memory > 0 && totalToBeAllocated.Memory > queueCapability.Memory {
		var (
			taskReqResourceMi    = taskReqResource.Memory / 1024 / 1024
			totalToBeAllocatedMi = totalToBeAllocated.Memory / 1024 / 1024
			queueCapabilityMi    = queueCapability.Memory / 1024 / 1024
		)
		klog.V(2).Infof(
			"Task <%s/%s>, Queue <%s> has no enough Memory, request <%v Mi>, total would be <%v Mi>, capability <%v Mi>",
			ti.Namespace, ti.Name, qAttr.name, taskReqResourceMi, totalToBeAllocatedMi, queueCapabilityMi,
		)
		if ti.Pod != nil {
			eventRecorder.Eventf(
				ti.Pod, v1.EventTypeWarning, EventTypeInsufficientMemoryQuota,
				"Queue <%s> has insufficient memory quota: requested <%v Mi>, total would be <%v Mi>, but capability is <%v Mi>",
				qAttr.name, taskReqResourceMi, totalToBeAllocatedMi, queueCapabilityMi,
			)
		}
		return false
	}

	// if r.scalar is nil, whatever rr.scalar is, r is less or equal to rr
	if totalToBeAllocated.ScalarResources == nil {
		return true
	}

	for scalarName, scalarQuant := range taskReqResource.ScalarResources {
		if api.IsIgnoredScalarResource(scalarName) {
			continue
		}
		checkResult := CheckSingleScalarResource(
			scalarName, scalarQuant, totalToBeAllocated, queueCapability,
		)
		if checkResult.Ok {
			continue
		}
		klog.V(2).Infof(
			"Task <%s/%s>, Queue <%s> has no enough %s, request <%v>, total would be <%v>, capability <%v>",
			ti.Namespace, ti.Name, qAttr.name,
			checkResult.NoEnoughScalarName,
			checkResult.NoEnoughScalarCount,
			checkResult.ToBeUsedScalarQuant,
			checkResult.QueueCapabilityQuant,
		)
		if ti.Pod != nil {
			eventRecorder.Eventf(
				ti.Pod, v1.EventTypeWarning, EventTypeInsufficientScalarQuota,
				"Queue <%s> has insufficient <%s> quota: requested <%v>, total would be <%v>, but capability is <%v>",
				qAttr.name,
				checkResult.NoEnoughScalarName,
				checkResult.NoEnoughScalarCount,
				checkResult.ToBeUsedScalarQuant,
				checkResult.QueueCapabilityQuant,
			)
		}
		return false
	}
	return true
}
