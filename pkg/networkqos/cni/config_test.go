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

package cni

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/stretchr/testify/assert"

	"volcano.sh/volcano/pkg/networkqos/utils"
)

var fileWithoutNetworkQoS = `{
    "cniVersion": "0.3.1",
    "name": "default-network",
    "plugins": [
        {
            "args": {
                "phynet": "phy_net1",
                "secret_name": "canal-secret",
                "tenant_id": "",
                "vpc_id": "00a6a77c-83c8-405f-b7e8-d8648adfafa7"
            },
            "capabilities": {
                "bandwidth": true
            },
            "ipam": {
                "subnet": "172.16.0.0/16"
            },
            "name": "default-network",
            "type": "vpc-router"
        },
        {
            "capabilities": {
                "portMappings": true
            },
            "externalSetMarkChain": "KUBE-MARK-MASQ",
            "type": "portmap"
        }
    ]
}`

var fileWithNetworkQoS = `{
    "cniVersion": "0.3.1",
    "name": "default-network",
    "plugins": [
        {
            "args": {
                "phynet": "phy_net1",
                "secret_name": "canal-secret",
                "tenant_id": "",
                "vpc_id": "00a6a77c-83c8-405f-b7e8-d8648adfafa7"
            },
            "capabilities": {
                "bandwidth": true
            },
            "ipam": {
                "subnet": "172.16.0.0/16"
            },
            "name": "default-network",
            "type": "vpc-router"
        },
        {
            "capabilities": {
                "portMappings": true
            },
            "externalSetMarkChain": "KUBE-MARK-MASQ",
            "type": "portmap"
        },
        {
            "args": {
                "colocation": "true",
                "offline-high-bandwidth": "50MB",
                "offline-low-bandwidth": "20MB",
                "online-bandwidth-watermark": "50MB"
            },
            "name": "network-qos",
            "type": "network-qos"
        }
    ]
}`

var fileWithNewNetworkQoS = `{
    "cniVersion": "0.3.1",
    "name": "default-network",
    "plugins": [
        {
            "args": {
                "phynet": "phy_net1",
                "secret_name": "canal-secret",
                "tenant_id": "",
                "vpc_id": "00a6a77c-83c8-405f-b7e8-d8648adfafa7"
            },
            "capabilities": {
                "bandwidth": true
            },
            "ipam": {
                "subnet": "172.16.0.0/16"
            },
            "name": "default-network",
            "type": "vpc-router"
        },
        {
            "capabilities": {
                "portMappings": true
            },
            "externalSetMarkChain": "KUBE-MARK-MASQ",
            "type": "portmap"
        },
        {
            "args": {
                "colocation": "true",
                "offline-high-bandwidth": "100MB",
                "offline-low-bandwidth": "20MB",
                "online-bandwidth-watermark": "100MB"
            },
            "name": "network-qos",
            "type": "network-qos"
        }
    ]
}`

var fileWithoutPlugins = `{
    "cniVersion": "0.3.1",
    "name": "default-network"
}`

var fileWithIllegalJson = `{
    "cniVersion": "0.3.1",
    "name": "default-network",
    "plugins": [
        {
            "args": {
                "phynet": "phy_net1",
                "secret_name": "canal-secret",
                "tenant_id": "",
                "vpc_id": "00a6a77c-83c8-405f-b7e8-d8648adfafa7"
            },
            "capabilities": {
                "bandwidth": true
            },
            "ipam": {
                "subnet": "172.16.0.0/16"
            },
            "name": "default-network",
            "type": "vpc-router"
        },,,,
    ]
}`

