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
Package nslb is using for HuaWei Ascend pin tor affinity.
*/
package nslb

import (
	"testing"

	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	test2 "volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/test"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

func newTestTorHandler(ssn *framework.Session) TorHandler {
	handle := test2.NewDefaultHandler()
	patch1 := test2.PatchGetCm(TorNodeCMName, "kube-system", test2.FakeTorNodeData())
	defer patch1.Reset()
	test2.InitNormalsHandlerBySsnFunc(ssn, handle.InitVolcanoFrameFromSsn, handle.InitNodesFromSsn,
		handle.InitJobsFromSsn, handle.InitTorNodeInfo)
	torHandler := TorHandler{}
	torHandler.globalTorEnv = handle.Tors
	job := handle.Jobs["vcjob/pg0"]
	torHandler.Job = &job
	return torHandler
}

type scoreBestNPUNodesTest struct {
	name       string
	torHandler TorHandler
	ssn        *framework.Session
	wantErr    bool
}

func buildScoreBestNPUNodes01() scoreBestNPUNodesTest {
	ssn := test.FakeNormalSSN(nil)
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex13)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, NormalSchema)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	for _, task := range fakeJob.Tasks {
		task.NodeName = ""
	}
	return scoreBestNPUNodesTest{
		name:       "01-ScoreBestNPUNodes nslb 1.0 test full tor is not enough, use logic tor",
		ssn:        ssn,
		torHandler: newTestTorHandler(ssn),
		wantErr:    false,
	}
}

func buildScoreBestNPUNodes02() scoreBestNPUNodesTest {
	ssn := test.FakeNormalSSN(nil)
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex12)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, NormalSchema)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	for _, task := range fakeJob.Tasks {
		task.NodeName = ""
	}
	return scoreBestNPUNodesTest{
		name:       "02-ScoreBestNPUNodes nslb 1.0 test full tor is enough, use physic tor",
		ssn:        ssn,
		torHandler: newTestTorHandler(ssn),
		wantErr:    false,
	}
}

func buildScoreBestNPUNodes03() scoreBestNPUNodesTest {
	ssn := test.FakeNormalSSN(nil)
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex2)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, LargeModelTag)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	for _, task := range fakeJob.Tasks {
		task.NodeName = ""
	}
	return scoreBestNPUNodesTest{
		name:       "03-ScoreBestNPUNodes nslb 1.0 test, score node for fill job",
		ssn:        ssn,
		torHandler: newTestTorHandler(ssn),
		wantErr:    false,
	}
}

func buildScoreBestNPUNodes04() scoreBestNPUNodesTest {
	ssn := test.FakeNormalSSN(nil)
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex15)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, NormalSchema)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	for _, task := range fakeJob.Tasks {
		task.NodeName = ""
	}
	return scoreBestNPUNodesTest{
		name:       "04-ScoreBestNPUNodes nslb 1.0 test full tor check filed by not enough logic tor",
		ssn:        ssn,
		torHandler: newTestTorHandler(ssn),
		wantErr:    true,
	}
}

func buildScoreBestNPUNodes05() scoreBestNPUNodesTest {
	return scoreBestNPUNodesTest{
		name:       "05-ScoreBestNPUNodes will return err when node is nil",
		ssn:        &framework.Session{},
		torHandler: TorHandler{},
		wantErr:    true,
	}
}

func buildScoreBestNPUNodesTestCases() []scoreBestNPUNodesTest {
	return []scoreBestNPUNodesTest{
		buildScoreBestNPUNodes01(),
		buildScoreBestNPUNodes02(),
		buildScoreBestNPUNodes03(),
		buildScoreBestNPUNodes04(),
		buildScoreBestNPUNodes05(),
	}
}

func TestTorHandlerV1ScoreBestNPUNodes(t *testing.T) {
	tTask := test.FakeNormalTestTasks(1)[0]
	scoreMap := map[string]float64{}
	for _, tt := range buildScoreBestNPUNodesTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			torHandlerV1 := TorHandlerV1{TorHandler: tt.torHandler}
			if err := torHandlerV1.ScoreBestNPUNodes(tTask, tt.ssn.NodeList, scoreMap); (err != nil) != tt.wantErr {
				t.Errorf("ScoreBestNPUNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type CheckNodeNPUByTaskV1Test struct {
	name    string
	node    plugin.NPUNode
	th      *TorHandlerV1
	wantErr bool
}

func buildCheckNodeNPUByTaskV1Test() []CheckNodeNPUByTaskV1Test {
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex8)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, NormalSchema)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	for _, task := range fakeJob.Tasks {
		if task.Name == "pod0" {
			task.NodeName = ""
			break
		}
	}
	return []CheckNodeNPUByTaskV1Test{
		{
			name:    "01 will return err  when th is nil",
			node:    plugin.NPUNode{},
			th:      nil,
			wantErr: true,
		},
		{
			name:    "02 will return nil  when job is pod scheduling",
			node:    plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "node0"}},
			th:      &TorHandlerV1{TorHandler: newTestTorHandler(ssn)},
			wantErr: false,
		},
	}
}

func TestTorHandlerV1CheckNodeNPUByTask(t *testing.T) {
	tTask := test.FakeNormalTestTasks(1)[0]
	for _, tt := range buildCheckNodeNPUByTaskV1Test() {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.th.CheckNodeNPUByTask(tTask, tt.node); (err != nil) != tt.wantErr {
				t.Errorf("CheckNodeNPUByTask() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type preStartActionTestCase struct {
	name    string
	th      *TorHandlerV1
	wantErr bool
}

func buildPreStartActionTestCase() []preStartActionTestCase {
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex8)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, NormalSchema)
	for _, task := range fakeJob.Tasks {
		if task.Name == "pod0" {
			task.NodeName = ""
			break
		}
	}
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	return []preStartActionTestCase{
		{
			name:    "01 will return err when job is nil",
			th:      &TorHandlerV1{},
			wantErr: true,
		},
		{
			name:    "02 will return err when global tor env is nil",
			th:      &TorHandlerV1{TorHandler: TorHandler{Job: &plugin.SchedulerJob{}}},
			wantErr: true,
		},
		{
			name:    "03 pre start success",
			th:      &TorHandlerV1{TorHandler: newTestTorHandler(ssn)},
			wantErr: false,
		},
		{
			name: "04 pre will return nil when scheduling task is 0",
			th: &TorHandlerV1{TorHandler: TorHandler{Job: &plugin.SchedulerJob{SchedulerJobAttr: util.SchedulerJobAttr{
				NPUJob: &util.NPUJob{}}}, globalTorEnv: &plugin.TorList{}}},
			wantErr: false,
		},
	}
}

func TestPreStartAction(t *testing.T) {
	for _, tt := range buildPreStartActionTestCase() {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.th.PreStartAction(nil); (err != nil) != tt.wantErr {
				t.Errorf("PreStartAction() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
