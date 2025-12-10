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
	sc.nodeMetricsCache[event.NodeName] = metrics

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

	return cpuChange > nodeUsageChangeThreshold || memChange > nodeUsageChangeThreshold
}
