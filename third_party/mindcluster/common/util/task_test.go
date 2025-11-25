/*
Copyright(C)2020-2025. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package util is using for the total variable.
*/
package util

import (
	"errors"
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

type DeleteRealPodByTaskTest struct {
	name     string
	npuTask  *NPUTask
	ssn      *framework.Session
	waitTime int64
	wantErr  bool
}

func buildDeleteRealPodByTaskTestCase01() DeleteRealPodByTaskTest {
	test01 := DeleteRealPodByTaskTest{
		name:    "01-DeleteRealPodByTaskTest will return err when task is nil",
		wantErr: true,
	}
	return test01
}

func buildDeleteRealPodByTaskTestCase02() DeleteRealPodByTaskTest {
	test01 := DeleteRealPodByTaskTest{
		name:    "02-DeleteRealPodByTaskTest will return err when POD is nil",
		npuTask: &NPUTask{ReqNPUNum: 1, Name: "task01"},
		ssn: &framework.Session{Jobs: map[api.JobID]*api.JobInfo{"job01": {Tasks: map[api.TaskID]*api.TaskInfo{
			"task01": {Name: "task01", Namespace: "default"}}}}},
		wantErr: true,
	}
	return test01
}

func buildDeleteRealPodByTaskTestCase03() DeleteRealPodByTaskTest {
	test01 := DeleteRealPodByTaskTest{
		name:    "03-DeleteRealPodByTaskTest will return err when ssn is nil",
		npuTask: &NPUTask{ReqNPUNum: 1, Name: "task01"},
		ssn:     nil,
		wantErr: true,
	}
	return test01
}

func buildDeleteRealPodByTaskTestCase() []DeleteRealPodByTaskTest {
	tests := []DeleteRealPodByTaskTest{
		buildDeleteRealPodByTaskTestCase01(),
		buildDeleteRealPodByTaskTestCase02(),
		buildDeleteRealPodByTaskTestCase03(),
	}
	return tests
}

func TestDeleteRealPodByTask(t *testing.T) {
	tests := buildDeleteRealPodByTaskTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.npuTask.DeleteRealPodByTask(tt.ssn, tt.waitTime); (err != nil) != tt.wantErr {
				t.Errorf("DeleteRealPodByTask() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type EvictJobByTaskTest struct {
	name     string
	ssn      *framework.Session
	taskName string
	reason   string
	asTask   *NPUTask
	wantErr  bool
}

func buildEvictJobByTaskTestCase01() EvictJobByTaskTest {
	test01 := EvictJobByTaskTest{
		name:    "01-EvictJobByTaskTest will return err when Task is nil",
		asTask:  nil,
		wantErr: true,
	}
	return test01
}

func buildEvictJobByTaskTestCase02() EvictJobByTaskTest {
	test02 := EvictJobByTaskTest{
		name:    "02-EvictJobByTaskTest will return err when ssn is nil",
		asTask:  &NPUTask{ReqNPUNum: 1, Name: "task01"},
		ssn:     nil,
		wantErr: true,
	}
	return test02
}

func buildEvictJobByTaskTestCase03() EvictJobByTaskTest {
	test03 := EvictJobByTaskTest{
		name:   "03-EvictJobByTaskTest will return err when ssn is nil",
		asTask: &NPUTask{ReqNPUNum: 1, Name: "task01"},
		ssn: &framework.Session{Jobs: map[api.JobID]*api.JobInfo{"job01": {Tasks: map[api.TaskID]*api.TaskInfo{
			"task01": {Name: "task01",
				Namespace: "default",
				Pod: &v1.Pod{Status: v1.PodStatus{
					Conditions: []v1.PodCondition{{Message: "PodCondition-message"}}}}}}}}},
		taskName: "task01",
		wantErr:  true,
	}
	return test03
}

func buildEvictJobByTaskTestCase() []EvictJobByTaskTest {
	tests := []EvictJobByTaskTest{
		buildEvictJobByTaskTestCase01(),
		buildEvictJobByTaskTestCase02(),
		buildEvictJobByTaskTestCase03(),
	}
	return tests
}

func TestEvictJobByTask(t *testing.T) {
	tests := buildEvictJobByTaskTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patch := gomonkey.ApplyMethod(reflect.TypeOf(tt.ssn),
				"Evict", func(*framework.Session, *api.TaskInfo, string) error {
					return errors.New("mock error Evict")
				})

			defer patch.Reset()

			if err := tt.asTask.EvictJobByTask(tt.ssn, tt.reason, tt.taskName); (err != nil) != tt.wantErr {
				t.Errorf("EvictJobByTask() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestForceDeletePodByTaskInf(t *testing.T) {
	type args struct {
		ssn      *framework.Session
		reason   string
		nodeName string
	}
	tests := []struct {
		name    string
		asTask  *NPUTask
		args    args
		wantErr bool
	}{
		{
			name: "01-ForceDeletePodByTaskInf will return err when ssn is nil",
			asTask: &NPUTask{
				Name: "task01",
				VTask: &VTask{
					Allocated: TaskAllocated{NodeName: "master"},
				},
			},
			args:    args{ssn: nil},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			if err := tt.asTask.ForceDeletePodByTaskInf(tt.args.ssn,
				tt.args.reason, tt.args.nodeName); (err != nil) != tt.wantErr {
				t.Errorf("ForceDeletePodByTaskInf() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type IsTaskInItsNodeTest struct {
	name     string
	asTask   *NPUTask
	ssn      *framework.Session
	nodeName string
	want     bool
}

func buildIsTaskInItsNodeTestCase() []IsTaskInItsNodeTest {
	tests := []IsTaskInItsNodeTest{
		{
			name: "01-IsTaskInItsNode will return false when Nodes is nil",
			asTask: &NPUTask{
				Name: "task01",
				VTask: &VTask{
					Allocated: TaskAllocated{NodeName: "master"},
				},
			},
			ssn:  &framework.Session{Nodes: map[string]*api.NodeInfo{}},
			want: false,
		},
		{
			name: "02-IsTaskInItsNode will return false when NodeInfo is nil",
			asTask: &NPUTask{
				Name: "task01",
				VTask: &VTask{
					Allocated: TaskAllocated{NodeName: "master"},
				},
			},
			ssn:      &framework.Session{Nodes: map[string]*api.NodeInfo{"master": {}}},
			nodeName: "master",
			want:     false,
		},
	}
	return tests
}

func TestIsTaskInItsNode(t *testing.T) {
	tests := buildIsTaskInItsNodeTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.asTask.IsTaskInItsNode(tt.ssn, tt.nodeName); got != tt.want {
				t.Errorf("IsTaskInItsNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

type GetVTaskUseTemplateTest struct {
	name    string
	taskInf *api.TaskInfo
	want    string
	wantErr error
}

func buildGetVTaskUseTemplateTestCase() []GetVTaskUseTemplateTest {
	tests := []GetVTaskUseTemplateTest{
		{
			name:    "01-GetVTaskUseTemplate will return err when pod is empty",
			taskInf: &api.TaskInfo{Pod: &v1.Pod{}},
			want:    "",
			wantErr: errors.New("'s anno has no huawei.com/npu-core"),
		},
		{
			name: "02-GetVTaskUseTemplate will return err when task is not VTask",
			taskInf: &api.TaskInfo{Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{AscendNPUCore: NPU910CardName},
			}}},
			want:    "",
			wantErr: errors.New(" not dyCut task :huawei.com/Ascend910"),
		},
		{
			name: "03-GetVTaskUseTemplate will return nil when task is VTask",
			taskInf: &api.TaskInfo{Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{AscendNPUCore: "vir-01"},
			}}},
			want:    "01",
			wantErr: nil,
		},
	}
	return tests
}

func TestGetVTaskUseTemplate(t *testing.T) {
	tests := buildGetVTaskUseTemplateTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetVTaskUseTemplate(tt.taskInf)
			if !reflect.DeepEqual(err, tt.wantErr) {
				t.Errorf("GetVTaskUseTemplate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetVTaskUseTemplate() got = %v, want %v", got, tt.want)
			}
		})
	}
}

type SetVTaskUseCardIDsTest struct {
	name string
	vt   *VTask
	want []int
}

func buildSetVTaskUseCardIDsTestCase() []SetVTaskUseCardIDsTest {
	tests := []SetVTaskUseCardIDsTest{
		{
			name: "01-SetVTaskUseCardIDs will return nil when vt is empty",
			vt:   &VTask{},
			want: nil,
		},
		{
			name: "02-SetVTaskUseCardIDs will return ids when PhysicsName is not empty",
			vt:   &VTask{Allocated: TaskAllocated{PhysicsName: []string{"", "Ascend310P-2c-100-1_1"}}},
			want: []int{1},
		},
	}
	return tests

}

func TestSetVTaskUseCardIDs(t *testing.T) {
	tests := buildSetVTaskUseCardIDsTestCase()
	vTask := &VTask{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vTask = tt.vt
			vTask.setVTaskUseCardIDs()
			if !reflect.DeepEqual(vTask.Allocated.CardName, tt.want) {
				t.Errorf("setVTaskUseCardIDs = %v, want %v", vTask.Allocated.CardName, tt.want)
			}
		})
	}
}

type SetVTaskAllocatedTest struct {
	name     string
	asTask   *NPUTask
	taskInfo *api.TaskInfo
	want     TaskAllocated
}

func buildSetVTaskAllocatedTestCase01() SetVTaskAllocatedTest {
	test01 := SetVTaskAllocatedTest{
		name:     "01-SetVTaskAllocated will return Allocated when Status is running, wrBack or failed",
		asTask:   &NPUTask{VTask: &VTask{Status: TaskStatusRunning}},
		taskInfo: &api.TaskInfo{Pod: &v1.Pod{}},
		want: TaskAllocated{
			NodeName:    "master",
			CardName:    []int{1, 2, 3, 4},
			PhysicsName: []string{"1", "2", "3", "4"},
		},
	}
	test01.taskInfo.NodeName = "master"
	test01.taskInfo.Pod.Annotations = map[string]string{AscendNPUPodRealUse: "1,2,3,4"}
	return test01
}

func buildSetVTaskAllocatedTestCase02() SetVTaskAllocatedTest {
	test02 := SetVTaskAllocatedTest{
		name:     "01-SetVTaskAllocated will return Allocated when Status is Allocate",
		asTask:   &NPUTask{VTask: &VTask{Status: TaskStatusAllocate}},
		taskInfo: &api.TaskInfo{Pod: &v1.Pod{}},
		want: TaskAllocated{
			NodeName:    "master",
			CardName:    nil,
			PhysicsName: nil,
		},
	}
	test02.taskInfo.NodeName = "master"
	test02.taskInfo.Pod.Annotations = map[string]string{AscendNPUPodRealUse: "1,2,3,4"}
	return test02
}

func buildSetVTaskAllocatedTestCase03() SetVTaskAllocatedTest {
	test03 := SetVTaskAllocatedTest{
		name:     "01-SetVTaskAllocated will return Allocated when Status is other",
		asTask:   &NPUTask{VTask: &VTask{Status: 0}},
		taskInfo: &api.TaskInfo{Pod: &v1.Pod{}},
		want: TaskAllocated{
			NodeName:    "",
			CardName:    nil,
			PhysicsName: nil,
		},
	}
	test03.taskInfo.NodeName = "master"
	test03.taskInfo.Pod.Annotations = map[string]string{AscendNPUPodRealUse: "1,2,3,4"}
	return test03
}

func buildSetVTaskAllocatedTestCase() []SetVTaskAllocatedTest {
	return []SetVTaskAllocatedTest{
		buildSetVTaskAllocatedTestCase01(),
		buildSetVTaskAllocatedTestCase02(),
		buildSetVTaskAllocatedTestCase03(),
	}
}

func TestSetVTaskAllocated(t *testing.T) {
	tests := buildSetVTaskAllocatedTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.asTask.setVTaskAllocated(tt.taskInfo)
			if !reflect.DeepEqual(tt.asTask.VTask.Allocated, tt.want) {
				t.Errorf("setVTaskAllocated = %v, want %v", tt.asTask.Allocated, tt.want)
			}
		})
	}
}

