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
Package superpod is using for HuaWei Atlas 900 A3 SuperPod affinity schedule.
*/
package superpod

import (
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/ascend910/ascend910a3"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/rescheduling"
	test2 "volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/test"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

type selectSuperPodForJobTestCase struct {
	name       string
	nodes      []*api.NodeInfo
	tasks      map[api.TaskID]util.NPUTask
	npuTaskNum int
	frameAttr  plugin.VolcanoFrame
	spBlock    int
	want       map[int32]struct{}
	wantErr    error
}

func newNPUNodeWithSuperPodID(nodeName string, superPodID int32) plugin.NPUNode {
	return plugin.NPUNode{
		CommonNode: plugin.CommonNode{
			Name:       nodeName,
			SuperPodID: superPodID,
			Tasks:      map[api.TaskID]*api.TaskInfo{},
		},
	}
}

func newNPUNodes(n int, sp int) map[string]plugin.NPUNode {
	nodes := make(map[string]plugin.NPUNode)
	for i := 0; i < n; i++ {
		nodeName := "node" + strconv.Itoa(i)
		nodes[nodeName] = newNPUNodeWithSuperPodID(nodeName, int32(i/sp))
	}
	return nodes
}

func newNPUTasks(n int) map[api.TaskID]util.NPUTask {
	tasks := make(map[api.TaskID]util.NPUTask)
	for i := 0; i < n; i++ {
		tasks[api.TaskID(strconv.Itoa(i))] = util.NPUTask{Name: "task" + strconv.Itoa(i)}
	}
	return tasks
}

var (
	node0  = &api.NodeInfo{Name: "node0"}
	node1  = &api.NodeInfo{Name: "node1"}
	node2  = &api.NodeInfo{Name: "node2"}
	node3  = &api.NodeInfo{Name: "node3"}
	node4  = &api.NodeInfo{Name: "node4"}
	node10 = &api.NodeInfo{Name: "node10"}
	node11 = &api.NodeInfo{Name: "node11"}
	node12 = &api.NodeInfo{Name: "node12"}
	node13 = &api.NodeInfo{Name: "node13"}
	node14 = &api.NodeInfo{Name: "node14"}
	node20 = &api.NodeInfo{Name: "node20"}
	node21 = &api.NodeInfo{Name: "node21"}
	node22 = &api.NodeInfo{Name: "node22"}
	node23 = &api.NodeInfo{Name: "node23"}
	node24 = &api.NodeInfo{Name: "node24"}
	node25 = &api.NodeInfo{Name: "node25"}
	node26 = &api.NodeInfo{Name: "node26"}
	node27 = &api.NodeInfo{Name: "node27"}
)

const (
	superPodSize10  = 10
	reservePodSize2 = 2
	reservePodSize4 = 4
	spBlockNum1     = 1
	spBlockNum2     = 2
	npuTaskNum1     = 1
	npuTaskNum2     = 2
	npuTaskNum4     = 4
	superPodId2     = 2
)

var selectSuperPodForJobTestCases = []selectSuperPodForJobTestCase{
	{
		name:  "01-total nodes is not fit for job require, should return err",
		tasks: newNPUTasks(npuTaskNum4),
		nodes: []*api.NodeInfo{node0, node1},
		frameAttr: plugin.VolcanoFrame{
			ConfigParameters: plugin.ConfigParameters{DynamicParameters: plugin.DynamicParameters{
				SuperPodSize:   superPodSize10,
				ReservePodSize: reservePodSize2,
			}}},
		npuTaskNum: npuTaskNum4,
		spBlock:    spBlockNum2,
		want:       nil,
		wantErr:    errors.New("select super pod failed, required 2, total 1"),
	},

	{
		name:       "02-2*2 job will select 4 and 5 from super pods with node 4, 5 or 6",
		tasks:      newNPUTasks(npuTaskNum4),
		npuTaskNum: npuTaskNum4,
		nodes: []*api.NodeInfo{node0, node1, node2, node3, node10, node11, node12, node13, node14, node20,
			node21, node22, node23, node24, node25,
		},

		frameAttr: plugin.VolcanoFrame{
			ConfigParameters: plugin.ConfigParameters{DynamicParameters: plugin.DynamicParameters{
				SuperPodSize:   superPodSize10,
				ReservePodSize: reservePodSize2,
			}}},
		spBlock: spBlockNum2,
		want:    map[int32]struct{}{0: {}, 1: {}},
		wantErr: nil,
	},

	{
		name:  "03-2*2 job will select 6 from super pods with node 5 or 6",
		tasks: newNPUTasks(npuTaskNum4),
		nodes: []*api.NodeInfo{node10, node11, node12, node13, node14, node20,
			node21, node22, node23, node24, node25,
		},
		npuTaskNum: npuTaskNum4,
		frameAttr: plugin.VolcanoFrame{
			ConfigParameters: plugin.ConfigParameters{DynamicParameters: plugin.DynamicParameters{
				SuperPodSize:   superPodSize10,
				ReservePodSize: reservePodSize2,
			}}},
		spBlock: spBlockNum2,
		want:    map[int32]struct{}{superPodId2: {}},
		wantErr: nil,
	},

	{
		name:  "04-2*2 job will select 5 and 8 from super pods with node 5 or 8",
		tasks: newNPUTasks(npuTaskNum4),
		nodes: []*api.NodeInfo{node10, node11, node12, node13, node14, node20,
			node21, node22, node23, node24, node25, node26, node27,
		},
		npuTaskNum: npuTaskNum4,
		frameAttr: plugin.VolcanoFrame{
			ConfigParameters: plugin.ConfigParameters{DynamicParameters: plugin.DynamicParameters{
				SuperPodSize:   superPodSize10,
				ReservePodSize: reservePodSize2,
			}}},
		spBlock: spBlockNum2,
		want:    map[int32]struct{}{1: {}, superPodId2: {}},
		wantErr: nil,
	},
	{
		name:  "05-2*2 job will select 5 and 8 from super pods with node 3, 5 or 8",
		tasks: newNPUTasks(npuTaskNum4),
		nodes: []*api.NodeInfo{node0, node1, node2, node10, node11, node12, node13, node14, node20,
			node21, node22, node23, node24, node25, node26, node27,
		},
		npuTaskNum: npuTaskNum4,
		frameAttr: plugin.VolcanoFrame{
			ConfigParameters: plugin.ConfigParameters{DynamicParameters: plugin.DynamicParameters{
				SuperPodSize:   superPodSize10,
				ReservePodSize: reservePodSize2,
			}}},
		spBlock: spBlockNum2,
		want:    map[int32]struct{}{1: {}, superPodId2: {}},
		wantErr: nil,
	},
	{
		name:  "06-2*2 job will select 3 and 5 from super pods with node 3, 5",
		tasks: newNPUTasks(npuTaskNum4),
		nodes: []*api.NodeInfo{node0, node1, node2, node10, node11, node12, node13, node14},
		frameAttr: plugin.VolcanoFrame{
			ConfigParameters: plugin.ConfigParameters{DynamicParameters: plugin.DynamicParameters{
				SuperPodSize:   superPodSize10,
				ReservePodSize: reservePodSize2,
			}}},
		npuTaskNum: npuTaskNum4,
		spBlock:    spBlockNum2,
		want:       map[int32]struct{}{1: {}, 0: {}},
		wantErr:    nil,
	},

	{
		name:  "07-2*2 job will select 5 and 5 from super pods with node 5, 5",
		tasks: newNPUTasks(npuTaskNum4),
		nodes: []*api.NodeInfo{node0, node1, node2, node3, node4, node10, node11, node12, node13, node14},
		frameAttr: plugin.VolcanoFrame{
			ConfigParameters: plugin.ConfigParameters{DynamicParameters: plugin.DynamicParameters{
				SuperPodSize:   superPodSize10,
				ReservePodSize: reservePodSize2,
			}}},
		npuTaskNum: npuTaskNum4,
		spBlock:    spBlockNum2,
		want:       map[int32]struct{}{1: {}, 0: {}},
		wantErr:    nil,
	},
	{
		name:  "08-2*2 job will select 3 from super pods with node 1, 3",
		tasks: newNPUTasks(npuTaskNum1),
		nodes: []*api.NodeInfo{node0, node10, node11, node12},
		frameAttr: plugin.VolcanoFrame{
			ConfigParameters: plugin.ConfigParameters{DynamicParameters: plugin.DynamicParameters{
				SuperPodSize:   superPodSize10,
				ReservePodSize: reservePodSize4,
			}},
		},
		npuTaskNum: npuTaskNum1,
		spBlock:    spBlockNum1,
		want:       map[int32]struct{}{1: {}},
		wantErr:    nil,
	},
}

func TestSelectSuperPodForJob(t *testing.T) {
	plg, _ := New(SchedulerName).(*module910SuperPod)
	plg.Name = "job1"
	plg.SchedulerJobAttr = util.SchedulerJobAttr{
		ComJob: util.ComJob{},
		NPUJob: &util.NPUJob{},
	}
	plg.ScheduleEnv = plugin.ScheduleEnv{}
	task := &api.TaskInfo{
		UID:  "0",
		Job:  "job1",
		Name: "task0",
	}
	const npuNodes = 30
	plg.Nodes = newNPUNodes(npuNodes, superPodSize10)
	scoreMap := make(map[string]float64, 0)
	for _, cs := range selectSuperPodForJobTestCases {
		t.Run(cs.name, func(t *testing.T) {
			plg.spBlock = cs.spBlock
			plg.Tasks = cs.tasks
			plg.FrameAttr = cs.frameAttr
			plg.NPUTaskNum = cs.npuTaskNum
			selectedNodes, err := plg.selectSuperPodForJob(task, cs.nodes, scoreMap)
			if !reflect.DeepEqual(err, cs.wantErr) {
				t.Errorf("InitMyJobPlugin() error = %v, wantErr %v", err, cs.wantErr)
			}
			if !reflect.DeepEqual(getSelectedNodesSuperPodID(selectedNodes), cs.want) {
				t.Errorf("InitMyJobPlugin() selectedNodes = %v, want: %v", selectedNodes, cs.want)
			}
		})

	}
}

func getSelectedNodesSuperPodID(selectedNodes map[string][]plugin.SuperNode) map[int32]struct{} {
	if selectedNodes == nil {
		return nil
	}
	selectedNodesID := make(map[int32]struct{}, 0)
	for _, sp := range selectedNodes {
		for _, node := range sp {
			selectedNodesID[node.SuperPodID] = struct{}{}
		}
	}
	return selectedNodesID
}

type validNPUJobTest struct {
	name          string
	spBlockNPUNum int
	superPodSize  int
	reqNPUNum     int
	taskNum       int
	wantPass      bool
}

func buildValidNPUJobTestCases() []validNPUJobTest {
	return []validNPUJobTest{
		{
			name:          "01 will return false when spBlockNPUNum is 0",
			spBlockNPUNum: 0,
			wantPass:      false,
		},
		{
			name:          "02 will return false when spBlockNPUNum is 1 but superPodSize is 0 ",
			spBlockNPUNum: util.NPUIndex1,
			wantPass:      false,
		},
		{
			name:          "03 will return false when spBlockNPUNum is not mutiple of node npu",
			spBlockNPUNum: util.CoreNum25,
			wantPass:      false,
		},
		{
			name:          "04 will return false when require npu num is 1 and sp-block is not 1",
			reqNPUNum:     util.NPUIndex1,
			spBlockNPUNum: util.NPUIndex2,
			superPodSize:  util.NPUIndex1,
			wantPass:      false,
		},
		{
			name:          "05 will return false when 1 task job require npu num is 1 and sp-block is not 1",
			taskNum:       util.NPUIndex1,
			reqNPUNum:     util.NPUIndex1,
			spBlockNPUNum: util.NPUIndex2,
			superPodSize:  util.NPUIndex1,
			wantPass:      false,
		},
		{
			name:          "06 will return false when job require npu num is 3 not mutiple of die npu",
			taskNum:       util.NPUIndex1,
			reqNPUNum:     util.NPUIndex3,
			spBlockNPUNum: util.NPUIndex1,
			superPodSize:  util.NPUIndex1,
			wantPass:      false,
		},
		{
			name:          "07 will return true when job require npu num is 1 and task num is 1",
			taskNum:       util.NPUIndex1,
			reqNPUNum:     util.NPUIndex1,
			spBlockNPUNum: util.NPUIndex1,
			superPodSize:  util.NPUIndex1,
			wantPass:      true,
		},
	}
}

func TestValidNPUJob(t *testing.T) {
	for _, tt := range buildValidNPUJobTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			tp := &module910SuperPod{}
			tp.NPUJob = &util.NPUJob{}
			tp.MaxNodeNPUNum = ascend910a3.NodeNPUNumber
			tp.MaxCardNPUNum = ascend910a3.DieNPUNumber
			tp.SpBlockNPUNum = tt.spBlockNPUNum
			tp.ReqNPUNum = tt.reqNPUNum
			tp.NPUTaskNum = tt.taskNum
			tp.FrameAttr.SuperPodSize = tt.superPodSize
			if got := tp.ValidNPUJob(); got != nil && !reflect.DeepEqual(got.Pass, tt.wantPass) {
				t.Errorf("ValidNPUJob() = %v, want %v", got.Pass, tt.wantPass)
			}
		})
	}
}

