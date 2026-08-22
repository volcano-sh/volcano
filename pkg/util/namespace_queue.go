/*
Copyright 2026 The Volcano Authors.

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

package util

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

const (
	// DefaultMaxNamespaceQueueDepth is the default number of NamespaceQueue
	// levels allowed below a cluster Queue.
	DefaultMaxNamespaceQueueDepth = 5

	NamespaceQueueAuthorizedCondition = "Authorized"
	NamespaceQueueReadyCondition      = "Ready"

	NamespaceQueueReasonInvalidParentReference     = "InvalidParentReference"
	NamespaceQueueReasonParentNotFound             = "ParentNotFound"
	NamespaceQueueReasonNamespaceNotAllowed        = "NamespaceNotAllowed"
	NamespaceQueueReasonNamespaceAllowed           = "NamespaceAllowed"
	NamespaceQueueReasonParentNotReady             = "ParentNotReady"
	NamespaceQueueReasonParentAuthorizationUnknown = "ParentAuthorizationUnknown"
	NamespaceQueueReasonParentNotAuthorized        = "ParentNotAuthorized"
	NamespaceQueueReasonHierarchyCycle             = "HierarchyCycle"
	NamespaceQueueReasonHierarchyDepthExceeded     = "HierarchyDepthExceeded"
	NamespaceQueueReasonParentConstraintViolation  = "ParentConstraintViolation"
	NamespaceQueueReasonDuplicateClusterAttachment = "DuplicateClusterAttachment"
	NamespaceQueueReasonReady                      = "Ready"
	NamespaceQueueReasonQueueClosing               = "QueueClosing"
	NamespaceQueueReasonQueueClosed                = "QueueClosed"
	NamespaceQueueReasonInvalidDesiredState        = "InvalidDesiredState"
	NamespaceQueueReasonParentChangeRequiresDrain  = "ParentChangeRequiresDrain"
	NamespaceQueueReasonStatusChanged              = "StatusChanged"
)

// ErrNamespaceQueueHierarchyCycle indicates that NamespaceQueue parent
// references form a cycle.
var ErrNamespaceQueueHierarchyCycle = errors.New("NamespaceQueue hierarchy contains a cycle")

// NamespaceQueueParentGetter retrieves a NamespaceQueue parent by namespace and name.
type NamespaceQueueParentGetter func(namespace, name string) (*schedulingv1beta1.NamespaceQueue, error)

// NamespaceQueueDepth returns the number of NamespaceQueue levels below the
// resolved cluster Queue. Cluster Queue hierarchy levels are not included.
func NamespaceQueueDepth(
	queue *schedulingv1beta1.NamespaceQueue,
	getParent NamespaceQueueParentGetter,
) (int, error) {
	if queue == nil {
		return 0, fmt.Errorf("NamespaceQueue is nil")
	}
	if getParent == nil {
		return 0, fmt.Errorf("NamespaceQueue parent getter is nil")
	}

	depth := 1
	current := queue
	visited := map[types.NamespacedName]struct{}{
		{Namespace: queue.Namespace, Name: queue.Name}: {},
	}
	for {
		parent, err := ResolveNamespaceQueueParentReference(current.Namespace, current.Spec.Parent)
		if err != nil {
			return 0, err
		}
		if parent.Scope == ClusterQueueReferenceScope {
			return depth, nil
		}

		key := types.NamespacedName{Namespace: parent.Namespace, Name: parent.Name}
		if _, found := visited[key]; found {
			return 0, fmt.Errorf("%w at %q", ErrNamespaceQueueHierarchyCycle, key.String())
		}
		visited[key] = struct{}{}

		current, err = getParent(parent.Namespace, parent.Name)
		if err != nil {
			return 0, fmt.Errorf("get parent NamespaceQueue %q: %w", key.String(), err)
		}
		if current == nil {
			return 0, fmt.Errorf("parent NamespaceQueue %q was not found", key.String())
		}
		depth++
	}
}

// EffectiveNamespaceQueueState returns the desired lifecycle state. Empty is
// treated as Open for objects created before spec.state was introduced.
func EffectiveNamespaceQueueState(state schedulingv1beta1.QueueState) schedulingv1beta1.QueueState {
	if state == "" {
		return schedulingv1beta1.QueueStateOpen
	}
	return state
}

// ResolveNamespaceQueueLifecycleState resolves the observed lifecycle from
// the desired state and the current drain observations.
func ResolveNamespaceQueueLifecycleState(
	desired schedulingv1beta1.QueueState,
	workloadDrained bool,
	runtimeDrained bool,
) schedulingv1beta1.QueueState {
	switch desired {
	case schedulingv1beta1.QueueStateOpen:
		return schedulingv1beta1.QueueStateOpen
	case schedulingv1beta1.QueueStateClosed:
		if workloadDrained && runtimeDrained {
			return schedulingv1beta1.QueueStateClosed
		}
		return schedulingv1beta1.QueueStateClosing
	default:
		return schedulingv1beta1.QueueStateUnknown
	}
}

// IsNamespaceQueueWorkloadDrained reports whether no active PodGroup remains.
func IsNamespaceQueueWorkloadDrained(status schedulingv1beta1.NamespaceQueueStatus) bool {
	return status.Unknown == 0 &&
		status.Pending == 0 &&
		status.Running == 0 &&
		status.Inqueue == 0
}

// IsNamespaceQueueRuntimeDrained reports whether scheduler-owned runtime
// resources have been released.
func IsNamespaceQueueRuntimeDrained(status schedulingv1beta1.NamespaceQueueStatus) bool {
	if len(status.Reservation.Nodes) != 0 {
		return false
	}

	for _, quantity := range status.Allocated {
		if !quantity.IsZero() {
			return false
		}
	}
	for _, quantity := range status.Reservation.Resource {
		if !quantity.IsZero() {
			return false
		}
	}

	return true
}

// IsNamespaceQueueDrained reports whether no active workload or scheduler-owned
// runtime resource remains. Completed workloads do not prevent draining.
func IsNamespaceQueueDrained(status schedulingv1beta1.NamespaceQueueStatus) bool {
	return IsNamespaceQueueWorkloadDrained(status) &&
		IsNamespaceQueueRuntimeDrained(status)
}

// IsNamespaceQueueClosedAndDrained reports whether a NamespaceQueue can be
// safely detached from its parent or deleted.
func IsNamespaceQueueClosedAndDrained(namespaceQueue *schedulingv1beta1.NamespaceQueue) bool {
	return namespaceQueue != nil &&
		namespaceQueue.Status.State == schedulingv1beta1.QueueStateClosed &&
		IsNamespaceQueueDrained(namespaceQueue.Status)
}

// ValidateNamespaceQueueParentChange rejects parent changes while a queue can
// still own workloads or scheduler runtime resources. The effective parent is
// compared so equivalent references such as "" and "cluster/default" do not
// require a drain.
func ValidateNamespaceQueueParentChange(
	oldQueue, newQueue *schedulingv1beta1.NamespaceQueue,
) error {
	if oldQueue == nil || newQueue == nil {
		return fmt.Errorf("old and new NamespaceQueue must not be nil")
	}

	oldParent, err := ResolveNamespaceQueueParentReference(oldQueue.Namespace, oldQueue.Spec.Parent)
	if err != nil {
		// Allow repairing objects created before the current reference grammar
		// was enforced. The new object is still validated below by admission.
		return nil
	}
	newParent, err := ResolveNamespaceQueueParentReference(newQueue.Namespace, newQueue.Spec.Parent)
	if err != nil {
		return fmt.Errorf("resolve new NamespaceQueue parent: %w", err)
	}
	if oldParent == newParent || IsNamespaceQueueClosedAndDrained(oldQueue) {
		return nil
	}

	return fmt.Errorf(
		"%s: NamespaceQueue parent cannot be changed until the queue is Closed and drained; current state is %q",
		NamespaceQueueReasonParentChangeRequiresDrain,
		oldQueue.Status.State,
	)
}