func TestAddOrUpdateCniPluginToConfList(t *testing.T) {
	testCases := []struct {
		name                 string
		plugin               map[string]interface{}
		fileContent          string
		jsonMarshalErr       error
		jsonMarshalIndentErr error
		expectedFileContent  string
		expectedError        bool
	}{

		{
			name: "cni-plugin-add",
			plugin: map[string]interface{}{
				"name": "network-qos",
				"type": "network-qos",
				"args": map[string]string{
					utils.NodeColocationEnable:        "true",
					utils.OnlineBandwidthWatermarkKey: "50MB",
					utils.OfflineLowBandwidthKey:      "20MB",
					utils.OfflineHighBandwidthKey:     "50MB",
				},
			},
			fileContent:         fileWithoutNetworkQoS,
			expectedFileContent: fileWithNetworkQoS,
			expectedError:       false,
		},

		{
			name: "cni-plugin-update",
			plugin: map[string]interface{}{
				"name": "network-qos",
				"type": "network-qos",
				"args": map[string]string{
					utils.NodeColocationEnable:        "true",
					utils.OnlineBandwidthWatermarkKey: "100MB",
					utils.OfflineLowBandwidthKey:      "20MB",
					utils.OfflineHighBandwidthKey:     "100MB",
				},
			},
			fileContent:         fileWithNetworkQoS,
			expectedFileContent: fileWithNewNetworkQoS,
			expectedError:       false,
		},

		{
			name: "empty-cni-config",
			plugin: map[string]interface{}{
				"name": "network-qos",
				"type": "network-qos",
				"args": map[string]string{
					utils.NodeColocationEnable:        "true",
					utils.OnlineBandwidthWatermarkKey: "500MB",
					utils.OfflineLowBandwidthKey:      "200MB",
					utils.OfflineHighBandwidthKey:     "300MB",
				},
			},
			fileContent:         "",
			expectedFileContent: "",
			expectedError:       true,
		},

		{
			name: "cni-config-without-plugins",
			plugin: map[string]interface{}{
				"name": "network-qos",
				"type": "network-qos",
				"args": map[string]string{
					utils.NodeColocationEnable:        "true",
					utils.OnlineBandwidthWatermarkKey: "1000MB",
					utils.OfflineLowBandwidthKey:      "300MB",
					utils.OfflineHighBandwidthKey:     "500MB",
				},
			},
			fileContent:         fileWithoutPlugins,
			expectedFileContent: "",
			expectedError:       true,
		},

		{
			name: "cni-config-with-illegal-json",
			plugin: map[string]interface{}{
				"name": "network-qos",
				"type": "network-qos",
				"args": map[string]string{
					utils.NodeColocationEnable:        "true",
					utils.OnlineBandwidthWatermarkKey: "2500MB",
					utils.OfflineLowBandwidthKey:      "500MB",
					utils.OfflineHighBandwidthKey:     "800MB",
				},
			},
			fileContent:         fileWithIllegalJson,
			expectedFileContent: "",
			expectedError:       true,
		},

		{
			name: "json Marshal failed",
			plugin: map[string]interface{}{
				"name": "network-qos",
				"type": "network-qos",
				"args": map[string]string{
					utils.NodeColocationEnable:        "true",
					utils.OnlineBandwidthWatermarkKey: "1200MB",
					utils.OfflineLowBandwidthKey:      "260MB",
					utils.OfflineHighBandwidthKey:     "560MB",
				},
			},
			fileContent:         fileWithoutNetworkQoS,
			jsonMarshalErr:      fmt.Errorf("json Marshal failed"),
			expectedFileContent: "",
			expectedError:       true,
		},

		{
			name: "json MarshalIndent failed",
			plugin: map[string]interface{}{
				"name": "network-qos",
				"type": "network-qos",
				"args": map[string]string{
					utils.NodeColocationEnable:        "true",
					utils.OnlineBandwidthWatermarkKey: "1050MB",
					utils.OfflineLowBandwidthKey:      "320MB",
					utils.OfflineHighBandwidthKey:     "650MB",
				},
			},
			fileContent:          fileWithoutNetworkQoS,
			jsonMarshalIndentErr: fmt.Errorf("json MarshalIndent failed"),
			expectedFileContent:  "",
			expectedError:        true,
		},
	}

	tmp, err := os.MkdirTemp("", "test-cni")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp)

	conflistFile := filepath.Join(tmp, "cni.conflist")
	defer os.Remove(conflistFile)

	for _, tc := range testCases {
		if err = os.WriteFile(conflistFile, []byte(tc.fileContent), os.FileMode(0644)); err != nil {
			t.Fatal(err)
		}

		var jsonMarshalPatch, jsonMarshalIndentPatch *gomonkey.Patches
		if tc.jsonMarshalErr != nil {
			jsonMarshalPatch = gomonkey.NewPatches()
			jsonMarshalPatch.ApplyFunc(json.Marshal, func(v interface{}) ([]byte, error) {
				return nil, tc.jsonMarshalErr
			})
		}

		if tc.jsonMarshalIndentErr != nil {
			jsonMarshalIndentPatch = gomonkey.NewPatches()
			jsonMarshalIndentPatch.ApplyFunc(json.Marshal, func(v interface{}) ([]byte, error) {
				return nil, tc.jsonMarshalErr
			})
		}

		actualErr := GetCNIPluginConfHandler().AddOrUpdateCniPluginToConfList(conflistFile, "network-qos", tc.plugin)

		if jsonMarshalPatch != nil {
			jsonMarshalPatch.Reset()
		}

		if jsonMarshalIndentPatch != nil {
			jsonMarshalIndentPatch.Reset()
		}

		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name)

		if actualErr != nil {
			continue
		}

		actualFileContent, err := os.ReadFile(conflistFile)
		if err != nil {
			t.Fatal(err)
		}

		assert.Equal(t, tc.expectedFileContent, string(actualFileContent), tc.name)
	}
}

