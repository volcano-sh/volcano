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
Package base is using for HuaWei Ascend pin affinity schedule.
*/
package base

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	itest "volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/test"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

const (
	maxNodeNPUNum = 64
	maxCardNPUNum = 4
)

func TestIsInstanceOfJobGroup(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{
			name:     "01 - jobID labels exist (app and jobID) should return true",
			labels:   map[string]string{jobGroupIDLabelKey: "123"},
			expected: true,
		},
		{
			name:     "02 - Missing app label should return false",
			labels:   map[string]string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := &NPUHandler{}
			tp.Label = tt.labels
			result := tp.IsInstanceOfJobGroup()
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("isMindIEJob() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// initMyJobPluginTestCase test case
type initMyJobPluginTestCase struct {
	Name    string
	Attr    util.SchedulerJobAttr
	Env     plugin.ScheduleEnv
	WantErr error
}

func buildInitMyJobPluginTestCases() []initMyJobPluginTestCase {
	return []initMyJobPluginTestCase{
		{
			Name: "01-InitMyJobPlugin return nil",
			Attr: util.SchedulerJobAttr{
				ComJob: util.ComJob{Label: map[string]string{}},
				NPUJob: &util.NPUJob{ReqNPUName: util.NPU310CardName},
			},
			Env:     plugin.ScheduleEnv{},
			WantErr: nil,
		},
	}
}

// TestInitMyJobPlugin
func TestInitMyJobPlugin(t *testing.T) {
	npu := NPUHandler{}
	testCases := buildInitMyJobPluginTestCases()
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			err := npu.InitMyJobPlugin(tt.Attr, tt.Env)
			if !reflect.DeepEqual(tt.Attr, npu.SchedulerJobAttr) ||
				!reflect.DeepEqual(tt.Env, npu.ScheduleEnv) ||
				!reflect.DeepEqual(tt.WantErr, err) {
				t.Errorf("InitMyJobPlugin() err: %v, wantErr: %v", err, tt.WantErr)
			}
		})
	}
}

func TestNilFunc(t *testing.T) {
	t.Run("tp nil test", func(t *testing.T) {
		var tp *NPUHandler
		tp.SetSchedulerAttr(util.SchedulerJobAttr{})
		tp.SetSchedulerEnv(plugin.ScheduleEnv{})
		wantErr := errors.New(util.ArgumentError)
		if err := tp.InitMyJobPlugin(util.SchedulerJobAttr{}, plugin.ScheduleEnv{}); !reflect.DeepEqual(err, wantErr) {
			t.Errorf("InitMyJobPlugin() err: %v, wantErr: %v", err, wantErr)
		}
		if result := tp.ValidNPUJob(); result.Pass != false {
			t.Errorf("ValidNPUJob() cann pass = %v, want %v", result.Pass, false)
		}
		if err := tp.CheckNodeNPUByTask(nil, plugin.NPUNode{}); !reflect.DeepEqual(err, wantErr) {
			t.Errorf("CheckNodeNPUByTask() error = %v, wantErr %v", err, wantErr)
		}
		if err := tp.ScoreBestNPUNodes(nil, nil, nil); !reflect.DeepEqual(err, wantErr) {
			t.Errorf("ScoreBestNPUNodes() error = %v, wantErr %v", err, wantErr)
		}
		if _, err := tp.SelectNPUFromNode(nil, plugin.NPUNode{}); !reflect.DeepEqual(err, wantErr) {
			t.Errorf("SelectNPUFromNode() error = %v, wantErr %v", err, wantErr)
		}
	})
}

// validNPUJobTestCase validNPUJob test case
type validNPUJobTestCase struct {
	WantErr *api.ValidateResult
	Name    string
	Attr    util.SchedulerJobAttr
}

