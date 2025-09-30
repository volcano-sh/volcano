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

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

func buildSingleLevelScoreBestNPUNodes01() []scoreBestNPUNodesTest {
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex2)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, NormalSchema)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	for _, task := range fakeJob.Tasks {
		task.NodeName = ""
	}
	return []scoreBestNPUNodesTest{
		{
			name:       "01-ScoreBestNPUNodes nslb test, score node for single single_layer job",
			ssn:        ssn,
			torHandler: newTestTorHandler(ssn),
			wantErr:    false,
		},
		{
			name:       "02-ScoreBestNPUNodes will return err when node is nil",
			ssn:        &framework.Session{},
			torHandler: TorHandler{},
			wantErr:    true,
		},
	}
}

func TestTorSingleLevelHandlerScoreBestNPUNodes(t *testing.T) {
	tTask := test.FakeNormalTestTasks(1)[0]
	scoreMap := map[string]float64{}
	for _, tt := range buildSingleLevelScoreBestNPUNodes01() {
		t.Run(tt.name, func(t *testing.T) {
			th := &TorSingleLevelHandler{
				TorHandler: tt.torHandler,
			}
			if err := th.ScoreBestNPUNodes(tTask, tt.ssn.NodeList, scoreMap); (err != nil) != tt.wantErr {
				t.Errorf("ScoreBestNPUNodes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type testSetSingleLayerTorJobNodesScore struct {
	name    string
	th      *TorSingleLevelHandler
	wantErr bool
}

func buildTestSetSingleLayerTorJobNodesScore() []testSetSingleLayerTorJobNodesScore {
	falseTag := false
	trueTag := true
	fakeJob := plugin.SchedulerJob{JobReadyTag: &falseTag}
	fakeJob2 := plugin.SchedulerJob{JobReadyTag: &trueTag}
	return []testSetSingleLayerTorJobNodesScore{
		{
			name:    "01 will return err when th job ready tag is false",
			th:      &TorSingleLevelHandler{TorHandler: TorHandler{Job: &fakeJob}},
			wantErr: false,
		},
		{
			name:    "02 will return err when th global tor is nil",
			th:      &TorSingleLevelHandler{TorHandler: TorHandler{Job: &fakeJob2}},
			wantErr: true,
		},
	}
}

func TestSetSingleLayerTorAffinityJobNodesScore(t *testing.T) {
	for _, tt := range buildTestSetSingleLayerTorJobNodesScore() {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.th.setSingleLayerTorJobNodesScore(&api.TaskInfo{}, nil, nil); (err != nil) != tt.wantErr {
				t.Errorf("setSingleLayerTorJobNodesScore() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
