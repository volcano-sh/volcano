/*
Copyright 2025 The Volcano Authors.

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

package validate

import "testing"

func TestIsDAG(t *testing.T) {
	tests := []struct {
		graph map[string][]string
		isDAG bool
		isErr bool
	}{
		{map[string][]string{"1": {"2"}, "2": {"1", "3"}}, false, true},
		{map[string][]string{"1": {"2"}, "2": {"3"}, "3": {"1"}}, false, false},
		{map[string][]string{"1": {"2"}, "2": {"1", "3"}, "3": {}}, false, false},
		{map[string][]string{"1": {"2", "3"}, "2": {"3"}, "3": {"4"}, "4": {}}, true, false},
		{map[string][]string{"1": {"2", "3"}, "2": {"4", "5"}, "3": {}, "4": {}, "5": {}}, true, false},
		{map[string][]string{"1": {"2", "3", "4"}, "2": {"3"}, "3": {"4"}, "4": {"5"}, "5": {}}, true, false},
		{map[string][]string{"1": {"2", "3", "4"}, "2": {"3", "5"}, "3": {"5"}, "4": {"3", "5"}, "5": {}}, true, false},
		{map[string][]string{"1": {"2", "3", "4"}, "2": {"3", "5"}, "3": {"5"}, "4": {"3", "5"}, "5": {"5", "3"}}, false, false},
	}

	for i, test := range tests {
		data, err := LoadVertexs(test.graph)
		if (err != nil) != test.isErr {
			t.Fatalf("Failed case #%d. LoadVertexs error %v, data: %v", i, err, data)
		}

		if test.isErr == false {
			if got := IsDAG(data); got != test.isDAG && test.isErr == false {
				t.Fatalf("Failed case #%d. Want %v got %v", i, test.isDAG, got)
			}
		}
	}
}
