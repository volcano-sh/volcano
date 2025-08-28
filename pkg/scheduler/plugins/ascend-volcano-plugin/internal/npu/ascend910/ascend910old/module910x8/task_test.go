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
Package module910x8 is using for HuaWei Ascend pin affinity schedule.
*/
package module910x8

import (
	"reflect"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

func TestJudgeNodeAndTaskNPU(t *testing.T) {
	tests := []struct {
		name    string
		taskNPU int
		nodeTop []int
		wantErr bool
	}{
		{
			name:    "01 will return err when task require num is 5",
			taskNPU: util.NPUIndex5,
			nodeTop: []int{},
			wantErr: true,
		},
		{
			name:    "02 will return err when task require num is 8 and node topo is 8",
			taskNPU: util.NPUIndex8,
			nodeTop: []int{util.NPUIndex0, util.NPUIndex1, util.NPUIndex2, util.NPUIndex3,
				util.NPUIndex4, util.NPUIndex5, util.NPUIndex6, util.NPUIndex7},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := judgeNodeAndTaskNPU(tt.taskNPU, tt.nodeTop); (err != nil) != tt.wantErr {
				t.Errorf("judgeNodeAndTaskNPU() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetNPUAllocPriorityArray(t *testing.T) {
	tests := []struct {
		name          string
		taskNPUNumber int
		want          []int
	}{
		{
			name:          "01 will return nil when task npu num is 5",
			taskNPUNumber: util.NPUIndex5,
			want:          nil,
		},
		{
			name:          "02 will return {0,1,2,3,4} when task require num is 0",
			taskNPUNumber: util.NPUIndex0,
			want:          []int{0, util.NPUIndex1, npuIndex2, npuIndex3, npuNumPerHccs},
		},
		{
			name:          "03 will return {2,4,3} when task require num is 2",
			taskNPUNumber: util.NPUIndex2,
			want:          []int{npuIndex2, npuNumPerHccs, npuIndex3},
		},
		{
			name:          "04 will return {4} when task require num is 4",
			taskNPUNumber: util.NPUIndex4,
			want:          []int{npuNumPerHccs},
		},
		{
			name:          "05 will return {8} when task require num is 8",
			taskNPUNumber: util.NPUIndex8,
			want:          []int{nodeNPUNumber},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := getNPUAllocPriorityArray(tt.taskNPUNumber)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getNPUAllocPriorityArray() got = %v, want %v", got, tt.want)
			}
		})
	}
}
