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
Package plugin is using for HuaWei Ascend pin affinity schedule frame.
*/
package plugin

import (
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

func TestGetJobLabelFromVcJob(t *testing.T) {
	tJob := test.FakeNormalTestJob("test1", 1)
	test.AddTestJobLabel(tJob, "haha", "who")
	type args struct {
		job *api.JobInfo
	}
	tests := []struct {
		name string
		args args
		want map[string]string
	}{
		{
			name: "01-getJobLabelFromVcJob get ok test",
			args: args{job: tJob},
			want: map[string]string{"fault-scheduling": "force", "haha": "who"},
		},
		{
			name: "02-getJobLabelFromVcJob return nil when job is nil",
			args: args{job: nil}, want: nil,
		},
		{
			name: "03-getJobLabelFromVcJob return nil when job tasks is empty",
			args: args{job: &api.JobInfo{}}, want: map[string]string{},
		},
		{
			name: "04-getJobLabelFromVcJob return selector when job tasks not empty",
			args: args{job: &api.JobInfo{Tasks: map[api.TaskID]*api.TaskInfo{
				"task1": {Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					"a": "1"}}}}},
			}}, want: map[string]string{"a": "1"},
		},
		{
			name: "05-getJobLabelFromVcJob return selector when job tasks not empty",
			args: args{job: &api.JobInfo{Tasks: map[api.TaskID]*api.TaskInfo{
				"task1": {Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					"a": "1", "b": "1"}}}},
				"task2": {Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{
					"a": "1"}}}}},
				PodGroup: &api.PodGroup{PodGroup: scheduling.PodGroup{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"c": "0"}}}},
			}}, want: map[string]string{"a": "1", "b": "1", "c": "0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getJobLabelFromVcJob(tt.args.job); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getJobLabelFromVcJob() = %v, want %v", got, tt.want)
			}
		})
	}
}

type getJobNPUTasksArgs struct {
	vcJob *api.JobInfo
}

type getJobNPUTasksTest struct {
	name string
	args getJobNPUTasksArgs
	want map[api.TaskID]util.NPUTask
}

func buildGetJobNPUTasksTest() []getJobNPUTasksTest {
	tJob1 := test.FakeNormalTestJob("test1", 1)
	test.AddFakeTaskResReq(tJob1.Tasks[`"vcjob"-"pod"`], util.NPU910CardName, util.NPUIndex8)
	tJob2 := test.FakeNormalTestJob("test1", 0)
	tests := []getJobNPUTasksTest{
		{
			name: "01-GetJobNPUTasks return nil when job is nil",
			args: getJobNPUTasksArgs{vcJob: nil},
			want: nil,
		},
		{
			name: "02-GetJobNPUTasks return nil when job tasks is empty",
			args: getJobNPUTasksArgs{vcJob: tJob2},
			want: nil,
		},
	}
	return tests
}

func TestGetJobNPUTasks(t *testing.T) {
	tests := buildGetJobNPUTasksTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetJobNPUTasks(tt.args.vcJob); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetJobNPUTasks() =%+v, want %+v", got, tt.want)
			}
		})
	}
}

type getJobSelectorFromVcJobArgs struct {
	job *api.JobInfo
}

type getJobSelectorFromVcJobTest struct {
	name string
	args getJobSelectorFromVcJobArgs
	want map[string]string
}

