/*
Copyright(C)2020-2023. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package vnpu is using for HuaWei Ascend pin vnpu allocation.
*/
package vnpu

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

type virtualNPUTestFields struct {
	DynamicByConf bool
	VT            VTemplate
	DynamicVNPU   DynamicVNPU
}

type getTaskResourceArgs struct {
	task *api.TaskInfo
	node plugin.NPUNode
}

type getTaskResourceTest struct {
	name    string
	fields  virtualNPUTestFields
	args    getTaskResourceArgs
	want    util.VResource
	wantErr error
}

var testVT = map[string]util.VResource{
	plugin.VNPUTempVir01:        {Aicore: util.NPUIndex1, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
	plugin.VNPUTempVir02:        {Aicore: util.NPUIndex2, Aicpu: util.NPUIndex2, DVPP: plugin.AscendDVPPEnabledNull},
	plugin.VNPUTempVir02C1:      {Aicore: util.NPUIndex2, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
	plugin.VNPUTempVir04:        {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex4, DVPP: plugin.AscendDVPPEnabledNull},
	plugin.VNPUTempVir04C3:      {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledNull},
	plugin.VNPUTempVir04C3NDVPP: {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledOff},
	plugin.VNPUTempVir04C4cDVPP: {Aicore: util.NPUIndex4, Aicpu: util.NPUIndex4, DVPP: plugin.AscendDVPPEnabledOn},
	plugin.VNPUTempVir08:        {Aicore: util.NPUIndex8, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledNull},
	plugin.VNPUTempVir16:        {Aicore: util.NPUIndex16, Aicpu: util.NPUIndex7, DVPP: plugin.AscendDVPPEnabledNull},
}

var conCache = map[string]map[string]map[api.TaskID]struct{}{
	"node1": {plugin.VNPUTempVir01: {"taskID1": struct{}{}}}}

func buildGetTaskResourceTestCase01() getTaskResourceTest {
	tempTask := test.FakeNormalTestTask("pod1", "node1", "vcjob")
	return getTaskResourceTest{
		name:    "01-task no ai-core test.",
		fields:  virtualNPUTestFields{},
		args:    getTaskResourceArgs{task: tempTask},
		want:    util.VResource{},
		wantErr: fmt.Errorf("task %s AscendNPUCore read failed", tempTask.Name),
	}
}

func buildGetTaskResourceTestCase02() getTaskResourceTest {
	tempTask := test.FakeVNPUTestTask("pod1", "node1", "vcjob", util.NPUIndex8)
	tempNode := plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "testNode2", Idle: map[v1.ResourceName]float64{
		util.NPU910CardName: util.NPUIndex8 * util.NPUHexKilo}}}
	return getTaskResourceTest{
		name:    "02-node no ai-core test.",
		fields:  virtualNPUTestFields{},
		args:    getTaskResourceArgs{task: tempTask, node: tempNode},
		want:    util.VResource{},
		wantErr: fmt.Errorf("%s not inital for Aicore is 0", tempNode.Name),
	}
}

func buildGetTaskResourceTestCase03() getTaskResourceTest {
	tempTask := test.FakeVNPUTestTask("pod1", "node1", "vcjob", util.NPUIndex8)
	tempNode := plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "testNode2"},
		VNode: plugin.VNode{ChipKind: plugin.Ascend310P, ServerType: Ascend310PCard,
			TotalRes: util.VResource{Aicore: util.NPUIndex8}}}
	return getTaskResourceTest{
		name:    "03-task whole card test.",
		fields:  virtualNPUTestFields{},
		args:    getTaskResourceArgs{task: tempTask, node: tempNode},
		want:    util.VResource{Aicore: util.NPUIndex8, Aicpu: 0, DVPP: plugin.AscendDVPPEnabledNull},
		wantErr: nil,
	}
}

