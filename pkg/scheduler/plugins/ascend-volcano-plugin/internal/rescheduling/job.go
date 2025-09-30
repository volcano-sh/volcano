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
Package rescheduling is using for HuaWei Ascend pin fault rescheduling.
*/
package rescheduling

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// GetJobFaultRescheduleLabel Get job's fault reschedule label.
func (fJob *FaultJob) GetJobFaultRescheduleLabel(job *plugin.SchedulerJob) string {
	if fJob == nil || job == nil {
		klog.V(util.LogErrorLev).Info(
			"GetJobFaultRescheduleLabel fJob or schedulerJob does not exist")
		return JobOffRescheduleLabelValue
	}
	value, ok := job.SchedulerJobAttr.Label[JobRescheduleLabelKey]
	if !ok {
		klog.V(util.LogInfoLev).Infof(
			"GetJobFaultRescheduleLabel %s. %s no job reschedule label", value, job.Name)
		return JobOffRescheduleLabelValue
	}
	klog.V(util.LogInfoLev).Infof("GetJobFaultRescheduleLabel job: %s, label: %s", job.Name, value)
	return value
}

// GetJobElasticSchedulingLabel get job's elastic scheduling label
func (fJob *FaultJob) GetJobElasticSchedulingLabel(job *plugin.SchedulerJob) string {
	if fJob == nil || job == nil {
		klog.V(util.LogErrorLev).Info(
			"GetJobElasticSchedulingLabel fJob or schedulerJob object does not exist")
		return JobOffElasticScheduling
	}
	value, ok := job.SchedulerJobAttr.Label[ElasticSchedulingKey]
	if !ok {
		klog.V(util.LogInfoLev).Infof(
			"GetJobElasticSchedulingLabel %s. %s no job reschedule label", value, job.Name)
		return JobOffRescheduleLabelValue
	}
	klog.V(util.LogInfoLev).Infof("GetJobElasticSchedulingLabel job: %s, label: %s", job.Name, value)
	return value
}

// IsNormalJobNeedRestart is Job has the key of PreSeparateNPU os Job has software fault
func (fJob *FaultJob) IsNormalJobNeedRestart() bool {
	if fJob == nil {
		return false
	}
	for _, fTask := range fJob.FaultTasks {
		if fTask.IsSoftwareFault {
			klog.V(util.LogWarningLev).Infof("task %s has software fault, need restart by platform", fTask.TaskName)
			return true
		}
		for _, reason := range fTask.Reason {
			if reason.FaultHandling == PreSeparateNPU {
				klog.V(util.LogWarningLev).Infof("task %s has PreSeparateNPU fault, "+
					"need restart by platform", fTask.TaskName)
				return true
			}
		}
	}
	return false
}

func (fJob *FaultJob) isJobGraceDeleteSuccess(jobInfo *api.JobInfo) bool {
	restartNum := 0
	deleteNum := 0
	for _, fTask := range fJob.FaultTasks {
		if fTask.IsFaultTask {
			deleteNum++
		}
		podCreateTimeRecord := fTask.PodCreateTime // former pods create time
		npuTask, ok := jobInfo.Tasks[fTask.TaskUID]
		if !ok { // task not in job indicates it has been restarted
			restartNum++
			klog.V(util.LogInfoLev).Infof("%s in %s has been deleted", fTask.TaskName, jobInfo.Name)
			continue
		}
		podCreateTimeCur := npuTask.Pod.CreationTimestamp.Unix() // current pod create time
		if podCreateTimeCur != podCreateTimeRecord {
			klog.V(util.LogInfoLev).Infof("pod %s restart success[new:%v---old:%v]",
				npuTask.Pod.Name, podCreateTimeCur, podCreateTimeRecord)
			restartNum++
		}
	}

	klog.V(util.LogDebugLev).Infof("<%d/%d> pod of job restarted", restartNum, jobInfo.MinAvailable)
	if len(jobInfo.PodGroup.Labels) != 0 && (jobInfo.PodGroup.Labels[util.SinglePodTag] == util.EnableFunc ||
		jobInfo.PodGroup.Labels[util.ProcessRecoverEnable] == util.EnableFunc) &&
		fJob.PendingSessionNum != pendingTimes {
		return restartNum >= deleteNum
	}
	return restartNum >= len(fJob.FaultTasks)
}

