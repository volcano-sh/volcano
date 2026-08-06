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
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

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
	c.enqueueNamespaceQueuesForParent(parentTarget{
		scope:     namespaceParentScope,
		namespace: nq.Namespace,
		name:      nq.Name,
	})
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

	if !equality.Semantic.DeepEqual(oldNQ.Spec, newNQ.Spec) {
		c.enqueueNamespaceQueue(newNQ)
	}

	// Status changes can affect descendants without requiring this object to
	// reconcile again. Spec changes also requeue descendants of the changed object.
	for _, target := range namespaceQueueParentTargets(oldNQ, newNQ) {
		c.enqueueNamespaceQueuesForParent(target)
	}
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

	// 当前对象已经删除，sync 时会因为 NotFound 直接结束。
	c.enqueueNamespaceQueue(nq)
	c.enqueueNamespaceQueuesForParent(parentTarget{
		scope:     namespaceParentScope,
		namespace: nq.Namespace,
		name:      nq.Name,
	})
}

func namespaceQueueParentTargets(objects ...*schedulingv1beta1.NamespaceQueue) []parentTarget {
	targets := make([]parentTarget, 0, len(objects))
	seen := make(map[parentTarget]struct{}, len(objects))
	for _, nq := range objects {
		if nq == nil || nq.Namespace == "" || nq.Name == "" {
			continue
		}
		target := parentTarget{
			scope:     namespaceParentScope,
			namespace: nq.Namespace,
			name:      nq.Name,
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	return targets
}

func (c *namespaceQueueController) addQueue(obj interface{}) {
	queue, ok := obj.(*schedulingv1beta1.Queue)
	if !ok {
		klog.Errorf("object is not a Queue: %#v", obj)
		return
	}

	c.enqueueNamespaceQueuesForParent(parentTarget{
		scope: clusterParentScope,
		name:  queue.Name,
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

	// Queue 的 spec 或 status 变化都可能影响 NamespaceQueue 的可用性。
	c.enqueueNamespaceQueuesForParent(parentTarget{
		scope: clusterParentScope,
		name:  newQueue.Name,
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

	c.enqueueNamespaceQueuesForParent(parentTarget{
		scope: clusterParentScope,
		name:  queue.Name,
	})
}

func (c *namespaceQueueController) enqueueNamespaceQueuesForParent(
	parent parentTarget,
) {
	namespaceQueues, err := c.namespaceQueueLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list NamespaceQueues: %v", err)
		return
	}

	visited := map[parentTarget]struct{}{parent: {}}
	parents := []parentTarget{parent}
	for len(parents) > 0 {
		current := parents[0]
		parents = parents[1:]

		for _, nq := range namespaceQueues {
			target, err := resolveParent(nq)
			if err != nil || target != current {
				continue
			}

			c.enqueueNamespaceQueue(nq)
			child := parentTarget{
				scope:     namespaceParentScope,
				namespace: nq.Namespace,
				name:      nq.Name,
			}
			if _, ok := visited[child]; ok {
				continue
			}
			visited[child] = struct{}{}
			parents = append(parents, child)
		}
	}
}