type CheckNodeNPUByTaskTest struct {
	name    string
	node    plugin.NPUNode
	task    *api.TaskInfo
	setup   func() *gomonkey.Patches
	wantErr bool
}

func buildCheckNodeNPUByTaskTestCases01() CheckNodeNPUByTaskTest {
	return CheckNodeNPUByTaskTest{
		name:    "01 will return err when task is nil",
		node:    plugin.NPUNode{},
		wantErr: true,
	}
}

func buildCheckNodeNPUByTaskTestCases02() CheckNodeNPUByTaskTest {
	node := plugin.NPUNode{}
	node.SuperPodID = util.ErrorInt
	node.Annotation = map[string]string{"test": "test"}
	return CheckNodeNPUByTaskTest{
		name:    "02 will return err when node SuperPodID is -1",
		node:    node,
		task:    &api.TaskInfo{},
		wantErr: true,
	}
}

func buildCheckNodeNPUByTaskTestCases03() CheckNodeNPUByTaskTest {
	node := plugin.NPUNode{}
	node.Annotation = map[string]string{"test": "test"}
	return CheckNodeNPUByTaskTest{
		name:    "03 will return err when GetTaskReqNPUNum return err",
		node:    node,
		task:    &api.TaskInfo{},
		wantErr: true,
	}
}

