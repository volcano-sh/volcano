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
Package rescheduling is using for HuaWei Ascend pin fault
*/
package rescheduling

import (
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	test2 "volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/test"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

const (
	sliceIndexZero = 0
	sliceIndexOne  = 1
	sliceIndexTwo  = 2
	sliceIndexFour = 4
	fakeNodeName   = "node0"
)

func fakeReSchedulerFaultTask(isFault bool, paras []string, podCreateTime int64) FaultTask {
	if len(paras) < test.NPUIndex5 {
		return FaultTask{}
	}
	name := paras[sliceIndexZero]
	ns := paras[sliceIndexOne]
	nodeName := paras[sliceIndexTwo]
	rankIndex := paras[sliceIndexFour]
	faultTask := FaultTask{
		IsFaultTask:   isFault,
		TaskUID:       api.TaskID(`"` + ns + `"-"` + name + `"`),
		TaskName:      name,
		TaskNamespace: ns,
		NodeName:      nodeName,
		NodeRankIndex: rankIndex,
		UseCardName:   []string{"Ascend910-1,Ascend910-2,Ascend910-3,Ascend910-4,Ascend910-5,Ascend910-6,Ascend910-7"},
		PodCreateTime: podCreateTime,
	}
	return faultTask
}

func fakeSchedulerJobEmptyTask(jobName, namespace string) plugin.SchedulerJob {
	job0 := plugin.SchedulerJob{
		SchedulerJobAttr: util.SchedulerJobAttr{
			ComJob: util.ComJob{
				Name:      api.JobID(jobName),
				NameSpace: namespace,
				Selector:  map[string]string{util.AcceleratorType: util.ModuleAcceleratorType},
				Label: map[string]string{
					JobRescheduleLabelKey: JobGraceRescheduleLabelValue,
				},
			},
			NPUJob: &util.NPUJob{
				ReqNPUName: util.NPU910CardName,
				ReqNPUNum:  0,
				Tasks:      make(map[api.TaskID]util.NPUTask, util.NPUIndex2),
			},
		},
	}
	return job0
}

type PreStartActionTestCase struct {
	name    string
	ssn     *framework.Session
	env     *plugin.ScheduleEnv
	wantErr bool
}

func buildPreStartActionTestCase01() PreStartActionTestCase {
	return PreStartActionTestCase{
		name:    "01 PreStartAction will return err when ssn is nil",
		ssn:     nil,
		env:     &plugin.ScheduleEnv{},
		wantErr: true,
	}
}

func buildPreStartActionTestCase02() PreStartActionTestCase {
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	return PreStartActionTestCase{
		name:    "02 PreStartAction will return nil when pre start is ok",
		ssn:     ssn,
		env:     test2.FakeScheduleEnv(),
		wantErr: false,
	}
}

func buildPreStartActionTestCases() []PreStartActionTestCase {
	return []PreStartActionTestCase{
		buildPreStartActionTestCase01(),
		buildPreStartActionTestCase02(),
	}
}

func TestReSchedulerPreStartAction(t *testing.T) {
	tests := buildPreStartActionTestCases()
	reScheduler, ok := NewHandler().(*ReScheduler)
	if !ok {
		return
	}
	reSchedulerCache = newReSchedulerCache()
	reSchedulerCache.FaultNodes = map[string]*FaultNode{fakeNodeName: {}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := reScheduler.Execute(tt.env, tt.ssn); (err != nil) != tt.wantErr {
				t.Errorf("PreStartAction() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.ssn == nil {
				return
			}
			tt.ssn.Jobs["vcjob/pg0"].Tasks = map[api.TaskID]*api.TaskInfo{}
			tt.ssn.Jobs["vcjob/pg0"].PodGroup.Labels = map[string]string{util.SinglePodTag: util.EnableFunc}
			reScheduler.FaultJobs["vcjob/pg0"].PendingSessionNum = pendingTimes - 1
			reScheduler.synCacheFaultJobWithSession(tt.ssn)
		})
	}
}
