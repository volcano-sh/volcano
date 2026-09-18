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

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

var (
	// NamespaceQueueReady reports whether a NamespaceQueue can accept work.
	NamespaceQueueReady = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: util.VolcanoSubSystemName,
			Name:      "namespacequeue_ready",
			Help:      "Whether a NamespaceQueue is ready for scheduling (1 for ready, 0 otherwise).",
		},
		[]string{"queue_name"},
	)
	// NamespaceQueueBlocked reports why a NamespaceQueue is not ready. reason
	// is the Ready condition's reason when reported, otherwise the lifecycle
	// state (QueueClosing, QueueClosed, or NotReady).
	NamespaceQueueBlocked = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: util.VolcanoSubSystemName,
			Name:      "namespacequeue_blocked",
			Help:      "Whether a NamespaceQueue is blocked, by reason (1 when blocked).",
		},
		[]string{"queue_name", "reason"},
	)
)

const namespaceQueueNotReadyReason = "NotReady"

// UpdateNamespaceQueueMetrics records user-facing NamespaceQueue state and
// workload counts. namespaceQueue must be non-nil; queue metrics are labeled
// with the canonical namespace/name.
func UpdateNamespaceQueueMetrics(namespaceQueue *schedulingv1beta1.NamespaceQueue, status *schedulingv1beta1.NamespaceQueueStatus) {
	if namespaceQueue == nil || status == nil {
		return
	}

	queueName := namespaceQueue.Namespace + "/" + namespaceQueue.Name
	UpdateQueuePodGroupCounts(
		queueName,
		status.Inqueue,
		status.Pending,
		status.Running,
		status.Unknown,
		status.Completed,
	)

	// Clear any stale blocked series before writing the current one; the
	// reason label changes when the underlying condition does.
	NamespaceQueueBlocked.DeletePartialMatch(prometheus.Labels{"queue_name": queueName})

	schedulable := commonutil.IsNamespaceQueueSchedulable(
		namespaceQueue.Generation,
		string(status.State),
		status.Conditions,
	)
	if schedulable {
		NamespaceQueueReady.WithLabelValues(queueName).Set(1)
		return
	}

	NamespaceQueueReady.WithLabelValues(queueName).Set(0)
	NamespaceQueueBlocked.WithLabelValues(queueName, namespaceQueueBlockReason(status)).Set(1)
}

// DeleteNamespaceQueueMetrics removes all metrics for a deleted NamespaceQueue.
func DeleteNamespaceQueueMetrics(namespaceQueue *schedulingv1beta1.NamespaceQueue) {
	if namespaceQueue == nil {
		return
	}

	queueName := namespaceQueue.Namespace + "/" + namespaceQueue.Name
	DeleteQueueMetrics(queueName)
	NamespaceQueueReady.DeleteLabelValues(queueName)
	NamespaceQueueBlocked.DeletePartialMatch(prometheus.Labels{"queue_name": queueName})
}

func namespaceQueueBlockReason(status *schedulingv1beta1.NamespaceQueueStatus) string {
	if condition := apiMeta.FindStatusCondition(status.Conditions, commonutil.NamespaceQueueReadyCondition); condition != nil && condition.Reason != "" {
		return condition.Reason
	}

	switch status.State {
	case schedulingv1beta1.QueueStateClosing:
		return commonutil.NamespaceQueueReasonQueueClosing
	case schedulingv1beta1.QueueStateClosed:
		return commonutil.NamespaceQueueReasonQueueClosed
	default:
		return namespaceQueueNotReadyReason
	}
}