func buildGetTaskResourceTestCase04() getTaskResourceTest {
	tempTask := test.FakeVNPUTestTask("pod1", "node1", "vcjob", util.NPUIndex4)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPUDVPP, "haha")
	tempNode := plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "testNode2"},
		VNode: plugin.VNode{ChipKind: plugin.Ascend310P, ServerType: Ascend310PCard,
			TotalRes: util.VResource{Aicore: util.NPUIndex8}}}
	return getTaskResourceTest{
		name:    "04-illegal task dvpp test.",
		fields:  virtualNPUTestFields{},
		args:    getTaskResourceArgs{task: tempTask, node: tempNode},
		want:    util.VResource{},
		wantErr: errors.New("err dvpp value:haha"),
	}
}

func buildGetTaskResourceTestCase05() getTaskResourceTest {
	tempTask := test.FakeVNPUTestTask("pod1", "node1", "vcjob", 1)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPUDVPP, plugin.AscendDVPPEnabledNull)
	tempNode := plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "testNode2"},
		VNode: plugin.VNode{ChipKind: plugin.Ascend310P, ServerType: Ascend310PCard,
			TotalRes: util.VResource{Aicore: util.NPUIndex16}}}
	return getTaskResourceTest{
		name:    "05-task coreNum 01 test.",
		fields:  virtualNPUTestFields{VT: VTemplate{Data: testVT}},
		args:    getTaskResourceArgs{task: tempTask, node: tempNode},
		want:    util.VResource{Aicore: 1, Aicpu: 1, DVPP: "null"},
		wantErr: nil,
	}
}

func buildGetTaskResourceTestCase06() getTaskResourceTest {
	tempTask := test.FakeVNPUTestTask("pod1", "node1", "vcjob", util.NPUIndex2)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPUDVPP, plugin.AscendDVPPEnabledNull)
	tempNode := plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "testNode2"},
		VNode: plugin.VNode{ChipKind: plugin.Ascend310P, ServerType: Ascend310PCard,
			TotalRes: util.VResource{Aicore: util.NPUIndex16}}}
	return getTaskResourceTest{
		name:    "06-task coreNum 02 test.",
		fields:  virtualNPUTestFields{VT: VTemplate{Data: testVT}},
		args:    getTaskResourceArgs{task: tempTask, node: tempNode},
		want:    util.VResource{Aicore: util.NPUIndex2, Aicpu: 1, DVPP: "null"},
		wantErr: nil,
	}
}

func buildGetTaskResourceTestCase07() getTaskResourceTest {
	tempTask := test.FakeVNPUTestTask("pod1", "node1", "vcjob", util.NPUIndex3)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPUDVPP, plugin.AscendDVPPEnabledNull)
	tempNode := plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "testNode2"},
		VNode: plugin.VNode{ChipKind: plugin.Ascend310P, ServerType: Ascend310PCard,
			TotalRes: util.VResource{Aicore: util.NPUIndex16}}}
	return getTaskResourceTest{
		name:    "07-task coreNum 03 test.",
		fields:  virtualNPUTestFields{VT: VTemplate{Data: testVT}},
		args:    getTaskResourceArgs{task: tempTask, node: tempNode},
		want:    util.VResource{},
		wantErr: errors.New("task<pod1> get virTemplate is null"),
	}
}

func buildGetTaskResourceTestCase08() getTaskResourceTest {
	tempTask := test.FakeVNPUTestTask("pod1", "node1", "vcjob", util.NPUIndex4)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPUDVPP, plugin.AscendDVPPEnabledOn)
	tempNode := plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "testNode2"},
		VNode: plugin.VNode{ChipKind: plugin.Ascend310P, ServerType: Ascend310PCard,
			TotalRes: util.VResource{Aicore: util.NPUIndex16}}}
	return getTaskResourceTest{
		name:    "08-task coreNum 04,dvpp on test.",
		fields:  virtualNPUTestFields{VT: VTemplate{Data: testVT}},
		args:    getTaskResourceArgs{task: tempTask, node: tempNode},
		want:    util.VResource{Aicore: util.NPUIndex4, Aicpu: util.NPUIndex4, DVPP: plugin.AscendDVPPEnabledOn},
		wantErr: nil,
	}
}

