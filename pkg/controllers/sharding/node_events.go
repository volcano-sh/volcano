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

// // addNodeEvent handles node addition events
// func (sc *ShardingController) addNodeEvent(obj interface{}) {
// 	node, ok := obj.(*corev1.Node)
// 	if !ok {
// 		klog.Errorf("Add node event received non-node object: %T", obj)
// 		return
// 	}

// 	klog.V(4).Infof("Node added: %s", node.Name)

// 	// Create node event
// 	event := &NodeEvent{
// 		Type:      "node-added",
// 		NodeName:  node.Name,
// 		Source:    "node-controller",
// 		Timestamp: time.Now(),
// 	}

// 	// Process in background
// 	go sc.updateNodeMetricsFromEvent(event)
// }

// // updateNodeEvent handles node update events
// func (sc *ShardingController) updateNodeEvent(oldObj, newObj interface{}) {
// 	newNode, ok := newObj.(*corev1.Node)
// 	if !ok {
// 		klog.Errorf("Update node event received non-node object: %T", newObj)
// 		return
// 	}

// 	oldNode, ok := oldObj.(*corev1.Node)
// 	if !ok {
// 		klog.Errorf("Update node event received non-node old object: %T", oldObj)
// 		return
// 	}

// 	// Check if significant changes occurred
// 	if sc.isNodeSignificantlyChanged(oldNode, newNode) {
// 		klog.V(4).Infof("Node significantly updated: %s", newNode.Name)

// 		// Create node event
// 		event := &NodeEvent{
// 			Type:      "node-updated",
// 			NodeName:  newNode.Name,
// 			Source:    "node-controller",
// 			Timestamp: time.Now(),
// 		}

// 		// Process in background
// 		go sc.updateNodeMetricsFromEvent(event)
// 	}
// }

// // deleteNodeEvent handles node deletion events
// func (sc *ShardingController) deleteNodeEvent(obj interface{}) {
// 	nodeName := ""
// 	switch obj := obj.(type) {
// 	case *corev1.Node:
// 		nodeName = obj.Name
// 	case cache.DeletedFinalStateUnknown:
// 		node, ok := obj.Obj.(*corev1.Node)
// 		if !ok {
// 			klog.Errorf("Delete node event received non-node object: %T", obj.Obj)
// 			return
// 		}
// 		nodeName = node.Name
// 	default:
// 		klog.Errorf("Delete node event received unexpected object type: %T", obj)
// 		return
// 	}

// 	klog.V(4).Infof("Node deleted: %s", nodeName)

// 	// Remove from cache
// 	sc.metricsMutex.Lock()
// 	delete(sc.nodeMetricsCache, nodeName)
// 	sc.metricsMutex.Unlock()

// 	// Trigger shard sync
// 	time.AfterFunc(100*time.Millisecond, func() {
// 		sc.syncShards()
// 	})
// }

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

// handleNodeDeletion handles node deletion event
// func (sc *ShardingController) handleNodeDeletion(eventKey string) {
// 	nodeName := strings.TrimPrefix(eventKey, "node-deleted-")
// 	klog.V(4).Infof("Handling deletion of node: %s", nodeName)

// 	// Check if node was assigned to any scheduler
// 	sc.cacheMutex.Lock()
// 	defer sc.cacheMutex.Unlock()

// 	if sc.assignmentCache == nil {
// 		return
// 	}

// 	for schedulerName, assignment := range sc.assignmentCache.Assignments {
// 		for _, node := range assignment.NodesDesired {
// 			if node == nodeName {
// 				klog.V(4).Infof("Node %s was assigned to %s, triggering resync", nodeName, schedulerName)
// 				sc.enqueueShard(schedulerName)
// 				break
// 			}
// 		}
// 	}
// }

// addNodeEvent handles node addition events
// func (sc *ShardingController) addNodeEvent(obj interface{}) {
// 	node, ok := obj.(*corev1.Node)
// 	if !ok {
// 		klog.Errorf("Object is not a Node: %T", obj)
// 		return
// 	}