func buildCheckNodeNPUByTaskTestCases04() CheckNodeNPUByTaskTest {
	node := plugin.NPUNode{}
	node.Annotation = map[string]string{"test": "test"}
	return CheckNodeNPUByTaskTest{
		name: "04 will return err when GetUsableTopFromNode return err",
		node: node,
		task: &api.TaskInfo{},
		setup: func() *gomonkey.Patches {
			return gomonkey.ApplyMethod(reflect.TypeOf(&base.NPUHandler{}), "GetTaskReqNPUNum",
				func(_ *base.NPUHandler, _ *api.TaskInfo) (int, error) { return util.NPUIndex8, nil })
		},
		wantErr: true,
	}
}

func buildCheckNodeNPUByTaskTestCases05() CheckNodeNPUByTaskTest {
	node := plugin.NPUNode{}
	node.Annotation = map[string]string{"test": "test"}
	return CheckNodeNPUByTaskTest{
		name: "05 will return err when JudgeNodeAndTaskNPU return err",
		node: node,
		task: &api.TaskInfo{},
		setup: func() *gomonkey.Patches {
			patches := gomonkey.NewPatches()
			patches.ApplyMethod(reflect.TypeOf(&base.NPUHandler{}), "GetTaskReqNPUNum",
				func(_ *base.NPUHandler, _ *api.TaskInfo) (int, error) { return util.NPUIndex8, nil })
			patches.ApplyMethod(reflect.TypeOf(&base.NPUHandler{}), "GetUsableTopFromNode",
				func(_ *base.NPUHandler, node plugin.NPUNode, disFlag bool) ([]int, error) { return nil, nil })
			return patches
		},
		wantErr: true,
	}
}
func buildCheckNodeNPUByTaskTestCases06() CheckNodeNPUByTaskTest {
	node := plugin.NPUNode{}
	node.Annotation = map[string]string{"test": "test"}
	return CheckNodeNPUByTaskTest{
		name: "06 will return nil when node topo meet job require",
		node: node,
		task: &api.TaskInfo{},
		setup: func() *gomonkey.Patches {
			patches := gomonkey.NewPatches()
			patches.ApplyMethod(reflect.TypeOf(&base.NPUHandler{}), "GetTaskReqNPUNum",
				func(_ *base.NPUHandler, _ *api.TaskInfo) (int, error) { return util.NPUIndex2, nil })
			patches.ApplyMethod(reflect.TypeOf(&base.NPUHandler{}), "GetUsableTopFromNode",
				func(_ *base.NPUHandler, node plugin.NPUNode, disFlag bool) ([]int, error) {
					return []int{util.NPUIndex1, util.NPUIndex0}, nil
				})
			return patches
		},
		wantErr: false,
	}
}

