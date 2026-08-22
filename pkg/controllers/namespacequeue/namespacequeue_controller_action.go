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
	commonutil "volcano.sh/volcano/pkg/util"
)

const namespaceQueueFinalizer = "scheduling.volcano.sh/namespacequeue-protection"

func (c *namespaceQueueController) reconcileNamespaceQueue(nq *schedulingv1beta1.NamespaceQueue) error {
	if c.cleanupOnly && nq.DeletionTimestamp == nil {
		return nil
	}
	if nq.DeletionTimestamp == nil {
		if err := c.ensureNamespaceQueueFinalizer(nq); err != nil {
			return err
		}
	} else if !hasFinalizer(nq, namespaceQueueFinalizer) {
		return nil
	}

	if err := c.updateNamespaceQueueStatus(nq); err != nil {
		return err
	}
	if nq.DeletionTimestamp == nil {
		return nil
	}

	ctx, cancel := c.apiContext()
	current, err := c.vcClient.SchedulingV1beta1().NamespaceQueues(nq.Namespace).
		Get(ctx, nq.Name, metav1.GetOptions{})
	cancel()
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to get deleting NamespaceQueue %s/%s: %w", nq.Namespace, nq.Name, err)
	}
	children, err := c.getDirectChildNamespaceQueues(namespaceQueueReference(current))
	if err != nil {
		return err
	}
	if len(children) != 0 || !commonutil.IsNamespaceQueueClosedAndDrained(current) {
		return nil
	}

	return c.removeNamespaceQueueFinalizer(current)
}

func hasFinalizer(nq *schedulingv1beta1.NamespaceQueue, finalizer string) bool {
	if nq == nil {
		return false
	}
	for _, existing := range nq.Finalizers {
		if existing == finalizer {
			return true
		}
	}
	return false
}

func (c *namespaceQueueController) ensureNamespaceQueueFinalizer(nq *schedulingv1beta1.NamespaceQueue) error {
	if hasFinalizer(nq, namespaceQueueFinalizer) {
		return nil
	}
	return c.patchNamespaceQueueFinalizers(nq.Namespace, nq.Name, func(finalizers []string) []string {
		for _, finalizer := range finalizers {
			if finalizer == namespaceQueueFinalizer {
				return finalizers
			}
		}
		return append(finalizers, namespaceQueueFinalizer)
	})
}