// deleteJobWithLabels delete job with labels
func (fJob *FaultJob) deleteJobWithLabels(ssn *framework.Session, reschedule *ReScheduler,
	schedulerJob *plugin.SchedulerJob, env plugin.ScheduleEnv) error {
	if !fJob.isFaultJobCanRestarted(reschedule) {
		return fmt.Errorf("job <%s> is not fault job or pod failed job reach max restart time", fJob.JobName)
	}
	if fJob.IsSubHealthFault {
		return fJob.deleteJobWithSubHealthyLabels(ssn, reschedule, schedulerJob, env)
	}
	return fJob.deleteJobWithFaultLabels(ssn, reschedule, schedulerJob, env)
}

func (fJob *FaultJob) deleteJobWithSubHealthyLabels(ssn *framework.Session, reschedule *ReScheduler,
	schedulerJob *plugin.SchedulerJob, env plugin.ScheduleEnv) error {
	klog.V(util.LogWarningLev).Infof("delete job %s subHealthyStrategy %s", fJob.JobName,
		fJob.SubHealthyStrategy)
	if fJob.SubHealthyStrategy == util.SubHealthyForceExit {
		return fJob.ForceDeleteJob(schedulerJob, env)
	}
	if !fJob.IsProcessReschedulingJob(schedulerJob) {
		klog.V(util.LogWarningLev).Infof("reset configmap be updated with graceExit, job:%s", fJob.JobName)
		updateResetConfigMapWithGraceExit(env.FrameAttr.KubeClient, plugin.ResetInfoCMNamePrefix+
			schedulerJob.ReferenceName, schedulerJob.NameSpace, plugin.GraceExitValue)
	}
	return fJob.GraceDeleteJob(ssn, schedulerJob, env)
}

// deleteJobWithLabels delete job with labels
func (fJob *FaultJob) deleteJobWithFaultLabels(ssn *framework.Session, reschedule *ReScheduler,
	schedulerJob *plugin.SchedulerJob, env plugin.ScheduleEnv) error {
	klog.V(util.LogWarningLev).Infof("delete job %s ReScheduleKey %s", fJob.JobName,
		fJob.ReScheduleKey)
	if fJob.ReScheduleKey == JobForceRescheduleLabelValue {
		return fJob.ForceDeleteJob(schedulerJob, env)
	}

	return fJob.GraceDeleteJob(ssn, schedulerJob, env)
}

// getVirSupPodId get virtual super pods pods id
func (fJob *FaultJob) getVirSupPodId(node string, env plugin.ScheduleEnv) string {
	if _, ok := env.SuperPodInfo.SuperPodReschdInfo[fJob.JobUID]; !ok {
		return ""
	}
	for id, v := range env.SuperPodInfo.SuperPodReschdInfo[fJob.JobUID] {
		for _, each := range v {
			if each.Name == node {
				return id
			}
		}
	}
	return ""
}

func (fJob *FaultJob) isContainTask(ids []string, nodeName string, env plugin.ScheduleEnv) bool {
	if _, ok := env.SuperPodInfo.SuperPodReschdInfo[fJob.JobUID]; !ok {
		return false
	}
	for _, v := range ids {
		nodes, ok := env.SuperPodInfo.SuperPodReschdInfo[fJob.JobUID][v]
		if !ok {
			return false
		}
		for _, each := range nodes {
			if each.Name == nodeName {
				return true
			}
		}
	}
	return false
}