func buildCheckNodeNPUByTaskTestCases() []CheckNodeNPUByTaskTest {
	return []CheckNodeNPUByTaskTest{
		buildCheckNodeNPUByTaskTestCases01(),
		buildCheckNodeNPUByTaskTestCases02(),
		buildCheckNodeNPUByTaskTestCases03(),
		buildCheckNodeNPUByTaskTestCases04(),
		buildCheckNodeNPUByTaskTestCases05(),
		buildCheckNodeNPUByTaskTestCases06(),
	}
}

func TestCheckNodeNPUByTask(t *testing.T) {
	for _, tt := range buildCheckNodeNPUByTaskTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				patches := tt.setup()
				defer patches.Reset()
			}
			tp := &module910SuperPod{spBlock: spBlockNum2}
			tp.NPUJob = &util.NPUJob{}
			tp.SetMaxNodeNPUNum(util.NPUIndex8)
			tp.SetMaxCardNPUNum(util.NPUIndex2)
			if err := tp.CheckNodeNPUByTask(tt.task, tt.node); (err != nil) != tt.wantErr {
				t.Errorf("CheckNodeNPUByTask() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type ScoreBestNPUNodesTest struct {
	name    string
	nodes   []*api.NodeInfo
	spBlock int
	env     plugin.ScheduleEnv
	taskNum int
	wantErr bool
}

func buildScoreBestNPUNodesTest() []ScoreBestNPUNodesTest {
	ssn := test.FakeNormalSSN(nil)
	return []ScoreBestNPUNodesTest{
		{
			name:    "01 will return err when node list is nil",
			nodes:   nil,
			env:     *test2.FakeScheduleEnv(),
			wantErr: true,
		},
		{
			name:    "02 will return nil when job has 3 task and sp block is 1 select node success",
			nodes:   ssn.NodeList,
			env:     *test2.FakeScheduleEnv(),
			spBlock: util.NPUIndex1,
			taskNum: util.NPUIndex3,
			wantErr: false,
		},
		{
			name:    "03 will return nil when job has 3 task and sp block is 16 select node success",
			nodes:   ssn.NodeList,
			env:     *test2.FakeScheduleEnv(),
			spBlock: util.NPUIndex16,
			taskNum: util.NPUIndex3,
			wantErr: false,
		},
		{
			name:    "04 will return nil when job has 1 task and sp block is 16 select node success",
			nodes:   ssn.NodeList,
			env:     *test2.FakeScheduleEnv(),
			spBlock: util.NPUIndex16,
			taskNum: util.NPUIndex1,
			wantErr: false,
		},
	}
}

func TestScoreBestNPUNodes(t *testing.T) {
	tTask := test.FakeNormalTestTasks(1)[0]
	scoreMap := map[string]float64{}
	patch := gomonkey.ApplyFunc(rescheduling.GetReSchedulerCache, func() *rescheduling.DealReSchedulerCache {
		return &rescheduling.DealReSchedulerCache{
			FaultJobs: map[api.JobID]*rescheduling.FaultJob{"vcjob/pg0": {
				FaultTasks: []rescheduling.FaultTask{{NodeName: "node0", IsFaultTask: true}},
				JobUID:     "vcjob/pg0", IsFaultJob: true,
				SuperPods: map[string][]plugin.SuperNode{"0": {{"node0", 0}, {"node1", 0}}}}},
		}
	})
	defer patch.Reset()
	for _, tt := range buildScoreBestNPUNodesTest() {
		t.Run(tt.name, func(t *testing.T) {
			initScoreMap(scoreMap, tt.nodes)
			tp := &module910SuperPod{
				nodeVPodId: map[string]string{},
				spBlock:    tt.spBlock,
			}
			if err := tp.InitMyJobPlugin(tt.env.Jobs["vcjob/pg0"].SchedulerJobAttr, tt.env); err != nil {
				return
			}
			tp.Label[util.SinglePodTag] = util.EnableFunc
			tp.NPUTaskNum = tt.taskNum
			if err := tp.ScoreBestNPUNodes(tTask, tt.nodes, scoreMap); (err != nil) != tt.wantErr {
				t.Errorf("ScoreBestNPUNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func initScoreMap(sMap map[string]float64, nodes []*api.NodeInfo) {
	for _, node := range nodes {
		sMap[node.Name] = 0
	}
}

type getSelectNodesTest struct {
	name         string
	fNodeNameMap map[string]struct{}
	spNodes      []plugin.SuperNode
	spNodeMaps   map[string]plugin.NPUNode
	want         []plugin.SuperNode
}

func buildGetSelectNodesTest() []getSelectNodesTest {
	return []getSelectNodesTest{
		{
			name: "01 will return nil when spNodeMaps is nil",
			want: nil,
		},
		{
			name:         "02 test node is not exist in fNodeNameMap",
			spNodes:      []plugin.SuperNode{{"node0", 0}},
			fNodeNameMap: nil,
			spNodeMaps:   map[string]plugin.NPUNode{"node0": {}},
			want:         []plugin.SuperNode{{"node0", 0}},
		},
		{
			name:         "03 test node is exist in fNodeNameMap",
			spNodes:      []plugin.SuperNode{{"node0", 0}},
			fNodeNameMap: map[string]struct{}{"node0": {}},
			spNodeMaps:   map[string]plugin.NPUNode{"node0": {}},
			want:         []plugin.SuperNode{{"", 0}},
		},
	}
}

func TestGetSelectNodes(t *testing.T) {
	for _, tt := range buildGetSelectNodesTest() {
		t.Run(tt.name, func(t *testing.T) {
			if got := getSelectNodes(tt.fNodeNameMap, tt.spNodes, tt.spNodeMaps); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getSelectNodes() = %v, want %v", got, tt.want)
			}
		})
	}
}

type getSuperPodRanksTest struct {
	name     string
	job      plugin.SchedulerJob
	rank     int
	wantSpID string
	wantLR   int
	wantErr  bool
}

func buildGetSuperPodRanksTest() []getSuperPodRanksTest {
	return []getSuperPodRanksTest{
		{
			name: "01 normal case - first pod in first superpod",
			job: plugin.SchedulerJob{SuperPods: map[string][]plugin.SuperNode{
				"0": {{Name: "node0"}, {Name: "node1"}}, "1": {{Name: "node2"}, {Name: "node3"}}}},
			rank:     0,
			wantSpID: "0",
			wantLR:   0,
		},
		{
			name: "02 normal case - last pod in first superpod",
			job: plugin.SchedulerJob{SuperPods: map[string][]plugin.SuperNode{
				"0": {{Name: "node0"}, {Name: "node1"}}, "1": {{Name: "node2"}, {Name: "node3"}}}},
			rank:     util.NPUIndex1,
			wantSpID: "0",
			wantLR:   util.NPUIndex1,
		},
		{
			name: "03 normal case - first pod in second superpod",
			job: plugin.SchedulerJob{
				SuperPods: map[string][]plugin.SuperNode{
					"0": {{Name: "node0"}, {Name: "node1"}}, "1": {{Name: "node2"}, {Name: "node3"}}}},
			rank:     util.NPUIndex2,
			wantSpID: "1",
			wantLR:   0,
		},
		{
			name: "04 edge case - rank exceeds total nodes",
			job: plugin.SchedulerJob{SuperPods: map[string][]plugin.SuperNode{
				"0": {{Name: "node0"}, {Name: "node1"}}, "1": {{Name: "node2"}, {Name: "node3"}}}},
			rank:    util.NPUIndex4,
			wantErr: true,
		},
		{
			name:    "05 edge case - empty superpods",
			job:     plugin.SchedulerJob{SuperPods: map[string][]plugin.SuperNode{}},
			rank:    0,
			wantErr: true,
		},
		{
			name:    "06 edge case - invalid superpod key",
			job:     plugin.SchedulerJob{SuperPods: map[string][]plugin.SuperNode{"invalid": {{Name: "node0"}}}},
			rank:    0,
			wantErr: true,
		},
	}
}

func TestGetSuperPodRanks(t *testing.T) {
	for _, tt := range buildGetSuperPodRanksTest() {
		t.Run(tt.name, func(t *testing.T) {
			gotSpID, gotLR := getSuperPodRanks(tt.job, tt.rank)
			if (gotSpID == "" && gotLR == 0) != tt.wantErr {
				t.Errorf("getSuperPodRanks() error = %v, wantErr %v", (gotSpID == "" && gotLR == 0), tt.wantErr)
				return
			}
			if gotSpID != tt.wantSpID {
				t.Errorf("getSuperPodRanks() gotSpID = %v, want %v", gotSpID, tt.wantSpID)
			}
			if gotLR != tt.wantLR {
				t.Errorf("getSuperPodRanks() gotLR = %v, want %v", gotLR, tt.wantLR)
			}
		})
	}
}

func TestGetVPodID(t *testing.T) {
	tests := []struct {
		name      string
		recorder  *vPodIdRecorder
		want      string
		wantRight int
	}{
		{
			name:      "01 normal case - get from unReadyId",
			recorder:  &vPodIdRecorder{unReadyId: []string{"pod1", "pod2"}, leftIndex: 1, rightIndex: 0},
			want:      "pod2",
			wantRight: 0,
		},
		{
			name:      "02 edge case - leftIndex out of range",
			recorder:  &vPodIdRecorder{unReadyId: []string{"pod1"}, leftIndex: util.NPUIndex2, rightIndex: 0},
			want:      "",
			wantRight: 0,
		},
		{
			name:      "03 edge case - leftIndex negative",
			recorder:  &vPodIdRecorder{unReadyId: []string{}, leftIndex: -1, rightIndex: 0},
			want:      "0",
			wantRight: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.recorder.getVPodID()
			if got != tt.want {
				t.Errorf("getVPodID() = %v, want %v", got, tt.want)
			}
			if tt.recorder.rightIndex != tt.wantRight {
				t.Errorf("rightIndex = %v, want %v", tt.recorder.rightIndex, tt.wantRight)
			}
		})
	}
}

type selectNodesForSoftStrategyTest struct {
	name        string
	recorder    *vPodIdRecorder
	totalNode   int
	superPods   []superPod
	selectNodes map[string][]plugin.SuperNode
	wantTotal   int
	wantLen     int
}

func buildSelectNodesForSoftStrategyTestCases() []selectNodesForSoftStrategyTest {
	return []selectNodesForSoftStrategyTest{
		{
			name: "01 normal case - select nodes from non-empty superPods",
			recorder: &vPodIdRecorder{
				unReadyId: []string{"pod1"},
				leftIndex: 0,
			},
			totalNode: util.NPUIndex2,
			superPods: []superPod{{"node1": {CommonNode: plugin.CommonNode{Name: "node1"}},
				"node2": {CommonNode: plugin.CommonNode{Name: "node2"}}},
				{"node3": {CommonNode: plugin.CommonNode{Name: "node3"}}},
			},
			selectNodes: make(map[string][]plugin.SuperNode),
			wantTotal:   0,
			wantLen:     util.NPUIndex2,
		},
		{
			name: "02 edge case - empty superPods",
			recorder: &vPodIdRecorder{
				unReadyId: []string{"pod1"},
				leftIndex: 0,
			},
			totalNode:   util.NPUIndex2,
			superPods:   []superPod{},
			selectNodes: make(map[string][]plugin.SuperNode),
			wantTotal:   util.NPUIndex2,
			wantLen:     0,
		},
		{
			name: "03 edge case - zero totalNode",
			recorder: &vPodIdRecorder{
				unReadyId: []string{"pod1"},
				leftIndex: 0,
			},
			totalNode: 0,
			superPods: []superPod{
				{"node1": {CommonNode: plugin.CommonNode{Name: "node1"}}},
			},
			selectNodes: make(map[string][]plugin.SuperNode),
			wantTotal:   0,
			wantLen:     1,
		},
	}
}

func TestSelectNodesForSoftStrategy(t *testing.T) {
	for _, tt := range buildSelectNodesForSoftStrategyTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			tp := &module910SuperPod{spBlock: 1}
			got := tp.selectNodesForSoftStrategy(tt.recorder, &tt.totalNode, tt.superPods, tt.selectNodes)
			if tt.totalNode != tt.wantTotal {
				t.Errorf("selectNodesForSoftStrategy() totalNode = %v, want %v", tt.totalNode, tt.wantTotal)
			}
			if len(got) != tt.wantLen {
				t.Errorf("selectNodesForSoftStrategy() len = %v, want %v", len(got), tt.wantLen)
			}
		})
	}
}

type isDelayingJobTest struct {
	name     string
	fJob     *rescheduling.FaultJob
	nodes    []*api.NodeInfo
	expected bool
}

func TestIsDelayingJob(t *testing.T) {
	now := time.Now().Unix()
	tests := []isDelayingJobTest{
		{
			name: "01 timeout case - should skip wait",
			fJob: &rescheduling.FaultJob{JobName: "test-job", RescheduleTime: now - util.NPUIndex11,
				FaultTasks: []rescheduling.FaultTask{{IsFaultTask: false, NodeName: "node1"}}},
			nodes:    []*api.NodeInfo{{Name: "node1"}},
			expected: true,
		},
		{
			name: "02 normal case - node released",
			fJob: &rescheduling.FaultJob{JobName: "test-job", RescheduleTime: now - util.NPUIndex5,
				FaultTasks: []rescheduling.FaultTask{{IsFaultTask: false, NodeName: "node1"}}},
			nodes:    []*api.NodeInfo{{Name: "node1"}},
			expected: true,
		},
		{
			name: "03 normal case - node not released",
			fJob: &rescheduling.FaultJob{JobName: "test-job", RescheduleTime: now - util.NPUIndex5,
				FaultTasks: []rescheduling.FaultTask{{IsFaultTask: false, NodeName: "node2"}}},
			nodes:    []*api.NodeInfo{{Name: "node1"}},
			expected: false,
		},
		{
			name: "04 edge case - no fault tasks",
			fJob: &rescheduling.FaultJob{JobName: "test-job", RescheduleTime: now - util.NPUIndex5,
				FaultTasks: []rescheduling.FaultTask{}},
			nodes:    []*api.NodeInfo{{Name: "node1"}},
			expected: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tp := &module910SuperPod{}
			result := tp.isDelayingJob(tt.fJob, tt.nodes)
			if result != tt.expected {
				t.Errorf("isDelayingJob() = %v, want %v", result, tt.expected)
			}
		})
	}
}
