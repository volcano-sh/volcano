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
Package card310x4 is using for HuaWei A300T Ascend pin affinity schedule.
*/
package card310x4

import (
	"errors"
	"fmt"
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
	MockTaskNumOne      = 1
	MockTaskNumTwo      = 2
	MockResourceNumZero = 0
	MockResourceNumOne  = 1
	MockNeedOne         = "1"
	MockNeedTwo         = "2"
	MockNeedThree       = "3"
	MockNeedFive        = "5"
	MockJobName         = "testJob"
	MockPodZeroName     = "pod0"
	MockPodOneName      = "pod1"
	MockNodeOneName     = "node1"
)

type nilTPTestCase struct {
	name               string
	tp                 *card310x4
	wantNode           *plugin.NPUNode
	wantArr            []int
	wantValidateResult *api.ValidateResult
	wantErr            error
}

func NewCard310x4(schedulerName string) *card310x4 {
	tp := &card310x4{}
	tp.SetMaxCardNPUNum(maxCardNPUNum)
	tp.SetMaxNodeNPUNum(maxNodeNPUNum)
	tp.SetPluginName(schedulerName)
	tp.SetAnnoName(util.NPU310CardName)
	tp.SetAnnoPreVal(util.NPU310CardNamePre)
	tp.affScoreList = [][]int{
		{affScore0, affScore2, affScore1, affScore3},
		{affScore4, affScore0, affScore1, affScore2},
		{affScore4, affScore4, affScore0, affScore1},
		{affScore4, affScore4, affScore4, affScore0},
	}
	return tp
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
		wantValidateResult: &api.ValidateResult{
			Pass:    false,
			Reason:  "invalid argument",
			Message: "invalid argument"},
		wantErr: errors.New("invalid argument"),
	}
}

// checkNodeNPUByTaskTestCase CheckNodeNPUByTask test case
type checkNodeNPUByTaskTestCase struct {
	Task    *api.TaskInfo
	Name    string
	Attr    util.SchedulerJobAttr
	Node    plugin.NPUNode
	WantErr error
}

func buildCheckNodeNPUByTaskTestCases() []checkNodeNPUByTaskTestCase {
	node := initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-1,Ascend310-2")
	return []checkNodeNPUByTaskTestCase{
		{
			Name:    "01-CheckNodeNPUByTask return nil when node npu meet task req",
			Task:    &api.TaskInfo{Job: MockJobName},
			Node:    node,
			WantErr: fmt.Errorf("%s is not npu job", MockJobName),
		},

		{
			Name:    "02-CheckNodeNPUByTask return err when task is not npu task",
			Task:    &api.TaskInfo{Name: MockPodOneName, Job: test.FakeJobName},
			Node:    node,
			WantErr: fmt.Errorf("task<%s> is not npu task", MockPodOneName),
		},
		{
			Name:    "03-CheckNodeNPUByTask return err when node has no req npu",
			Task:    test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, util.NPUIndex3),
			Node:    initNode(MockNodeOneName, util.NPU310PCardName, ""),
			WantErr: fmt.Errorf("getUsableTopFromNode don't have %s", util.NPU310CardName),
		},
		{
			Name: "04-CheckNodeNPUByTask return err when node has no req npu",
			Task: test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, util.NPUIndex3),
			Node: initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0, Ascend310-1"),
			WantErr: fmt.Errorf("checkNodeNPUByTask %s : %v", util.NodeNotMeetTopologyWarning,
				fmt.Errorf("req npu(%d) illegal not meet node top<%v>", util.NPUIndex3, []int{})),
		},
		{
			Name: "05-CheckNodeNPUByTask return err when node has no req npu",
			Task: test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, util.NPUIndex3),
			Node: initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-1,Ascend310-4"),
			WantErr: fmt.Errorf("checkNodeNPUByTask %s : %v", util.NodeNotMeetTopologyWarning,
				fmt.Errorf("req npu(%d) illegal not meet node top<%v>", util.NPUIndex3, []int{0, 1, 4})),
		},
		{
			Name:    "06-CheckNodeNPUByTask return nil when node npu meet task req",
			Task:    test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, util.NPUIndex3),
			Node:    node,
			WantErr: nil,
		},
	}
}

