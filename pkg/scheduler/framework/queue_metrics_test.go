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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	schedmetrics "volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestOpenSessionUpdatesQueueTaskMetrics(t *testing.T) {
	const (
		parentQueueName = "queue-state-metrics-parent"
		childQueueName  = "queue-state-metrics-child"
	)
	for _, queueName := range []string{parentQueueName, childQueueName} {
		schedmetrics.DeleteQueueMetrics(queueName)
		defer schedmetrics.DeleteQueueMetrics(queueName)
	}

	schedulerCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
	defer schedulerCache.OnSessionClose()
	for _, queueName := range []string{parentQueueName, childQueueName} {
		queue := api.NewQueueInfo(&scheduling.Queue{ObjectMeta: metav1.ObjectMeta{Name: queueName}})
		schedulerCache.Queues[queue.UID] = queue
	}

	newTask := func(name string, status api.TaskStatus) *api.TaskInfo {
		pod := util.BuildPod("test", name, "", v1.PodPending, nil, "test-job", nil, nil)
		task := api.NewTaskInfo(pod)
		task.Status = status
		return task
	}
	job := api.NewJobInfo(
		"test/test-job",
		newTask("pending-task-1", api.Pending),
		newTask("pending-task-2", api.Pending),
		newTask("allocated-task", api.Allocated),
		newTask("running-task", api.Running),
		newTask("failed-task", api.Failed),
		newTask("unexpected-status-task", api.Pending|api.Running),
	)
	job.Queue = api.QueueID(childQueueName)
	job.PodGroup = &api.PodGroup{PodGroup: scheduling.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "test-job", Namespace: "test"},
		Spec:       scheduling.PodGroupSpec{Queue: childQueueName},
	}}
	schedulerCache.Jobs[job.UID] = job

	OpenSession(schedulerCache, nil, nil)

	_, found := queueGaugeValue(t, "volcano_queue_pod_group_count", map[string]string{
		"queue_name": childQueueName,
		"phase":      "pending",
	})
	require.False(t, found, "the controller-manager metrics already cover PodGroup counts")

	taskCounts := map[string]float64{
		"pending": 2, "allocated": 1, "pipelined": 0, "binding": 0, "bound": 0,
		"running": 1, "releasing": 0, "succeeded": 0, "failed": 1, "unknown": 1,
	}
	for status, expected := range taskCounts {
		requireQueueGaugeValue(t, "volcano_queue_session_start_task_count", map[string]string{
			"queue_name": childQueueName,
			"status":     status,
		}, expected)
	}

	// Jobs in a child queue must not be rolled up into its parent queue.
	requireQueueGaugeValue(t, "volcano_queue_session_start_task_count", map[string]string{
		"queue_name": parentQueueName,
		"status":     "pending",
	}, 0)

	// A later session with no jobs must reset previously non-zero series.
	schedulerCache.OnSessionClose()
	delete(schedulerCache.Jobs, job.UID)
	OpenSession(schedulerCache, nil, nil)
	requireQueueGaugeValue(t, "volcano_queue_session_start_task_count", map[string]string{
		"queue_name": childQueueName,
		"status":     "pending",
	}, 0)

	// Queue deletion must remove every status series for that queue.
	schedmetrics.DeleteQueueMetrics(childQueueName)
	_, found = queueGaugeValue(t, "volcano_queue_session_start_task_count", map[string]string{
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