// ForceDeleteJob force delete jobs includes labelled force delete ones and grace delete failed ones
func (fJob *FaultJob) ForceDeleteJob(schedulerJob *plugin.SchedulerJob,
	env plugin.ScheduleEnv) error {
	klog.V(util.LogDebugLev).Infof("enter ForceDeleteJob")
	if fJob == nil || schedulerJob == nil {
		return fmt.Errorf(
			"getJobFaultRescheduleLabel fJob object or ssn or schedulerJob does not exist")
	}
	var isMasterFault bool
	for _, fTask := range fJob.FaultTasks {
		if fTask.IsFaultTask && fTask.NodeRankIndex == util.Rank0 {
			isMasterFault = true
		}
	}
	superPod := false
	ids := make([]string, 0)
	if _, ok := schedulerJob.Annotation[SuperPodAnnoKey]; ok {
		superPod = true
		ids = fJob.getIds(env)
	}
	fJob.updateSuperPodsReschdInfo(env)
	dpi := &deletePodInfo{
		isMasterFault: isMasterFault,
		superPod:      superPod,
		ids:           ids,
	}
	fJob.forceDeletePods(schedulerJob, env, dpi)
	return nil
}

func (fJob *FaultJob) forceDeletePods(schedulerJob *plugin.SchedulerJob,
	env plugin.ScheduleEnv, dpi *deletePodInfo) {
	var waitDeleteTask = make([]FaultTask, 0)
	for _, fTask := range fJob.FaultTasks {
		if !fJob.isNormalTaskCanBeDelete(fTask, schedulerJob, env, dpi) {
			continue
		}
		fJob.updateSuperPodMapInfo(env, fTask.TaskName, fTask.NodeName)
		waitDeleteTask = append(waitDeleteTask, fTask)
		klog.V(util.LogInfoLev).Infof("superpod delete pods:%s", fTask.TaskName)
	}
	fJob.deletingTasksConcurrently(waitDeleteTask, env.FrameAttr.KubeClient)
}

func (fJob *FaultJob) isNormalTaskCanBeDelete(fTask FaultTask, schedulerJob *plugin.SchedulerJob,
	env plugin.ScheduleEnv, dpi *deletePodInfo) bool {
	klog.V(util.LogDebugLev).Infof("not masterFault is %v, job single rescheduling is %v ,"+
		"not fault task is %v",
		!dpi.isMasterFault, fJob.IsJobSingleRescheduling(schedulerJob), !fTask.IsFaultTask)
	if dpi.isMasterFault {
		return true
	}
	// when pod rescheduling, master fault try job rescheduling
	// tor affinity job, first delete fault task, when first task pending 12 session, try job rescheduling
	// super pod, first delete fault task, when first task pending 6 session, try super pod rescheduling
	// super pod, when pod pending 12 session, try job rescheduling
	if fJob.IsJobSingleRescheduling(schedulerJob) && !fTask.IsFaultTask {
		if !dpi.superPod {
			return false
		}
		// single pod rescheduling stage, delete no pod
		// virtual super pod rescheduling stage, delete all virtual super pod where fault task in
		if fJob.PendingSessionNum < spPendingTimes {
			return false
		} else if !fJob.isContainTask(dpi.ids, fTask.NodeName, env) {
			return false
		}
	}
	// when master pod fault try job rescheduling
	// when job is process rescheduling and not in pod rescheduling label, only delete fault task.
	// when  process rescheduling is failed, process-rescheduling change to pause, try job rescheduling
	if fJob.IsProcessReschedulingJob(schedulerJob) && !fTask.IsFaultTask {
		return false
	}
	return true
}

func (fJob *FaultJob) deletingTasksConcurrently(waitDeleteTask []FaultTask, kubeClient kubernetes.Interface) {
	if len(waitDeleteTask) == 0 {
		return
	}
	if len(waitDeleteTask) <= singleThreadDeletePodNum {
		fJob.forceDeleteTasks(waitDeleteTask, kubeClient)
		return
	}
	var deleteJobSync sync.WaitGroup
	for i := 0; i < len(waitDeleteTask); i += singleThreadDeletePodNum {
		deleteJobSync.Add(1)
		if i+singleThreadDeletePodNum > len(waitDeleteTask) {
			go fJob.forceDeleteTasksConcurrently(waitDeleteTask[i:], kubeClient, &deleteJobSync)
			continue
		}
		go fJob.forceDeleteTasksConcurrently(waitDeleteTask[i:i+singleThreadDeletePodNum], kubeClient, &deleteJobSync)
	}
	deleteJobSync.Wait()
}

