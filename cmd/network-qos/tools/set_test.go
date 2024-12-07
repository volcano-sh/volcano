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
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"volcano.sh/volcano/cmd/network-qos/tools/options"
	"volcano.sh/volcano/pkg/networkqos/api"
	"volcano.sh/volcano/pkg/networkqos/throttling"
	mockthrottling "volcano.sh/volcano/pkg/networkqos/throttling/mocks"
)

func TestSetCmdRun(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	mockThr := mockthrottling.NewMockThrottlingConfig(mockController)
	throttling.SetNetworkThrottlingConfig(mockThr)

	testCases := []struct {
		name                 string
		opt                  options.Options
		apiCall              []*gomock.Call
		jsonMarshalErr       error
		expectedOut          string
		expectedErrOut       string
		expectedOsExitCalled bool
	}{
		{
			name: "[set] json Marshal failed",
			opt: options.Options{
				CheckoutInterval:         "100",
				OnlineBandwidthWatermark: "100MB",
				OfflineLowBandwidth:      "30MB",
				OfflineHighBandwidth:     "50MB",
			},
			apiCall: []*gomock.Call{
				mockThr.EXPECT().CreateOrUpdateThrottlingConfig("100MB", "30MB", "50MB", "100").Return(&api.EbpfNetThrottlingConfig{
					Interval:  10000000,
					WaterLine: 100 * 1024 * 1024,
					LowRate:   30 * 1024 * 1024,
					HighRate:  50 * 1024 * 1024,
				},
					nil),
			},
			jsonMarshalErr:       fmt.Errorf("json marshal failed"),
			expectedErrOut:       `execute command[set] failed, error:failed to create/update throttling config: json marshal failed` + "\n",
			expectedOsExitCalled: true,
		},

		{
			name: "[set] set throttling conf map successfully",
			opt: options.Options{
				CheckoutInterval:         "200",
				OnlineBandwidthWatermark: "1000MB",
				OfflineLowBandwidth:      "200MB",
				OfflineHighBandwidth:     "500MB",
			},
			apiCall: []*gomock.Call{
				mockThr.EXPECT().CreateOrUpdateThrottlingConfig("1000MB", "200MB", "500MB", "200").Return(&api.EbpfNetThrottlingConfig{
					Interval:  20000000,
					WaterLine: 1000 * 1024 * 1024,
					LowRate:   200 * 1024 * 1024,
					HighRate:  500 * 1024 * 1024,
				},
					nil),
			},
			expectedOut: `throttling config set: {"online_bandwidth_watermark":1048576000,"interval":20000000,"offline_low_bandwidth":209715200,"offline_high_bandwidth":524288000}` + "\n",
		},

		{
			name: "[set] set throttling conf map failed",
			opt: options.Options{
				CheckoutInterval:         "300",
				OnlineBandwidthWatermark: "800MB",
				OfflineLowBandwidth:      "250MB",
				OfflineHighBandwidth:     "350MB",
			},
			apiCall: []*gomock.Call{
				mockThr.EXPECT().CreateOrUpdateThrottlingConfig("800MB", "250MB", "350MB", "300").Return(nil,
					fmt.Errorf("failed to update conf map")),
			},
			expectedErrOut:       `execute command[set] failed, error:failed to create/update throttling config: failed to update conf map` + "\n",
			expectedOsExitCalled: true,
		},
	}

	exitCalled := false
	setPatch := gomonkey.NewPatches()
	setPatch.ApplyFunc(os.Exit, func(code int) {
		exitCalled = true
	})
	defer setPatch.Reset()

	for _, tc := range testCases {
		exitCalled = false
		var jsonMarshalPatch *gomonkey.Patches
		if tc.jsonMarshalErr != nil {
			jsonMarshalPatch = gomonkey.NewPatches()
			jsonMarshalPatch.ApplyFunc(json.Marshal, func(v interface{}) ([]byte, error) {
				return nil, tc.jsonMarshalErr
			})
		}

		out := &bytes.Buffer{}
		errOut := &bytes.Buffer{}
		opt = tc.opt
		cmd := newSetCmd(out, errOut)
		cmd.Execute()
		if jsonMarshalPatch != nil {
			jsonMarshalPatch.Reset()
		}
		assert.Equal(t, tc.expectedOsExitCalled, exitCalled, tc.name)
		assert.Equal(t, tc.expectedOut, out.String(), tc.name)
		assert.Equal(t, tc.expectedErrOut, errOut.String(), tc.name)
	}
}
