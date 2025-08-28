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
	"reflect"
	"strings"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

func deleteNodeByNodeName(nodes []*api.NodeInfo, nodeName string) []*api.NodeInfo {
	tmpNodes := make([]*api.NodeInfo, 0)
	for _, node := range nodes {
		if node.Name == nodeName {
			continue
		}
		tmpNodes = append(tmpNodes, node)
	}
	return tmpNodes
}

func buildScoreBestNPUNodesV201() scoreBestNPUNodesTest {
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex12)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, NormalSchema)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	for _, task := range fakeJob.Tasks {
		task.NodeName = ""
	}
	return scoreBestNPUNodesTest{
		name:       "01-ScoreBestNPUNodes nslb 2.0 test ,use 2 physic tor and 2 shared tor",
		ssn:        ssn,
		torHandler: newTestTorHandler(ssn),
		wantErr:    false,
	}
}

func buildScoreBestNPUNodesV202() scoreBestNPUNodesTest {
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex2)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, LargeModelTag)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	for _, task := range fakeJob.Tasks {
		task.NodeName = ""
	}
	return scoreBestNPUNodesTest{
		name:       "02-ScoreBestNPUNodes nslb 2.0 test, fill job use shared tor",
		ssn:        ssn,
		torHandler: newTestTorHandler(ssn),
		wantErr:    false,
	}
}

func buildScoreBestNPUNodesV203() scoreBestNPUNodesTest {
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex15)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, NormalSchema)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	for _, task := range fakeJob.Tasks {
		task.NodeName = ""
	}
	return scoreBestNPUNodesTest{
		name:       "03-ScoreBestNPUNodes nslb 2.0 test, tor node num is not enough for normal job",
		ssn:        ssn,
		torHandler: newTestTorHandler(ssn),
		wantErr:    true,
	}
}

func buildScoreBestNPUNodesV204() scoreBestNPUNodesTest {
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
	return scoreBestNPUNodesTest{
		name:       "04-ScoreBestNPUNodes nslb 2.0 test, score node for reschedule job when used tor node is enough",
		ssn:        ssn,
		torHandler: newTestTorHandler(ssn),
		wantErr:    false,
	}
}

func buildScoreBestNPUNodesV205() scoreBestNPUNodesTest {
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
	return scoreBestNPUNodesTest{
		name:       "05-ScoreBestNPUNodes nslb 2.0 test, score node for reschedule job when use new tor",
		ssn:        ssn,
		torHandler: newTestTorHandler(ssn),
		wantErr:    false,
	}
}

func buildScoreBestNPUNodesV206() scoreBestNPUNodesTest {
	return scoreBestNPUNodesTest{
		name:       "06-ScoreBestNPUNodes will return nil when ssn is nil",
		ssn:        &framework.Session{},
		torHandler: TorHandler{},
		wantErr:    true,
	}
}

func buildScoreBestNPUNodesV207() scoreBestNPUNodesTest {
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex13)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, NormalSchema)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	for _, task := range fakeJob.Tasks {
		task.NodeName = ""
	}
	return scoreBestNPUNodesTest{
		name:       "07-ScoreBestNPUNodes nslb 2.0 test, tor node num is enough for normal job",
		ssn:        ssn,
		torHandler: newTestTorHandler(ssn),
		wantErr:    false,
	}
}

func buildScoreBestNPUNodesV2TestCases() []scoreBestNPUNodesTest {
	return []scoreBestNPUNodesTest{
		buildScoreBestNPUNodesV201(),
		buildScoreBestNPUNodesV202(),
		buildScoreBestNPUNodesV203(),
		buildScoreBestNPUNodesV204(),
		buildScoreBestNPUNodesV205(),
		buildScoreBestNPUNodesV206(),
		buildScoreBestNPUNodesV207(),
	}
}

func TestTorHandlerV2ScoreBestNPUNodes(t *testing.T) {
	tTask := test.FakeNormalTestTasks(1)[0]
	scoreMap := map[string]float64{}
	for _, tt := range buildScoreBestNPUNodesV2TestCases() {
		t.Run(tt.name, func(t *testing.T) {
			torHandlerV2 := TorHandlerV2{TorHandler: tt.torHandler}
			torHandlerV2.PreStartAction(nil)
			if strings.Contains(tt.name, "05-ScoreBestNPUNodes") {
				tt.ssn.NodeList = tt.ssn.NodeList[util.NPUIndex8:]
			}
			if err := torHandlerV2.ScoreBestNPUNodes(tTask, tt.ssn.NodeList, scoreMap); (err != nil) != tt.wantErr {
				t.Errorf("ScoreBestNPUNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type torHandlerUseAnnotationTest struct {
	name     string
	th       *TorHandlerV2
	node     plugin.NPUNode
	wantNode *plugin.NPUNode
}

func buildTorHandlerUseAnnotationTest() []torHandlerUseAnnotationTest {
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex2)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, LargeModelTag)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	torHandlerV2 := &TorHandlerV2{TorHandler: newTestTorHandler(ssn)}
	torHandlerV2.PreStartAction(nil)
	return []torHandlerUseAnnotationTest{
		{
			name:     "01 will return err when th is nil",
			th:       nil,
			node:     plugin.NPUNode{},
			wantNode: nil,
		},
		{
			name:     "02 will return nil when annotation set success",
			th:       &TorHandlerV2{TorHandler: newTestTorHandler(ssn)},
			node:     plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "node0"}},
			wantNode: &plugin.NPUNode{CommonNode: plugin.CommonNode{Name: "node0"}},
		},
	}
}

func TestTorHandlerV2UseAnnotation(t *testing.T) {
	tTask := test.FakeNormalTestTasks(1)[0]
	for _, tt := range buildTorHandlerUseAnnotationTest() {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.th.UseAnnotation(tTask, tt.node); !reflect.DeepEqual(got, tt.wantNode) {
				t.Errorf("UseAnnotation() = %v, want %v", got, tt.wantNode)
			}
		})
	}
}