func (fJob *FaultJob) forceDeleteTasksConcurrently(waitDeleteTask []FaultTask, kubeClient kubernetes.Interface,
	deleteJobSync *sync.WaitGroup) {
	fJob.forceDeleteTasks(waitDeleteTask, kubeClient)
	deleteJobSync.Done()
}

func (fJob *FaultJob) forceDeleteTasks(waitDeleteTask []FaultTask, kubeClient kubernetes.Interface) {
	for _, fTask := range waitDeleteTask {
		if deleteErr := fTask.DeleteRealPodByTask(kubeClient, 0); deleteErr != nil {
			klog.V(util.LogWarningLev).Infof("ForceDeleteFaultPod %s: %s.", fTask.TaskName, util.SafePrint(deleteErr))
		}
	}
}

func (fJob *FaultJob) updateSuperPodMapInfo(env plugin.ScheduleEnv, taskName string, nodeName string) {
	if nodeName == "" {
		klog.V(util.LogInfoLev).Infof("updateSuperPodMapInfo nodeName is empty")
		return
	}
	mapInfo := make(map[string]string)
	if _, ok := env.SuperPodInfo.SuperPodMapFaultTaskNodes[fJob.JobUID]; ok {
		mapInfo = env.SuperPodInfo.SuperPodMapFaultTaskNodes[fJob.JobUID]
	}
	mapInfo[taskName] = nodeName
	env.SuperPodInfo.SuperPodMapFaultTaskNodes[fJob.JobUID] = mapInfo
}

func (fJob *FaultJob) updateSuperPodsReschdInfo(env plugin.ScheduleEnv) {
	if _, ok := env.SuperPodInfo.SuperPodFaultTaskNodes[fJob.JobUID]; !ok {
		var nodesName []string
		for _, fTask := range fJob.FaultTasks {
			if fTask.IsFaultTask && fTask.NodeName != "" {
				nodesName = append(nodesName, fTask.NodeName)
			}
		}
		env.SuperPodInfo.SuperPodFaultTaskNodes[fJob.JobUID] = nodesName
	}
	if len(fJob.SuperPods) > 0 {
		env.SuperPodInfo.SuperPodReschdInfo[fJob.JobUID] = fJob.SuperPods
	}
	klog.V(util.LogInfoLev).Infof("cache superpods %+v", env.SuperPodInfo.SuperPodReschdInfo[fJob.JobUID])
}

func (fJob *FaultJob) getIds(env plugin.ScheduleEnv) []string {
	var ids []string
	if _, ok := env.SuperPodInfo.SuperPodFaultTaskNodes[fJob.JobUID]; !ok {
		return ids
	}
	for _, node := range env.SuperPodInfo.SuperPodFaultTaskNodes[fJob.JobUID] {
		klog.V(util.LogInfoLev).Infof("cache node %s", node)
		id := fJob.getVirSupPodId(node, env)
		if id != "" {
			ids = append(ids, id)
		}
	}
	klog.V(util.LogInfoLev).Infof("getIds super pod ids:%v", ids)
	return ids
}

// IsJobSingleRescheduling valid job.
func (fJob *FaultJob) IsJobSingleRescheduling(sJob *plugin.SchedulerJob) bool {
	if sJob.Label[util.SinglePodTag] == util.EnableFunc && fJob.PendingSessionNum < pendingTimes {
		return true
	}
	return false
}

// IsProcessReschedulingJob valid job.
func (fJob *FaultJob) IsProcessReschedulingJob(sJob *plugin.SchedulerJob) bool {
	if sJob.Label[util.ProcessRecoverEnable] == util.EnableFunc {
		return true
	}
	return false
}

