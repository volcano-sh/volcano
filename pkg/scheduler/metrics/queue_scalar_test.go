package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	v1 "k8s.io/api/core/v1"
)

func TestUpdateScalarResourceMetrics_ZeroAndCleanup(t *testing.T) {
	queueName := "testqueue"
	resourceA := v1.ResourceName("nvidia.com/gpu")
	resourceB := v1.ResourceName("amd.com/gpu")

	// 1. Set resourceA to 5, resourceB to 10
	UpdateQueueAllocated(queueName, 0, 0, map[v1.ResourceName]float64{resourceA: 5, resourceB: 10})
	if got := testutil.ToFloat64(queueAllocatedScalarResource.WithLabelValues(queueName, string(resourceA))); got != 5 {
		t.Errorf("expected %s to be 5, got %v", resourceA, got)
	}
	if got := testutil.ToFloat64(queueAllocatedScalarResource.WithLabelValues(queueName, string(resourceB))); got != 10 {
		t.Errorf("expected %s to be 10, got %v", resourceB, got)
	}

	// 2. Update with only resourceA, resourceB should be set to zero
	UpdateQueueAllocated(queueName, 0, 0, map[v1.ResourceName]float64{resourceA: 3})
	if got := testutil.ToFloat64(queueAllocatedScalarResource.WithLabelValues(queueName, string(resourceA))); got != 3 {
		t.Errorf("expected %s to be 3, got %v", resourceA, got)
	}
	if got := testutil.ToFloat64(queueAllocatedScalarResource.WithLabelValues(queueName, string(resourceB))); got != 0 {
		t.Errorf("expected %s to be 0 after missing in update, got %v", resourceB, got)
	}

	// 3. Update with nil scalarResources, all known resources should be set to zero
	UpdateQueueAllocated(queueName, 0, 0, nil)
	if got := testutil.ToFloat64(queueAllocatedScalarResource.WithLabelValues(queueName, string(resourceA))); got != 0 {
		t.Errorf("expected %s to be 0 after nil update, got %v", resourceA, got)
	}
	if got := testutil.ToFloat64(queueAllocatedScalarResource.WithLabelValues(queueName, string(resourceB))); got != 0 {
		t.Errorf("expected %s to be 0 after nil update, got %v", resourceB, got)
	}

	// 4. Delete metrics and ensure they're gone
	DeleteQueueMetrics(queueName)
	if count := testutil.CollectAndCount(queueAllocatedScalarResource); count != 0 {
		t.Errorf("expected no metrics for queueAllocatedScalarResource after delete, got %d", count)
	}
}
