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
	"testing"

	"k8s.io/apimachinery/pkg/util/sets"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

type npuAllocateFuncArgs struct {
	task *api.TaskInfo
}

type npuAllocateFuncTest struct {
	name   string
	fields fields
	args   npuAllocateFuncArgs
	want   string
}

func buildNPUAllocateFuncTest() []npuAllocateFuncTest {
	task := test.FakeNormalTestTasks(1)[0]
	name, num := getVCTaskReqNPUTypeFromTaskInfo(task)
	tmpJobReadyTag := true
	npuTask := util.NPUTask{
		Name: task.Name, NameSpace: task.Namespace, ReqNPUName: name,
		ReqNPUNum: num,
		Label:     getTaskLabels(task), VTask: &util.VTask{}}
	tests := []npuAllocateFuncTest{
		{
			name:   "01-NPUAllocateFunc task nil test",
			fields: fields{},
			args:   npuAllocateFuncArgs{task: nil},
			want:   "",
		},
		{
			name: "02-NPUAllocateFunc no job test.",
			fields: fields{NPUPlugins: make(sets.String),
				ScheduleEnv: ScheduleEnv{
					ClusterCache: NewClusterCache(),
					FrameAttr:    VolcanoFrame{}}},
			args: npuAllocateFuncArgs{task: task},
			want: "",
		},
		{
			name: "03-NPUAllocateFunc no node test",
			fields: fields{NPUPlugins: make(sets.String),
				ScheduleEnv: ScheduleEnv{
					ClusterCache: ClusterCache{
						Jobs: map[api.JobID]SchedulerJob{task.Job: {
							JobReadyTag: &tmpJobReadyTag,
							SchedulerJobAttr: util.SchedulerJobAttr{
								NPUJob: &util.NPUJob{
									Tasks: map[api.TaskID]util.NPUTask{task.UID: npuTask},
								},
							}}},
						Nodes: map[string]NPUNode{}},
					FrameAttr: VolcanoFrame{}}},
			args: npuAllocateFuncArgs{task: task},
			want: "",
		},
		{
			name: "04-NPUAllocateFunc UseAnnotation failed test.",
			fields: fields{NPUPlugins: make(sets.String),
				ScheduleEnv: newDefaultsHandlerByFakeSsn().ScheduleEnv},
			args: npuAllocateFuncArgs{task: task},
			want: "",
		},
	}
	return tests
}

func TestNPUAllocateFunc(t *testing.T) {
	tests := buildNPUAllocateFuncTest()
	temp := func(task *api.TaskInfo) string {
		if task == nil {
			return ""
		}
		value, _ := task.Pod.Annotations[test.NPU910CardName]
		return value
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sHandle := ScheduleHandler{
				NPUPlugins:  tt.fields.NPUPlugins,
				ScheduleEnv: tt.fields.ScheduleEnv,
			}
			sHandle.NPUAllocateFunc(tt.args.task)
			value := temp(tt.args.task)
			if value != tt.want {
				t.Errorf("NPUAllocateFunc() got = %v, want %v", value, tt.want)
			}
		})
	}
}

type npuDeallocateFuncArgs struct {
	task *api.TaskInfo
}

type npuDeallocateFuncTest struct {
	name   string
	fields fields
	args   npuDeallocateFuncArgs
	want   string
}

func makeNPUDeallocateFuncTest01(_ *api.TaskInfo) npuDeallocateFuncTest {
	return npuDeallocateFuncTest{
		name:   "01-NPUDeallocateFunc task nil test",
		fields: fields{}, args: npuDeallocateFuncArgs{task: nil}, want: "",
	}
}

func makeNPUDeallocateFuncTest02(vTask *api.TaskInfo) npuDeallocateFuncTest {
	return npuDeallocateFuncTest{
		name: "02-NPUAllocateFunc no job test.",
		fields: fields{NPUPlugins: make(sets.String),
			ScheduleEnv: ScheduleEnv{ClusterCache: NewClusterCache()}},
		args: npuDeallocateFuncArgs{task: vTask}, want: "Ascend910-4",
	}
}

