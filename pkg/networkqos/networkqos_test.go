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

package networkqos

import (
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	utilpointer "k8s.io/utils/pointer"

	coloConf "volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/utils/exec"
	mockexec "volcano.sh/volcano/pkg/agent/utils/exec/mocks"
	"volcano.sh/volcano/pkg/config"
	"volcano.sh/volcano/pkg/networkqos/utils"
)

func TestGetOnlineBandwidthWatermark(t *testing.T) {
	testCases := []struct {
		name            string
		qosConf         *coloConf.NetworkQos
		serverRateQuota int64
		expectedResult  string
		expectedErr     bool
	}{
		{
			name:            "get value from conf",
			serverRateQuota: 10000,
			qosConf: &coloConf.NetworkQos{
				OnlineBandwidthWatermarkPercent: utilpointer.Int(50),
			},
			expectedResult: "5000Mbps",
			expectedErr:    false,
		},
		{
			name:            "nil network qos conf",
			serverRateQuota: 5000,
			expectedResult:  "",
			expectedErr:     true,
		},

		{
			name:            "nil OfflineHighBandwidthPercent",
			serverRateQuota: 6000,
			qosConf:         &coloConf.NetworkQos{},
			expectedResult:  "",
			expectedErr:     true,
		},
	}

	for _, tc := range testCases {
		actualResult, actualErr := GetOnlineBandwidthWatermark(tc.serverRateQuota, tc.qosConf)
		assert.Equal(t, tc.expectedResult, actualResult, tc.name)
		assert.Equal(t, tc.expectedErr, actualErr != nil, tc.name)
	}
}

func TestGetOfflineLowBandwidthPercent(t *testing.T) {
	testCases := []struct {
		name            string
		qosConf         *coloConf.NetworkQos
		serverRateQuota int64
		expectedResult  string
		expectedErr     bool
	}{
		{
			name: "get value from conf",
			qosConf: &coloConf.NetworkQos{
				OfflineLowBandwidthPercent: utilpointer.Int(25),
			},
			serverRateQuota: 10000,
			expectedResult:  "2500Mbps",
			expectedErr:     false,
		},
		{
			name:            "nil network qos conf",
			serverRateQuota: 5000,
			expectedResult:  "",
			expectedErr:     true,
		},

		{
			name:            "nil OfflineLowBandwidthPercent",
			serverRateQuota: 6000,
			qosConf:         &coloConf.NetworkQos{},
			expectedResult:  "",
			expectedErr:     true,
		},
	}

	for _, tc := range testCases {
		actualResult, actualErr := GetOfflineLowBandwidthPercent(tc.serverRateQuota, tc.qosConf)
		assert.Equal(t, tc.expectedResult, actualResult, tc.name)
		assert.Equal(t, tc.expectedErr, actualErr != nil, tc.name)
	}
}

func TestGetOfflineHighBandwidthPercent(t *testing.T) {
	testCases := []struct {
		name            string
		qosConf         *coloConf.NetworkQos
		serverRateQuota int64
		expectedResult  string
		expectedErr     bool
	}{

		{
			name: "get value from conf",
			qosConf: &coloConf.NetworkQos{
				OfflineHighBandwidthPercent: utilpointer.Int(30),
			},
			serverRateQuota: 20000,
			expectedResult:  "6000Mbps",
			expectedErr:     false,
		},
		{
			name:            "nil network qos conf",
			serverRateQuota: 5000,
			expectedResult:  "",
			expectedErr:     true,
		},

		{
			name:            "nil OfflineHighBandwidthPercent",
			serverRateQuota: 6000,
			qosConf:         &coloConf.NetworkQos{},
			expectedResult:  "",
			expectedErr:     true,
		},
	}

	for _, tc := range testCases {
		actualResult, actualErr := GetOfflineHighBandwidthPercent(tc.serverRateQuota, tc.qosConf)
		assert.Equal(t, tc.expectedResult, actualResult, tc.name)
		assert.Equal(t, tc.expectedErr, actualErr != nil, tc.name)
	}
}

