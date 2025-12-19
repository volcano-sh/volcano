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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	POD_SOURCE         = "pod-controller"
	POD_ADD_EVENT      = "pod-added"
	POD_SCHEDULE_EVENT = "pod-scheduled"
	POD_REMOVE_EVENT   = "pod-removed"
	POD_DELETE_EVENT   = "pod-deleted"
	POD_CHANGE_EVENT   = "pod-changed"
)

// addPod handles pod addition events
func (sc *ShardingController) addPod(obj interface{}) {
	pod := sc.getPodFromObject(obj)
	if pod == nil || pod.Spec.NodeName == "" {
		return
	}

	sc.enqueueNodeEvent(pod.Spec.NodeName, POD_ADD_EVENT, POD_SOURCE)
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
		sc.enqueueNodeEvent(newPod.Spec.NodeName, POD_SCHEDULE_EVENT, POD_SOURCE)
		return
	}

	// Pod moved between nodes
	if oldPod.Spec.NodeName != newPod.Spec.NodeName && newPod.Spec.NodeName != "" {
		if oldPod.Spec.NodeName != "" {
			sc.enqueueNodeEvent(oldPod.Spec.NodeName, POD_REMOVE_EVENT, POD_SOURCE)
		}
		sc.enqueueNodeEvent(newPod.Spec.NodeName, POD_ADD_EVENT, POD_SOURCE)
		return
	}

	// Resource requests changed
	if sc.podResourcesChanged(oldPod, newPod) {
		sc.enqueueNodeEvent(newPod.Spec.NodeName, POD_CHANGE_EVENT, POD_SOURCE)
	}
}

// deletePod handles pod deletion events
func (sc *ShardingController) deletePod(obj interface{}) {
	pod := sc.getPodFromObject(obj)
	if pod == nil || pod.Spec.NodeName == "" {
		return
	}

	sc.enqueueNodeEvent(pod.Spec.NodeName, POD_DELETE_EVENT, POD_SOURCE)
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