func TestDeleteCniPluginFromConfList(t *testing.T) {
	testCases := []struct {
		name                 string
		fileContent          string
		jsonMarshalErr       error
		jsonMarshalIndentErr error
		expectedFileContent  string
		expectedError        bool
	}{
		{
			name:                "cni-plugin-delete",
			fileContent:         fileWithNetworkQoS,
			expectedFileContent: fileWithoutNetworkQoS,
			expectedError:       false,
		},

		{
			name:                "cni-plugin-not-exists",
			fileContent:         fileWithoutNetworkQoS,
			expectedFileContent: fileWithoutNetworkQoS,
			expectedError:       false,
		},

		{
			name:          "empty-cni-config",
			expectedError: true,
		},

		{
			name:          "cni-config-without-plugins",
			fileContent:   fileWithoutPlugins,
			expectedError: true,
		},

		{
			name:          "cni-config-with-illegal-json",
			fileContent:   fileWithIllegalJson,
			expectedError: true,
		},

		{
			name:           "json Marshal failed",
			fileContent:    fileWithNetworkQoS,
			jsonMarshalErr: fmt.Errorf("json Marshal failed"),
			expectedError:  true,
		},

		{
			name: "json MarshalIndent failed",

			fileContent:          fileWithNetworkQoS,
			jsonMarshalIndentErr: fmt.Errorf("json MarshalIndent failed"),
			expectedError:        true,
		},
	}

	tmp, err := os.MkdirTemp("", "test-cni")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmp)

	conflistFile := filepath.Join(tmp, "cni.conflist")
	defer os.Remove(conflistFile)

	for _, tc := range testCases {
		if err = os.WriteFile(conflistFile, []byte(tc.fileContent), os.FileMode(0644)); err != nil {
			t.Fatal(err)
		}

		var jsonMarshalPatch, jsonMarshalIndentPatch *gomonkey.Patches
		if tc.jsonMarshalErr != nil {
			jsonMarshalPatch = gomonkey.NewPatches()
			jsonMarshalPatch.ApplyFunc(json.Marshal, func(v interface{}) ([]byte, error) {
				return nil, tc.jsonMarshalErr
			})
		}

		if tc.jsonMarshalIndentErr != nil {
			jsonMarshalIndentPatch = gomonkey.NewPatches()
			jsonMarshalIndentPatch.ApplyFunc(json.Marshal, func(v interface{}) ([]byte, error) {
				return nil, tc.jsonMarshalErr
			})
		}

		cniConf := GetCNIPluginConfHandler()
		actualErr := cniConf.DeleteCniPluginFromConfList(conflistFile, "network-qos")

		if jsonMarshalPatch != nil {
			jsonMarshalPatch.Reset()
		}

		if jsonMarshalIndentPatch != nil {
			jsonMarshalIndentPatch.Reset()
		}

		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name)

		if actualErr != nil {
			continue
		}

		content, err := os.ReadFile(conflistFile)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, tc.expectedFileContent, string(content), tc.name)
	}
}