// 	klog.V(4).Infof("Node added: %s", node.Name)
// 	sc.updateNodeState(node)

// 	// Trigger resharding after node addition
// 	sc.enqueueNodeChangeEvent("node-added-" + node.Name)
// }

// // updateNodeEvent handles node update events
// func (sc *ShardingController) updateNodeEvent(oldObj, newObj interface{}) {
// 	oldNode, ok := oldObj.(*corev1.Node)
// 	if !ok {
// 		klog.Errorf("Old object is not a Node: %T", oldObj)
// 		return
// 	}

// 	newNode, ok := newObj.(*corev1.Node)
// 	if !ok {
// 		klog.Errorf("New object is not a Node: %T", newObj)
// 		return
// 	}

// 	// Check if node is significantly changed
// 	if sc.isNodeSignificantlyChanged(oldNode, newNode) {
// 		klog.V(4).Infof("Node significantly updated: %s", newNode.Name)
// 		sc.updateNodeState(newNode)
// 		sc.enqueueNodeChangeEvent("node-updated-" + newNode.Name)
// 	}
// }

// // deleteNodeEvent handles node deletion events
// func (sc *ShardingController) deleteNodeEvent(obj interface{}) {
// 	var nodeName string
// 	switch obj := obj.(type) {
// 	case *corev1.Node:
// 		nodeName = obj.Name
// 	case cache.DeletedFinalStateUnknown:
// 		node, ok := obj.Obj.(*corev1.Node)
// 		if !ok {
// 			klog.Errorf("DeletedFinalStateUnknown contained non-Node object: %T", obj.Obj)
// 			return
// 		}
// 		nodeName = node.Name
// 	default:
// 		klog.Errorf("Unexpected object type in deleteNodeEvent: %T", obj)
// 		return
// 	}

// 	klog.V(4).Infof("Node deleted: %s", nodeName)
// 	sc.removeNodeState(nodeName)
// 	sc.enqueueNodeChangeEvent("node-deleted-" + nodeName)
// }

// // isNodeSignificantlyChanged checks if node changed significantly
// func (sc *ShardingController) isNodeSignificantlyChanged(oldNode, newNode *corev1.Node) bool {
// 	// Check labels/annotations change
// 	if !sc.areMapsEqual(oldNode.Labels, newNode.Labels) ||
// 		!sc.areMapsEqual(oldNode.Annotations, newNode.Annotations) {
// 		return true
// 	}

// 	// Check capacity change
// 	oldCPU := oldNode.Status.Capacity.Cpu().Value()
// 	newCPU := newNode.Status.Capacity.Cpu().Value()
// 	if float64(abs(int(newCPU-oldCPU)))/float64(oldCPU+1) > 0.1 { // 10% change
// 		return true
// 	}

// 	// Check allocatable change
// 	oldAllocCPU := oldNode.Status.Allocatable.Cpu().Value()
// 	newAllocCPU := newNode.Status.Allocatable.Cpu().Value()
// 	return float64(abs(int(newAllocCPU-oldAllocCPU)))/float64(oldAllocCPU+1) > 0.1
// }

// // areMapsEqual checks if two maps are equal
// func (sc *ShardingController) areMapsEqual(a, b map[string]string) bool {
// 	if len(a) != len(b) {
// 		return false
// 	}
// 	for k, v := range a {
// 		if bv, ok := b[k]; !ok || bv != v {
// 			return false
// 		}
// 	}
// 	return true
// }

// enqueueNodeChangeEvent adds node change event to queue
// func (sc *ShardingController) enqueueNodeEvent(key string) {
// 	sc.nodeEventQueue.AddRateLimited(key)
// }

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
// func (sc *ShardingController) processNodeEvent() bool {
// 	key, quit := sc.nodeEventQueue.Get()
// 	if quit {
// 		return false
// 	}
// 	defer sc.nodeEventQueue.Done(key)

// 	eventType := key
// 	klog.V(4).Infof("Processing node event: %s", eventType)

