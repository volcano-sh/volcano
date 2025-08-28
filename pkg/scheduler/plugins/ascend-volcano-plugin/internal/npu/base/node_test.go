/*
Copyright(C)2025. Huawei Technologies Co.,Ltd. All rights reserved.

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

/*
Package base is using for HuaWei Ascend pin affinity schedule.
*/
package base

import (
	"reflect"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

func TestGetCardNumGroupsFromTop(t *testing.T) {
	tests := []struct {
		name            string
		tp              *NPUHandler
		nodeNPUTopology []int
		expected        [][]int
	}{
		{
			name:            "01 Nil NPUHandler test",
			tp:              nil,
			nodeNPUTopology: []int{util.NPUIndex1, util.NPUIndex2, util.NPUIndex3},
			expected:        nil,
		},
		{
			name:            "02 MaxCardNPUNum is zero",
			tp:              &NPUHandler{MaxCardNPUNum: 0},
			nodeNPUTopology: []int{util.NPUIndex1, util.NPUIndex2, util.NPUIndex3},
			expected:        nil,
		},
		{
			name:            "03 Single group test",
			tp:              &NPUHandler{MaxCardNPUNum: 4},
			nodeNPUTopology: []int{util.NPUIndex1, util.NPUIndex2, util.NPUIndex3},
			expected:        [][]int{{util.NPUIndex1, util.NPUIndex2, util.NPUIndex3}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.tp.GetCardNumGroupsFromTop(tt.nodeNPUTopology)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("GetCardNumGroupsFromTop() = %v, expected %v", result, tt.expected)
			}
		})
	}
}