func buildGetJobSelectorFromVcJobTest() []getJobSelectorFromVcJobTest {
	tJob1 := test.FakeNormalTestJob("test1", 1)
	test.AddTestJobLabel(tJob1, "haha", "heihei")
	tJob2 := test.FakeNormalTestJob("test1", 1)
	tests := []getJobSelectorFromVcJobTest{
		{
			name: "01-getJobSelectorFromVcJob nil job selector test",
			args: getJobSelectorFromVcJobArgs{job: tJob2},
			want: make(map[string]string, util.MapInitNum),
		},
		{
			name: "02-getJobSelectorFromVcJob nil job selector test",
			args: getJobSelectorFromVcJobArgs{job: tJob1},
			want: map[string]string{"haha": "heihei"},
		},
		{
			name: "03-getJobSelectorFromVcJob return nil when job is nil",
			args: getJobSelectorFromVcJobArgs{job: nil},
			want: nil,
		},
		{
			name: "04-getJobSelectorFromVcJob return nil when job tasks is empty",
			args: getJobSelectorFromVcJobArgs{job: &api.JobInfo{}},
			want: map[string]string{},
		},
		{
			name: "05-getJobSelectorFromVcJob return selector when job tasks not empty",
			args: getJobSelectorFromVcJobArgs{
				job: &api.JobInfo{Tasks: map[api.TaskID]*api.TaskInfo{
					"task1": {Pod: &v1.Pod{Spec: v1.PodSpec{NodeSelector: map[string]string{"a": "1"}}}}}}},
			want: map[string]string{"a": "1"},
		},
		{
			name: "06-getJobSelectorFromVcJob return selector when job tasks not empty",
			args: getJobSelectorFromVcJobArgs{
				job: &api.JobInfo{Tasks: map[api.TaskID]*api.TaskInfo{
					"task1": {Pod: &v1.Pod{Spec: v1.PodSpec{NodeSelector: map[string]string{
						"a": "1", "b": "1"}}}},
					"task2": {Pod: &v1.Pod{Spec: v1.PodSpec{NodeSelector: map[string]string{
						"a": "1", "b": "1"}}}}}}},
			want: map[string]string{"a": "1", "b": "1"},
		},
	}
	return tests
}

func TestGetJobSelectorFromVcJob(t *testing.T) {
	tests := buildGetJobSelectorFromVcJobTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getJobSelectorFromVcJob(tt.args.job); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getJobSelectorFromVcJob() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetTaskSelectors(t *testing.T) {
	tTasks := test.FakeNormalTestTasks(util.NPUIndex2)
	test.AddTestTaskLabel(tTasks[1], "haha", "who")
	type args struct {
		task *api.TaskInfo
	}
	tests := []struct {
		name string
		args args
		want map[string]string
	}{
		{
			name: "01-getTaskSelectors no selector test.",
			args: args{task: tTasks[0]},
			want: nil,
		},
		{
			name: "02-getTaskSelectors ok test.",
			args: args{task: tTasks[1]},
			want: map[string]string{"haha": "who"},
		},
		{
			name: "01-getTaskSelectors return nil when task is nil",
			args: args{task: nil},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getTaskSelectors(tt.args.task); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getTaskSelectors() = %v, want %v", got, tt.want)
			}
		})
	}
}

type getVCJobReqNPUTypeFromJobInfoArgs struct {
	vcJob *api.JobInfo
}

type getVCJobReqNPUTypeFromJobInfoTest struct {
	name    string
	args    getVCJobReqNPUTypeFromJobInfoArgs
	want    string
	want1   int
	wantErr bool
}

func buildGetVCJobReqNPUTypeFromJobInfoTest() []getVCJobReqNPUTypeFromJobInfoTest {
	tJob1 := test.FakeNormalTestJob("test1", 1)
	test.SetFakeJobRequestSource(tJob1, util.NPU910CardName, util.NPUIndex8)
	tests := []getVCJobReqNPUTypeFromJobInfoTest{
		{
			name:    "01-GetVCJobReqNPUTypeFromJobInfo nil job test.",
			args:    getVCJobReqNPUTypeFromJobInfoArgs{vcJob: nil},
			want:    "",
			want1:   0.0,
			wantErr: true,
		},
		{
			name:    "03-GetVCJobReqNPUTypeFromJobInfo ok test.",
			args:    getVCJobReqNPUTypeFromJobInfoArgs{vcJob: tJob1},
			want:    util.NPU910CardName,
			want1:   util.NPUIndex8,
			wantErr: false,
		},
	}
	return tests
}

