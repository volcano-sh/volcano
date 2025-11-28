package sharding

import (
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/klog/v2"
)

// Implement NodeMetricsProvider interface
func (sc *ShardingController) GetNodeMetrics(nodeName string) *NodeMetrics {
	sc.metricsMutex.RLock()
	defer sc.metricsMutex.RUnlock()
	return sc.nodeMetricsCache[nodeName]
}

func (sc *ShardingController) GetAllNodeMetrics() map[string]*NodeMetrics {
	sc.metricsMutex.RLock()
	defer sc.metricsMutex.RUnlock()

	// Create a copy to avoid concurrent modification
	metricsCopy := make(map[string]*NodeMetrics, len(sc.nodeMetricsCache))
	for name, metrics := range sc.nodeMetricsCache {
		metricsCopy[name] = metrics
	}
	return metricsCopy
}

func (sc *ShardingController) UpdateNodeMetrics(nodeName string, metrics *NodeMetrics) {
	sc.metricsMutex.Lock()
	defer sc.metricsMutex.Unlock()

	// Update or create metrics
	if sc.nodeMetricsCache == nil {
		sc.nodeMetricsCache = make(map[string]*NodeMetrics)
	}
	sc.nodeMetricsCache[nodeName] = metrics
}

// initializeNodeMetrics initializes metrics for all nodes
func (sc *ShardingController) initializeNodeMetrics() {
	klog.Info("Initializing node metrics...")

	nodes, err := sc.nodeLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("Failed to list nodes for initialization: %v", err)
		return
	}

	for _, node := range nodes {
		// Initialize in background to avoid blocking startup
		// go func(nodeName string) {
		event := &NodeEvent{
			EventType: "initialization",
			NodeName:  node.Name,
			Source:    "startup",
			Timestamp: time.Now(),
		}
		sc.updateNodeMetricsFromEvent(event)
		// }(node.Name)
	}

	klog.Infof("Finish initialization for %d nodes", len(nodes))
}

// recoverNodeMetrics recovers node metrics after controller restart
// func (sc *ShardingController) recoverNodeMetrics() {
// 	klog.Info("Recovering node metrics...")

// 	nodes, err := sc.nodeLister.List(labels.Everything())
// 	if err != nil {
// 		klog.Errorf("Failed to list nodes for recovery: %v", err)
// 		return
// 	}

// 	for _, node := range nodes {
// 		// Recover in background
// 		go func(nodeName string) {
// 			metrics := sc.GetNodeMetrics(nodeName)
// 			if metrics == nil || time.Since(metrics.LastUpdated) > 5*time.Minute {
// 				event := &NodeEvent{
// 					EventType: "recovery",
// 					NodeName:  nodeName,
// 					Source:    "recovery",
// 					Timestamp: time.Now(),
// 				}
// 				sc.updateNodeMetricsFromEvent(event)
// 			}
// 		}(node.Name)
// 	}

// 	klog.Infof("Started recovery for %d nodes", len(nodes))
// }

// // initializeNodeMetrics initializes metrics for all nodes at startup
// func (sc *ShardingController) initializeNodeMetrics() {
// 	klog.Info("Initializing node metrics...")

// 	nodes, err := sc.nodeLister.List(labels.Everything())
// 	if err != nil {
// 		klog.Errorf("Failed to list nodes for initialization: %v", err)
// 		return
// 	}

// 	for _, node := range nodes {
// 		// Calculate metrics in background to avoid blocking startup
// 		go func(nodeName string) {
// 			metrics, err := sc.calculateNodeUtilization(nodeName)
// 			if err != nil {
// 				klog.Warningf("Failed to calculate initial metrics for node %s: %v", nodeName, err)
// 				return
// 			}
// 			sc.updateNodeUtilization(nodeName, metrics)
// 		}(node.Name)
// 	}

// 	klog.Infof("Initialized metrics for %d nodes", len(nodes))
// }

// // recoverNodeMetrics recovers node metrics after controller restart
// func (sc *ShardingController) recoverNodeMetrics() {
// 	klog.Info("Recovering node metrics...")

// 	// Get all nodes
// 	nodes, err := sc.nodeLister.List(labels.Everything())
// 	if err != nil {
// 		klog.Errorf("Failed to list nodes for recovery: %v", err)
// 		return
// 	}

// 	for _, node := range nodes {
// 		// Check if we have cached metrics
// 		if metrics := sc.GetNodeMetrics(node.Name); metrics != nil {
// 			// Validate metrics freshness
// 			if time.Since(metrics.LastUpdated) < 5*time.Minute {
// 				continue // Metrics are fresh enough
// 			}
// 		}

// 		// Recalculate metrics in background
// 		go func(nodeName string) {
// 			metrics, err := sc.calculateNodeUtilization(nodeName)
// 			if err != nil {
// 				klog.Warningf("Failed to recover metrics for node %s: %v", nodeName, err)
// 				return
// 			}
// 			sc.updateNodeUtilization(nodeName, metrics)
// 		}(node.Name)
// 	}

// 	klog.Infof("Recovered metrics for %d nodes", len(nodes))
// }