// 	// Trigger global resharding after node events
// 	sc.syncShards()

// 	return true
// }

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

// processNodeEvent processes a single node event
// func (sc *ShardingController) processNodeEvent() bool {
// 	key, quit := sc.nodeEventQueue.Get()
// 	if quit {
// 		return false
// 	}
// 	defer sc.nodeEventQueue.Done(key)

// 	eventType := strings.Split(key, "-")[0]
// 	klog.V(4).Infof("Processing node event: %s", key)

// 	switch eventType {
// 	case "node-change":
// 		// Trigger full resync after a short delay to batch multiple node changes
// 		time.AfterFunc(500*time.Millisecond, func() {
// 			if sc.nodeEventQueue.Len() == 0 {
// 				klog.V(4).Infof("Triggering shard sync after node changes")
// 				sc.syncShards()
// 			}
// 		})

// 	case "global-sync-trigger":
// 		sc.syncShards()

// 	case "node-deleted":
// 		// Handle node deletion immediately
// 		sc.handleNodeDeletion(key)
// 	}

// 	return true
// }

// removeNodeState removes node state from cache
// func (sc *ShardingController) removeNodeState(nodeName string) {
// 	sc.metricsMutex.Lock()
// 	defer sc.metricsMutex.Unlock()

// 	// Check if node exists in cache
// 	if _, exists := sc.nodeMetricsCache[nodeName]; exists {
// 		delete(sc.nodeMetricsCache, nodeName)
// 		klog.V(4).Infof("Removed node state for %s from cache", nodeName)
// 	}

// 	// Also check and clean up assignment cache if needed
// 	sc.cacheMutex.Lock()
// 	defer sc.cacheMutex.Unlock()

// 	if sc.assignmentCache != nil {
// 		// Check if this node was in any assignments
// 		nodeInAssignments := false
// 		for schedulerName, assignment := range sc.assignmentCache.Assignments {
// 			for i, node := range assignment.NodesDesired {
// 				if node == nodeName {
// 					// Create a new slice without this node
// 					newNodes := make([]string, 0, len(assignment.NodesDesired)-1)
// 					newNodes = append(newNodes, assignment.NodesDesired[:i]...)
// 					newNodes = append(newNodes, assignment.NodesDesired[i+1:]...)

// 					// Update assignment
// 					updatedAssignment := *assignment
// 					updatedAssignment.NodesDesired = newNodes
// 					sc.assignmentCache.Assignments[schedulerName] = &updatedAssignment

// 					nodeInAssignments = true
// 					klog.V(4).Infof("Removed node %s from assignment for %s", nodeName, schedulerName)
// 				}
// 			}
// 		}

// 		if nodeInAssignments {
// 			// Invalidate cache by setting old timestamp
// 			sc.assignmentCache.Timestamp = time.Now().Add(-maxAssignmentCacheRetention)
// 			klog.V(4).Infof("Invalidated assignment cache due to node deletion")
// 		}
// 	}
// }

// updateNodeState updates node state in cache
// func (sc *ShardingController) updateNodeState(node *corev1.Node) {
// 	nodeName := node.Name
// 	// Recalculate utilization
// 	util, err := sc.calculateNodeUtilization(nodeName)
// 	if err != nil {
// 		klog.Warningf("Failed to calculate utilization for node %s: %v", nodeName, err)
// 		return
// 	}

// 	// Update node state
// 	sc.updateNodeUtilization(nodeName, util)
// }

// snapshotNodeStates creates a snapshot of current node states
// func (sc *ShardingController) snapshotNodeStates() map[string]*NodeMetrics {
// 	sc.metricsMutex.RLock()
// 	defer sc.metricsMutex.RUnlock()

// 	snapshot := make(map[string]*NodeMetrics)
// 	for name, state := range sc.nodeMetricsCache {
// 		// Deep copy
// 		copied := *state
// 		snapshot[name] = &copied
// 	}

// 	return snapshot
// }
