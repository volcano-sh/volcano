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

package throttling

import (
	"fmt"
	"os"
	"path"
	"testing"

	cilliumbpf "github.com/cilium/ebpf"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"volcano.sh/volcano/pkg/networkqos/api"
	networkqosbpf "volcano.sh/volcano/pkg/networkqos/utils/ebpf"
	mockmap "volcano.sh/volcano/pkg/networkqos/utils/ebpf/mocks"
)

func TestCreateThrottlingConfig(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()
	mockEbpfMap := mockmap.NewMockEbpfMap(mockController)
	networkqosbpf.SetEbpfMap(mockEbpfMap)

	testCases := []struct {
		name                     string
		onlineBandwidthWatermark string
		offlineLowBandwidth      string
		offlineHighBandwidth     string
		checkInterval            string
		apiCall                  []*gomock.Call
		expectedThrottlingConfig *api.EbpfNetThrottlingConfig
		expectedError            bool
	}{
		{
			name:                     "case-1",
			onlineBandwidthWatermark: "100Mbps",
			offlineLowBandwidth:      "21Mbps",
			offlineHighBandwidth:     "55Mbps",
			checkInterval:            "50000",
			apiCall: []*gomock.Call{
				mockEbpfMap.EXPECT().CreateAndPinArrayMap(path.Dir(TCEbpfPath), ThrottleConfigMapName, gomock.Any()).Return(nil),
			},
			expectedThrottlingConfig: &api.EbpfNetThrottlingConfig{
				Interval:  50000,
				WaterLine: 100 * 1000 * 1000 / 8,
				LowRate:   21 * 1000 * 1000 / 8,
				HighRate:  55 * 1000 * 1000 / 8,
			},
			expectedError: false,
		},

		{
			name:                     "illegal onlineBandwidthWatermark",
			onlineBandwidthWatermark: "890M",
			offlineLowBandwidth:      "456Mbps",
			offlineHighBandwidth:     "600Mbps",
			checkInterval:            "5000",
			expectedThrottlingConfig: nil,
			expectedError:            true,
		},

		{
			name:                     "illegal lowBandwidth",
			onlineBandwidthWatermark: "580Mbps",
			offlineLowBandwidth:      "280M",
			offlineHighBandwidth:     "300Mbps",
			checkInterval:            "800000",
			expectedThrottlingConfig: nil,
			expectedError:            true,
		},

		{
			name:                     "illegal highBandwidth",
			onlineBandwidthWatermark: "50Mbps",
			offlineLowBandwidth:      "20Mbps",
			offlineHighBandwidth:     "50M",
			checkInterval:            "10000000",
			expectedThrottlingConfig: nil,
			expectedError:            true,
		},

		{
			name:                     "illegal intervalUInt",
			onlineBandwidthWatermark: "700Mbps",
			offlineLowBandwidth:      "350Mbps",
			offlineHighBandwidth:     "400Mbps",
			checkInterval:            "10000000.0",
			expectedThrottlingConfig: nil,
			expectedError:            true,
		},

		{
			name:                     "create-map-failed",
			onlineBandwidthWatermark: "9000Mbps",
			offlineLowBandwidth:      "3000Mbps",
			offlineHighBandwidth:     "6000Mbps",
			checkInterval:            "10000000",
			apiCall: []*gomock.Call{
				mockEbpfMap.EXPECT().CreateAndPinArrayMap(path.Dir(TCEbpfPath), ThrottleConfigMapName, gomock.Any()).Return(fmt.Errorf("create map failed")),
			},
			expectedThrottlingConfig: nil,
			expectedError:            true,
		},
	}

	for _, tc := range testCases {
		throttling := &NetworkThrottlingConfig{}
		gomock.InOrder(tc.apiCall...)
		actualThrottlingConfig, actualErr := throttling.CreateThrottlingConfig(tc.onlineBandwidthWatermark, tc.offlineLowBandwidth, tc.offlineHighBandwidth, tc.checkInterval)
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name)
		assert.Equal(t, tc.expectedThrottlingConfig, actualThrottlingConfig, tc.name)
	}
}

