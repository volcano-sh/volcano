package sharding

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// addPod handles pod addition events
func (sc *ShardingController) addPod(obj interface{}) {
	pod := sc.getPodFromObject(obj)
	if pod == nil || pod.Spec.NodeName == "" {
		return
	}

	sc.enqueueNodeEvent(pod.Spec.NodeName, "pod-added", "pod-controller")
}

// updatePod handles pod update events
func (sc *ShardingController) updatePod(oldObj, newObj interface{}) {
	oldPod := sc.getPodFromObject(oldObj)
	newPod := sc.getPodFromObject(newObj)

	if oldPod == nil || newPod == nil {
		return
	}

	// Pod scheduled to a node
	if oldPod.Spec.NodeName == "" && newPod.Spec.NodeName != "" {
		sc.enqueueNodeEvent(newPod.Spec.NodeName, "pod-scheduled", "pod-controller")
		return
	}

	// Pod moved between nodes
	if oldPod.Spec.NodeName != newPod.Spec.NodeName && newPod.Spec.NodeName != "" {
		if oldPod.Spec.NodeName != "" {
			sc.enqueueNodeEvent(oldPod.Spec.NodeName, "pod-removed", "pod-controller")
		}
		sc.enqueueNodeEvent(newPod.Spec.NodeName, "pod-added", "pod-controller")
		return
	}

	// Resource requests changed
	if sc.podResourcesChanged(oldPod, newPod) {
		sc.enqueueNodeEvent(newPod.Spec.NodeName, "pod-resources-changed", "pod-controller")
	}
}

// deletePod handles pod deletion events
func (sc *ShardingController) deletePod(obj interface{}) {
	pod := sc.getPodFromObject(obj)
	if pod == nil || pod.Spec.NodeName == "" {
		return
	}

	sc.enqueueNodeEvent(pod.Spec.NodeName, "pod-deleted", "pod-controller")
}

// // addPod handles pod addition events
// func (sc *ShardingController) addPod(obj interface{}) {
// 	pod, ok := obj.(*corev1.Pod)
// 	if !ok {
// 		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
// 		if !ok {
// 			klog.Errorf("Cannot convert to *corev1.Pod: %T", obj)
// 			return
// 		}
// 		pod, ok = tombstone.Obj.(*corev1.Pod)
// 		if !ok {
// 			klog.Errorf("Tombstone contained object that is not a Pod: %T", obj)
// 			return
// 		}
// 	}

// 	sc.handlePodEvent(pod, "added")
// }

// // updatePod handles pod update events
// func (sc *ShardingController) updatePod(oldObj, newObj interface{}) {
// 	newPod, ok := newObj.(*corev1.Pod)
// 	if !ok {
// 		klog.Errorf("Cannot convert newObj to *corev1.Pod: %T", newObj)
// 		return
// 	}

// 	oldPod, ok := oldObj.(*corev1.Pod)
// 	if !ok {
// 		klog.Errorf("Cannot convert oldObj to *corev1.Pod: %T", oldObj)
// 		return
// 	}

// 	// Only process if pod is scheduled
// 	if oldPod.Spec.NodeName == "" && newPod.Spec.NodeName != "" {
// 		sc.handlePodEvent(newPod, "scheduled")
// 		return
// 	}

// 	// Only process if pod moved between nodes
// 	if oldPod.Spec.NodeName != "" && oldPod.Spec.NodeName != newPod.Spec.NodeName {
// 		// Handle old node first
// 		sc.handlePodEvent(oldPod, "removed")
// 		// Then handle new node
// 		sc.handlePodEvent(newPod, "added")
// 		return
// 	}

// 	// Only process if resource requests changed
// 	if sc.podResourcesChanged(oldPod, newPod) {
// 		sc.handlePodEvent(newPod, "resources-changed")
// 	}
// }

