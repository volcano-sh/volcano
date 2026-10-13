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

package metrics

import schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

// UpdateNamespaceQueueMetrics records NamespaceQueue workload counts.
// namespaceQueue must be non-nil; queue metrics are labeled with the canonical
// namespace/name. Readiness remains available through status conditions.
func UpdateNamespaceQueueMetrics(namespaceQueue *schedulingv1beta1.NamespaceQueue, status *schedulingv1beta1.NamespaceQueueStatus) {
	if namespaceQueue == nil || status == nil {
		return
	}

	queueName := namespaceQueue.Namespace + "/" + namespaceQueue.Name
	UpdateQueuePodGroupPendingCount(queueName, status.Pending)
	UpdateQueuePodGroupRunningCount(queueName, status.Running)
	UpdateQueuePodGroupUnknownCount(queueName, status.Unknown)
	UpdateQueuePodGroupInqueueCount(queueName, status.Inqueue)
	UpdateQueuePodGroupCompletedCount(queueName, status.Completed)
}

// DeleteNamespaceQueueMetrics removes all metrics for a deleted NamespaceQueue.
func DeleteNamespaceQueueMetrics(namespaceQueue *schedulingv1beta1.NamespaceQueue) {
	if namespaceQueue == nil {
		return
	}

	queueName := namespaceQueue.Namespace + "/" + namespaceQueue.Name
	DeleteQueueMetrics(queueName)
}
