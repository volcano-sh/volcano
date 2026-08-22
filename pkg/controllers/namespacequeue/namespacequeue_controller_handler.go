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
	controllerutil "volcano.sh/volcano/pkg/util"
)

const (
	namespaceQueuePodGroupIndex   = "namespaceQueue"
	namespaceQueueParentIndexName = "namespaceQueueParent"
	clusterQueueParentIndexName   = "namespaceQueueClusterParent"
)

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

func (c *namespaceQueueController) enqueueNamespaceQueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("failed to get NamespaceQueue key: %v", err)
		return
	}

	c.workQueue.Add(key)
}

func (c *namespaceQueueController) addNamespaceQueue(obj interface{}) {
	nq, ok := obj.(*schedulingv1beta1.NamespaceQueue)
	if !ok {
		klog.Errorf("object is not a NamespaceQueue: %#v", obj)
		return
	}

	c.enqueueNamespaceQueue(nq)
	c.enqueueDescendantNamespaceQueues(namespaceQueueReference(nq))
}

func (c *namespaceQueueController) updateNamespaceQueue(oldObj, newObj interface{}) {
	oldNQ, ok := oldObj.(*schedulingv1beta1.NamespaceQueue)
	if !ok {
		return
	}

	newNQ, ok := newObj.(*schedulingv1beta1.NamespaceQueue)
	if !ok {
		return
	}

	if oldNQ.ResourceVersion == newNQ.ResourceVersion {
		return
	}
	if oldNQ.DeletionTimestamp == nil && newNQ.DeletionTimestamp != nil {
		c.enqueueNamespaceQueue(newNQ)
	}

	if !equality.Semantic.DeepEqual(oldNQ.Spec, newNQ.Spec) {
		c.enqueueNamespaceQueue(newNQ)
	}
	if controllerutil.EffectiveNamespaceQueueState(newNQ.Spec.State) == schedulingv1beta1.QueueStateClosed &&
		(!equality.Semantic.DeepEqual(oldNQ.Status.Allocated, newNQ.Status.Allocated) ||
			!equality.Semantic.DeepEqual(oldNQ.Status.Reservation, newNQ.Status.Reservation)) {
		c.enqueueNamespaceQueue(newNQ)
	}

	if namespaceQueueAffectsDescendants(oldNQ, newNQ) {
		c.enqueueDescendantNamespaceQueues(namespaceQueueReference(newNQ))
		for _, queue := range []*schedulingv1beta1.NamespaceQueue{oldNQ, newNQ} {
			parent, err := resolveParent(queue)
			if err != nil {
				continue
			}
			c.enqueueDescendantNamespaceQueues(parent)
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
	nq, ok := obj.(*schedulingv1beta1.NamespaceQueue)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("unexpected delete object: %#v", obj)
			return
		}

		nq, ok = tombstone.Obj.(*schedulingv1beta1.NamespaceQueue)
		if !ok {
			klog.Errorf("tombstone object is not a NamespaceQueue: %#v", tombstone.Obj)
			return
		}
	}

	// The object is already deleted; sync will stop when the lister returns NotFound.
	c.enqueueNamespaceQueue(nq)
	c.enqueueDescendantNamespaceQueues(namespaceQueueReference(nq))
}

func (c *namespaceQueueController) addPodGroup(obj interface{}) {
	pg, ok := obj.(*schedulingv1beta1.PodGroup)
	if !ok {
		klog.Errorf("object is not a PodGroup: %#v", obj)
		return
	}

	c.enqueueNamespaceQueueTargetForPodGroup(pg)
}