func TestGetVCJobReqNPUTypeFromJobInfo(t *testing.T) {
	tests := buildGetVCJobReqNPUTypeFromJobInfoTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := GetVCJobReqNPUTypeFromJobInfo(tt.args.vcJob)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetVCJobReqNPUTypeFromJobInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetVCJobReqNPUTypeFromJobInfo() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("GetVCJobReqNPUTypeFromJobInfo() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

type getVCTaskReqNPUTypeFromTaskInfoArgs struct {
	vcTask *api.TaskInfo
}

type getVCTaskReqNPUTypeFromTaskInfoTest struct {
	name  string
	args  getVCTaskReqNPUTypeFromTaskInfoArgs
	want  string
	want1 int
}

func buildGetVCTaskReqNPUTypeFromTaskInfoTest() []getVCTaskReqNPUTypeFromTaskInfoTest {
	tTasks := test.FakeNormalTestTasks(1)
	tests := []getVCTaskReqNPUTypeFromTaskInfoTest{
		{
			name:  "01-getVCTaskReqNPUTypeFromTaskInfo nil test.",
			args:  getVCTaskReqNPUTypeFromTaskInfoArgs{vcTask: nil},
			want:  "",
			want1: 0,
		},
		{
			name:  "02-getVCTaskReqNPUTypeFromTaskInfo ok test.",
			args:  getVCTaskReqNPUTypeFromTaskInfoArgs{vcTask: tTasks[0]},
			want:  util.NPU910CardName,
			want1: util.NPUIndex8,
		},
		{
			name: "03-getVCTaskReqNPUTypeFromTaskInfo no res test.",
			args: getVCTaskReqNPUTypeFromTaskInfoArgs{vcTask: &api.TaskInfo{Resreq: &api.Resource{
				ScalarResources: map[v1.ResourceName]float64{v1.ResourceName("abc"): float64(util.NPUIndex8)},
			}}},
			want:  "",
			want1: 0,
		},
	}
	return tests
}

func TestGetVCTaskReqNPUTypeFromTaskInfo(t *testing.T) {
	tests := buildGetVCTaskReqNPUTypeFromTaskInfoTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := getVCTaskReqNPUTypeFromTaskInfo(tt.args.vcTask)
			if got != tt.want {
				t.Errorf("getVCTaskReqNPUTypeFromTaskInfo() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("getVCTaskReqNPUTypeFromTaskInfo() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}

type isJobInitialArgs struct {
	job *api.JobInfo
}

type isJobInitialTest struct {
	name string
	args isJobInitialArgs
	want bool
}

func buildGIsJobInitialTest() []isJobInitialTest {
	tJob := test.FakeNormalTestJob("testJob", 1)
	tJob2 := test.FakeNormalTestJob("testJob2", 1)
	test.SetFakeNPUJobStatusPending(tJob2)
	tests := []isJobInitialTest{
		{
			name: "01-isJobInitial pending test",
			args: isJobInitialArgs{job: tJob2},
			want: true,
		},
		{
			name: "02-isJobInitial test ok",
			args: isJobInitialArgs{job: tJob},
			want: true,
		},
	}
	return tests
}

func TestIsJobInitial(t *testing.T) {
	tests := buildGIsJobInitialTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isJobInitial(tt.args.job); got != tt.want {
				t.Errorf("isJobInitial() = %v, want %v", got, tt.want)
			}
		})
	}
}

type jobValidArgs struct {
	obj interface{}
}

type jobValidTest struct {
	name   string
	fields fields
	args   jobValidArgs
	want   *api.ValidateResult
}

func buildJobValidTest() []jobValidTest {
	tJob := test.FakeNormalTestJob("testJob", 1)
	tJob2 := test.FakeNormalTestJob("testJob2", 1)
	test.SetFakeNPUJobStatusPending(tJob2)
	tJob3 := test.FakeNormalTestJob("testJob", 1)
	test.AddTestJobLabel(tJob3, "haha", "who")
	fakeRsNum := int32(util.NPUIndex1)
	tests := []jobValidTest{
		{
			name:   "01-JobValid not job test.",
			fields: fields{ScheduleEnv: ScheduleEnv{}},
			args:   jobValidArgs{obj: "haha"},
			want: &api.ValidateResult{Pass: false, Reason: "job convert failed",
				Message: `validJobFn ["haha"] failed:job convert failed`},
		},
		{
			name:   "02-JobValid job not initial test.",
			fields: fields{ScheduleEnv: ScheduleEnv{}},
			args:   jobValidArgs{obj: tJob2},
			want:   nil,
		},
		{
			name: "03-JobValid job not in jobs test.",
			fields: fields{NPUPlugins: make(sets.String),
				ScheduleEnv: ScheduleEnv{ClusterCache: NewClusterCache()}},
			args: jobValidArgs{obj: tJob},
			want: nil,
		},
		{
			name: "04-JobValid job is deployment job and task is not ready.",
			fields: fields{NPUPlugins: make(sets.String),
				ScheduleEnv: ScheduleEnv{ClusterCache: ClusterCache{
					Jobs: map[api.JobID]SchedulerJob{tJob.UID: {Owner: OwnerInfo{
						OwnerReference: metav1.OwnerReference{Kind: ReplicaSetType},
						Replicas:       &fakeRsNum},
						SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{Tasks: map[api.TaskID]util.NPUTask{}}}},
					}}}},
			args: jobValidArgs{obj: tJob},
			want: &api.ValidateResult{Pass: false, Reason: "job is not ready",
				Message: "job  task num 0 less than replicas 1"},
		},
	}
	return tests
}