func TestCreateOrUpdateThrottlingConfig(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()
	mockEbpfMap := mockmap.NewMockEbpfMap(mockController)
	networkqosbpf.SetEbpfMap(mockEbpfMap)
	throttling := &NetworkThrottlingConfig{}

	testCases := []struct {
		name                     string
		onlineBandwidthWatermark string
		offlineLowBandwidth      string
		offlineHighBandwidth     string
		checkInterval            string
		apiCall                  []*gomock.Call
		expectedThrottlingConfig *api.EbpfNetThrottlingConfig
		expectedError            bool
	}{
		{
			name:                     "map not exists",
			onlineBandwidthWatermark: "50Mbps",
			offlineLowBandwidth:      "20Mbps",
			offlineHighBandwidth:     "50Mbps",
			checkInterval:            "10000000",
			apiCall: []*gomock.Call{
				mockEbpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(cilliumbpf.ErrKeyNotExist),
				mockEbpfMap.EXPECT().CreateAndPinArrayMap(path.Dir(TCEbpfPath), ThrottleConfigMapName, gomock.Any()).Return(nil),
			},
			expectedThrottlingConfig: &api.EbpfNetThrottlingConfig{
				Interval:  10000000,
				WaterLine: 50 * 1000 * 1000 / 8,
				LowRate:   20 * 1000 * 1000 / 8,
				HighRate:  50 * 1000 * 1000 / 8,
			},
			expectedError: false,
		},

		{
			name:                     "pin file not exists",
			onlineBandwidthWatermark: "100Mbps",
			offlineLowBandwidth:      "30Mbps",
			offlineHighBandwidth:     "60Mbps",
			checkInterval:            "20000000",
			apiCall: []*gomock.Call{
				mockEbpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(os.ErrNotExist),
				mockEbpfMap.EXPECT().CreateAndPinArrayMap(path.Dir(TCEbpfPath), ThrottleConfigMapName, gomock.Any()).Return(nil),
			},
			expectedThrottlingConfig: &api.EbpfNetThrottlingConfig{
				Interval:  20000000,
				WaterLine: 100 * 1000 * 1000 / 8,
				LowRate:   30 * 1000 * 1000 / 8,
				HighRate:  60 * 1000 * 1000 / 8,
			},
			expectedError: false,
		},

		{
			name:                "update lowBandwidth value",
			offlineLowBandwidth: "150Mbps",
			checkInterval:       "25000000",
			apiCall: []*gomock.Call{
				mockEbpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
				mockEbpfMap.EXPECT().UpdateMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
			},
			expectedThrottlingConfig: &api.EbpfNetThrottlingConfig{
				Interval:  25000000,
				WaterLine: 0,
				LowRate:   150 * 1000 * 1000 / 8,
				HighRate:  0,
			},
			expectedError: false,
		},

		{
			name:                     "update onlineBandwidthWatermark value",
			onlineBandwidthWatermark: "350Mbps",
			checkInterval:            "30000000",
			apiCall: []*gomock.Call{
				// LookUpMapValue returns config map with 0 values
				mockEbpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
				mockEbpfMap.EXPECT().UpdateMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
			},
			expectedThrottlingConfig: &api.EbpfNetThrottlingConfig{
				Interval:  30000000,
				WaterLine: 350 * 1000 * 1000 / 8,
				LowRate:   0,
				HighRate:  0,
			},
			expectedError: false,
		},

		{
			name:                     "update map values",
			onlineBandwidthWatermark: "2300Mbps",
			offlineLowBandwidth:      "800Mbps",
			offlineHighBandwidth:     "1200Mbps",
			checkInterval:            "80000000",
			apiCall: []*gomock.Call{
				// LookUpMapValue returns config map with 0 values
				mockEbpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
				mockEbpfMap.EXPECT().UpdateMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
			},
			expectedThrottlingConfig: &api.EbpfNetThrottlingConfig{
				Interval:  80000000,
				WaterLine: 2300 * 1000 * 1000 / 8,
				LowRate:   800 * 1000 * 1000 / 8,
				HighRate:  1200 * 1000 * 1000 / 8,
			},
			expectedError: false,
		},

		{
			name:                     "illegal onlineBandwidthWatermark",
			onlineBandwidthWatermark: "50M",
			offlineLowBandwidth:      "330Mbps",
			offlineHighBandwidth:     "550Mbps",
			checkInterval:            "70000000",
			apiCall: []*gomock.Call{
				// LookUpMapValue returns config map with 0 values
				mockEbpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
			},
			expectedThrottlingConfig: nil,
			expectedError:            true,
		},

		{
			name:                     "illegal lowBandwidth",
			onlineBandwidthWatermark: "200Mbps",
			offlineLowBandwidth:      "70M",
			offlineHighBandwidth:     "150Mbps",
			checkInterval:            "10000000",
			apiCall: []*gomock.Call{
				// LookUpMapValue returns config map with 0 values
				mockEbpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
			},
			expectedThrottlingConfig: nil,
			expectedError:            true,
		},

		{
			name:                     "illegal highBandwidth",
			onlineBandwidthWatermark: "50Mbps",
			offlineLowBandwidth:      "30Mbps",
			offlineHighBandwidth:     "70M",
			checkInterval:            "10000000",
			apiCall: []*gomock.Call{
				// LookUpMapValue returns config map with 0 values
				mockEbpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
			},
			expectedThrottlingConfig: nil,
			expectedError:            true,
		},

		{
			name:                     "illegal intervalUInt",
			onlineBandwidthWatermark: "2000Mbps",
			offlineLowBandwidth:      "1000Mbps",
			offlineHighBandwidth:     "1500Mbps",
			checkInterval:            "50000000.0",
			apiCall: []*gomock.Call{
				// LookUpMapValue returns config map with 0 values
				mockEbpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
			},
			expectedThrottlingConfig: nil,
			expectedError:            true,
		},
	}

	for _, tc := range testCases {
		gomock.InOrder(tc.apiCall...)
		actualThrottlingConfig, actualErr := throttling.CreateOrUpdateThrottlingConfig(tc.onlineBandwidthWatermark, tc.offlineLowBandwidth, tc.offlineHighBandwidth, tc.checkInterval)
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name)
		assert.Equal(t, tc.expectedThrottlingConfig, actualThrottlingConfig, tc.name)
	}
}

