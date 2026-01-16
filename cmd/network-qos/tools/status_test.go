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

func TestStatusCmdExecute(t *testing.T) {
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
			name: "[status] json Marshal failed",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingStatus().Return(&api.EbpfNetThrottling{
					TLast:         65923918434500,
					Rate:          50000000,
					TXBytes:       20000,
					OnlineTXBytes: 20000,
					TStart:        88736282681743,
					EbpfNetThrottlingStatus: api.EbpfNetThrottlingStatus{
						CheckTimes:  300,
						HighTimes:   100,
						LowTimes:    200,
						OnlinePKTs:  100,
						OfflinePKTs: 100,
					},
				}, nil),
			},
			jsonMarshalErr:       fmt.Errorf("json marshal failed"),
			expectedErrOut:       `execute command[status] failed, error:failed to marshal throttling status &{65923918434500 50000000 20000 20000 88736282681743 {300 100 200 100 100 0 0 0}} to json: json marshal failed` + "\n",
			expectedOsExitCalled: true,
		},
		{
			name: "[status] get conf successfully",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingStatus().Return(&api.EbpfNetThrottling{
					TLast:         65923918434483,
					Rate:          60000000,
					TXBytes:       10000,
					OnlineTXBytes: 10000,
					TStart:        88736282681743,
					EbpfNetThrottlingStatus: api.EbpfNetThrottlingStatus{
						CheckTimes:  1000,
						HighTimes:   500,
						LowTimes:    500,
						OnlinePKTs:  500,
						OfflinePKTs: 500,
					},
				}, nil),
			},
			expectedOut: `throttling status: {"latest_offline_packet_send_time":65923918434483,"offline_bandwidth_limit":60000000,"offline_tx_bytes":10000,"online_tx_bytes":10000,"latest_check_time":88736282681743,"check_times":1000,"high_times":500,"low_times":500,"online_tx_packages":500,"offline_tx_packages":500,"offline_prio":0,"latest_online_bandwidth":0,"latest_offline_bandwidth":0}` + "\n",
		},

		{
			name: "[status] get conf failed",
			apiCall: []*gomock.Call{
				mockThr.EXPECT().GetThrottlingStatus().Return(nil, fmt.Errorf("request failed")),
			},
			expectedErrOut:       `execute command[status] failed, error:failed to get throttling status: request failed` + "\n",
			expectedOsExitCalled: true,
		},
	}

	exitCalled := false
	statusPatch := gomonkey.NewPatches()
	statusPatch.ApplyFunc(os.Exit, func(code int) {
		exitCalled = true
	})
	defer statusPatch.Reset()

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
		cmd := newStatusCmd(out, errOut)
		cmd.Execute()
		if jsonMarPatch != nil {
			jsonMarPatch.Reset()
		}
		assert.Equal(t, tc.expectedOsExitCalled, exitCalled, tc.name)
		assert.Equal(t, tc.expectedOut, out.String(), tc.name)
		assert.Equal(t, tc.expectedErrOut, errOut.String(), tc.name)
	}
}