// TestCheckNodeNPUByTask
func TestCheckNodeNPUByTask(t *testing.T) {
	testCase := buildNilTPTestCase("CheckNodeNPUByTask")
	t.Run(testCase.name, func(t *testing.T) {
		if err := testCase.tp.CheckNodeNPUByTask(nil,
			plugin.NPUNode{}); !reflect.DeepEqual(err, testCase.wantErr) {
			t.Errorf("CheckNodeNPUByTask() = %v, want %v", err, testCase.wantErr)
		}
	})
	attr := initAttr(MockJobName, MockTaskNumOne, MockNeedThree)
	env := plugin.ScheduleEnv{}
	env.Jobs = map[api.JobID]plugin.SchedulerJob{test.FakeJobName: {SchedulerJobAttr: attr}}
	npu := initNPU(attr, env)
	testCases := buildCheckNodeNPUByTaskTestCases()
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			if err := npu.CheckNodeNPUByTask(tt.Task, tt.Node); !reflect.DeepEqual(err, tt.WantErr) {
				t.Errorf("CheckNodeNPUByTask() error = %v, wantErr %v", err, tt.WantErr)
			}
		})
	}
}

// scoreBestNPUNodesTestCase scoreBestNPUNodes test case
type scoreBestNPUNodesTestCase struct {
	Task     *api.TaskInfo
	Nodes    []*api.NodeInfo
	ScoreMap map[string]float64
	WantSMap map[string]float64
	Name     string
	WantErr  error
	Attr     util.SchedulerJobAttr
}

func buildScoreBestNPUNodesTestCases() []scoreBestNPUNodesTestCase {
	const (
		score32 = 32
		score24 = 24
		score16 = 16
		score8  = 8
	)
	return []scoreBestNPUNodesTestCase{
		{
			Name:     "01-ScoreBestNPUNodes when return nil node npu meet task req",
			Task:     test.FakeTaskWithResReq("pod1", util.NPU310CardName, MockResourceNumOne),
			Nodes:    []*api.NodeInfo{{Name: "node1"}, {Name: "node2"}, {Name: "node3"}, {Name: "node4"}},
			ScoreMap: map[string]float64{"node1": 0, "node2": 0, "node3": 0, "node4": 0},
			WantSMap: map[string]float64{"node1": 0, "node2": 0, "node3": 0, "node4": 0},
			WantErr:  errors.New("task<pod1> is not npu task"),
		},
		{
			Name:     "02-ScoreBestNPUNodes when return nil node npu meet task req",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU310CardName, MockResourceNumOne),
			Nodes:    []*api.NodeInfo{nil, {Name: "node0"}, {Name: "node1"}, {Name: "node2"}, {Name: "node3"}, {Name: "node4"}},
			ScoreMap: map[string]float64{"node1": 0, "node2": 0, "node3": 0, "node4": 0},
			WantSMap: map[string]float64{"node1": score32, "node2": score16, "node3": score24, "node4": score8},
			WantErr:  nil,
		},
		{
			Name:     "03-ScoreBestNPUNodes return nil when node npu meet task req",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU310CardName, MockResourceNumOne),
			Nodes:    []*api.NodeInfo{{Name: "node1"}, {Name: "node2"}, {Name: "node3"}, {Name: "node5"}},
			ScoreMap: map[string]float64{"node1": 0, "node2": 0, "node3": 0, "node5": 0},
			WantSMap: map[string]float64{"node1": score32, "node2": score16, "node3": score24, "node5": 0},
			WantErr:  nil,
		},
		{
			Name:     "04-ScoreBestNPUNodes return err when node npu not meet task req",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU310CardName, MockResourceNumOne),
			Nodes:    []*api.NodeInfo{{Name: "node1"}, {Name: "node2"}, {Name: "node3"}, {Name: "node6"}},
			ScoreMap: map[string]float64{"node1": 0, "node2": 0, "node3": 0, "node6": 0},
			WantSMap: map[string]float64{"node1": score32, "node2": score16, "node3": score24, "node6": 0},
			WantErr:  nil,
		},
		{
			Name:     "05-ScoreBestNPUNodes return nil when node npu not meet task req",
			Task:     test.FakeTaskWithResReq("pod0", util.NPU310CardName, MockResourceNumOne),
			Nodes:    []*api.NodeInfo{{Name: "node1"}, {Name: "node2"}, {Name: "node3"}, {Name: "node7"}},
			ScoreMap: map[string]float64{"node1": 0, "node2": 0, "node3": 0, "node7": 0},
			WantSMap: map[string]float64{"node1": score32, "node2": score16, "node3": score24, "node7": score32},
			WantErr:  nil,
		},
	}
}

