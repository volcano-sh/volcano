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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	volumescheduling "k8s.io/kubernetes/pkg/scheduler/framework/plugins/volumebinding"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
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

// JobWaitingTime is maximum waiting time that a job could stay Pending in service level agreement
// when job waits longer than waiting time, it should be inqueue at once, and cluster should reserve resources for it
const JobWaitingTime = "sla-waiting-time"

// TaskID is UID type for Task
type TaskID types.UID

// TransactionContext holds all the fields that needed by scheduling transaction
type TransactionContext struct {
	NodeName string
	Status   TaskStatus
}

// Clone return a clone of TransactionContext
func (ctx *TransactionContext) Clone() *TransactionContext {
	if ctx == nil {
		return nil
	}
	clone := *ctx
	return &clone
}

type TopologyInfo struct {
	Policy string
	ResMap map[int]v1.ResourceList // key: numa ID
}

func (info *TopologyInfo) Clone() *TopologyInfo {
	copyInfo := &TopologyInfo{
		Policy: info.Policy,
		ResMap: make(map[int]v1.ResourceList),
	}

	for numaID, resList := range info.ResMap {
		copyInfo.ResMap[numaID] = resList.DeepCopy()
	}

	return copyInfo
}

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

	TransactionContext
	// LastTransaction holds the context of last scheduling transaction
	LastTransaction *TransactionContext

	Priority    int32
	VolumeReady bool
	Preemptable bool
	BestEffort  bool

	// RevocableZone support set volcano.sh/revocable-zone annotaion or label for pod/podgroup
	// we only support empty value or * value for this version and we will support specify revocable zone name for futrue release
	// empty value means workload can not use revocable node
	// * value means workload can use all the revocable node for during node active revocable time.
	RevocableZone string

	NumaInfo   *TopologyInfo
	PodVolumes *volumescheduling.PodVolumes
	Pod        *v1.Pod
}

func getJobID(pod *v1.Pod) JobID {
	if gn, found := pod.Annotations[v1beta1.KubeGroupNameAnnotationKey]; found && len(gn) != 0 {
		// Make sure Pod and PodGroup belong to the same namespace.
		jobID := fmt.Sprintf("%s/%s", pod.Namespace, gn)
		return JobID(jobID)
	}

	return ""
}

func getTaskID(pod *v1.Pod) TaskID {
	if ts, found := pod.Annotations[batch.TaskSpecKey]; found && len(ts) != 0 {
		return TaskID(ts)
	}

	return ""
}

const TaskPriorityAnnotation = "volcano.sh/task-priority"

// NewTaskInfo creates new taskInfo object for a Pod
func NewTaskInfo(pod *v1.Pod) *TaskInfo {
	initResReq := GetPodResourceRequest(pod)
	resReq := initResReq
	bestEffort := initResReq.IsEmpty()
	preemptable := GetPodPreemptable(pod)
	revocableZone := GetPodRevocableZone(pod)
	topologyInfo := GetPodTopologyInfo(pod)

	jobID := getJobID(pod)

	ti := &TaskInfo{
		UID:           TaskID(pod.UID),
		Job:           jobID,
		Name:          pod.Name,
		Namespace:     pod.Namespace,
		Priority:      1,
		Pod:           pod,
		Resreq:        resReq,
		InitResreq:    initResReq,
		Preemptable:   preemptable,
		BestEffort:    bestEffort,
		RevocableZone: revocableZone,
		NumaInfo:      topologyInfo,
		TransactionContext: TransactionContext{
			NodeName: pod.Spec.NodeName,
			Status:   getTaskStatus(pod),
		},
	}

	if pod.Spec.Priority != nil {
		ti.Priority = *pod.Spec.Priority
	}

	if taskPriority, ok := pod.Annotations[TaskPriorityAnnotation]; ok {
		if priority, err := strconv.ParseInt(taskPriority, 10, 32); err == nil {
			ti.Priority = int32(priority)
		}
	}

	return ti
}

// GetTransactionContext get transaction context of a task
func (ti *TaskInfo) GetTransactionContext() TransactionContext {
	return ti.TransactionContext
}

// GenerateLastTxContext generate and set context of last transaction for a task
func (ti *TaskInfo) GenerateLastTxContext() {
	ctx := ti.GetTransactionContext()
	ti.LastTransaction = &ctx
}

// ClearLastTxContext clear context of last transaction for a task
func (ti *TaskInfo) ClearLastTxContext() {
	ti.LastTransaction = nil
}

