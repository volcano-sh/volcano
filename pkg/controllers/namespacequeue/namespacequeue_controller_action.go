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

package namespacequeue

import (
	"encoding/json"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	controllermetrics "volcano.sh/volcano/pkg/controllers/metrics"
	commonutil "volcano.sh/volcano/pkg/util"
)

const namespaceQueueFinalizer = "scheduling.volcano.sh/namespacequeue-protection"

// reconcileNamespaceQueue publishes controller-owned status and protects
// deletion until the user has drained the NamespaceQueue.
func (c *namespaceQueueController) reconcileNamespaceQueue(namespaceQueue *schedulingv1beta1.NamespaceQueue) error {
	if namespaceQueue.DeletionTimestamp == nil {
		if err := c.ensureNamespaceQueueFinalizer(namespaceQueue); err != nil {
			return err
		}
	} else if !hasFinalizer(namespaceQueue, namespaceQueueFinalizer) {
		return nil
	}

	if err := c.updateNamespaceQueueStatus(namespaceQueue); err != nil {
		return err
	}
	if namespaceQueue.DeletionTimestamp == nil {
		return nil
	}

	ctx, cancel := c.apiContext()
	currentQueue, err := c.vcClient.SchedulingV1beta1().NamespaceQueues(namespaceQueue.Namespace).
		Get(ctx, namespaceQueue.Name, metav1.GetOptions{})
	cancel()
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get deleting NamespaceQueue %s/%s: %w", namespaceQueue.Namespace, namespaceQueue.Name, err)
	}
	childQueues, err := c.getDirectChildNamespaceQueues(namespaceQueueReference(currentQueue))
	if err != nil {
		return err
	}
	if len(childQueues) != 0 || !commonutil.IsNamespaceQueueDrained(currentQueue.Status) {
		return nil
	}

	return c.removeNamespaceQueueFinalizer(currentQueue)
}

func hasFinalizer(namespaceQueue *schedulingv1beta1.NamespaceQueue, finalizer string) bool {
	if namespaceQueue == nil {
		return false
	}
	for _, existing := range namespaceQueue.Finalizers {
		if existing == finalizer {
			return true
		}
	}
	return false
}

func (c *namespaceQueueController) ensureNamespaceQueueFinalizer(namespaceQueue *schedulingv1beta1.NamespaceQueue) error {
	if hasFinalizer(namespaceQueue, namespaceQueueFinalizer) {
		return nil
	}
	return c.patchNamespaceQueueFinalizers(namespaceQueue.Namespace, namespaceQueue.Name, func(finalizers []string) []string {
		for _, finalizer := range finalizers {
			if finalizer == namespaceQueueFinalizer {
				return finalizers
			}
		}
		return append(finalizers, namespaceQueueFinalizer)
	})
}

func (c *namespaceQueueController) removeNamespaceQueueFinalizer(namespaceQueue *schedulingv1beta1.NamespaceQueue) error {
	return c.patchNamespaceQueueFinalizers(namespaceQueue.Namespace, namespaceQueue.Name, func(finalizers []string) []string {
		updated := finalizers[:0]
		for _, finalizer := range finalizers {
			if finalizer != namespaceQueueFinalizer {
				updated = append(updated, finalizer)
			}
		}
		return updated
	})
}

func (c *namespaceQueueController) patchNamespaceQueueFinalizers(
	namespace, name string,
	mutate func([]string) []string,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		ctx, cancel := c.apiContext()
		defer cancel()
		currentQueue, err := c.vcClient.SchedulingV1beta1().NamespaceQueues(namespace).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		finalizers := mutate(append([]string(nil), currentQueue.Finalizers...))
		if equality.Semantic.DeepEqual(currentQueue.Finalizers, finalizers) {
			return nil
		}

		patch, err := json.Marshal(map[string]interface{}{
			"metadata": map[string]interface{}{
				"resourceVersion": currentQueue.ResourceVersion,
				"finalizers":      finalizers,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to marshal NamespaceQueue finalizer patch: %w", err)
		}
		_, err = c.vcClient.SchedulingV1beta1().NamespaceQueues(namespace).
			Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{})
		return err
	})
}

type namespaceQueueConditionResult struct {
	authorizedStatus  metav1.ConditionStatus
	authorizedReason  string
	authorizedMessage string
	readyStatus       metav1.ConditionStatus
	readyReason       string
	readyMessage      string
}