func buildValidNPUJobTestCase01() []validNPUJobTestCase {
	job01 := test.FakeNormalTestJob("job01", 1)
	test.SetFakeJobResRequest(job01, util.NPU310CardName, "1")
	attr1 := itest.FakeSchedulerJobAttrByJob(job01)
	job02 := test.FakeNormalTestJob("job02", 1)
	test.SetFakeJobResRequest(job02, util.NPU310CardName, "65")
	attr2 := itest.FakeSchedulerJobAttrByJob(job02)
	job03 := test.FakeNormalTestJob("job02", 1)
	test.SetFakeJobResRequest(job03, util.NPU310CardName, "2")
	attr3 := itest.FakeSchedulerJobAttrByJob(job03)
	return []validNPUJobTestCase{
		{
			Name:    "01-ValidNPUJob should return error when job request no npu",
			Attr:    attr1,
			WantErr: nil,
		},
		{
			Name: "02-ValidNPUJob should return error when tasks request npu more than 64",
			Attr: attr2,
			WantErr: &api.ValidateResult{
				Pass:    false,
				Reason:  "task req npu num is invalid",
				Message: "job<vcjob/job02> req npu num<65> is invalid",
			},
		},
		{
			Name:    "03-ValidNPUJob should return nil when tasks request is valid",
			Attr:    attr3,
			WantErr: nil,
		},
	}
}

// TestValidNPUJob
func TestValidNPUJob(t *testing.T) {
	npu := NPUHandler{
		MaxCardNPUNum: maxCardNPUNum,
		MaxNodeNPUNum: maxNodeNPUNum,
	}
	testCases := buildValidNPUJobTestCase01()
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			npu.SetSchedulerAttr(tt.Attr)
			if err := npu.ValidNPUJob(); !reflect.DeepEqual(err, tt.WantErr) {
				t.Errorf("ValidNPUJob() error = %v, wantErr %v", err, tt.WantErr)
			}
		})
	}
}

type checkNodeNPUByTaskTestCase struct {
	Task    *api.TaskInfo
	Name    string
	Attr    util.SchedulerJobAttr
	Node    plugin.NPUNode
	WantErr error
}

func buildCheckNodeNPUByTaskTestCase1() checkNodeNPUByTaskTestCase {
	return checkNodeNPUByTaskTestCase{
		Name: "01-CheckNodeNPUByTask when return nil node npu meet task req",
		Task: test.FakeTaskWithResReq("pod0", util.NPU310PCardName, util.NPUIndex2),
		Node: plugin.NPUNode{
			CommonNode: plugin.CommonNode{
				Name:       "node1",
				Annotation: map[string]string{util.NPU310PCardName: "Ascend310P-0,Ascend310P-1"},
			},
		},
		WantErr: nil,
	}
}

func buildCheckNodeNPUByTaskTestCase2() checkNodeNPUByTaskTestCase {
	return checkNodeNPUByTaskTestCase{
		Name: "02-CheckNodeNPUByTask return err when task is not npu task",
		Task: test.FakeTaskWithResReq("pod1", util.NPU310PCardName, util.NPUIndex2),
		Node: plugin.NPUNode{
			CommonNode: plugin.CommonNode{
				Name:       "node1",
				Annotation: map[string]string{util.NPU310PCardName: "Ascend310P-0,Ascend310P-1"},
			},
		},
		WantErr: errors.New("task<pod1> is not npu task"),
	}
}

func buildCheckNodeNPUByTaskTestCase3() checkNodeNPUByTaskTestCase {
	return checkNodeNPUByTaskTestCase{
		Name: "03-CheckNodeNPUByTask return err when node has no req npu",
		Task: test.FakeTaskWithResReq("pod0", util.NPU310PCardName, util.NPUIndex2),
		Node: plugin.NPUNode{
			CommonNode: plugin.CommonNode{
				Name:       "node1",
				Annotation: map[string]string{util.NPU910CardName: "Ascend310P-0,Ascend310P-1"},
			},
		},
		WantErr: errors.New("getUsableTopFromNode don't have huawei.com/Ascend310P"),
	}
}

func buildCheckNodeNPUByTaskTestCase4() checkNodeNPUByTaskTestCase {
	return checkNodeNPUByTaskTestCase{
		Name: "04-CheckNodeNPUByTask return err when node has no req npu",
		Task: test.FakeTaskWithResReq("pod0", util.NPU310PCardName, util.NPUIndex2),
		Node: plugin.NPUNode{
			CommonNode: plugin.CommonNode{
				Name:       "node1",
				Annotation: map[string]string{util.NPU310PCardName: "Ascend310P-0, Ascend310P-1"},
			},
		},
		WantErr: fmt.Errorf("judgeNodeAndTaskNPU node don't have enough resource, req<2>, idle<0>"),
	}
}

