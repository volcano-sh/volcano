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

package features

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
	utilpointer "k8s.io/utils/pointer"

	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

func TestFeatureEnable(t *testing.T) {
	tests := []struct {
		name             string
		cfg              *api.ColocationConfig
		expectedFeatures map[Feature]bool
		expectedErrors   map[Feature]bool
	}{
		{
			name: "all-features-enabled",
			cfg: &api.ColocationConfig{
				NodeLabelConfig: &api.NodeLabelConfig{
					NodeColocationEnable:       utilpointer.Bool(true),
					NodeOverSubscriptionEnable: utilpointer.Bool(true),
				},
				CPUQosConfig:    &api.CPUQos{Enable: utilpointer.Bool(true)},
				CPUBurstConfig:  &api.CPUBurst{Enable: utilpointer.Bool(true)},
				MemoryQosConfig: &api.MemoryQos{Enable: utilpointer.Bool(true)},
				NetworkQosConfig: &api.NetworkQos{
					Enable: utilpointer.Bool(true),
				},
				OverSubscriptionConfig: &api.OverSubscription{
					Enable: utilpointer.Bool(true),
				},
			},
			expectedFeatures: map[Feature]bool{
				CPUQoSFeature:           true,
				CPUBurstFeature:         true,
				MemoryQoSFeature:        true,
				NetworkQoSFeature:       true,
				OverSubscriptionFeature: true,
			},
			expectedErrors: map[Feature]bool{
				CPUQoSFeature:           false,
				CPUBurstFeature:         false,
				MemoryQoSFeature:        false,
				NetworkQoSFeature:       false,
				OverSubscriptionFeature: false,
			},
		},

		{
			name: "node-colocation-disabled && node-OverSubscription-disabled",
			cfg: &api.ColocationConfig{
				NodeLabelConfig: &api.NodeLabelConfig{
					NodeColocationEnable:       utilpointer.Bool(false),
					NodeOverSubscriptionEnable: utilpointer.Bool(false),
				},
				CPUQosConfig:    &api.CPUQos{Enable: utilpointer.Bool(true)},
				CPUBurstConfig:  &api.CPUBurst{Enable: utilpointer.Bool(true)},
				MemoryQosConfig: &api.MemoryQos{Enable: utilpointer.Bool(true)},
				NetworkQosConfig: &api.NetworkQos{
					Enable: utilpointer.Bool(true),
				},
				OverSubscriptionConfig: &api.OverSubscription{
					Enable: utilpointer.Bool(true),
				},
			},
			expectedFeatures: map[Feature]bool{
				CPUQoSFeature:           false,
				CPUBurstFeature:         false,
				MemoryQoSFeature:        false,
				NetworkQoSFeature:       false,
				OverSubscriptionFeature: false,
			},
			expectedErrors: map[Feature]bool{
				CPUQoSFeature:           false,
				CPUBurstFeature:         false,
				MemoryQoSFeature:        false,
				NetworkQoSFeature:       false,
				OverSubscriptionFeature: false,
			},
		},

		{
			name: "node-colocation-enabled && node-OverSubscription-disabled",
			cfg: &api.ColocationConfig{
				NodeLabelConfig: &api.NodeLabelConfig{
					NodeColocationEnable:       utilpointer.Bool(true),
					NodeOverSubscriptionEnable: utilpointer.Bool(false),
				},
				CPUQosConfig:    &api.CPUQos{Enable: utilpointer.Bool(true)},
				CPUBurstConfig:  &api.CPUBurst{Enable: utilpointer.Bool(true)},
				MemoryQosConfig: &api.MemoryQos{Enable: utilpointer.Bool(true)},
				NetworkQosConfig: &api.NetworkQos{
					Enable: utilpointer.Bool(true),
				},
				OverSubscriptionConfig: &api.OverSubscription{
					Enable: utilpointer.Bool(true),
				},
			},
			expectedFeatures: map[Feature]bool{
				CPUQoSFeature:           true,
				CPUBurstFeature:         true,
				MemoryQoSFeature:        true,
				NetworkQoSFeature:       true,
				OverSubscriptionFeature: false,
			},
			expectedErrors: map[Feature]bool{
				CPUQoSFeature:           false,
				CPUBurstFeature:         false,
				MemoryQoSFeature:        false,
				NetworkQoSFeature:       false,
				OverSubscriptionFeature: false,
			},
		},

		{
			name: "node-colocation-disabled && node-OverSubscription-enabled",
			cfg: &api.ColocationConfig{
				NodeLabelConfig: &api.NodeLabelConfig{
					NodeColocationEnable:       utilpointer.Bool(false),
					NodeOverSubscriptionEnable: utilpointer.Bool(true),
				},
				CPUQosConfig:    &api.CPUQos{Enable: utilpointer.Bool(true)},
				CPUBurstConfig:  &api.CPUBurst{Enable: utilpointer.Bool(true)},
				MemoryQosConfig: &api.MemoryQos{Enable: utilpointer.Bool(true)},
				NetworkQosConfig: &api.NetworkQos{
					Enable: utilpointer.Bool(true),
				},
				OverSubscriptionConfig: &api.OverSubscription{
					Enable: utilpointer.Bool(true),
				},
			},
			expectedFeatures: map[Feature]bool{
				CPUQoSFeature:           true,
				CPUBurstFeature:         true,
				MemoryQoSFeature:        true,
				NetworkQoSFeature:       true,
				OverSubscriptionFeature: true,
			},
			expectedErrors: map[Feature]bool{
				CPUQoSFeature:           false,
				CPUBurstFeature:         false,
				MemoryQoSFeature:        false,
				NetworkQoSFeature:       false,
				OverSubscriptionFeature: false,
			},
		},

		{
			name: "cpu-qos-disabled && memory-qos-disabled && network-qos-disabled",
			cfg: &api.ColocationConfig{
				NodeLabelConfig: &api.NodeLabelConfig{
					NodeColocationEnable:       utilpointer.Bool(true),
					NodeOverSubscriptionEnable: utilpointer.Bool(false),
				},
				CPUQosConfig:    &api.CPUQos{Enable: utilpointer.Bool(false)},
				CPUBurstConfig:  &api.CPUBurst{Enable: utilpointer.Bool(true)},
				MemoryQosConfig: &api.MemoryQos{Enable: utilpointer.Bool(false)},
				NetworkQosConfig: &api.NetworkQos{
					Enable: utilpointer.Bool(false),
				},
				OverSubscriptionConfig: &api.OverSubscription{
					Enable: utilpointer.Bool(true),
				},
			},
			expectedFeatures: map[Feature]bool{
				CPUQoSFeature:           false,
				CPUBurstFeature:         true,
				MemoryQoSFeature:        false,
				NetworkQoSFeature:       false,
				OverSubscriptionFeature: false,
			},
			expectedErrors: map[Feature]bool{
				CPUQoSFeature:           false,
				CPUBurstFeature:         false,
				MemoryQoSFeature:        false,
				NetworkQoSFeature:       false,
				OverSubscriptionFeature: false,
			},
		},

		{
			name: "cpu-burst-disabled && overSubscription-disabled",
			cfg: &api.ColocationConfig{
				NodeLabelConfig: &api.NodeLabelConfig{
					NodeColocationEnable:       utilpointer.Bool(false),
					NodeOverSubscriptionEnable: utilpointer.Bool(true),
				},
				CPUQosConfig:    &api.CPUQos{Enable: utilpointer.Bool(true)},
				CPUBurstConfig:  &api.CPUBurst{Enable: utilpointer.Bool(false)},
				MemoryQosConfig: &api.MemoryQos{Enable: utilpointer.Bool(true)},
				NetworkQosConfig: &api.NetworkQos{
					Enable: utilpointer.Bool(true),
				},
				OverSubscriptionConfig: &api.OverSubscription{
					Enable: utilpointer.Bool(false),
				},
			},
			expectedFeatures: map[Feature]bool{
				CPUQoSFeature:           true,
				CPUBurstFeature:         false,
				MemoryQoSFeature:        true,
				NetworkQoSFeature:       true,
				OverSubscriptionFeature: false,
			},
			expectedErrors: map[Feature]bool{
				CPUQoSFeature:           false,
				CPUBurstFeature:         false,
				MemoryQoSFeature:        false,
				NetworkQoSFeature:       false,
				OverSubscriptionFeature: false,
			},
		},
	}

	for _, tt := range tests {
		for feature, expectedEnabled := range tt.expectedFeatures {
			actualEnabled, actualErr := DefaultFeatureGate.Enabled(feature, tt.cfg)
			assert.Equal(t, expectedEnabled, actualEnabled, tt.name, feature)
			assert.Equal(t, tt.expectedErrors[feature], actualErr != nil, tt.name, feature)
		}
	}
}