// evaluateParent derives authorization and readiness from the effective parent
// chain. Unknown parent observations remain explicit so the scheduler never
// treats stale informer state as scheduling permission.
func (c *namespaceQueueController) evaluateParent(
	namespaceQueue *schedulingv1beta1.NamespaceQueue,
) (namespaceQueueConditionResult, error) {
	result := namespaceQueueConditionResult{
		authorizedStatus: metav1.ConditionUnknown,
		readyStatus:      metav1.ConditionFalse,
	}

	parentRef, err := resolveParent(namespaceQueue)
	if err != nil {
		result.authorizedReason = commonutil.NamespaceQueueReasonInvalidParentReference
		result.authorizedMessage = err.Error()
		result.readyReason = result.authorizedReason
		result.readyMessage = result.authorizedMessage
		return result, nil
	}

	switch parentRef.Scope {
	case commonutil.ClusterQueueReferenceScope:
		parentQueue, err := c.queueLister.Get(parentRef.Name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				message := fmt.Sprintf("parent Queue %q was not found", parentRef.Name)
				result.authorizedReason = commonutil.NamespaceQueueReasonParentNotFound
				result.authorizedMessage = message
				result.readyReason = result.authorizedReason
				result.readyMessage = message
				return result, nil
			}
			return result, fmt.Errorf("failed to get parent Queue %q: %w", parentRef.Name, err)
		}

		if !isNamespaceAllowed(parentQueue, namespaceQueue.Namespace) {
			message := fmt.Sprintf(
				"namespace %q is not allowed to use Queue %q",
				namespaceQueue.Namespace,
				parentQueue.Name,
			)
			result.authorizedStatus = metav1.ConditionFalse
			result.authorizedReason = commonutil.NamespaceQueueReasonNamespaceNotAllowed
			result.authorizedMessage = message
			result.readyReason = result.authorizedReason
			result.readyMessage = message
			return result, nil
		}

		result.authorizedStatus = metav1.ConditionTrue
		result.authorizedReason = commonutil.NamespaceQueueReasonNamespaceAllowed
		result.authorizedMessage = "namespace is authorized to use the parent"
		if !isClusterQueueReady(parentQueue) {
			result.readyReason = commonutil.NamespaceQueueReasonParentNotReady
			result.readyMessage = fmt.Sprintf("parent Queue %q is not ready", parentQueue.Name)
			return result, nil
		}

	case commonutil.NamespaceQueueReferenceScope:
		parentQueue, err := c.namespaceQueueLister.
			NamespaceQueues(parentRef.Namespace).
			Get(parentRef.Name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				message := fmt.Sprintf(
					"parent NamespaceQueue %q/%q was not found",
					parentRef.Namespace,
					parentRef.Name,
				)
				result.authorizedReason = commonutil.NamespaceQueueReasonParentNotFound
				result.authorizedMessage = message
				result.readyReason = result.authorizedReason
				result.readyMessage = message
				return result, nil
			}
			return result, fmt.Errorf(
				"failed to get parent NamespaceQueue %q/%q: %w",
				parentRef.Namespace,
				parentRef.Name,
				err,
			)
		}

		depth, err := commonutil.NamespaceQueueDepth(
			namespaceQueue,
			func(namespace, name string) (*schedulingv1beta1.NamespaceQueue, error) {
				return c.namespaceQueueLister.NamespaceQueues(namespace).Get(name)
			},
		)
		if err != nil {
			if errors.Is(err, commonutil.ErrNamespaceQueueHierarchyCycle) {
				result.authorizedReason = commonutil.NamespaceQueueReasonHierarchyCycle
				result.authorizedMessage = err.Error()
				result.readyReason = result.authorizedReason
				result.readyMessage = result.authorizedMessage
				return result, nil
			}
			return result, err
		}
		if depth > c.maxNamespaceQueueDepth {
			result.authorizedReason = commonutil.NamespaceQueueReasonHierarchyDepthExceeded
			result.authorizedMessage = fmt.Sprintf(
				"NamespaceQueue hierarchy depth %d exceeds maximum depth %d",
				depth,
				c.maxNamespaceQueueDepth,
			)
			result.readyReason = result.authorizedReason
			result.readyMessage = result.authorizedMessage
			return result, nil
		}

		parentAuthorized := apiMeta.FindStatusCondition(
			parentQueue.Status.Conditions,
			commonutil.NamespaceQueueAuthorizedCondition,
		)
		if parentAuthorized == nil || parentAuthorized.ObservedGeneration != parentQueue.Generation {
			result.authorizedStatus = metav1.ConditionUnknown
			result.authorizedReason = commonutil.NamespaceQueueReasonParentAuthorizationUnknown
			result.authorizedMessage = "parent NamespaceQueue authorization has not been observed"
			result.readyReason = result.authorizedReason
			result.readyMessage = result.authorizedMessage
			return result, nil
		}
		result.authorizedStatus = parentAuthorized.Status
		result.authorizedReason = parentAuthorized.Reason
		result.authorizedMessage = parentAuthorized.Message
		if parentAuthorized.Status != metav1.ConditionTrue {
			result.readyReason = commonutil.NamespaceQueueReasonParentNotAuthorized
			result.readyMessage = fmt.Sprintf(
				"parent NamespaceQueue %q/%q is not authorized",
				parentRef.Namespace,
				parentRef.Name,
			)
			return result, nil
		}
		if !isNamespaceQueueReady(parentQueue) {
			result.readyReason = commonutil.NamespaceQueueReasonParentNotReady
			result.readyMessage = fmt.Sprintf(
				"parent NamespaceQueue %q/%q is not ready",
				parentRef.Namespace,
				parentRef.Name,
			)
			return result, nil
		}

	default:
		return result, fmt.Errorf("unknown parent scope %q", parentRef.Scope)
	}

	if reason, message, err := c.validateNamespaceQueueConstraints(namespaceQueue, parentRef); err != nil {
		return result, err
	} else if reason != "" {
		result.readyReason = reason
		result.readyMessage = message
		return result, nil
	}

	result.readyStatus = metav1.ConditionTrue
	result.readyReason = commonutil.NamespaceQueueReasonReady
	result.readyMessage = "NamespaceQueue is ready for scheduling"
	return result, nil
}