func TestJobValid(t *testing.T) {
	tests := buildJobValidTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sHandle := &ScheduleHandler{
				NPUPlugins:  tt.fields.NPUPlugins,
				ScheduleEnv: tt.fields.ScheduleEnv,
			}
			sHandle.FrameAttr.IsFirstSession = util.PtrInit(false)
			if got := sHandle.JobValid(tt.args.obj); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("JobValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

type setJobPendReasonByNodesCaseArgs struct {
	job *api.JobInfo
}

type setJobPendReasonByNodesCaseTest struct {
	name   string
	fields fields
	args   setJobPendReasonByNodesCaseArgs
}

func buildSetJobPendReasonByNodesCaseTest() []setJobPendReasonByNodesCaseTest {
	tJob := test.FakeNormalTestJob("testJob", 1)
	tJob1 := test.FakeNormalTestJob("testJob1", 1)
	test.SetFakeNPUJobErrors(tJob1, "haha")
	tests := []setJobPendReasonByNodesCaseTest{
		{
			name: "01-SetJobPendReasonByNodesCase job no error test.",
			fields: fields{NPUPlugins: make(sets.String),
				ScheduleEnv: ScheduleEnv{
					ClusterCache: NewClusterCache(),
					FrameAttr:    VolcanoFrame{}}},
			args: setJobPendReasonByNodesCaseArgs{job: tJob},
		},
		{
			name: "02-SetJobPendReasonByNodesCase test ok.",
			fields: fields{NPUPlugins: make(sets.String),
				ScheduleEnv: ScheduleEnv{
					ClusterCache: NewClusterCache(),
					FrameAttr:    VolcanoFrame{}}},
			args: setJobPendReasonByNodesCaseArgs{job: tJob1},
		},
	}
	return tests
}

func TestSetJobPendReasonByNodesCase(t *testing.T) {
	tests := buildSetJobPendReasonByNodesCaseTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sHandle := ScheduleHandler{
				NPUPlugins:  tt.fields.NPUPlugins,
				ScheduleEnv: tt.fields.ScheduleEnv,
			}
			sHandle.SetJobPendReasonByNodesCase(tt.args.job)
		})
	}
}

type setJobPendingReasonArgs struct {
	vcJob  *api.JobInfo
	reason interface{}
}

type setJobPendingReasonTest struct {
	name    string
	fields  fields
	args    setJobPendingReasonArgs
	wantErr bool
}

func buildSetJobPendingReasonTest() []setJobPendingReasonTest {
	tJob := test.FakeNormalTestJob("testJob", 1)
	tests := []setJobPendingReasonTest{
		{
			name: "01-SetJobPendingReason nil test.",
			fields: fields{NPUPlugins: make(sets.String),
				ScheduleEnv: ScheduleEnv{
					ClusterCache: NewClusterCache(),
					FrameAttr:    VolcanoFrame{}}},
			args:    setJobPendingReasonArgs{vcJob: nil},
			wantErr: true,
		},
		{
			name: "02-SetJobPendingReason not support type test.",
			fields: fields{NPUPlugins: make(sets.String),
				ScheduleEnv: ScheduleEnv{
					ClusterCache: NewClusterCache(),
					FrameAttr:    VolcanoFrame{}}},
			args:    setJobPendingReasonArgs{vcJob: tJob, reason: api.NodeInfo{}},
			wantErr: true,
		},
		{
			name: "03-SetJobPendingReason string type test.",
			fields: fields{NPUPlugins: make(sets.String),
				ScheduleEnv: ScheduleEnv{
					ClusterCache: NewClusterCache(),
					FrameAttr:    VolcanoFrame{}}},
			args:    setJobPendingReasonArgs{vcJob: tJob, reason: "haha"},
			wantErr: false,
		},
		{
			name: "04-SetJobPendingReason nodeErrors test.",
			fields: fields{NPUPlugins: make(sets.String),
				ScheduleEnv: ScheduleEnv{
					ClusterCache: NewClusterCache(),
					FrameAttr:    VolcanoFrame{}}},
			args:    setJobPendingReasonArgs{vcJob: tJob, reason: map[api.TaskID]*api.FitErrors{}},
			wantErr: false,
		},
	}
	return tests
}

