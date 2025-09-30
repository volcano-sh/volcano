/*
Copyright(C)2024. Huawei Technologies Co.,Ltd. All rights reserved.

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
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

const (
	Ascend910Card = "Ascend910-8"
	num4          = 4
)

type vNPUTaskFields struct {
	StaticByConf bool
	VT           VTemplate
	DynamicVNPU  DynamicVNPU
}

type vNPUTaskArgs struct {
	task       *api.TaskInfo
	node       plugin.NPUNode
	nodes      []*api.NodeInfo
	scoreMap   map[string]float64
	taskResReq util.VResource
	vt         VTemplate
}

type vNPUTestCase struct {
	name    string
	fields  vNPUTaskFields
	args    vNPUTaskArgs
	wantRes bool
	wantErr bool
}

func mockCommonNode() plugin.CommonNode {
	return plugin.CommonNode{
		Name:           "node1",
		Capability:     map[v1.ResourceName]float64{util.NPU910CardName: num4 * util.NPUHexKilo},
		Allocate:       map[v1.ResourceName]float64{util.NPU910CardName: num4 * util.NPUHexKilo},
		Idle:           map[v1.ResourceName]float64{util.NPU910CardName: num4 * util.NPUHexKilo},
		BaseDeviceInfo: "",
		Annotation:     map[string]string{util.NPU910CardName: "Ascend910-0,Ascend910-1"},
		Label:          map[string]string{util.Accelerator: "huawei-Ascend910"},
		Address:        "",
		SuperPodID:     0,
	}
}

func mockVNode() plugin.VNode {
	return plugin.VNode{
		Chips: map[int]*plugin.VChip{
			0: {TotalRes: util.VResource{Aicore: util.NPUIndex8},
				FreeRes: util.VResource{Aicore: util.NPUIndex8, Aicpu: util.NPUIndex8},
				Name:    Ascend910Card},
		},
		ChipKind:         plugin.Ascend910,
		UnhealthyChipIds: map[int]struct{}{},
		ServerType:       Ascend910Card,
		TotalChipNum:     util.NPUIndex8,
		AiCorePerChip:    util.NPUIndex8,
		FreeChipNum:      util.NPUIndex8,
		TotalRes:         util.VResource{Aicore: util.CoreNum25},
		ValidVNode:       true,
		ChipType:         plugin.ChipTypeB1,
	}
}

func mockNode() plugin.NPUNode {
	return plugin.NPUNode{
		CommonNode: mockCommonNode(),
		VNode:      mockVNode(),
	}
}

func mockVNPUTaskFields() vNPUTaskFields {
	return vNPUTaskFields{
		StaticByConf: false,
		DynamicVNPU: DynamicVNPU{
			DowngradeCache: make(map[string][]string),
			ConCache:       make(map[string]map[string]map[api.TaskID]struct{})},
	}
}

func mockVirtualNPU(fields vNPUTaskFields) *VirtualNPU {
	return &VirtualNPU{
		StaticByConf: fields.StaticByConf,
		VT:           fields.VT,
		DynamicVNPU:  fields.DynamicVNPU,
	}
}

func mockDynamicVNPU(fields dynamicVNPUFields) *DynamicVNPU {
	return &DynamicVNPU{
		DowngradeCache: fields.DowngradeCache,
		ConCache:       fields.ConCache,
	}
}

func nilTaskTestCase() vNPUTestCase {
	return vNPUTestCase{
		name:    "01 will return error when tp is nil or task is nil",
		fields:  vNPUTaskFields{},
		args:    vNPUTaskArgs{},
		wantErr: true,
		wantRes: true,
	}
}

func buildCheckNodeNPUByDyTaskTestCase05() vNPUTestCase {
	node := mockNode()
	node.Chips[0] = &plugin.VChip{FreeRes: util.VResource{Aicore: 2, Aicpu: 2},
		TotalRes: util.VResource{Aicore: 2, Aicpu: 2}}
	return vNPUTestCase{
		name:   "05 will return nil when  node res is  meet job require",
		fields: mockVNPUTaskFields(),
		args: vNPUTaskArgs{task: &api.TaskInfo{}, node: node,
			taskResReq: util.VResource{Aicore: 2, Aicpu: 2}},
		wantErr: false,
	}
}

func buildCheckNodeNPUByDyTaskTestCase06() vNPUTestCase {
	node := mockNode()
	node.ChipKind = plugin.Ascend310P
	return vNPUTestCase{
		name:   "06 will return error when node.ChipKind is plugin.Ascend310P",
		fields: mockVNPUTaskFields(),
		args: vNPUTaskArgs{task: &api.TaskInfo{}, node: node,
			taskResReq: util.VResource{Aicore: util.NPUIndex16, Aicpu: util.NPUIndex16}},
		wantErr: true,
	}
}

func buildCheckNodeNPUByDyTaskTestCase07() vNPUTestCase {
	tp := mockVNPUTaskFields()
	tp.DynamicVNPU.ConCache = map[string]map[string]map[api.TaskID]struct{}{
		"node1": {plugin.VNPUTempVir02: {"taskID1": struct{}{}}}}
	node := mockNode()
	return vNPUTestCase{
		name:   "07 will return error when get template failed",
		fields: tp,
		args: vNPUTaskArgs{task: &api.TaskInfo{}, node: node,
			taskResReq: util.VResource{Aicore: 0, Aicpu: 0}},
		wantErr: true,
	}
}

func buildCheckNodeNPUByDyTaskTestCase() []vNPUTestCase {
	return []vNPUTestCase{
		nilTaskTestCase(),
		{
			name:   "02 will return error when node is not vNode",
			fields: vNPUTaskFields{StaticByConf: false},
			args: vNPUTaskArgs{
				task: &api.TaskInfo{},
				node: plugin.NPUNode{VNode: plugin.VNode{ValidVNode: false}}},
			wantErr: true,
		},
		{
			name:   "03 will return error when node res is not meet job require",
			fields: vNPUTaskFields{StaticByConf: false},
			args: vNPUTaskArgs{
				task:       &api.TaskInfo{},
				node:       mockNode(),
				taskResReq: util.VResource{Aicore: util.NPUIndex16, Aicpu: util.NPUIndex16},
			},
			wantErr: true,
		},
		{
			name:   "04 will return error when task can be down grade and node res is  meet job require",
			fields: mockVNPUTaskFields(),
			args: vNPUTaskArgs{task: &api.TaskInfo{}, node: mockNode(),
				taskResReq: util.VResource{Aicore: 2, Aicpu: 2}},
			wantErr: false,
		},
		buildCheckNodeNPUByDyTaskTestCase05(),
		buildCheckNodeNPUByDyTaskTestCase06(),
		buildCheckNodeNPUByDyTaskTestCase07(),
	}
}

func TestVirtualNPUCheckNodeNPUByDyTask(t *testing.T) {
	tests := buildCheckNodeNPUByDyTaskTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := mockVirtualNPU(tt.fields)
			err := tp.CheckNodeNPUByDyTask(tt.args.task, tt.args.node, tt.args.taskResReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckNodeNPUByDyTask() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type dynamicVNPUFields struct {
	DowngradeCache map[string][]string
	ConCache       map[string]map[string]map[api.TaskID]struct{}
}

type scoreBestNPUNodesTest struct {
	name    string
	fields  dynamicVNPUFields
	args    vNPUTaskArgs
	wantErr bool
}

func buildScoreBestNPUNodesTestCase() []scoreBestNPUNodesTest {
	return []scoreBestNPUNodesTest{
		{
			name:    "01 will return err when tp is nil",
			fields:  dynamicVNPUFields{},
			args:    vNPUTaskArgs{},
			wantErr: true,
		},
		{
			name:    "02 will return err when scoreMap is nil",
			fields:  dynamicVNPUFields{DowngradeCache: map[string][]string{}},
			args:    vNPUTaskArgs{task: &api.TaskInfo{}, nodes: []*api.NodeInfo{{}}},
			wantErr: true,
		},
		{
			name:   "03 will return nil when node is not in down grade map ",
			fields: dynamicVNPUFields{DowngradeCache: map[string][]string{}},
			args: vNPUTaskArgs{task: &api.TaskInfo{}, nodes: []*api.NodeInfo{{}},
				scoreMap: map[string]float64{"node1": 200}},
			wantErr: false,
		},
		{
			name:   "04 will return nil when node is in down grade map ",
			fields: dynamicVNPUFields{DowngradeCache: map[string][]string{"task01": {"node1"}}},
			args: vNPUTaskArgs{task: &api.TaskInfo{Name: "task01"}, nodes: []*api.NodeInfo{{Name: "node1"}},
				scoreMap: map[string]float64{"node1": 200}},
			wantErr: false,
		},
		{
			name:   "05 will return nil when node is not in down grade map ",
			fields: dynamicVNPUFields{DowngradeCache: map[string][]string{"task01": {"node1"}}},
			args: vNPUTaskArgs{task: &api.TaskInfo{Name: "task01"}, nodes: []*api.NodeInfo{{Name: ""}},
				scoreMap: map[string]float64{"node1": 200}},
			wantErr: false,
		},
	}
}

func TestDynamicVNPUScoreBestNPUNodes(t *testing.T) {
	tests := buildScoreBestNPUNodesTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := mockDynamicVNPU(tt.fields)
			err := tp.ScoreBestNPUNodes(tt.args.task, tt.args.nodes, tt.args.scoreMap)
			if (err != nil) != tt.wantErr {
				t.Errorf("ScoreBestNPUNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func buildIsNodeHasDifferentUnFinishedTaskTestCases() []vNPUTestCase {
	fields := vNPUTaskFields{
		VT:          VTemplate{Data: testVT},
		DynamicVNPU: DynamicVNPU{ConCache: conCache}}
	return []vNPUTestCase{
		nilTaskTestCase(),
		{
			name:    "02 will return error when get template failed",
			fields:  fields,
			args:    vNPUTaskArgs{task: &api.TaskInfo{}, node: mockNode()},
			wantErr: true,
		},
		{
			name:   "03 will return error when nodeTempMap not find template",
			fields: fields,
			args: vNPUTaskArgs{task: &api.TaskInfo{}, node: mockNode(),
				taskResReq: util.VResource{
					Aicore: util.NPUIndex2, Aicpu: util.NPUIndex2, DVPP: plugin.AscendDVPPEnabledNull,
				},
			},
			wantErr: true,
		},
		{
			name:   "04 will return nil when cache no template",
			fields: fields,
			args: vNPUTaskArgs{task: &api.TaskInfo{}, node: mockNode(),
				taskResReq: util.VResource{Aicore: util.NPUIndex1, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
			},
			wantErr: false,
		},
	}
}

func TestIsNodeHasDifferentUnFinishedTask(t *testing.T) {
	tests := buildIsNodeHasDifferentUnFinishedTaskTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := mockVirtualNPU(tt.fields)
			err := tp.IsNodeHasDifferentUnFinishedTask(tt.args.task, tt.args.node, tt.args.taskResReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckNodeNPUByDyTask() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func buildGetTemplateByResReqTestCases() []vNPUTestCase {
	return []vNPUTestCase{
		{
			name:    "01 will return error when tp is nil",
			args:    vNPUTaskArgs{},
			wantErr: true,
		},
		{
			name:   "02 will return nil when tp is not nil and task is not nil",
			fields: vNPUTaskFields{},
			args: vNPUTaskArgs{
				taskResReq: util.VResource{
					Aicore: util.NPUIndex1, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledOff},
				vt: VTemplate{Data: testVT},
			},
			wantErr: true,
		},
		{
			name:   "03 will return nil when tp is not nil and task is not nil",
			fields: vNPUTaskFields{},
			args: vNPUTaskArgs{
				taskResReq: util.VResource{
					Aicore: util.NPUIndex2, Aicpu: util.NPUIndex2, DVPP: plugin.AscendDVPPEnabledNull},
				vt: VTemplate{Data: testVT},
			},
			wantErr: false,
		},
	}
}

func TestGetTemplateByResReq(t *testing.T) {
	tests := buildGetTemplateByResReqTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := mockVirtualNPU(tt.fields)
			_, err := tp.GetTemplateByResReq(tt.args.taskResReq, tt.args.vt)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTemplateByResReq() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func mockTask() *api.TaskInfo {
	return &api.TaskInfo{
		UID:        "taskID1",
		Job:        "jobID1",
		Name:       "task1",
		Namespace:  "ns1",
		Resreq:     &api.Resource{},
		InitResreq: &api.Resource{},
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: make(map[string]string),
			}},
	}
}

func buildReleaseTaskInConCacheTestCase01() []vNPUTestCase {
	mockTask1 := mockTask()
	mockTask1.Pod.Annotations = map[string]string{util.AscendNPUCore: "0-vir00"}
	mockTask2 := mockTask()
	mockTask2.Pod.Annotations = map[string]string{util.AscendNPUCore: "1-vir01"}
	mockTask2.UID = "taskID2"
	return []vNPUTestCase{
		{
			name:    "01 will return error when GetVTaskUseTemplate failed",
			fields:  vNPUTaskFields{},
			args:    vNPUTaskArgs{task: mockTask(), node: mockNode()},
			wantErr: true,
		},
		{
			name:    "02 will return error when ConCache not found node name",
			fields:  vNPUTaskFields{DynamicVNPU: DynamicVNPU{ConCache: conCache}},
			args:    vNPUTaskArgs{task: mockTask1, node: plugin.NPUNode{}},
			wantErr: true,
		},
		{
			name:    "03 will return error when ConCache not found template",
			fields:  vNPUTaskFields{DynamicVNPU: DynamicVNPU{ConCache: conCache}},
			args:    vNPUTaskArgs{task: mockTask1, node: mockNode()},
			wantErr: true,
		},
		{
			name:    "04 will return error when ConCache not found task.UID",
			fields:  vNPUTaskFields{DynamicVNPU: DynamicVNPU{ConCache: conCache}},
			args:    vNPUTaskArgs{task: mockTask2, node: mockNode()},
			wantErr: true,
		},
	}
}

func buildReleaseTaskInConCacheTestCase02() []vNPUTestCase {
	mockTask1 := mockTask()
	mockTask1.Pod.Annotations = map[string]string{util.AscendNPUCore: "1-vir01"}
	mockConCache1 := map[string]map[string]map[api.TaskID]struct{}{
		"node1": {plugin.VNPUTempVir01: {"taskID1": struct{}{}, "taskID2": struct{}{}}},
	}
	mockConCache2 := map[string]map[string]map[api.TaskID]struct{}{
		"node1": {
			plugin.VNPUTempVir01: {"taskID1": struct{}{}},
			plugin.VNPUTempVir02: {"taskID1": struct{}{}},
		},
	}
	return []vNPUTestCase{
		{
			name:    "05 will return nil when ConCache found task.UID",
			fields:  vNPUTaskFields{DynamicVNPU: DynamicVNPU{ConCache: conCache}},
			args:    vNPUTaskArgs{task: mockTask1, node: mockNode()},
			wantErr: false,
		},
		{
			name:    "06 will return nil when len(tIDs) not equal 0 after delete by uid",
			fields:  vNPUTaskFields{DynamicVNPU: DynamicVNPU{ConCache: mockConCache1}},
			args:    vNPUTaskArgs{task: mockTask1, node: mockNode()},
			wantErr: false,
		},
		{
			name:    "07 will return nil when len(temp) not equal 0 after delete by template",
			fields:  vNPUTaskFields{DynamicVNPU: DynamicVNPU{ConCache: mockConCache2}},
			args:    vNPUTaskArgs{task: mockTask1, node: mockNode()},
			wantErr: false,
		},
	}
}

func TestReleaseTaskInConCache(t *testing.T) {
	tests := buildReleaseTaskInConCacheTestCase01()
	tests = append(tests, buildReleaseTaskInConCacheTestCase02()...)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := mockVirtualNPU(tt.fields)
			err := tp.releaseTaskInConCache(tt.args.task, tt.args.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTemplateByResReq() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func buildReleaseAnnotationTestCase() []vNPUTestCase {
	mockTask1 := mockTask()
	mockTask1.Pod.Annotations = map[string]string{util.AscendNPUCore: "1-vir01"}
	return []vNPUTestCase{
		nilTaskTestCase(),
		{
			name:    "02 will return not nil when tp and task both not nil",
			fields:  vNPUTaskFields{DynamicVNPU: DynamicVNPU{ConCache: conCache}},
			args:    vNPUTaskArgs{task: mockTask1, node: mockNode()},
			wantRes: true,
		},
	}
}

func TestReleaseAnnotation(t *testing.T) {
	tests := buildReleaseAnnotationTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := mockVirtualNPU(tt.fields)
			res := tp.ReleaseAnnotation(tt.args.task, tt.args.node)
			if (res != nil) != tt.wantRes {
				t.Errorf("ReleaseAnnotation() res = %v, wantRes %v", res, tt.wantRes)
			}
		})
	}
}

func buildAddTaskInConCacheTestCase() []vNPUTestCase {
	mockTask1 := mockTask()
	mockTask1.Pod.Annotations = map[string]string{util.AscendNPUCore: "1-vir01"}
	return []vNPUTestCase{
		{
			name:   "01 will return nil when node is nil",
			fields: vNPUTaskFields{DynamicVNPU: DynamicVNPU{ConCache: conCache}},
			args: vNPUTaskArgs{task: mockTask1, vt: VTemplate{Data: testVT},
				taskResReq: util.VResource{
					Aicore: util.NPUIndex1, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull,
				},
			},
			wantErr: false,
		},
		{
			name:   "02 will return nil when node is not nil",
			fields: vNPUTaskFields{DynamicVNPU: DynamicVNPU{ConCache: conCache}},
			args: vNPUTaskArgs{task: mockTask1, node: mockNode(), vt: VTemplate{Data: testVT},
				taskResReq: util.VResource{
					Aicore: util.NPUIndex1, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull,
				},
			},
			wantErr: false,
		},
	}
}

func TestAddTaskInConCache(t *testing.T) {
	tests := buildAddTaskInConCacheTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := mockVirtualNPU(tt.fields)
			res := tp.addTaskInConCache(tt.args.task, tt.args.node, tt.args.taskResReq, tt.args.vt)
			if (res != nil) != tt.wantErr {
				t.Errorf("addTaskInConCache() res = %v, wantRes %v", res, tt.wantErr)
			}
		})
	}
}

func buildUseAnnotationTestCase() []vNPUTestCase {
	mockTask1 := mockTask()
	mockTask1.Pod.Annotations = map[string]string{util.AscendNPUCore: "1-vir01"}
	return []vNPUTestCase{
		nilTaskTestCase(),
		{
			name:   "02 will return not nil when tp and task are not nil",
			fields: vNPUTaskFields{DynamicVNPU: DynamicVNPU{ConCache: conCache}},
			args: vNPUTaskArgs{task: mockTask1, node: mockNode(), vt: VTemplate{Data: testVT},
				taskResReq: util.VResource{
					Aicore: util.NPUIndex1, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
			},
			wantRes: true,
		},
		{
			name:   "03 will return not nil when tp and task are whole card",
			fields: vNPUTaskFields{DynamicVNPU: DynamicVNPU{ConCache: conCache}},
			args: vNPUTaskArgs{task: mockTask1, node: mockNode(), vt: VTemplate{Data: testVT},
				taskResReq: util.VResource{
					Aicore: util.NPUIndex8, Aicpu: util.NPUIndex1, DVPP: plugin.AscendDVPPEnabledNull},
			},
			wantRes: true,
		},
	}
}

func TestUseAnnotation(t *testing.T) {
	tests := buildUseAnnotationTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := tt.fields.DynamicVNPU.UseAnnotation(tt.args.task, tt.args.node, tt.args.taskResReq, tt.args.vt)
			if (res != nil) != tt.wantRes {
				t.Errorf("UseAnnotation() res = %v, wantRes %v", res, tt.wantRes)
			}
		})
	}
}
