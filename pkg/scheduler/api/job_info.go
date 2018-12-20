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
	"sort"
	"strings"

	"k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubernetes-sigs/kube-batch/cmd/kube-batch/app/options"
	arbcorev1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/apis/utils"
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

func getJobID(pod *v1.Pod) JobID {
	if len(pod.Annotations) != 0 {
		if gn, found := pod.Annotations[arbcorev1.GroupNameAnnotationKey]; found && len(gn) != 0 {
			// Make sure Pod and PodGroup belong to the same namespace.
			jobID := fmt.Sprintf("%s/%s", pod.Namespace, gn)
			return JobID(jobID)
		}
	}
	return JobID(utils.GetController(pod))
}

func NewTaskInfo(pod *v1.Pod) *TaskInfo {
	req := EmptyResource()

	// TODO(k82cn): also includes initContainers' resource.
	for _, c := range pod.Spec.Containers {
		req.Add(NewResource(c.Resources.Requests))
	}

	ti := &TaskInfo{
		UID:       TaskID(pod.UID),
		Job:       getJobID(pod),
		Name:      pod.Name,
		Namespace: pod.Namespace,
		NodeName:  pod.Spec.NodeName,
		Status:    getTaskStatus(pod),
		Priority:  1,
		Pod:       pod,
		Resreq:    req,
	}

	if pod.Spec.Priority != nil {
		ti.Priority = *pod.Spec.Priority
	}

	return ti
}

func (ti *TaskInfo) Clone() *TaskInfo {
	return &TaskInfo{
		UID:       ti.UID,
		Job:       ti.Job,
		Name:      ti.Name,
		Namespace: ti.Namespace,
		NodeName:  ti.NodeName,
		Status:    ti.Status,
		Priority:  ti.Priority,
		Pod:       ti.Pod,
		Resreq:    ti.Resreq.Clone(),
	}
}

func (ti TaskInfo) String() string {
	return fmt.Sprintf("Task (%v:%v/%v): job %v, status %v, pri %v, resreq %v",
		ti.UID, ti.Namespace, ti.Name, ti.Job, ti.Status, ti.Priority, ti.Resreq)
}

// JobID is the type of JobInfo's ID.
type JobID types.UID

type tasksMap map[TaskID]*TaskInfo

type NodeResourceMap map[string]*Resource

type JobInfo struct {
	UID JobID

	Name      string
	Namespace string

	Queue QueueID

	Priority int

	NodeSelector map[string]string
	MinAvailable int32

	NodesFitDelta NodeResourceMap

	// All tasks of the Job.
	TaskStatusIndex map[TaskStatus]tasksMap
	Tasks           tasksMap

	Allocated    *Resource
	TotalRequest *Resource

	CreationTimestamp metav1.Time
	PodGroup          *arbcorev1.PodGroup

	// TODO(k82cn): keep backward compatbility, removed it when v1alpha1 finalized.
	PDB *policyv1.PodDisruptionBudget
}

func NewJobInfo(uid JobID) *JobInfo {
	return &JobInfo{
		UID: uid,

		MinAvailable:  0,
		NodeSelector:  make(map[string]string),
		NodesFitDelta: make(NodeResourceMap),
		Allocated:     EmptyResource(),
		TotalRequest:  EmptyResource(),

		TaskStatusIndex: map[TaskStatus]tasksMap{},
		Tasks:           tasksMap{},
	}
}

func (ji *JobInfo) UnsetPodGroup() {
	ji.PodGroup = nil
}

func (ji *JobInfo) SetPodGroup(pg *arbcorev1.PodGroup) {
	ji.Name = pg.Name
	ji.Namespace = pg.Namespace
	ji.MinAvailable = pg.Spec.MinMember

	//set queue name based on the available information
	//in the following priority order:
	// 1. queue name from PodGroup spec (if available)
	// 2. queue name from default-queue command line option (if specified)
	// 3. namespace name
	if len(pg.Spec.Queue) > 0 {
		ji.Queue = QueueID(pg.Spec.Queue)
	} else if len(options.Options().DefaultQueue) > 0 {
		ji.Queue = QueueID(options.Options().DefaultQueue)
	} else {
		ji.Queue = QueueID(pg.Namespace)
	}

	ji.CreationTimestamp = pg.GetCreationTimestamp()
	ji.PodGroup = pg
}

func (ji *JobInfo) SetPDB(pdb *policyv1.PodDisruptionBudget) {
	ji.Name = pdb.Name
	ji.MinAvailable = pdb.Spec.MinAvailable.IntVal
	ji.Namespace = pdb.Namespace
	if len(options.Options().DefaultQueue) == 0 {
		ji.Queue = QueueID(pdb.Namespace)
	} else {
		ji.Queue = QueueID(options.Options().DefaultQueue)
	}

	ji.CreationTimestamp = pdb.GetCreationTimestamp()
	ji.PDB = pdb
}

