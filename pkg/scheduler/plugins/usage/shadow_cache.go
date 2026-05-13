/*
Copyright 2024 The Volcano Authors.

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
	"sync"
)

// ShadowLoadCache is a session-level cache that tracks the estimated resource
// consumption of shadow pods (pods that have been scheduled but whose metrics
// are not yet visible in the monitoring system).
type ShadowLoadCache struct {
	mu sync.RWMutex

	// nodeCPUEst stores the total estimated CPU consumption (in milliCPU)
	// of all shadow pods on each node.
	nodeCPUEst map[string]float64

	// nodeMemEst stores the total estimated Memory consumption (in bytes)
	// of all shadow pods on each node.
	nodeMemEst map[string]float64

	// BestEffortCounts stores the number of BestEffort pods on each node
	// that are tracked in the shadow cache.
	BestEffortCounts map[string]int
}

// NewShadowLoadCache creates a new empty ShadowLoadCache.
func NewShadowLoadCache() *ShadowLoadCache {
	return &ShadowLoadCache{
		nodeCPUEst:       make(map[string]float64),
		nodeMemEst:       make(map[string]float64),
		BestEffortCounts: make(map[string]int),
	}
}

// Reset clears all data in the cache.
func (c *ShadowLoadCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodeCPUEst = make(map[string]float64)
	c.nodeMemEst = make(map[string]float64)
	c.BestEffortCounts = make(map[string]int)
}

// IsClean returns true if the cache contains no data.
func (c *ShadowLoadCache) IsClean() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.nodeCPUEst) == 0 && len(c.nodeMemEst) == 0 && len(c.BestEffortCounts) == 0
}

// AddEstimate adds a pod's estimated resource consumption to the specified node.
// This is called when a pod is determined to be a shadow pod (during OnSessionOpen
// initialization or when an Allocate event fires).
func (c *ShadowLoadCache) AddEstimate(nodeName string, cpuMillis, memBytes float64, isBestEffort bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodeCPUEst[nodeName] += cpuMillis
	c.nodeMemEst[nodeName] += memBytes
	if isBestEffort {
		c.BestEffortCounts[nodeName]++
	}
}

// SubEstimate subtracts a pod's estimated resource consumption from the specified node.
// This is the reverse of AddEstimate, called during Deallocate events.
func (c *ShadowLoadCache) SubEstimate(nodeName string, cpuMillis, memBytes float64, isBestEffort bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodeCPUEst[nodeName] -= cpuMillis
	if c.nodeCPUEst[nodeName] < 0 {
		c.nodeCPUEst[nodeName] = 0
	}
	c.nodeMemEst[nodeName] -= memBytes
	if c.nodeMemEst[nodeName] < 0 {
		c.nodeMemEst[nodeName] = 0
	}
	if isBestEffort {
		c.BestEffortCounts[nodeName]--
		if c.BestEffortCounts[nodeName] < 0 {
			c.BestEffortCounts[nodeName] = 0
		}
	}
}

// GetNodeEst returns the total estimated CPU (milliCPU) and Memory (bytes)
// for the specified node.
func (c *ShadowLoadCache) GetNodeEst(nodeName string) (cpuEst, memEst float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nodeCPUEst[nodeName], c.nodeMemEst[nodeName]
}

// GetBestEffortCount returns the number of BestEffort pods tracked in the
// shadow cache for the specified node.
func (c *ShadowLoadCache) GetBestEffortCount(nodeName string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.BestEffortCounts[nodeName]
}