func buildGetTaskResourceTestCase09() getTaskResourceTest {
	tempTask := test.FakeVNPUTestTask("pod1", "node1", "vcjob", util.NPUIndex4)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPUDVPP, plugin.AscendDVPPEnabledOff)
	tempNode := plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "testNode2"},
		VNode: plugin.VNode{ChipKind: plugin.Ascend310P, ServerType: Ascend310PCard,
			TotalRes: util.VResource{Aicore: util.NPUIndex16}}}
	return getTaskResourceTest{
		name:    "09-task coreNum 04,dvpp off test.",
		fields:  virtualNPUTestFields{VT: VTemplate{Data: testVT}},
		args:    getTaskResourceArgs{task: tempTask, node: tempNode},
		want:    util.VResource{Aicore: util.NPUIndex4, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledOff},
		wantErr: nil,
	}
}

func buildGetTaskResourceTestCase10() getTaskResourceTest {
	tempTask := test.FakeVNPUTestTask("pod1", "node1", "vcjob", util.NPUIndex4)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPUDVPP, plugin.AscendDVPPEnabledOn)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPULevel, plugin.AscendVNPULevelHigh)
	tempNode := plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "testNode2"},
		VNode: plugin.VNode{ChipKind: plugin.Ascend310P, ServerType: Ascend310PCard,
			TotalRes: util.VResource{Aicore: util.NPUIndex16}}}
	return getTaskResourceTest{
		name:    "10-task coreNum 04,level high, dvpp on test.",
		fields:  virtualNPUTestFields{VT: VTemplate{Data: testVT}},
		args:    getTaskResourceArgs{task: tempTask, node: tempNode},
		want:    util.VResource{Aicore: util.NPUIndex4, Aicpu: util.NPUIndex4, DVPP: plugin.AscendDVPPEnabledOn},
		wantErr: nil,
	}
}

func buildGetTaskResourceTestCase11() getTaskResourceTest {
	tempTask := test.FakeVNPUTestTask("pod1", "node1", "vcjob", util.NPUIndex4)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPUDVPP, plugin.AscendDVPPEnabledNull)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPULevel, plugin.AscendVNPULevelHigh)
	tempNode := plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "testNode2"},
		VNode: plugin.VNode{ChipKind: plugin.Ascend310P, ServerType: Ascend310PCard,
			TotalRes: util.VResource{Aicore: util.NPUIndex16}}}
	return getTaskResourceTest{
		name:    "11-task coreNum 04,level high, dvpp null test.",
		fields:  virtualNPUTestFields{VT: VTemplate{Data: testVT}},
		args:    getTaskResourceArgs{task: tempTask, node: tempNode},
		want:    util.VResource{Aicore: util.NPUIndex4, Aicpu: util.NPUIndex4, DVPP: plugin.AscendDVPPEnabledNull},
		wantErr: nil,
	}
}

func buildGetTaskResourceTestCase12() getTaskResourceTest {
	tempTask := test.FakeVNPUTestTask("pod1", "node1", "vcjob", util.NPUIndex4)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPUDVPP, plugin.AscendDVPPEnabledNull)
	test.AddTestTaskLabel(tempTask, plugin.AscendVNPULevel, plugin.AscendVNPULevelLow)
	tempNode := plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "testNode2"},
		VNode: plugin.VNode{ChipKind: plugin.Ascend310P, ServerType: Ascend310PCard,
			TotalRes: util.VResource{Aicore: util.NPUIndex16}}}
	return getTaskResourceTest{
		name:    "12-task coreNum 04,level low, dvpp null test.",
		fields:  virtualNPUTestFields{VT: VTemplate{Data: testVT}},
		args:    getTaskResourceArgs{task: tempTask, node: tempNode},
		want:    util.VResource{Aicore: util.NPUIndex4, Aicpu: util.NPUIndex3, DVPP: plugin.AscendDVPPEnabledNull},
		wantErr: nil,
	}
}

func buildGetTaskResourceTestCases() []getTaskResourceTest {
	testCases := []getTaskResourceTest{
		buildGetTaskResourceTestCase01(),
		buildGetTaskResourceTestCase02(),
		buildGetTaskResourceTestCase03(),
		buildGetTaskResourceTestCase04(),
		buildGetTaskResourceTestCase05(),
		buildGetTaskResourceTestCase06(),
		buildGetTaskResourceTestCase07(),
		buildGetTaskResourceTestCase08(),
		buildGetTaskResourceTestCase09(),
		buildGetTaskResourceTestCase10(),
		buildGetTaskResourceTestCase11(),
		buildGetTaskResourceTestCase12(),
	}
	return testCases
}

