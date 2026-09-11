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
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	commonutil "volcano.sh/volcano/pkg/util"
)

func resetNamespaceQueueMetrics() {
	NamespaceQueueReady.Reset()
	NamespaceQueueBlocked.Reset()
	queuePodGroupPending.Reset()
	queuePodGroupInqueue.Reset()
	queuePodGroupRunning.Reset()
	queuePodGroupUnknown.Reset()
	queuePodGroupCompleted.Reset()
}

func namespaceQueueFixture() *schedulingv1beta1.NamespaceQueue {
	return &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:  "team-a",
			Name:       "training",
			Generation: 1,
		},
	}
}

func TestUpdateNamespaceQueueMetrics(t *testing.T) {
	resetNamespaceQueueMetrics()
	t.Cleanup(resetNamespaceQueueMetrics)

	namespaceQueue := namespaceQueueFixture()
	status := &schedulingv1beta1.NamespaceQueueStatus{
		State:     schedulingv1beta1.QueueStateOpen,
		Pending:   2,
		Inqueue:   1,
		Running:   3,
		Unknown:   4,
		Completed: 5,
		Conditions: []metav1.Condition{
			{Type: commonutil.NamespaceQueueAuthorizedCondition, Status: metav1.ConditionTrue, ObservedGeneration: 1},
			{Type: commonutil.NamespaceQueueReadyCondition, Status: metav1.ConditionTrue, ObservedGeneration: 1},
		},
	}

	UpdateNamespaceQueueMetrics(namespaceQueue, status)
	queueName := namespaceQueue.Namespace + "/" + namespaceQueue.Name
	if got := testutil.ToFloat64(queuePodGroupPending.WithLabelValues(queueName)); got != 2 {
		t.Fatalf("pending PodGroup metric = %v, want 2", got)
	}
	if got := testutil.ToFloat64(queuePodGroupInqueue.WithLabelValues(queueName)); got != 1 {
		t.Fatalf("inqueue PodGroup metric = %v, want 1", got)
	}
	if got := testutil.ToFloat64(queuePodGroupRunning.WithLabelValues(queueName)); got != 3 {
		t.Fatalf("running PodGroup metric = %v, want 3", got)
	}
	if got := testutil.ToFloat64(queuePodGroupUnknown.WithLabelValues(queueName)); got != 4 {
		t.Fatalf("unknown PodGroup metric = %v, want 4", got)
	}
	if got := testutil.ToFloat64(queuePodGroupCompleted.WithLabelValues(queueName)); got != 5 {
		t.Fatalf("completed PodGroup metric = %v, want 5", got)
	}
	if got := testutil.ToFloat64(NamespaceQueueReady.WithLabelValues(queueName)); got != 1 {
		t.Fatalf("ready metric = %v, want 1", got)
	}

	status.State = schedulingv1beta1.QueueStateClosed
	status.Conditions = []metav1.Condition{
		{Type: commonutil.NamespaceQueueAuthorizedCondition, Status: metav1.ConditionTrue, ObservedGeneration: 1},
		{Type: commonutil.NamespaceQueueReadyCondition, Status: metav1.ConditionFalse, Reason: commonutil.NamespaceQueueReasonQueueClosed, ObservedGeneration: 1},
	}
	UpdateNamespaceQueueMetrics(namespaceQueue, status)
	if got := testutil.ToFloat64(NamespaceQueueReady.WithLabelValues(queueName)); got != 0 {
		t.Fatalf("ready metric after close = %v, want 0", got)
	}
	if got := testutil.ToFloat64(NamespaceQueueBlocked.WithLabelValues(queueName, commonutil.NamespaceQueueReasonQueueClosed)); got != 1 {
		t.Fatalf("blocked metric = %v, want 1", got)
	}

	// Without a Ready condition the reason falls back to the lifecycle state.
	status.State = schedulingv1beta1.QueueStateClosing
	status.Conditions = nil
	UpdateNamespaceQueueMetrics(namespaceQueue, status)
	if got := testutil.ToFloat64(NamespaceQueueReady.WithLabelValues(queueName)); got != 0 {
		t.Fatalf("ready metric after closing = %v, want 0", got)
	}
	if got := testutil.ToFloat64(NamespaceQueueBlocked.WithLabelValues(queueName, commonutil.NamespaceQueueReasonQueueClosing)); got != 1 {
		t.Fatalf("blocked metric after closing = %v, want 1", got)
	}
	// The stale "QueueClosed" series must have been cleaned up.
	if got := testutil.CollectAndCount(NamespaceQueueBlocked); got != 1 {
		t.Fatalf("blocked metric series = %v, want 1", got)
	}
}

func TestDeleteNamespaceQueueMetrics(t *testing.T) {
	resetNamespaceQueueMetrics()
	t.Cleanup(resetNamespaceQueueMetrics)

	namespaceQueue := namespaceQueueFixture()
	UpdateNamespaceQueueMetrics(namespaceQueue, &schedulingv1beta1.NamespaceQueueStatus{
		State: schedulingv1beta1.QueueStateClosed,
		Conditions: []metav1.Condition{{
			Type:   commonutil.NamespaceQueueReadyCondition,
			Status: metav1.ConditionFalse,
			Reason: commonutil.NamespaceQueueReasonQueueClosed,
		}},
	})
	DeleteNamespaceQueueMetrics(namespaceQueue)

	if got := testutil.CollectAndCount(NamespaceQueueReady); got != 0 {
		t.Fatalf("ready metric series after deletion = %v, want 0", got)
	}
	if got := testutil.CollectAndCount(NamespaceQueueBlocked); got != 0 {
		t.Fatalf("blocked metric series after deletion = %v, want 0", got)
	}
	if got := testutil.CollectAndCount(queuePodGroupPending); got != 0 {
		t.Fatalf("pending metric series after deletion = %v, want 0", got)
	}
}
