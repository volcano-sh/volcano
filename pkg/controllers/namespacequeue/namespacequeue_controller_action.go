/*
Copyright 2019 The Volcano Authors.

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
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func (c *namespaceQueueController) reconcileNamespaceQueue(nq *schedulingv1beta1.NamespaceQueue) error {
	status := *nq.Status.DeepCopy()

	if status.State == "" {
		status.State = schedulingv1beta1.QueueStateOpen
	}

	target, err := resolveParent(nq)
	if err != nil {
		setCondition(
			&status,
			nq.Generation,
			"Authorized",
			metav1.ConditionUnknown,
			"InvalidParentReference",
			err.Error(),
		)
		setCondition(
			&status,
			nq.Generation,
			"Ready",
			metav1.ConditionFalse,
			"InvalidParentReference",
			err.Error(),
		)

		return c.updateNamespaceQueueStatus(nq, status)
	}

	switch target.scope {
	case clusterParentScope:
		parent, err := c.queueLister.Get(target.name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				message := fmt.Sprintf(
					"parent Queue %q was not found",
					target.name,
				)
				setCondition(
					&status,
					nq.Generation,
					"Authorized",
					metav1.ConditionUnknown,
					"ParentNotFound",
					message,
				)
				setCondition(
					&status,
					nq.Generation,
					"Ready",
					metav1.ConditionFalse,
					"ParentNotFound",
					message,
				)

				return c.updateNamespaceQueueStatus(nq, status)
			}

			return fmt.Errorf("failed to get parent Queue %q: %w", target.name, err)
		}

		if !isNamespaceAllowed(parent, nq.Namespace) {
			message := fmt.Sprintf(
				"namespace %q is not allowed to use Queue %q",
				nq.Namespace,
				parent.Name,
			)
			setCondition(
				&status,
				nq.Generation,
				"Authorized",
				metav1.ConditionFalse,
				"NamespaceNotAllowed",
				message,
			)
			setCondition(
				&status,
				nq.Generation,
				"Ready",
				metav1.ConditionFalse,
				"NamespaceNotAllowed",
				message,
			)

			return c.updateNamespaceQueueStatus(nq, status)
		}

	case namespaceParentScope:
		parent, err := c.namespaceQueueLister.
			NamespaceQueues(target.namespace).
			Get(target.name)
		if err != nil {
			if apierrors.IsNotFound(err) {
				message := fmt.Sprintf(
					"parent NamespaceQueue %q/%q was not found",
					target.namespace,
					target.name,
				)
				setCondition(
					&status,
					nq.Generation,
					"Authorized",
					metav1.ConditionUnknown,
					"ParentNotFound",
					message,
				)
				setCondition(
					&status,
					nq.Generation,
					"Ready",
					metav1.ConditionFalse,
					"ParentNotFound",
					message,
				)

				return c.updateNamespaceQueueStatus(nq, status)
			}
			return fmt.Errorf(
				"failed to get parent NamespaceQueue %q/%q: %w",
				target.namespace,
				target.name,
				err,
			)
		}

		if !isNamespaceQueueReady(parent) {
			message := fmt.Sprintf(
				"parent NamespaceQueue %q/%q is not ready",
				target.namespace,
				target.name,
			)
			setCondition(
				&status,
				nq.Generation,
				"Ready",
				metav1.ConditionFalse,
				"ParentNotReady",
				message,
			)

			return c.updateNamespaceQueueStatus(nq, status)
		}

	default:
		return fmt.Errorf("unknown parent scope %q", target.scope)
	}

	setCondition(
		&status,
		nq.Generation,
		"Authorized",
		metav1.ConditionTrue,
		"NamespaceAllowed",
		"namespace is authorized to use the parent",
	)

	if status.State == schedulingv1beta1.QueueStateOpen {
		setCondition(
			&status,
			nq.Generation,
			"Ready",
			metav1.ConditionTrue,
			"Ready",
			"NamespaceQueue is ready for scheduling",
		)
	} else {
		setCondition(
			&status,
			nq.Generation,
			"Ready",
			metav1.ConditionFalse,
			"QueueNotOpen",
			"NamespaceQueue is not open",
		)
	}

	return c.updateNamespaceQueueStatus(nq, status)
}

func isNamespaceQueueReady(nq *schedulingv1beta1.NamespaceQueue) bool {
	if nq == nil || nq.Status.State != schedulingv1beta1.QueueStateOpen {
		return false
	}

	condition := apiMeta.FindStatusCondition(nq.Status.Conditions, "Ready")
	return condition != nil && condition.Status == metav1.ConditionTrue
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
	status schedulingv1beta1.NamespaceQueueStatus,
) error {
	if equality.Semantic.DeepEqual(nq.Status, status) {
		return nil
	}

	nqCopy := nq.DeepCopy()
	nqCopy.Status = status

	_, err := c.vcClient.
		SchedulingV1beta1().
		NamespaceQueues(nq.Namespace).
		UpdateStatus(
			context.Background(),
			nqCopy,
			metav1.UpdateOptions{},
		)
	if err != nil {
		return fmt.Errorf("failed to update NamespaceQueue status: %w", err)
	}

	return nil
}