type SetVTaskStatusFromInfoTest struct {
	name    string
	asTask  *NPUTask
	taskInf *api.TaskInfo
	want    int
}

func buildSetVTaskStatusFromInfoTestCase01() SetVTaskStatusFromInfoTest {
	test01 := SetVTaskStatusFromInfoTest{
		name:    "01-SetVTaskStatusFromInfo will return nil when pod is nil",
		asTask:  &NPUTask{VTask: &VTask{}},
		taskInf: &api.TaskInfo{Pod: &v1.Pod{}},
		want:    TaskStatusInit,
	}
	return test01
}

func buildSetVTaskStatusFromInfoTestCase02() SetVTaskStatusFromInfoTest {
	test02 := SetVTaskStatusFromInfoTest{
		name:   "02-SetVTaskStatusFromInfo will return nil when AscendNPUPodRealUse is nil",
		asTask: &NPUTask{VTask: &VTask{}},
		taskInf: &api.TaskInfo{Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				AscendNPUCore: NPU910CardName},
		}}},
		want: TaskStatusAllocate,
	}
	return test02
}

func buildSetVTaskStatusFromInfoTestCase03() SetVTaskStatusFromInfoTest {
	test03 := SetVTaskStatusFromInfoTest{
		name:   "02-SetVTaskStatusFromInfo will return nil when taskInf.Status is running",
		asTask: &NPUTask{VTask: &VTask{}},
		taskInf: &api.TaskInfo{Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				AscendNPUCore:       NPU910CardName,
				AscendNPUPodRealUse: NPU910CardName},
		}}},
		want: TaskStatusRunning,
	}
	test03.taskInf.Status = api.Running
	return test03
}

