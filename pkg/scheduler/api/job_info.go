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
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/klog/v2"

	"k8s.io/kubernetes/pkg/features"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// DisruptionBudget define job min pod available and max pod unavailable value
type DisruptionBudget struct {
	MinAvailable   string
	MaxUnavailable string
}

// NewDisruptionBudget create disruption budget for job
func NewDisruptionBudget(minAvailable, maxUnavailable string) *DisruptionBudget {
	disruptionBudget := &DisruptionBudget{
		MinAvailable:   minAvailable,
		MaxUnavailable: maxUnavailable,
	}
	return disruptionBudget
}

// Clone return a clone of DisruptionBudget
func (db *DisruptionBudget) Clone() *DisruptionBudget {
	return &DisruptionBudget{
		MinAvailable:   db.MinAvailable,
		MaxUnavailable: db.MaxUnavailable,
	}
}

// JobWaitingTime is maximum waiting time that a job could stay Pending in service level agreement
// when job waits longer than waiting time, it should enqueue at once, and cluster should reserve resources for it
const JobWaitingTime = "sla-waiting-time"

// TaskID is UID type for Task
type TaskID types.UID

// TransactionContext holds all the fields that needed by scheduling transaction
type TransactionContext struct {
	NodeName              string
	EvictionOccurred      bool
	JobAllocatedHyperNode string
	Status                TaskStatus
}

// Clone returns a clone of TransactionContext
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
	TaskRole  string // value of "volcano.sh/task-spec"

	// Resreq is the resource that used when task running.
	Resreq *Resource
	// InitResreq is the resource that used to launch a task.
	InitResreq *Resource

	TransactionContext
	// LastTransaction holds the context of last scheduling transaction
	LastTransaction *TransactionContext

	Priority                    int32
	VolumeReady                 bool
	Preemptable                 bool
	BestEffort                  bool
	HasRestartableInitContainer bool
	SchGated                    bool

	// RevocableZone supports setting volcano.sh/revocable-zone annotation or label for pod/podgroup
	// we only support empty value or * value for this version and we will support specify revocable zone name for future releases
	// empty value means workload can not use revocable node
	// * value means workload can use all the revocable node for during node active revocable time.
	RevocableZone string

	NumaInfo *TopologyInfo
	Pod      *v1.Pod

	// CustomBindErrHandler is a custom callback func called when task bind err.
	CustomBindErrHandler func() error `json:"-"`
	// CustomBindErrHandlerSucceeded indicates whether CustomBindErrHandler is executed successfully.
	CustomBindErrHandlerSucceeded bool
}

func getJobID(pod *v1.Pod) JobID {
	if gn, found := pod.Annotations[v1beta1.KubeGroupNameAnnotationKey]; found && len(gn) != 0 {
		// Make sure Pod and PodGroup belong to the same namespace.
		jobID := fmt.Sprintf("%s/%s", pod.Namespace, gn)
		return JobID(jobID)
	}

	return ""
}

