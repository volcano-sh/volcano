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

	"volcano.sh/volcano/pkg/networkqos/api"
	"volcano.sh/volcano/pkg/networkqos/throttling"
	mockthrottling "volcano.sh/volcano/pkg/networkqos/throttling/mocks"
)

func TestGetCmdExecute(t *testing.T) {
	mockController := gomock.NewController(t)
	defer mockController.Finish()

	mockThr := mockthrottling.NewMockThrottlingConfig(mockController)
	throttling.SetNetworkThrottlingConfig(mockThr)

	testCases := []struct {
		name                 string
		apiCall              []*gomock.Call
		jsonMarshalErr       error
		expectedOut          string
		expectedErrOut       string
		expectedOsExitCalled bool
	}{
		{
			name: "[show] json Marshal failed",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(&api.EbpfNetThrottlingConfig{
					Interval:  10000000,
					WaterLine: 2000,
					LowRate:   3000,
					HighRate:  4000,
				}, nil),
			},
			jsonMarshalErr:       fmt.Errorf("json marshal failed"),
			expectedErrOut:       `execute command[show] failed, error:failed to marshal throttling config &{2000 10000000 3000 4000} to json: json marshal failed` + "\n",
			expectedOsExitCalled: true,
		},

		{
			name: "[show] get conf successfully",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(&api.EbpfNetThrottlingConfig{
					Interval:  10000000,
					WaterLine: 1500,
					LowRate:   2500,
					HighRate:  3500,
				}, nil),
			},
			expectedOut: `network-qos config: {"online_bandwidth_watermark":"12000bps","interval":10000000,"offline_low_bandwidth":"20000bps","offline_high_bandwidth":"28000bps"}` + "\n",
		},

		{
			name: "[show] get conf failed",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(nil, fmt.Errorf("request failed")),
			},
			expectedErrOut:       `execute command[show] failed, error:failed to get throttling config: request failed` + "\n",
			expectedOsExitCalled: true,
		},
	}

	exitCalled := false
	getPatch := gomonkey.NewPatches()
	getPatch.ApplyFunc(os.Exit, func(code int) {
		exitCalled = true
	})
	defer getPatch.Reset()

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
		cmd := newGetCmd(out, errOut)
		cmd.Execute()
		if jsonMarshalPatch != nil {
			jsonMarshalPatch.Reset()
		}
		assert.Equal(t, tc.expectedOsExitCalled, exitCalled, tc.name)
		assert.Equal(t, tc.expectedOut, out.String(), tc.name)
		assert.Equal(t, tc.expectedErrOut, errOut.String(), tc.name)
	}
}