func (ti *TaskInfo) SetPodResourceDecision() error {
	if ti.NumaInfo == nil || len(ti.NumaInfo.ResMap) == 0 {
		return nil
	}

	klog.V(4).Infof("%v/%v resource decision: %v", ti.Namespace, ti.Name, ti.NumaInfo.ResMap)
	decision := PodResourceDecision{
		NUMAResources: ti.NumaInfo.ResMap,
	}

	layout, err := json.Marshal(&decision)
	if err != nil {
		return err
	}

	metav1.SetMetaDataAnnotation(&ti.Pod.ObjectMeta, topologyDecisionAnnotation, string(layout[:]))
	return nil
}

func (ti *TaskInfo) UnsetPodResourceDecision() {
	delete(ti.Pod.Annotations, topologyDecisionAnnotation)
}

// Clone is used for cloning a task
func (ti *TaskInfo) Clone() *TaskInfo {
	return &TaskInfo{
		UID:           ti.UID,
		Job:           ti.Job,
		Name:          ti.Name,
		Namespace:     ti.Namespace,
		Priority:      ti.Priority,
		PodVolumes:    ti.PodVolumes,
		Pod:           ti.Pod,
		Resreq:        ti.Resreq.Clone(),
		InitResreq:    ti.InitResreq.Clone(),
		VolumeReady:   ti.VolumeReady,
		Preemptable:   ti.Preemptable,
		BestEffort:    ti.BestEffort,
		RevocableZone: ti.RevocableZone,
		NumaInfo:      ti.NumaInfo.Clone(),
		TransactionContext: TransactionContext{
			NodeName: ti.NodeName,
			Status:   ti.Status,
		},
		LastTransaction: ti.LastTransaction.Clone(),
	}
}

func (ti *TaskInfo) GetTaskSpecKey() TaskID {
	if ti.Pod == nil {
		return ""
	}
	return getTaskID(ti.Pod)
}

// String returns the taskInfo details in a string
func (ti TaskInfo) String() string {
	if ti.NumaInfo == nil {
		return fmt.Sprintf("Task (%v:%v/%v): job %v, status %v, pri %v"+
			"resreq %v, preemptable %v, revocableZone %v",
			ti.UID, ti.Namespace, ti.Name, ti.Job, ti.Status, ti.Priority,
			ti.Resreq, ti.Preemptable, ti.RevocableZone)
	}

	return fmt.Sprintf("Task (%v:%v/%v): job %v, status %v, pri %v"+
		"resreq %v, preemptable %v, revocableZone %v, numaInfo %v",
		ti.UID, ti.Namespace, ti.Name, ti.Job, ti.Status, ti.Priority,
		ti.Resreq, ti.Preemptable, ti.RevocableZone, *ti.NumaInfo)
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

	WaitingTime *time.Duration

	JobFitErrors   string
	NodesFitErrors map[TaskID]*FitErrors

	// All tasks of the Job.
	TaskStatusIndex       map[TaskStatus]tasksMap
	Tasks                 tasksMap
	TaskMinAvailable      map[TaskID]int32
	TaskMinAvailableTotal int32

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

	var err error
	ji.WaitingTime, err = ji.extractWaitingTime(pg, v1beta1.JobWaitingTime)
	if err != nil {
		klog.Warningf("Error occurs in parsing waiting time for job <%s/%s>, err: %s.",
			pg.Namespace, pg.Name, err.Error())
		ji.WaitingTime = nil
	}
	if ji.WaitingTime == nil {
		ji.WaitingTime, err = ji.extractWaitingTime(pg, JobWaitingTime)
		if err != nil {
			klog.Warningf("Error occurs in parsing waiting time for job <%s/%s>, err: %s.",
				pg.Namespace, pg.Name, err.Error())
			ji.WaitingTime = nil
		}
	}

	ji.Preemptable = ji.extractPreemptable(pg)
	ji.RevocableZone = ji.extractRevocableZone(pg)
	ji.Budget = ji.extractBudget(pg)

	taskMinAvailableTotal := int32(0)
	for task, member := range pg.Spec.MinTaskMember {
		ji.TaskMinAvailable[TaskID(task)] = member
		taskMinAvailableTotal += member
	}
	ji.TaskMinAvailableTotal = taskMinAvailableTotal

	ji.PodGroup = pg
}

// extractWaitingTime reads sla waiting time for job from podgroup annotations
// TODO: should also read from given field in volcano job spec
func (ji *JobInfo) extractWaitingTime(pg *PodGroup, waitingTimeKey string) (*time.Duration, error) {
	if _, exist := pg.Annotations[waitingTimeKey]; !exist {
		return nil, nil
	}

	jobWaitingTime, err := time.ParseDuration(pg.Annotations[waitingTimeKey])
	if err != nil {
		return nil, err
	}

	if jobWaitingTime <= 0 {
		return nil, errors.New("invalid sla waiting time")
	}

	return &jobWaitingTime, nil
}

