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
	"time"

	"github.com/imdario/mergo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
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
        "nodeLabelConfig":{
            "nodeColocationEnable":true,
            "nodeOverSubscriptionEnable":false
        },
        "cpuBurstConfig":{
            "enable":false
        },
		"cpuQosConfig":{
			"enable":false
		},
		"memoryQosConfig":{
			"enable":false
		},
        "networkQosConfig":{
            "enable":false,
            "onlineBandwidthWatermarkPercent":80,
            "offlineLowBandwidthPercent":10,
            "offlineHighBandwidthPercent":40,
            "qosCheckInterval":10000000
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
			// Open colocation by default globally, however, fine-grained features such as CPU QoS and CPU burst need to be manually enabled,
			// which depends on the OS type and kernel version.
			NodeColocationEnable:       utilpointer.Bool(true),
			NodeOverSubscriptionEnable: utilpointer.Bool(false),
		},
		// CPU/Memory/Network Qos are disabled by default, these features need a custom operating system.
		// CPU burst is also disabled by default, CPU burst currently is integrated into Linux kernel, but requiring Linux kernel version >= 5.14.
		CPUQosConfig:    &api.CPUQos{Enable: utilpointer.Bool(false)},
		CPUBurstConfig:  &api.CPUBurst{Enable: utilpointer.Bool(false)},
		MemoryQosConfig: &api.MemoryQos{Enable: utilpointer.Bool(false)},
		NetworkQosConfig: &api.NetworkQos{
			Enable:                          utilpointer.Bool(false),
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

func SetDefaultVolcanoAgentConfig(cfg *api.VolcanoAgentConfig) {
	if cfg.GlobalConfig != nil {
		// TODO: is TimeBasedQoSPolicies only used in global config?
		for _, policy := range cfg.GlobalConfig.TimeBasedQoSPolicies {
			if policy.Enable == nil {
				policy.Enable = utilpointer.Bool(true)
			}
			if policy.CheckInterval == nil {
				policy.CheckInterval = utilpointer.Duration(15 * time.Second)
			}
		}
	}
}

func ValidateVolcanoAgentConfig(cfg *api.VolcanoAgentConfig) field.ErrorList {
	allErrs := field.ErrorList{}
	if cfg.GlobalConfig != nil {
		path := field.NewPath("globalConfig")
		for i, policy := range cfg.GlobalConfig.TimeBasedQoSPolicies {
			policyPath := path.Child("timeBasedQoSPolicies").Index(i)
			allErrs = append(allErrs, validateTimeBasedQoSPolicy(policy, policyPath)...)
		}
	}

	return allErrs
}

func validateTimeBasedQoSPolicy(policy *api.TimeBasedQoSPolicy, basePath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if policy.StartTime == nil {
		allErrs = append(allErrs, field.Required(basePath.Child("startTime"), "startTime is required"))
	} else if _, err := time.Parse("15:04", *policy.StartTime); err != nil {
		allErrs = append(allErrs, field.Invalid(basePath.Child("startTime"), *policy.StartTime, "invalid time format, expected HH:MM"))
	}

	if policy.EndTime == nil {
		allErrs = append(allErrs, field.Required(basePath.Child("endTime"), "endTime is required"))
	} else if _, err := time.Parse("15:04", *policy.EndTime); err != nil {
		allErrs = append(allErrs, field.Invalid(basePath.Child("endTime"), *policy.EndTime, "invalid time format, expected HH:MM"))
	}

	if policy.TimeZone != nil {
		if _, err := time.LoadLocation(*policy.TimeZone); err != nil {
			allErrs = append(allErrs, field.Invalid(basePath.Child("timeZone"), *policy.TimeZone, "invalid timeZone"))
		}
	}

	if policy.Selector == nil {
		allErrs = append(allErrs, field.Required(basePath.Child("selector"), "selector is required"))
	} else {
		allErrs = append(allErrs, validation.ValidateLabelSelector(policy.Selector, validation.LabelSelectorValidationOptions{}, basePath.Child("selector"))...)
	}

	if policy.TargetQoSLevel == nil {
		allErrs = append(allErrs, field.Required(basePath.Child("targetQoSLevel"), "targetQoSLevel is required"))
	}

	if policy.CheckInterval != nil && *policy.CheckInterval <= 0 {
		allErrs = append(allErrs, field.Invalid(basePath.Child("checkInterval"), *policy.CheckInterval, "checkInterval must be greater than 0"))
	}

	return allErrs
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