func buildCheckNodeNPUByTaskTestCase5() checkNodeNPUByTaskTestCase {
	return checkNodeNPUByTaskTestCase{
		Name: "05-CheckNodeNPUByTask return err when node has no enough npu",
		Task: test.FakeTaskWithResReq("pod0", util.NPU310PCardName, util.NPUIndex2),
		Node: plugin.NPUNode{
			CommonNode: plugin.CommonNode{
				Name:       "node1",
				Annotation: map[string]string{util.NPU310PCardName: "Ascend310P-0"},
			},
		},
		WantErr: errors.New("judgeNodeAndTaskNPU node don't have enough resource, req<2>, idle<1>"),
	}
}

func buildCheckNodeNPUByTaskTestCases() []checkNodeNPUByTaskTestCase {
	return []checkNodeNPUByTaskTestCase{
		buildCheckNodeNPUByTaskTestCase1(),
		buildCheckNodeNPUByTaskTestCase2(),
		buildCheckNodeNPUByTaskTestCase3(),
		buildCheckNodeNPUByTaskTestCase4(),
		buildCheckNodeNPUByTaskTestCase5(),
	}
}

// TestCheckNodeNPUByTask
func TestCheckNodeNPUByTask(t *testing.T) {
	npu := New(util.NPU310PCardName,
		WithAnnoPreVal(util.NPU310PCardNamePre), WithMaxNodeNum(maxNodeNPUNum),
		WithNpuInvalidMap(nil), WithNetworkFault(true))
	job := test.FakeNormalTestJob("job", 1)
	test.SetFakeJobResRequest(job, util.NPU310PCardName, "2")
	attr1 := itest.FakeSchedulerJobAttrByJob(job)
	npu.SetSchedulerAttr(attr1)
	env := plugin.ScheduleEnv{ClusterCache: plugin.ClusterCache{
		Jobs: map[api.JobID]plugin.SchedulerJob{test.FakeJobName: {SchedulerJobAttr: attr1}}},
	}
	npu.SetSchedulerEnv(env)
	testCases := buildCheckNodeNPUByTaskTestCases()
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			if err := npu.CheckNodeNPUByTask(tt.Task, tt.Node); !reflect.DeepEqual(err, tt.WantErr) {
				t.Errorf("CheckNodeNPUByTask() error = %v, wantErr %v", err, tt.WantErr)
			}
		})
	}
}

type useAnnotationTestCase struct {
	Task     *api.TaskInfo
	WantNode *plugin.NPUNode
	Name     string
	Node     plugin.NPUNode
	PodAnno  string
	Attr     util.SchedulerJobAttr
}

func buildUseAnnotationTestCases01() []useAnnotationTestCase {
	return []useAnnotationTestCase{
		{
			Name: "01-UseAnnotation success when node resource meet task req",
			Task: test.FakeTaskWithResReq("pod0", util.NPU310PCardName, util.NPUIndex2),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU310PCardName: "Ascend310P-0,Ascend310P-1",
						networkUnhealthyNPU: "Ascend910-0"},
					Allocate:       map[v1.ResourceName]float64{util.NPU310PCardName: util.NPUIndex2 * util.NPUHexKilo},
					BaseDeviceInfo: fakeBaseInfo(),
				},
			},
			PodAnno: "Ascend310P-0,Ascend310P-1",
			WantNode: &plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Allocate:       map[v1.ResourceName]float64{util.NPU310PCardName: util.NPUIndex2 * util.NPUHexKilo},
					Annotation:     map[string]string{util.NPU310PCardName: "", networkUnhealthyNPU: "Ascend910-0"},
					BaseDeviceInfo: fakeBaseInfo(),
				},
			},
		},
		{
			Name: "02-UseAnnotation return err when task is nil",
			Task: nil,
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU310PCardName: ""},
					Allocate:   map[v1.ResourceName]float64{util.NPU310PCardName: 0},
				},
			},
			PodAnno:  "",
			WantNode: nil,
		},
		{
			Name: "03-UseAnnotation return err when node annotation is nil",
			Task: new(api.TaskInfo),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: nil,
					Allocate:   map[v1.ResourceName]float64{util.NPU310PCardName: 0},
				},
			},
			PodAnno:  "",
			WantNode: nil,
		},
	}
}

