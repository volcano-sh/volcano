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

package usage

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestNewShadowLoadCache(t *testing.T) {
	cache := NewShadowLoadCache()
	if cache == nil {
		t.Fatal("NewShadowLoadCache() returned nil")
	}
	if !cache.IsClean() {
		t.Error("New cache should be clean")
	}
}

func TestShadowLoadCache_AddEstimate(t *testing.T) {
	cache := NewShadowLoadCache()

	// Add first pod estimate
	cache.AddEstimate("node1", "task1", 1000, 2048)
	cpuEst, memEst := cache.GetNodeEst("node1")
	if math.Abs(cpuEst-1000) > testEps {
		t.Errorf("Expected cpuEst=1000, got %v", cpuEst)
	}
	if math.Abs(memEst-2048) > testEps {
		t.Errorf("Expected memEst=2048, got %v", memEst)
	}

	// Add second pod estimate to same node
	cache.AddEstimate("node1", "task2", 500, 1024)
	cpuEst, memEst = cache.GetNodeEst("node1")
	if math.Abs(cpuEst-1500) > testEps {
		t.Errorf("Expected cpuEst=1500, got %v", cpuEst)
	}
	if math.Abs(memEst-3072) > testEps {
		t.Errorf("Expected memEst=3072, got %v", memEst)
	}

	// Add third pod estimate
	cache.AddEstimate("node1", "task3", 800, 1600)
	cpuEst, memEst = cache.GetNodeEst("node1")
	if math.Abs(cpuEst-2300) > testEps {
		t.Errorf("Expected cpuEst=2300, got %v", cpuEst)
	}
	if math.Abs(memEst-4672) > testEps {
		t.Errorf("Expected memEst=4672, got %v", memEst)
	}

	// Verify snapshots were recorded
	snap := cache.GetSnapshot("task1")
	if snap == nil || snap.CPUMillis != 1000 || snap.MemBytes != 2048 {
		t.Errorf("Snapshot for task1 incorrect: %+v", snap)
	}
	snap3 := cache.GetSnapshot("task3")
	if snap3 == nil || snap3.CPUMillis != 800 || snap3.MemBytes != 1600 {
		t.Errorf("Snapshot for task3 incorrect: %+v", snap3)
	}
}

func TestShadowLoadCache_AddEstimate_UpdateExistingTask(t *testing.T) {
	cache := NewShadowLoadCache()

	cache.AddEstimate("node1", "task1", 1000, 2048)
	cache.AddEstimate("node2", "task1", 500, 1024)

	cpuEst, memEst := cache.GetNodeEst("node1")
	if math.Abs(cpuEst) > testEps || math.Abs(memEst) > testEps {
		t.Errorf("Expected node1 estimate to be cleared after task update, got (%v, %v)", cpuEst, memEst)
	}

	cpuEst, memEst = cache.GetNodeEst("node2")
	if math.Abs(cpuEst-500) > testEps || math.Abs(memEst-1024) > testEps {
		t.Errorf("Expected node2 estimate to be updated to (500, 1024), got (%v, %v)", cpuEst, memEst)
	}

	cache.SubEstimateBySnapshot("task1")
	cpuEst, memEst = cache.GetNodeEst("node2")
	if math.Abs(cpuEst) > testEps || math.Abs(memEst) > testEps {
		t.Errorf("Expected node2 estimate to be cleared after subtract, got (%v, %v)", cpuEst, memEst)
	}
}

func TestShadowLoadCache_SubEstimateBySnapshot(t *testing.T) {
	cache := NewShadowLoadCache()

	// Add then subtract using snapshot
	cache.AddEstimate("node1", "task1", 1000, 2048)
	cache.AddEstimate("node1", "task2", 500, 1024)
	cache.SubEstimateBySnapshot("task2")

	cpuEst, memEst := cache.GetNodeEst("node1")
	if math.Abs(cpuEst-1000) > testEps {
		t.Errorf("Expected cpuEst=1000 after sub, got %v", cpuEst)
	}
	if math.Abs(memEst-2048) > testEps {
		t.Errorf("Expected memEst=2048 after sub, got %v", memEst)
	}

	// Subtract remaining pod
	cache.SubEstimateBySnapshot("task1")
	cpuEst, memEst = cache.GetNodeEst("node1")
	if cpuEst != 0 || memEst != 0 {
		t.Errorf("Expected (0, 0) after all subs, got (%v, %v)", cpuEst, memEst)
	}
	// Snapshot should be deleted after sub
	if cache.GetSnapshot("task1") != nil {
		t.Error("Snapshot for task1 should be nil after SubEstimateBySnapshot")
	}
}

func TestShadowLoadCache_SubEstimateBySnapshot_NonExistent(t *testing.T) {
	cache := NewShadowLoadCache()

	// Subtracting a non-existent task should be a no-op
	cache.AddEstimate("node1", "task1", 100, 200)
	cache.SubEstimateBySnapshot("nonexistent")

	cpuEst, memEst := cache.GetNodeEst("node1")
	if math.Abs(cpuEst-100) > testEps || math.Abs(memEst-200) > testEps {
		t.Errorf("Non-existent sub should be no-op, got (%v, %v)", cpuEst, memEst)
	}
}

