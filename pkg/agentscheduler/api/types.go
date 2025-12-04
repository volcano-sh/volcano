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
