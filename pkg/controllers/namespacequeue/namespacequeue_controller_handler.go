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

	"k8s.io/apimachinery/pkg/api/equality"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	controllermetrics "volcano.sh/volcano/pkg/controllers/metrics"
	controllerutil "volcano.sh/volcano/pkg/util"
)

const (
	namespaceQueuePodGroupIndex   = "namespaceQueue"
	namespaceQueueParentIndexName = "namespaceQueueParent"
	clusterQueueParentIndexName   = "namespaceQueueClusterParent"
)

// clusterQueueParentIndexFunc indexes Queue objects by their cluster-scoped
// parent so Queue changes can requeue affected NamespaceQueue subtrees.
func clusterQueueParentIndexFunc(obj interface{}) ([]string, error) {
	queue, ok := obj.(*schedulingv1beta1.Queue)
	if !ok {
		return nil, fmt.Errorf("object is not a Queue: %T", obj)
	}
	if queue.Spec.Parent == "" {
		return nil, nil
	}
	return []string{queue.Spec.Parent}, nil
}

// enqueueNamespaceQueue enqueues one NamespaceQueue key derived from object
// metadata. It is safe to call from informer callbacks because it performs no
// API operation.
func (c *namespaceQueueController) enqueueNamespaceQueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("failed to get NamespaceQueue key: %v", err)
		return
	}

	c.workQueue.Add(key)
}

// addNamespaceQueue requeues the new queue and its parent tree so sibling
// aggregate constraints are recalculated after a child is attached.
func (c *namespaceQueueController) addNamespaceQueue(obj interface{}) {
	namespaceQueue, ok := obj.(*schedulingv1beta1.NamespaceQueue)
	if !ok {
		klog.Errorf("object is not a NamespaceQueue: %#v", obj)
		return
	}

	c.enqueueNamespaceQueue(namespaceQueue)
	c.enqueueDescendantNamespaceQueues(namespaceQueueReference(namespaceQueue))
	if parentRef, err := resolveParent(namespaceQueue); err == nil {
		c.enqueueNamespaceQueueTree(parentRef)
	}
}

// updateNamespaceQueue requeues the current subtree and both parent trees when
// a Spec change moves or resizes a NamespaceQueue.
func (c *namespaceQueueController) updateNamespaceQueue(oldObj, newObj interface{}) {
	oldNamespaceQueue, ok := oldObj.(*schedulingv1beta1.NamespaceQueue)
	if !ok {
		return
	}

	newNamespaceQueue, ok := newObj.(*schedulingv1beta1.NamespaceQueue)
	if !ok {
		return
	}

	if oldNamespaceQueue.ResourceVersion == newNamespaceQueue.ResourceVersion {
		return
	}
	if oldNamespaceQueue.DeletionTimestamp == nil && newNamespaceQueue.DeletionTimestamp != nil {
		c.enqueueNamespaceQueue(newNamespaceQueue)
	}

	specChanged := !equality.Semantic.DeepEqual(oldNamespaceQueue.Spec, newNamespaceQueue.Spec)
	if specChanged {
		c.enqueueNamespaceQueue(newNamespaceQueue)
	}
	// Requeue runtime changes while a deleting queue drains so finalizer
	// removal follows scheduler-owned status updates.
	if newNamespaceQueue.DeletionTimestamp != nil &&
		(!equality.Semantic.DeepEqual(oldNamespaceQueue.Status.Allocated, newNamespaceQueue.Status.Allocated) ||
			!equality.Semantic.DeepEqual(oldNamespaceQueue.Status.Reservation, newNamespaceQueue.Status.Reservation)) {
		c.enqueueNamespaceQueue(newNamespaceQueue)
	}
	if namespaceQueueAffectsDescendants(oldNamespaceQueue, newNamespaceQueue) {
		c.enqueueDescendantNamespaceQueues(namespaceQueueReference(newNamespaceQueue))
	}
	if specChanged {
		for _, queue := range []*schedulingv1beta1.NamespaceQueue{oldNamespaceQueue, newNamespaceQueue} {
			parentRef, err := resolveParent(queue)
			if err != nil {
				continue
			}
			c.enqueueNamespaceQueueTree(parentRef)
		}
	}
}

func namespaceQueueAffectsDescendants(oldQueue, newQueue *schedulingv1beta1.NamespaceQueue) bool {
	if !equality.Semantic.DeepEqual(oldQueue.Spec, newQueue.Spec) ||
		oldQueue.Status.State != newQueue.Status.State {
		return true
	}
	for _, conditionType := range []string{
		controllerutil.NamespaceQueueAuthorizedCondition,
		controllerutil.NamespaceQueueReadyCondition,
	} {
		if !equality.Semantic.DeepEqual(
			apiMeta.FindStatusCondition(oldQueue.Status.Conditions, conditionType),
			apiMeta.FindStatusCondition(newQueue.Status.Conditions, conditionType),
		) {
			return true
		}
	}
	return false
}

