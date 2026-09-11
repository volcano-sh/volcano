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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	commonutil "volcano.sh/volcano/pkg/util"
)

type queueResources struct {
	capability corev1.ResourceList
	guarantee  corev1.ResourceList
	deserved   corev1.ResourceList
}

// namespaceQueueResources extracts the resource constraints owned by a
// NamespaceQueue for local and aggregate validation.
func namespaceQueueResources(queue *schedulingv1beta1.NamespaceQueue) queueResources {
	return queueResources{
		capability: queue.Spec.Capability,
		guarantee:  queue.Spec.Guarantee.Resource,
		deserved:   queue.Spec.Deserved,
	}
}

// clusterQueueResources extracts the equivalent resource constraints from a
// cluster Queue parent.
func clusterQueueResources(queue *schedulingv1beta1.Queue) queueResources {
	return queueResources{
		capability: queue.Spec.Capability,
		guarantee:  queue.Spec.Guarantee.Resource,
		deserved:   queue.Spec.Deserved,
	}
}

// validateNamespaceQueueConstraints checks local resource relations, ancestor
// capability limits, and child aggregates.
func (c *namespaceQueueController) validateNamespaceQueueConstraints(
	namespaceQueue *schedulingv1beta1.NamespaceQueue,
	parentRef commonutil.ResolvedQueueReference,
) (string, string, error) {
	resources := namespaceQueueResources(namespaceQueue)
	if err := commonutil.ValidateQueueResourceRelations(
		resources.capability, resources.guarantee, resources.deserved,
	); err != nil {
		return commonutil.NamespaceQueueReasonParentConstraintViolation, err.Error(), nil
	}

	if err := c.validateCapabilityAgainstAncestors(namespaceQueue); err != nil {
		return commonutil.NamespaceQueueReasonParentConstraintViolation, err.Error(), nil
	}
	if err := c.validateChildrenAggregate(parentRef); err != nil {
		return commonutil.NamespaceQueueReasonParentConstraintViolation, err.Error(), nil
	}
	if err := c.validateChildrenAggregate(namespaceQueueReference(namespaceQueue)); err != nil {
		if !apierrors.IsNotFound(err) {
			return commonutil.NamespaceQueueReasonParentConstraintViolation, err.Error(), nil
		}
	}

	return "", "", nil
}

// validateCapabilityAgainstAncestors prevents a descendant capability from
// exceeding any NamespaceQueue or cluster Queue capability in its parent chain.
func (c *namespaceQueueController) validateCapabilityAgainstAncestors(
	namespaceQueue *schedulingv1beta1.NamespaceQueue,
) error {
	capability := namespaceQueue.Spec.Capability
	currentQueue := namespaceQueue
	for {
		parentRef, err := resolveParent(currentQueue)
		if err != nil {
			return nil
		}

		if parentRef.Scope == commonutil.NamespaceQueueReferenceScope {
			parentQueue, err := c.namespaceQueueLister.NamespaceQueues(parentRef.Namespace).Get(parentRef.Name)
			if err != nil {
				return err
			}
			if err := commonutil.ValidateResourceListLimit(capability, parentQueue.Spec.Capability, "capability"); err != nil {
				return fmt.Errorf("NamespaceQueue %s/%s: %w", namespaceQueue.Namespace, namespaceQueue.Name, err)
			}
			currentQueue = parentQueue
			continue
		}

		parentQueue, err := c.queueLister.Get(parentRef.Name)
		if err != nil {
			return err
		}
		for parentQueue != nil {
			if err := commonutil.ValidateResourceListLimit(capability, parentQueue.Spec.Capability, "capability"); err != nil {
				return fmt.Errorf("NamespaceQueue %s/%s: %w", namespaceQueue.Namespace, namespaceQueue.Name, err)
			}
			if parentQueue.Spec.Parent == "" || parentQueue.Spec.Parent == "root" {
				return nil
			}
			parentQueue, err = c.queueLister.Get(parentQueue.Spec.Parent)
			if err != nil {
				return err
			}
		}
	}
}

// validateChildrenAggregate checks guarantees and deserved resources against
// the selected parent. Cluster Queue children are included because both queue
// scopes share the same parent resource budget.
func (c *namespaceQueueController) validateChildrenAggregate(
	parentRef commonutil.ResolvedQueueReference,
) error {
	var parentResources queueResources
	switch parentRef.Scope {
	case commonutil.NamespaceQueueReferenceScope:
		parentQueue, err := c.namespaceQueueLister.NamespaceQueues(parentRef.Namespace).Get(parentRef.Name)
		if err != nil {
			return err
		}
		parentResources = namespaceQueueResources(parentQueue)
	case commonutil.ClusterQueueReferenceScope:
		parentQueue, err := c.queueLister.Get(parentRef.Name)
		if err != nil {
			return err
		}
		parentResources = clusterQueueResources(parentQueue)
	default:
		return fmt.Errorf("unknown parent scope %q", parentRef.Scope)
	}

	totalGuarantee := corev1.ResourceList{}
	totalDeserved := corev1.ResourceList{}
	childQueues, err := c.getDirectChildNamespaceQueues(parentRef)
	if err != nil {
		return err
	}
	for _, childQueue := range childQueues {
		commonutil.AddResourceList(totalGuarantee, childQueue.Spec.Guarantee.Resource)
		commonutil.AddResourceList(totalDeserved, childQueue.Spec.Deserved)
	}

	if parentRef.Scope == commonutil.ClusterQueueReferenceScope {
		indexedObjects, err := c.queueInformer.Informer().GetIndexer().ByIndex(clusterQueueParentIndexName, parentRef.Name)
		if err != nil {
			return err
		}
		for _, indexedObject := range indexedObjects {
			childQueue, ok := indexedObject.(*schedulingv1beta1.Queue)
			if !ok {
				continue
			}
			commonutil.AddResourceList(totalGuarantee, childQueue.Spec.Guarantee.Resource)
			commonutil.AddResourceList(totalDeserved, childQueue.Spec.Deserved)
		}
	}

	if err := commonutil.ValidateResourceListLimit(totalGuarantee, parentResources.guarantee, "sum of child guarantees"); err != nil {
		return err
	}
	return commonutil.ValidateResourceListLimit(totalDeserved, parentResources.deserved, "sum of child deserved resources")
}
