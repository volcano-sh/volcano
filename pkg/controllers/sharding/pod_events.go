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

func (sc *ShardingController) enqueueSyncEvent() {
	sc.nodeEventQueue.Add("global-sync-trigger")
}
