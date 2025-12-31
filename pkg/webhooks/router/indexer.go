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
