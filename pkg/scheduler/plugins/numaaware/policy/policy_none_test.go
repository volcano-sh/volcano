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

package policy

import (
	"reflect"
	"testing"
)

func Test_none_predicate(t *testing.T) {
	testCases := []struct {
		name           string
		providersHints []map[string][]TopologyHint
		expect         TopologyHint
	}{
		{
			name:           "test-1",
			providersHints: []map[string][]TopologyHint{},
			expect:         TopologyHint{},
		},
	}

	for _, testcase := range testCases {
		policy := NewPolicyNone([]int{0, 1})
		bestHit, _ := policy.Predicate(testcase.providersHints)
		if !reflect.DeepEqual(bestHit, testcase.expect) {
			t.Errorf("%s failed, expect %v, bestHit= %v\n", testcase.name, testcase.expect, bestHit)
		}
	}
}