func TestFeatureSupport(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "MkdirTemp")
	defer func() {
		err = os.RemoveAll(dir)
		if err != nil {
			t.Errorf("remove dir(%s) failed: %v", dir, err)
		}
		assert.Equal(t, err == nil, true)
	}()
	assert.Equal(t, err == nil, true)

	tests := []struct {
		name           string
		configuration  *config.Configuration
		expectedErrors map[Feature]bool
	}{
		{
			name: "all-features-enabled",
			configuration: &config.Configuration{
				GenericConfiguration: &config.VolcanoAgentConfiguration{
					SupportedFeatures: []string{"*"},
				},
			},
			expectedErrors: map[Feature]bool{
				CPUQoSFeature:           false,
				CPUBurstFeature:         false,
				MemoryQoSFeature:        false,
				NetworkQoSFeature:       false,
				OverSubscriptionFeature: false,
			},
		},

		{
			name: "all-features-enabled && support networkqos && support memoryqos",
			configuration: &config.Configuration{
				GenericConfiguration: &config.VolcanoAgentConfiguration{
					SupportedFeatures: []string{"NetworkQoS", "MemoryQoS"},
				},
			},
			expectedErrors: map[Feature]bool{
				CPUQoSFeature:           true,
				CPUBurstFeature:         true,
				MemoryQoSFeature:        false,
				NetworkQoSFeature:       false,
				OverSubscriptionFeature: true,
			},
		},

		{
			name: "all-features-enabled && not support memoryqos",
			configuration: &config.Configuration{
				GenericConfiguration: &config.VolcanoAgentConfiguration{
					SupportedFeatures: []string{"*", "-MemoryQoS"},
				},
			},
			expectedErrors: map[Feature]bool{
				CPUQoSFeature:           false,
				CPUBurstFeature:         false,
				MemoryQoSFeature:        true,
				NetworkQoSFeature:       false,
				OverSubscriptionFeature: false,
			},
		},
	}

	tmpFile := path.Join(dir, "os-release")
	if err = os.WriteFile(tmpFile, []byte(openEulerOS), 0660); err != nil {
		assert.Equal(t, nil, err)
	}
	if err = os.Setenv(utils.HostOSReleasePathEnv, tmpFile); err != nil {
		assert.Equal(t, nil, err)
	}

	for _, tt := range tests {
		for feature, expectedErr := range tt.expectedErrors {
			actualErr := DefaultFeatureGate.Supported(feature, tt.configuration)
			assert.Equal(t, expectedErr, actualErr != nil, tt.name, feature)
		}
	}
}

