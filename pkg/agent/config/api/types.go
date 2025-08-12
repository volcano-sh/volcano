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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VolcanoAgentConfig include global and node colocation config.
type VolcanoAgentConfig struct {
	// GlobalConfig is a global config for all nodes.
	GlobalConfig *ColocationConfig `json:"globalConfig,omitempty"`

	// NodesConfig will overwrite GlobalConfig for selector matched nodes, which is usually nodePool level.
	NodesConfig []NodesConfig `json:"nodesConfig,omitempty"`
}

// NodeLabelConfig does not support getting from configmap
type NodeLabelConfig struct {
	// NodeColocationEnable enables node colocation or not.
	NodeColocationEnable *bool
	// NodeColocationEnable enables node oversubscription or not.
	NodeOverSubscriptionEnable *bool
}

type NodesConfig struct {
	// nodes that match label selector will apply current configuration
	Selector         *metav1.LabelSelector `json:"selector,omitempty"`
	ColocationConfig `json:",inline"`
}

type ColocationConfig struct {
	// got from node labels
	NodeLabelConfig *NodeLabelConfig `json:"-"`

	// cpu qos related config.
	CPUQosConfig *CPUQos `json:"cpuQosConfig,omitempty" configKey:"CPUQoS"`

	// cpu burst related config.
	CPUBurstConfig *CPUBurst `json:"cpuBurstConfig,omitempty" configKey:"CPUBurst"`

	// memory qos related config.
	MemoryQosConfig *MemoryQos `json:"memoryQosConfig,omitempty" configKey:"MemoryQoS"`

	// network qos related config.
	NetworkQosConfig *NetworkQos `json:"networkQosConfig,omitempty" configKey:"NetworkQoS"`

	// overSubscription related config.
	OverSubscriptionConfig *OverSubscription `json:"overSubscriptionConfig,omitempty" configKey:"OverSubscription"`

	// Evicting related config.
	EvictingConfig *Evicting `json:"evictingConfig,omitempty" configKey:"Evicting"`

	// TimeBasedQoSPolicies is a list of time based QoS policies.
	TimeBasedQoSPolicies []*TimeBasedQoSPolicy `json:"timeBasedQoSPolicies,omitempty" configKey:"TimeBasedQoSPolicies"`
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
}

// TODO: need to set default values for TimeBasedQoSPolicy
type TimeBasedQoSPolicy struct {
	// Enable indicates whether this TimeBasedQoSPolicy is enabled.
	Enable *bool `json:"enable,omitempty"`
	// Name is the name of the TimeBasedQoSPolicy.
	Name string `json:"name,omitempty"`
	// StartTime is the start time of the policy, format: "HH:MM", e.g., "08:00".
	StartTime *string `json:"startTime,omitempty"`
	// EndTime is the end time of the policy, format: "HH:MM", e.g., "18:00".
	EndTime *string `json:"endTime,omitempty"`
	// TimeZone specifies the timezone for the policy, e.g., "UTC", "Asia/Shanghai". If timeZone is not specified, the local timezone will be used.
	TimeZone *string `json:"timeZone,omitempty"`
	// Selector is used to select pods that this policy applies to.
	Selector *metav1.LabelSelector `json:"selector,omitempty"`
	// TargetQoSLevel defines the target QoS level to be applied during the specified time period.
	TargetQoSLevel *int `json:"targetQoSLevel,omitempty"`
	// CheckInterval specifies how often to check if the policy should be applied, format is a duration string, e.g., "30s", "1m". Default is 15s.
	CheckInterval *time.Duration `json:"checkInterval,omitempty"`
}
