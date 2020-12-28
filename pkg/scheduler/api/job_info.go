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
	"strconv"
	"strings"

	"k8s.io/klog"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
)

// DisruptionBudget define job min pod available and max pod unvailable value
type DisruptionBudget struct {
	MinAvailable  string
	MaxUnavilable string
}

// NewDisruptionBudget create disruption budget for job
func NewDisruptionBudget(minAvailable, maxUnavilable string) *DisruptionBudget {

	disruptionBudget := &DisruptionBudget{
		MinAvailable:  minAvailable,
		MaxUnavilable: maxUnavilable,
	}
	return disruptionBudget
}

// Clone return a clone of DisruptionBudget
func (db *DisruptionBudget) Clone() *DisruptionBudget {
	return &DisruptionBudget{
		MinAvailable:  db.MinAvailable,
		MaxUnavilable: db.MaxUnavilable,
	}
}

// TaskID is UID type for Task
type TaskID types.UID

// TaskInfo will have all infos about the task
type TaskInfo struct {
	UID TaskID
	Job JobID

	Name      string
	Namespace string

	// Resreq is the resource that used when task running.
	Resreq *Resource
	// InitResreq is the resource that used to launch a task.
	InitResreq *Resource

	NodeName    string
	Status      TaskStatus
	Priority    int32
	VolumeReady bool
	Preemptable bool

	// RevocableZone support set volcano.sh/revocable-zone annotaion or label for pod/podgroup
	// we only support empty value or * value for this version and we will support specify revocable zone name for futrue release
	// empty value means workload can not use revocable node
	// * value means workload can use all the revocable node for during node active revocable time.
	RevocableZone string

	Pod *v1.Pod
}

func getJobID(pod *v1.Pod) JobID {
	if gn, found := pod.Annotations[v1beta1.KubeGroupNameAnnotationKey]; found && len(gn) != 0 {
		// Make sure Pod and PodGroup belong to the same namespace.
		jobID := fmt.Sprintf("%s/%s", pod.Namespace, gn)
		return JobID(jobID)
	}

	return ""
}

// NewTaskInfo creates new taskInfo object for a Pod
func NewTaskInfo(pod *v1.Pod) *TaskInfo {
	req := GetPodResourceWithoutInitContainers(pod)
	initResreq := GetPodResourceRequest(pod)
	preemptable := GetPodPreemptable(pod)
	revocableZone := GetPodRevocableZone(pod)

	jobID := getJobID(pod)

	ti := &TaskInfo{
		UID:           TaskID(pod.UID),
		Job:           jobID,
		Name:          pod.Name,
		Namespace:     pod.Namespace,
		NodeName:      pod.Spec.NodeName,
		Status:        getTaskStatus(pod),
		Priority:      1,
		Pod:           pod,
		Resreq:        req,
		InitResreq:    initResreq,
		Preemptable:   preemptable,
		RevocableZone: revocableZone,
	}

	if pod.Spec.Priority != nil {
		ti.Priority = *pod.Spec.Priority
	}

	return ti
}

// Clone is used for cloning a task
func (ti *TaskInfo) Clone() *TaskInfo {
	return &TaskInfo{
		UID:           ti.UID,
		Job:           ti.Job,
		Name:          ti.Name,
		Namespace:     ti.Namespace,
		NodeName:      ti.NodeName,
		Status:        ti.Status,
		Priority:      ti.Priority,
		Pod:           ti.Pod,
		Resreq:        ti.Resreq.Clone(),
		InitResreq:    ti.InitResreq.Clone(),
		VolumeReady:   ti.VolumeReady,
		Preemptable:   ti.Preemptable,
		RevocableZone: ti.RevocableZone,
	}
}

// String returns the taskInfo details in a string
func (ti TaskInfo) String() string {
	return fmt.Sprintf("Task (%v:%v/%v): job %v, status %v, pri %v, resreq %v, preemptable %v, revocableZone %v",
		ti.UID, ti.Namespace, ti.Name, ti.Job, ti.Status, ti.Priority, ti.Resreq, ti.Preemptable, ti.RevocableZone)
}

// JobID is the type of JobInfo's ID.
type JobID types.UID

type tasksMap map[TaskID]*TaskInfo

// NodeResourceMap stores resource in a node
type NodeResourceMap map[string]*Resource

// JobInfo will have all info of a Job
type JobInfo struct {
	UID JobID

	Name      string
	Namespace string

	Queue QueueID

	Priority int32

	MinAvailable int32

	JobFitErrors   string
	NodesFitErrors map[TaskID]*FitErrors

	// All tasks of the Job.
	TaskStatusIndex  map[TaskStatus]tasksMap
	Tasks            tasksMap
	TaskMinAvailable map[TaskID]int32

	Allocated    *Resource
	TotalRequest *Resource

	CreationTimestamp metav1.Time
	PodGroup          *PodGroup

	ScheduleStartTimestamp metav1.Time

	Preemptable bool

	// RevocableZone support set volcano.sh/revocable-zone annotaion or label for pod/podgroup
	// we only support empty value or * value for this version and we will support specify revocable zone name for futrue release
	// empty value means workload can not use revocable node
	// * value means workload can use all the revocable node for during node active revocable time.
	RevocableZone string
	Budget        *DisruptionBudget
}