func (fJob *FaultJob) updateTaskPodUid(jobInfo *api.JobInfo) {
	for i := 0; i < len(fJob.FaultTasks); i++ {
		fJob.FaultTasks[i].TaskUID = getTaskPodUidByTaskName(fJob.FaultTasks[i].TaskName, jobInfo)
	}
}

func getTaskPodUidByTaskName(taskName string, jobInfo *api.JobInfo) api.TaskID {
	for _, task := range jobInfo.Tasks {
		if task.Name == taskName && task.Pod != nil {
			return api.TaskID(task.Pod.UID)
		}
	}
	return ""
}

func (fJob *FaultJob) updateFaultJobWhenNewPodError(jobInfo *api.JobInfo) {
	if jobInfo.PodGroup.Labels[util.SinglePodTag] != util.EnableFunc &&
		jobInfo.PodGroup.Labels[util.ProcessRecoverEnable] != util.EnableFunc {
		return
	}
	newFailedTask := make(map[api.TaskID]struct{})
	for taskId, task := range jobInfo.Tasks {
		if task.Pod.Status.Phase == v1.PodFailed {
			newFailedTask[taskId] = struct{}{}
		}
	}
	if len(newFailedTask) == 0 {
		return
	}
	fJob.DeleteExecutedFlag = false

	for i, fTask := range fJob.FaultTasks {
		if fTask.IsFaultTask {
			continue
		}
		if _, ok := newFailedTask[fTask.TaskUID]; ok {
			fJob.FaultTasks[i].IsFaultTask = true
			fJob.FaultTasks[i].faultType = PodFailed
		}
	}
}

// isPodFailedCanRestarted if the pod status is failed, judge whether job can be restarted
func (fJob *FaultJob) isFaultJobCanRestarted(reScheduler *ReScheduler) bool {
	if !fJob.IsFaultJob {
		klog.V(util.LogWarningLev).Infof("fJob %s has software fault or PreSeparateNPU fault, "+
			"need restarted by platform skip delete pod", fJob.JobName)
		return false
	}
	if fJob.faultReason == PodFailed {
		if fJob.FaultRetryTimes == 0 {
			klog.V(util.LogInfoLev).Infof("job<%s> retry times is 0", fJob.JobUID)
			return false
		}
		remain, ok := reScheduler.JobRemainRetryTimes[fJob.JobUID]
		if !ok || remain == nil {
			return false
		}
		if remain.Times <= 0 {
			klog.V(util.LogInfoLev).Infof("job<%s> remain retry times: %d", fJob.JobUID,
				reScheduler.JobRemainRetryTimes[fJob.JobUID].Times)
			return false
		}
	}
	return true
}

// GraceDeleteJob grace delete jobs labelled to be deleted gracefully
func (fJob *FaultJob) GraceDeleteJob(ssn *framework.Session, npuJob *plugin.SchedulerJob,
	env plugin.ScheduleEnv) error {
	if fJob == nil {
		return fmt.Errorf("getJobFaultRescheduleLabel fJob object does not exist")
	}
	if ssn == nil {
		return fmt.Errorf("session does not exist")
	}
	if npuJob == nil {
		return fmt.Errorf("schedulerJob does not exist")
	}
	reason, isMasterFault := fJob.getRestartInfos()
	superPod := false
	ids := make([]string, 0)
	if _, ok := npuJob.Annotation[SuperPodAnnoKey]; ok {
		superPod = true
		ids = fJob.getIds(env)
	}
	fJob.updateSuperPodsReschdInfo(env)
	dpi := &deletePodInfo{
		isMasterFault: isMasterFault,
		superPod:      superPod,
		ids:           ids,
		reason:        reason,
	}
	err := fJob.graceDeletePods(ssn, npuJob, env, dpi)
	if err != nil {
		return err
	}
	return nil
}