func buildUseAnnotationTestCases02() []useAnnotationTestCase {
	return []useAnnotationTestCase{
		{
			Name: "04-UseAnnotation return err when node annotation resource less than task req npu",
			Task: test.FakeTaskWithResReq("pod0", util.NPU310PCardName, util.NPUIndex2),
			Node: plugin.NPUNode{
				CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU310PCardName: ""},
					Allocate:   map[v1.ResourceName]float64{util.NPU310PCardName: 0},
				},
			},
			PodAnno:  "",
			WantNode: nil,
		},
	}
}

// TestUseAnnotation
func TestUseAnnotation(t *testing.T) {
	npu := NPUHandler{}
	npu.SetAnnoName(util.NPU310PCardName)
	npu.SetAnnoPreVal(util.NPU310PCardNamePre)
	npu.SetMaxNodeNPUNum(maxNodeNPUNum)
	npu.SetIsNetworkFaultAttention(true)
	job := test.FakeNormalTestJob("job", util.NPUIndex2)
	test.SetFakeJobResRequest(job, util.NPU910CardName, "2")
	attr := itest.FakeSchedulerJobAttrByJob(job)
	env := plugin.ScheduleEnv{ClusterCache: plugin.ClusterCache{
		Jobs: map[api.JobID]plugin.SchedulerJob{test.FakeJobName: {SchedulerJobAttr: attr}}}}
	if err := npu.InitMyJobPlugin(attr, env); err != nil {
		t.Errorf("InitMyJobPlugin failed err: %s", err.Error())
	}
	testCases := buildUseAnnotationTestCases01()
	testCases = append(testCases, buildUseAnnotationTestCases02()...)
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			node := npu.UseAnnotation(tt.Task, tt.Node)
			if tt.Task != nil && tt.Node.Annotation != nil && !reflect.DeepEqual(
				tt.Task.Pod.Annotations[util.NPU310PCardName], tt.PodAnno) {
				t.Errorf("UseAnnotation() anno %v, wantAnno %v", tt.Task.Pod.Annotations, tt.PodAnno)
			}
			if !reflect.DeepEqual(node, tt.WantNode) {
				t.Errorf("UseAnnotation() node: %v, wantNode: %v", node, tt.WantNode)
			}
		})
	}
}

// judgeNodeAndTaskNPUTestCase JudgeNodeAndTaskNPU test case
type judgeNodeAndTaskNPUTestCase struct {
	NodeTop []int
	Name    string
	TaskNPU int
	WantErr error
}

func buildJudgeNodeAndTaskNPUTestCases() []judgeNodeAndTaskNPUTestCase {
	const npuNum65 = 65
	return []judgeNodeAndTaskNPUTestCase{
		{
			Name:    "01-JudgeNodeAndTaskNPU return err when task npu nun is 0",
			TaskNPU: 0,
			NodeTop: []int{0, 1},
			WantErr: errors.New("judgeNodeAndTaskNPU task req num<0> is invalid"),
		},
		{
			Name:    "02-JudgeNodeAndTaskNPU return err when task npu num is 65",
			TaskNPU: npuNum65,
			NodeTop: []int{0, 1},
			WantErr: errors.New("judgeNodeAndTaskNPU task req num<65> is invalid"),
		},
		{
			Name:    "03-JudgeNodeAndTaskNPU return err when node not meet task npu num",
			TaskNPU: util.NPUIndex2,
			NodeTop: []int{0},
			WantErr: errors.New("judgeNodeAndTaskNPU node don't have enough resource, req<2>, idle<1>"),
		},
	}
}

