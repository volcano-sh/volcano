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
	"sync"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// PodEstSnapshot records the estimated resource consumption of a pod at the time
// it was allocated. This snapshot is used during deallocation to ensure the exact
// same value is subtracted, avoiding inconsistencies caused by re-calculation
// when node pressure or estimator configuration changes between allocate and
// deallocate.
type PodEstSnapshot struct {
	NodeName  string
	CPUMillis float64
	MemBytes  float64
}

// ShadowLoadCache is a session-level cache that tracks the estimated resource
// consumption of shadow pods (pods that have been scheduled but whose metrics
// are not yet visible in the monitoring system).
//
// It uses a "snapshot persistence" strategy: when a pod is allocated, its estimated
// resource values are recorded in a per-pod snapshot. During deallocation, the exact
// snapshot values are subtracted, guaranteeing add/sub consistency regardless of
// formula state changes between the two events.
type ShadowLoadCache struct {
	mu sync.RWMutex

	// nodeCPUEst stores the total estimated CPU consumption (in milliCPU)
	// of all shadow pods on each node.
	nodeCPUEst map[string]float64

	// nodeMemEst stores the total estimated Memory consumption (in bytes)
	// of all shadow pods on each node.
	nodeMemEst map[string]float64

	// podSnapshots stores the per-pod estimation snapshot, keyed by TaskID.
	// This ensures that deallocation subtracts the exact value that was added.
	podSnapshots map[api.TaskID]*PodEstSnapshot
}

// NewShadowLoadCache creates a new empty ShadowLoadCache.
func NewShadowLoadCache() *ShadowLoadCache {
	return &ShadowLoadCache{
		nodeCPUEst:   make(map[string]float64),
		nodeMemEst:   make(map[string]float64),
		podSnapshots: make(map[api.TaskID]*PodEstSnapshot),
	}
}

// Reset clears all data in the cache.
func (c *ShadowLoadCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodeCPUEst = make(map[string]float64)
	c.nodeMemEst = make(map[string]float64)
	c.podSnapshots = make(map[api.TaskID]*PodEstSnapshot)
}

// IsClean returns true if the cache contains no data.
func (c *ShadowLoadCache) IsClean() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.nodeCPUEst) == 0 && len(c.nodeMemEst) == 0 && len(c.podSnapshots) == 0
}

// AddEstimate adds a pod's estimated resource consumption to the specified node
// and records a snapshot for later precise deallocation.
// This is called when a pod is determined to be a shadow pod (during OnSessionOpen
// initialization or when an Allocate event fires).
func (c *ShadowLoadCache) AddEstimate(nodeName string, taskID api.TaskID, cpuMillis, memBytes float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if snapshot, ok := c.podSnapshots[taskID]; ok {
		c.subtractSnapshot(snapshot)
	}
	c.nodeCPUEst[nodeName] += cpuMillis
	c.nodeMemEst[nodeName] += memBytes
	// Record snapshot for precise deallocation
	c.podSnapshots[taskID] = &PodEstSnapshot{
		NodeName:  nodeName,
		CPUMillis: cpuMillis,
		MemBytes:  memBytes,
	}
}

// SubEstimateBySnapshot subtracts a pod's estimated resource consumption using
// the snapshot recorded during allocation. This guarantees that the exact value
// added is subtracted, regardless of any state changes between allocate and deallocate.
// If no snapshot exists for the given taskID, this is a no-op.
func (c *ShadowLoadCache) SubEstimateBySnapshot(taskID api.TaskID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot, ok := c.podSnapshots[taskID]
	if !ok {
		klog.V(5).Infof("ShadowLoadCache: no snapshot found for task %s, skipping SubEstimate", taskID)
		return
	}
	c.subtractSnapshot(snapshot)
	delete(c.podSnapshots, taskID)
}

func (c *ShadowLoadCache) subtractSnapshot(snapshot *PodEstSnapshot) {
	c.nodeCPUEst[snapshot.NodeName] -= snapshot.CPUMillis
	if c.nodeCPUEst[snapshot.NodeName] < 0 {
		c.nodeCPUEst[snapshot.NodeName] = 0
	}
	c.nodeMemEst[snapshot.NodeName] -= snapshot.MemBytes
	if c.nodeMemEst[snapshot.NodeName] < 0 {
		c.nodeMemEst[snapshot.NodeName] = 0
	}
}

// GetNodeEst returns the total estimated CPU (milliCPU) and Memory (bytes)
// for the specified node.
func (c *ShadowLoadCache) GetNodeEst(nodeName string) (cpuEst, memEst float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nodeCPUEst[nodeName], c.nodeMemEst[nodeName]
}

// GetSnapshot returns the estimation snapshot for a specific task.
// Returns nil if no snapshot exists.
func (c *ShadowLoadCache) GetSnapshot(taskID api.TaskID) *PodEstSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.podSnapshots[taskID]
}