func makeNPUDeallocateFuncTest03(vTask *api.TaskInfo) npuDeallocateFuncTest {
	return npuDeallocateFuncTest{
		name: "03-NPUAllocateFunc no node test",
		fields: fields{NPUPlugins: make(sets.String),
			ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs:  map[api.JobID]SchedulerJob{vTask.Job: {}},
					Nodes: map[string]NPUNode{}}}},
		args: npuDeallocateFuncArgs{task: vTask}, want: "Ascend910-4",
	}
}

func makeNPUDeallocateFuncTest04(vTask *api.TaskInfo) npuDeallocateFuncTest {
	return npuDeallocateFuncTest{
		name: "04-NPUAllocateFunc UseAnnotation failed test.",
		fields: fields{NPUPlugins: make(sets.String),
			ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs: map[api.JobID]SchedulerJob{
						vTask.Job: {SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{Tasks: nil}}}},
					Nodes: map[string]NPUNode{vTask.NodeName: {}}}}},
		args: npuDeallocateFuncArgs{task: vTask}, want: "Ascend910-4",
	}
}

func makeNPUDeallocateFuncTest05(vTask *api.TaskInfo) npuDeallocateFuncTest {
	return npuDeallocateFuncTest{
		name: "05-NPUAllocateFunc pod no req test.",
		fields: fields{NPUPlugins: make(sets.String),
			ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs: map[api.JobID]SchedulerJob{
						vTask.Job: {
							SchedulerJobAttr: util.SchedulerJobAttr{
								NPUJob: &util.NPUJob{Tasks: map[api.TaskID]util.NPUTask{vTask.UID: {ReqNPUName: "haha"}}}},
						},
					},
					Nodes: map[string]NPUNode{vTask.NodeName: {}}}}},
		args: npuDeallocateFuncArgs{task: vTask}, want: "Ascend910-4",
	}
}

func makeNPUDeallocateFuncTest06(vTask *api.TaskInfo) npuDeallocateFuncTest {
	tmpSchedulerJobAttr := util.SchedulerJobAttr{
		NPUJob: &util.NPUJob{
			Tasks: map[api.TaskID]util.NPUTask{
				vTask.UID: {ReqNPUName: test.NPU910CardName, ReqNPUNum: util.NPUIndex2}},
		},
	}

	return npuDeallocateFuncTest{
		name: "06-NPUAllocateFunc pod req num not meet test.",
		fields: fields{NPUPlugins: make(sets.String),
			ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs: map[api.JobID]SchedulerJob{
						vTask.Job: {
							SchedulerJobAttr: tmpSchedulerJobAttr,
						},
					},
					Nodes: map[string]NPUNode{vTask.NodeName: {}}}}},
		args: npuDeallocateFuncArgs{task: vTask}, want: "Ascend910-4",
	}
}

func makeNPUDeallocateFuncTest07(vTask *api.TaskInfo) npuDeallocateFuncTest {
	tmpSchedulerJobAttr := util.SchedulerJobAttr{
		NPUJob: &util.NPUJob{
			Tasks: map[api.TaskID]util.NPUTask{
				vTask.UID: {ReqNPUName: test.NPU910CardName, ReqNPUNum: 1}},
		},
	}
	tmpNPUNode := NPUNode{
		CommonNode: CommonNode{Annotation: nil},
	}
	return npuDeallocateFuncTest{
		name: "07-NPUAllocateFunc node no annotation value test.",
		fields: fields{NPUPlugins: make(sets.String),
			ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs: map[api.JobID]SchedulerJob{
						vTask.Job: {SchedulerJobAttr: tmpSchedulerJobAttr},
					},
					Nodes: map[string]NPUNode{vTask.NodeName: tmpNPUNode}}}},
		args: npuDeallocateFuncArgs{task: vTask}, want: "Ascend910-4",
	}
}

