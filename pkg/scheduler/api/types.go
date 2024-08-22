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
	"strings"

	k8sframework "k8s.io/kubernetes/pkg/scheduler/framework"
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

// ValidateWithCandidateFn behaves like ValidateFn but take the candidate task into consideration.
type ValidateWithCandidateFn func(interface{}, interface{}) bool

// ValidateResult is struct to which can used to determine the result
type ValidateResult struct {
	Pass    bool
	Reason  string
	Message string
}

// These are predefined codes used in a Status.
const (
	// Success means that plugin ran correctly and found pod schedulable.
	// NOTE: A nil status is also considered as "Success".
	Success int = iota
	// Error is used for internal plugin errors, unexpected input, etc.
	Error
	// Unschedulable is used when a plugin finds a pod unschedulable. The scheduler might attempt to
	// preempt other pods to get this pod scheduled. Use UnschedulableAndUnresolvable to make the
	// scheduler skip preemption.
	// The accompanying status message should explain why the pod is unschedulable.
	Unschedulable
	// UnschedulableAndUnresolvable is used when a plugin finds a pod unschedulable and
	// preemption would not change anything. Plugins should return Unschedulable if it is possible
	// that the pod can get scheduled with preemption.
	// The accompanying status message should explain why the pod is unschedulable.
	UnschedulableAndUnresolvable
	// Wait is used when a Permit plugin finds a pod scheduling should wait.
	Wait
	// Skip is used when a Bind plugin chooses to skip binding.
	Skip
	// There is a Pending status in k8s.
	// Pending means that the scheduling process is finished successfully,
	// but the plugin wants to stop the scheduling cycle/binding cycle here.
)

type Status struct {
	Code   int
	Reason string
	Plugin string
}

// String represents status string
func (s Status) String() string {
	return s.Reason
}

type StatusSets []*Status

func (s StatusSets) ContainsUnschedulable() bool {
	for _, status := range s {
		if status == nil {
			continue
		}
		if status.Code == Unschedulable {
			return true
		}
	}
	return false
}

func (s StatusSets) ContainsUnschedulableAndUnresolvable() bool {
	for _, status := range s {
		if status == nil {
			continue
		}
		if status.Code == UnschedulableAndUnresolvable {
			return true
		}
	}
	return false
}

func (s StatusSets) ContainsErrorSkipOrWait() bool {
	for _, status := range s {
		if status == nil {
			continue
		}
		if status.Code == Error || status.Code == Skip || status.Code == Wait {
			return true
		}
	}
	return false
}

// Message return the message generated from StatusSets
func (s StatusSets) Message() string {
	if s == nil {
		return ""
	}
	all := make([]string, 0, len(s))
	for _, status := range s {
		if status.Reason == "" {
			continue
		}
		all = append(all, status.Reason)
	}
	return strings.Join(all, ",")
}

// Reasons return the reasons list
func (s StatusSets) Reasons() []string {
	if s == nil {
		return nil
	}
	all := make([]string, 0, len(s))
	for _, status := range s {
		if status.Reason == "" {
			continue
		}
		all = append(all, status.Reason)
	}
	return all
}

// ConvertPredicateStatus return predicate status from k8sframework status
func ConvertPredicateStatus(status *k8sframework.Status) *Status {
	internalStatus := &Status{}
	if status != nil {
		internalStatus.Plugin = status.Plugin() // function didn't check whether Status is nil
	}
	switch status.Code() {
	case k8sframework.Error:
		internalStatus.Code = Error
	case k8sframework.Unschedulable:
		internalStatus.Code = Unschedulable
	case k8sframework.UnschedulableAndUnresolvable:
		internalStatus.Code = UnschedulableAndUnresolvable
	case k8sframework.Wait:
		internalStatus.Code = Wait
	case k8sframework.Skip:
		internalStatus.Code = Skip
	default:
		internalStatus.Code = Success
	}
	// in case that pod's scheduling message is not identifiable with message: 'all nodes are unavailable'
	if internalStatus.Code != Success {
		internalStatus.Reason = status.Message()
	}
	return internalStatus
}

// ValidateExFn is the func declaration used to validate the result.
type ValidateExFn func(interface{}) *ValidateResult

// VoteFn is the func declaration used to check object's complicated status.
type VoteFn func(interface{}) int

// JobEnqueuedFn is the func declaration used to call after job enqueued.
type JobEnqueuedFn func(interface{})

// PredicateFn is the func declaration used to predicate node for task.
type PredicateFn func(*TaskInfo, *NodeInfo) error

// PrePredicateFn is the func declaration used to pre-predicate node for task.
type PrePredicateFn func(*TaskInfo) error

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
type VictimTasksFn func([]*TaskInfo) []*TaskInfo

// AllocatableFn is the func declaration used to check whether the task can be allocated
type AllocatableFn func(*QueueInfo, *TaskInfo) bool