func TestScheduleHandlerSetJobPendingReason(t *testing.T) {
	tests := buildSetJobPendingReasonTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sHandle := &ScheduleHandler{
				NPUPlugins:  tt.fields.NPUPlugins,
				ScheduleEnv: tt.fields.ScheduleEnv,
			}
			if err := sHandle.SetJobPendingReason(tt.args.vcJob, tt.args.reason); (err != nil) != tt.wantErr {
				t.Errorf("SetJobPendingReason() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type schedulerJobFields struct {
	SchedulerJobAttr util.SchedulerJobAttr
	handler          SchedulerPlugin
}

type CheckNodeNumArgs struct {
	taskInfo *api.TaskInfo
	vcNode   NPUNode
}

type CheckNodeNumTest struct {
	name    string
	fields  schedulerJobFields
	args    CheckNodeNumArgs
	wantErr bool
}

func buildCheckNodeNumTest() []CheckNodeNumTest {
	tTasks := test.FakeNormalTestTasks(1)
	tNode1 := NPUNode{CommonNode: CommonNode{
		Name: "testNode1", Idle: map[v1.ResourceName]float64{util.NPU910CardName: util.NPUIndex2}},
	}
	tNode2 := NPUNode{CommonNode: CommonNode{Name: "testNode2", Idle: map[v1.ResourceName]float64{
		util.NPU910CardName: util.NPUIndex8 * util.NPUHexKilo}}}
	tests := []CheckNodeNumTest{
		{
			name:    "01-checkNodeNum no task request test.",
			fields:  schedulerJobFields{SchedulerJobAttr: util.SchedulerJobAttr{}},
			args:    CheckNodeNumArgs{taskInfo: nil},
			wantErr: true,
		},
		{
			name: "02-checkNodeNum no task test.",
			fields: schedulerJobFields{SchedulerJobAttr: util.SchedulerJobAttr{
				NPUJob: &util.NPUJob{Tasks: map[api.TaskID]util.NPUTask{}}}},
			args: CheckNodeNumArgs{taskInfo: tTasks[0], vcNode: NPUNode{CommonNode{Name: "testNode1", Idle: nil},
				VNode{}}},
			wantErr: true,
		},
		{
			name: "03-checkNodeNum no node idle test.",
			fields: schedulerJobFields{SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{
				Tasks: map[api.TaskID]util.NPUTask{tTasks[0].UID: {Name: tTasks[0].Name,
					ReqNPUName: util.NPU910CardName, ReqNPUNum: util.NPUIndex8}}}}},
			args: CheckNodeNumArgs{taskInfo: tTasks[0], vcNode: NPUNode{CommonNode{Name: "testNode1", Idle: nil},
				VNode{}}},
			wantErr: true,
		},
		{
			name: "04-checkNodeNum not meet test.",
			fields: schedulerJobFields{SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{
				Tasks: map[api.TaskID]util.NPUTask{tTasks[0].UID: {Name: tTasks[0].Name,
					ReqNPUName: util.NPU910CardName, ReqNPUNum: util.NPUIndex8}}}}},
			args:    CheckNodeNumArgs{taskInfo: tTasks[0], vcNode: tNode1},
			wantErr: true,
		},
		{
			name: "05-checkNodeNum meet test.",
			fields: schedulerJobFields{SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{
				Tasks: map[api.TaskID]util.NPUTask{tTasks[0].UID: {Name: tTasks[0].Name,
					ReqNPUName: util.NPU910CardName, ReqNPUNum: util.NPUIndex8}}}}},
			args:    CheckNodeNumArgs{taskInfo: tTasks[0], vcNode: tNode2},
			wantErr: false,
		},
	}
	return tests
}

