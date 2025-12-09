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

package api

import (
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
)

// SchedulingContext contains all information needed for scheduling a task
type SchedulingContext struct {
	// Task is the task to be scheduled
	Task *api.TaskInfo
	// QueuedPodInfo is the original pod info from the scheduling queue
	QueuedPodInfo *framework.QueuedPodInfo
}

type BindContext struct {
	SchedCtx *SchedulingContext
	// Extensions stores extra bind context information of each plugin
	Extensions map[string]cache.BindContextExtension
}

// PodScheduleResult contains the scheduling result for a pod
type PodScheduleResult struct {
	SchedCtx         *SchedulingContext
	BindContext      *BindContext
	SuggestedNodes   []*api.NodeInfo
	ScheduleCycleUID types.UID
}