func isClusterQueueReady(parentQueue *schedulingv1beta1.Queue) bool {
	return parentQueue != nil &&
		(parentQueue.Status.State == "" || parentQueue.Status.State == schedulingv1beta1.QueueStateOpen)
}

// calculatePodGroupCounters counts active and completed PodGroups from the
// namespace queue index. Completed groups remain visible for observability but
// do not block queue draining.
func (c *namespaceQueueController) calculatePodGroupCounters(
	namespaceQueue *schedulingv1beta1.NamespaceQueue,
) (schedulingv1beta1.NamespaceQueueStatus, error) {
	status := schedulingv1beta1.NamespaceQueueStatus{}
	podGroups, err := c.podGroupInformer.Informer().GetIndexer().ByIndex(
		namespaceQueuePodGroupIndex,
		namespaceQueue.Namespace+"/"+namespaceQueue.Name,
	)
	if err != nil {
		return status, fmt.Errorf(
			"failed to list PodGroups for NamespaceQueue %s/%s: %w",
			namespaceQueue.Namespace,
			namespaceQueue.Name,
			err,
		)
	}

	for _, obj := range podGroups {
		podGroup, ok := obj.(*schedulingv1beta1.PodGroup)
		if !ok {
			return status, fmt.Errorf("indexed object is not a PodGroup: %T", obj)
		}

		switch podGroup.Status.Phase {
		case schedulingv1beta1.PodGroupPending:
			status.Pending++
		case schedulingv1beta1.PodGroupRunning:
			status.Running++
		case schedulingv1beta1.PodGroupInqueue:
			status.Inqueue++
		case schedulingv1beta1.PodGroupCompleted:
			status.Completed++
		case schedulingv1beta1.PodGroupUnknown:
			status.Unknown++
		default:
			// Treat an unset or unrecognized phase as active unknown work so
			// deletion safety does not mistake the queue for drained.
			status.Unknown++
		}
	}

	return status, nil
}

func isNamespaceQueueReady(namespaceQueue *schedulingv1beta1.NamespaceQueue) bool {
	if namespaceQueue == nil {
		return false
	}

	return commonutil.IsNamespaceQueueSchedulable(
		namespaceQueue.Generation,
		string(namespaceQueue.Status.State),
		namespaceQueue.Status.Conditions,
	)
}