func TestShadowLoadCache_SnapshotConsistency(t *testing.T) {
	// This test verifies that deallocation subtracts the exact values
	// recorded at allocation time.
	cache := NewShadowLoadCache()

	cache.AddEstimate("node1", "pod1", 800, 1600)
	cache.AddEstimate("node1", "pod2", 960, 1920)
	cache.AddEstimate("node1", "pod3", 1152, 2304)

	totalCPU := 800.0 + 960.0 + 1152.0
	totalMem := 1600.0 + 1920.0 + 2304.0

	cpuEst, memEst := cache.GetNodeEst("node1")
	if math.Abs(cpuEst-totalCPU) > testEps {
		t.Errorf("Expected cpuEst=%v, got %v", totalCPU, cpuEst)
	}
	if math.Abs(memEst-totalMem) > testEps {
		t.Errorf("Expected memEst=%v, got %v", totalMem, memEst)
	}

	// Deallocate in reverse order - each should subtract exactly what was added
	cache.SubEstimateBySnapshot("pod3")
	cpuEst, memEst = cache.GetNodeEst("node1")
	expectedCPU := 800.0 + 960.0
	expectedMem := 1600.0 + 1920.0
	if math.Abs(cpuEst-expectedCPU) > testEps {
		t.Errorf("After removing pod3: expected cpuEst=%v, got %v", expectedCPU, cpuEst)
	}
	if math.Abs(memEst-expectedMem) > testEps {
		t.Errorf("After removing pod3: expected memEst=%v, got %v", expectedMem, memEst)
	}

	cache.SubEstimateBySnapshot("pod1")
	cpuEst, memEst = cache.GetNodeEst("node1")
	if math.Abs(cpuEst-960) > testEps {
		t.Errorf("After removing pod1: expected cpuEst=960, got %v", cpuEst)
	}

	cache.SubEstimateBySnapshot("pod2")
	cpuEst, memEst = cache.GetNodeEst("node1")
	if cpuEst != 0 || memEst != 0 {
		t.Errorf("After removing all: expected (0, 0), got (%v, %v)", cpuEst, memEst)
	}
}

func TestShadowLoadCache_MultipleNodes(t *testing.T) {
	cache := NewShadowLoadCache()

	cache.AddEstimate("node1", "t1", 1000, 2000)
	cache.AddEstimate("node2", "t2", 3000, 4000)
	cache.AddEstimate("node3", "t3", 500, 800)

	cpuEst1, memEst1 := cache.GetNodeEst("node1")
	cpuEst2, memEst2 := cache.GetNodeEst("node2")
	cpuEst3, memEst3 := cache.GetNodeEst("node3")

	if math.Abs(cpuEst1-1000) > testEps || math.Abs(memEst1-2000) > testEps {
		t.Errorf("node1: expected (1000, 2000), got (%v, %v)", cpuEst1, memEst1)
	}
	if math.Abs(cpuEst2-3000) > testEps || math.Abs(memEst2-4000) > testEps {
		t.Errorf("node2: expected (3000, 4000), got (%v, %v)", cpuEst2, memEst2)
	}
	if math.Abs(cpuEst3-500) > testEps || math.Abs(memEst3-800) > testEps {
		t.Errorf("node3: expected (500, 800), got (%v, %v)", cpuEst3, memEst3)
	}

}

func TestShadowLoadCache_GetNodeEst_NonExistentNode(t *testing.T) {
	cache := NewShadowLoadCache()

	cpuEst, memEst := cache.GetNodeEst("nonexistent")
	if cpuEst != 0 || memEst != 0 {
		t.Errorf("Non-existent node should return (0, 0), got (%v, %v)", cpuEst, memEst)
	}
}

func TestShadowLoadCache_Reset(t *testing.T) {
	cache := NewShadowLoadCache()

	cache.AddEstimate("node1", "t1", 1000, 2000)
	cache.AddEstimate("node2", "t2", 3000, 4000)

	if cache.IsClean() {
		t.Error("Cache should not be clean after adding estimates")
	}

	cache.Reset()

	if !cache.IsClean() {
		t.Error("Cache should be clean after reset")
	}

	cpuEst, memEst := cache.GetNodeEst("node1")
	if cpuEst != 0 || memEst != 0 {
		t.Errorf("After reset, node1 should return (0, 0), got (%v, %v)", cpuEst, memEst)
	}
	if cache.GetSnapshot("t1") != nil {
		t.Error("After reset, snapshots should be nil")
	}
}

func TestShadowLoadCache_IsClean(t *testing.T) {
	cache := NewShadowLoadCache()

	if !cache.IsClean() {
		t.Error("New cache should be clean")
	}

	cache.AddEstimate("node1", "t1", 100, 200)
	if cache.IsClean() {
		t.Error("Cache with data should not be clean")
	}
}

func TestShadowLoadCache_ConcurrentAccess(t *testing.T) {
	cache := NewShadowLoadCache()
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			taskID := api.TaskID(fmt.Sprintf("task-%d", idx))
			if idx%2 == 0 {
				cache.AddEstimate("node1", taskID, 10, 20)
			} else {
				cache.SubEstimateBySnapshot(taskID)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.GetNodeEst("node1")
			cache.IsClean()
		}()
	}

	wg.Wait()

	// Just verify no panic occurred
	cpuEst, _ := cache.GetNodeEst("node1")
	if cpuEst < 0 {
		t.Errorf("cpuEst should not be negative after concurrent access, got %v", cpuEst)
	}
}

func TestShadowLoadCache_AllocateDeallocateExactMatch(t *testing.T) {
	// Verify that Allocate + Deallocate for the same task results in near-zero (within floating point precision)
	cache := NewShadowLoadCache()

	cache.AddEstimate("node1", "task-a", 1234.5678, 9876543.21)
	cache.AddEstimate("node1", "task-b", 567.89, 1234567.89)

	cache.SubEstimateBySnapshot("task-a")
	cache.SubEstimateBySnapshot("task-b")

	cpuEst, memEst := cache.GetNodeEst("node1")
	if math.Abs(cpuEst) > testEps {
		t.Errorf("Expected cpuEst≈0 after exact match, got %v", cpuEst)
	}
	if math.Abs(memEst) > testEps {
		t.Errorf("Expected memEst≈0 after exact match, got %v", memEst)
	}
}