// extractPreemptable return volcano.sh/preemptable value for job
func (ji *JobInfo) extractPreemptable(pg *PodGroup) bool {
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

// extractRevocableZone return volcano.sh/revocable-zone value for pod/podgroup
func (ji *JobInfo) extractRevocableZone(pg *PodGroup) string {
	// check annotation first
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

// extractBudget return budget value for job
func (ji *JobInfo) extractBudget(pg *PodGroup) *DisruptionBudget {
	if len(pg.Annotations) > 0 {
		if value, found := pg.Annotations[v1beta1.JDBMinAvailable]; found {
			return NewDisruptionBudget(value, "")
		} else if value, found := pg.Annotations[v1beta1.JDBMaxUnavailable]; found {
			return NewDisruptionBudget("", value)
		}
	}

	return NewDisruptionBudget("", "")
}

// GetMinResources return the min resources of podgroup.
func (ji *JobInfo) GetMinResources() *Resource {
	if ji.PodGroup.Spec.MinResources == nil {
		return EmptyResource()
	}

	return NewResource(*ji.PodGroup.Spec.MinResources)
}

func (ji *JobInfo) GetElasticResources() *Resource {
	if ji.Allocated.LessEqualPartly(ji.GetMinResources(), Zero) {
		return EmptyResource()
	}
	return ji.Allocated.Clone().Sub(ji.GetMinResources())
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
		WaitingTime:    ji.WaitingTime,
		JobFitErrors:   ji.JobFitErrors,
		NodesFitErrors: make(map[TaskID]*FitErrors),
		Allocated:      EmptyResource(),
		TotalRequest:   EmptyResource(),

		PodGroup: ji.PodGroup.Clone(),

		TaskStatusIndex:       map[TaskStatus]tasksMap{},
		TaskMinAvailable:      ji.TaskMinAvailable,
		TaskMinAvailableTotal: ji.TaskMinAvailableTotal,
		Tasks:                 tasksMap{},
		Preemptable:           ji.Preemptable,
		RevocableZone:         ji.RevocableZone,
		Budget:                ji.Budget.Clone(),
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
	sortReasonsHistogram := func(reasons map[string]int) []string {
		reasonStrings := []string{}
		for k, v := range reasons {
			reasonStrings = append(reasonStrings, fmt.Sprintf("%v %v", v, k))
		}
		sort.Strings(reasonStrings)
		return reasonStrings
	}

	// Stat histogram for all tasks of the job
	reasons := make(map[string]int)
	for status, taskMap := range ji.TaskStatusIndex {
		reasons[status.String()] += len(taskMap)
	}
	reasons["minAvailable"] = int(ji.MinAvailable)
	reasonMsg := fmt.Sprintf("%v, %v", scheduling.PodGroupNotReady, strings.Join(sortReasonsHistogram(reasons), ", "))

	// Stat histogram for pending tasks only
	reasons = make(map[string]int)
	for uid := range ji.TaskStatusIndex[Pending] {
		reason, _ := ji.TaskSchedulingReason(uid)
		reasons[reason]++
	}
	if len(reasons) > 0 {
		reasonMsg += "; " + fmt.Sprintf("%s: %s", Pending.String(), strings.Join(sortReasonsHistogram(reasons), ", "))
	}
	return reasonMsg
}

// TaskSchedulingReason get detailed reason and message of the given task
// It returns detailed reason and message for tasks based on last scheduling transaction.
func (ji *JobInfo) TaskSchedulingReason(tid TaskID) (reason string, msg string) {
	taskInfo, exists := ji.Tasks[tid]
	if !exists {
		return "", ""
	}

	// Get detailed scheduling reason based on LastTransaction
	ctx := taskInfo.GetTransactionContext()
	if taskInfo.LastTransaction != nil {
		ctx = *taskInfo.LastTransaction
	}

	msg = ji.JobFitErrors
	switch status := ctx.Status; status {
	case Allocated, Pipelined:
		// Pod is schedulable
		msg = fmt.Sprintf("Pod %s/%s can possibly be assigned to %s", taskInfo.Namespace, taskInfo.Name, ctx.NodeName)
		if status == Pipelined {
			msg += " once resource is released"
		}
		return PodReasonSchedulable, msg
	case Pending:
		if fe := ji.NodesFitErrors[tid]; fe != nil {
			// Pod is not schedulable
			return PodReasonUnschedulable, fe.Error()
		}
		// Pod is not scheduled yet
		return PodReasonUndetermined, msg
	default:
		return status.String(), msg
	}
}

// ReadyTaskNum returns the number of tasks that are ready or that is best-effort.
func (ji *JobInfo) ReadyTaskNum() int32 {
	occupied := 0
	occupied += len(ji.TaskStatusIndex[Bound])
	occupied += len(ji.TaskStatusIndex[Binding])
	occupied += len(ji.TaskStatusIndex[Running])
	occupied += len(ji.TaskStatusIndex[Allocated])
	occupied += len(ji.TaskStatusIndex[Succeeded])

	if tasks, found := ji.TaskStatusIndex[Pending]; found {
		for _, task := range tasks {
			if task.BestEffort {
				occupied++
			}
		}
	}

	return int32(occupied)
}

// WaitingTaskNum returns the number of tasks that are pipelined.
func (ji *JobInfo) WaitingTaskNum() int32 {
	return int32(len(ji.TaskStatusIndex[Pipelined]))
}

// CheckTaskValid returns whether each task of job is valid.
func (ji *JobInfo) CheckTaskValid() bool {
	// if job minAvailable is less than sumof(task minAvailable), skip this check
	if ji.MinAvailable < ji.TaskMinAvailableTotal {
		return true
	}

	actual := map[TaskID]int32{}
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded ||
			status == Pipelined ||
			status == Pending {
			for _, task := range tasks {
				actual[getTaskID(task.Pod)]++
			}
		}
	}

	klog.V(4).Infof("job %s/%s actual: %+v, ji.TaskMinAvailable: %+v", ji.Name, ji.Namespace, actual, ji.TaskMinAvailable)
	for task, minAvailable := range ji.TaskMinAvailable {
		if minAvailable == 0 {
			continue
		}
		if act, ok := actual[task]; !ok || act < minAvailable {
			return false
		}
	}

	return true
}

// CheckTaskReady return whether each task of job is ready.
func (ji *JobInfo) CheckTaskReady() bool {
	if ji.MinAvailable < ji.TaskMinAvailableTotal {
		return true
	}
	occupiedMap := map[TaskID]int32{}
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded {
			for _, task := range tasks {
				occupiedMap[getTaskID(task.Pod)]++
			}
			continue
		}

		if status == Pending {
			for _, task := range tasks {
				if task.InitResreq.IsEmpty() {
					occupiedMap[getTaskID(task.Pod)]++
				}
			}
		}
	}
	for taskID, minNum := range ji.TaskMinAvailable {
		if occupiedMap[taskID] < minNum {
			klog.V(4).Infof("Job %s/%s Task %s occupied %v less than task min avaliable", ji.Namespace, ji.Name, taskID, occupiedMap[taskID])
			return false
		}
	}
	return true
}