// NewJobInfo creates a new jobInfo for set of tasks
func NewJobInfo(uid JobID, tasks ...*TaskInfo) *JobInfo {
	job := &JobInfo{
		UID:              uid,
		MinAvailable:     0,
		NodesFitErrors:   make(map[TaskID]*FitErrors),
		Allocated:        EmptyResource(),
		TotalRequest:     EmptyResource(),
		TaskStatusIndex:  map[TaskStatus]tasksMap{},
		Tasks:            tasksMap{},
		TaskMinAvailable: map[TaskID]int32{},
	}

	for _, task := range tasks {
		job.AddTaskInfo(task)
	}

	return job
}

// UnsetPodGroup removes podGroup details from a job
func (ji *JobInfo) UnsetPodGroup() {
	ji.PodGroup = nil
}

// SetPodGroup sets podGroup details to a job
func (ji *JobInfo) SetPodGroup(pg *PodGroup) {
	ji.Name = pg.Name
	ji.Namespace = pg.Namespace
	ji.MinAvailable = pg.Spec.MinMember
	ji.Queue = QueueID(pg.Spec.Queue)
	ji.CreationTimestamp = pg.GetCreationTimestamp()
	ji.Preemptable = GetJobPreemptable(pg)
	ji.RevocableZone = GetJobRevocableZone(pg)
	ji.Budget = GetBudget(pg)
	for task, member := range pg.Spec.MinTaskMember {
		ji.TaskMinAvailable[TaskID(task)] = member
	}

	ji.PodGroup = pg

}

// GetJobPreemptable return volcano.sh/preemptable value for job
func GetJobPreemptable(pg *PodGroup) bool {
	// check annotaion first
	if len(pg.Annotations) > 0 {
		if value, found := pg.Annotations[v1beta1.PodPreemptable]; found {
			b, err := strconv.ParseBool(value)
			if err != nil {
				klog.Warningf("invalid %s=%s", v1beta1.PodPreemptable, value)
				return false
			}
			return b
		}
	}

	// it annotation does not exit, check label
	if len(pg.Labels) > 0 {
		if value, found := pg.Labels[v1beta1.PodPreemptable]; found {
			b, err := strconv.ParseBool(value)
			if err != nil {
				klog.Warningf("invalid %s=%s", v1beta1.PodPreemptable, value)
				return false
			}
			return b
		}
	}

	return false
}

// GetJobRevocableZone return volcano.sh/revocable-zone value for pod/podgroup
func GetJobRevocableZone(pg *PodGroup) string {
	// check annotaion first
	if len(pg.Annotations) > 0 {
		if value, found := pg.Annotations[v1beta1.RevocableZone]; found {
			if value != "*" {
				return ""
			}
			return value
		}

		if value, found := pg.Annotations[v1beta1.PodPreemptable]; found {
			if b, err := strconv.ParseBool(value); err == nil && b {
				return "*"
			}
		}
	}

	return ""
}

// GetBudget return budget value for job
func GetBudget(pg *PodGroup) *DisruptionBudget {
	if len(pg.Annotations) > 0 {
		if value, found := pg.Annotations[v1beta1.JDBMinAvailable]; found {
			return NewDisruptionBudget(value, "")
		} else if value, found := pg.Annotations[v1beta1.JDBMaxUnavailable]; found {
			return NewDisruptionBudget("", value)
		}
	}

	return NewDisruptionBudget("", "")
}

func (ji *JobInfo) addTaskIndex(ti *TaskInfo) {
	if _, found := ji.TaskStatusIndex[ti.Status]; !found {
		ji.TaskStatusIndex[ti.Status] = tasksMap{}
	}
	ji.TaskStatusIndex[ti.Status][ti.UID] = ti
}

// AddTaskInfo is used to add a task to a job
func (ji *JobInfo) AddTaskInfo(ti *TaskInfo) {
	ji.Tasks[ti.UID] = ti
	ji.addTaskIndex(ti)
	ji.TotalRequest.Add(ti.Resreq)
	if AllocatedStatus(ti.Status) {
		ji.Allocated.Add(ti.Resreq)
	}
}