func isNamespaceAllowed(
	parentQueue *schedulingv1beta1.Queue,
	namespace string,
) bool {
	for _, allowedNamespace := range parentQueue.Spec.AllowedNamespaces {
		if allowedNamespace == "*" || allowedNamespace == namespace {
			return true
		}
	}

	return false
}

func setCondition(
	status *schedulingv1beta1.NamespaceQueueStatus,
	generation int64,
	conditionType string,
	conditionStatus metav1.ConditionStatus,
	reason string,
	message string,
) {
	apiMeta.SetStatusCondition(
		&status.Conditions,
		metav1.Condition{
			Type:               conditionType,
			Status:             conditionStatus,
			ObservedGeneration: generation,
			Reason:             reason,
			Message:            message,
		},
	)
}

// updateNamespaceQueueStatus updates only controller-owned status fields and
// preserves scheduler-owned Allocated and Reservation across conflict retries.
func (c *namespaceQueueController) updateNamespaceQueueStatus(
	namespaceQueue *schedulingv1beta1.NamespaceQueue,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		ctx, cancel := c.apiContext()
		defer cancel()
		currentQueue, err := c.vcClient.
			SchedulingV1beta1().
			NamespaceQueues(namespaceQueue.Namespace).
			Get(ctx, namespaceQueue.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get NamespaceQueue status: %w", err)
		}
		if currentQueue.Generation != namespaceQueue.Generation {
			// Spec advanced past the informer snapshot — don't mutate status
			// for a stale generation, but refresh gauges from the live API
			// object so lifecycle transitions are visible immediately.
			controllermetrics.UpdateNamespaceQueueMetrics(currentQueue, &currentQueue.Status)
			return nil
		}

		counters, err := c.calculatePodGroupCounters(currentQueue)
		if err != nil {
			return err
		}

		desiredStatus := *currentQueue.Status.DeepCopy()
		desiredStatus.Unknown = counters.Unknown
		desiredStatus.Pending = counters.Pending
		desiredStatus.Running = counters.Running
		desiredStatus.Inqueue = counters.Inqueue
		desiredStatus.Completed = counters.Completed

		parentConditions, err := c.evaluateParent(currentQueue)
		if err != nil {
			return err
		}

		targetState := schedulingv1beta1.QueueStateOpen
		if currentQueue.DeletionTimestamp != nil {
			targetState = schedulingv1beta1.QueueStateClosed
		}
		runtimeDrained := commonutil.IsNamespaceQueueRuntimeDrained(currentQueue.Status)
		desiredStatus.State = targetState
		if targetState == schedulingv1beta1.QueueStateClosed &&
			(!commonutil.IsNamespaceQueueWorkloadDrained(desiredStatus) || !runtimeDrained) {
			desiredStatus.State = schedulingv1beta1.QueueStateClosing
		}
		setNamespaceQueueConditions(&desiredStatus, currentQueue.Generation, parentConditions)

		if !namespaceQueueControllerStatusEqual(currentQueue.Status, desiredStatus) {
			updatedQueue := currentQueue.DeepCopy()
			updatedQueue.Status.State = desiredStatus.State
			updatedQueue.Status.Unknown = desiredStatus.Unknown
			updatedQueue.Status.Pending = desiredStatus.Pending
			updatedQueue.Status.Running = desiredStatus.Running
			updatedQueue.Status.Inqueue = desiredStatus.Inqueue
			updatedQueue.Status.Completed = desiredStatus.Completed
			updatedQueue.Status.Conditions = append([]metav1.Condition(nil), desiredStatus.Conditions...)

			updatedQueue, err = c.vcClient.
				SchedulingV1beta1().
				NamespaceQueues(namespaceQueue.Namespace).
				UpdateStatus(ctx, updatedQueue, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update NamespaceQueue status: %w", err)
			}
			if currentQueue.Status.State != updatedQueue.Status.State {
				c.recorder.Eventf(
					updatedQueue,
					corev1.EventTypeNormal,
					"LifecycleStateChanged",
					"NamespaceQueue lifecycle state changed from %s to %s",
					currentQueue.Status.State,
					updatedQueue.Status.State,
				)
			}
			c.recordConditionEvents(currentQueue, updatedQueue)
		}

		// Metrics are refreshed on every reconcile (including the status-equal
		// event path) so stable queues keep their gauges populated after a
		// controller restart; on the update path they are written only after a
		// successful UpdateStatus so an unpersisted status is never reported.
		controllermetrics.UpdateNamespaceQueueMetrics(currentQueue, &desiredStatus)

		return nil
	})
}