func makeNPUDeallocateFuncTest08(vTask *api.TaskInfo) npuDeallocateFuncTest {
	tmpSchedulerJobAttr := util.SchedulerJobAttr{
		NPUJob: &util.NPUJob{
			Tasks: map[api.TaskID]util.NPUTask{
				vTask.UID: {ReqNPUName: test.NPU910CardName, ReqNPUNum: 1,
					VTask: &util.VTask{}}},
		},
	}
	tmpNPUNode := NPUNode{
		CommonNode: CommonNode{
			Annotation: map[string]string{test.NPU910CardName: ""},
		},
	}
	return npuDeallocateFuncTest{
		name: "08-NPUAllocateFunc node has empty annotation value test.",
		fields: fields{NPUPlugins: make(sets.String),
			ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs: map[api.JobID]SchedulerJob{vTask.Job: {SchedulerJobAttr: tmpSchedulerJobAttr,
						policyHandler: New(testPluginName)}},
					Nodes: map[string]NPUNode{vTask.NodeName: tmpNPUNode}}}},
		args: npuDeallocateFuncArgs{task: vTask}, want: "",
	}
}

func makeNPUDeallocateFuncTest09(vTask *api.TaskInfo) npuDeallocateFuncTest {
	tmpSchedulerJobAttr := util.SchedulerJobAttr{
		NPUJob: &util.NPUJob{
			Tasks: map[api.TaskID]util.NPUTask{
				vTask.UID: {ReqNPUName: test.NPU910CardName, ReqNPUNum: 1}},
		},
	}
	tmpNPUNode := NPUNode{
		CommonNode: CommonNode{
			Annotation: map[string]string{test.NPU910CardName: "Ascend910-3"},
		},
	}
	return npuDeallocateFuncTest{
		name: "09-NPUAllocateFunc ok test.",
		fields: fields{NPUPlugins: make(sets.String),
			ScheduleEnv: ScheduleEnv{
				ClusterCache: ClusterCache{
					Jobs: map[api.JobID]SchedulerJob{vTask.Job: {SchedulerJobAttr: tmpSchedulerJobAttr,
						policyHandler: New(testPluginName)}},
					Nodes: map[string]NPUNode{vTask.NodeName: tmpNPUNode}}}},
		args: npuDeallocateFuncArgs{task: vTask}, want: "",
	}
}

func buildNPUDeallocateFuncTest() []npuDeallocateFuncTest {
	vTask := test.BuildTestTaskWithAnnotation(test.NPU910CardName, "1", "Ascend910-4")
	tests := []npuDeallocateFuncTest{
		makeNPUDeallocateFuncTest01(vTask),
		makeNPUDeallocateFuncTest02(vTask),
		makeNPUDeallocateFuncTest03(vTask),
		makeNPUDeallocateFuncTest04(vTask),
		makeNPUDeallocateFuncTest05(vTask),
		makeNPUDeallocateFuncTest06(vTask),
		makeNPUDeallocateFuncTest07(vTask),
		makeNPUDeallocateFuncTest08(vTask),
		makeNPUDeallocateFuncTest09(vTask),
	}
	return tests
}

func TestNPUDeallocateFunc(t *testing.T) {
	tests := buildNPUDeallocateFuncTest()
	temp := func(task *api.TaskInfo) string {
		if task == nil {
			return ""
		}
		value, _ := task.Pod.Annotations[test.NPU910CardName]
		return value
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sHandle := &ScheduleHandler{
				NPUPlugins:  tt.fields.NPUPlugins,
				ScheduleEnv: tt.fields.ScheduleEnv,
			}
			sHandle.NPUDeallocateFunc(tt.args.task)
			value := temp(tt.args.task)
			if value != tt.want {
				t.Errorf("NPUDeallocateFunc() got = %v, want %v", value, tt.want)
			}
		})
	}
}