func (c *namespaceQueueController) removeNamespaceQueueFinalizer(nq *schedulingv1beta1.NamespaceQueue) error {
	return c.patchNamespaceQueueFinalizers(nq.Namespace, nq.Name, func(finalizers []string) []string {
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
		current, err := c.vcClient.SchedulingV1beta1().NamespaceQueues(namespace).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		finalizers := mutate(append([]string(nil), current.Finalizers...))
		if equality.Semantic.DeepEqual(current.Finalizers, finalizers) {
			return nil
		}

		patch, err := json.Marshal(map[string]interface{}{
			"metadata": map[string]interface{}{
				"resourceVersion": current.ResourceVersion,
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

func (c *namespaceQueueController) evaluateParent(
	nq *schedulingv1beta1.NamespaceQueue,
) (namespaceQueueConditionResult, error) {
	result := namespaceQueueConditionResult{
		authorizedStatus: metav1.ConditionUnknown,
		readyStatus:      metav1.ConditionFalse,
	}

	parent, err := resolveParent(nq)
	if err != nil {
		result.authorizedReason = commonutil.NamespaceQueueReasonInvalidParentReference
		result.authorizedMessage = err.Error()
		result.readyReason = result.authorizedReason
		result.readyMessage = result.authorizedMessage
		return result, nil
	}

	switch parent.Scope {
	case commonutil.ClusterQueueReferenceScope:
		queue, err := c.queueLister.Get(parent.Name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				message := fmt.Sprintf("parent Queue %q was not found", parent.Name)
				result.authorizedReason = commonutil.NamespaceQueueReasonParentNotFound
				result.authorizedMessage = message
				result.readyReason = result.authorizedReason
				result.readyMessage = message
				return result, nil
			}
			return result, fmt.Errorf("failed to get parent Queue %q: %w", parent.Name, err)
		}

		if !isNamespaceAllowed(queue, nq.Namespace) {
			message := fmt.Sprintf(
				"namespace %q is not allowed to use Queue %q",
				nq.Namespace,
				queue.Name,
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
		if !isClusterQueueReady(queue) {
			result.readyReason = commonutil.NamespaceQueueReasonParentNotReady
			result.readyMessage = fmt.Sprintf("parent Queue %q is not ready", queue.Name)
			return result, nil
		}

	case commonutil.NamespaceQueueReferenceScope:
		parentQueue, err := c.namespaceQueueLister.
			NamespaceQueues(parent.Namespace).
			Get(parent.Name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				message := fmt.Sprintf(
					"parent NamespaceQueue %q/%q was not found",
					parent.Namespace,
					parent.Name,
				)
				result.authorizedReason = commonutil.NamespaceQueueReasonParentNotFound
				result.authorizedMessage = message
				result.readyReason = result.authorizedReason
				result.readyMessage = message
				return result, nil
			}
			return result, fmt.Errorf(
				"failed to get parent NamespaceQueue %q/%q: %w",
				parent.Namespace,
				parent.Name,
				err,
			)
		}

		depth, err := commonutil.NamespaceQueueDepth(
			nq,
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
				parent.Namespace,
				parent.Name,
			)
			return result, nil
		}
		if !isNamespaceQueueReady(parentQueue) {
			result.readyReason = commonutil.NamespaceQueueReasonParentNotReady
			result.readyMessage = fmt.Sprintf(
				"parent NamespaceQueue %q/%q is not ready",
				parent.Namespace,
				parent.Name,
			)
			return result, nil
		}

	default:
		return result, fmt.Errorf("unknown parent scope %q", parent.Scope)
	}

	if reason, message, err := c.validateNamespaceQueueConstraints(nq, parent); err != nil {
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

func isClusterQueueReady(queue *schedulingv1beta1.Queue) bool {
	return queue != nil &&
		(queue.Status.State == "" || queue.Status.State == schedulingv1beta1.QueueStateOpen)
}

func (c *namespaceQueueController) calculatePodGroupCounters(
	nq *schedulingv1beta1.NamespaceQueue,
) (schedulingv1beta1.NamespaceQueueStatus, error) {
	status := schedulingv1beta1.NamespaceQueueStatus{}
	podGroups, err := c.podGroupInformer.Informer().GetIndexer().ByIndex(
		namespaceQueuePodGroupIndex,
		nq.Namespace+"/"+nq.Name,
	)
	if err != nil {
		return status, fmt.Errorf(
			"failed to list PodGroups for NamespaceQueue %s/%s: %w",
			nq.Namespace,
			nq.Name,
			err,
		)
	}

	for _, obj := range podGroups {
		pg, ok := obj.(*schedulingv1beta1.PodGroup)
		if !ok {
			return status, fmt.Errorf("indexed object is not a PodGroup: %T", obj)
		}

		switch pg.Status.Phase {
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

func isNamespaceQueueReady(nq *schedulingv1beta1.NamespaceQueue) bool {
	if nq == nil ||
		commonutil.EffectiveNamespaceQueueState(nq.Spec.State) != schedulingv1beta1.QueueStateOpen ||
		nq.Status.State != schedulingv1beta1.QueueStateOpen {
		return false
	}

	condition := apiMeta.FindStatusCondition(
		nq.Status.Conditions,
		commonutil.NamespaceQueueReadyCondition,
	)
	return condition != nil &&
		condition.Status == metav1.ConditionTrue &&
		condition.ObservedGeneration == nq.Generation
}

func isNamespaceAllowed(
	queue *schedulingv1beta1.Queue,
	namespace string,
) bool {
	for _, allowed := range queue.Spec.AllowedNamespaces {
		if allowed == "*" || allowed == namespace {
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

func (c *namespaceQueueController) updateNamespaceQueueStatus(
	nq *schedulingv1beta1.NamespaceQueue,
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		ctx, cancel := c.apiContext()
		defer cancel()
		current, err := c.vcClient.
			SchedulingV1beta1().
			NamespaceQueues(nq.Namespace).
			Get(ctx, nq.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get NamespaceQueue status: %w", err)
		}
		if current.Generation != nq.Generation {
			return nil
		}

		counters, err := c.calculatePodGroupCounters(current)
		if err != nil {
			return err
		}

		status := *current.Status.DeepCopy()
		status.Unknown = counters.Unknown
		status.Pending = counters.Pending
		status.Running = counters.Running
		status.Inqueue = counters.Inqueue
		status.Completed = counters.Completed

		parentResult, err := c.evaluateParent(current)
		if err != nil {
			return err
		}

		desiredState := commonutil.EffectiveNamespaceQueueState(current.Spec.State)
		if current.DeletionTimestamp != nil {
			desiredState = schedulingv1beta1.QueueStateClosed
		}
		status.State = commonutil.ResolveNamespaceQueueLifecycleState(
			desiredState,
			commonutil.IsNamespaceQueueWorkloadDrained(status),
			commonutil.IsNamespaceQueueRuntimeDrained(current.Status),
		)
		setNamespaceQueueConditions(&status, current.Generation, parentResult)

		if namespaceQueueControllerStatusEqual(current.Status, status) {
			return nil
		}

		updated := current.DeepCopy()
		updated.Status.State = status.State
		updated.Status.Unknown = status.Unknown
		updated.Status.Pending = status.Pending
		updated.Status.Running = status.Running
		updated.Status.Inqueue = status.Inqueue
		updated.Status.Completed = status.Completed
		updated.Status.Conditions = append([]metav1.Condition(nil), status.Conditions...)

		updated, err = c.vcClient.
			SchedulingV1beta1().
			NamespaceQueues(nq.Namespace).
			UpdateStatus(ctx, updated, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update NamespaceQueue status: %w", err)
		}
		if current.Status.State != updated.Status.State {
			c.recorder.Eventf(
				updated,
				corev1.EventTypeNormal,
				"LifecycleStateChanged",
				"NamespaceQueue lifecycle state changed from %s to %s",
				current.Status.State,
				updated.Status.State,
			)
		}
		c.recordConditionEvents(current, updated)

		return nil
	})
}

func (c *namespaceQueueController) recordConditionEvents(
	oldNQ, newNQ *schedulingv1beta1.NamespaceQueue,
) {
	for _, conditionType := range []string{
		commonutil.NamespaceQueueAuthorizedCondition,
		commonutil.NamespaceQueueReadyCondition,
	} {
		oldCondition := apiMeta.FindStatusCondition(oldNQ.Status.Conditions, conditionType)
		newCondition := apiMeta.FindStatusCondition(newNQ.Status.Conditions, conditionType)
		if !conditionEventChanged(oldCondition, newCondition) || newCondition == nil {
			continue
		}

		eventType := corev1.EventTypeNormal
		if newCondition.Status == metav1.ConditionFalse {
			eventType = corev1.EventTypeWarning
		}
		c.recorder.Eventf(
			newNQ,
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
			readyReason = commonutil.NamespaceQueueReasonInvalidDesiredState
			readyMessage = "NamespaceQueue desired state is invalid"
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
	current schedulingv1beta1.NamespaceQueueStatus,
	desired schedulingv1beta1.NamespaceQueueStatus,
) bool {
	return current.State == desired.State &&
		current.Unknown == desired.Unknown &&
		current.Pending == desired.Pending &&
		current.Running == desired.Running &&
		current.Inqueue == desired.Inqueue &&
		current.Completed == desired.Completed &&
		equality.Semantic.DeepEqual(current.Conditions, desired.Conditions)
}
