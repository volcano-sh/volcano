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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	utilpointer "k8s.io/utils/pointer"
)

func TestColocationConfigValidate(t *testing.T) {
	testCases := []struct {
		name          string
		colocationCfg *ColocationConfig
		expectedErr   []error
	}{
		{
			name: "legal configuration",
			colocationCfg: &ColocationConfig{
				NodeLabelConfig: &NodeLabelConfig{
					NodeColocationEnable:       utilpointer.Bool(false),
					NodeOverSubscriptionEnable: utilpointer.Bool(false),
				},
				CPUQosConfig:    &CPUQos{Enable: utilpointer.Bool(true)},
				CPUBurstConfig:  &CPUBurst{Enable: utilpointer.Bool(true)},
				MemoryQosConfig: &MemoryQos{Enable: utilpointer.Bool(true)},
				NetworkQosConfig: &NetworkQos{
					Enable:                          utilpointer.Bool(true),
					OnlineBandwidthWatermarkPercent: utilpointer.Int(10),
					OfflineLowBandwidthPercent:      utilpointer.Int(10),
					OfflineHighBandwidthPercent:     utilpointer.Int(20),
					QoSCheckInterval:                utilpointer.Int(100),
				},
				OverSubscriptionConfig: &OverSubscription{
					Enable:                utilpointer.Bool(true),
					OverSubscriptionTypes: utilpointer.String("cpu"),
				},
				EvictingConfig: &Evicting{
					EvictingCPUHighWatermark:    utilpointer.Int(20),
					EvictingMemoryHighWatermark: utilpointer.Int(20),
					EvictingCPULowWatermark:     utilpointer.Int(10),
					EvictingMemoryLowWatermark:  utilpointer.Int(10),
				},
			},
		},

		{
			name: "illegal NetworkQosConfig && negative parameters",
			colocationCfg: &ColocationConfig{
				NetworkQosConfig: &NetworkQos{
					Enable:                          utilpointer.Bool(true),
					OnlineBandwidthWatermarkPercent: utilpointer.Int(-10),
					OfflineLowBandwidthPercent:      utilpointer.Int(-20),
					OfflineHighBandwidthPercent:     utilpointer.Int(-20),
					QoSCheckInterval:                utilpointer.Int(-100),
				},
			},
			expectedErr: []error{errors.New(IllegalQoSCheckIntervalMsg), errors.New(IllegalOnlineBandwidthWatermarkPercentMsg),
				errors.New(IllegalOfflineHighBandwidthPercentMsg), errors.New(IllegalOfflineLowBandwidthPercentMsg)},
		},

		{
			name: "illegal NetworkQosConfig && OfflineLowBandwidthPercent higher than OfflineHighBandwidthPercent",
			colocationCfg: &ColocationConfig{
				NetworkQosConfig: &NetworkQos{
					Enable:                          utilpointer.Bool(true),
					OnlineBandwidthWatermarkPercent: utilpointer.Int(10),
					OfflineLowBandwidthPercent:      utilpointer.Int(20),
					OfflineHighBandwidthPercent:     utilpointer.Int(10),
					QoSCheckInterval:                utilpointer.Int(100),
				},
			},
			expectedErr: []error{errors.New(OfflineHighBandwidthPercentLessOfflineLowBandwidthPercentMsg)},
		},

		{
			name: "illegal NetworkQosConfig && OfflineLowBandwidthPercent higher than OfflineHighBandwidthPercent",
			colocationCfg: &ColocationConfig{
				NetworkQosConfig: &NetworkQos{
					Enable:                          utilpointer.Bool(true),
					OnlineBandwidthWatermarkPercent: utilpointer.Int(10),
					OfflineLowBandwidthPercent:      utilpointer.Int(20),
					OfflineHighBandwidthPercent:     utilpointer.Int(10),
					QoSCheckInterval:                utilpointer.Int(100),
				},
			},
			expectedErr: []error{errors.New(OfflineHighBandwidthPercentLessOfflineLowBandwidthPercentMsg)},
		},

		{
			name: "illegal EvictingConfig && negative parameters",
			colocationCfg: &ColocationConfig{
				EvictingConfig: &Evicting{
					EvictingCPUHighWatermark:    utilpointer.Int(-10),
					EvictingMemoryHighWatermark: utilpointer.Int(-10),
					EvictingCPULowWatermark:     utilpointer.Int(-20),
					EvictingMemoryLowWatermark:  utilpointer.Int(-20),
				},
			},
			expectedErr: []error{errors.New(IllegalEvictingCPUHighWatermark), errors.New(IllegalEvictingMemoryHighWatermark),
				errors.New(IllegalEvictingCPULowWatermark), errors.New(IllegalEvictingMemoryLowWatermark)},
		},

		{
			name: "illegal OverSubscriptionConfig",
			colocationCfg: &ColocationConfig{
				OverSubscriptionConfig: &OverSubscription{
					Enable:                utilpointer.Bool(true),
					OverSubscriptionTypes: utilpointer.String("fake"),
				},
			},
			expectedErr: []error{fmt.Errorf(IllegalOverSubscriptionTypes, "fake")},
		},
		{
			name: "cpu evicting low watermark higher high watermark",
			colocationCfg: &ColocationConfig{
				EvictingConfig: &Evicting{
					EvictingCPUHighWatermark:    utilpointer.Int(20),
					EvictingMemoryHighWatermark: utilpointer.Int(30),
					EvictingCPULowWatermark:     utilpointer.Int(50),
					EvictingMemoryLowWatermark:  utilpointer.Int(80),
				},
			},
			expectedErr: []error{errors.New(EvictingCPULowWatermarkHigherThanHighWatermark), errors.New(EvictingMemoryLowWatermarkHigherThanHighWatermark)},
		},
	}

	for _, tc := range testCases {
		actualErrs := tc.colocationCfg.Validate()
		assert.Equal(t, tc.expectedErr, actualErrs)
	}
}