type nilTPTestCase struct {
	name               string
	tp                 *VirtualNPU
	wantNode           *plugin.NPUNode
	wantArr            []int
	wantValidateResult *api.ValidateResult
	wantErr            error
}

func buildNilTPTestCase(funcName string) nilTPTestCase {
	return nilTPTestCase{
		name: "00-" + funcName + " will return when tp is nil",
		wantValidateResult: &api.ValidateResult{
			Pass:    false,
			Reason:  "invalid argument",
			Message: "invalid argument"},
		wantErr: errors.New("invalid argument"),
	}
}

// TestGetTaskResource test GetTaskResource of TestVirtualNPU.
func TestGetTaskResource(t *testing.T) {
	testCase := buildNilTPTestCase("GetTaskResource")
	t.Run(testCase.name, func(t *testing.T) {
		if _, err := testCase.tp.GetTaskResource(nil, plugin.NPUNode{}); !reflect.DeepEqual(err, testCase.wantErr) {
			t.Errorf("GetTaskResource() = %v, want %v", err, testCase.wantErr)
		}
	})
	tests := buildGetTaskResourceTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tp = &VirtualNPU{
				StaticByConf: tt.fields.DynamicByConf,
				VT:           tt.fields.VT,
				DynamicVNPU:  tt.fields.DynamicVNPU,
			}
			got, err := tp.GetTaskResource(tt.args.task, tt.args.node)
			if !reflect.DeepEqual(err, tt.wantErr) {
				t.Errorf("GetTaskResource() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetTaskResource() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func newVirtualNPU() *VirtualNPU {
	fields := virtualNPUTestFields{}
	return &VirtualNPU{
		StaticByConf: fields.DynamicByConf,
		VT:           fields.VT,
		DynamicVNPU:  fields.DynamicVNPU,
	}
}

func TestGetVTaskDVPP(t *testing.T) {
	t.Run("01-getVTaskDVPP test", func(t *testing.T) {
		tp := newVirtualNPU()
		tempTask := test.FakeNormalTestTask("pod1", "node1", "vcjob")
		res, err := tp.getVTaskDVPP(tempTask)
		if !reflect.DeepEqual(err, nil) {
			t.Errorf("GetTaskResource() error = %v, wantErr %v", err, nil)
			return
		}
		if !reflect.DeepEqual(res, plugin.AscendDVPPEnabledNull) {
			t.Errorf("GetTaskResource() got = %v, want %v", res, plugin.AscendDVPPEnabledNull)
		}
	})

}

func TestGetVTaskLevel(t *testing.T) {
	t.Run("01-getVTaskLevel test", func(t *testing.T) {
		tp := newVirtualNPU()
		tempTask := test.FakeNormalTestTask("pod1", "node1", "vcjob")
		tempTask.Pod.Labels = make(map[string]string, util.MapInitNum)
		tempTask.Pod.Labels[plugin.AscendVNPULevel] = ""
		res := tp.getVTaskLevel(tempTask)
		if !reflect.DeepEqual(res, plugin.AscendVNPULevelLow) {
			t.Errorf("GetTaskResource() got = %v, want %v", res, plugin.AscendVNPULevelLow)
		}
	})
}

func TestGetAiCoreNumFromTask(t *testing.T) {
	t.Run("01-getAiCoreNumFromTask test", func(t *testing.T) {
		tempTask := test.FakeNormalTestTask("pod1", "node1", "vcjob")
		res, err := getAiCoreNumFromTask(tempTask)
		wantErr := fmt.Errorf("getAiCoreNumFromTask get resource requests failed")
		if !reflect.DeepEqual(err, wantErr) {
			t.Errorf("GetTaskResource() error = %v, wantErr %v", err, wantErr)
			return
		}
		if !reflect.DeepEqual(res, 0) {
			t.Errorf("GetTaskResource() got = %v, want %v", res, 0)
		}
	})
}

type getResTemplateTestCase struct {
	name     string
	coreNum  int
	dvpp     string
	chipType string
	want     string
}

func buildGetResTemplateTestCases() []getResTemplateTestCase {
	return []getResTemplateTestCase{
		{
			name: "01-getResTemplateFromTaskSettingAndChipType return plugin.VNPUTempVir03 " +
				"when coreNum is plugin.NPUIndex3 and chipType is plugin.ChipTypeB1",
			coreNum:  util.NPUIndex3,
			chipType: plugin.ChipTypeB1,
			want:     plugin.VNPUTempVir03,
		},
		{
			name: "02-getResTemplateFromTaskSettingAndChipType return plugin.VNPUTempVir06 " +
				"when coreNum is plugin.NPUIndex6 and chipType is plugin.ChipTypeB2C",
			coreNum:  util.NPUIndex6,
			chipType: plugin.ChipTypeB2C,
			want:     plugin.VNPUTempVir06,
		},
		{
			name: "03-getResTemplateFromTaskSettingAndChipType return plugin.VNPUTempVir05 " +
				"when coreNum is plugin.NPUIndex5 and chipType is plugin.ChipTypeB3",
			coreNum:  util.NPUIndex5,
			chipType: plugin.ChipTypeB3,
			want:     plugin.VNPUTempVir05,
		},
		{
			name: "04-getResTemplateFromTaskSettingAndChipType return plugin.VNPUTempVir05 " +
				"when coreNum is plugin.NPUIndex5 and chipType is plugin.ChipTypeB4",
			coreNum:  util.NPUIndex5,
			chipType: plugin.ChipTypeB4,
			want:     plugin.VNPUB4TempVir05,
		},
	}
}

func TestGetResTemplateFromTaskSettingAndChipType(t *testing.T) {
	testCases := buildGetResTemplateTestCases()
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			res := getResTemplateFromTaskSettingAndChipType(tt.coreNum, tt.dvpp, tt.chipType)
			if !reflect.DeepEqual(res, tt.want) {
				t.Errorf("getResTemplateFromTaskSettingAndChipType() = %v, want %v", res, tt.want)
			}
		})
	}
}

