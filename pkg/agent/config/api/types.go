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

package api

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VolcanoAgentConfig include global and node colocation config.
type VolcanoAgentConfig struct {
	// GlobalConfig is a global config for all nodes.
	GlobalConfig *ColocationConfig `json:"globalConfig,omitempty"`

	// NodesConfig will overwrite GlobalConfig for selector matched nodes, which is usually nodePool level.
	NodesConfig []NodesConfig `json:"nodesConfig,omitempty"`
}

// NodeLabelConfig provides global defaults for node colocation/oversubscription.
// These values can be overridden by node labels (volcano.sh/colocation and volcano.sh/oversubscription).
type NodeLabelConfig struct {
	// NodeColocationEnable enables node colocation globally. Can be overridden by node label.
	NodeColocationEnable *bool `json:"nodeColocationEnable,omitempty"`
	// NodeOverSubscriptionEnable enables node oversubscription globally. Can be overridden by node label.
	NodeOverSubscriptionEnable *bool `json:"nodeOverSubscriptionEnable,omitempty"`
}

type NodesConfig struct {
	// nodes that match label selector will apply current configuration
	Selector         *metav1.LabelSelector `json:"selector,omitempty"`
	ColocationConfig `json:",inline"`
}

type ColocationConfig struct {
	// NodeLabelConfig provides global defaults, can be overridden by node labels
	NodeLabelConfig *NodeLabelConfig `json:"nodeLabelConfig,omitempty"`

	// cpu qos related config.
	CPUQosConfig *CPUQos `json:"cpuQosConfig,omitempty" configKey:"CPUQoS"`

	// cpu burst related config.
	CPUBurstConfig *CPUBurst `json:"cpuBurstConfig,omitempty" configKey:"CPUBurst"`

	// memory qos related config.
	MemoryQosConfig *MemoryQos `json:"memoryQosConfig,omitempty" configKey:"MemoryQoS"`

	// memory qos v2 related config.
	MemoryQosV2Config *MemoryQos `json:"memoryQosV2Config,omitempty" configKey:"MemoryQoSV2"`

	// network qos related config.
	NetworkQosConfig *NetworkQos `json:"networkQosConfig,omitempty" configKey:"NetworkQoS"`

	// overSubscription related config.
	OverSubscriptionConfig *OverSubscription `json:"overSubscriptionConfig,omitempty" configKey:"OverSubscription"`

	// Evicting related config.
	EvictingConfig *Evicting `json:"evictingConfig,omitempty" configKey:"Evicting"`

	// cpuThrottling related config
	CPUThrottlingConfig *CPUThrottling `json:"cpuThrottlingConfig,omitempty" configKey:"CPUThrottling"`
}

type CPUQos struct {
	// Enable CPUQos or not.
	Enable *bool `json:"enable,omitempty"`
}

type CPUBurst struct {
	// Enable CPUBurst or not.
	Enable *bool `json:"enable,omitempty"`
}

type MemoryQos struct {
	// Enable MemoryQos or not.
	Enable *bool `json:"enable,omitempty"`
}

type NetworkQos struct {
	// Enable NetworkQos or not.
	Enable *bool `json:"enable,omitempty"`
	// OnlineBandwidthWatermarkPercent presents the online bandwidth threshold percent.
	OnlineBandwidthWatermarkPercent *int `json:"onlineBandwidthWatermarkPercent,omitempty"`
	// OfflineLowBandwidthPercent presents the offline low bandwidth threshold percent.
	OfflineLowBandwidthPercent *int `json:"offlineLowBandwidthPercent,omitempty"`
	// OfflineHighBandwidthPercent presents the offline high bandwidth threshold percent.
	OfflineHighBandwidthPercent *int `json:"offlineHighBandwidthPercent,omitempty"`
	// QoSCheckInterval presents the network Qos checkout interval
	QoSCheckInterval *int `json:"qosCheckInterval,omitempty"`
}

type OverSubscription struct {
	// Enable OverSubscription or not.
	Enable *bool `json:"enable,omitempty"`
	// OverSubscriptionTypes defines over subscription types, such as cpu,memory.
	OverSubscriptionTypes *string `json:"overSubscriptionTypes,omitempty"`
}

type Evicting struct {
	// EvictingCPUHighWatermark defines the high watermark percent of cpu usage when evicting offline pods.
	EvictingCPUHighWatermark *int `json:"evictingCPUHighWatermark,omitempty"`
	// EvictingMemoryHighWatermark defines the high watermark percent of memory usage when evicting offline pods.
	EvictingMemoryHighWatermark *int `json:"evictingMemoryHighWatermark,omitempty"`
	// EvictingCPULowWatermark defines the low watermark percent of cpu usage when the node recover schedule pods.
	EvictingCPULowWatermark *int `json:"evictingCPULowWatermark,omitempty"`
	// EvictingMemoryLowWatermark defines the low watermark percent of memory usage when the node could recover schedule pods.
	EvictingMemoryLowWatermark *int `json:"evictingMemoryLowWatermark,omitempty"`
	// MonitorInterval defines the interval (in seconds) for monitoring node resource usage.
	MonitorInterval *int `json:"monitorInterval,omitempty"`
	// HighUsageCountLimit defines how many consecutive high usage detections trigger eviction.
	HighUsageCountLimit *int `json:"highUsageCountLimit,omitempty"`
}

type CPUThrottling struct {
	Enable *bool `json:"enable,omitempty"`
	// CPUThrottlingThreshold defines the threshold percent of CPU usage when CPU throttling should begin.
	CPUThrottlingThreshold *int `json:"cpuThrottlingThreshold,omitempty"`
	// CPUJitterLimitPercent defines the percentage range of allowed CPU usage variation.
	// Within this range, CPU throttle modifications for BestEffort pods will not be triggered.
	CPUJitterLimitPercent *int `json:"cpuJitterLimitPercent,omitempty"`
	// CPURecoverLimitPercent defines the maximum percent CPU quota increase allowed per interval.
	CPURecoverLimitPercent *int `json:"cpuRecoverLimitPercent,omitempty"`
}