func TestEnableNetworkQoS(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()
	mockExec := mockexec.NewMockExecInterface(mockController)
	exec.SetExecutor(mockExec)

	testCases := []struct {
		name        string
		config      *config.Configuration
		node        *corev1.Node
		apiCall     []*gomock.Call
		qosConf     *coloConf.NetworkQos
		expectedErr bool
	}{
		{
			name: "enable NetworkQoS succeed",
			config: &config.Configuration{
				GenericConfiguration: &config.VolcanoAgentConfiguration{
					KubeNodeName: "test-node-1",
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-node-1",
					Annotations: map[string]string{
						"volcano.sh/network-bandwidth-rate": "100",
					},
				},
			},
			qosConf: &coloConf.NetworkQos{
				OnlineBandwidthWatermarkPercent: utilpointer.Int(80),
				OfflineHighBandwidthPercent:     utilpointer.Int(40),
				OfflineLowBandwidthPercent:      utilpointer.Int(10),
			},
			apiCall: []*gomock.Call{
				mockExec.EXPECT().CommandContext(gomock.Any(), "/usr/local/bin/network-qos prepare "+
					"--enable-network-qos=true --online-bandwidth-watermark=80Mbps --offline-low-bandwidth=10Mbps "+
					"--offline-high-bandwidth=40Mbps --check-interval=").Return("", nil),
			},
		},
	}

	for _, tc := range testCases {
		fakeClient := fake.NewSimpleClientset(tc.node)
		mgr := &NetworkQoSManagerImp{
			config: &config.Configuration{
				GenericConfiguration: &config.VolcanoAgentConfiguration{
					KubeClient:   fakeClient,
					KubeNodeName: tc.node.Name,
				},
			},
		}
		mgr.config.GenericConfiguration.KubeClient = fake.NewSimpleClientset(tc.node)
		actualErr := mgr.EnableNetworkQoS(tc.qosConf)
		gomock.InOrder(tc.apiCall...)
		assert.Equal(t, tc.expectedErr, actualErr != nil, tc.name)
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

func TestInstallNetworkQoS(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "MkdirTemp")
	defer func() {
		err = os.RemoveAll(dir)
		if err != nil {
			t.Errorf("remove dir(%s) failed: %v", dir, err)
		}
		assert.Equal(t, err == nil, true)
	}()
	assert.Equal(t, err == nil, true)
	tmpFile := path.Join(dir, "os-release")
	if err = os.WriteFile(tmpFile, []byte(openEulerOS), 0660); err != nil {
		assert.Equal(t, nil, err)
	}
	if err = os.Setenv(utils.HostOSReleasePathEnv, tmpFile); err != nil {
		assert.Equal(t, nil, err)
	}

	mockController := gomock.NewController(t)
	defer mockController.Finish()
	mockExec := mockexec.NewMockExecInterface(mockController)
	exec.SetExecutor(mockExec)

	testCases := []struct {
		name          string
		apiCall       []*gomock.Call
		expectedError bool
	}{
		{
			name: "install network-qos successfully && existed old bwm_tc.o && existed old network-qos",
			apiCall: []*gomock.Call{
				mockExec.EXPECT().CommandContext(gomock.Any(), "sudo /bin/cp -f /usr/local/bin/bwm_tc.o /usr/share/bwmcli").Return("", nil),
				mockExec.EXPECT().CommandContext(gomock.Any(), "sudo /bin/cp -f /usr/local/bin/network-qos /opt/cni/bin").Return("", nil),
			},
			expectedError: false,
		},

		{
			name: "install bwm_tc.o failed",
			apiCall: []*gomock.Call{
				mockExec.EXPECT().CommandContext(gomock.Any(), "sudo /bin/cp -f /usr/local/bin/bwm_tc.o /usr/share/bwmcli").Return("", fmt.Errorf("errors")),
			},
			expectedError: true,
		},

		{
			name: "install network-qos failed",
			apiCall: []*gomock.Call{
				mockExec.EXPECT().CommandContext(gomock.Any(), "sudo /bin/cp -f /usr/local/bin/bwm_tc.o /usr/share/bwmcli").Return("", nil),
				mockExec.EXPECT().CommandContext(gomock.Any(), "sudo /bin/cp -f /usr/local/bin/network-qos /opt/cni/bin").Return("", fmt.Errorf("errors")),
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		actualErr := InstallNetworkQoS()
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name)
	}
}
