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

package version

import "testing"

func TestHigherOrEqual(t *testing.T) {
	tests := []struct {
		name     string
		version1 string
		version2 string
		want     bool
	}{
		{
			name:     "v1 > v2",
			version1: "23.03 (LTS-SP1)",
			version2: "22.03 (LTS-SP2)",
			want:     true,
		},
		{
			name:     "v1 > v2",
			version1: "23.03 (LTS-SP2)",
			version2: "22.03 (LTS-SP2)",
			want:     true,
		},
		{
			name:     "v1 = v2",
			version1: "22.03 (LTS-SP2)",
			version2: "22.03 (LTS-SP2)",
			want:     true,
		},
		{
			name:     "v1 < v2",
			version1: "21.03 (LTS-SP2)",
			version2: "22.03 (LTS-SP2)",
			want:     false,
		},
		{
			name:     "v1 < v2",
			version1: "22.03 (LTS-SP1)",
			version2: "22.03 (LTS-SP2)",
			want:     false,
		},
		{
			name:     "invalid version",
			version1: "22.03LTS-SP2",
			version2: "22.03 (LTS-SP2)",
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HigherOrEqual(tt.version1, tt.version2); got != tt.want {
				t.Errorf("HigherOrEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}