func buildSetVTaskStatusFromInfoTestCase04() SetVTaskStatusFromInfoTest {
	test04 := SetVTaskStatusFromInfoTest{
		name:   "02-SetVTaskStatusFromInfo will return nil when taskInf.Status is running",
		asTask: &NPUTask{VTask: &VTask{}},
		taskInf: &api.TaskInfo{Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				AscendNPUCore:       NPU910CardName,
				AscendNPUPodRealUse: NPU910CardName},
		}}},
		want: TaskStatusFailed,
	}
	test04.taskInf.Status = api.Failed
	return test04
}

func buildSetVTaskStatusFromInfoTestCase() []SetVTaskStatusFromInfoTest {
	tests := []SetVTaskStatusFromInfoTest{
		buildSetVTaskStatusFromInfoTestCase01(),
		buildSetVTaskStatusFromInfoTestCase02(),
		buildSetVTaskStatusFromInfoTestCase03(),
		buildSetVTaskStatusFromInfoTestCase04(),
	}
	return tests
}

func TestSetVTaskStatusFromInfo(t *testing.T) {
	tests := buildSetVTaskStatusFromInfoTestCase()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.asTask.setVTaskStatusFromInfo(tt.taskInf)
			if err != nil {
				return
			}
			if !reflect.DeepEqual(tt.asTask.Status, tt.want) {
				t.Errorf("setVTaskStatusFromInfo()  = %v, wantErr %v", tt.asTask.Status, tt.want)
			}
		})
	}
}