func (fJob *FaultJob) graceDeletePods(ssn *framework.Session, npuJob *plugin.SchedulerJob, env plugin.ScheduleEnv,
	dpi *deletePodInfo) error {
	for _, fTask := range fJob.FaultTasks {
		npuTask, ok := npuJob.Tasks[fTask.TaskUID]
		if !ok {
			klog.V(util.LogDebugLev).Infof(
				"GraceDeleteJob: npuTask %s has been deleted in session.", fTask.TaskName)
			return fmt.Errorf("npuTask %s not in session", fTask.TaskName)
		}
		if !fJob.isNormalTaskCanBeDelete(fTask, npuJob, env, dpi) {
			continue
		}
		fJob.updateSuperPodMapInfo(env, fTask.TaskName, fTask.NodeName)
		if delErr := npuTask.ForceDeletePodByTaskInf(ssn, dpi.reason, fTask.NodeName); delErr != nil {
			klog.V(util.LogErrorLev).Infof("ForceDeletePodByTaskInf %s: %s.", npuTask.Name, delErr)
		}
	}
	return nil
}

// GraceDeleteJob grace delete jobs labelled to be deleted gracefully
func (fJob *FaultJob) getRestartInfos() (string, bool) {
	var reasonList []FaultReasonList
	var isMasterFault bool
	for _, fTask := range fJob.FaultTasks {
		if fTask.IsFaultTask && fTask.NodeRankIndex == util.Rank0 {
			isMasterFault = true
		}
		if fTask.Reason != nil {
			reasonList = append(reasonList, fTask.Reason...)
		}
	}
	reason := GetTaskRestartReason(reasonList)
	return reason, isMasterFault
}

func (fJob *FaultJob) restartSingleFaultJob(ssn *framework.Session,
	reschedule *ReScheduler, schedulerJob *plugin.SchedulerJob, env plugin.ScheduleEnv) error {

	// delete jobs
	var deleteErr error

	switch fJob.ReScheduleKey {
	case JobForceRescheduleLabelValue, JobGraceRescheduleLabelValue:
		deleteErr = fJob.deleteJobWithLabels(ssn, reschedule, schedulerJob, env)
	case JobOffRescheduleLabelValue:
		deleteErr = fmt.Errorf("job reschedule %s", fJob.ReScheduleKey)
	default:
		deleteErr = fmt.Errorf("not support %s to reschedule job", fJob.ReScheduleKey)
	}

	return deleteErr
}

func (fJob *FaultJob) resetGraceExitCode(k8sClient kubernetes.Interface) {
	var isNotSubHealthFault bool
	for _, faultType := range fJob.FaultTypes {
		if faultType != SubHealthFault {
			isNotSubHealthFault = true
			break
		}
	}
	// if faultTypes contain other fault type, skip update exit code
	if isNotSubHealthFault || !fJob.IsFaultJob {
		return
	}
	updateResetConfigMapWithGraceExit(k8sClient, plugin.ResetInfoCMNamePrefix+
		fJob.ReferenceName, fJob.JobNamespace, plugin.DefaultExitValue)
}

func (fJob *FaultJob) jobInfoInSession(jobs map[api.JobID]*api.JobInfo) *api.JobInfo {
	if fJob.ElasticScheduling == JobOnElasticScheduling {
		for _, job := range jobs {
			// consider elastic scheduling which lead job uid/name change
			if job.Namespace == fJob.JobNamespace &&
				(job.Name == fJob.JobName || util.ReferenceNameOfJob(job) == fJob.ReferenceName) {
				return job
			}
		}
		return nil
	}

	job, ok := jobs[fJob.JobUID]
	if ok && util.UuidOfJob(job) == fJob.UUID {
		return job
	}

	return nil
}

func (fJob *FaultJob) getIsFaultJob() bool {
	for _, task := range fJob.FaultTasks {
		if task.IsFaultTask {
			klog.V(util.LogDebugLev).Infof(
				"job %s isFaultJob set true because of having fault tasks", fJob.JobName)
			return true
		}
	}
	return false
}

