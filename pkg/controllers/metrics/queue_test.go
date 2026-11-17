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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// queueGauges names the PodGroup count gauges of a queue for failure messages.
var queueGauges = map[string]*prometheus.GaugeVec{
	"inqueue":   queuePodGroupInqueue,
	"pending":   queuePodGroupPending,
	"running":   queuePodGroupRunning,
	"unknown":   queuePodGroupUnknown,
	"completed": queuePodGroupCompleted,
}

func TestUpdateQueueMetrics(t *testing.T) {
	tests := []struct {
		name     string
		statuses []*schedulingv1beta1.QueueStatus
		want     map[string]float64
	}{
		{
			name: "sets every PodGroup count from the status",
			statuses: []*schedulingv1beta1.QueueStatus{
				{Inqueue: 1, Pending: 2, Running: 3, Unknown: 4, Completed: 5},
			},
			want: map[string]float64{"inqueue": 1, "pending": 2, "running": 3, "unknown": 4, "completed": 5},
		},
		{
			name: "replaces the counts of an earlier status",
			statuses: []*schedulingv1beta1.QueueStatus{
				{Inqueue: 1, Pending: 2, Running: 3, Unknown: 4, Completed: 5},
				{Inqueue: 0, Pending: 6, Running: 1, Unknown: 0, Completed: 7},
			},
			want: map[string]float64{"inqueue": 0, "pending": 6, "running": 1, "unknown": 0, "completed": 7},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetNamespaceQueueMetrics()
			t.Cleanup(resetNamespaceQueueMetrics)

			for _, status := range tt.statuses {
				UpdateQueueMetrics("default", status)
			}

			for name, gauge := range queueGauges {
				assert.Equal(t, tt.want[name], testutil.ToFloat64(gauge.WithLabelValues("default")), "%s PodGroup metric", name)
			}
		})
	}
}

func TestDeleteQueueMetrics(t *testing.T) {
	resetNamespaceQueueMetrics()
	t.Cleanup(resetNamespaceQueueMetrics)

	UpdateQueueMetrics("deleted", &schedulingv1beta1.QueueStatus{Inqueue: 1, Pending: 1, Running: 1, Unknown: 1, Completed: 1})
	UpdateQueueMetrics("kept", &schedulingv1beta1.QueueStatus{Inqueue: 2, Pending: 2, Running: 2, Unknown: 2, Completed: 2})

	DeleteQueueMetrics("deleted")

	for name, gauge := range queueGauges {
		assert.Equal(t, 1, testutil.CollectAndCount(gauge), "%s metric series after deletion", name)
		assert.Equal(t, float64(2), testutil.ToFloat64(gauge.WithLabelValues("kept")), "%s PodGroup metric of the kept queue", name)
	}
}
