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

import "testing"

func TestVolcanoAgentOptions_Validate(t *testing.T) {
	tests := []struct {
		name                  string
		OverSubscriptionRatio int
		wantErr               bool
	}{
		{
			name:                  "over subscription ratio lower than 0",
			OverSubscriptionRatio: -1,
			wantErr:               true,
		},
		{
			name:                  "over subscription ratio greater than 100",
			OverSubscriptionRatio: 110,
			wantErr:               false,
		},
		{
			name:                  "valid over subscription ratio",
			OverSubscriptionRatio: 80,
			wantErr:               false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := &VolcanoAgentOptions{
				OverSubscriptionRatio: tt.OverSubscriptionRatio,
			}
			if err := options.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
