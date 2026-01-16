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
	"math"
	"os"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	cilliumbpf "github.com/cilium/ebpf"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	"volcano.sh/volcano/pkg/networkqos/api"
	"volcano.sh/volcano/pkg/networkqos/throttling"
	mockthrottling "volcano.sh/volcano/pkg/networkqos/throttling/mocks"
)

func TestResetCmdRun(t *testing.T) {
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
			name: "[reset] json Marshal failed",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(&api.EbpfNetThrottlingConfig{
					Interval:  10000000,
					WaterLine: 1000,
					LowRate:   200,
					HighRate:  500,
				}, nil),
				mockThr.EXPECT().CreateOrUpdateThrottlingConfig("", "1024Tibps", "1024Tibps", "10000000").Return(&api.EbpfNetThrottlingConfig{
					Interval:  10000000,
					WaterLine: math.MaxUint64,
					LowRate:   math.MaxUint64,
					HighRate:  math.MaxUint64,
				},
					nil),
			},
			jsonMarshalErr:       fmt.Errorf("json marshal failed"),
			expectedErrOut:       `execute command[reset] failed, error:failed to marshal throttling conf: &{18446744073709551615 10000000 18446744073709551615 18446744073709551615} to json json marshal failed` + "\n",
			expectedOsExitCalled: true,
		},

		{
			name: "[reset] reset existed conf successfully",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(&api.EbpfNetThrottlingConfig{
					Interval:  10000000,
					WaterLine: 2000,
					LowRate:   800,
					HighRate:  1200,
				}, nil),
				mockThr.EXPECT().CreateOrUpdateThrottlingConfig("", "1024Tibps", "1024Tibps", "10000000").Return(&api.EbpfNetThrottlingConfig{
					Interval:  10000000,
					WaterLine: math.MaxUint64,
					LowRate:   math.MaxUint64,
					HighRate:  math.MaxUint64,
				},
					nil),
			},
			expectedOut: `throttling config reset: {"online_bandwidth_watermark":18446744073709551615,"interval":10000000,"offline_low_bandwidth":18446744073709551615,"offline_high_bandwidth":18446744073709551615}` + "\n",
		},

		{
			name: "[reset] cni conf ebpf map not exists",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(nil, cilliumbpf.ErrKeyNotExist),
			},
			expectedOut: `throttling config does not exist, reset successfully`,
		},

		{
			name: "[reset] cni conf ebpf map pinned file not exists",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(nil, os.ErrNotExist),
			},
			expectedOut: `throttling config does not exist, reset successfully`,
		},

		{
			name: "[reset] get cni conf map failed",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(nil, fmt.Errorf("failed to get ebpf map")),
			},
			expectedErrOut:       `execute command[reset] failed, error:failed to get throttling config: failed to get ebpf map` + "\n",
			expectedOsExitCalled: true,
		},

		{
			name: "[reset] update cni conf map failed",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingConfig().Return(&api.EbpfNetThrottlingConfig{
					Interval:  10000000,
					WaterLine: 5000,
					LowRate:   1000,
					HighRate:  3000,
				}, nil),
				mockThr.EXPECT().CreateOrUpdateThrottlingConfig("", "1024Tibps", "1024Tibps", "10000000").Return(nil, fmt.Errorf("update failed")),
			},
			expectedErrOut:       `execute command[reset] failed, error:failed to update throttling config: update failed` + "\n",
			expectedOsExitCalled: true,
		},
	}

	exitCalled := false
	resetPatch := gomonkey.NewPatches()
	resetPatch.ApplyFunc(os.Exit, func(code int) {
		exitCalled = true
	})

	for _, tc := range testCases {
		exitCalled = false
		var jsonMarPatch *gomonkey.Patches
		if tc.jsonMarshalErr != nil {
			jsonMarPatch = gomonkey.NewPatches()
			jsonMarPatch.ApplyFunc(json.Marshal, func(v interface{}) ([]byte, error) {
				return nil, tc.jsonMarshalErr
			})
		}

		out := &bytes.Buffer{}
		errOut := &bytes.Buffer{}
		cmd := newResetCmd(out, errOut)
		cmd.Execute()
		if jsonMarPatch != nil {
			jsonMarPatch.Reset()
		}
		assert.Equal(t, tc.expectedOsExitCalled, exitCalled, tc.name)
		assert.Equal(t, tc.expectedOut, out.String(), tc.name)
		assert.Equal(t, tc.expectedErrOut, errOut.String(), tc.name)
	}
}
