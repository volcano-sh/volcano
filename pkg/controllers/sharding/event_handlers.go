package sharding

import (
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
	// klog.Infof("Calculated metrics for node %s: CPU=%.2f, Memory=%.2f",
	// 	event.NodeName, metrics.CPUUtilization, metrics.MemoryUtilization)

	// Update cache
	if sc.nodeMetricsCache == nil {
		sc.nodeMetricsCache = make(map[string]*NodeMetrics)
	}
	sc.nodeMetricsCache[event.NodeName] = metrics

	// klog.Infof("Updated metrics for node %s: CPU=%.2f, Memory=%.2f",
	// 	event.NodeName, metrics.CPUUtilization, metrics.MemoryUtilization)

	// Trigger shard sync if utilization changed significantly
	if sc.isUtilizationSignificantlyChanged(event.NodeName, metrics) {
		klog.V(4).Infof("Node %s utilization changed significantly, scheduling sync", event.NodeName)
		time.AfterFunc(200*time.Millisecond, func() {
			sc.syncShards()
		})
	}
}

// handlePodEvent processes pod-related events and updates node metrics
// func (sc *ShardingController) handlePodEvent(pod *corev1.Pod, eventType string) {
// 	if pod.Spec.NodeName == "" {
// 		return // Pod not scheduled yet
// 	}

// 	klog.V(5).Infof("Pod %s/%s %s on node %s", pod.Namespace, pod.Name, eventType, pod.Spec.NodeName)

// 	// Create node event
// 	event := &NodeEvent{
// 		EventType: fmt.Sprintf("pod-%s", eventType),
// 		NodeName:  pod.Spec.NodeName,
// 		Source:    "pod-controller",
// 		Timestamp: time.Now(),
// 	}

// 	// Process event in background to avoid blocking
// 	go sc.updateNodeMetricsFromEvent(event)
// }

// isUtilizationSignificantlyChanged checks if utilization changed significantly
func (sc *ShardingController) isUtilizationSignificantlyChanged(nodeName string, newMetrics *NodeMetrics) bool {
	oldMetrics, exists := sc.nodeMetricsCache[nodeName]
	if !exists {
		return true // New node always considered significant
	}

	cpuChange := newMetrics.CPUUtilization - oldMetrics.CPUUtilization
	if cpuChange < 0 {
		cpuChange = -cpuChange
	}

	memChange := newMetrics.MemoryUtilization - oldMetrics.MemoryUtilization
	if memChange < 0 {
		memChange = -memChange
	}

	return cpuChange > 0.1 || memChange > 0.1 // 10% threshold
}
