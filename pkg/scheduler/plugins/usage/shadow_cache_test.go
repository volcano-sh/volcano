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
	"math"
	"sync"
	"testing"
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
	cache.AddEstimate("node1", 1000, 2048, false)
	cpuEst, memEst := cache.GetNodeEst("node1")
	if math.Abs(cpuEst-1000) > testEps {
		t.Errorf("Expected cpuEst=1000, got %v", cpuEst)
	}
	if math.Abs(memEst-2048) > testEps {
		t.Errorf("Expected memEst=2048, got %v", memEst)
	}
	if cache.GetBestEffortCount("node1") != 0 {
		t.Errorf("Expected BestEffortCount=0, got %v", cache.GetBestEffortCount("node1"))
	}

	// Add second pod estimate to same node
	cache.AddEstimate("node1", 500, 1024, false)
	cpuEst, memEst = cache.GetNodeEst("node1")
	if math.Abs(cpuEst-1500) > testEps {
		t.Errorf("Expected cpuEst=1500, got %v", cpuEst)
	}
	if math.Abs(memEst-3072) > testEps {
		t.Errorf("Expected memEst=3072, got %v", memEst)
	}

	// Add BestEffort pod
	cache.AddEstimate("node1", 800, 1600, true)
	cpuEst, memEst = cache.GetNodeEst("node1")
	if math.Abs(cpuEst-2300) > testEps {
		t.Errorf("Expected cpuEst=2300, got %v", cpuEst)
	}
	if math.Abs(memEst-4672) > testEps {
		t.Errorf("Expected memEst=4672, got %v", memEst)
	}
	if cache.GetBestEffortCount("node1") != 1 {
		t.Errorf("Expected BestEffortCount=1, got %v", cache.GetBestEffortCount("node1"))
	}
}

func TestShadowLoadCache_SubEstimate(t *testing.T) {
	cache := NewShadowLoadCache()

	// Add then subtract
	cache.AddEstimate("node1", 1000, 2048, true)
	cache.AddEstimate("node1", 500, 1024, false)
	cache.SubEstimate("node1", 500, 1024, false)

	cpuEst, memEst := cache.GetNodeEst("node1")
	if math.Abs(cpuEst-1000) > testEps {
		t.Errorf("Expected cpuEst=1000 after sub, got %v", cpuEst)
	}
	if math.Abs(memEst-2048) > testEps {
		t.Errorf("Expected memEst=2048 after sub, got %v", memEst)
	}
	if cache.GetBestEffortCount("node1") != 1 {
		t.Errorf("Expected BestEffortCount=1, got %v", cache.GetBestEffortCount("node1"))
	}

	// Subtract BestEffort
	cache.SubEstimate("node1", 1000, 2048, true)
	if cache.GetBestEffortCount("node1") != 0 {
		t.Errorf("Expected BestEffortCount=0, got %v", cache.GetBestEffortCount("node1"))
	}
}

func TestShadowLoadCache_SubEstimate_NoNegative(t *testing.T) {
	cache := NewShadowLoadCache()

	// Subtract more than what was added - should clamp to 0
	cache.AddEstimate("node1", 100, 200, true)
	cache.SubEstimate("node1", 500, 1000, true)

	cpuEst, memEst := cache.GetNodeEst("node1")
	if cpuEst < 0 {
		t.Errorf("cpuEst should not be negative, got %v", cpuEst)
	}
	if memEst < 0 {
		t.Errorf("memEst should not be negative, got %v", memEst)
	}
	if cache.GetBestEffortCount("node1") < 0 {
		t.Errorf("BestEffortCount should not be negative, got %v", cache.GetBestEffortCount("node1"))
	}
}

func TestShadowLoadCache_MultipleNodes(t *testing.T) {
	cache := NewShadowLoadCache()

	cache.AddEstimate("node1", 1000, 2000, false)
	cache.AddEstimate("node2", 3000, 4000, true)
	cache.AddEstimate("node3", 500, 800, false)

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

	if cache.GetBestEffortCount("node1") != 0 {
		t.Errorf("node1 BestEffortCount expected 0, got %v", cache.GetBestEffortCount("node1"))
	}
	if cache.GetBestEffortCount("node2") != 1 {
		t.Errorf("node2 BestEffortCount expected 1, got %v", cache.GetBestEffortCount("node2"))
	}
}

func TestShadowLoadCache_GetNodeEst_NonExistentNode(t *testing.T) {
	cache := NewShadowLoadCache()

	cpuEst, memEst := cache.GetNodeEst("nonexistent")
	if cpuEst != 0 || memEst != 0 {
		t.Errorf("Non-existent node should return (0, 0), got (%v, %v)", cpuEst, memEst)
	}
	if cache.GetBestEffortCount("nonexistent") != 0 {
		t.Errorf("Non-existent node BestEffortCount should be 0, got %v", cache.GetBestEffortCount("nonexistent"))
	}
}

func TestShadowLoadCache_Reset(t *testing.T) {
	cache := NewShadowLoadCache()

	cache.AddEstimate("node1", 1000, 2000, true)
	cache.AddEstimate("node2", 3000, 4000, false)

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
}

func TestShadowLoadCache_IsClean(t *testing.T) {
	cache := NewShadowLoadCache()

	if !cache.IsClean() {
		t.Error("New cache should be clean")
	}

	cache.AddEstimate("node1", 100, 200, false)
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
			nodeName := "node1"
			if idx%2 == 0 {
				cache.AddEstimate(nodeName, 10, 20, idx%3 == 0)
			} else {
				cache.SubEstimate(nodeName, 5, 10, idx%3 == 0)
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.GetNodeEst("node1")
			cache.GetBestEffortCount("node1")
			cache.IsClean()
		}()
	}

	wg.Wait()

	// Just verify no panic occurred - the exact values depend on goroutine scheduling
	cpuEst, _ := cache.GetNodeEst("node1")
	if cpuEst < 0 {
		t.Errorf("cpuEst should not be negative after concurrent access, got %v", cpuEst)
	}
}

func TestShadowLoadCache_BestEffortCounts(t *testing.T) {
	cache := NewShadowLoadCache()

	// Add multiple BestEffort pods
	cache.AddEstimate("node1", 100, 200, true)
	cache.AddEstimate("node1", 100, 200, true)
	cache.AddEstimate("node1", 100, 200, true)
	cache.AddEstimate("node1", 100, 200, false) // non-BE

	if cache.GetBestEffortCount("node1") != 3 {
		t.Errorf("Expected BestEffortCount=3, got %v", cache.GetBestEffortCount("node1"))
	}

	// Remove one BE
	cache.SubEstimate("node1", 100, 200, true)
	if cache.GetBestEffortCount("node1") != 2 {
		t.Errorf("Expected BestEffortCount=2 after sub, got %v", cache.GetBestEffortCount("node1"))
	}

	// Remove non-BE should not affect count
	cache.SubEstimate("node1", 100, 200, false)
	if cache.GetBestEffortCount("node1") != 2 {
		t.Errorf("Expected BestEffortCount=2 after non-BE sub, got %v", cache.GetBestEffortCount("node1"))
	}
}
