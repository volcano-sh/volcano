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
Package vnpu is using for HuaWei Ascend pin vnpu allocation.
*/
package vnpu

import (
	"errors"
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	test2 "volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/test"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

type preStartActionTestCase struct {
	name    string
	env     *plugin.ScheduleEnv
	ssn     *framework.Session
	wantErr bool
}

func buildPreStartActionTestCase01() preStartActionTestCase {
	return preStartActionTestCase{
		name:    "01 will return err when ssn is nil",
		env:     &plugin.ScheduleEnv{},
		ssn:     nil,
		wantErr: true,
	}
}

func buildPreStartActionTestCase02() preStartActionTestCase {
	return preStartActionTestCase{
		name:    "02 will return nil when ssn is not nil",
		env:     test2.FakeScheduleEnv(),
		ssn:     test.FakeNormalSSN(nil),
		wantErr: false,
	}
}

func buildPreStartActionTestCases() []preStartActionTestCase {
	return []preStartActionTestCase{
		buildPreStartActionTestCase01(),
		buildPreStartActionTestCase02(),
	}
}

func TestVirtualNPUPreStartAction(t *testing.T) {
	for _, tt := range buildPreStartActionTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			tp := &VirtualNPU{}
			if err := tp.PreStartAction(tt.env, tt.ssn); (err != nil) != tt.wantErr {
				t.Errorf("PreStartAction() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type TestGetAllDyJobsTest struct {
	Name string
	Jobs map[api.JobID]plugin.SchedulerJob
	Want map[api.JobID]plugin.SchedulerJob
}

func buildTestGetAllDyJobsTestCase() []TestGetAllDyJobsTest {
	tests := []TestGetAllDyJobsTest{
		{
			Name: "01-getAllDyJobs will return jobMap when VJob is nil",
			Jobs: map[api.JobID]plugin.SchedulerJob{},
			Want: map[api.JobID]plugin.SchedulerJob{},
		},
		{
			Name: "01-getAllDyJobs will return jobMap when VJob is nil",
			Jobs: map[api.JobID]plugin.SchedulerJob{
				"Job01": {SchedulerJobAttr: util.SchedulerJobAttr{
					NPUJob: &util.NPUJob{ReqNPUName: util.AscendNPUCore}}}},
			Want: map[api.JobID]plugin.SchedulerJob{"Job01": {SchedulerJobAttr: util.SchedulerJobAttr{
				NPUJob: &util.NPUJob{ReqNPUName: util.AscendNPUCore}}}},
		},
	}
	return tests
}

func TestGetAllDyJobs(t *testing.T) {
	n := VirtualNPU{}
	tests := buildTestGetAllDyJobsTestCase()
	for _, tt := range tests {
		env := plugin.ScheduleEnv{}
		env.Jobs = tt.Jobs
		t.Run(tt.Name, func(t *testing.T) {
			if got := n.getAllDyJobs(&env); !reflect.DeepEqual(got, tt.Want) {
				t.Errorf("ValidNPUJob() got = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestGetFailedDyTasksFromJobs(t *testing.T) {
	tests := []struct {
		Name  string
		vJobs map[api.JobID]plugin.SchedulerJob
		Want  map[api.TaskID]util.NPUTask
	}{
		{
			Name: "01-getFailedDyTasksFromJobs will return vTask when call this function",
			vJobs: map[api.JobID]plugin.SchedulerJob{
				"vjob01": {
					SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{
						Tasks: map[api.TaskID]util.NPUTask{
							"Task01": {VTask: &util.VTask{Status: util.TaskStatusFailed}}}}},
				},
			},
			Want: map[api.TaskID]util.NPUTask{"Task01": {VTask: &util.VTask{Status: util.TaskStatusFailed}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := getFailedDyTasksFromJobs(tt.vJobs); !reflect.DeepEqual(got, tt.Want) {
				t.Errorf("GetFailedDyTasksFromJobs() got = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestGetDyFailedNamespaces(t *testing.T) {
	tests := []struct {
		Name string
		VT   map[api.TaskID]util.NPUTask
		Want map[string]struct{}
	}{
		{
			Name: "01-testGetDyFailedNamespaces will return nsMap when when call this function",
			VT: map[api.TaskID]util.NPUTask{
				"task01": {NameSpace: "default"},
				"task02": {NameSpace: "vcjob"},
				"task03": {NameSpace: "kube-system"},
			},
			Want: map[string]struct{}{
				"default":     {},
				"vcjob":       {},
				"kube-system": {},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := getDyFailedNamespaces(tt.VT); !reflect.DeepEqual(got, tt.Want) {
				t.Errorf("GetDyFailedNamespaces() got = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestGetAllDyFailedTasks(t *testing.T) {
	tests := []struct {
		Name  string
		SSN   *framework.Session
		nsMap map[string]struct{}
		Want  []api.TaskID
	}{
		{
			Name: "01-testGetAllDyFailedTasks will return IDs when when call this function",
			SSN:  &framework.Session{},
			nsMap: map[string]struct{}{
				"default":     {},
				"vcjob":       {},
				"kube-system": {},
			},
			Want: []api.TaskID{"0001", "0001", "0001"},
		},
	}

	patch := gomonkey.ApplyFunc(GetSegmentFailureTaskIDs,
		func(ssn *framework.Session, namespace string) []api.TaskID {
			return []api.TaskID{"0001"}
		})

	defer patch.Reset()

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := getAllDyFailedTasks(tt.SSN, tt.nsMap); !reflect.DeepEqual(got, tt.Want) {
				t.Errorf("GetAllDyFailedTasks() got = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestGetDyFailedTaskIDsInFaileds(t *testing.T) {
	tests := []struct {
		Name string
		VT   map[api.TaskID]util.NPUTask
		Ids  []api.TaskID
		Want []api.TaskID
	}{
		{
			Name: "01-testGetDyFailedTaskIDsInFaileds will return tIDs when call this function",
			VT: map[api.TaskID]util.NPUTask{
				"task01": {NameSpace: "default"},
				"task02": {NameSpace: "vcjob"},
				"task03": {NameSpace: "kube-system"},
			},
			Ids:  []api.TaskID{"task01", "task02", "task03"},
			Want: []api.TaskID{"task01", "task02", "task03"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := getDyFailedTaskIDsInFaileds(tt.Ids, tt.VT); !reflect.DeepEqual(got, tt.Want) {
				t.Errorf("GetDyFailedTaskIDsInFaileds() got = %v, want %v", got, tt.Want)
			}
		})
	}
}

func TestGetDyFailedTasksFromFailed(t *testing.T) {
	tests := []struct {
		Name string
		ssn  *framework.Session
		VT   map[api.TaskID]util.NPUTask
		Want []api.TaskID
	}{
		{
			Name: "01-getDyFailedTasksFromFailed will return taskId when call this function",
			ssn:  &framework.Session{},
			VT: map[api.TaskID]util.NPUTask{
				"task01": {NameSpace: "default"},
			},
			Want: []api.TaskID{"task01"},
		},
	}

	patch := gomonkey.ApplyFunc(GetSegmentFailureTaskIDs,
		func(ssn *framework.Session, namespace string) []api.TaskID {
			return []api.TaskID{"task01"}
		})

	defer patch.Reset()

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if got := getDyFailedTasksFromFailed(tt.ssn, tt.VT); !reflect.DeepEqual(got, tt.Want) {
				t.Errorf("GetDyFailedTasksFromFailed() got = %v, want %v", got, tt.Want)
			}
		})
	}
}

type TestGetRestartDyTasksFromJobsTest struct {
	Name  string
	Tasks []api.TaskID
	VJob  map[api.JobID]plugin.SchedulerJob
	ssn   *framework.Session
	Want  []util.NPUTask
}

func buildTestGetRestartDyTasksFromJobsTestCase() []TestGetRestartDyTasksFromJobsTest {
	tests := []TestGetRestartDyTasksFromJobsTest{
		{
			Name:  "01-GetRestartDyTasksFromJobs will return nil  when vjob is nil",
			Tasks: []api.TaskID{},
			VJob:  nil,
			ssn:   nil,
			Want:  nil,
		},
		{
			Name:  "02-GetRestartDyTasksFromJobs will return nil  when fTIDs is 0",
			Tasks: []api.TaskID{"task01"},
			VJob:  nil,
			ssn:   nil,
			Want:  nil,
		},
		{
			Name:  "03-GetRestartDyTasksFromJobs will return nSlice  when call this method",
			Tasks: []api.TaskID{"task01"},
			VJob: map[api.JobID]plugin.SchedulerJob{
				"vjob01": {
					SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{
						Tasks: map[api.TaskID]util.NPUTask{
							"task01": {VTask: &util.VTask{Status: util.TaskStatusFailed}}}}}}},
			Want: []util.NPUTask{{VTask: &util.VTask{Status: util.TaskStatusFailed}}},
		},
	}
	return tests

}

func TestGetRestartDyTasksFromJobs(t *testing.T) {
	n := VirtualNPU{}
	tests := buildTestGetRestartDyTasksFromJobsTestCase()
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			patch := gomonkey.ApplyFunc(getDyFailedTasksFromFailed,
				func(ssn *framework.Session, vT map[api.TaskID]util.NPUTask) []api.TaskID {
					return tt.Tasks
				})
			defer patch.Reset()
			if got := n.getRestartDyTasksFromJobs(tt.VJob, tt.ssn); !reflect.DeepEqual(got, tt.Want) {
				t.Errorf("GetAllDyFailedTasks() got = %v, want %v", got, tt.Want)
			}
		})
	}
}

type InitDyCutConCacheByJobInfoTest struct {
	Name    string
	JobInfo *api.JobInfo
	VJob    plugin.SchedulerJob
	NPUJob  *util.NPUJob
	Nodes   map[string]map[string]map[api.TaskID]struct{}
	WantErr error
}

func buildInitDyCutConCacheByJobInfoTestCase() []InitDyCutConCacheByJobInfoTest {
	tests := []InitDyCutConCacheByJobInfoTest{
		{
			Name:    "01-InitDyCutConCacheByJobInfo will return err when jobInfo is nil",
			WantErr: errors.New("initDyCutConCacheByJobInfo :invalid argument"),
		},
		{
			Name:    "02-InitDyCutConCacheByJobInfo will return nil when taskInfo do not exist ",
			JobInfo: test.FakeNormalTestJob("job01", 0),
			NPUJob: &util.NPUJob{
				Tasks: map[api.TaskID]util.NPUTask{
					"task01": {NameSpace: "default", VTask: &util.VTask{Status: util.TaskStatusAllocate}},
				},
			},
			WantErr: nil,
		},
		{
			Name: "03-InitDyCutConCacheByJobInfo will return nil when taskInfo exist ",
			JobInfo: &api.JobInfo{Tasks: map[api.TaskID]*api.TaskInfo{
				"task01": {Name: "task01-test"},
			}},
			NPUJob: &util.NPUJob{
				Tasks: map[api.TaskID]util.NPUTask{
					"task01": {NameSpace: "default", VTask: &util.VTask{Status: util.TaskStatusAllocate}},
				},
			},
			WantErr: nil,
		},
	}
	return tests
}

func TestInitDyCutConCacheByJobInfo(t *testing.T) {
	tests := buildInitDyCutConCacheByJobInfoTestCase()

	patch := gomonkey.ApplyFunc(util.GetVTaskUseTemplate, func(taskInf *api.TaskInfo) (string, error) {
		return "", errors.New("task01's anno has no huawei.com/npu-core")
	})
	defer patch.Reset()

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.VJob.NPUJob = tt.NPUJob
			if err := initDyCutConCacheByJobInfo(tt.Nodes, tt.JobInfo, tt.VJob); !reflect.DeepEqual(err, tt.WantErr) {
				t.Errorf("InitDyCutConCacheByJobInfo() err = %v want %v", err, tt.WantErr)
			}
		})
	}
}

type InitConcacheByTemplateTest struct {
	Name     string
	nodes    map[string]map[string]map[api.TaskID]struct{}
	vT       util.NPUTask
	template string
	taskID   api.TaskID
	WantNode map[string]map[string]map[api.TaskID]struct{}
}

func buildInitConcacheByTemplateTestCase() []InitConcacheByTemplateTest {
	tests := []InitConcacheByTemplateTest{
		{
			Name:     "01-InitConcacheByTemplate will return nil when node is nil",
			nodes:    nil,
			vT:       util.NPUTask{},
			template: util.NPU310PCardName,
			taskID:   "task01",
			WantNode: nil,
		},
		{
			Name: "02-InitConcacheByTemplate will return template node when nodeName is not nil",
			nodes: map[string]map[string]map[api.TaskID]struct{}{
				"node1": {"node1-1": {"task01": {}}},
			},
			vT:       util.NPUTask{VTask: &util.VTask{}},
			template: util.NPU310PCardName,
			taskID:   "task01",
			WantNode: map[string]map[string]map[api.TaskID]struct{}{
				"node name test01": {
					"huawei.com/Ascend310P": map[api.TaskID]struct{}{"task01": {}},
				},
				"node1": {"node1-1": map[api.TaskID]struct{}{"task01": {}}},
			},
		},
		{
			Name: "03-InitConcacheByTemplate will return node when nodeName is nil",
			nodes: map[string]map[string]map[api.TaskID]struct{}{
				"node1": {"node1-1": {"task01": {}}},
			},
			vT:       util.NPUTask{VTask: &util.VTask{}},
			template: util.NPU310PCardName,
			taskID:   "task01",
			WantNode: map[string]map[string]map[api.TaskID]struct{}{
				"node1": {"node1-1": {"task01": {}}},
			},
		},
	}
	tests[1].vT.Allocated.NodeName = "node name test01"
	return tests
}

func TestInitConcacheByTemplate(t *testing.T) {
	tests := buildInitConcacheByTemplateTestCase()
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			if initConcacheByTemplate(tt.nodes, tt.vT, tt.template, tt.taskID); !reflect.DeepEqual(tt.nodes, tt.WantNode) {
				t.Errorf("initConcacheByTemplate() tt.nodes = %v want %v", tt.nodes, tt.WantNode)
			}
		})
	}
}

type InitConCacheTest struct {
	Name    string
	ssn     *framework.Session
	vNPU    *VirtualNPU
	jobs    map[api.JobID]plugin.SchedulerJob
	WantErr error
}

func buildInitConCacheTestCase01() InitConCacheTest {
	test02 := InitConCacheTest{
		Name:    "01-InitConCache will return nil when jobOk is false",
		ssn:     test.FakeNormalSSN(nil),
		vNPU:    &VirtualNPU{},
		jobs:    map[api.JobID]plugin.SchedulerJob{"Job02": {}},
		WantErr: nil,
	}
	return test02
}

func buildInitConCacheTestCase02() InitConCacheTest {
	test03 := InitConCacheTest{
		Name: "02-InitConCache will return nil when jobOk is true",
		ssn:  test.FakeNormalSSN(nil),
		vNPU: &VirtualNPU{},
		jobs: map[api.JobID]plugin.SchedulerJob{
			"Job03": {SchedulerJobAttr: util.SchedulerJobAttr{NPUJob: &util.NPUJob{}}}},
		WantErr: nil,
	}
	test03.ssn.Jobs = map[api.JobID]*api.JobInfo{
		"Job03": {Name: "Job03"},
	}
	return test03
}

func buildInitConCacheTestCase() []InitConCacheTest {
	return []InitConCacheTest{
		buildInitConCacheTestCase01(),
		buildInitConCacheTestCase02(),
	}
}

func TestInitConCache(t *testing.T) {
	npu := &VirtualNPU{}
	tests := buildInitConCacheTestCase()
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			env := plugin.ScheduleEnv{}
			env.Jobs = tt.jobs
			if err := npu.initConCache(&env, tt.ssn); !reflect.DeepEqual(err, tt.WantErr) {
				t.Errorf("initConCache() error = %v, wantErr %v", err, tt.WantErr)
			}
		})
	}
}

type TestDeleteDyCutErrTasksTest struct {
	Name    string
	ssn     *framework.Session
	Jobs    map[api.JobID]plugin.SchedulerJob
	WantErr error
}

func buildTestDeleteDyCutErrTasksTestCase() []TestDeleteDyCutErrTasksTest {
	tests := []TestDeleteDyCutErrTasksTest{
		{
			Name:    "01-DeleteDyCutErrTasks will return nil when nTasks is nil",
			ssn:     nil,
			WantErr: nil,
		},
		{
			Name: "02-DeleteDyCutErrTasks will return nil when VTask is nil",
			ssn:  nil,
			Jobs: map[api.JobID]plugin.SchedulerJob{"Job01": {SchedulerJobAttr: util.SchedulerJobAttr{
				NPUJob: &util.NPUJob{
					ReqNPUName: util.AscendNPUCore,
					Tasks: map[api.TaskID]util.NPUTask{
						"task01": {VTask: &util.VTask{Status: util.TaskStatusFailed}}}}}}},
			WantErr: nil,
		},
		{
			Name: "03-DeleteDyCutErrTasks will return nil when VTask is not nil",
			ssn:  nil,
			Jobs: map[api.JobID]plugin.SchedulerJob{"Job01": {SchedulerJobAttr: util.SchedulerJobAttr{
				NPUJob: &util.NPUJob{
					ReqNPUName: util.AscendNPUCore,
					Tasks: map[api.TaskID]util.NPUTask{
						"task01": {VTask: &util.VTask{Status: util.TaskStatusFailed}}}}}}},
			WantErr: nil,
		},
	}
	return tests
}

func TestDeleteDyCutErrTasks(t *testing.T) {
	n := VirtualNPU{}

	patch := gomonkey.ApplyFunc(getDyFailedTasksFromFailed,
		func(ssn *framework.Session, vT map[api.TaskID]util.NPUTask) []api.TaskID {
			return []api.TaskID{"task01"}
		})

	defer patch.Reset()

	tests := buildTestDeleteDyCutErrTasksTestCase()
	for _, tt := range tests {
		env := plugin.ScheduleEnv{}
		env.Jobs = tt.Jobs
		t.Run(tt.Name, func(t *testing.T) {
			if err := n.deleteDyCutErrTasks(&env, tt.ssn); !reflect.DeepEqual(err, tt.WantErr) {
				t.Errorf("deleteDyCutErrTasks() err = %v, want %v", err, tt.WantErr)
			}
		})
	}
}