// CheckTaskPipelined return whether each task of job is pipelined.
func (ji *JobInfo) CheckTaskPipelined() bool {
	if ji.MinAvailable < ji.TaskMinAvailableTotal {
		return true
	}
	occupiedMap := map[TaskID]int32{}
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded ||
			status == Pipelined {
			for _, task := range tasks {
				occupiedMap[getTaskID(task.Pod)]++
			}
			continue
		}

		if status == Pending {
			for _, task := range tasks {
				if task.InitResreq.IsEmpty() {
					occupiedMap[getTaskID(task.Pod)]++
				}
			}
		}
	}
	for taskID, minNum := range ji.TaskMinAvailable {
		if occupiedMap[taskID] < minNum {
			klog.V(4).Infof("Job %s/%s Task %s occupied %v less than task min avaliable", ji.Namespace, ji.Name, taskID, occupiedMap[taskID])
			return false
		}
	}
	return true
}

// CheckTaskStarving return whether job has at least one task which is starving.
func (ji *JobInfo) CheckTaskStarving() bool {
	if ji.MinAvailable < ji.TaskMinAvailableTotal {
		return true
	}
	occupiedMap := map[TaskID]int32{}
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded ||
			status == Pipelined {
			for _, task := range tasks {
				occupiedMap[getTaskID(task.Pod)]++
			}
			continue
		}

		if status == Pending {
			for _, task := range tasks {
				if task.InitResreq.IsEmpty() {
					occupiedMap[getTaskID(task.Pod)]++
				}
			}
		}
	}
	for taskID, minNum := range ji.TaskMinAvailable {
		if occupiedMap[taskID] < minNum {
			klog.V(4).Infof("Job %s/%s Task %s occupied %v less than task min avaliable", ji.Namespace, ji.Name, taskID, occupiedMap[taskID])
			return true
		}
	}
	return false
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

// IsPending returns whether job is in pending status
func (ji *JobInfo) IsPending() bool {
	if ji.PodGroup == nil || ji.PodGroup.Status.Phase == scheduling.PodGroupPending || ji.PodGroup.Status.Phase == "" {
		return true
	}

	return false
}
