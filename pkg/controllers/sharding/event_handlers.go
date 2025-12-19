/*
Copyright 2025 The Volcano Authors.
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

package sharding

import (
	"math"
	"time"

	"k8s.io/klog/v2"
)

// NodeEvent represents a node-related event
type NodeEvent struct {
	EventType string // "pod-added", "pod-updated", "pod-deleted", "node-updated"
	NodeName  string
	Source    string // "pod-controller", "node-controller"
	Timestamp time.Time
}

// updateNodeMetricsFromEvent updates node metrics based on an event
func (sc *ShardingController) updateNodeMetricsFromEvent(event *NodeEvent) {
	sc.metricsMutex.Lock()
	defer sc.metricsMutex.Unlock()

	metrics, err := sc.calculateNodeUtilization(event.NodeName)
	if err != nil {
		klog.Warningf("Failed to calculate metrics for node %s: %v", event.NodeName, err)
		return
	}

	// Update cache
	if sc.nodeMetricsCache == nil {
		sc.nodeMetricsCache = make(map[string]*NodeMetrics)
	}

	// Trigger shard sync if utilization changed significantly
	if sc.isUtilizationSignificantlyChanged(event.NodeName, metrics) {
		klog.V(4).Infof("Node %s utilization changed significantly, scheduling sync", event.NodeName)
		sc.enqueueNodeEvent(event.NodeName, "node-updated", "node-controller")
	}
}

// isUtilizationSignificantlyChanged checks if utilization changed significantly
func (sc *ShardingController) isUtilizationSignificantlyChanged(nodeName string, newMetrics *NodeMetrics) bool {
	oldMetrics, exists := sc.nodeMetricsCache[nodeName]
	if !exists {
		return true // New node always considered significant
	}
	cpuChange := math.Abs(newMetrics.CPUUtilization - oldMetrics.CPUUtilization)
	memChange := math.Abs(newMetrics.MemoryUtilization - oldMetrics.MemoryUtilization)
	// update the node metrics cache
	sc.nodeMetricsCache[nodeName] = newMetrics
	return cpuChange > nodeUsageChangeThreshold || memChange > nodeUsageChangeThreshold
}