func TestSchedulerJobCheckNodeNum(t *testing.T) {
	tests := buildCheckNodeNumTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sJob := &SchedulerJob{
				SchedulerJobAttr: tt.fields.SchedulerJobAttr,
				policyHandler:    tt.fields.handler,
			}
			if err := sJob.checkNodeNum(tt.args.taskInfo, tt.args.vcNode); (err != nil) != tt.wantErr {
				t.Errorf("checkNodeNum() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type initArgs struct {
	vcJob   *api.JobInfo
	sHandle *ScheduleHandler
}

type initTest struct {
	name    string
	fields  schedulerJobFields
	args    initArgs
	wantErr bool
}

func buildInitTest() []initTest {
	tJob := test.FakeNormalTestJob("haha", 1)
	tests := []initTest{
		{
			name:    "01-init vcJob nil test.",
			fields:  schedulerJobFields{SchedulerJobAttr: util.SchedulerJobAttr{}},
			args:    initArgs{vcJob: nil},
			wantErr: true,
		},
		{
			name:    "02-init plugin not register test.",
			fields:  schedulerJobFields{SchedulerJobAttr: util.SchedulerJobAttr{}},
			args:    initArgs{vcJob: tJob},
			wantErr: true,
		},
	}
	return tests
}

func TestSchedulerJobInit(t *testing.T) {
	tests := buildInitTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sJob := &SchedulerJob{
				SchedulerJobAttr: tt.fields.SchedulerJobAttr,
				policyHandler:    tt.fields.handler,
			}
			if err := sJob.init(tt.args.vcJob, tt.args.sHandle); (err != nil) != tt.wantErr {
				t.Errorf("init() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type fieldsResetConfigMap struct {
	SchedulerJobAttr util.SchedulerJobAttr
	handler          SchedulerPlugin
	ServerList       []*Tor
	TorBlackMaps     map[string]struct{}
	JobReadyTag      bool
}

type argsResetConfigMap struct {
	sHandle *ScheduleHandler
}

type updateResetConfigMapTestCase struct {
	name   string
	fields fieldsResetConfigMap
	args   argsResetConfigMap
	wantCm *v1.ConfigMap
}

func buildUpdateResetConfigMapTestCase01() updateResetConfigMapTestCase {
	tmpTest := updateResetConfigMapTestCase{}
	tmpTest.name = "01-will return nil when client is nil"
	return tmpTest
}

func buildUpdateResetConfigMapTestCases() []updateResetConfigMapTestCase {
	return []updateResetConfigMapTestCase{
		buildUpdateResetConfigMapTestCase01(),
	}
}

func TestSchedulerJobUpdateResetConfigMap(t *testing.T) {
	tests := buildUpdateResetConfigMapTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sJob := &SchedulerJob{
				SchedulerJobAttr: tt.fields.SchedulerJobAttr,
				policyHandler:    tt.fields.handler,
				JobReadyTag:      &tt.fields.JobReadyTag,
			}
			sJob.updateResetConfigMap(tt.args.sHandle)
		})
	}
}

func TestIsSelectorContains(t *testing.T) {
	tests := []struct {
		name     string
		defValue string
		jobValue string
		want     bool
	}{
		{
			name:     "01-isSelectorContains return true when jobValue is empty",
			defValue: "",
			jobValue: "",
			want:     true,
		},
		{
			name:     "02-isSelectorContains return true when jobValue contains defValue",
			defValue: "0|1",
			jobValue: "1",
			want:     true,
		},
		{
			name:     "03-isSelectorContains return false when jobValue not contains defValue",
			defValue: "0|1",
			jobValue: "2",
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSelectorContains(tt.defValue, tt.jobValue); got != tt.want {
				t.Errorf("isSelectorContains() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsEachStringContainsSameElement(t *testing.T) {
	tests := []struct {
		name   string
		first  string
		second string
		seq    string
		want   bool
	}{
		{
			name:   "01-isEachStringContainsSameElement return true when first equals second",
			first:  "x",
			second: "x",
			want:   true,
		},
		{
			name:   "02-isEachStringContainsSameElement return true when first contains second",
			first:  "x",
			second: "x,y,z",
			want:   true,
		},
		{
			name:   "03-isEachStringContainsSameElement return false when first not contains second",
			first:  "x,y,z",
			second: "a",
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEachStringContainsSameElement(tt.first, tt.second, tt.seq); got != tt.want {
				t.Errorf("isEachStringContainsSameElement() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetTaskLabels(t *testing.T) {
	tests := []struct {
		name string
		task *api.TaskInfo
		want map[string]string
	}{
		{
			name: "01-getTaskLabels return nil when task is nil",
			task: nil,
			want: nil,
		},
		{
			name: "02-getTaskLabels return labels when task labels not nil",
			task: &api.TaskInfo{Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"foo": "bar"}}}},
			want: map[string]string{"foo": "bar"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getTaskLabels(tt.task); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getTaskLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInitVcJobHcclIndex(t *testing.T) {
	tests := []struct {
		name    string
		taskInf *api.TaskInfo
		want    map[string]string
	}{
		{
			name:    "01-InitVcJobHcclIndex do nothing when task not exist hccl/rankIndex annotation",
			taskInf: &api.TaskInfo{Pod: &v1.Pod{}},
			want:    map[string]string{},
		},

		{
			name: "02-InitVcJobHcclIndex do nothing when task not exist hccl/rankIndex annotation",
			taskInf: &api.TaskInfo{Pod: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{podRankIndex: "0"}}}},
			want: map[string]string{podRankIndex: "0"},
		},
		{
			name: "03-InitVcJobHcclIndex set annotation when task exist hccl/rankIndex annotation",
			taskInf: &api.TaskInfo{Pod: &v1.Pod{
				Spec: v1.PodSpec{Containers: []v1.Container{{
					Name: "container1",
					Env:  []v1.EnvVar{{Name: vcTaskIndex, Value: "1"}},
				}}}}},
			want: map[string]string{podRankIndex: "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initVcJobHcclIndex(tt.taskInf)
			if !reflect.DeepEqual(tt.taskInf.Pod.Annotations, tt.want) {
				t.Errorf("InitVcJobHcclIndex() = %v, want %v", tt.taskInf.Pod.Annotations, tt.want)
			}
		})
	}
}

func TestGetJobInfoAllocatedTaskNum(t *testing.T) {
	tests := []struct {
		name    string
		jobInfo *api.JobInfo
		want    int32
	}{
		{
			name:    "01-GetJobInfoAllocatedTaskNum return 0 when jobInfo is nil",
			jobInfo: nil,
			want:    0,
		},
		{
			name:    "02-GetJobInfoAllocatedTaskNum return 0 when jobInfo not exist task",
			jobInfo: &api.JobInfo{},
			want:    0,
		},
		{
			name: "03-GetJobInfoAllocatedTaskNum return 1 when jobInfo exist one task",
			jobInfo: &api.JobInfo{Tasks: map[api.TaskID]*api.TaskInfo{
				"task1": {TransactionContext: api.TransactionContext{NodeName: "node1"}}}},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetJobInfoAllocatedTaskNum(tt.jobInfo); got != tt.want {
				t.Errorf("GetJobInfoAllocatedTaskNum() = %v, want %v", got, tt.want)
			}
		})
	}
}

type validJobFnTest struct {
	name string
	sJob SchedulerJob
	want bool
}

func buildValidJobFnTestCases() []validJobFnTest {
	fakeTaskNum := int32(util.NPUIndex1)
	return []validJobFnTest{
		{
			name: "01 will return false when job is deployment and task is not ready",
			sJob: SchedulerJob{Owner: OwnerInfo{OwnerReference: metav1.OwnerReference{Kind: ReplicaSetType},
				Replicas: &fakeTaskNum}, SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{}}},
			want: false,
		},
		{
			name: "02 will return true when job is deployment and task is  ready",
			sJob: SchedulerJob{Owner: OwnerInfo{OwnerReference: metav1.OwnerReference{Kind: ReplicaSetType},
				Replicas: &fakeTaskNum}, SchedulerJobAttr: util.SchedulerJobAttr{
				NPUJob: &util.NPUJob{Tasks: map[api.TaskID]util.NPUTask{"test-pod": {}}}}},
			want: false,
		},
	}
}

func TestValidJobFn(t *testing.T) {
	for _, tt := range buildValidJobFnTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			if result := tt.sJob.validJobFn(); result != nil && !reflect.DeepEqual(result.Pass, tt.want) {
				t.Errorf("validJobFn() = %v, want %v", result.Pass, tt.want)
			}
		})
	}
}

func TestValidVirtualDevJob(t *testing.T) {
	patch := gomonkey.ApplyFunc(GetVCJobReqNPUTypeFromJobInfo, func(vcJob *api.JobInfo) (string, int, error) {
		return "huawei.com/Ascend910-2c", util.NPUIndex2, nil
	})
	defer patch.Reset()
	t.Run("01 no pass by job request is 2", func(t *testing.T) {
		if got := validVirtualDevJob(&api.JobInfo{}); !reflect.DeepEqual(got.Pass, false) {
			t.Errorf("validVirtualDevJob() = %v, want %v", got.Pass, false)
		}
	})
}