func (ji *JobInfo) UnsetPDB() {
	ji.PDB = nil
}

func (ji *JobInfo) GetTasks(statuses ...TaskStatus) []*TaskInfo {
	var res []*TaskInfo

	for _, status := range statuses {
		if tasks, found := ji.TaskStatusIndex[status]; found {
			for _, task := range tasks {
				res = append(res, task.Clone())
			}
		}
	}

	return res
}

func (ji *JobInfo) addTaskIndex(ti *TaskInfo) {
	if _, found := ji.TaskStatusIndex[ti.Status]; !found {
		ji.TaskStatusIndex[ti.Status] = tasksMap{}
	}

	ji.TaskStatusIndex[ti.Status][ti.UID] = ti
}

func (ji *JobInfo) AddTaskInfo(ti *TaskInfo) {
	ji.Tasks[ti.UID] = ti
	ji.addTaskIndex(ti)

	ji.TotalRequest.Add(ti.Resreq)

	if AllocatedStatus(ti.Status) {
		ji.Allocated.Add(ti.Resreq)
	}
}

func (ji *JobInfo) UpdateTaskStatus(task *TaskInfo, status TaskStatus) error {
	if err := validateStatusUpdate(task.Status, status); err != nil {
		return err
	}

	// Remove the task from the task list firstly
	ji.DeleteTaskInfo(task)

	// Update task's status to the target status
	task.Status = status
	ji.AddTaskInfo(task)

	return nil
}

func (ji *JobInfo) deleteTaskIndex(ti *TaskInfo) {
	if tasks, found := ji.TaskStatusIndex[ti.Status]; found {
		delete(tasks, ti.UID)

		if len(tasks) == 0 {
			delete(ji.TaskStatusIndex, ti.Status)
		}
	}
}

func (ji *JobInfo) DeleteTaskInfo(ti *TaskInfo) error {
	if task, found := ji.Tasks[ti.UID]; found {
		ji.TotalRequest.Sub(task.Resreq)

		if AllocatedStatus(task.Status) {
			ji.Allocated.Sub(task.Resreq)
		}

		delete(ji.Tasks, task.UID)

		ji.deleteTaskIndex(task)
		return nil
	}

	return fmt.Errorf("failed to find task <%v/%v> in job <%v/%v>",
		ti.Namespace, ti.Name, ji.Namespace, ji.Name)
}

func (ji *JobInfo) Clone() *JobInfo {
	info := &JobInfo{
		UID:       ji.UID,
		Name:      ji.Name,
		Namespace: ji.Namespace,
		Queue:     ji.Queue,

		MinAvailable:  ji.MinAvailable,
		NodeSelector:  map[string]string{},
		Allocated:     ji.Allocated.Clone(),
		TotalRequest:  ji.TotalRequest.Clone(),
		NodesFitDelta: make(NodeResourceMap),

		PDB:      ji.PDB,
		PodGroup: ji.PodGroup,

		TaskStatusIndex: map[TaskStatus]tasksMap{},
		Tasks:           tasksMap{},
	}

	ji.CreationTimestamp.DeepCopyInto(&info.CreationTimestamp)

	for k, v := range ji.NodeSelector {
		info.NodeSelector[k] = v
	}

	for _, task := range ji.Tasks {
		info.AddTaskInfo(task.Clone())
	}

	return info
}

func (ji JobInfo) String() string {
	res := ""

	i := 0
	for _, task := range ji.Tasks {
		res = res + fmt.Sprintf("\n\t %d: %v", i, task)
		i++
	}

	return fmt.Sprintf("Job (%v): name %v, minAvailable %d", ji.UID, ji.Name, ji.MinAvailable) + res
}

// Error returns detailed information on why a job's task failed to fit on
// each available node
func (f *JobInfo) FitError() string {
	if len(f.NodesFitDelta) == 0 {
		reasonMsg := fmt.Sprintf("0 nodes are available")
		return reasonMsg
	}

	reasons := make(map[string]int)
	for _, v := range f.NodesFitDelta {
		if v.Get(v1.ResourceCPU) < 0 {
			reasons["cpu"]++
		}
		if v.Get(v1.ResourceMemory) < 0 {
			reasons["memory"]++
		}
		if v.Get(GPUResourceName) < 0 {
			reasons["GPU"]++
		}
	}

	sortReasonsHistogram := func() []string {
		reasonStrings := []string{}
		for k, v := range reasons {
			reasonStrings = append(reasonStrings, fmt.Sprintf("%v insufficient %v", v, k))
		}
		sort.Strings(reasonStrings)
		return reasonStrings
	}
	reasonMsg := fmt.Sprintf("0/%v nodes are available, %v.", len(f.NodesFitDelta), strings.Join(sortReasonsHistogram(), ", "))
	return reasonMsg
}
