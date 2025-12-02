/*
Copyright 2025 The Volcano Authors.

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

package framework

import (
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// SchedulingContext contains all information needed for scheduling a task
type SchedulingContext struct {
	// Task is the task to be scheduled
	Task *api.TaskInfo
	// QueuedPodInfo is the original pod info from the scheduling queue
	QueuedPodInfo *k8sframework.QueuedPodInfo
}

// Action is the interface of agent scheduler action.
type Action interface {
	// Name returns the unique name of Action.
	Name() string

	// Initialize initializes the allocator plugins.
	Initialize()

	// Execute allocates resources for the given task.
	Execute(fwk *Framework, schedCtx *SchedulingContext)

	// UnInitialize un-initializes the allocator plugins.
	UnInitialize()
}

// Plugin is the interface of agent scheduler plugin
type Plugin interface {
	// Name returns the unique name of Plugin.
	Name() string

	// OnPluginInit initializes the plugin. It is called once when the framework is created.
	OnPluginInit(fwk *Framework)

	// OnCycleStart is called at the beginning of a scheduling cycle.
	OnCycleStart(fwk *Framework)

	// OnCycleEnd is called at the end of a scheduling cycle.
	OnCycleEnd(fwk *Framework)
}