func (c *namespaceQueueController) deleteNamespaceQueue(obj interface{}) {
	namespaceQueue, ok := obj.(*schedulingv1beta1.NamespaceQueue)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("unexpected delete object: %#v", obj)
			return
		}

		namespaceQueue, ok = tombstone.Obj.(*schedulingv1beta1.NamespaceQueue)
		if !ok {
			klog.Errorf("tombstone object is not a NamespaceQueue: %#v", tombstone.Obj)
			return
		}
	}
	controllermetrics.DeleteNamespaceQueueMetrics(namespaceQueue)

	// The object is already deleted; sync will stop when the lister returns NotFound.
	c.enqueueNamespaceQueue(namespaceQueue)
	c.enqueueDescendantNamespaceQueues(namespaceQueueReference(namespaceQueue))
	if parentRef, err := resolveParent(namespaceQueue); err == nil {
		c.enqueueNamespaceQueueTree(parentRef)
	}
}

func (c *namespaceQueueController) addPodGroup(obj interface{}) {
	podGroup, ok := obj.(*schedulingv1beta1.PodGroup)
	if !ok {
		klog.Errorf("object is not a PodGroup: %#v", obj)
		return
	}

	c.enqueueNamespaceQueueForPodGroup(podGroup)
}

func (c *namespaceQueueController) updatePodGroup(oldObj, newObj interface{}) {
	oldPodGroup, ok := oldObj.(*schedulingv1beta1.PodGroup)
	if !ok {
		return
	}

	newPodGroup, ok := newObj.(*schedulingv1beta1.PodGroup)
	if !ok {
		return
	}

	if oldPodGroup.ResourceVersion == newPodGroup.ResourceVersion ||
		(oldPodGroup.Spec.Queue == newPodGroup.Spec.Queue && oldPodGroup.Status.Phase == newPodGroup.Status.Phase) {
		return
	}

	keys := make(map[string]struct{}, 2)
	for _, podGroup := range []*schedulingv1beta1.PodGroup{oldPodGroup, newPodGroup} {
		if name, ok := namespaceQueueNameForPodGroup(podGroup); ok {
			keys[podGroup.Namespace+"/"+name] = struct{}{}
		}
	}
	for key := range keys {
		c.workQueue.Add(key)
	}
}

func (c *namespaceQueueController) deletePodGroup(obj interface{}) {
	podGroup, ok := obj.(*schedulingv1beta1.PodGroup)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("unexpected delete object: %#v", obj)
			return
		}

		podGroup, ok = tombstone.Obj.(*schedulingv1beta1.PodGroup)
		if !ok {
			klog.Errorf("tombstone object is not a PodGroup: %#v", tombstone.Obj)
			return
		}
	}

	c.enqueueNamespaceQueueForPodGroup(podGroup)
}

func (c *namespaceQueueController) enqueueNamespaceQueueForPodGroup(
	podGroup *schedulingv1beta1.PodGroup,
) {
	name, ok := namespaceQueueNameForPodGroup(podGroup)
	if !ok {
		return
	}

	c.workQueue.Add(podGroup.Namespace + "/" + name)
}

func namespaceQueueNameForPodGroup(
	podGroup *schedulingv1beta1.PodGroup,
) (string, bool) {
	if podGroup == nil {
		return "", false
	}

	resolved, err := controllerutil.ResolveWorkloadQueueReference(podGroup.Namespace, podGroup.Spec.Queue, "")
	if err != nil || resolved.Scope != controllerutil.NamespaceQueueReferenceScope {
		return "", false
	}

	return resolved.Name, true
}

func namespaceQueuePodGroupIndexFunc(obj interface{}) ([]string, error) {
	podGroup, ok := obj.(*schedulingv1beta1.PodGroup)
	if !ok {
		return nil, fmt.Errorf("object is not a PodGroup: %T", obj)
	}

	name, ok := namespaceQueueNameForPodGroup(podGroup)
	if !ok {
		return nil, nil
	}

	return []string{podGroup.Namespace + "/" + name}, nil
}

func (c *namespaceQueueController) addQueue(obj interface{}) {
	queue, ok := obj.(*schedulingv1beta1.Queue)
	if !ok {
		klog.Errorf("object is not a Queue: %#v", obj)
		return
	}

	c.enqueueDescendantNamespaceQueues(controllerutil.ResolvedQueueReference{
		Scope: controllerutil.ClusterQueueReferenceScope,
		Name:  queue.Name,
	})
}

