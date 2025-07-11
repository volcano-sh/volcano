/*
Copyright 2024 The Volcano Authors.

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

package apis

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
)

const (
	// OverSubscriptionTypesKey define the overSubscription resource types
	OverSubscriptionTypesKey = "volcano.sh/oversubscription-types"

	// NodeOverSubscriptionCPUKey define the oversubscription cpu resource on the node
	NodeOverSubscriptionCPUKey = "volcano.sh/oversubscription-cpu"
	// NodeOverSubscriptionMemoryKey define the oversubscription memory resource on the node
	NodeOverSubscriptionMemoryKey = "volcano.sh/oversubscription-memory"

	// PodQosLevelKey define pod qos level, see pkg/agent/apis/extension/qos.go for specific values.
	PodQosLevelKey = "volcano.sh/qos-level"
	// PodEvictingKey define if the offline job is evicting
	PodEvictingKey = "volcano.sh/offline-job-evicting"
	// ColocationEnableNodeLabelKey is the label name for colocation,
	// indicates whether the node enable colocation, set "true" or "false", default is "false"
	ColocationEnableNodeLabelKey = "volcano.sh/colocation"
	// OverSubscriptionNodeLabelKey define whether a node is oversubscription node.
	OverSubscriptionNodeLabelKey = "volcano.sh/oversubscription"

	// NetworkBandwidthRateAnnotationKey is the annotation key of network bandwidth rate, unit Mbps.
	NetworkBandwidthRateAnnotationKey = "volcano.sh/network-bandwidth-rate"

	// Deprecated:This is used to be compatible with old api.
	// PodEvictedOverSubscriptionCPUHighWaterMarkKey define the high watermark of cpu usage when evicting offline pods
	PodEvictedOverSubscriptionCPUHighWaterMarkKey = "volcano.sh/oversubscription-evicting-cpu-high-watermark"
	// Deprecated:This is used to be compatible with old api.
	// PodEvictedOverSubscriptionMemoryHighWaterMarkKey define the high watermark of memory usage when evicting offline pods
	PodEvictedOverSubscriptionMemoryHighWaterMarkKey = "volcano.sh/oversubscription-evicting-memory-high-watermark"
	// Deprecated:This is used to be compatible with old api.
	// PodEvictedOverSubscriptionCPULowWaterMarkKey define the low watermark of cpu usage when the node could overSubscription resources
	PodEvictedOverSubscriptionCPULowWaterMarkKey = "volcano.sh/oversubscription-evicting-cpu-low-watermark"
	// Deprecated:This is used to be compatible with old api.
	// PodEvictedOverSubscriptionMemoryLowWaterMarkKey define the low watermark of memory usage when the node could overSubscription resources
	PodEvictedOverSubscriptionMemoryLowWaterMarkKey = "volcano.sh/oversubscription-evicting-memory-low-watermark"

	// PodEvictedCPUHighWaterMarkKey define the high watermark of cpu usage when evicting offline pods
	PodEvictedCPUHighWaterMarkKey = "volcano.sh/evicting-cpu-high-watermark"
	// PodEvictedMemoryHighWaterMarkKey define the high watermark of memory usage when evicting offline pods
	PodEvictedMemoryHighWaterMarkKey = "volcano.sh/evicting-memory-high-watermark"
	// PodEvictedCPULowWaterMarkKey define the low watermark of cpu usage when the node could overSubscription resources
	PodEvictedCPULowWaterMarkKey = "volcano.sh/evicting-cpu-low-watermark"
	// PodEvictedMemoryLowWaterMarkKey define the low watermark of memory usage when the node could overSubscription resources
	PodEvictedMemoryLowWaterMarkKey = "volcano.sh/evicting-memory-low-watermark"

	// ResourceDefaultPrefix is the extended resource prefix.
	ResourceDefaultPrefix = "kubernetes.io/"

	// ColocationPolicyKey is the label key of node custom colocation policy.
	ColocationPolicyKey = "colocation-policy"
)

var (
	ExtendResourceCPU        = ResourceDefaultPrefix + "batch-cpu"
	extendResourceCPULock    sync.RWMutex
	ExtendResourceMemory     = ResourceDefaultPrefix + "batch-memory"
	extendResourceMemoryLock sync.RWMutex
)

func SetExtendResourceCPU(val string) {
	extendResourceCPULock.Lock()
	defer extendResourceCPULock.Unlock()
	ExtendResourceCPU = val
}
func GetExtendResourceCPU() corev1.ResourceName {
	extendResourceCPULock.RLock()
	defer extendResourceCPULock.RUnlock()
	return corev1.ResourceName(ExtendResourceCPU)
}
func SetExtendResourceMemory(val string) {
	extendResourceMemoryLock.Lock()
	defer extendResourceMemoryLock.Unlock()
	ExtendResourceMemory = val
}
func GetExtendResourceMemory() corev1.ResourceName {
	extendResourceMemoryLock.RLock()
	defer extendResourceMemoryLock.RUnlock()
	return corev1.ResourceName(ExtendResourceMemory)
}

var OverSubscriptionResourceTypes = []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory}

// GetOverSubscriptionResourceTypesIncludeExtendResources returns oversubscription resource types including extend resources.
func GetOverSubscriptionResourceTypesIncludeExtendResources() []corev1.ResourceName {
	return []corev1.ResourceName{
		corev1.ResourceCPU,
		corev1.ResourceMemory,
		GetExtendResourceCPU(),
		GetExtendResourceMemory(),
	}
}

// Resource mapping resource type and usage.
type Resource map[corev1.ResourceName]int64

// Watermark defines resource eviction watermark.
type Watermark map[corev1.ResourceName]int