func (c *namespaceQueueController) updatePodGroup(oldObj, newObj interface{}) {
	oldPG, ok := oldObj.(*schedulingv1beta1.PodGroup)
	if !ok {
		return
	}

	newPG, ok := newObj.(*schedulingv1beta1.PodGroup)
	if !ok {
		return
	}

	if oldPG.ResourceVersion == newPG.ResourceVersion ||
		(oldPG.Spec.Queue == newPG.Spec.Queue && oldPG.Status.Phase == newPG.Status.Phase) {
		return
	}

	keys := make(map[string]struct{}, 2)
	for _, pg := range []*schedulingv1beta1.PodGroup{oldPG, newPG} {
		if name, ok := namespaceQueueNameForPodGroup(pg); ok {
			keys[pg.Namespace+"/"+name] = struct{}{}
		}
	}
	for key := range keys {
		c.workQueue.Add(key)
	}
}

func (c *namespaceQueueController) deletePodGroup(obj interface{}) {
	pg, ok := obj.(*schedulingv1beta1.PodGroup)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("unexpected delete object: %#v", obj)
			return
		}

		pg, ok = tombstone.Obj.(*schedulingv1beta1.PodGroup)
		if !ok {
			klog.Errorf("tombstone object is not a PodGroup: %#v", tombstone.Obj)
			return
		}
	}

	c.enqueueNamespaceQueueTargetForPodGroup(pg)
}

func (c *namespaceQueueController) enqueueNamespaceQueueTargetForPodGroup(
	pg *schedulingv1beta1.PodGroup,
) {
	name, ok := namespaceQueueNameForPodGroup(pg)
	if !ok {
		return
	}

	c.workQueue.Add(pg.Namespace + "/" + name)
}

func namespaceQueueNameForPodGroup(
	pg *schedulingv1beta1.PodGroup,
) (string, bool) {
	if pg == nil {
		return "", false
	}

	resolved, err := controllerutil.ResolveWorkloadQueueReference(pg.Namespace, pg.Spec.Queue, "")
	if err != nil || resolved.Scope != controllerutil.NamespaceQueueReferenceScope {
		return "", false
	}

	return resolved.Name, true
}

func namespaceQueuePodGroupIndexFunc(obj interface{}) ([]string, error) {
	pg, ok := obj.(*schedulingv1beta1.PodGroup)
	if !ok {
		return nil, fmt.Errorf("object is not a PodGroup: %T", obj)
	}

	name, ok := namespaceQueueNameForPodGroup(pg)
	if !ok {
		return nil, nil
	}

	return []string{pg.Namespace + "/" + name}, nil
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
	nq, ok := obj.(*schedulingv1beta1.NamespaceQueue)
	if !ok {
		return nil, fmt.Errorf("object is not a NamespaceQueue: %T", obj)
	}

	parent, err := resolveParent(nq)
	if err != nil {
		return nil, nil
	}

	key := queueReferenceKey(parent)
	if key == "" {
		return nil, nil
	}

	return []string{key}, nil
}

func (c *namespaceQueueController) getDirectChildNamespaceQueues(
	parent controllerutil.ResolvedQueueReference,
) ([]*schedulingv1beta1.NamespaceQueue, error) {
	key := queueReferenceKey(parent)
	if key == "" {
		return nil, fmt.Errorf("invalid parent queue reference: %#v", parent)
	}

	objects, err := c.namespaceQueueInformer.Informer().GetIndexer().ByIndex(
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

	children := make([]*schedulingv1beta1.NamespaceQueue, 0, len(objects))
	for _, object := range objects {
		child, ok := object.(*schedulingv1beta1.NamespaceQueue)
		if !ok {
			continue
		}
		children = append(children, child)
	}

	return children, nil
}

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
		children, err := c.getDirectChildNamespaceQueues(currentParentRef)
		if err != nil {
			klog.Errorf(
				"failed to get children of queue %q: %v",
				queueReferenceKey(currentParentRef),
				err,
			)
			return
		}

		for _, child := range children {
			childQueueRef := namespaceQueueReference(child)
			if _, alreadyVisited := visited[childQueueRef]; alreadyVisited {
				continue
			}

			visited[childQueueRef] = struct{}{}
			c.enqueueNamespaceQueue(child)
			frontier = append(frontier, childQueueRef)
		}
	}
}
