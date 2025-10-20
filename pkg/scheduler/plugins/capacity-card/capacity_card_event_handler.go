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
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// OnAllocate is invoked when a task is allocated.
// This callback handler is very critical for capacity plugin to maintain the queue allocated resource,
// especially card resource is being allocated during current scheduling session.
func (p *Plugin) OnAllocate(ssn *framework.Session, event *framework.Event) {
	var (
		task    = event.Task
		taskJob = ssn.Jobs[task.Job]
		qAttr   = p.queueOpts[taskJob.Queue]
	)

	taskReqResource, err := p.GetTaskRequestResources(task)
	if err != nil {
		klog.V(5).Infof(
			"Get request resource for Task <%s/%s> failed, error: <%s>",
			task.Namespace, task.Name, err.Error(),
		)
		return
	}
	qAttr.allocated.Add(taskReqResource)

	metrics.UpdateQueueAllocated(
		qAttr.name, qAttr.allocated.MilliCPU, qAttr.allocated.Memory, qAttr.allocated.ScalarResources,
	)
	p.updateShare(qAttr)
	klog.V(4).Infof(
		"Capacity AllocateFunc: task <%v/%v>, resreq <%v>, share <%v>",
		task.Namespace, task.Name, taskReqResource, qAttr.share,
	)
}

// OnDeallocate is invoked when a task is deallocated.
func (p *Plugin) OnDeallocate(ssn *framework.Session, event *framework.Event) {
	var (
		task    = event.Task
		taskJob = ssn.Jobs[task.Job]
		qAttr   = p.queueOpts[taskJob.Queue]
	)

	taskReqResource, err := p.GetTaskRequestResources(task)
	if err != nil {
		klog.V(5).Infof(
			"Get request resource for Task <%s/%s> failed, error: <%s>",
			task.Namespace, task.Name, err.Error(),
		)
		return
	}
	qAttr.allocated.Sub(taskReqResource)

	metrics.UpdateQueueAllocated(
		qAttr.name, qAttr.allocated.MilliCPU, qAttr.allocated.Memory, qAttr.allocated.ScalarResources,
	)
	klog.V(4).Infof(
		"Capacity EvictFunc: task <%v/%v>, resreq <%v>, share <%v>",
		task.Namespace, task.Name, taskReqResource, qAttr.share,
	)
}