// // deletePod handles pod deletion events
// func (sc *ShardingController) deletePod(obj interface{}) {
// 	pod, ok := obj.(*corev1.Pod)
// 	if !ok {
// 		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
// 		if !ok {
// 			klog.Errorf("Cannot convert to *corev1.Pod: %T", obj)
// 			return
// 		}
// 		pod, ok = tombstone.Obj.(*corev1.Pod)
// 		if !ok {
// 			klog.Errorf("Tombstone contained object that is not a Pod: %T", obj)
// 			return
// 		}
// 	}

// 	if pod.Spec.NodeName != "" {
// 		sc.handlePodEvent(pod, "deleted")
// 	}
// }

// podResourcesChanged checks if pod resource requests changed
func (sc *ShardingController) podResourcesChanged(oldPod, newPod *corev1.Pod) bool {
	if len(oldPod.Spec.Containers) != len(newPod.Spec.Containers) {
		return true
	}

	for i, oldContainer := range oldPod.Spec.Containers {
		newContainer := newPod.Spec.Containers[i]

		oldCPU := oldContainer.Resources.Requests.Cpu().MilliValue()
		newCPU := newContainer.Resources.Requests.Cpu().MilliValue()
		if oldCPU != newCPU {
			return true
		}

		oldMemory := oldContainer.Resources.Requests.Memory().Value()
		newMemory := newContainer.Resources.Requests.Memory().Value()
		if oldMemory != newMemory {
			return true
		}
	}

	return false
}

// getPodFromObject safely extracts a Pod from an object
func (sc *ShardingController) getPodFromObject(obj interface{}) *corev1.Pod {
	switch obj := obj.(type) {
	case *corev1.Pod:
		return obj
	case cache.DeletedFinalStateUnknown:
		// Handle tombstone objects
		if pod, ok := obj.Obj.(*corev1.Pod); ok {
			return pod
		}
		klog.Warningf("DeletedFinalStateUnknown contained non-Pod object: %T", obj.Obj)
		return nil
	default:
		klog.Warningf("Unexpected object type in pod event handler: %T", obj)
		return nil
	}
}

// // addPod handles pod addition events
// func (sc *ShardingController) addPod(obj interface{}) {
// 	pod := sc.getPodFromObject(obj)
// 	if pod == nil || pod.Spec.NodeName == "" {
// 		return
// 	}

// 	klog.V(5).Infof("Pod added to node %s: %s/%s", pod.Spec.NodeName, pod.Namespace, pod.Name)
// 	sc.handlePodEvent(pod.Spec.NodeName, "pod-added")
// }

// // updatePod handles pod update events
// func (sc *ShardingController) updatePod(oldObj, newObj interface{}) {
// 	oldPod := sc.getPodFromObject(oldObj)
// 	newPod := sc.getPodFromObject(newObj)

// 	if oldPod == nil || newPod == nil {
// 		return
// 	}

// 	// Pod scheduled to a node
// 	if oldPod.Spec.NodeName == "" && newPod.Spec.NodeName != "" {
// 		klog.V(5).Infof("Pod %s/%s scheduled to node %s", newPod.Namespace, newPod.Name, newPod.Spec.NodeName)
// 		sc.handlePodEvent(newPod.Spec.NodeName, "pod-scheduled")
// 		return
// 	}

// 	// Pod moved between nodes
// 	if oldPod.Spec.NodeName != newPod.Spec.NodeName && newPod.Spec.NodeName != "" {
// 		klog.V(5).Infof("Pod %s/%s moved from %s to %s", newPod.Namespace, newPod.Name, oldPod.Spec.NodeName, newPod.Spec.NodeName)
// 		if oldPod.Spec.NodeName != "" {
// 			sc.handlePodEvent(oldPod.Spec.NodeName, "pod-removed")
// 		}
// 		sc.handlePodEvent(newPod.Spec.NodeName, "pod-added")
// 		return
// 	}