var openEulerOS = `
NAME="openEuler"
VERSION="22.03 (LTS-SP2)"
ID="openEuler"
VERSION_ID="22.03"
PRETTY_NAME="openEuler 22.03 (LTS-SP2)"
ANSI_COLOR="0;31"
`

var ubuntuOS = `
NAME="Ubuntu"
VERSION="16.04.5 LTS (Xenial Xerus)"
ID=ubuntu
ID_LIKE=debian
PRETTY_NAME="Ubuntu 16.04.5 LTS"
VERSION_ID="16.04"
`

func TestCheckNodeSupportNetworkQoS(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "MkdirTemp")
	defer func() {
		err = os.RemoveAll(dir)
		if err != nil {
			t.Errorf("remove dir(%s) failed: %v", dir, err)
		}
		assert.Equal(t, err == nil, true)
	}()
	assert.Equal(t, err == nil, true)

	testCases := []struct {
		name           string
		releaseContent string
		expectedError  bool
	}{
		{
			name:           "openEuler 22.03 SP2",
			releaseContent: openEulerOS,
			expectedError:  false,
		},
		{
			name:           "ubuntu && x86",
			releaseContent: ubuntuOS,
			expectedError:  true,
		},
	}
	for _, tc := range testCases {
		tmpFile := path.Join(dir, "os-release")
		if err = os.WriteFile(tmpFile, []byte(tc.releaseContent), 0660); err != nil {
			assert.Equal(t, nil, err)
		}
		if err = os.Setenv(utils.HostOSReleasePathEnv, tmpFile); err != nil {
			assert.Equal(t, nil, err)
		}
		actualErr := CheckNodeSupportNetworkQoS()
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name)
	}
}
