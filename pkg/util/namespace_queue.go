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

	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

const (
	// DefaultMaxNamespaceQueueDepth is the default number of NamespaceQueue
	// levels allowed below a cluster Queue.
	DefaultMaxNamespaceQueueDepth = 5

	// NamespaceQueueAuthorizedCondition identifies the parent authorization condition.
	NamespaceQueueAuthorizedCondition = "Authorized"
	// NamespaceQueueReadyCondition identifies the scheduling readiness condition.
	NamespaceQueueReadyCondition = "Ready"

	// NamespaceQueueReasonInvalidParentReference identifies an invalid parent reference.
	NamespaceQueueReasonInvalidParentReference = "InvalidParentReference"
	// NamespaceQueueReasonParentNotFound identifies a missing parent resource.
	NamespaceQueueReasonParentNotFound = "ParentNotFound"
	// NamespaceQueueReasonNamespaceNotAllowed identifies a denied namespace attachment.
	NamespaceQueueReasonNamespaceNotAllowed = "NamespaceNotAllowed"
	// NamespaceQueueReasonNamespaceAllowed identifies an authorized namespace attachment.
	NamespaceQueueReasonNamespaceAllowed = "NamespaceAllowed"
	// NamespaceQueueReasonParentNotReady identifies a parent that cannot schedule.
	NamespaceQueueReasonParentNotReady = "ParentNotReady"
	// NamespaceQueueReasonParentAuthorizationUnknown identifies an unobserved parent authorization.
	NamespaceQueueReasonParentAuthorizationUnknown = "ParentAuthorizationUnknown"
	// NamespaceQueueReasonParentNotAuthorized identifies a denied NamespaceQueue parent.
	NamespaceQueueReasonParentNotAuthorized = "ParentNotAuthorized"
	// NamespaceQueueReasonHierarchyCycle identifies a cyclic parent relationship.
	NamespaceQueueReasonHierarchyCycle = "HierarchyCycle"
	// NamespaceQueueReasonHierarchyDepthExceeded identifies a hierarchy beyond the configured limit.
	NamespaceQueueReasonHierarchyDepthExceeded = "HierarchyDepthExceeded"
	// NamespaceQueueReasonParentConstraintViolation identifies a resource constraint violation.
	NamespaceQueueReasonParentConstraintViolation = "ParentConstraintViolation"
	// NamespaceQueueReasonReady identifies a queue ready for scheduling.
	NamespaceQueueReasonReady = "Ready"
	// NamespaceQueueReasonQueueClosing identifies a queue draining before closure.
	NamespaceQueueReasonQueueClosing = "QueueClosing"
	// NamespaceQueueReasonQueueClosed identifies a closed queue.
	NamespaceQueueReasonQueueClosed = "QueueClosed"
	// NamespaceQueueReasonParentChangeRequiresDrain identifies a blocked parent change.
	NamespaceQueueReasonParentChangeRequiresDrain = "ParentChangeRequiresDrain"
	// NamespaceQueueReasonStatusChanged identifies a generic status transition.
	NamespaceQueueReasonStatusChanged = "StatusChanged"
)

// ErrNamespaceQueueHierarchyCycle indicates that NamespaceQueue parent
// references form a cycle.
var ErrNamespaceQueueHierarchyCycle = errors.New("NamespaceQueue hierarchy contains a cycle")

// NamespaceQueueParentGetter retrieves a NamespaceQueue parent by namespace and name.
type NamespaceQueueParentGetter func(namespace, name string) (*schedulingv1beta1.NamespaceQueue, error)

// NamespaceQueueDepth returns the number of NamespaceQueue levels below the
// resolved cluster Queue. Cluster Queue hierarchy levels are not included.
func NamespaceQueueDepth(
	namespaceQueue *schedulingv1beta1.NamespaceQueue,
	getParentNamespaceQueue NamespaceQueueParentGetter,
) (int, error) {
	if namespaceQueue == nil {
		return 0, fmt.Errorf("NamespaceQueue is nil")
	}
	if getParentNamespaceQueue == nil {
		return 0, fmt.Errorf("NamespaceQueue parent getter is nil")
	}

	depth := 1
	currentQueue := namespaceQueue
	visited := map[types.NamespacedName]struct{}{
		{Namespace: namespaceQueue.Namespace, Name: namespaceQueue.Name}: {},
	}
	for {
		parentRef, err := ResolveNamespaceQueueParentReference(currentQueue.Namespace, currentQueue.Spec.Parent)
		if err != nil {
			return 0, err
		}
		if parentRef.Scope == ClusterQueueReferenceScope {
			return depth, nil
		}

		parentKey := types.NamespacedName{Namespace: parentRef.Namespace, Name: parentRef.Name}
		if _, found := visited[parentKey]; found {
			return 0, fmt.Errorf("%w at %q", ErrNamespaceQueueHierarchyCycle, parentKey.String())
		}
		visited[parentKey] = struct{}{}

		currentQueue, err = getParentNamespaceQueue(parentRef.Namespace, parentRef.Name)
		if err != nil {
			return 0, fmt.Errorf("get parent NamespaceQueue %q: %w", parentKey.String(), err)
		}
		if currentQueue == nil {
			return 0, fmt.Errorf("parent NamespaceQueue %q was not found", parentKey.String())
		}
		depth++
	}
}

// IsNamespaceQueueSchedulable reports whether the observed NamespaceQueue
// state is authoritative for generation and both controller conditions allow
// scheduling. State values are strings so the predicate can be shared by the
// versioned admission type and the scheduler's internal type.
func IsNamespaceQueueSchedulable(
	generation int64,
	observedState string,
	conditions []metav1.Condition,
) bool {
	if observedState != string(schedulingv1beta1.QueueStateOpen) {
		return false
	}

	authorized := apiMeta.FindStatusCondition(conditions, NamespaceQueueAuthorizedCondition)
	if authorized == nil ||
		authorized.ObservedGeneration != generation ||
		authorized.Status != metav1.ConditionTrue {
		return false
	}

	ready := apiMeta.FindStatusCondition(conditions, NamespaceQueueReadyCondition)
	return ready != nil &&
		ready.ObservedGeneration == generation &&
		ready.Status == metav1.ConditionTrue
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
// still own workloads or scheduler runtime resources. Objects created before
// the current parent-reference grammar was enforced may be repaired by
// updating them to a valid parent reference.
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
