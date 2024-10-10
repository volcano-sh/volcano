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

package options

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestAddFlags(t *testing.T) {
	testCases := []struct {
		name            string
		args            []string
		opts            *Options
		expectedOptions *Options
		expectedErr     bool
	}{
		{
			name: "test set flags",
			args: []string{"--check-interval=20000", "--online-bandwidth-watermark=500Mbps", "--offline-low-bandwidth=300Mbps", "--offline-high-bandwidth=700Mbps"},
			opts: &Options{},
			expectedOptions: &Options{
				CheckoutInterval:         "20000",
				OnlineBandwidthWatermark: "500Mbps",
				OfflineLowBandwidth:      "300Mbps",
				OfflineHighBandwidth:     "700Mbps",
			},
			expectedErr: false,
		},
	}

	for _, tc := range testCases {
		cmds := &cobra.Command{}
		tc.opts.AddFlags(cmds)
		parseErr := cmds.Flags().Parse(tc.args)
		if parseErr != nil {
			t.Errorf("unexpected parse err: %v", parseErr)
		}
		assert.Equal(t, tc.expectedOptions, tc.opts, tc.name)
	}
}
