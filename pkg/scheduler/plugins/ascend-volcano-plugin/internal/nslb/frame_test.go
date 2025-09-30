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
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

type InitPolicyHandlerTest struct {
	name string
	job  util.SchedulerJobAttr
	env  plugin.ScheduleEnv
	want plugin.SchedulerPluginNeed
}

func buildInitPolicyHandlerTestCases() []InitPolicyHandlerTest {
	defaultHandler := TorHandler{
		pluginName: pluginName,
	}
	return []InitPolicyHandlerTest{
		{
			name: "01 will return nil when job is null",
			job:  util.SchedulerJobAttr{},
			env:  plugin.ScheduleEnv{},
			want: nil,
		},
		{
			name: "02 will return default handler when tor is nil",
			job:  util.SchedulerJobAttr{ComJob: util.ComJob{Label: map[string]string{TorAffinityKey: LargeModelTag}}},
			env:  plugin.ScheduleEnv{},
			want: &defaultHandler,
		},
		{
			name: "03 will return single level handler when tor level is Single Layer",
			job:  util.SchedulerJobAttr{ComJob: util.ComJob{Label: map[string]string{TorAffinityKey: LargeModelTag}}},
			env:  plugin.ScheduleEnv{ClusterCache: plugin.ClusterCache{Tors: &plugin.TorList{TorLevel: SingleLayer}}},
			want: &TorSingleLevelHandler{TorHandler: TorHandler{globalTorEnv: &plugin.TorList{TorLevel: SingleLayer},
				pluginName: pluginName}},
		},
		{
			name: "04 will return default handler when tor level is not Single Layer",
			job:  util.SchedulerJobAttr{ComJob: util.ComJob{Label: map[string]string{TorAffinityKey: LargeModelTag}}},
			env:  plugin.ScheduleEnv{ClusterCache: plugin.ClusterCache{Tors: &plugin.TorList{}}},
			want: &TorHandler{globalTorEnv: &plugin.TorList{}, pluginName: pluginName},
		},
	}
}

func TestInitPolicyHandler(t *testing.T) {
	for _, tt := range buildInitPolicyHandlerTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := InitPolicyHandler(tt.job, tt.env)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("InitPolicyHandler() got = %v, want %v", got, tt.want)
			}
		})
	}
}

type ValidNPUJobTest struct {
	name     string
	th       *TorHandler
	wantPass bool
}

func buildValidNPUJobTestCases() []ValidNPUJobTest {
	fakeJob := plugin.SchedulerJob{}
	fakeJob.Label = map[string]string{TorAffinityKey: LargeModelTag + "test"}
	return []ValidNPUJobTest{
		{
			name:     "01 will not pass when th is nil",
			th:       nil,
			wantPass: false,
		},
		{
			name:     "02 will not pass  when th global tor is nil",
			th:       &TorHandler{Job: &fakeJob},
			wantPass: false,
		},
		{
			name:     "03 will not pass when th job label is not meet require",
			th:       &TorHandler{globalTorEnv: &plugin.TorList{}, Job: &fakeJob},
			wantPass: false,
		},
	}
}

func TestTorHandlerValidNPUJob(t *testing.T) {
	for _, tt := range buildValidNPUJobTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.th.ValidNPUJob(); !reflect.DeepEqual(got.Pass, tt.wantPass) {
				t.Errorf("ValidNPUJob() = %v, want %v", got.Pass, tt.wantPass)
			}
		})
	}
}

func TestISchedulerPluginNeedInterface(t *testing.T) {
	th := &TorHandler{}
	t.Run("01 test CheckNodeNPUByTask will return nil", func(t *testing.T) {
		if got := th.CheckNodeNPUByTask(nil, plugin.NPUNode{}); !reflect.DeepEqual(got, nil) {
			t.Errorf("CheckNodeNPUByTask() = %v, want %v", got, nil)
		}
	})
	t.Run("02 test ScoreBestNPUNodes will return nil", func(t *testing.T) {
		if got := th.ScoreBestNPUNodes(nil, nil, nil); !reflect.DeepEqual(got, nil) {
			t.Errorf("ScoreBestNPUNodes() = %v, want %v", got, nil)
		}
	})
	t.Run("03 test UseAnnotation will return nil", func(t *testing.T) {
		if got := th.UseAnnotation(nil, plugin.NPUNode{}); !reflect.DeepEqual(got, &plugin.NPUNode{}) {
			t.Errorf("UseAnnotation() = %v, want %v", got, &plugin.NPUNode{})
		}
	})
	t.Run("04 test ReleaseAnnotation will return nil", func(t *testing.T) {
		if got := th.ReleaseAnnotation(nil, plugin.NPUNode{}); !reflect.DeepEqual(got, &plugin.NPUNode{}) {
			t.Errorf("ReleaseAnnotation() = %v, want %v", got, &plugin.NPUNode{})
		}
	})

	t.Run("05 test PreStartAction will return nil", func(t *testing.T) {
		if got := th.PreStartAction(nil); !reflect.DeepEqual(got, nil) {
			t.Errorf("PreStartAction() = %v, want %v", got, nil)
		}
	})
}

func TestInitMyJobPlugin(t *testing.T) {
	var th *TorHandler
	t.Run("01 test InitMyJobPlugin will return nil", func(t *testing.T) {
		if got := th.InitMyJobPlugin(util.SchedulerJobAttr{}, plugin.ScheduleEnv{}); !reflect.DeepEqual(got, nil) {
			t.Errorf("InitMyJobPlugin() = %v, want %v", got, nil)
		}
	})
	th = &TorHandler{globalTorEnv: &plugin.TorList{}}
	fakeJobAttr := util.SchedulerJobAttr{}
	fakeJobAttr.Name = "test"
	fakeJob := plugin.SchedulerJob{SchedulerJobAttr: fakeJobAttr}
	t.Run("02 test InitMyJobPlugin will return nil", func(t *testing.T) {
		if got := th.InitMyJobPlugin(fakeJobAttr, plugin.ScheduleEnv{ClusterCache: plugin.ClusterCache{
			Jobs: map[api.JobID]plugin.SchedulerJob{"test": fakeJob}}}); !reflect.DeepEqual(got, nil) {
			t.Errorf("InitMyJobPlugin() = %v, want %v", got, nil)
		}
	})
}
