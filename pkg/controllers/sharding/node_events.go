package sharding

import (
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"

	"k8s.io/klog/v2"
)

// addNodeEvent handles node addition events
func (sc *ShardingController) addNodeEvent(obj interface{}) {
	node := sc.getNodeFromObject(obj)
	if node == nil {
		return
	}

	klog.V(4).Infof("Node added: %s", node.Name)
	sc.enqueueNodeEvent(node.Name, "node-added", "node-controller")
}

// updateNodeEvent handles node update events
func (sc *ShardingController) updateNodeEvent(oldObj, newObj interface{}) {
	newNode := sc.getNodeFromObject(newObj)
	if newNode == nil {
		return
	}

	oldNode := sc.getNodeFromObject(oldObj)
	if oldNode == nil {
		// Treat as add if old object not found
		sc.enqueueNodeEvent(newNode.Name, "node-added", "node-controller")
		return
	}

	// Check if significant changes occurred
	if sc.isNodeSignificantlyChanged(oldNode, newNode) {
		klog.V(4).Infof("Node significantly updated: %s", newNode.Name)
		sc.enqueueNodeEvent(newNode.Name, "node-updated", "node-controller")
	}
}

// deleteNodeEvent handles node deletion events
func (sc *ShardingController) deleteNodeEvent(obj interface{}) {
	node := sc.getNodeFromObject(obj)
	if node == nil {
		return
	}

	klog.V(4).Infof("Node deleted: %s", node.Name)

	// Clean up metrics cache
	sc.metricsMutex.Lock()
	delete(sc.nodeMetricsCache, node.Name)
	sc.metricsMutex.Unlock()

	// Trigger shard sync
	time.AfterFunc(100*time.Millisecond, func() {
		sc.syncShards()
	})
}

// getNodeFromObject safely extracts a Node from an object
func (sc *ShardingController) getNodeFromObject(obj interface{}) *corev1.Node {
	switch obj := obj.(type) {
	case *corev1.Node:
		return obj
	case cache.DeletedFinalStateUnknown:
		if node, ok := obj.Obj.(*corev1.Node); ok {
			return node
		}
		klog.Warningf("DeletedFinalStateUnknown contained non-Node object: %T", obj.Obj)
	default:
		klog.Warningf("Unexpected object type in node event handler: %T", obj)
	}
	return nil
}

// isNodeSignificantlyChanged checks if node changed significantly
func (sc *ShardingController) isNodeSignificantlyChanged(oldNode, newNode *corev1.Node) bool {
	// Check labels/annotations change
	if !sc.areMapsEqual(oldNode.Labels, newNode.Labels) ||
		!sc.areMapsEqual(oldNode.Annotations, newNode.Annotations) {
		return true
	}

	// Check capacity change
	oldCPU := oldNode.Status.Capacity.Cpu().Value()
	newCPU := newNode.Status.Capacity.Cpu().Value()
	if float64(abs(int(newCPU-oldCPU)))/float64(oldCPU+1) > 0.1 { // 10% change
		return true
	}

	return false
}

// areMapsEqual checks if two maps are equal
func (sc *ShardingController) areMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// enqueueNodeEvent adds a node event to the queue
func (sc *ShardingController) enqueueNodeEvent(nodeName, eventType, source string) {
	// Use a consistent key format for deduplication
	key := fmt.Sprintf("%s:%s:%s", nodeName, eventType, source)
	sc.nodeEventQueue.Add(key)

	klog.V(5).Infof("Enqueued node event: %s", key)
}

// nodeEventWorker processes node events
func (sc *ShardingController) nodeEventWorker() {
	for sc.processNodeEvent() {
	}
}

// processNodeEvent processes a single node event
func (sc *ShardingController) processNodeEvent() bool {
	eventKey, quit := sc.nodeEventQueue.Get()
	if quit {
		return false
	}
	defer sc.nodeEventQueue.Done(eventKey)

	parts := strings.SplitN(eventKey, ":", 3)
	if len(parts) < 3 {
		klog.Warningf("Invalid node event key: %s", eventKey)
		return true
	}

	nodeName := parts[0]
	eventType := parts[1]
	source := parts[2]

	klog.V(4).Infof("Processing node event: %s %s from %s", nodeName, eventType, source)

	// Process event with retry logic
	if err := sc.processNodeEventWithRetry(nodeName, eventType, source, 3); err != nil {
		klog.Errorf("Failed to process node event %s: %v", eventKey, err)
		if sc.nodeEventQueue.NumRequeues(eventKey) < 3 {
			sc.nodeEventQueue.AddRateLimited(eventKey)
			return true
		}
		klog.Warningf("Dropping node event %s after 3 retries", eventKey)
	}

	return true
}

// processNodeEventWithRetry processes a node event with retry logic
func (sc *ShardingController) processNodeEventWithRetry(nodeName, eventType, source string, maxRetries int) error {
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		// Get node from cache
		_, err := sc.nodeLister.Get(nodeName)
		if err != nil {
			lastErr = fmt.Errorf("node %s not found in cache (retry %d/%d): %v", nodeName, i+1, maxRetries, err)
			if errors.IsNotFound(err) {
				// Node doesn't exist anymore
				klog.V(4).Infof("Node %s no longer exists, skipping event processing", nodeName)
				return nil
			}
			time.Sleep(100 * time.Millisecond * time.Duration(i+1))
			continue
		}

		// Calculate metrics
		metrics, err := sc.calculateNodeUtilization(nodeName)
		if err != nil {
			lastErr = fmt.Errorf("failed to calculate metrics for node %s (retry %d/%d): %v", nodeName, i+1, maxRetries, err)
			time.Sleep(100 * time.Millisecond * time.Duration(i+1))
			continue
		}

		// Update metrics cache
		sc.UpdateNodeMetrics(nodeName, metrics)

		// Check if significant change
		if sc.isUtilizationSignificantlyChanged(nodeName, metrics) {
			klog.V(4).Infof("Node %s utilization changed significantly, scheduling shard sync", nodeName)
			// Schedule sync in background with delay
			time.AfterFunc(200*time.Millisecond, func() {
				sc.syncShards()
			})
		}

		return nil
	}

	return lastErr
}