func buildTaskNPUNumTestCase() scoreBestNPUNodesTestCase {
	return scoreBestNPUNodesTestCase{
		Name:     "06-ScoreBestNPUNodes return err when taskNPUNum is invalid",
		Task:     test.FakeTaskWithResReq("pod0", util.NPU310CardName, MockResourceNumZero),
		Nodes:    []*api.NodeInfo{{Name: "node1"}, {Name: "node2"}, {Name: "node3"}, {Name: "node4"}},
		ScoreMap: map[string]float64{"node1": 0, "node2": 0, "node3": 0, "node4": 0},
		WantSMap: map[string]float64{"node1": 0, "node2": 0, "node3": 0, "node4": 0},
		WantErr:  errors.New("task<pod0> req npu num<5> is invalid"),
	}
}

func initScheduleEnv(attr util.SchedulerJobAttr) plugin.ScheduleEnv {
	return plugin.ScheduleEnv{
		ClusterCache: plugin.ClusterCache{
			Jobs: map[api.JobID]plugin.SchedulerJob{test.FakeJobName: {SchedulerJobAttr: attr}},
			Nodes: map[string]plugin.NPUNode{
				"node1": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU310CardName: "Ascend310-0"}}},
				"node2": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU310CardName: "Ascend310-0," +
					"Ascend310-1"}}},
				"node3": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU310CardName: "Ascend310-0," +
					"Ascend310-1,Ascend310-2"}}},
				"node4": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU310CardName: "Ascend310-0," +
					"Ascend310-1,Ascend310-2,Ascend310-3"}}},
				"node5": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU310CardName: "Ascend310-0, " +
					"Ascend310-4"}}},
				"node6": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU310CardName: ""}}},
				"node7": {CommonNode: plugin.CommonNode{Annotation: map[string]string{util.NPU310CardName: "Ascend310-4"}}},
			},
		},
	}
}