func (fJob *FaultJob) recordFaultJobsToLogs() {
	if !fJob.IsFaultJob {
		return
	}
	var tmpfJobInfo miniFaultJob
	tmpfJobInfo.ReferenceName = fJob.ReferenceName
	for _, fTask := range fJob.FaultTasks {
		if !fTask.IsFaultTask {
			continue
		}
		var tmpfTaskInfo miniFaultTask
		tmpfTaskInfo.Reason = fTask.Reason
		tmpfTaskInfo.TaskName = fTask.TaskName
		tmpfTaskInfo.NodeName = fTask.NodeName
		tmpfTaskInfo.FaultType = fTask.faultType
		tmpfTaskInfo.UseCardName = fTask.UseCardName
		tmpfTaskInfo.NodeRankIndex = fTask.NodeRankIndex
		if judgePublicFaultInReason(&tmpfTaskInfo) {
			tmpfTaskInfo.FaultType = PublicFaultType
		}
		tmpfJobInfo.FaultTasks = append(tmpfJobInfo.FaultTasks, tmpfTaskInfo)
	}
	str, err := json.Marshal(tmpfJobInfo)
	if err != nil {
		return
	}
	klog.V(util.LogWarningLev).Infof("Add FaultJob %s fault info: %s", fJob.JobName, string(str))
}

func (fJob *FaultJob) setJobFaultReScheduleLabel(value string) {
	fJob.ReScheduleKey = value
}

func (fJob *FaultJob) setJobElasticReScheduleLabel(value string) {
	fJob.ElasticScheduling = value
}

func (fJob *FaultJob) setFaultTasks(value []FaultTask) {
	fJob.FaultTasks = value
}

func (fJob *FaultJob) setIsSubHealthFault() {
	if !fJob.IsFaultJob {
		return
	}
	for _, faultType := range fJob.FaultTypes {
		if faultType != SubHealthFault {
			fJob.IsSubHealthFault = false
			return
		}
	}
	fJob.IsSubHealthFault = true
}

func (fJob *FaultJob) setIsFaultJob(value bool) {
	fJob.IsFaultJob = value
}

func newFaultJobDefault(job *api.JobInfo, updateTime int64) *FaultJob {
	faultJob := &FaultJob{
		ReScheduleKey:     JobOffRescheduleLabelValue, // off/grace/force
		JobName:           job.Name,
		JobUID:            job.UID,
		JobNamespace:      job.Namespace,
		UpdateTime:        updateTime,
		FaultTypes:        make([]string, 0),
		ElasticScheduling: JobOffElasticScheduling,
		ReferenceName:     util.ReferenceNameOfJob(job),
		UUID:              util.UuidOfJob(job),
		FaultRetryTimes:   faultRetryTimeOfJob(job),
	}
	subHealthyStrategy, exist := job.PodGroup.Labels[util.SubHealthyStrategyLabel]
	if !exist || !util.CheckStrInSlice(subHealthyStrategy,
		[]string{util.SubHealthyIgnore, util.SubHealthyGraceExit, util.SubHealthyForceExit}) {
		subHealthyStrategy = util.SubHealthyIgnore
	}
	faultJob.SubHealthyStrategy = subHealthyStrategy
	return faultJob
}

func faultRetryTimeOfJob(job *api.JobInfo) int {
	value, ok := job.PodGroup.Labels[FaultRetryTimesKey]
	if !ok {
		klog.V(util.LogInfoLev).Infof("get job<%s> label<%s> failed", job.UID, FaultRetryTimesKey)
		return 0
	}
	v, err := strconv.Atoi(value)
	if err != nil {
		klog.V(util.LogInfoLev).Infof("Failed to convert fault-retry-times <%s> of job <%s> into number",
			value, job.UID)
		return 0
	}
	klog.V(util.LogInfoLev).Infof("get job: %s, fault-retry-times: %s", job.Name, value)
	return v
}

func getRealFaultJobForCache(fJobs map[api.JobID]*FaultJob) map[api.JobID]*FaultJob {
	realFaultJobs := make(map[api.JobID]*FaultJob)
	for _, fJob := range fJobs {
		if fJob.IsFaultJob {
			realFaultJobs[fJob.JobUID] = fJob
		}
	}
	return realFaultJobs
}