// TestJudgeNodeAndTaskNPU
func TestJudgeNodeAndTaskNPU(t *testing.T) {
	npu := NPUHandler{}
	npu.SetMaxNodeNPUNum(maxNodeNPUNum)
	testCases := buildJudgeNodeAndTaskNPUTestCases()
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			if err := npu.JudgeNodeAndTaskNPU(tt.TaskNPU, tt.NodeTop); !reflect.DeepEqual(err, tt.WantErr) {
				t.Errorf("JudgeNodeAndTaskNPU() err: %s, wanterr: %s", err, tt.WantErr)
			}
		})
	}
}

// setMaxNodeNPUNumTestCase  SetMaxNodeNPUNum test case
type setMaxNodeNPUNumTestCase struct {
	Name    string
	Num     int
	WantNum int
}

func buildSetMaxNodeNPUNumTestCase() []setMaxNodeNPUNumTestCase {
	return []setMaxNodeNPUNumTestCase{
		{
			Name:    "01-SetMaxNodeNPUNum the get num not equal wantNum when num is invalid",
			Num:     -1,
			WantNum: 0,
		},
		{
			Name:    "02-SetMaxNodeNPUNum the get num equal wantNum when num is valid",
			Num:     1,
			WantNum: 1,
		},
	}
}

// TestSetMaxNodeNPUNum
func TestSetMaxNodeNPUNum(t *testing.T) {
	npu := NPUHandler{}
	testCases := buildSetMaxNodeNPUNumTestCase()
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			npu.SetMaxNodeNPUNum(tt.Num)
			if npu.MaxNodeNPUNum != tt.WantNum {
				t.Errorf("SetMaxNodeNPUNum() num: %d, wanterr: %d", npu.MaxNodeNPUNum, tt.WantNum)
			}
		})
	}
}

type scoreBestNPUNodesTestCase struct {
	Task     *api.TaskInfo
	Nodes    []*api.NodeInfo
	ScoreMap map[string]float64
	WantSMap map[string]float64
	Name     string
	WantErr  error
	Attr     util.SchedulerJobAttr
}

func buildScoreBestNPUNodesTestCases01() []scoreBestNPUNodesTestCase {
	const score = 400
	return []scoreBestNPUNodesTestCase{
		{
			Name:     "01-ScoreBestNPUNodes return err when task is not this job npu task ",
			Task:     test.FakeTaskWithResReq("pod1", util.NPU910CardName, 1),
			Nodes:    []*api.NodeInfo{{Name: "node1"}, {Name: "node2"}},
			ScoreMap: map[string]float64{"node1": 0, "node2": 0},
			WantSMap: map[string]float64{"node1": score, "node2": 0},
			WantErr:  nil,
		},
		{
			Name:     "02-ScoreBestNPUNodes scoreMap no refresh when node is not this job npu node",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Nodes:    []*api.NodeInfo{{Name: "node6"}},
			ScoreMap: map[string]float64{"node6": 0},
			WantSMap: map[string]float64{"node6": 0},
			WantErr:  nil,
		},
		{
			Name:     "03-ScoreBestNPUNodes scoreMap no refresh when node netUnhealthyNPU not define",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Nodes:    []*api.NodeInfo{{Name: "node7"}},
			ScoreMap: map[string]float64{"node7": 0},
			WantSMap: map[string]float64{"node7": 0},
			WantErr:  nil,
		},
	}
}

func buildScoreBestNPUNodesTestCases02() []scoreBestNPUNodesTestCase {
	const (
		score = 400
	)
	return []scoreBestNPUNodesTestCase{
		{
			Name:     "04-ScoreBestNPUNodes scoreMap no refresh when node has no npu",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Nodes:    []*api.NodeInfo{{Name: "node8"}},
			ScoreMap: map[string]float64{"node8": 0},
			WantSMap: map[string]float64{"node8": 0},
			WantErr:  nil,
		},
		{
			Name:     "05-ScoreBestNPUNodes return nil when node npu meet task req",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU910CardName, 1),
			Nodes:    []*api.NodeInfo{{Name: "node1"}, {Name: "node3"}, {Name: "node4"}, {Name: "node5"}},
			ScoreMap: map[string]float64{"node1": 0, "node3": 0, "node4": 0, "node5": 0},
			WantSMap: map[string]float64{"node1": score, "node3": score, "node4": score, "node5": score},
			WantErr:  nil,
		},
	}
}

