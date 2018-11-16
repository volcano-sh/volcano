/*
Copyright 2017 The Kubernetes Authors.

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

package api

import (
	"fmt"

	"k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/utils"
	arbv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
)

type TaskID types.UID

type TaskInfo struct {
	UID TaskID
	Job JobID

	Name      string
	Namespace string

	Resreq *Resource

	NodeName string
	Status   TaskStatus
	Priority int32

	Pod *v1.Pod
}

func NewTaskInfo(pod *v1.Pod) *TaskInfo {
	req := EmptyResource()

	// TODO(k82cn): also includes initContainers' resource.
	for _, c := range pod.Spec.Containers {
		req.Add(NewResource(c.Resources.Requests))
	}

	pi := &TaskInfo{
		UID:       TaskID(pod.UID),
		Job:       JobID(utils.GetController(pod)),
		Name:      pod.Name,
		Namespace: pod.Namespace,
		NodeName:  pod.Spec.NodeName,
		Status:    getTaskStatus(pod),
		Priority:  1,

		Pod:    pod,
		Resreq: req,
	}

	if pod.Spec.Priority != nil {
		pi.Priority = *pod.Spec.Priority
	}

	return pi
}

func (pi *TaskInfo) Clone() *TaskInfo {
	return &TaskInfo{
		UID:       pi.UID,
		Job:       pi.Job,
		Name:      pi.Name,
		Namespace: pi.Namespace,
		NodeName:  pi.NodeName,
		Status:    pi.Status,
		Priority:  pi.Priority,
		Pod:       pi.Pod,
		Resreq:    pi.Resreq.Clone(),
	}
}

func (pi TaskInfo) String() string {
	return fmt.Sprintf("Task (%v:%v/%v): job %v, status %v, pri %v, resreq %v",
		pi.UID, pi.Namespace, pi.Name, pi.Job, pi.Status, pi.Priority, pi.Resreq)
}

// JobID is the type of JobInfo's ID.
type JobID types.UID

type tasksMap map[TaskID]*TaskInfo

type JobInfo struct {
	UID JobID

	Name      string
	Namespace string

	Priority int

	NodeSelector map[string]string
	MinAvailable int

	// All tasks of the Job.
	TaskStatusIndex map[TaskStatus]tasksMap
	Tasks           tasksMap

	Allocated    *Resource
	TotalRequest *Resource

	// Candidate hosts for this job.
	Candidates []*NodeInfo

	SchedSpec *arbv1.SchedulingSpec

	// TODO(k82cn): keep backward compatbility, removed it when v1alpha1 finalized.
	PDB *policyv1.PodDisruptionBudget
}

func NewJobInfo(uid JobID) *JobInfo {
	return &JobInfo{
		UID: uid,

		MinAvailable: 0,
		NodeSelector: make(map[string]string),

		Allocated:    EmptyResource(),
		TotalRequest: EmptyResource(),

		TaskStatusIndex: map[TaskStatus]tasksMap{},
		Tasks:           tasksMap{},
	}
}

func (ps *JobInfo) UnsetSchedulingSpec() {
	ps.SchedSpec = nil
}

func (ps *JobInfo) SetSchedulingSpec(spec *arbv1.SchedulingSpec) {
	ps.Name = spec.Name
	ps.Namespace = spec.Namespace
	ps.MinAvailable = spec.Spec.MinAvailable

	for k, v := range spec.Spec.NodeSelector {
		ps.NodeSelector[k] = v
	}

	ps.SchedSpec = spec
}

func (ps *JobInfo) SetPDB(pbd *policyv1.PodDisruptionBudget) {
	ps.Name = pbd.Name
	ps.MinAvailable = int(pbd.Spec.MinAvailable.IntVal)

	ps.PDB = pbd
}

func (ps *JobInfo) UnsetPDB() {
	ps.PDB = nil
}

func (ps *JobInfo) GetTasks(statuses ...TaskStatus) []*TaskInfo {
	var res []*TaskInfo

	for _, status := range statuses {
		if tasks, found := ps.TaskStatusIndex[status]; found {
			for _, task := range tasks {
				res = append(res, task.Clone())
			}
		}
	}

	return res
}

func (ps *JobInfo) addTaskIndex(pi *TaskInfo) {
	if _, found := ps.TaskStatusIndex[pi.Status]; !found {
		ps.TaskStatusIndex[pi.Status] = tasksMap{}
	}

	ps.TaskStatusIndex[pi.Status][pi.UID] = pi
}

func (ps *JobInfo) AddTaskInfo(pi *TaskInfo) {
	ps.Tasks[pi.UID] = pi
	ps.addTaskIndex(pi)

	ps.TotalRequest.Add(pi.Resreq)

	if AllocatedStatus(pi.Status) {
		ps.Allocated.Add(pi.Resreq)
	}
}

func (ps *JobInfo) UpdateTaskStatus(task *TaskInfo, status TaskStatus) error {
	if err := validateStatusUpdate(task.Status, status); err != nil {
		return err
	}

	// Remove the task from the task list firstly
	ps.DeleteTaskInfo(task)

	// Update task's status to the target status
	task.Status = status
	ps.AddTaskInfo(task)

	return nil
}

func (ps *JobInfo) deleteTaskIndex(ti *TaskInfo) {
	if tasks, found := ps.TaskStatusIndex[ti.Status]; found {
		delete(tasks, ti.UID)

		if len(tasks) == 0 {
			delete(ps.TaskStatusIndex, ti.Status)
		}
	}
}

func (ps *JobInfo) DeleteTaskInfo(pi *TaskInfo) error {
	if task, found := ps.Tasks[pi.UID]; found {
		ps.TotalRequest.Sub(task.Resreq)

		if AllocatedStatus(task.Status) {
			ps.Allocated.Sub(task.Resreq)
		}

		delete(ps.Tasks, task.UID)

		ps.deleteTaskIndex(task)
		return nil
	}

	return fmt.Errorf("failed to find task <%v/%v> in job <%v/%v>",
		pi.Namespace, pi.Name, ps.Namespace, ps.Name)
}

func (ps *JobInfo) Clone() *JobInfo {
	info := &JobInfo{
		UID:       ps.UID,
		Name:      ps.Name,
		Namespace: ps.Namespace,

		MinAvailable: ps.MinAvailable,
		NodeSelector: map[string]string{},
		Allocated:    ps.Allocated.Clone(),
		TotalRequest: ps.TotalRequest.Clone(),

		TaskStatusIndex: map[TaskStatus]tasksMap{},
		Tasks:           tasksMap{},
	}

	for k, v := range ps.NodeSelector {
		info.NodeSelector[k] = v
	}

	for _, task := range ps.Tasks {
		info.AddTaskInfo(task.Clone())
	}

	return info
}

func (ps JobInfo) String() string {
	res := ""

	i := 0
	for _, task := range ps.Tasks {
		res = res + fmt.Sprintf("\n\t %d: %v", i, task)
		i++
	}

	return fmt.Sprintf("Job (%v): name %v, minAvailable %d", ps.UID, ps.Name, ps.MinAvailable) + res
}
