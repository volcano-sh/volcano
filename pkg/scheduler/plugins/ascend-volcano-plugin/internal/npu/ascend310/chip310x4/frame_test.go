/*
Copyright(C)2020-2022. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package chip310x4 is using for HuaWei Ascend pin affinity schedule.
*/
package chip310x4

import (
	"errors"
	"reflect"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
	itest "volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/test"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

const (
	MockTaskNumOne     = 1
	MockResourceNumOne = 1
	MockNeedOne        = "1"
	MockNeedTwo        = "2"
	MockNeedFive       = "5"
	MockNeedSixtyFive  = "65"
	MockJobName        = "testJob"
	MockPodZeroName    = "pod0"
	MockPodOneName     = "pod1"
	MockNodeOneName    = "node1"
)

type nilTPTestCase struct {
	name               string
	tp                 *chip310x4
	wantNode           *plugin.NPUNode
	wantArr            []int
	wantValidateResult *api.ValidateResult
	wantErr            error
}

type validNPUJobTestCase struct {
	name    string
	attr    util.SchedulerJobAttr
	wantErr *api.ValidateResult
}

func NewChip310x4(schedulerName string) *chip310x4 {
	chip := &chip310x4{}
	chip.SetPluginName(schedulerName)
	chip.SetAnnoName(util.NPU310CardName)
	chip.SetAnnoPreVal(util.NPU310CardNamePre)
	chip.SetMaxNodeNPUNum(maxNodeNPUNum)
	chip.SetMaxCardNPUNum(maxCardNPUNum)
	return chip
}

func initNPU(attr util.SchedulerJobAttr, env plugin.ScheduleEnv) base.AscendHandler {
	npu := New(SchedulerName)
	npu.SetSchedulerAttr(attr)
	npu.SetSchedulerEnv(env)
	return npu
}

func initAttr(jobName string, taskNum int, need string) util.SchedulerJobAttr {
	job := test.FakeNormalTestJob(jobName, taskNum)
	test.SetFakeJobResRequest(job, util.NPU310CardName, need)
	return itest.FakeSchedulerJobAttrByJob(job)
}

func initNode(nodeName string, annoKey string, annoVal string) plugin.NPUNode {
	return plugin.NPUNode{CommonNode: plugin.CommonNode{
		Name:       nodeName,
		Annotation: map[string]string{annoKey: annoVal},
	}}
}

func buildNilTPTestCase(funcName string) nilTPTestCase {
	return nilTPTestCase{
		name: "00-" + funcName + " will return when tp is nil",
		tp:   nil,
		wantValidateResult: &api.ValidateResult{
			Pass:    false,
			Reason:  "invalid argument",
			Message: "invalid argument"},
	}
}

// useAnnotationTestCase useAnnotation test case
type useAnnotationTestCase struct {
	Task     *api.TaskInfo
	WantNode *plugin.NPUNode
	Name     string
	Node     plugin.NPUNode
	PodAnno  string
	Attr     util.SchedulerJobAttr
}

func buildUseAnnotationTestCases() []useAnnotationTestCase {
	return []useAnnotationTestCase{
		{
			Name:    "01-UseAnnotation task will select the npu which is on the card that has 1 npu other than 2",
			Task:    test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, MockResourceNumOne),
			Node:    initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-4,Ascend310-5"),
			PodAnno: "Ascend310-0",
		},
		{
			Name: "02-UseAnnotation task will select the npu which is on the card that has 2 npu other than 3",
			Task: test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, MockResourceNumOne),
			Node: initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-1,Ascend310-4,"+
				"Ascend310-5,Ascend310-6"),
			PodAnno: "Ascend310-0",
		},
		{
			Name: "03-UseAnnotation task will select the npu which is on the card that has 3 npu other than 4",
			Task: test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, MockResourceNumOne),
			Node: initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-1,Ascend310-2,"+
				"Ascend310-4,Ascend310-5,Ascend310-6,Ascend310-7"),
			PodAnno: "Ascend310-0",
		},
		{
			Name: "04-UseAnnotation task will return nil when select npu from node failed",
			Task: test.FakeTaskWithResReq(MockPodOneName, util.NPU310CardName, MockResourceNumOne),
			Node: initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-1,Ascend310-2,"+
				"Ascend310-4,Ascend310-5,Ascend310-6,Ascend310-7"),
			PodAnno: "Ascend310-0",
		},
	}
}