func buildFakeScheduleEnv() plugin.ScheduleEnv {
	const allocateNPUNum4 = 4
	return plugin.ScheduleEnv{
		ClusterCache: plugin.ClusterCache{
			Nodes: map[string]plugin.NPUNode{
				"node1": {
					CommonNode: plugin.CommonNode{
						Annotation: map[string]string{util.NPU910CardName: "Ascend910-0", networkUnhealthyNPU: ""},
						Allocate:   map[v1.ResourceName]float64{util.NPU910CardName: allocateNPUNum4 * util.NPUHexKilo},
					},
				},
				"node2": {
					CommonNode: plugin.CommonNode{
						Annotation: map[string]string{util.NPU910CardName: "Ascend910-0,Ascend910-1"},
					},
				},
				"node3": {
					CommonNode: plugin.CommonNode{
						Annotation: map[string]string{util.NPU910CardName: "Ascend910-0,Ascend910-1,Ascend910-2",
							networkUnhealthyNPU: ""},
						Allocate: map[v1.ResourceName]float64{util.NPU910CardName: allocateNPUNum4 * util.NPUHexKilo}},
				},
				"node4": {
					CommonNode: plugin.CommonNode{
						Annotation: map[string]string{util.NPU910CardName: "Ascend910-0,Ascend910-1",
							networkUnhealthyNPU: ""},
						Allocate: map[v1.ResourceName]float64{util.NPU910CardName: allocateNPUNum4 * util.NPUHexKilo}},
				},
				"node5": {CommonNode: plugin.CommonNode{
					Annotation: map[string]string{util.NPU910CardName: "Ascend910-0,Ascend910-1,Ascend910-2," +
						"Ascend910-3", networkUnhealthyNPU: ""},
					Allocate: map[v1.ResourceName]float64{util.NPU910CardName: allocateNPUNum4 * util.NPUHexKilo}},
				},
				"node6": {CommonNode: plugin.CommonNode{Annotation: map[string]string{}}},
				"node7": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU910CardName: "Ascend910-0"}}},
				"node8": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU910CardName: "",
					networkUnhealthyNPU: ""}}},
				"node9": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU910CardName: "",
					networkUnhealthyNPU: ""}}}},
		},
	}
}

// TestScoreBestNPUNodes
func TestScoreBestNPUNodes(t *testing.T) {
	npu := &NPUHandler{}
	npu.SetAnnoName(util.NPU910CardName)
	job := test.FakeNormalTestJob("job", 1)
	test.SetFakeJobResRequest(job, util.NPU910CardName, "1")
	attr := itest.FakeSchedulerJobAttrByJob(job)
	npu.SetSchedulerAttr(attr)
	env := buildFakeScheduleEnv()
	env.Jobs = map[api.JobID]plugin.SchedulerJob{test.FakeJobName: {SchedulerJobAttr: attr}}
	npu.SetSchedulerEnv(env)
	testCases := buildScoreBestNPUNodesTestCases01()
	testCases = append(testCases, buildScoreBestNPUNodesTestCases02()...)
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			err := npu.ScoreBestNPUNodes(tt.Task, tt.Nodes, tt.ScoreMap)
			if !reflect.DeepEqual(err, tt.WantErr) || !reflect.DeepEqual(tt.ScoreMap, tt.WantSMap) {
				t.Errorf("ScoreBestNPUNodes() scoreMap: %v, wantSMap: %v, error = %v, wantErr %v",
					tt.ScoreMap, tt.WantSMap, err, tt.WantErr)
			}
		})
	}
}

func fakeBaseInfo() string {
	fakeInfo := map[string]*util.NpuBaseInfo{
		"Ascend310P-0": {IP: "testIp", SuperDeviceID: 0},
		"Ascend310P-1": {IP: "testIp", SuperDeviceID: 0},
	}
	str, err := json.Marshal(fakeInfo)
	if err != nil {
		return ""
	}
	return string(str)
}
