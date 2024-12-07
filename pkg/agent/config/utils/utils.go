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

package utils

import (
	"reflect"

	"github.com/imdario/mergo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	"volcano.sh/volcano/pkg/agent/config/api"
	utilnode "volcano.sh/volcano/pkg/agent/utils/node"
)

const (
	ConfigMapName       = "volcano-agent-configuration"
	ColocationConfigKey = "colocation-config"
	ObjectNameField     = "metadata.name"
)

const (
	// Network Qos config
	DefaultOnlineBandwidthWatermarkPercent = 80
	DefaultOfflineLowBandwidthPercent      = 10
	DefaultOfflineHighBandwidthPercent     = 40
	DefaultNetworkQoSInterval              = 10000000 // 1000000 纳秒 = 10 毫秒

	// OverSubscription config
	DefaultOverSubscriptionTypes = "cpu,memory"

	// Evicting config
	DefaultEvictingCPUHighWatermark    = 80
	DefaultEvictingMemoryHighWatermark = 60
	DefaultEvictingCPULowWatermark     = 30
	DefaultEvictingMemoryLowWatermark  = 30
)

const (
	DefaultCfg = `
{
    "globalConfig":{
        "cpuBurstConfig":{
            "enable":true
        },
        "networkQosConfig":{
            "enable":true,
            "onlineBandwidthWatermarkPercent":80,
            "offlineLowBandwidthPercent":10,
            "offlineHighBandwidthPercent":40,
            "qosCheckInterval": 10000000
        },
        "overSubscriptionConfig":{
            "enable":true,
            "overSubscriptionTypes":"cpu,memory"
        },
        "evictingConfig":{
            "evictingCPUHighWatermark":80,
            "evictingMemoryHighWatermark":60,
            "evictingCPULowWatermark":30,
            "evictingMemoryLowWatermark":30
        }
    }
}
`
)

// DefaultColocationConfig is the default colocation config.
func DefaultColocationConfig() *api.ColocationConfig {
	return &api.ColocationConfig{
		NodeLabelConfig: &api.NodeLabelConfig{
			NodeColocationEnable:       utilpointer.Bool(false),
			NodeOverSubscriptionEnable: utilpointer.Bool(false),
		},
		CPUQosConfig:    &api.CPUQos{Enable: utilpointer.Bool(true)},
		CPUBurstConfig:  &api.CPUBurst{Enable: utilpointer.Bool(true)},
		MemoryQosConfig: &api.MemoryQos{Enable: utilpointer.Bool(true)},
		NetworkQosConfig: &api.NetworkQos{
			Enable:                          utilpointer.Bool(true),
			OnlineBandwidthWatermarkPercent: utilpointer.Int(DefaultOnlineBandwidthWatermarkPercent),
			OfflineLowBandwidthPercent:      utilpointer.Int(DefaultOfflineLowBandwidthPercent),
			OfflineHighBandwidthPercent:     utilpointer.Int(DefaultOfflineHighBandwidthPercent),
			QoSCheckInterval:                utilpointer.Int(DefaultNetworkQoSInterval),
		},
		OverSubscriptionConfig: &api.OverSubscription{
			Enable:                utilpointer.Bool(true),
			OverSubscriptionTypes: utilpointer.String(DefaultOverSubscriptionTypes),
		},
		EvictingConfig: &api.Evicting{
			EvictingCPUHighWatermark:    utilpointer.Int(DefaultEvictingCPUHighWatermark),
			EvictingMemoryHighWatermark: utilpointer.Int(DefaultEvictingMemoryHighWatermark),
			EvictingCPULowWatermark:     utilpointer.Int(DefaultEvictingCPULowWatermark),
			EvictingMemoryLowWatermark:  utilpointer.Int(DefaultEvictingMemoryLowWatermark),
		},
	}
}

// DefaultVolcanoAgentConfig returns default volcano agent config.
func DefaultVolcanoAgentConfig() *api.VolcanoAgentConfig {
	return &api.VolcanoAgentConfig{
		GlobalConfig: DefaultColocationConfig(),
	}
}

type nullTransformer struct {
}

// Transformer temporary solution, waiting https://github.com/imdario/mergo/issues/131 to be fixed.
func (t *nullTransformer) Transformer(typ reflect.Type) func(dst, src reflect.Value) error {
	if typ.Kind() == reflect.Pointer && typ.Elem().Kind() != reflect.Struct {
		return func(dst, src reflect.Value) error {
			if dst.CanSet() && !src.IsNil() {
				dst.Set(src)
			}
			return nil
		}
	}
	return nil
}

func MergerCfg(fullConfig *api.VolcanoAgentConfig, node *corev1.Node) (*api.ColocationConfig, error) {
	mergedCfg := DefaultColocationConfig()
	defaultCfg := DefaultColocationConfig()
	if fullConfig == nil || fullConfig.GlobalConfig == nil {
		klog.InfoS("full or global config is nil, use default config")
		return defaultCfg, nil
	}

	if err := mergo.Merge(mergedCfg, fullConfig.GlobalConfig, mergo.WithOverride, mergo.WithTransformers(&nullTransformer{})); err != nil {
		klog.ErrorS(err, "Failed to merge default and global config, use default config")
		return defaultCfg, nil
	}

	nodeCfg := &api.ColocationConfig{}
	for idx := range fullConfig.NodesConfig {
		selector, err := metav1.LabelSelectorAsSelector(fullConfig.NodesConfig[idx].Selector)
		if err != nil || !selector.Matches(labels.Set(node.Labels)) {
			continue
		}
		// choose the last config if multi labels matched.
		nodeCfg = &fullConfig.NodesConfig[idx].ColocationConfig
	}

	if err := mergo.Merge(mergedCfg, nodeCfg, mergo.WithOverride, mergo.WithTransformers(&nullTransformer{})); err != nil {
		klog.ErrorS(err, "Failed to merge node config")
		return mergedCfg, err
	}

	enableOverSubscription := utilpointer.Bool(utilnode.IsNodeSupportOverSubscription(node))
	mergedCfg.NodeLabelConfig.NodeColocationEnable = utilpointer.Bool(utilnode.IsNodeSupportColocation(node) || *enableOverSubscription)
	mergedCfg.NodeLabelConfig.NodeOverSubscriptionEnable = enableOverSubscription

	validateErr := utilerrors.NewAggregate(mergedCfg.Validate())
	if validateErr != nil {
		klog.ErrorS(validateErr, "Config is invalid, keep original config")
		return mergedCfg, validateErr
	}

	return mergedCfg, nil
}