func (c *namespaceQueueController) recordConditionEvents(
	oldNamespaceQueue, newNamespaceQueue *schedulingv1beta1.NamespaceQueue,
) {
	for _, conditionType := range []string{
		commonutil.NamespaceQueueAuthorizedCondition,
		commonutil.NamespaceQueueReadyCondition,
	} {
		oldCondition := apiMeta.FindStatusCondition(oldNamespaceQueue.Status.Conditions, conditionType)
		newCondition := apiMeta.FindStatusCondition(newNamespaceQueue.Status.Conditions, conditionType)
		if !conditionEventChanged(oldCondition, newCondition) || newCondition == nil {
			continue
		}

		eventType := corev1.EventTypeNormal
		if newCondition.Status == metav1.ConditionFalse {
			eventType = corev1.EventTypeWarning
		}
		c.recorder.Eventf(
			newNamespaceQueue,
			eventType,
			namespaceQueueConditionReason(newCondition),
			"NamespaceQueue %s condition changed to %s: %s",
			conditionType,
			newCondition.Status,
			newCondition.Message,
		)
	}
}

func conditionEventChanged(oldCondition, newCondition *metav1.Condition) bool {
	if oldCondition == nil {
		return newCondition != nil
	}
	if newCondition == nil {
		return true
	}

	return oldCondition.Status != newCondition.Status ||
		oldCondition.Reason != newCondition.Reason ||
		oldCondition.Message != newCondition.Message
}

func namespaceQueueConditionReason(condition *metav1.Condition) string {
	if condition == nil || condition.Reason == "" {
		return commonutil.NamespaceQueueReasonStatusChanged
	}
	return condition.Reason
}

func setNamespaceQueueConditions(
	status *schedulingv1beta1.NamespaceQueueStatus,
	generation int64,
	result namespaceQueueConditionResult,
) {
	setCondition(
		status,
		generation,
		commonutil.NamespaceQueueAuthorizedCondition,
		result.authorizedStatus,
		result.authorizedReason,
		result.authorizedMessage,
	)

	readyStatus := result.readyStatus
	readyReason := result.readyReason
	readyMessage := result.readyMessage
	if readyStatus == metav1.ConditionTrue {
		switch status.State {
		case schedulingv1beta1.QueueStateOpen:
		// The parent evaluation already established readiness.
		case schedulingv1beta1.QueueStateClosing:
			readyStatus = metav1.ConditionFalse
			readyReason = commonutil.NamespaceQueueReasonQueueClosing
			readyMessage = "NamespaceQueue is closing and waiting to drain"
		case schedulingv1beta1.QueueStateClosed:
			readyStatus = metav1.ConditionFalse
			readyReason = commonutil.NamespaceQueueReasonQueueClosed
			readyMessage = "NamespaceQueue is closed"
		default:
			readyStatus = metav1.ConditionFalse
			readyReason = commonutil.NamespaceQueueReasonStatusChanged
			readyMessage = "NamespaceQueue lifecycle state is not open"
		}
	}

	setCondition(
		status,
		generation,
		commonutil.NamespaceQueueReadyCondition,
		readyStatus,
		readyReason,
		readyMessage,
	)
}

// namespaceQueueControllerStatusEqual compares only status fields owned by the
// NamespaceQueue controller. Allocated and Reservation are owned by scheduler.
func namespaceQueueControllerStatusEqual(
	currentStatus schedulingv1beta1.NamespaceQueueStatus,
	desiredStatus schedulingv1beta1.NamespaceQueueStatus,
) bool {
	return currentStatus.State == desiredStatus.State &&
		currentStatus.Unknown == desiredStatus.Unknown &&
		currentStatus.Pending == desiredStatus.Pending &&
		currentStatus.Running == desiredStatus.Running &&
		currentStatus.Inqueue == desiredStatus.Inqueue &&
		currentStatus.Completed == desiredStatus.Completed &&
		equality.Semantic.DeepEqual(currentStatus.Conditions, desiredStatus.Conditions)
}