func TestDeleteThrottlingConfig(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()
	ebpfMap := mockmap.NewMockEbpfMap(mockController)
	networkqosbpf.SetEbpfMap(ebpfMap)
	throttling := &NetworkThrottlingConfig{}

	testCases := []struct {
		name          string
		apiCall       []*gomock.Call
		expectedError bool
	}{
		{
			name: "unpin successfully",
			apiCall: []*gomock.Call{
				ebpfMap.EXPECT().UnpinArrayMap(ThrottleConfigMapPinPath).Return(nil),
				ebpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(cilliumbpf.ErrKeyNotExist),
			},
			expectedError: false,
		},

		{
			name: "unpin failed",
			apiCall: []*gomock.Call{
				ebpfMap.EXPECT().UnpinArrayMap(ThrottleConfigMapPinPath).Return(fmt.Errorf("unpin failed")),
			},
			expectedError: true,
		},

		{
			name: "delete failed 1",
			apiCall: []*gomock.Call{
				ebpfMap.EXPECT().UnpinArrayMap(ThrottleConfigMapPinPath).Return(nil),
				ebpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
			},
			expectedError: true,
		},

		{
			name: "delete failed 2",
			apiCall: []*gomock.Call{
				ebpfMap.EXPECT().UnpinArrayMap(ThrottleConfigMapPinPath).Return(nil),
				ebpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(fmt.Errorf("fake error")),
			},
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		gomock.InOrder(tc.apiCall...)
		actualErr := throttling.DeleteThrottlingConfig()
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name)
	}
}

func TestGetThrottlingConfig(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()
	ebpfMap := mockmap.NewMockEbpfMap(mockController)
	networkqosbpf.SetEbpfMap(ebpfMap)
	throttling := &NetworkThrottlingConfig{}

	testCases := []struct {
		name                     string
		apiCall                  []*gomock.Call
		expectedThrottlingConfig *api.EbpfNetThrottlingConfig
		expectedError            bool
	}{
		{
			name: "map not exists",
			apiCall: []*gomock.Call{
				ebpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(cilliumbpf.ErrKeyNotExist),
			},
			expectedThrottlingConfig: nil,
			expectedError:            true,
		},

		{
			name: "map exists",
			apiCall: []*gomock.Call{
				ebpfMap.EXPECT().LookUpMapValue(ThrottleConfigMapPinPath, uint32(0), gomock.Any()).Return(nil),
			},
			expectedThrottlingConfig: &api.EbpfNetThrottlingConfig{
				Interval:  0,
				WaterLine: 0,
				LowRate:   0,
				HighRate:  0,
			},
			expectedError: false,
		},
	}

	for _, tc := range testCases {
		gomock.InOrder(tc.apiCall...)
		actualThrottlingConfig, actualErr := throttling.GetThrottlingConfig()
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name)
		assert.Equal(t, tc.expectedThrottlingConfig, actualThrottlingConfig, tc.name)
	}
}

func TestGetThrottlingStatus(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()
	ebpfMap := mockmap.NewMockEbpfMap(mockController)
	networkqosbpf.SetEbpfMap(ebpfMap)
	throttling := &NetworkThrottlingConfig{}

	testCases := []struct {
		name                     string
		apiCall                  []*gomock.Call
		expectedThrottlingStatus *api.EbpfNetThrottling
		expectedError            bool
	}{
		{
			name: "map not exists",
			apiCall: []*gomock.Call{
				ebpfMap.EXPECT().LookUpMapValue(ThrottleStatusMapPinPath, uint32(0), gomock.Any()).Return(cilliumbpf.ErrKeyNotExist),
			},
			expectedThrottlingStatus: nil,
			expectedError:            true,
		},

		{
			name: "map exists",
			apiCall: []*gomock.Call{
				ebpfMap.EXPECT().LookUpMapValue(ThrottleStatusMapPinPath, uint32(0), gomock.Any()).Return(nil),
			},
			expectedThrottlingStatus: &api.EbpfNetThrottling{},
			expectedError:            false,
		},
	}

	for _, tc := range testCases {
		gomock.InOrder(tc.apiCall...)
		actualThrottlingStatus, actualErr := throttling.GetThrottlingStatus()
		assert.Equal(t, tc.expectedError, actualErr != nil, tc.name)
		assert.Equal(t, tc.expectedThrottlingStatus, actualThrottlingStatus, tc.name)
	}
}