// TestUseAnnotation
func TestUseAnnotation(t *testing.T) {
	testCase := buildNilTPTestCase("UseAnnotation")
	t.Run(testCase.name, func(t *testing.T) {
		if res := testCase.tp.UseAnnotation(nil, plugin.NPUNode{}); !reflect.DeepEqual(res,
			testCase.wantNode) {
			t.Errorf("UseAnnotation() res: %v, want: %v", res, testCase.wantNode)
		}
	})
	attr := initAttr(MockJobName, MockTaskNumOne, MockNeedOne)
	env := plugin.ScheduleEnv{}
	env.Jobs = map[api.JobID]plugin.SchedulerJob{test.FakeJobName: {SchedulerJobAttr: attr}}
	npu := initNPU(attr, env)
	testCases := buildUseAnnotationTestCases()
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			node := npu.UseAnnotation(tt.Task, tt.Node)
			if node == nil && !reflect.DeepEqual(node, tt.WantNode) {
				t.Errorf("UseAnnotation() node: %v, wantNode: %v", node, tt.WantNode)
			}
			if node != nil && (!reflect.DeepEqual(node.Annotation, tt.Node.Annotation) ||
				!reflect.DeepEqual(tt.Task.Pod.Annotations[util.NPU310CardName], tt.PodAnno)) {
				t.Errorf("UseAnnotation() node: %v, wantNode: %v, anno %v, wantAnno %v",
					node, tt.WantNode, tt.Task.Pod.Annotations, tt.PodAnno)
			}
		})
	}
}

type selectNPUFromNodeTestCase struct {
	Name      string
	Task      *api.TaskInfo
	Node      plugin.NPUNode
	WantSlice []int
	WantErr   error
}

func buildSelectNPUFromNodeTestCases() []selectNPUFromNodeTestCase {
	return []selectNPUFromNodeTestCase{
		{
			Name:      "01-SelectNPUFromNode return error when node without target annotation",
			Task:      test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, MockResourceNumOne),
			Node:      initNode(MockNodeOneName, util.NPU310PCardName, "Ascend310-0,Ascend310-4,Ascend310-5"),
			WantSlice: nil,
			WantErr:   errors.New("getUsableTopFromNode  don't have huawei.com/Ascend310"),
		},
		{
			Name:      "02-SelectNPUFromNode return error when task request illegal npu number",
			Task:      test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, MockResourceNumOne),
			Node:      initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-4,Ascend310-5"),
			WantSlice: nil,
			WantErr:   errors.New("illegal request npu number: 5"),
		},
	}
}

func TestSelectNPUFromNode(t *testing.T) {
	testCase := buildNilTPTestCase("SelectNPUFromNode")
	t.Run(testCase.name, func(t *testing.T) {
		if res, err := testCase.tp.SelectNPUFromNode(nil, plugin.NPUNode{}); !reflect.DeepEqual(res,
			testCase.wantArr) && !reflect.DeepEqual(err, testCase.wantErr) {
			t.Errorf("UseAnnotation() res: %v, err: %v, wantArr: %v, wantErr: %v",
				res, err, testCase.wantArr, testCase.wantErr)
		}
	})
	tp := NewChip310x4(SchedulerName)
	attr := initAttr(MockJobName, MockTaskNumOne, MockNeedFive)
	env := plugin.ScheduleEnv{}
	env.Jobs = map[api.JobID]plugin.SchedulerJob{test.FakeJobName: {SchedulerJobAttr: attr}}
	tp.SetSchedulerAttr(attr)
	tp.SetSchedulerEnv(env)
	testCases := buildSelectNPUFromNodeTestCases()
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			if res, err := tp.SelectNPUFromNode(tt.Task, tt.Node); !reflect.DeepEqual(res,
				tt.WantSlice) && !reflect.DeepEqual(err, tt.WantErr) {
				t.Errorf("SelectNPUFromNode() res: %v, err: %v, wantSlice: %v, wantErr: %v",
					res, err, tt.WantSlice, tt.WantErr)
			}
		})
	}
}
