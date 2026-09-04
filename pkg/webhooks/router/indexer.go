/*
Copyright 2025 The Volcano Authors.

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

package router

import (
	"fmt"

	"k8s.io/apimachinery/pkg/labels"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	commonutil "volcano.sh/volcano/pkg/util"
)

const (
	// QueueParentIndexName is the name of the index for parent queue lookup
	QueueParentIndexName = "queueParent"
)

// QueueParentIndexFunc is an index function that indexes queues by their parent name
// This allows efficient lookup of all children of a given parent queue
func QueueParentIndexFunc(obj interface{}) ([]string, error) {
	queue, ok := obj.(*schedulingv1beta1.Queue)
	if !ok {
		return []string{}, nil
	}

	// Index by parent name
	if queue.Spec.Parent != "" {
		return []string{queue.Spec.Parent}, nil
	}

	// Root queue or queues without parent
	return []string{}, nil
}

// GetQueuesByParent returns all queues that have the specified parent using the queue parent index.
// This method leverages the informer's indexer for efficient lookups when available,
// falling back to listing all queues if the informer is not initialized (e.g., in tests).
func (asc *AdmissionServiceConfig) GetQueuesByParent(parentName string) ([]*schedulingv1beta1.Queue, error) {
	// If informer is available, use the efficient index-based lookup
	if asc.QueueInformer != nil {
		objs, err := asc.QueueInformer.GetIndexer().ByIndex(QueueParentIndexName, parentName)
		if err != nil {
			return nil, fmt.Errorf("failed to query index %s for parent %s: %v", QueueParentIndexName, parentName, err)
		}

		queues := make([]*schedulingv1beta1.Queue, 0, len(objs))
		for _, obj := range objs {
			queue, ok := obj.(*schedulingv1beta1.Queue)
			if !ok {
				continue
			}
			queues = append(queues, queue)
		}

		return queues, nil
	}

	// Fallback: list all queues and filter (for backward compatibility and tests)
	// This is less efficient but ensures functionality when informer is not available
	allQueues, err := asc.QueueLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list queues: %v", err)
	}

	queues := make([]*schedulingv1beta1.Queue, 0)
	for _, queue := range allQueues {
		if queue.Spec.Parent == parentName {
			queues = append(queues, queue)
		}
	}

	return queues, nil
}

// GetNamespaceQueuesByParent returns NamespaceQueues attached to parent. It
// uses the informer index when available and retains a lister fallback for
// tests and legacy construction paths.
func (asc *AdmissionServiceConfig) GetNamespaceQueuesByParent(
	parentRef ResolvedQueueReference,
	namespace string,
) ([]*schedulingv1beta1.NamespaceQueue, error) {
	if asc == nil {
		return nil, fmt.Errorf("admission service config is nil")
	}
	if asc.NamespaceQueueInformer != nil {
		objects, err := asc.NamespaceQueueInformer.GetIndexer().ByIndex(
			NamespaceQueueParentIndexName,
			NamespaceQueueParentIndexKey(parentRef),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to query NamespaceQueue parent index: %w", err)
		}

		queues := make([]*schedulingv1beta1.NamespaceQueue, 0, len(objects))
		for _, object := range objects {
			queue, ok := object.(*schedulingv1beta1.NamespaceQueue)
			if ok && (namespace == "" || queue.Namespace == namespace) {
				queues = append(queues, queue)
			}
		}
		return queues, nil
	}
	if asc.NamespaceQueueLister == nil {
		return nil, fmt.Errorf("NamespaceQueue lister is not configured")
	}

	var queues []*schedulingv1beta1.NamespaceQueue
	var err error
	if namespace == "" {
		queues, err = asc.NamespaceQueueLister.List(labels.Everything())
	} else {
		queues, err = asc.NamespaceQueueLister.NamespaceQueues(namespace).List(labels.Everything())
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list NamespaceQueues: %w", err)
	}

	matched := make([]*schedulingv1beta1.NamespaceQueue, 0, len(queues))
	for _, queue := range queues {
		resolvedParentRef, err := commonutil.ResolveNamespaceQueueParentReference(queue.Namespace, queue.Spec.Parent)
		if err == nil && resolvedParentRef == parentRef {
			matched = append(matched, queue)
		}
	}
	return matched, nil
}

// GetNamespaceQueueDescendants returns the NamespaceQueue subtree below a
// cluster Queue. Indexed traversal visits only affected descendants; the
// lister fallback builds one adjacency map to avoid repeated full scans.
func (asc *AdmissionServiceConfig) GetNamespaceQueueDescendants(
	clusterQueueName string,
) ([]*schedulingv1beta1.NamespaceQueue, error) {
	if clusterQueueName == "" {
		return nil, fmt.Errorf("cluster Queue name is empty")
	}
	root := ResolvedQueueReference{
		Scope: commonutil.ClusterQueueReferenceScope,
		Name:  clusterQueueName,
	}

	if asc == nil {
		return nil, fmt.Errorf("admission service config is nil")
	}

	getChildQueues := func(parentRef ResolvedQueueReference) ([]*schedulingv1beta1.NamespaceQueue, error) {
		return asc.GetNamespaceQueuesByParent(parentRef, "")
	}
	if asc.NamespaceQueueInformer == nil {
		childQueuesByParent, err := asc.namespaceQueueChildrenIndex()
		if err != nil {
			return nil, err
		}
		getChildQueues = func(parentRef ResolvedQueueReference) ([]*schedulingv1beta1.NamespaceQueue, error) {
			return childQueuesByParent[NamespaceQueueParentIndexKey(parentRef)], nil
		}
	}
	return walkNamespaceQueueDescendants(root, getChildQueues)
}

func (asc *AdmissionServiceConfig) namespaceQueueChildrenIndex() (
	map[string][]*schedulingv1beta1.NamespaceQueue,
	error,
) {
	if asc.NamespaceQueueLister == nil {
		return nil, fmt.Errorf("NamespaceQueue lister is not configured")
	}
	queues, err := asc.NamespaceQueueLister.List(labels.Everything())
	if err != nil {
		return nil, fmt.Errorf("failed to list NamespaceQueues: %w", err)
	}

	childrenByParent := make(map[string][]*schedulingv1beta1.NamespaceQueue, len(queues))
	for _, queue := range queues {
		parentRef, err := commonutil.ResolveNamespaceQueueParentReference(queue.Namespace, queue.Spec.Parent)
		if err != nil {
			continue
		}
		key := NamespaceQueueParentIndexKey(parentRef)
		childrenByParent[key] = append(childrenByParent[key], queue)
	}

	return childrenByParent, nil
}

func walkNamespaceQueueDescendants(
	root ResolvedQueueReference,
	getChildQueues func(ResolvedQueueReference) ([]*schedulingv1beta1.NamespaceQueue, error),
) ([]*schedulingv1beta1.NamespaceQueue, error) {
	descendantQueues := make([]*schedulingv1beta1.NamespaceQueue, 0)
	frontier := []ResolvedQueueReference{root}
	visitedQueueRefs := make(map[string]struct{})
	for len(frontier) > 0 {
		currentParentRef := frontier[0]
		frontier = frontier[1:]
		directChildQueues, err := getChildQueues(currentParentRef)
		if err != nil {
			return nil, err
		}
		for _, childQueue := range directChildQueues {
			childQueueKey := childQueue.Namespace + "/" + childQueue.Name
			if _, found := visitedQueueRefs[childQueueKey]; found {
				return nil, fmt.Errorf("NamespaceQueue hierarchy contains a cycle at %q", childQueueKey)
			}
			visitedQueueRefs[childQueueKey] = struct{}{}
			descendantQueues = append(descendantQueues, childQueue)
			frontier = append(frontier, ResolvedQueueReference{
				Scope:     commonutil.NamespaceQueueReferenceScope,
				Namespace: childQueue.Namespace,
				Name:      childQueue.Name,
			})
		}
	}
	return descendantQueues, nil
}
