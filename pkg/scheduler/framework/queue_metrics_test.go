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

package framework

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	schedmetrics "volcano.sh/volcano/pkg/scheduler/metrics"
)

func TestUpdateQueueStateMetrics(t *testing.T) {
	const (
		parentQueueName = "queue-state-metrics-parent"
		childQueueName  = "queue-state-metrics-child"
	)
	parentQueueID := api.QueueID(parentQueueName)
	childQueueID := api.QueueID(childQueueName)

	for _, queueName := range []string{parentQueueName, childQueueName} {
		schedmetrics.DeleteQueueMetrics(queueName)
		defer schedmetrics.DeleteQueueMetrics(queueName)
	}

	ssn := &Session{
		Queues: map[api.QueueID]*api.QueueInfo{
			parentQueueID: {UID: parentQueueID, Name: parentQueueName},
			childQueueID:  {UID: childQueueID, Name: childQueueName},
		},
		Jobs: map[api.JobID]*api.JobInfo{
			"pending-job": {
				Queue: childQueueID,
				PodGroup: &api.PodGroup{PodGroup: scheduling.PodGroup{
					Status: scheduling.PodGroupStatus{Phase: scheduling.PodGroupPending},
				}},
				TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
					api.Pending: {
						"pending-task-1": nil,
						"pending-task-2": nil,
					},
					api.Allocated: {"allocated-task": nil},
				},
			},
			"running-job": {
				Queue: childQueueID,
				PodGroup: &api.PodGroup{PodGroup: scheduling.PodGroup{
					Status: scheduling.PodGroupStatus{Phase: scheduling.PodGroupRunning},
				}},
				TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
					api.Running: {"running-task": nil},
					api.Failed:  {"failed-task": nil},
				},
			},
		},
	}

	updateQueueStateMetrics(ssn)

	podGroupCounts := map[string]float64{
		"pending": 1, "inqueue": 0, "running": 1, "completed": 0, "unknown": 0,
	}
	for phase, expected := range podGroupCounts {
		requireQueueGaugeValue(t, "volcano_queue_pod_group_count", map[string]string{
			"queue_name": childQueueName,
			"phase":      phase,
		}, expected)
	}
	taskCounts := map[string]float64{
		"pending": 2, "allocated": 1, "pipelined": 0, "binding": 0, "bound": 0,
		"running": 1, "releasing": 0, "succeeded": 0, "failed": 1, "unknown": 0,
	}
	for status, expected := range taskCounts {
		requireQueueGaugeValue(t, "volcano_queue_task_count", map[string]string{
			"queue_name": childQueueName,
			"status":     status,
		}, expected)
	}

	// Jobs in a child queue must not be rolled up into its parent queue.
	requireQueueGaugeValue(t, "volcano_queue_pod_group_count", map[string]string{
		"queue_name": parentQueueName,
		"phase":      "pending",
	}, 0)
	requireQueueGaugeValue(t, "volcano_queue_task_count", map[string]string{
		"queue_name": parentQueueName,
		"status":     "pending",
	}, 0)

	// A later session with no jobs must reset previously non-zero series.
	ssn.Jobs = map[api.JobID]*api.JobInfo{}
	updateQueueStateMetrics(ssn)
	requireQueueGaugeValue(t, "volcano_queue_pod_group_count", map[string]string{
		"queue_name": childQueueName,
		"phase":      "pending",
	}, 0)
	requireQueueGaugeValue(t, "volcano_queue_task_count", map[string]string{
		"queue_name": childQueueName,
		"status":     "pending",
	}, 0)

	// Queue deletion must remove every phase/status series for that queue.
	schedmetrics.DeleteQueueMetrics(childQueueName)
	_, found := queueGaugeValue(t, "volcano_queue_pod_group_count", map[string]string{
		"queue_name": childQueueName,
		"phase":      "pending",
	})
	require.False(t, found)
	_, found = queueGaugeValue(t, "volcano_queue_task_count", map[string]string{
		"queue_name": childQueueName,
		"status":     "pending",
	})
	require.False(t, found)
}

func requireQueueGaugeValue(t *testing.T, metricName string, labels map[string]string, expected float64) {
	t.Helper()
	actual, found := queueGaugeValue(t, metricName, labels)
	require.True(t, found, "metric %s with labels %v was not found", metricName, labels)
	require.Equal(t, expected, actual)
}

func queueGaugeValue(t *testing.T, metricName string, labels map[string]string) (float64, bool) {
	t.Helper()
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	for _, metricFamily := range metricFamilies {
		if metricFamily.GetName() != metricName {
			continue
		}
		for _, metric := range metricFamily.GetMetric() {
			if len(metric.GetLabel()) != len(labels) {
				continue
			}
			matches := true
			for _, label := range metric.GetLabel() {
				expected, found := labels[label.GetName()]
				if !found || expected != label.GetValue() {
					matches = false
					break
				}
			}
			if matches {
				require.NotNil(t, metric.GetGauge())
				return metric.GetGauge().GetValue(), true
			}
		}
	}

	return 0, false
}
