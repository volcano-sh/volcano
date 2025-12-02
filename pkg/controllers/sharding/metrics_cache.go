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
