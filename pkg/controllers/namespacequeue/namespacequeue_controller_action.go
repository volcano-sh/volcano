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

func (c *namespaceQueueController) reconcileNamespaceQueue(namespaceQueue *schedulingv1beta1.NamespaceQueue) error {
	if namespaceQueue.DeletionTimestamp == nil {
		if err := c.ensureNamespaceQueueFinalizer(namespaceQueue); err != nil {
			return err
		}
	} else if !hasFinalizer(namespaceQueue, namespaceQueueFinalizer) {
		return nil
	}

	currentQueue, err := c.updateNamespaceQueueStatus(namespaceQueue)
	if err != nil {
		return err
	}
	if currentQueue == nil || currentQueue.DeletionTimestamp == nil {
		return nil
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
	return containsFinalizer(namespaceQueue.Finalizers, finalizer)
}

func containsFinalizer(finalizers []string, finalizer string) bool {
	for _, existing := range finalizers {
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
		if containsFinalizer(finalizers, namespaceQueueFinalizer) {
			return finalizers
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

func parentConditionFailure(result *namespaceQueueConditionResult, reason, message string) {
	result.authorizedReason = reason
	result.authorizedMessage = message
	result.readyReason = reason
	result.readyMessage = message
}

func (c *namespaceQueueController) evaluateParent(
	namespaceQueue *schedulingv1beta1.NamespaceQueue,
) (namespaceQueueConditionResult, error) {
	result := namespaceQueueConditionResult{
		authorizedStatus: metav1.ConditionUnknown,
		readyStatus:      metav1.ConditionFalse,
	}

	parentRef, err := resolveParent(namespaceQueue)
	if err != nil {
		parentConditionFailure(&result, commonutil.NamespaceQueueReasonInvalidParentReference, err.Error())
		return result, nil
	}

	switch parentRef.Scope {
	case commonutil.ClusterQueueReferenceScope:
		parentQueue, err := c.queueLister.Get(parentRef.Name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				message := fmt.Sprintf("parent Queue %q was not found", parentRef.Name)
				parentConditionFailure(&result, commonutil.NamespaceQueueReasonParentNotFound, message)
				return result, nil
			}
			return result, fmt.Errorf("failed to get parent Queue %q: %w", parentRef.Name, err)
		}

		if !commonutil.IsNamespaceAllowedByQueue(parentQueue, namespaceQueue.Namespace) {
			message := fmt.Sprintf(
				"namespace %q is not allowed to use Queue %q",
				namespaceQueue.Namespace,
				parentQueue.Name,
			)
			result.authorizedStatus = metav1.ConditionFalse
			parentConditionFailure(&result, commonutil.NamespaceQueueReasonNamespaceNotAllowed, message)
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
				parentConditionFailure(&result, commonutil.NamespaceQueueReasonParentNotFound, message)
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
				parentConditionFailure(&result, commonutil.NamespaceQueueReasonHierarchyCycle, err.Error())
				return result, nil
			}
			return result, err
		}
		if depth > c.maxNamespaceQueueDepth {
			parentConditionFailure(&result, commonutil.NamespaceQueueReasonHierarchyDepthExceeded, fmt.Sprintf(
				"NamespaceQueue hierarchy depth %d exceeds maximum depth %d",
				depth,
				c.maxNamespaceQueueDepth,
			))
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
		if !commonutil.IsNamespaceQueueSchedulable(parentQueue.Generation, string(parentQueue.Status.State), parentQueue.Status.Conditions) {
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
			status.Unknown++
		}
	}

	return status, nil
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
	namespaceQueue *schedulingv1beta1.NamespaceQueue,
) (*schedulingv1beta1.NamespaceQueue, error) {
	var latestQueue *schedulingv1beta1.NamespaceQueue
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		ctx, cancel := c.apiContext()
		defer cancel()
		currentQueue, err := c.vcClient.
			SchedulingV1beta1().
			NamespaceQueues(namespaceQueue.Namespace).
			Get(ctx, namespaceQueue.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get NamespaceQueue status: %w", err)
		}
		latestQueue = currentQueue
		if currentQueue.Generation != namespaceQueue.Generation {
			controllermetrics.UpdateNamespaceQueueMetrics(currentQueue, &currentQueue.Status)
			return nil
		}

		counters, err := c.calculatePodGroupCounters(currentQueue)
		if err != nil {
			return err
		}

		parentConditions, err := c.evaluateParent(currentQueue)
		if err != nil {
			return err
		}
		desiredStatus := desiredNamespaceQueueStatus(currentQueue, counters, parentConditions)

		if !namespaceQueueControllerStatusEqual(currentQueue.Status, desiredStatus) {
			updatedQueue := currentQueue.DeepCopy()
			updatedQueue.Status = desiredStatus

			updatedQueue, err = c.vcClient.
				SchedulingV1beta1().
				NamespaceQueues(namespaceQueue.Namespace).
				UpdateStatus(ctx, updatedQueue, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update NamespaceQueue status: %w", err)
			}
			latestQueue = updatedQueue
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

		controllermetrics.UpdateNamespaceQueueMetrics(currentQueue, &desiredStatus)

		return nil
	})
	return latestQueue, err
}

func desiredNamespaceQueueStatus(
	queue *schedulingv1beta1.NamespaceQueue,
	counters schedulingv1beta1.NamespaceQueueStatus,
	parentConditions namespaceQueueConditionResult,
) schedulingv1beta1.NamespaceQueueStatus {
	status := *queue.Status.DeepCopy()
	status.Unknown = counters.Unknown
	status.Pending = counters.Pending
	status.Running = counters.Running
	status.Inqueue = counters.Inqueue
	status.Completed = counters.Completed
	status.State = schedulingv1beta1.QueueStateOpen
	if queue.DeletionTimestamp != nil {
		status.State = schedulingv1beta1.QueueStateClosed
		if !commonutil.IsNamespaceQueueWorkloadDrained(status) || !commonutil.IsNamespaceQueueRuntimeDrained(queue.Status) {
			status.State = schedulingv1beta1.QueueStateClosing
		}
	}
	setNamespaceQueueConditions(&status, queue.Generation, parentConditions)
	return status
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

func namespaceQueueControllerStatusEqual(
	currentStatus schedulingv1beta1.NamespaceQueueStatus,
	desiredStatus schedulingv1beta1.NamespaceQueueStatus,
) bool {
	currentStatus.Allocated = nil
	desiredStatus.Allocated = nil
	currentStatus.Reservation = schedulingv1beta1.Reservation{}
	desiredStatus.Reservation = schedulingv1beta1.Reservation{}
	return equality.Semantic.DeepEqual(currentStatus, desiredStatus)
}
