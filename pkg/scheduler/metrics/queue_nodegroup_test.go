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
	"fmt"
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

	SyncQueueNodeGroupAllocated([]QueueNodeGroupResource{
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

	// A later session removes omitted pairs and scalar resource names.
	SyncQueueNodeGroupAllocated([]QueueNodeGroupResource{
		{QueueName: queue1, NodeGroupName: group1, MilliCPU: 1600, Memory: 1636},
		{QueueName: queue2, NodeGroupName: group1, MilliCPU: 3000, Memory: 3072},
	})
	if got := testutil.CollectAndCount(queueNodeGroupAllocatedScalarResource); got != 0 {
		t.Fatalf("expected removed GPU series to be deleted, got %d", got)
	}
	if got := testutil.ToFloat64(queueNodeGroupAllocatedMilliCPU.WithLabelValues(queue1, group1)); got != 1600 {
		t.Fatalf("expected updated CPU to be 1600, got %v", got)
	}
	if got := testutil.CollectAndCount(queueNodeGroupAllocatedMilliCPU); got != 2 {
		t.Fatalf("expected only two active CPU series, got %d", got)
	}
	if got := testutil.CollectAndCount(queueNodeGroupAllocatedMemory); got != 2 {
		t.Fatalf("expected only two active memory series, got %d", got)
	}

	// A later session removes metrics for queues that no longer exist.
	SyncQueueNodeGroupAllocated([]QueueNodeGroupResource{
		{QueueName: queue1, NodeGroupName: group1, MilliCPU: 1600, Memory: 1636},
	})
	if got := testutil.CollectAndCount(queueNodeGroupAllocatedMilliCPU); got != 1 {
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

func TestQueueNodeGroupAllocatedScalarChanges(t *testing.T) {
	resetQueueNodeGroupAllocatedMetricsForTest()
	t.Cleanup(resetQueueNodeGroupAllocatedMetricsForTest)

	const queue = "queue-nodegroup-scalar-changes"
	gpu := v1.ResourceName("nvidia.com/gpu")
	fpga := v1.ResourceName("example.com/fpga")
	// The caller owns this map and may change it after publishing a snapshot.
	scalarResources := map[v1.ResourceName]float64{}
	resource := QueueNodeGroupResource{QueueName: queue, NodeGroupName: "group", ScalarResources: scalarResources}
	tests := []struct {
		name      string
		resources map[v1.ResourceName]float64
	}{
		{name: "initial", resources: map[v1.ResourceName]float64{gpu: 1}},
		{name: "unchanged", resources: map[v1.ResourceName]float64{gpu: 1}},
		{name: "changed", resources: map[v1.ResourceName]float64{gpu: 2, fpga: 1}},
		{name: "removed", resources: map[v1.ResourceName]float64{fpga: 1}},
		{name: "empty"},
		{name: "reintroduced zero", resources: map[v1.ResourceName]float64{gpu: 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clear(scalarResources)
			for name, value := range tt.resources {
				scalarResources[name] = value
			}
			SyncQueueNodeGroupAllocated([]QueueNodeGroupResource{resource})
			if got := testutil.CollectAndCount(queueNodeGroupAllocatedScalarResource); got != len(tt.resources) {
				t.Fatalf("expected %d scalar series, got %d", len(tt.resources), got)
			}
			for name, want := range tt.resources {
				if got := testutil.ToFloat64(queueNodeGroupAllocatedScalarResource.WithLabelValues(queue, "group", string(name))); got != want {
					t.Errorf("expected %s to be %v, got %v", name, want, got)
				}
			}
		})
	}

	SyncQueueNodeGroupAllocated(nil)
	if got := testutil.CollectAndCount(queueNodeGroupAllocatedScalarResource); got != 0 {
		t.Fatalf("expected an empty snapshot to delete scalar series, got %d", got)
	}
	SyncQueueNodeGroupAllocated([]QueueNodeGroupResource{resource})
	if got := testutil.CollectAndCount(queueNodeGroupAllocatedScalarResource); got != 1 {
		t.Fatalf("expected the reintroduced pair to export its scalar series, got %d", got)
	}
}

func BenchmarkSyncQueueNodeGroupAllocatedUnchanged(b *testing.B) {
	resetQueueNodeGroupAllocatedMetricsForTest()
	b.Cleanup(resetQueueNodeGroupAllocatedMetricsForTest)
	resources := make([]QueueNodeGroupResource, 1000)
	for i := range resources {
		resources[i] = QueueNodeGroupResource{
			QueueName: "benchmark-queue", NodeGroupName: fmt.Sprintf("group-%d", i),
			MilliCPU: 1000, Memory: 1024,
			ScalarResources: map[v1.ResourceName]float64{"nvidia.com/gpu": 1},
		}
	}
	SyncQueueNodeGroupAllocated(resources)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SyncQueueNodeGroupAllocated(resources)
	}
}

func resetQueueNodeGroupAllocatedMetricsForTest() {
	queueNodeGroupAllocatedMetricsLock.Lock()
	defer queueNodeGroupAllocatedMetricsLock.Unlock()
	queueNodeGroupAllocatedMilliCPU.Reset()
	queueNodeGroupAllocatedMemory.Reset()
	queueNodeGroupAllocatedScalarResource.Reset()
	queueNodeGroupAllocatedResources = make(map[queueNodeGroupKey]*queueNodeGroupAllocation)
}
