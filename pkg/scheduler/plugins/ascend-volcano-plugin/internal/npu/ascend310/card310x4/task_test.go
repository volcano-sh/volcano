/*
Copyright(C)2020-2025. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package card310x4 is using for HuaWei A300T Ascend pin affinity schedule.
*/
package card310x4

import (
	"errors"
	"reflect"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

const (
	mockIllegalTaskNPUNumber = -1
	mockTaskNPUNumberZero    = 0
	mockTaskNPUNumberOne     = 1
	mockTaskNPUNumberTwo     = 2
	mockTaskNPUNumberThree   = 3
	mockTaskNPUNumberFour    = 4
)

type getNPUAllocPriorityArrayTestCase struct {
	Name          string
	TaskNPUNumber int
	WantArr       []int
	WantErr       error
}

func buildGetNPUAllocPriorityArrayTestCases() []getNPUAllocPriorityArrayTestCase {
	return []getNPUAllocPriorityArrayTestCase{
		{
			Name:          "01-getNPUAllocPriorityArray return nil when taskNPUNumber is 0",
			TaskNPUNumber: mockTaskNPUNumberZero,
		},
		{
			Name:          "02-getNPUAllocPriorityArray return array when taskNPUNumber is 1",
			TaskNPUNumber: mockTaskNPUNumberOne,
			WantArr:       []int{1, util.NPUIndex3, util.NPUIndex2, maxCardNPUNum},
		},
		{
			Name:          "03-getNPUAllocPriorityArray return array when taskNPUNumber is 2",
			TaskNPUNumber: mockTaskNPUNumberTwo,
			WantArr:       []int{util.NPUIndex2, util.NPUIndex3, maxCardNPUNum},
		},
		{
			Name:          "04-getNPUAllocPriorityArray return array when taskNPUNumber is 3",
			TaskNPUNumber: mockTaskNPUNumberThree,
			WantArr:       []int{util.NPUIndex3, maxCardNPUNum},
		},
		{
			Name:          "05-getNPUAllocPriorityArray return array when taskNPUNumber is 4",
			TaskNPUNumber: mockTaskNPUNumberFour,
			WantArr:       []int{maxCardNPUNum},
		},
		{
			Name:          "06-getNPUAllocPriorityArray return error when taskNPUNumber is -1",
			TaskNPUNumber: mockIllegalTaskNPUNumber,
			WantErr:       errors.New("illegal request npu number: -1"),
		},
	}
}

func TestGetNPUAllocPriorityArray(t *testing.T) {
	testCases := buildGetNPUAllocPriorityArrayTestCases()
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			if res, err := getNPUAllocPriorityArray(tt.TaskNPUNumber); !reflect.DeepEqual(res,
				tt.WantArr) && !reflect.DeepEqual(err, tt.WantErr) {
				t.Errorf("getNPUAllocPriorityArray() res: %v, err: %v , wantArr: %v, wantErr: %v",
					res, err, tt.WantArr, tt.WantErr)
			}
		})
	}
}
