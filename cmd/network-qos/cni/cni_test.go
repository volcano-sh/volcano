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
	"fmt"
	"os"
	"testing"

	cilliumbpf "github.com/cilium/ebpf"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"volcano.sh/volcano/pkg/networkqos/api"
	"volcano.sh/volcano/pkg/networkqos/tc"
	mocktc "volcano.sh/volcano/pkg/networkqos/tc/mocks"
	"volcano.sh/volcano/pkg/networkqos/throttling"
	mockthrottling "volcano.sh/volcano/pkg/networkqos/throttling/mocks"
)

var fileWithoutNetworkQos = `{
    "cniVersion": "0.3.1",
    "name": "default-network",
    "args": {
        "check-interval": "10000000",
        "offline-high-bandwidth": "50MB",
        "offline-low-bandwidth": "20MB",
        "colocation": "true",
        "online-bandwidth-watermark": "50MB"
    },
    "name": "network-qos",
    "type": "network-qos",
    "prevResult": {
        "cniVersion":"0.3.1",
        "ips":[{"version":"4","address":"10.3.3.190/17"}],
        "dns":{}
    }
}`

func TestCmdAdd(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	mockTc := mocktc.NewMockTC(mockController)
	tc.SetTcCmd(mockTc)
	mockThr := mockthrottling.NewMockThrottlingConfig(mockController)
	throttling.SetNetworkThrottlingConfig(mockThr)

	testCases := []struct {
		name          string
		args          *skel.CmdArgs
		apiCall       []*gomock.Call
		expectedError bool
	}{
		{
			name: "add dev successful",
			args: &skel.CmdArgs{
				Netns:     "test-ns",
				IfName:    "eth0",
				StdinData: []byte(fileWithoutNetworkQos),
			},
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(&api.EbpfNetThrottlingConfig{
					Interval:  10000000,
					WaterLine: 1000,
					LowRate:   1000,
					HighRate:  1000,
				}, nil),
				mockTc.EXPECT().PreAddFilter("test-ns", "eth0").Return(true, nil),
				mockTc.EXPECT().AddFilter("test-ns", "eth0").Return(nil),
			},
			expectedError: false,
		},

		{
			name: "throttling conf map not exists && throttling enabled",
			args: &skel.CmdArgs{
				Netns:     "test-ns2",
				IfName:    "eth0",
				StdinData: []byte(fileWithoutNetworkQos),
			},
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(nil, cilliumbpf.ErrKeyNotExist),
				mockThr.EXPECT().CreateThrottlingConfig("50MB", "20MB", "50MB", "10000000").Return(&api.EbpfNetThrottlingConfig{
					WaterLine: 1000,
					LowRate:   1000,
					HighRate:  1000,
					Interval:  10000000,
				}, nil),
				mockTc.EXPECT().PreAddFilter("test-ns2", "eth0").Return(true, nil),
				mockTc.EXPECT().AddFilter("test-ns2", "eth0").Return(nil),
			},
			expectedError: false,
		},

		{
			name: "throttling conf map pinned file not exists && throttling enabled",
			args: &skel.CmdArgs{
				Netns:     "test-ns3",
				IfName:    "eth0",
				StdinData: []byte(fileWithoutNetworkQos),
			},
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(nil, os.ErrNotExist),
				mockThr.EXPECT().CreateThrottlingConfig("50MB", "20MB", "50MB", "10000000").Return(&api.EbpfNetThrottlingConfig{
					WaterLine: 1000,
					LowRate:   1000,
					HighRate:  1000,
					Interval:  10000000,
				}, nil),
				mockTc.EXPECT().PreAddFilter("test-ns3", "eth0").Return(true, nil),
				mockTc.EXPECT().AddFilter("test-ns3", "eth0").Return(nil),
			},
			expectedError: false,
		},

		{
			name: "throttling conf map pinned file not exists && throttling enabled && create conf map failed",
			args: &skel.CmdArgs{
				Netns:     "test-ns4",
				IfName:    "eth0",
				StdinData: []byte(fileWithoutNetworkQos),
			},
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(nil, os.ErrNotExist),
				mockThr.EXPECT().CreateThrottlingConfig("50MB", "20MB", "50MB", "10000000").Return(nil, fmt.Errorf("create failed")),
			},
			expectedError: true,
		},

		{
			name: "add tc filter failed",
			args: &skel.CmdArgs{
				Netns:     "test-ns5",
				IfName:    "eth0",
				StdinData: []byte(fileWithoutNetworkQos),
			},
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(nil, os.ErrNotExist),
				mockThr.EXPECT().CreateThrottlingConfig("50MB", "20MB", "50MB", "10000000").Return(&api.EbpfNetThrottlingConfig{
					WaterLine: 1000,
					LowRate:   1000,
					HighRate:  1000,
					Interval:  10000000,
				}, nil),
				mockTc.EXPECT().PreAddFilter("test-ns5", "eth0").Return(true, nil),
				mockTc.EXPECT().AddFilter("test-ns5", "eth0").Return(fmt.Errorf("add failed")),
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		gomock.InOrder(tc.apiCall...)
		actualErr := cmdAdd(tc.args)
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name, actualErr)
	}
}

func TestCmdDel(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	mockTc := mocktc.NewMockTC(mockController)
	tc.SetTcCmd(mockTc)

	testCases := []struct {
		name          string
		args          *skel.CmdArgs
		apiCall       []*gomock.Call
		expectedError bool
	}{

		{
			name: "delete dev successful",
			args: &skel.CmdArgs{
				Netns:     "test-ns",
				IfName:    "eth0",
				StdinData: []byte(fileWithoutNetworkQos),
			},
			apiCall: []*gomock.Call{
				mockTc.EXPECT().RemoveFilter("test-ns", "eth0").Return(nil),
			},
			expectedError: false,
		},

		{
			name: "delete tc filter failed",
			args: &skel.CmdArgs{
				Netns:     "test-ns2",
				IfName:    "eth0",
				StdinData: []byte(fileWithoutNetworkQos),
			},
			apiCall: []*gomock.Call{
				mockTc.EXPECT().RemoveFilter("test-ns2", "eth0").Return(fmt.Errorf("delete failed")),
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		gomock.InOrder(tc.apiCall...)
		actualErr := cmdDel(tc.args)
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name, actualErr)
	}
}
