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
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

const fakeMemoryValue = "32G"

type setHardwareTypeToPodTestCase struct {
	name     string
	Task     *api.TaskInfo
	Node     plugin.NPUNode
	WantSMap map[string]string
}

func buildSetHardwareTypeToPodTestCases01() []setHardwareTypeToPodTestCase {
	return []setHardwareTypeToPodTestCase{
		{
			name: fmt.Sprintf("01 node label[%s] not exist", nPUChipMemoryKey),
			Task: &api.TaskInfo{
				Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			Node:     plugin.NPUNode{CommonNode: plugin.CommonNode{Label: map[string]string{}}},
			WantSMap: map[string]string{},
		},
		{
			name: fmt.Sprintf("02 node label[%s] not exist", util.AcceleratorType),
			Task: &api.TaskInfo{
				Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			Node: plugin.NPUNode{CommonNode: plugin.CommonNode{Label: map[string]string{
				nPUChipMemoryKey: fakeMemoryValue,
			}}},
			WantSMap: map[string]string{},
		},
		{
			name: fmt.Sprintf("03 node label[%s] not exist", serverUsageKey),
			Task: &api.TaskInfo{
				Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			Node: plugin.NPUNode{CommonNode: plugin.CommonNode{Label: map[string]string{
				nPUChipMemoryKey:     "",
				util.AcceleratorType: "",
			}}},
			WantSMap: map[string]string{},
		},
	}
}

func buildSetHardwareTypeToPodTestCases02() []setHardwareTypeToPodTestCase {
	return []setHardwareTypeToPodTestCase{
		{
			name: fmt.Sprint("04 node label exist, not 800I-A2"),
			Task: &api.TaskInfo{
				Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			Node: plugin.NPUNode{CommonNode: plugin.CommonNode{Label: map[string]string{
				nPUChipMemoryKey:     "",
				util.AcceleratorType: "",
				serverUsageKey:       "",
			}}},
			WantSMap: map[string]string{},
		},
		{
			name: fmt.Sprint("05 node label exist, is 800I-A2"),
			Task: &api.TaskInfo{
				Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}},
			},
			Node: plugin.NPUNode{CommonNode: plugin.CommonNode{Label: map[string]string{
				nPUChipMemoryKey:     fakeMemoryValue,
				util.AcceleratorType: util.Module910bx8AcceleratorType,
				serverUsageKey:       inferUsage,
			}}},
			WantSMap: map[string]string{
				podUsedHardwareTypeKey: fmt.Sprintf("%s-%s", hardwareType800IA2, fakeMemoryValue),
			},
		},
	}
}

func TestSetHardwareTypeToPod(t *testing.T) {
	npu := &NPUHandler{}
	testCases := buildSetHardwareTypeToPodTestCases01()
	testCases = append(testCases, buildSetHardwareTypeToPodTestCases02()...)
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			npu.setHardwareTypeToPod(tt.Task, tt.Node)
			if !reflect.DeepEqual(tt.Task.Pod.Annotations, tt.WantSMap) {
				t.Errorf("setHardwareTypeToPod() = %v, expected %v", tt.Task.Pod.Annotations, tt.WantSMap)
			}
		})
	}
}