func TestNPUTaskIsVNPUTask(t *testing.T) {
	tests := []struct {
		name string
		task *NPUTask
		want bool
	}{
		{
			name: "01-IsVNPUTask nil task",
			task: nil,
			want: false,
		},
		{
			name: "02-IsVNPUTask non vnpu task",
			task: &NPUTask{ReqNPUName: Ascend910bName},
			want: false,
		},
		{
			name: "03-IsVNPUTask vnpu task",
			task: &NPUTask{ReqNPUName: AscendNPUCore},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.task.IsVNPUTask(); got != tt.want {
				t.Errorf("IsVNPUTask() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNPUTaskInitVTask(t *testing.T) {
	tests := []struct {
		name       string
		task       *NPUTask
		taskInfo   *api.TaskInfo
		taskStatus int
		wantErr    bool
	}{
		{
			name: "01-InitVTask task whole",
			task: &NPUTask{ReqNPUName: Ascend910bName, VTask: &VTask{}},
			taskInfo: &api.TaskInfo{Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{AscendNPUPodRealUse: Ascend910bName}}}},
			taskStatus: TaskStatusInit,
			wantErr:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.task.InitVTask(tt.taskInfo); (err != nil) != tt.wantErr || (tt.task.Status != tt.taskStatus) {
				t.Errorf("InitVTask() error = %v, wantErr %v, getTaskStatus:%v, wantTaskStatsu:%v",
					err, tt.wantErr, tt.task.Status, tt.taskStatus)
			}
		})
	}
}

func TestGetVTaskUsePhysicsNamesByInfo(t *testing.T) {
	tests := []struct {
		name     string
		taskInfo *api.TaskInfo
		want     []string
	}{
		{
			name: "01-getVTaskUsePhysicsNamesByInfo exist huawei.com/AscendReal annotation",
			taskInfo: &api.TaskInfo{Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{AscendNPUPodRealUse: "0,1"},
			}}},
			want: []string{"0", "1"},
		},
		{
			name: "02-getVTaskUsePhysicsNamesByInfo not exist huawei.com/AscendReal annotation",
			taskInfo: &api.TaskInfo{Pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{Ascend910bName: "0,1"},
			}}},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getVTaskUsePhysicsNamesByInfo(tt.taskInfo); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getVTaskUsePhysicsNamesByInfo() = %v, want %v", got, tt.want)
			}
		})
	}
}