// 	// Resource requests changed
// 	if sc.podResourcesChanged(oldPod, newPod) {
// 		klog.V(5).Infof("Pod %s/%s resources changed on node %s", newPod.Namespace, newPod.Name, newPod.Spec.NodeName)
// 		sc.handlePodEvent(newPod.Spec.NodeName, "pod-resources-changed")
// 	}
// }

// // deletePod handles pod deletion events
// func (sc *ShardingController) deletePod(obj interface{}) {
// 	pod := sc.getPodFromObject(obj)
// 	if pod == nil {
// 		return
// 	}

// 	if pod.Spec.NodeName == "" {
// 		return
// 	}

// 	klog.V(5).Infof("Pod deleted from node %s: %s/%s", pod.Spec.NodeName, pod.Namespace, pod.Name)
// 	sc.handlePodEvent(pod.Spec.NodeName, "pod-deleted")
// }

// func (sc *ShardingController) handlePodEvent(nodeName, eventType string) {
// 	klog.V(4).Infof("handling pod event '%s' for node %s", eventType, nodeName)

// 	// 先确保所有节点状态是最新的
// 	sc.ensureNodeStatesUpdated()

// 	// Recalculate utilization
// 	util, err := sc.calculateNodeUtilization(nodeName)
// 	if err != nil {
// 		klog.Warningf("Failed to calculate utilization for node %s: %v", nodeName, err)
// 		return
// 	}

// 	// Update node state
// 	sc.updateNodeUtilization(nodeName, util)

// 	// // Check if utilization changed significantly
// 	// if sc.isUtilizationSignificantlyChanged(nodeName, util) {
// 	// 	klog.V(4).Infof("Node %s utilization changed significantly (%s), triggering resharding",
// 	// 		nodeName, eventType)
// 	// 	sc.enqueueSyncEvent()
// 	// }
// }

// // pkg/controllers/sharding/pod_events.go
// func (sc *ShardingController) podResourcesChanged(oldPod, newPod *corev1.Pod) bool {
// 	// Quick check: different number of containers
// 	if len(oldPod.Spec.Containers) != len(newPod.Spec.Containers) {
// 		return true
// 	}

// 	// Compare each container's resources
// 	for i, oldContainer := range oldPod.Spec.Containers {
// 		newContainer := newPod.Spec.Containers[i]

// 		// Compare CPU requests
// 		oldCPURequest := oldContainer.Resources.Requests.Cpu()
// 		newCPURequest := newContainer.Resources.Requests.Cpu()
// 		if !oldCPURequest.Equal(*newCPURequest) {
// 			klog.V(5).Infof("CPU request changed for container %s in pod %s: %v -> %v",
// 				oldContainer.Name, newPod.Name, oldCPURequest, newCPURequest)
// 			return true
// 		}

// 		// Compare memory requests
// 		oldMemoryRequest := oldContainer.Resources.Requests.Memory()
// 		newMemoryRequest := newContainer.Resources.Requests.Memory()
// 		if !oldMemoryRequest.Equal(*newMemoryRequest) {
// 			klog.V(5).Infof("Memory request changed for container %s in pod %s: %v -> %v",
// 				oldContainer.Name, newPod.Name, oldMemoryRequest, newMemoryRequest)
// 			return true
// 		}

// 		// Compare CPU limits (optional, but some workloads are sensitive to this)
// 		oldCPULimit := oldContainer.Resources.Limits.Cpu()
// 		newCPULimit := newContainer.Resources.Limits.Cpu()
// 		if oldCPULimit != nil && newCPULimit != nil && !oldCPULimit.Equal(*newCPULimit) {
// 			klog.V(5).Infof("CPU limit changed for container %s in pod %s: %v -> %v",
// 				oldContainer.Name, newPod.Name, oldCPULimit, newCPULimit)
// 			return true
// 		}
// 	}

// 	return false
// }

func (sc *ShardingController) enqueueSyncEvent() {
	sc.nodeEventQueue.Add("global-sync-trigger")
}