func getTaskRole(pod *v1.Pod) string {
	if pod == nil {
		return ""
	}
	if ts, found := pod.Annotations[batch.TaskSpecKey]; found && len(ts) != 0 {
		return ts
	}
	// keep searching pod labels, as it is also added in labels in job controller
	if ts, found := pod.Labels[batch.TaskSpecKey]; found && len(ts) != 0 {
		return ts
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
	role := getTaskRole(pod)
	hasRestartableInitContainer := hasRestartableInitContainer(pod)
	// initialize pod scheduling gates info here since it will not change in a scheduling cycle
	schGated := calSchedulingGated(pod)
	jobID := getJobID(pod)

	ti := &TaskInfo{
		UID:                         TaskID(pod.UID),
		Job:                         jobID,
		Name:                        pod.Name,
		Namespace:                   pod.Namespace,
		TaskRole:                    role,
		Priority:                    1,
		Pod:                         pod,
		Resreq:                      resReq,
		InitResreq:                  initResReq,
		Preemptable:                 preemptable,
		BestEffort:                  bestEffort,
		HasRestartableInitContainer: hasRestartableInitContainer,
		RevocableZone:               revocableZone,
		NumaInfo:                    topologyInfo,
		SchGated:                    schGated,
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

// Return if the pod of a task is scheduling gated by checking if length of sch gates is zero
// When the Pod is not yet created or sch gates field not set, return false
func calSchedulingGated(pod *v1.Pod) bool {
	// Only enable if features.PodSchedulingReadiness feature gate is enabled
	if utilfeature.DefaultFeatureGate.Enabled(features.PodSchedulingReadiness) {
		return pod != nil && pod.Spec.SchedulingGates != nil && len(pod.Spec.SchedulingGates) != 0
	}
	return false
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
		UID:                         ti.UID,
		Job:                         ti.Job,
		Name:                        ti.Name,
		Namespace:                   ti.Namespace,
		TaskRole:                    ti.TaskRole,
		Priority:                    ti.Priority,
		Pod:                         ti.Pod,
		Resreq:                      ti.Resreq.Clone(),
		InitResreq:                  ti.InitResreq.Clone(),
		VolumeReady:                 ti.VolumeReady,
		Preemptable:                 ti.Preemptable,
		BestEffort:                  ti.BestEffort,
		HasRestartableInitContainer: ti.HasRestartableInitContainer,
		RevocableZone:               ti.RevocableZone,
		NumaInfo:                    ti.NumaInfo.Clone(),
		SchGated:                    ti.SchGated,
		TransactionContext: TransactionContext{
			NodeName: ti.NodeName,
			Status:   ti.Status,
		},
		LastTransaction: ti.LastTransaction.Clone(),
	}
}

// hasRestartableInitContainer returns whether pod has restartable container.
func hasRestartableInitContainer(pod *v1.Pod) bool {
	for _, c := range pod.Spec.InitContainers {
		if c.RestartPolicy != nil && *c.RestartPolicy == v1.ContainerRestartPolicyAlways {
			return true
		}
	}
	return false
}

// String returns the taskInfo details in a string
func (ti TaskInfo) String() string {
	res := fmt.Sprintf("Task (%v:%v/%v): taskSpec %s, job %v, status %v, pri %v, "+
		"resreq %v, preemptable %v, revocableZone %v",
		ti.UID, ti.Namespace, ti.Name, ti.TaskRole, ti.Job, ti.Status, ti.Priority,
		ti.Resreq, ti.Preemptable, ti.RevocableZone)

	if ti.NumaInfo != nil {
		res += fmt.Sprintf(", numaInfo %v", *ti.NumaInfo)
	}

	return res
}

// JobID is the type of JobInfo's ID.
type JobID types.UID

type tasksMap map[TaskID]*TaskInfo

// NodeResourceMap stores resource in a node
type NodeResourceMap map[string]*Resource

// JobInfo will have all info of a Job
type JobInfo struct {
	UID   JobID
	PgUID types.UID

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
	TaskMinAvailable      map[string]int32 // key is value of "volcano.sh/task-spec", value is number
	TaskMinAvailableTotal int32

	Allocated    *Resource
	TotalRequest *Resource

	CreationTimestamp metav1.Time
	PodGroup          *PodGroup

	ScheduleStartTimestamp metav1.Time

	Preemptable bool

	// RevocableZone support set volcano.sh/revocable-zone annotation or label for pod/podgroup
	// we only support empty value or * value for this version and we will support specify revocable zone name for future release
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
		TaskMinAvailable: map[string]int32{},
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

	ji.ParseMinMemberInfo(pg)

	ji.PgUID = pg.UID
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
	// check annotation first
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

// ParseMinMemberInfo set the information about job's min member
// 1. set number of each role to TaskMinAvailable
// 2. calculate sum of all roles' min members and set to TaskMinAvailableTotal
func (ji *JobInfo) ParseMinMemberInfo(pg *PodGroup) {
	taskMinAvailableTotal := int32(0)
	for task, member := range pg.Spec.MinTaskMember {
		ji.TaskMinAvailable[task] = member
		taskMinAvailableTotal += member
	}
	ji.TaskMinAvailableTotal = taskMinAvailableTotal
}

// GetMinResources return the min resources of podgroup.
func (ji *JobInfo) GetMinResources() *Resource {
	if ji.PodGroup.Spec.MinResources == nil {
		return EmptyResource()
	}

	return NewResource(*ji.PodGroup.Spec.MinResources)
}

// Get the total resources of tasks whose pod is scheduling gated
// By definition, if a pod is scheduling gated, it's status is Pending
func (ji *JobInfo) GetSchGatedPodResources() *Resource {
	res := EmptyResource()
	for _, task := range ji.Tasks {
		if task.SchGated {
			res.Add(task.Resreq)
		}
	}
	return res
}

// DeductSchGatedResources deduct resources of scheduling gated pod from Resource res;
// If resource is less than gated resources, return zero;
// Note: The purpose of this functionis to deduct the resources of scheduling gated tasks
// in a job when calculating inqueued resources so that it will not block other jobs from being inqueued.
func (ji *JobInfo) DeductSchGatedResources(res *Resource) *Resource {
	schGatedResource := ji.GetSchGatedPodResources()
	// Most jobs do not have any scheduling gated tasks, hence we add this short cut
	if schGatedResource.IsEmpty() {
		return res
	}

	result := res.Clone()
	// schGatedResource can be larger than MinResource because minAvailable of a job can be smaller than number of replica
	result.MilliCPU = max(result.MilliCPU-schGatedResource.MilliCPU, 0)
	result.Memory = max(result.Memory-schGatedResource.Memory, 0)

	// If a scalar resource is present in schGatedResource but not in minResource, skip it
	for name, resource := range res.ScalarResources {
		if schGatedRes, ok := schGatedResource.ScalarResources[name]; ok {
			result.ScalarResources[name] = max(resource-schGatedRes, 0)
		}
	}
	klog.V(3).Infof("Gated resources: %s, MinResource: %s: Result: %s", schGatedResource.String(), res.String(), result.String())
	return result
}

// GetElasticResources returns those partly resources in allocated which are more than its minResource
func (ji *JobInfo) GetElasticResources() *Resource {
	if ji.Allocated == nil {
		return EmptyResource()
	}
	minResource := ji.GetMinResources()
	elastic := ExceededPart(ji.Allocated, minResource)

	return elastic
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

	klog.Warningf("failed to find task <%v/%v> in job <%v/%v>", ti.Namespace, ti.Name, ji.Namespace, ji.Name)
	return nil
}

// Clone is used to clone a jobInfo object
func (ji *JobInfo) Clone() *JobInfo {
	info := &JobInfo{
		UID:       ji.UID,
		PgUID:     ji.PgUID,
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
		TaskMinAvailable:      make(map[string]int32, len(ji.TaskMinAvailable)),
		TaskMinAvailableTotal: ji.TaskMinAvailableTotal,
		Tasks:                 tasksMap{},
		Preemptable:           ji.Preemptable,
		RevocableZone:         ji.RevocableZone,
		Budget:                ji.Budget.Clone(),
	}

	ji.CreationTimestamp.DeepCopyInto(&info.CreationTimestamp)

	for task, minAvailable := range ji.TaskMinAvailable {
		info.TaskMinAvailable[task] = minAvailable
	}
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
		ji.UID, ji.Namespace, ji.Queue, ji.Name, ji.MinAvailable, ji.PodGroup, ji.Preemptable, ji.RevocableZone, ji.Budget.MinAvailable, ji.Budget.MaxUnavailable) + res
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
		reason, _, _ := ji.TaskSchedulingReason(uid)
		reasons[reason]++
	}
	if len(reasons) > 0 {
		reasonMsg += "; " + fmt.Sprintf("%s: %s", Pending.String(), strings.Join(sortReasonsHistogram(reasons), ", "))
	}

	// record the original reason: such as can not enqueue or failed reasons of first pod failed to predicated
	if ji.JobFitErrors != "" {
		reasonMsg += ". Origin reason is: " + ji.JobFitErrors
	} else {
		for _, taskInfo := range ji.Tasks {
			fitError := ji.NodesFitErrors[taskInfo.UID]
			if fitError != nil {
				reasonMsg += fmt.Sprintf(". Origin reason is %v: %v", taskInfo.Name, fitError.Error())
				break
			}
		}
	}

	return reasonMsg
}

// TaskSchedulingReason get detailed reason and message of the given task
// It returns detailed reason and message for tasks based on last scheduling transaction.
func (ji *JobInfo) TaskSchedulingReason(tid TaskID) (reason, msg, nominatedNodeName string) {
	taskInfo, exists := ji.Tasks[tid]
	if !exists {
		return "", "", ""
	}

	// Get detailed scheduling reason based on LastTransaction
	ctx := taskInfo.GetTransactionContext()
	if taskInfo.LastTransaction != nil {
		ctx = *taskInfo.LastTransaction
	}

	msg = ji.JobFitErrors
	switch status := ctx.Status; status {
	case Allocated:
		// Pod is schedulable
		msg = fmt.Sprintf("Pod %s/%s can possibly be assigned to %s, once minAvailable is satisfied", taskInfo.Namespace, taskInfo.Name, ctx.NodeName)
		return PodReasonSchedulable, msg, ""
	case Pipelined:
		msg = fmt.Sprintf("Pod %s/%s can possibly be assigned to %s, once resource is released and minAvailable is satisfied", taskInfo.Namespace, taskInfo.Name, ctx.NodeName)
		if ctx.EvictionOccurred {
			nominatedNodeName = ctx.NodeName
		}
		return PodReasonUnschedulable, msg, nominatedNodeName
	case Pending:
		if fe := ji.NodesFitErrors[tid]; fe != nil {
			// Pod is unschedulable
			return PodReasonUnschedulable, fe.Error(), ""
		}
		// Pod is not scheduled yet, keep UNSCHEDULABLE as the reason to support cluster autoscaler
		return PodReasonUnschedulable, msg, ""
	default:
		return status.String(), msg, ""
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

	return int32(occupied)
}

// WaitingTaskNum returns the number of tasks that are pipelined.
func (ji *JobInfo) WaitingTaskNum() int32 {
	return int32(len(ji.TaskStatusIndex[Pipelined]))
}

func (ji *JobInfo) PendingBestEffortTaskNum() int32 {
	count := 0
	for _, task := range ji.TaskStatusIndex[Pending] {
		if task.BestEffort {
			count++
		}
	}
	return int32(count)
}

// FitFailedRoles returns the job roles' failed fit records
func (ji *JobInfo) FitFailedRoles() map[string]struct{} {
	failedRoles := map[string]struct{}{}
	for tid := range ji.NodesFitErrors {
		task := ji.Tasks[tid]
		failedRoles[task.TaskRole] = struct{}{}
	}
	return failedRoles
}

// TaskHasFitErrors checks if the task has fit errors and can continue try predicating
func (ji *JobInfo) TaskHasFitErrors(task *TaskInfo) bool {
	// if the task didn't set the spec key, should not use the cache
	if len(task.TaskRole) == 0 {
		return false
	}

	_, exist := ji.FitFailedRoles()[task.TaskRole]
	return exist
}

// NeedContinueAllocating checks whether it can continue on allocating for current job
// when its one pod predicated failed, there are two cases to continue:
//  1. job's total allocatable number meet its minAvailable(each task role has no independent minMember setting):
//     because there are cases that some of the pods are not allocatable, but other pods are allocatable and
//     the number of this kind pods can meet the gang-scheduling
//  2. each task's allocable number meet its independent minAvailable
//     this is for the case that each task role has its own independent minMember.
//     eg, current role's pod has a failed predicating result but its allocated number has meet its minMember,
//     the other roles' pods which have no failed predicating results can continue on
//
// performance analysis:
//
//	As the failed predicating role has been pre-checked when it was popped from queue,
//	this function will only be called at most as the number of roles in this job.
func (ji *JobInfo) NeedContinueAllocating() bool {
	// Ensures all tasks must be running; if any pod allocation fails, further execution stops
	if int(ji.MinAvailable) == len(ji.Tasks) {
		return false
	}
	failedRoles := ji.FitFailedRoles()

	pending := map[string]int32{}
	for _, task := range ji.TaskStatusIndex[Pending] {
		pending[task.TaskRole]++
	}
	// 1. don't consider each role's min, just consider total allocatable number vs job's MinAvailable
	if ji.MinAvailable < ji.TaskMinAvailableTotal {
		left := int32(0)
		for role, cnt := range pending {
			if _, ok := failedRoles[role]; !ok {
				left += cnt
			}
		}
		return ji.ReadyTaskNum()+left >= ji.MinAvailable
	}

	// 2. if each task role has its independent minMember, check it
	allocated := ji.getJobAllocatedRoles()
	for role := range failedRoles {
		min := ji.TaskMinAvailable[role]
		if min == 0 {
			continue
		}
		// current role predicated failed and it means the left task with same role can not be allocated,
		// and allocated number less than minAvailable, it can not be ready
		if allocated[role] < min {
			return false
		}
	}

	return true
}

// getJobAllocatedRoles returns result records each role's allocated number
func (ji *JobInfo) getJobAllocatedRoles() map[string]int32 {
	occupiedMap := map[string]int32{}
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded {
			for _, task := range tasks {
				occupiedMap[task.TaskRole]++
			}
			continue
		}

		if status == Pending {
			for _, task := range tasks {
				if task.InitResreq.IsEmpty() {
					occupiedMap[task.TaskRole]++
				}
			}
		}
	}
	return occupiedMap
}

// CheckTaskValid returns whether each task of job is valid.
func (ji *JobInfo) CheckTaskValid() bool {
	// if job minAvailable is less than sum of(task minAvailable), skip this check
	if ji.MinAvailable < ji.TaskMinAvailableTotal {
		return true
	}

	actual := map[string]int32{}
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded ||
			status == Pipelined ||
			status == Pending {
			for _, task := range tasks {
				actual[task.TaskRole]++
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
	occupiedMap := ji.getJobAllocatedRoles()
	for taskSpec, minNum := range ji.TaskMinAvailable {
		if occupiedMap[taskSpec] < minNum {
			klog.V(4).Infof("Job %s/%s Task %s occupied %v less than task min available", ji.Namespace, ji.Name, taskSpec, occupiedMap[taskSpec])
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
	occupiedMap := map[string]int32{}
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded ||
			status == Pipelined {
			for _, task := range tasks {
				occupiedMap[task.TaskRole]++
			}
			continue
		}

		if status == Pending {
			for _, task := range tasks {
				if task.InitResreq.IsEmpty() {
					occupiedMap[task.TaskRole]++
				}
			}
		}
	}
	for taskSpec, minNum := range ji.TaskMinAvailable {
		if occupiedMap[taskSpec] < minNum {
			klog.V(4).Infof("Job %s/%s Task %s occupied %v less than task min available", ji.Namespace, ji.Name, taskSpec, occupiedMap[taskSpec])
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
	occupiedMap := map[string]int32{}
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded ||
			status == Pipelined {
			for _, task := range tasks {
				occupiedMap[task.TaskRole]++
			}
			continue
		}
	}
	for taskSpec, minNum := range ji.TaskMinAvailable {
		if occupiedMap[taskSpec] < minNum {
			klog.V(4).Infof("Job %s/%s Task %s occupied %v less than task min available", ji.Namespace, ji.Name, taskSpec, occupiedMap[taskSpec])
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

func (ji *JobInfo) IsReady() bool {
	return ji.ReadyTaskNum()+ji.PendingBestEffortTaskNum() >= ji.MinAvailable
}

func (ji *JobInfo) IsPipelined() bool {
	return ji.WaitingTaskNum()+ji.ReadyTaskNum()+ji.PendingBestEffortTaskNum() >= ji.MinAvailable
}

func (ji *JobInfo) IsStarving() bool {
	return ji.WaitingTaskNum()+ji.ReadyTaskNum() < ji.MinAvailable
}

// IsPending returns whether job is in pending status
func (ji *JobInfo) IsPending() bool {
	return ji.PodGroup == nil ||
		ji.PodGroup.Status.Phase == scheduling.PodGroupPending ||
		ji.PodGroup.Status.Phase == ""
}

// HasPendingTasks return whether job has pending tasks
func (ji *JobInfo) HasPendingTasks() bool {
	return len(ji.TaskStatusIndex[Pending]) != 0
}

// IsHardTopologyMode return whether the job's network topology mode is hard and also return the highest allowed tier
func (ji *JobInfo) IsHardTopologyMode() (bool, int) {
	if ji.PodGroup == nil || ji.PodGroup.Spec.NetworkTopology == nil || ji.PodGroup.Spec.NetworkTopology.HighestTierAllowed == nil {
		return false, 0
	}

	return ji.PodGroup.Spec.NetworkTopology.Mode == scheduling.HardNetworkTopologyMode, *ji.PodGroup.Spec.NetworkTopology.HighestTierAllowed
}

// IsSoftTopologyMode returns whether the job has configured network topologies with soft mode.
func (ji *JobInfo) IsSoftTopologyMode() bool {
	if ji.PodGroup == nil || ji.PodGroup.Spec.NetworkTopology == nil {
		return false
	}
	return ji.PodGroup.Spec.NetworkTopology.Mode == scheduling.SoftNetworkTopologyMode
}

// ResetFitErr will set job and node fit err to nil.
func (ji *JobInfo) ResetFitErr() {
	ji.JobFitErrors = ""
	ji.NodesFitErrors = make(map[TaskID]*FitErrors)
}