type getResTemplateForB1AndB2CTestCase struct {
	name    string
	coreNum int
	want    string
}

func buildGetResTemplateForB1AndB2CTestCases() []getResTemplateForB1AndB2CTestCase {
	return []getResTemplateForB1AndB2CTestCase{
		{
			name: "01-getResTemplateFromTaskSettingForB2 return plugin.VNPUTempVir03 " +
				"when coreNum is util.NPUIndex3",
			coreNum: util.NPUIndex3,
			want:    plugin.VNPUTempVir03,
		},
		{
			name: "02-getResTemplateFromTaskSettingForB2 return plugin.VNPUTempVir06 " +
				"when coreNum is util.NPUIndex6",
			coreNum: util.NPUIndex6,
			want:    plugin.VNPUTempVir06,
		},
		{
			name: "03-getResTemplateFromTaskSettingForB2 return plugin.VNPUTempVir12 " +
				"when coreNum is util.NPUIndex12",
			coreNum: util.NPUIndex12,
			want:    plugin.VNPUTempVir12,
		},
		{
			name:    "04-getResTemplateFromTaskSettingForB2 return empty when coreNum is util.ErrorInt",
			coreNum: util.ErrorInt,
			want:    "",
		},
	}
}

func TestGetResTemplateFromTaskSettingForB2(t *testing.T) {
	testCases := buildGetResTemplateForB1AndB2CTestCases()
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			res := getResTemplateFromTaskSettingForB1AndB2C(tt.coreNum)
			if !reflect.DeepEqual(res, tt.want) {
				t.Errorf("getResTemplateFromTaskSettingForB1AndB2C() = %v, want %v", res, tt.want)
			}
		})
	}
}

type getResTemplateForB3TestCase struct {
	name    string
	coreNum int
	want    string
}

