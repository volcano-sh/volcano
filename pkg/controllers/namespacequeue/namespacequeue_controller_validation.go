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

func namespaceQueueResources(queue *schedulingv1beta1.NamespaceQueue) queueResources {
	return queueResources{
		capability: queue.Spec.Capability,
		guarantee:  queue.Spec.Guarantee.Resource,
		deserved:   queue.Spec.Deserved,
	}
}

func clusterQueueResources(queue *schedulingv1beta1.Queue) queueResources {
	return queueResources{
		capability: queue.Spec.Capability,
		guarantee:  queue.Spec.Guarantee.Resource,
		deserved:   queue.Spec.Deserved,
	}
}

func (c *namespaceQueueController) validateNamespaceQueueConstraints(
	queue *schedulingv1beta1.NamespaceQueue,
	parent commonutil.ResolvedQueueReference,
) (string, string, error) {
	resources := namespaceQueueResources(queue)
	if err := commonutil.ValidateQueueResourceRelations(
		resources.capability, resources.guarantee, resources.deserved,
	); err != nil {
		return commonutil.NamespaceQueueReasonParentConstraintViolation, err.Error(), nil
	}

	if parent.Scope == commonutil.ClusterQueueReferenceScope {
		children, err := c.getDirectChildNamespaceQueues(parent)
		if err != nil {
			return "", "", err
		}
		for _, child := range children {
			if child.Namespace == queue.Namespace && child.Name != queue.Name {
				return commonutil.NamespaceQueueReasonDuplicateClusterAttachment,
					fmt.Sprintf("namespace %q already attaches NamespaceQueue %q to Queue %q", queue.Namespace, child.Name, parent.Name), nil
			}
		}
	}

	if err := c.validateCapabilityAgainstAncestors(queue); err != nil {
		return commonutil.NamespaceQueueReasonParentConstraintViolation, err.Error(), nil
	}
	if err := c.validateChildrenAggregate(parent); err != nil {
		return commonutil.NamespaceQueueReasonParentConstraintViolation, err.Error(), nil
	}
	if err := c.validateChildrenAggregate(namespaceQueueReference(queue)); err != nil {
		if !apierrors.IsNotFound(err) {
			return commonutil.NamespaceQueueReasonParentConstraintViolation, err.Error(), nil
		}
	}

	return "", "", nil
}

func (c *namespaceQueueController) validateCapabilityAgainstAncestors(
	queue *schedulingv1beta1.NamespaceQueue,
) error {
	capability := queue.Spec.Capability
	current := queue
	for {
		parent, err := resolveParent(current)
		if err != nil {
			return nil
		}

		if parent.Scope == commonutil.NamespaceQueueReferenceScope {
			parentQueue, err := c.namespaceQueueLister.NamespaceQueues(parent.Namespace).Get(parent.Name)
			if err != nil {
				return err
			}
			if err := commonutil.ValidateResourceListLimit(capability, parentQueue.Spec.Capability, "capability"); err != nil {
				return fmt.Errorf("NamespaceQueue %s/%s: %w", queue.Namespace, queue.Name, err)
			}
			current = parentQueue
			continue
		}

		parentQueue, err := c.queueLister.Get(parent.Name)
		if err != nil {
			return err
		}
		for parentQueue != nil {
			if err := commonutil.ValidateResourceListLimit(capability, parentQueue.Spec.Capability, "capability"); err != nil {
				return fmt.Errorf("NamespaceQueue %s/%s: %w", queue.Namespace, queue.Name, err)
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

func (c *namespaceQueueController) validateChildrenAggregate(
	parent commonutil.ResolvedQueueReference,
) error {
	var parentResources queueResources
	switch parent.Scope {
	case commonutil.NamespaceQueueReferenceScope:
		queue, err := c.namespaceQueueLister.NamespaceQueues(parent.Namespace).Get(parent.Name)
		if err != nil {
			return err
		}
		parentResources = namespaceQueueResources(queue)
	case commonutil.ClusterQueueReferenceScope:
		queue, err := c.queueLister.Get(parent.Name)
		if err != nil {
			return err
		}
		parentResources = clusterQueueResources(queue)
	default:
		return fmt.Errorf("unknown parent scope %q", parent.Scope)
	}

	totalGuarantee := corev1.ResourceList{}
	totalDeserved := corev1.ResourceList{}
	children, err := c.getDirectChildNamespaceQueues(parent)
	if err != nil {
		return err
	}
	for _, child := range children {
		commonutil.AddResourceList(totalGuarantee, child.Spec.Guarantee.Resource)
		commonutil.AddResourceList(totalDeserved, child.Spec.Deserved)
	}

	if parent.Scope == commonutil.ClusterQueueReferenceScope {
		objects, err := c.queueInformer.Informer().GetIndexer().ByIndex(clusterQueueParentIndexName, parent.Name)
		if err != nil {
			return err
		}
		for _, object := range objects {
			child, ok := object.(*schedulingv1beta1.Queue)
			if !ok {
				continue
			}
			commonutil.AddResourceList(totalGuarantee, child.Spec.Guarantee.Resource)
			commonutil.AddResourceList(totalDeserved, child.Spec.Deserved)
		}
	}

	if err := commonutil.ValidateResourceListLimit(totalGuarantee, parentResources.guarantee, "sum of child guarantees"); err != nil {
		return err
	}
	return commonutil.ValidateResourceListLimit(totalDeserved, parentResources.deserved, "sum of child deserved resources")
}