func (c *namespaceQueueController) updateQueue(oldObj, newObj interface{}) {
	oldQueue, ok := oldObj.(*schedulingv1beta1.Queue)
	if !ok {
		return
	}

	newQueue, ok := newObj.(*schedulingv1beta1.Queue)
	if !ok {
		return
	}

	if oldQueue.ResourceVersion == newQueue.ResourceVersion {
		return
	}
	if equality.Semantic.DeepEqual(oldQueue.Spec, newQueue.Spec) &&
		oldQueue.Status.State == newQueue.Status.State {
		return
	}

	// Changes to the Queue spec or status may affect NamespaceQueue availability.
	c.enqueueDescendantNamespaceQueues(controllerutil.ResolvedQueueReference{
		Scope: controllerutil.ClusterQueueReferenceScope,
		Name:  newQueue.Name,
	})
}

func (c *namespaceQueueController) deleteQueue(obj interface{}) {
	queue, ok := obj.(*schedulingv1beta1.Queue)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("unexpected delete object: %#v", obj)
			return
		}

		queue, ok = tombstone.Obj.(*schedulingv1beta1.Queue)
		if !ok {
			klog.Errorf("tombstone object is not a Queue: %#v", tombstone.Obj)
			return
		}
	}

	c.enqueueDescendantNamespaceQueues(controllerutil.ResolvedQueueReference{
		Scope: controllerutil.ClusterQueueReferenceScope,
		Name:  queue.Name,
	})
}

func namespaceQueueParentIndexFunc(obj interface{}) ([]string, error) {
	namespaceQueue, ok := obj.(*schedulingv1beta1.NamespaceQueue)
	if !ok {
		return nil, fmt.Errorf("object is not a NamespaceQueue: %T", obj)
	}

	parentRef, err := resolveParent(namespaceQueue)
	if err != nil {
		return nil, nil
	}

	key := queueReferenceKey(parentRef)
	if key == "" {
		return nil, nil
	}

	return []string{key}, nil
}

func (c *namespaceQueueController) getDirectChildNamespaceQueues(
	parentRef controllerutil.ResolvedQueueReference,
) ([]*schedulingv1beta1.NamespaceQueue, error) {
	key := queueReferenceKey(parentRef)
	if key == "" {
		return nil, fmt.Errorf("invalid parent queue reference: %#v", parentRef)
	}

	indexedObjects, err := c.namespaceQueueInformer.Informer().GetIndexer().ByIndex(
		namespaceQueueParentIndexName,
		key,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to query NamespaceQueue parent index %q: %w",
			key,
			err,
		)
	}

	childQueues := make([]*schedulingv1beta1.NamespaceQueue, 0, len(indexedObjects))
	for _, indexedObject := range indexedObjects {
		childQueue, ok := indexedObject.(*schedulingv1beta1.NamespaceQueue)
		if !ok {
			continue
		}
		childQueues = append(childQueues, childQueue)
	}

	return childQueues, nil
}

// enqueueDescendantNamespaceQueues traverses the indexed hierarchy without
// holding application locks. The visited set bounds work even when admission
// data is temporarily inconsistent or a cycle is observed from the cache.
func (c *namespaceQueueController) enqueueDescendantNamespaceQueues(
	root controllerutil.ResolvedQueueReference,
) {
	if queueReferenceKey(root) == "" {
		klog.Errorf("cannot enqueue descendants of invalid queue reference: %#v", root)
		return
	}

	visited := map[controllerutil.ResolvedQueueReference]struct{}{root: {}}
	frontier := []controllerutil.ResolvedQueueReference{root}
	for index := 0; index < len(frontier); index++ {
		currentParentRef := frontier[index]
		childQueues, err := c.getDirectChildNamespaceQueues(currentParentRef)
		if err != nil {
			klog.Errorf(
				"failed to get children of queue %q: %v",
				queueReferenceKey(currentParentRef),
				err,
			)
			return
		}

		for _, childQueue := range childQueues {
			childQueueRef := namespaceQueueReference(childQueue)
			if _, alreadyVisited := visited[childQueueRef]; alreadyVisited {
				continue
			}

			visited[childQueueRef] = struct{}{}
			c.enqueueNamespaceQueue(childQueue)
			frontier = append(frontier, childQueueRef)
		}
	}
}

// enqueueNamespaceQueueTree enqueues a NamespaceQueue root, when present, and
// every NamespaceQueue below it. A cluster Queue root has no controller work
// item, so only its NamespaceQueue descendants are enqueued.
func (c *namespaceQueueController) enqueueNamespaceQueueTree(
	root controllerutil.ResolvedQueueReference,
) {
	if root.Scope == controllerutil.NamespaceQueueReferenceScope &&
		root.Namespace != "" && root.Name != "" {
		c.workQueue.Add(root.Namespace + "/" + root.Name)
	}
	c.enqueueDescendantNamespaceQueues(root)
}
