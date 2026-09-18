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
	v1 "k8s.io/api/core/v1"
)

func TestQueueNodeGroupAllocatedMetricsLifecycle(t *testing.T) {
	resetQueueNodeGroupAllocatedMetricsForTest()
	t.Cleanup(resetQueueNodeGroupAllocatedMetricsForTest)

	const (
		queue1 = "queue-nodegroup-metrics-1"
		queue2 = "queue-nodegroup-metrics-2"
		group1 = "group-1"
		group2 = "group-2"
	)
	gpu := v1.ResourceName("nvidia.com/gpu")

	SyncQueueNodeGroupAllocated([]string{queue1, queue2}, []QueueNodeGroupResource{
		{
			QueueName:       queue1,
			NodeGroupName:   group1,
			MilliCPU:        1000,
			Memory:          1024,
			ScalarResources: map[v1.ResourceName]float64{gpu: 1},
		},
		{QueueName: queue1, NodeGroupName: group2, MilliCPU: 2000, Memory: 2048},
		{QueueName: queue2, NodeGroupName: group1, MilliCPU: 3000, Memory: 3072},
	})

	if got := testutil.ToFloat64(queueNodeGroupAllocatedMilliCPU.WithLabelValues(queue1, group1)); got != 1000 {
		t.Fatalf("expected initial CPU to be 1000, got %v", got)
	}
	if got := testutil.ToFloat64(queueNodeGroupAllocatedMemory.WithLabelValues(queue1, group2)); got != 2048 {
		t.Fatalf("expected initial memory to be 2048, got %v", got)
	}
	if got := testutil.ToFloat64(queueNodeGroupAllocatedScalarResource.WithLabelValues(queue1, group1, string(gpu))); got != 1 {
		t.Fatalf("expected initial GPU to be 1, got %v", got)
	}

	// A later session keeps known pairs for existing queues and zeros omitted values.
	SyncQueueNodeGroupAllocated([]string{queue1, queue2}, []QueueNodeGroupResource{
		{QueueName: queue1, NodeGroupName: group1, MilliCPU: 1600, Memory: 1636},
		{QueueName: queue2, NodeGroupName: group1, MilliCPU: 3000, Memory: 3072},
	})
	if got := testutil.ToFloat64(queueNodeGroupAllocatedScalarResource.WithLabelValues(queue1, group1, string(gpu))); got != 0 {
		t.Fatalf("expected removed GPU dimension to be reset to zero, got %v", got)
	}
	if got := testutil.ToFloat64(queueNodeGroupAllocatedMilliCPU.WithLabelValues(queue1, group2)); got != 0 {
		t.Fatalf("expected omitted nodegroup pair to be reset to zero, got %v", got)
	}
	if got := testutil.CollectAndCount(queueNodeGroupAllocatedMilliCPU); got != 3 {
		t.Fatalf("expected all three known CPU series to be retained, got %d", got)
	}

	// A later session removes metrics for queues that no longer exist.
	SyncQueueNodeGroupAllocated([]string{queue1}, []QueueNodeGroupResource{
		{QueueName: queue1, NodeGroupName: group1, MilliCPU: 1600, Memory: 1636},
	})
	if got := testutil.CollectAndCount(queueNodeGroupAllocatedMilliCPU); got != 2 {
		t.Fatalf("expected only queue1 CPU series after deleting queue2, got %d", got)
	}

	DeleteQueueMetrics(queue1)
	if got := testutil.CollectAndCount(queueNodeGroupAllocatedMilliCPU); got != 0 {
		t.Fatalf("expected all CPU series to be deleted, got %d", got)
	}
	if got := testutil.CollectAndCount(queueNodeGroupAllocatedMemory); got != 0 {
		t.Fatalf("expected all memory series to be deleted, got %d", got)
	}
	if got := testutil.CollectAndCount(queueNodeGroupAllocatedScalarResource); got != 0 {
		t.Fatalf("expected all scalar series to be deleted, got %d", got)
	}
}

func resetQueueNodeGroupAllocatedMetricsForTest() {
	queueNodeGroupAllocatedMetricsLock.Lock()
	defer queueNodeGroupAllocatedMetricsLock.Unlock()
	queueNodeGroupAllocatedMilliCPU.Reset()
	queueNodeGroupAllocatedMemory.Reset()
	queueNodeGroupAllocatedScalarResource.Reset()
	knownQueueNodeGroupScalarResources = make(map[queueNodeGroupKey]map[string]struct{})
}