func buildGetResTemplateForB3TestCases() []getResTemplateForB3TestCase {
	return []getResTemplateForB3TestCase{
		{
			name: "01-getResTemplateFromTaskSettingForB3 return plugin.VNPUTempVir05 " +
				"when coreNum is util.NPUIndex5",
			coreNum: util.NPUIndex5,
			want:    plugin.VNPUTempVir05,
		},
		{
			name: "02-getResTemplateFromTaskSettingForB3 return plugin.VNPUTempVir10 " +
				"when coreNum is util.NPUIndex10 and dvpp is plugin.AscendDVPPEnabledOn",
			coreNum: util.NPUIndex10,
			want:    plugin.VNPUTempVir10,
		},
		{
			name: "03-getResTemplateFromTaskSettingForB3 return empty " +
				"when coreNum is util.ErrorInt",
			coreNum: util.ErrorInt,
			want:    "",
		},
	}
}

func TestGetResTemplateFromTaskSettingForB3(t *testing.T) {
	testCases := buildGetResTemplateForB3TestCases()
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			res := getResTemplateFromTaskSettingForB3(tt.coreNum)
			if !reflect.DeepEqual(res, tt.want) {
				t.Errorf("getResTemplateFromTaskSettingForB3() got = %v, want %v", res, tt.want)
			}
		})
	}
}

type getResTemplateForB4TestCase struct {
	name    string
	coreNum int
	dvpp    string
	want    string
}

func buildGetResTemplateForB4TestCases() []getResTemplateForB4TestCase {
	return []getResTemplateForB4TestCase{
		{
			name: "01-getResTemplateFromTaskSettingForB4 return plugin.VNPUB4TempVir05 " +
				"when coreNum is util.NPUIndex5",
			coreNum: util.NPUIndex5,
			want:    plugin.VNPUB4TempVir05,
		},
		{
			name: "02-getResTemplateFromTaskSettingForB4 return plugin.VNPUB4TempVir10C4M " +
				"when coreNum is util.NPUIndex10 and dvpp is plugin.AscendDVPPEnabledOn",
			coreNum: util.NPUIndex10,
			dvpp:    plugin.AscendDVPPEnabledOn,
			want:    plugin.VNPUB4TempVir10C4M,
		},
		{
			name: "03-getResTemplateFromTaskSettingForB4 return empty " +
				"when coreNum is util.ErrorInt",
			coreNum: util.ErrorInt,
			want:    "",
		},
	}
}

func TestGetResTemplateFromTaskSettingForB4(t *testing.T) {
	testCases := buildGetResTemplateForB4TestCases()
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			res := getResTemplateFromTaskSettingForB4(tt.coreNum, tt.dvpp)
			if !reflect.DeepEqual(res, tt.want) {
				t.Errorf("getResTemplateFromTaskSettingForB4() got = %v, want %v", res, tt.want)
			}
		})
	}
}

type getB4TemplateTestCase struct {
	name string
	dvpp string
	want string
}

func buildGetB4TemplateTestCases() []getB4TemplateTestCase {
	return []getB4TemplateTestCase{
		{
			name: "01-getB4TempFromTaskLabel return plugin.VNPUB4TempVir10C4M " +
				"when dvpp is plugin.AscendDVPPEnabledOn",
			dvpp: plugin.AscendDVPPEnabledOn,
			want: plugin.VNPUB4TempVir10C4M,
		},
		{
			name: "02-getB4TempFromTaskLabel return plugin.VNPUB4TempVir10C3NM " +
				"when dvpp is plugin.AscendDVPPEnabledOff",
			dvpp: plugin.AscendDVPPEnabledOff,
			want: plugin.VNPUB4TempVir10C3NM,
		},
		{
			name: "02-getB4TempFromTaskLabel return plugin.VNPUB4TempVir10 " +
				"when dvpp is AscendDVPPEnabledNull",
			dvpp: plugin.AscendDVPPEnabledNull,
			want: plugin.VNPUB4TempVir10,
		},
	}
}

func TestGetB4TempFromTaskLabel(t *testing.T) {
	testCases := buildGetB4TemplateTestCases()
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			if res := getB4TempFromTaskLabel(tt.dvpp); !reflect.DeepEqual(res, tt.want) {
				t.Errorf("getB4TempFromTaskLabel() = %v, want %v", res, tt.want)
			}
		})
	}
}
