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

package api

import (
	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework/v1alpha1"
)

// TaskStatus defines the status of a task/pod.
type TaskStatus int

const (
	// Pending means the task is pending in the apiserver.
	Pending TaskStatus = 1 << iota

	// Allocated means the scheduler assigns a host to it.
	Allocated

	// Pipelined means the scheduler assigns a host to wait for releasing resource.
	Pipelined

	// Binding means the scheduler send Bind request to apiserver.
	Binding

	// Bound means the task/Pod bounds to a host.
	Bound

	// Running means a task is running on the host.
	Running

	// Releasing means a task/pod is deleted.
	Releasing

	// Succeeded means that all containers in the pod have voluntarily terminated
	// with a container exit code of 0, and the system is not going to restart any of these containers.
	Succeeded

	// Failed means that all containers in the pod have terminated, and at least one container has
	// terminated in a failure (exited with a non-zero exit code or was stopped by the system).
	Failed

	// Unknown means the status of task/pod is unknown to the scheduler.
	Unknown
)

func (ts TaskStatus) String() string {
	switch ts {
	case Pending:
		return "Pending"
	case Allocated:
		return "Allocated"
	case Pipelined:
		return "Pipelined"
	case Binding:
		return "Binding"
	case Bound:
		return "Bound"
	case Running:
		return "Running"
	case Releasing:
		return "Releasing"
	case Succeeded:
		return "Succeeded"
	case Failed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// NodePhase defines the phase of node
type NodePhase int

const (
	// Ready means the node is ready for scheduling
	Ready NodePhase = 1 << iota
	// NotReady means the node is not ready for scheduling
	NotReady
)

func (np NodePhase) String() string {
	switch np {
	case Ready:
		return "Ready"
	case NotReady:
		return "NotReady"
	}

	return "Unknown"
}

// validateStatusUpdate validates whether the status transfer is valid.
func validateStatusUpdate(oldStatus, newStatus TaskStatus) error {
	return nil
}

// LessFn is the func declaration used by sort or priority queue.
type LessFn func(interface{}, interface{}) bool

// CompareFn is the func declaration used by sort or priority queue.
type CompareFn func(interface{}, interface{}) int

// ValidateFn is the func declaration used to check object's status.
type ValidateFn func(interface{}) bool

// ValidateResult is struct to which can used to determine the result
type ValidateResult struct {
	Pass    bool
	Reason  string
	Message string
}

// ValidateExFn is the func declaration used to validate the result.
type ValidateExFn func(interface{}) *ValidateResult

// VoteFn is the func declaration used to check object's complicated status.
type VoteFn func(interface{}) int

// JobEnqueuedFn is the func declaration used to call after job enqueued.
type JobEnqueuedFn func(interface{})

// PredicateFn is the func declaration used to predicate node for task.
type PredicateFn func(*TaskInfo, *NodeInfo) error

// BestNodeFn is the func declaration used to return the nodeScores to plugins.
type BestNodeFn func(*TaskInfo, map[float64][]*NodeInfo) *NodeInfo

// EvictableFn is the func declaration used to evict tasks.
type EvictableFn func(*TaskInfo, []*TaskInfo) ([]*TaskInfo, int)

// NodeOrderFn is the func declaration used to get priority score for a node for a particular task.
type NodeOrderFn func(*TaskInfo, *NodeInfo) (float64, error)

// BatchNodeOrderFn is the func declaration used to get priority score for ALL nodes for a particular task.
type BatchNodeOrderFn func(*TaskInfo, []*NodeInfo) (map[string]float64, error)

// NodeMapFn is the func declaration used to get priority score for a node for a particular task.
type NodeMapFn func(*TaskInfo, *NodeInfo) (float64, error)

// NodeReduceFn is the func declaration used to reduce priority score for a node for a particular task.
type NodeReduceFn func(*TaskInfo, k8sframework.NodeScoreList) error

// NodeOrderMapFn is the func declaration used to get priority score of all plugins for a node for a particular task.
type NodeOrderMapFn func(*TaskInfo, *NodeInfo) (map[string]float64, float64, error)

// NodeOrderReduceFn is the func declaration used to reduce priority score of all nodes for a plugin for a particular task.
type NodeOrderReduceFn func(*TaskInfo, map[string]k8sframework.NodeScoreList) (map[string]float64, error)

// TargetJobFn is the func declaration used to select the target job satisfies some conditions
type TargetJobFn func([]*JobInfo) *JobInfo

// ReservedNodesFn is the func declaration used to select the reserved nodes
type ReservedNodesFn func()

// VictimTasksFn is the func declaration used to select victim tasks
type VictimTasksFn func() []*TaskInfo

// AllocatableFn is the func declaration used to check whether the task can be allocated
type AllocatableFn func(*QueueInfo, *TaskInfo) bool
