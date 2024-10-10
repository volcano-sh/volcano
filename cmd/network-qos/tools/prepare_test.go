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

package tools

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"volcano.sh/volcano/pkg/agent/utils/exec"
	mockexec "volcano.sh/volcano/pkg/agent/utils/exec/mocks"
	"volcano.sh/volcano/pkg/networkqos/cni"
	mockcni "volcano.sh/volcano/pkg/networkqos/cni/mocks"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

func TestUnInstall(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	mockExec := mockexec.NewMockExecInterface(mockController)
	exec.SetExecutor(mockExec)
	mockCniConf := mockcni.NewMockCNIPluginsConfHandler(mockController)
	cni.SetCNIPluginConfHandler(mockCniConf)

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := &prepareCmd{
		out:    out,
		errOut: errOut,
	}

	testCases := []struct {
		name          string
		apiCall       []*gomock.Call
		expectedError bool
	}{
		{
			name: "[prepare uninstall] network-qos reset failed",
			apiCall: []*gomock.Call{
				mockExec.EXPECT().CommandContext(gomock.Any(), utils.NetWorkCmdFile+" reset").Return("", fmt.Errorf("fake error")),
			},
			expectedError: true,
		},

		{
			name: "[prepare uninstall] update cni plugin conf failed",
			apiCall: []*gomock.Call{
				mockExec.EXPECT().CommandContext(gomock.Any(), utils.NetWorkCmdFile+" reset").Return("", nil),
				mockCniConf.EXPECT().DeleteCniPluginFromConfList(utils.DefaultCNIConfFile, utils.CNIPluginName).Return(fmt.Errorf("fake error")),
			},
			expectedError: true,
		},
		{
			name: "[prepare uninstall] uninstall successfully",
			apiCall: []*gomock.Call{
				mockExec.EXPECT().CommandContext(gomock.Any(), utils.NetWorkCmdFile+" reset").Return("", nil),
				mockCniConf.EXPECT().DeleteCniPluginFromConfList(utils.DefaultCNIConfFile, utils.CNIPluginName).Return(nil),
			},
			expectedError: false,
		},
	}

	for _, tc := range testCases {
		actualErr := cmd.unInstall()
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name)
	}
}

func TestInstall(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	mockExec := mockexec.NewMockExecInterface(mockController)
	exec.SetExecutor(mockExec)
	mockCniConf := mockcni.NewMockCNIPluginsConfHandler(mockController)
	cni.SetCNIPluginConfHandler(mockCniConf)

	cmd := &prepareCmd{}

	testCases := []struct {
		name                     string
		onlineBandwidthWatermark string
		offlineLowBandwidth      string
		offlineHighBandwidth     string
		interval                 string
		apiCall                  []*gomock.Call
		expectedError            bool
	}{
		{
			name:                     "[prepare install] network QoS enable && empty check-interval",
			interval:                 "",
			onlineBandwidthWatermark: "5000Mbps",
			offlineLowBandwidth:      "2500Mbps",
			offlineHighBandwidth:     "3500Mbps",
			apiCall: []*gomock.Call{
				mockExec.EXPECT().CommandContext(gomock.Any(), utils.NetWorkCmdFile+" set --online-bandwidth-watermark=5000Mbps "+
					"--offline-low-bandwidth=2500Mbps --offline-high-bandwidth=3500Mbps --check-interval=10000000").Return("", nil),
				mockCniConf.EXPECT().AddOrUpdateCniPluginToConfList(utils.DefaultCNIConfFile, utils.CNIPluginName, map[string]interface{}{
					"name": "network-qos",
					"type": "network-qos",
					"args": map[string]string{
						utils.NetWorkQoSCheckInterval:     "10000000",
						utils.NodeColocationEnable:        "true",
						utils.OnlineBandwidthWatermarkKey: "5000Mbps",
						utils.OfflineLowBandwidthKey:      "2500Mbps",
						utils.OfflineHighBandwidthKey:     "3500Mbps",
					},
				}).Return(nil),
			},
			expectedError: false,
		},

		{
			name:                     "[prepare install] network QoS enable",
			interval:                 "20000000",
			onlineBandwidthWatermark: "10000Mbps",
			offlineLowBandwidth:      "2000Mbps",
			offlineHighBandwidth:     "5000Mbps",

			apiCall: []*gomock.Call{
				mockExec.EXPECT().CommandContext(gomock.Any(), utils.NetWorkCmdFile+" set --online-bandwidth-watermark=10000Mbps "+
					"--offline-low-bandwidth=2000Mbps --offline-high-bandwidth=5000Mbps --check-interval=20000000").Return("", nil),
				mockCniConf.EXPECT().AddOrUpdateCniPluginToConfList(utils.DefaultCNIConfFile, utils.CNIPluginName, map[string]interface{}{
					"name": "network-qos",
					"type": "network-qos",
					"args": map[string]string{
						utils.NetWorkQoSCheckInterval:     "20000000",
						utils.NodeColocationEnable:        "true",
						utils.OnlineBandwidthWatermarkKey: "10000Mbps",
						utils.OfflineLowBandwidthKey:      "2000Mbps",
						utils.OfflineHighBandwidthKey:     "5000Mbps",
					},
				}).Return(nil),
			},
			expectedError: false,
		},

		{
			name:                     "[prepare install] colocation set failed",
			interval:                 "",
			onlineBandwidthWatermark: "900Mbps",
			offlineLowBandwidth:      "550Mbps",
			offlineHighBandwidth:     "750Mbps",
			apiCall: []*gomock.Call{
				mockExec.EXPECT().CommandContext(gomock.Any(), utils.NetWorkCmdFile+" set --online-bandwidth-watermark=900Mbps "+
					"--offline-low-bandwidth=550Mbps --offline-high-bandwidth=750Mbps --check-interval=10000000").Return("", fmt.Errorf("fake error")),
			},
			expectedError: true,
		},

		{
			name:                     "[prepare install] update cni config failed",
			interval:                 "30000000",
			onlineBandwidthWatermark: "30000Mbps",
			offlineLowBandwidth:      "5500Mbps",
			offlineHighBandwidth:     "7500Mbps",
			apiCall: []*gomock.Call{
				mockExec.EXPECT().CommandContext(gomock.Any(), utils.NetWorkCmdFile+" set --online-bandwidth-watermark=30000Mbps "+
					"--offline-low-bandwidth=5500Mbps --offline-high-bandwidth=7500Mbps --check-interval=30000000").Return("", nil),
				mockCniConf.EXPECT().AddOrUpdateCniPluginToConfList(utils.DefaultCNIConfFile, utils.CNIPluginName, map[string]interface{}{
					"name": "network-qos",
					"type": "network-qos",
					"args": map[string]string{
						utils.NetWorkQoSCheckInterval:     "30000000",
						utils.NodeColocationEnable:        "true",
						utils.OnlineBandwidthWatermarkKey: "30000Mbps",
						utils.OfflineLowBandwidthKey:      "5500Mbps",
						utils.OfflineHighBandwidthKey:     "7500Mbps",
					},
				}).Return(fmt.Errorf("fake error")),
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		actualErr := cmd.install(tc.onlineBandwidthWatermark, tc.offlineLowBandwidth, tc.offlineHighBandwidth, tc.interval)
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name, actualErr)
	}
}