// UpdateTaskStatus is used to update task's status in a job.
// If error occurs both task and job are guaranteed to be in the original state.
func (ji *JobInfo) UpdateTaskStatus(task *TaskInfo, status TaskStatus) error {
	if err := validateStatusUpdate(task.Status, status); err != nil {
		return err
	}

	// First remove the task (if exist) from the task list.
	if _, found := ji.Tasks[task.UID]; found {
		if err := ji.DeleteTaskInfo(task); err != nil {
			return err
		}
	}

	// Update task's status to the target status once task addition is guaranteed to succeed.
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

// DeleteTaskInfo is used to delete a task from a job
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

// Clone is used to clone a jobInfo object
func (ji *JobInfo) Clone() *JobInfo {
	info := &JobInfo{
		UID:       ji.UID,
		Name:      ji.Name,
		Namespace: ji.Namespace,
		Queue:     ji.Queue,
		Priority:  ji.Priority,

		MinAvailable:   ji.MinAvailable,
		NodesFitErrors: make(map[TaskID]*FitErrors),
		Allocated:      EmptyResource(),
		TotalRequest:   EmptyResource(),

		PodGroup: ji.PodGroup,

		TaskStatusIndex: map[TaskStatus]tasksMap{},
		Tasks:           tasksMap{},
		Preemptable:     ji.Preemptable,
		RevocableZone:   ji.RevocableZone,
		Budget:          ji.Budget.Clone(),
	}

	ji.CreationTimestamp.DeepCopyInto(&info.CreationTimestamp)

	for _, task := range ji.Tasks {
		info.AddTaskInfo(task.Clone())
	}

	return info
}

// String returns a jobInfo object in string format
func (ji JobInfo) String() string {
	res := ""

	i := 0
	for _, task := range ji.Tasks {
		res += fmt.Sprintf("\n\t %d: %v", i, task)
		i++
	}

	return fmt.Sprintf("Job (%v): namespace %v (%v), name %v, minAvailable %d, podGroup %+v, preemptable %+v, revocableZone %+v, minAvailable %+v, maxAvailable %+v",
		ji.UID, ji.Namespace, ji.Queue, ji.Name, ji.MinAvailable, ji.PodGroup, ji.Preemptable, ji.RevocableZone, ji.Budget.MinAvailable, ji.Budget.MaxUnavilable) + res
}

// FitError returns detailed information on why a job's task failed to fit on
// each available node
func (ji *JobInfo) FitError() string {
	reasons := make(map[string]int)
	for status, taskMap := range ji.TaskStatusIndex {
		reasons[status.String()] += len(taskMap)
	}
	reasons["minAvailable"] = int(ji.MinAvailable)

	sortReasonsHistogram := func() []string {
		reasonStrings := []string{}
		for k, v := range reasons {
			reasonStrings = append(reasonStrings, fmt.Sprintf("%v %v", v, k))
		}
		sort.Strings(reasonStrings)
		return reasonStrings
	}
	reasonMsg := fmt.Sprintf("%v, %v.", scheduling.PodGroupNotReady, strings.Join(sortReasonsHistogram(), ", "))
	return reasonMsg
}

// ReadyTaskNum returns the number of tasks that are ready or that is best-effort.
func (ji *JobInfo) ReadyTaskNum() int32 {
	var occupied int32
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded {
			occupied += int32(len(tasks))
			continue
		}

		if status == Pending {
			for _, task := range tasks {
				if task.InitResreq.IsEmpty() {
					occupied++
				}
			}
		}
	}

	return occupied
}

// WaitingTaskNum returns the number of tasks that are pipelined.
func (ji *JobInfo) WaitingTaskNum() int32 {
	occupid := 0
	for status, tasks := range ji.TaskStatusIndex {
		if status == Pipelined {
			occupid += len(tasks)
		}
	}

	return int32(occupid)
}

// ValidTaskMinAvailable returns whether each task of job is valid.
func (ji *JobInfo) ValidTaskMinAvailable() (map[TaskID]int32, bool) {
	actual := map[TaskID]int32{}
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded ||
			status == Pipelined ||
			status == Pending {
			for _, task := range tasks {
				actual[task.UID] += 1
			}
		}
	}

	klog.Infof("actual: %v, ji.TaskMinAvailable: %v", actual, ji.TaskMinAvailable)

	for task, occupied := range actual {
		if minAvailable, ok := ji.TaskMinAvailable[task]; ok && occupied < minAvailable {
			return actual, false
		}
	}

	return actual, true
}

// ValidTaskNum returns the number of tasks that are valid.
func (ji *JobInfo) ValidTaskNum() int32 {
	occupied := 0
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded ||
			status == Pipelined ||
			status == Pending {
			occupied += len(tasks)
		}
	}

	return int32(occupied)
}

// Ready returns whether job is ready for run
func (ji *JobInfo) Ready() bool {
	occupied := ji.ReadyTaskNum()

	return occupied >= ji.MinAvailable
}

// Pipelined returns whether the number of ready and pipelined task is enough
func (ji *JobInfo) Pipelined() bool {
	occupied := ji.WaitingTaskNum() + ji.ReadyTaskNum()

	return occupied >= ji.MinAvailable
}