// TestCheckNodeNPUByTask
func TestScoreBestNPUNodes(t *testing.T) {
	testCase := buildNilTPTestCase("ScoreBestNPUNodes")
	t.Run(testCase.name, func(t *testing.T) {
		if err := testCase.tp.ScoreBestNPUNodes(nil, nil,
			nil); !reflect.DeepEqual(err, testCase.wantErr) {
			t.Errorf("ScoreBestNPUNodes() = %v, want %v", err, testCase.wantErr)
		}
	})
	attr := initAttr(MockJobName, MockTaskNumOne, MockNeedOne)
	env := initScheduleEnv(attr)
	npu := initNPU(attr, env)
	testCases := buildScoreBestNPUNodesTestCases()
	for _, tt := range testCases {
		t.Run(tt.Name, func(t *testing.T) {
			err := npu.ScoreBestNPUNodes(tt.Task, tt.Nodes, tt.ScoreMap)
			if !reflect.DeepEqual(err, tt.WantErr) || !reflect.DeepEqual(tt.ScoreMap, tt.WantSMap) {
				t.Errorf("ScoreBestNPUNodes() scoreMap: %v, wantSMap: %v, error = %v, wantErr %v",
					tt.ScoreMap, tt.WantSMap, err, tt.WantErr)
			}
		})
	}
	attr = initAttr(MockJobName, MockTaskNumOne, MockNeedFive)
	env = initScheduleEnv(attr)
	npu = initNPU(attr, env)
	extraTestCase := buildTaskNPUNumTestCase()
	t.Run(extraTestCase.Name, func(t *testing.T) {
		err := npu.ScoreBestNPUNodes(extraTestCase.Task, extraTestCase.Nodes, extraTestCase.ScoreMap)
		if !reflect.DeepEqual(err, extraTestCase.WantErr) || !reflect.DeepEqual(extraTestCase.ScoreMap,
			extraTestCase.WantSMap) {
			t.Errorf("ScoreBestNPUNodes() scoreMap: %v, wantSMap: %v, error = %v, wantErr %v",
				extraTestCase.ScoreMap, extraTestCase.WantSMap, err, extraTestCase.WantErr)
		}
	})
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
			Name:    "01-UseAnnotation task will select the npu which is the only one on the card",
			Task:    test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, MockResourceNumOne),
			Node:    initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-4,Ascend310-5"),
			PodAnno: "Ascend310-0",
		},
		{
			Name: "02-UseAnnotation task will select the npu which is on the card that has 3 npu other than 2",
			Task: test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, MockResourceNumOne),
			Node: initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-1,Ascend310-2,"+
				"Ascend310-4,Ascend310-5"),
			PodAnno: "Ascend310-0",
		},
		{
			Name: "03-UseAnnotation task will select the npu which is on the card that has 3 npu other than 2",
			Task: test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, MockResourceNumOne),
			Node: initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-1,Ascend310-4,"+
				"Ascend310-5,Ascend310-6"),
			PodAnno: "Ascend310-4",
		},
		{
			Name: "04-UseAnnotation task will select the npu which is on the card that has 2 npu other than 4",
			Task: test.FakeTaskWithResReq(MockPodZeroName, util.NPU310CardName, MockResourceNumOne),
			Node: initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-1,Ascend310-4,"+
				"Ascend310-5,Ascend310-6,Ascend310-7"),
			PodAnno: "Ascend310-0",
		},
		{
			Name:     "05-UseAnnotation return nil when task is not npu task",
			Task:     test.FakeTaskWithResReq(MockPodOneName, util.NPU310CardName, MockResourceNumOne),
			Node:     initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0,Ascend310-4,Ascend310-5"),
			PodAnno:  "Ascend310-0",
			WantNode: nil,
		},
	}
}

func TestUseAnnotation(t *testing.T) {
	testCase := buildNilTPTestCase("UseAnnotation")
	t.Run(testCase.name, func(t *testing.T) {
		if res := testCase.tp.UseAnnotation(nil,
			plugin.NPUNode{}); !reflect.DeepEqual(res, testCase.wantNode) {
			t.Errorf("UseAnnotation() = %v, want %v", res, testCase.wantNode)
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
			Node:      initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0"),
			WantSlice: nil,
			WantErr:   errors.New("getUsableTopFromNode  don't have huawei.com/Ascend310"),
		},
		{
			Name:      "02-SelectNPUFromNode return error when task request illegal npu number",
			Task:      test.FakeTaskWithResReq("pod0", util.NPU310CardName, MockResourceNumOne),
			Node:      initNode(MockNodeOneName, util.NPU310CardName, "Ascend310-0"),
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
			t.Errorf("SelectNPUFromNode() res: %v, err: %v, want %v", res, err, testCase.wantErr)
		}
	})
	testCases := buildSelectNPUFromNodeTestCases()
	tp := NewCard310x4(SchedulerName)
	attr := initAttr(MockJobName, MockTaskNumOne, MockNeedFive)
	tp.SetSchedulerAttr(attr)
	env := plugin.ScheduleEnv{}
	env.Jobs = map[api.JobID]plugin.SchedulerJob{test.FakeJobName: {SchedulerJobAttr: attr}}
	tp.SetSchedulerEnv(env)
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
