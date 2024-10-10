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
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilpointer "k8s.io/utils/pointer"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/config/api"
)

func TestMergerCfg(t *testing.T) {
	tests := []struct {
		name       string
		volcanoCfg *api.VolcanoAgentConfig
		node       *corev1.Node
		wantCfg    *api.ColocationConfig
		wantErr    bool
	}{
		{
			name: "disable cpu qos && disable cpu burst",
			volcanoCfg: &api.VolcanoAgentConfig{
				GlobalConfig: &api.ColocationConfig{
					CPUQosConfig:   &api.CPUQos{Enable: utilpointer.Bool(false)},
					CPUBurstConfig: &api.CPUBurst{Enable: utilpointer.Bool(false)},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{},
			},
			wantCfg: disableCPUBurst(disableCPUBurst(disableCPUQos(DefaultColocationConfig()))),
			wantErr: false,
		},

		{
			name: "disable memory qos && disable overSubscription",
			volcanoCfg: &api.VolcanoAgentConfig{
				GlobalConfig: &api.ColocationConfig{
					MemoryQosConfig:        &api.MemoryQos{Enable: utilpointer.Bool(false)},
					OverSubscriptionConfig: &api.OverSubscription{Enable: utilpointer.Bool(false)},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						apis.ColocationEnableNodeLabelKey: "true",
						apis.OverSubscriptionNodeLabelKey: "true",
					},
				},
			},
			wantCfg: disableOverSubscription(disableMemoryQos(enableNodeOverSubscription(enableNodeColocation(DefaultColocationConfig())))),
			wantErr: false,
		},

		{
			name: "disable network qos",
			volcanoCfg: &api.VolcanoAgentConfig{
				GlobalConfig: &api.ColocationConfig{
					NetworkQosConfig: &api.NetworkQos{Enable: utilpointer.Bool(false)},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						apis.ColocationEnableNodeLabelKey: "true",
					},
				},
			},
			wantCfg: disableNetworkQos(enableNodeColocation(DefaultColocationConfig())),
			wantErr: false,
		},

		{
			name: "modify network qos",
			volcanoCfg: &api.VolcanoAgentConfig{
				GlobalConfig: &api.ColocationConfig{
					NetworkQosConfig: &api.NetworkQos{
						OnlineBandwidthWatermarkPercent: utilpointer.Int(15),
						OfflineLowBandwidthPercent:      utilpointer.Int(16),
						OfflineHighBandwidthPercent:     utilpointer.Int(17),
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						apis.OverSubscriptionNodeLabelKey: "true",
					},
				},
			},
			wantCfg: withNewNetworkQoS(enableNodeOverSubscription(enableNodeColocation(DefaultColocationConfig())), &api.NetworkQos{
				Enable:                          utilpointer.Bool(true),
				QoSCheckInterval:                utilpointer.Int(10000000),
				OnlineBandwidthWatermarkPercent: utilpointer.Int(15),
				OfflineLowBandwidthPercent:      utilpointer.Int(16),
				OfflineHighBandwidthPercent:     utilpointer.Int(17),
			}),
			wantErr: false,
		},

		{
			name: "modify network qos && OnlineBandwidthWatermarkPercent=0",
			volcanoCfg: &api.VolcanoAgentConfig{
				GlobalConfig: &api.ColocationConfig{
					NetworkQosConfig: &api.NetworkQos{
						OnlineBandwidthWatermarkPercent: utilpointer.Int(0),
						OfflineLowBandwidthPercent:      utilpointer.Int(18),
						OfflineHighBandwidthPercent:     utilpointer.Int(19),
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						apis.OverSubscriptionNodeLabelKey: "true",
					},
				},
			},
			wantCfg: withNewNetworkQoS(enableNodeOverSubscription(enableNodeColocation(DefaultColocationConfig())), &api.NetworkQos{
				Enable:                          utilpointer.Bool(true),
				QoSCheckInterval:                utilpointer.Int(10000000),
				OnlineBandwidthWatermarkPercent: utilpointer.Int(0),
				OfflineLowBandwidthPercent:      utilpointer.Int(18),
				OfflineHighBandwidthPercent:     utilpointer.Int(19),
			}),
			wantErr: true,
		},

		{
			name: "modify OverSubscription && OverSubscriptionTypes=\"\"",
			volcanoCfg: &api.VolcanoAgentConfig{
				GlobalConfig: &api.ColocationConfig{
					OverSubscriptionConfig: &api.OverSubscription{
						Enable:                utilpointer.Bool(true),
						OverSubscriptionTypes: utilpointer.String(""),
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						apis.OverSubscriptionNodeLabelKey: "true",
					},
				},
			},
			wantCfg: withNewOverSubscription(enableNodeOverSubscription(enableNodeColocation(DefaultColocationConfig())), &api.OverSubscription{
				Enable:                utilpointer.Bool(true),
				OverSubscriptionTypes: utilpointer.String(""),
			}),
			wantErr: false,
		},
		{
			name: "overwrite default config label selector",
			volcanoCfg: &api.VolcanoAgentConfig{
				GlobalConfig: DefaultColocationConfig(),
				NodesConfig: []api.NodesConfig{
					{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{
							"label-key": "label-value",
						}},
						ColocationConfig: api.ColocationConfig{EvictingConfig: &api.Evicting{
							EvictingCPUHighWatermark: utilpointer.Int(10),
						}},
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"label-key": "label-value",
					},
				},
			},
			wantCfg: withLabelSelector(DefaultColocationConfig()),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actualCfg, actualErr := MergerCfg(tc.volcanoCfg, tc.node)
			assert.Equal(t, tc.wantCfg, actualCfg, tc.name)
			assert.Equal(t, tc.wantErr, actualErr != nil, tc.name)
		})
	}
}

func withNewOverSubscription(config *api.ColocationConfig, overSubscription *api.OverSubscription) *api.ColocationConfig {
	config.OverSubscriptionConfig = overSubscription
	return config
}

func withNewNetworkQoS(config *api.ColocationConfig, networkqos *api.NetworkQos) *api.ColocationConfig {
	config.NetworkQosConfig = networkqos
	return config
}

func enableNodeColocation(config *api.ColocationConfig) *api.ColocationConfig {
	config.NodeLabelConfig.NodeColocationEnable = utilpointer.Bool(true)
	return config
}

func enableNodeOverSubscription(config *api.ColocationConfig) *api.ColocationConfig {
	config.NodeLabelConfig.NodeOverSubscriptionEnable = utilpointer.Bool(true)
	return config
}

func disableCPUQos(config *api.ColocationConfig) *api.ColocationConfig {
	config.CPUQosConfig.Enable = utilpointer.Bool(false)
	return config
}

func disableOverSubscription(config *api.ColocationConfig) *api.ColocationConfig {
	config.OverSubscriptionConfig.Enable = utilpointer.Bool(false)
	return config
}

func disableNetworkQos(config *api.ColocationConfig) *api.ColocationConfig {
	config.NetworkQosConfig.Enable = utilpointer.Bool(false)
	return config
}

func disableMemoryQos(config *api.ColocationConfig) *api.ColocationConfig {
	config.MemoryQosConfig.Enable = utilpointer.Bool(false)
	return config
}

func disableCPUBurst(config *api.ColocationConfig) *api.ColocationConfig {
	config.CPUBurstConfig.Enable = utilpointer.Bool(false)
	return config
}

func withLabelSelector(config *api.ColocationConfig) *api.ColocationConfig {
	config.EvictingConfig.EvictingCPUHighWatermark = utilpointer.Int(10)
	return config
}
