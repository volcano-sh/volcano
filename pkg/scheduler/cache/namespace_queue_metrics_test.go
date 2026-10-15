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

package cache

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

func TestSchedulerCache_DeleteNamespaceQueueRemovesMetrics(t *testing.T) {
	const queueName = "ns-queue-delete-metrics"
	namespaceQueueID := string(api.NamespaceQueueID("team-a", queueName))
	for _, name := range []string{queueName, namespaceQueueID} {
		metrics.DeleteQueueMetrics(name)
		defer metrics.DeleteQueueMetrics(name)
	}

	sc := &SchedulerCache{Queues: make(map[api.QueueID]*api.QueueInfo)}
	namespaceQueue := &schedulingv1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: queueName},
		Spec:       schedulingv1.NamespaceQueueSpec{Parent: "cluster/" + queueName},
	}
	sc.AddQueueV1beta1(&schedulingv1.Queue{ObjectMeta: metav1.ObjectMeta{Name: queueName}})
	sc.AddNamespaceQueueV1beta1(namespaceQueue)
	for _, name := range []string{queueName, namespaceQueueID} {
		metrics.UpdateQueueTaskCounts(name, map[string]int{"pending": 1})
		metrics.UpdateQueueWeight(name, 1)
	}

	sc.DeleteNamespaceQueueV1beta1(namespaceQueue)

	for _, metricName := range []string{"volcano_queue_session_start_task_count", "volcano_queue_weight"} {
		require.False(t, hasQueueSeries(t, metricName, namespaceQueueID),
			"%s series for deleted NamespaceQueue %s remained", metricName, namespaceQueueID)
		require.True(t, hasQueueSeries(t, metricName, queueName),
			"%s series for cluster Queue %s was removed", metricName, queueName)
	}
}

func hasQueueSeries(t *testing.T, metricName, queueName string) bool {
	t.Helper()
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, metricFamily := range metricFamilies {
		if metricFamily.GetName() != metricName {
			continue
		}
		for _, metric := range metricFamily.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "queue_name" && label.GetValue() == queueName {
					return true
				}
			}
		}
	}
	return false
}
