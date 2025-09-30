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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

func (reScheduler *ReScheduler) setGraceOverTime(value int64) {
	reScheduler.GraceDeleteTime = value
}

// createFaultTaskHandler Create FaultTask struct and set the corresponding values
func (reScheduler *ReScheduler) createFaultTaskHandler(job *api.JobInfo, cardName string,
	env plugin.ScheduleEnv, subHealthyStrategy string) ([]FaultTask, error) {
	faultTasks := make([]FaultTask, 0)
	var runTaskNum int
	for _, task := range job.Tasks {
		if task.NodeName != "" {
			runTaskNum++
		}
	}
	for _, task := range job.Tasks {
		faultTask := newFaultTaskDefault(task, job, env)
		// 2. updateNodeRankIndex by pod.Annotation
		tmpNodeRankIndex, err := faultTask.getNodeRankIndex(task)
		if err != nil {
			klog.V(util.LogInfoLev).Infof("getNodeRankIndex %s %s.", task.Name, util.SafePrint(err))
		}
		faultTask.setNodeRankIndex(tmpNodeRankIndex)
		// 3. update UseCardName
		tmpUseCardName, getErr := faultTask.getUseCardName(task, cardName)
		if getErr != nil {
			klog.V(util.LogInfoLev).Infof("getUseCardName %s %s", task.Name, util.SafePrint(getErr))
		}
		faultTask.setUseCardName(tmpUseCardName)
		err = reScheduler.setTaskCardHealthCode(&faultTask)
		if err != nil {
			klog.V(util.LogDebugLev).Infof("setTaskCardHealthCode task %s err %s", task.Name, util.SafePrint(err))
		}
		isFaultTask, healthState := reScheduler.getTaskHealthState(&faultTask, task, subHealthyStrategy)
		klog.V(util.LogInfoLev).Infof("task %s is fault task: %v, health state: %s", task.Name, isFaultTask,
			healthState)
		faultTask.setIsFaultTask(isFaultTask)
		faultTask.setFaultType(healthState)
		faultTasks = append(faultTasks, faultTask)
	}
	return faultTasks, nil
}

// GetRunningJobs get all the running jobs of <UseCardName> type
func (reScheduler *ReScheduler) GetRunningJobs(ssn *framework.Session) map[api.JobID]*api.JobInfo {
	var myJobs = make(map[api.JobID]*api.JobInfo, util.MapInitNum)
	for _, jobInfo := range ssn.Jobs {
		if (jobInfo.PodGroup.Status.Phase != util.PodGroupRunning) &&
			(jobInfo.PodGroup.Status.Phase != util.PodGroupUnknown) { // pending jobs would not be put into cache
			klog.V(util.LogWarningLev).Infof("job %s pod group is not running but %s, skip",
				jobInfo.Name, jobInfo.PodGroup.Status.Phase)
			continue
		}
		schedulerJob, ok := reScheduler.Jobs[jobInfo.UID]
		if !ok || schedulerJob.NPUJob == nil {
			klog.V(util.LogWarningLev).Infof("job %s not in session, skip", jobInfo.UID)
			continue
		}
		// req type is not current card type
		if schedulerJob.ReqNPUNum == 0 {
			klog.V(util.LogWarningLev).Infof("job %s requires npu %d is illegal, skip",
				schedulerJob.Name, schedulerJob.ReqNPUNum)
			continue
		}
		myJobs[jobInfo.UID] = jobInfo
	}
	return myJobs
}

func (reScheduler *ReScheduler) updateNewFaultJobAttr(
	faultJob *FaultJob, jobInfo *api.JobInfo, env plugin.ScheduleEnv) *FaultJob {

	npuJob := reScheduler.Jobs[faultJob.JobUID] // 1. set the value of ReScheduleKey, grace/force/off

	tmpElasticKey := faultJob.GetJobElasticSchedulingLabel(&npuJob)
	faultJob.setJobElasticReScheduleLabel(tmpElasticKey)

	tmpReScheduleKey := faultJob.GetJobFaultRescheduleLabel(&npuJob)
	faultJob.setJobFaultReScheduleLabel(tmpReScheduleKey)
	klog.V(util.LogInfoLev).Infof("job %s set rescheduleLabel %v", jobInfo.Name, tmpReScheduleKey)
	if tmpReScheduleKey == JobOffRescheduleLabelValue {
		klog.V(util.LogInfoLev).Infof("job %s rescheduleLabel off, skip rescheduling.", jobInfo.Name)
		return faultJob
	}
	npuName := util.GetNpuNameFromJobRequire(npuJob.ReqNPUName)
	// 2. create new FaultTask objects and update corresponding attributes
	tmpFaultTasks, err := reScheduler.createFaultTaskHandler(jobInfo, npuName, env, faultJob.SubHealthyStrategy)
	if err != nil {
		klog.V(util.LogInfoLev).Infof("job %s createFaultTaskHandler failed: %s", jobInfo.Name, util.SafePrint(err))
	}
	faultJob.setFaultTasks(tmpFaultTasks)
	tmpIsFaultJob := faultJob.getIsFaultJob() // 4. update the value of IsFaultJob
	klog.V(util.LogDebugLev).Infof("job %s if fault job: %v", faultJob.JobName, tmpIsFaultJob)
	faultJob.setIsFaultJob(tmpIsFaultJob)
	faultJob.SuperPods = npuJob.SuperPods
	// 6. update FaultTypes of the job by status of FaultTasks bound on the job
	if faultJob.IsFaultJob {
		npuJob.SuperPods = nil
		reScheduler.Jobs[faultJob.JobUID] = npuJob
		for _, fTask := range faultJob.FaultTasks {
			if fTask.IsFaultTask {
				faultJob.FaultTypes = append(faultJob.FaultTypes, fTask.faultType)
			}
		}
	}
	faultJob.setIsSubHealthFault()
	klog.V(util.LogDebugLev).Infof("job %s fault types: %v", faultJob.JobName, faultJob.FaultTypes)
	if npuName == util.NPU910CardName { // 5. update JobRankIds of fault cards
		_, ok := reScheduler.JobRemainRetryTimes[faultJob.JobUID]
		if !ok {
			if reScheduler.JobRemainRetryTimes == nil {
				reScheduler.JobRemainRetryTimes = make(map[api.JobID]*RemainRetryTimes)
			}
			reScheduler.JobRemainRetryTimes[faultJob.JobUID] = &RemainRetryTimes{
				UUID:  faultJob.UUID,
				Times: faultJob.FaultRetryTimes,
			}
		}
	}
	return faultJob
}

// AddFaultJobWithSession read all running jobs of given card types and create the corresponding FaultJob objects
func (reScheduler *ReScheduler) AddFaultJobWithSession(
	jobs map[api.JobID]*api.JobInfo, env plugin.ScheduleEnv) error {
	klog.V(util.LogInfoLev).Info("enter AddFaultJobWithSession ... ")
	defer klog.V(util.LogInfoLev).Info("leave AddFaultJobWithSession ... ")
	if reScheduler == nil {
		klog.V(util.LogDebugLev).Infof("AddFaultJobWithSession: %s, nil reScheduler or job", util.ArgumentError)
		return errors.New(util.ArgumentError)
	}
	klog.V(util.LogDebugLev).Infof("ReSchedulerCache fault jobs before add: %#v", reScheduler.FaultJobs)
	nowTime := time.Now().Unix()
	for _, jobInfo := range jobs {
		klog.V(util.LogDebugLev).Infof("ReSchedulerCache considering job %s", jobInfo.Name)
		if _, ok := reScheduler.FaultJobs[jobInfo.UID]; ok {
			// 1. jobs already in cache: go through the continue logic
			continue
		}
		// 2. create FaultJob objects for jobs not in cache but sent by session
		klog.V(util.LogDebugLev).Infof("Add job %s to cache", jobInfo.Name)
		faultJob := newFaultJobDefault(jobInfo, nowTime)
		faultJob = reScheduler.updateNewFaultJobAttr(faultJob, jobInfo, env)
		reScheduler.FaultJobs[jobInfo.UID] = faultJob
	}
	reScheduler.initSuperPodInfo(env)
	klog.V(util.LogDebugLev).Infof("ReSchedulerCache fault jobs after add: %#v", reScheduler.FaultJobs)
	return nil
}

func (reScheduler *ReScheduler) initSuperPodInfo(env plugin.ScheduleEnv) {
	superPodReschdInfo := make(map[api.JobID]map[string][]plugin.SuperNode)
	superPodFaultTaskNodes := make(map[api.JobID][]string)
	superPodMapFaultTaskNodes := make(map[api.JobID]map[string]string)
	for _, fJob := range reScheduler.FaultJobs {
		if value, ok := env.SuperPodInfo.SuperPodReschdInfo[fJob.JobUID]; ok {
			superPodReschdInfo[fJob.JobUID] = value
		}
		if value, ok := env.SuperPodInfo.SuperPodFaultTaskNodes[fJob.JobUID]; ok {
			superPodFaultTaskNodes[fJob.JobUID] = value
		}
		if value, ok := env.SuperPodInfo.SuperPodMapFaultTaskNodes[fJob.JobUID]; ok {
			superPodMapFaultTaskNodes[fJob.JobUID] = value
		}
	}
	env.SuperPodInfo.SuperPodReschdInfo = superPodReschdInfo
	env.SuperPodInfo.SuperPodFaultTaskNodes = superPodFaultTaskNodes
	env.SuperPodInfo.SuperPodMapFaultTaskNodes = superPodMapFaultTaskNodes
}

// GetTaskRestartReason convert to json str
func GetTaskRestartReason(reasonList []FaultReasonList) string {
	str, err := json.Marshal(reasonList)
	if err != nil {
		klog.V(util.LogInfoLev).Infof("convertToReSchedulerJobsMapFromCM marshal: %s.", util.SafePrint(err))
		return ""
	}
	return string(str)
}

// getGraceDeleteFaultJobs get jobs needed to be deleted gracefully, only fault jobs with grace label would be selected
func (reScheduler ReScheduler) getGraceDeleteFaultJobs() []*FaultJob {
	var graceDeleteJobs []*FaultJob
	for _, fJob := range reScheduler.FaultJobs {
		if !fJob.IsFaultJob || fJob.ReScheduleKey != JobGraceRescheduleLabelValue {
			continue
		}
		graceDeleteJobs = append(graceDeleteJobs, fJob)
	}
	return graceDeleteJobs
}

// GetNeedForceDeleteDelayingNPUJobs get fault jobs with grace label but haven't been evicted successfully
func (reScheduler *ReScheduler) GetNeedForceDeleteDelayingNPUJobs(
	schedulerJobs map[api.JobID]plugin.SchedulerJob, ssn *framework.Session) ([]plugin.SchedulerJob, error) {
	klog.V(util.LogInfoLev).Infof("enter GetNeedForceDeleteDelayingNPUJobs ... ")
	defer klog.V(util.LogInfoLev).Infof("leave GetNeedForceDeleteDelayingNPUJobs ... ")
	if reScheduler == nil || len(schedulerJobs) == 0 || ssn == nil {
		klog.V(util.LogDebugLev).Infof("GetNeedForceDeleteDelayingNPUJobs: %s, "+
			"nil reScheduler or schedulerJobs or session", util.ArgumentError)
		return nil, errors.New(util.ArgumentError)
	}
	forceJobs := make([]plugin.SchedulerJob, 0)
	graceDeleteFaultJobs := reScheduler.getGraceDeleteFaultJobs()
	for _, fJob := range graceDeleteFaultJobs {
		jobInfo := fJob.jobInfoInSession(ssn.Jobs)
		if jobInfo == nil {
			klog.V(util.LogDebugLev).Infof(
				"GetNeedForceDeleteDelayingNPUJobs %v not in ssn.Jobs.", fJob.JobName)
			continue

		}
		if fJob.isJobGraceDeleteSuccess(jobInfo) { // if job successfully restarted, do not force delete
			continue
		}
		if !reScheduler.isDelayingJobTimeout(fJob) { // if job not restarted and not time out, do not force delete
			continue
		}
		klog.V(util.LogWarningLev).Infof("grace delete job %s is time out for force delete.", fJob.JobName)
		schedulerJob, ok := schedulerJobs[fJob.JobUID]
		if !ok {
			continue
		}
		forceJobs = append(forceJobs, schedulerJob)
	}
	if len(forceJobs) == 0 {
		klog.V(util.LogInfoLev).Infof("GetNeedForceDeleteDelayingNPUJobs get nil jobs.")
		return nil, errors.New(getNoneJobsErr)
	}
	return forceJobs, nil
}

func (reScheduler *ReScheduler) isDelayingJobTimeout(fJob *FaultJob) bool {
	nowTime := time.Now().Unix()
	createTime := fJob.UpdateTime
	klog.V(util.LogDebugLev).Infof("isDelayingJobTimeOut now: %v create: %v.", nowTime, createTime)
	if nowTime-createTime > reScheduler.GraceDeleteTime {
		klog.V(util.LogInfoLev).Infof("Time out: %v > %v", nowTime-createTime, reScheduler.GraceDeleteTime)
		return true
	}
	return false
}

// synCacheFaultJobWithSession Synchronise FaultJobs in cache by updating the information using current session
func (reScheduler *ReScheduler) synCacheFaultJobWithSession(ssn *framework.Session) {
	klog.V(util.LogInfoLev).Infof("enter synCacheFaultJobWithSession...")
	defer klog.V(util.LogInfoLev).Infof("leave synCacheFaultJobWithSession...")
	updatedFaultJobs := make(map[api.JobID]*FaultJob)
	nowTime := time.Now().Unix()
	for jobId, faultJob := range reScheduler.FaultJobs {
		// 1. cache Jobs exceeded max waiting time should be deleted and treated as normal new jobs
		if nowTime-faultJob.UpdateTime > maxIntervalTime+reScheduler.GraceDeleteTime {
			klog.V(util.LogWarningLev).Infof("delete %s from CM for overTime %v => %v.",
				faultJob.JobName, nowTime, faultJob.UpdateTime)
			continue
		}

		jobInfo := faultJob.jobInfoInSession(ssn.Jobs)
		if jobInfo == nil {
			klog.V(util.LogWarningLev).Infof("faultJob name: %s not in session", faultJob.JobName)
			continue
		}
		// 2. cache Jobs turned normal in session should be deleted ,meaning it has been restarted
		if faultJob.isJobGraceDeleteSuccess(jobInfo) {
			faultJob.updateFaultJobWhenNewPodError(jobInfo)
			klog.V(util.LogDebugLev).Infof("%s grace deleted successful.", faultJob.JobName)
			// delete cache when all pods have been allocated
			reScheduler.singlePodReschedulingUpgrade(jobInfo, faultJob)
			if plugin.GetJobInfoAllocatedTaskNum(jobInfo) >= jobInfo.MinAvailable {
				// if fault scheduling reason is sub healthy fault and job has been rescheduled
				// update the reset config map grace exit code to 0.
				faultJob.resetGraceExitCode(ssn.KubeClient())
				continue
			}
		}
		if faultJob.ElasticScheduling == JobOnElasticScheduling {
			continue
		}
		if !faultJob.DeleteExecutedFlag {
			reScheduler.updateJobHealthCode(faultJob)
			faultJob.updateTaskPodUid(jobInfo)
		}
		reScheduler.setFaultTaskUseNodeLinkDownTime(faultJob)
		updatedFaultJobs[jobId] = faultJob
	}
	reScheduler.setFaultJobs(updatedFaultJobs)
	klog.V(util.LogDebugLev).Infof("ReSchedulerCache fault jobs after sync: %#v", reScheduler.FaultJobs)
}

func (reScheduler *ReScheduler) setFaultTaskUseNodeLinkDownTime(fJob *FaultJob) {
	for _, fTask := range fJob.FaultTasks {
		if !fTask.IsFaultTask || fTask.faultType != util.RelationFault {
			continue
		}
		fNode, ok := reScheduler.FaultNodes[fTask.NodeName]
		if !ok {
			continue
		}
		hasL1LinkDown := false
		for _, deviceFault := range fNode.FaultDeviceList {
			if deviceFault.FaultLevel == NotHandleFault && deviceFault.FaultCode == linkDownFaultCode {
				hasL1LinkDown = true
			}
		}
		if !hasL1LinkDown {
			continue
		}
		fNode.LinkDownTime = fTask.FaultTime
	}
}

func (reScheduler *ReScheduler) singlePodReschedulingUpgrade(jobInfo *api.JobInfo, fJob *FaultJob) {
	if jobInfo.PodGroup.Labels[util.SinglePodTag] != util.EnableFunc {
		return
	}
	if jobInfo.PodGroup.Labels[util.ProcessRecoverEnable] == util.EnableFunc {
		return
	}

	fJob.PendingSessionNum++

	if _, ok := jobInfo.PodGroup.Annotations[SuperPodAnnoKey]; ok {
		if fJob.PendingSessionNum == spPendingTimes {
			fJob.DeleteExecutedFlag = false
		}
	}

	if fJob.PendingSessionNum == pendingTimes {
		fJob.DeleteExecutedFlag = false
	}
}

// SyncJobRemainRetryTimes Synchronise job remain retry times in cache by updating the information using current session
func (reScheduler *ReScheduler) SyncJobRemainRetryTimes(ssn *framework.Session) {
	klog.V(util.LogInfoLev).Info("enter SynJobRemainRetryTimes...")
	defer klog.V(util.LogInfoLev).Info("leave SynJobRemainRetryTimes...")
	if reScheduler == nil {
		klog.V(util.LogErrorLev).Infof("synCacheFaultJobWithSession: %s, nil reScheduler", util.ArgumentError)
		return
	}

	klog.V(util.LogDebugLev).Infof("job remain retry times, sync before: %v", reScheduler.JobRemainRetryTimes)
	defer klog.V(util.LogDebugLev).Infof("job remain retry times, sync after: %v", reScheduler.JobRemainRetryTimes)

	newInfo := make(map[api.JobID]*RemainRetryTimes)
	for jobID, rt := range reScheduler.JobRemainRetryTimes {
		job, ok := ssn.Jobs[jobID]
		if !ok {
			klog.V(util.LogWarningLev).Infof("job<%s> is not session, remain retry times will be delete", jobID)
			continue
		}

		elastic, ok := job.PodGroup.Labels[ElasticSchedulingKey]
		if ok && elastic == JobOnElasticScheduling {
			continue
		}

		if util.UuidOfJob(job) != rt.UUID {
			continue
		}

		newInfo[jobID] = rt
	}
	reScheduler.JobRemainRetryTimes = newInfo
}

// SyncJobRecentRescheduleReason sync recent reschedule records with ssn, to ensure cache is new and sync
func (reScheduler *ReScheduler) SyncJobRecentRescheduleReason(ssn *framework.Session) {
	klog.V(util.LogInfoLev).Info("enter SyncJobRecentRescheduleReason...")
	defer klog.V(util.LogInfoLev).Info("leave SyncJobRecentRescheduleReason...")
	if reScheduler == nil {
		klog.V(util.LogErrorLev).Infof("SyncJobRecentRescheduleReason: %s, nil reScheduler", util.ArgumentError)
		return
	}
	klog.V(util.LogDebugLev).Infof("job reschedule records, sync before: %v", reScheduler.JobRecentRescheduleRecords)
	defer klog.V(util.LogDebugLev).Infof("job reschedule records, sync after: %v",
		reScheduler.JobRecentRescheduleRecords)
	newInfo := make(map[api.JobID]*RescheduleReason)
	for jobID, rescheduleRecord := range reScheduler.JobRecentRescheduleRecords {
		if _, ok := ssn.Jobs[jobID]; !ok {
			// job is no longer in ssn cache, will delete it from cache
			klog.V(util.LogWarningLev).Infof("job<%s> is not session, job reschedule records will delete it", jobID)
			continue

		}
		newInfo[jobID] = rescheduleRecord
	}
	reScheduler.JobRecentRescheduleRecords = newInfo
}

// AddFaultNodeWithSession Add FaultNode objects for new nodes in session not in cache
func (reScheduler *ReScheduler) AddFaultNodeWithSession() {
	klog.V(util.LogInfoLev).Infof("enter AddFaultNodeWithSession ...")
	defer klog.V(util.LogInfoLev).Infof("leave AddFaultNodeWithSession ...")
	if reScheduler == nil {
		klog.V(util.LogErrorLev).Infof("AddFaultNodeWithSession: %s, nil reScheduler", util.ArgumentError)
		return
	}
	nowTime := time.Now().Unix()
	tmpFaultNodes := make(map[string]*FaultNode, len(reScheduler.Nodes))
	for name, npuNode := range reScheduler.Nodes {
		klog.V(util.LogDebugLev).Infof("Adding node %s to reScheduler cache", name)
		chipKind, nameErr := npuNode.GetChipKindFromNpuNode()
		if nameErr != nil {
			klog.V(util.LogDebugLev).Infof("get chip name err by err:%s", nameErr)
			continue
		}
		npuName := util.HwPreName + chipKind
		// 0. Initialise faultNode
		faultNode := newFaultNodeDefault(npuNode.Name, nowTime)
		faultNode.NPUName = npuName
		faultNode.SuperPodID = npuNode.SuperPodID
		faultNode.updateFaultNodesFromDeviceInfo(&npuNode)
		faultNode.updateFaultNodesAttr(&npuNode)
		tmpFaultNodes[name] = faultNode
	}
	reScheduler.setFaultNodes(tmpFaultNodes)
}

// RestartNeedForceDeleteJobs Restart jobs that need to be force deleted
func (reScheduler *ReScheduler) RestartNeedForceDeleteJobs(ssn *framework.Session, env plugin.ScheduleEnv) error {
	klog.V(util.LogInfoLev).Infof("enter RestartNeedForceDeleteJobs...")
	defer klog.V(util.LogInfoLev).Infof("leave RestartNeedForceDeleteJobs...")
	if reScheduler == nil || ssn == nil {
		klog.V(util.LogErrorLev).Infof("RestartNeedForceDeleteJobs failed: %s, nil reScheduler or session",
			util.ArgumentError)
		return errors.New(util.ArgumentError)
	}
	needDeleteNPUJobs, err := reScheduler.GetNeedForceDeleteDelayingNPUJobs(reScheduler.Jobs, ssn)
	if err != nil {
		if err.Error() == getNoneJobsErr {
			return nil
		}
		return err
	}
	klog.V(util.LogDebugLev).Infof("GetNeedForceDeleteDelayingNPUJobs: %#v", needDeleteNPUJobs)
	for _, schedulerJob := range needDeleteNPUJobs {
		for _, faultJob := range reScheduler.FaultJobs {
			if schedulerJob.Name != faultJob.JobUID {
				continue
			}
			klog.V(util.LogWarningLev).Infof("grace delete job %s is timeout,force delete.", schedulerJob.Name)
			if deleteErr := faultJob.ForceDeleteJob(&schedulerJob, env); deleteErr != nil {
				klog.V(util.LogErrorLev).Infof("%s ForceDeleteJob: %s", schedulerJob.Name, util.SafePrint(deleteErr))
			}
		}
	}
	return nil
}

// RestartFaultJobs Restart fault jobs by its corresponding strategy  grace,force,off
func (reScheduler *ReScheduler) RestartFaultJobs(ssn *framework.Session, env plugin.ScheduleEnv) error {
	klog.V(util.LogInfoLev).Infof("enter RestartFaultJobs...")
	defer klog.V(util.LogInfoLev).Infof("leave RestartFaultJobs...")
	if reScheduler == nil || ssn == nil {
		klog.V(util.LogErrorLev).Infof("RestartFaultJobs failed: %s, nil reScheduler or nil session",
			util.ArgumentError)
		return errors.New(util.ArgumentError)
	}
	// 1. Get fault jobs, only faultJobs that haven't been evicted yet should be put into list
	restartFaultJobs := reScheduler.getJobsToBeRestarted(reScheduler.getRealFaultJobs())
	newCacheJobs := reScheduler.getNewCacheJobs(restartFaultJobs)

	klog.V(util.LogDebugLev).Infof("Jobs to be restarted: %#v", restartFaultJobs)
	// 2. Restart fault jobs
	for _, restartFaultJob := range restartFaultJobs {
		schedulerJob, ok := reScheduler.Jobs[restartFaultJob.JobUID]
		if !ok {
			klog.V(util.LogWarningLev).Infof("restartFaultJob %s not in session, has already been deleted",
				schedulerJob.Name)
			continue
		}
		reScheduler.doRestartJob(ssn, env, restartFaultJob, schedulerJob)
		newCacheJobs[restartFaultJob.JobUID] = restartFaultJob // modify restartFlag and put modified fJob into cache
	}
	reScheduler.setFaultJobs(newCacheJobs)
	return nil
}

func (reScheduler *ReScheduler) doRestartJob(ssn *framework.Session, env plugin.ScheduleEnv,
	restartFaultJob *FaultJob, schedulerJob plugin.SchedulerJob) {
	klog.V(util.LogInfoLev).Infof("%s need restart.", restartFaultJob.JobName)
	if restartErr := restartFaultJob.restartSingleFaultJob(
		ssn, reScheduler, &schedulerJob, env); restartErr != nil {
		klog.V(util.LogErrorLev).Infof("RestartJob %s, err: %s.", schedulerJob.Name, util.SafePrint(restartErr))
	} else {
		for i, fTask := range restartFaultJob.FaultTasks {
			if !fTask.IsFaultTask || fTask.faultType != util.RelationFault {
				continue
			}
			restartFaultJob.FaultTasks[i].FaultTime = time.Now().Unix()
		}
		restartFaultJob.recordFaultJobsToLogs()
		// update rescheduling reason
		reScheduler.JobRecentRescheduleRecords[restartFaultJob.JobUID] =
			updateRescheduleReason(reScheduler.JobRecentRescheduleRecords[restartFaultJob.JobUID], restartFaultJob)
		restartFaultJob.DeleteExecutedFlag = true
		if restartFaultJob.faultReason == PodFailed {
			reScheduler.JobRemainRetryTimes[restartFaultJob.JobUID].Times -= 1
			klog.V(util.LogWarningLev).Infof("job<%s> restart success, "+
				"remain retry times reduce 1", restartFaultJob.JobUID)
		}
		klog.V(util.LogWarningLev).Infof("delete %s pod execution success, set flag true", schedulerJob.Name)
	}
	return
}

func updateRescheduleReason(Reasons *RescheduleReason, fJob *FaultJob) *RescheduleReason {
	if fJob == nil {
		klog.V(util.LogErrorLev).Infof("cannot updateRescheduleReason cause nil FaultJob, err:%s", util.ArgumentError)
		return nil
	}
	if Reasons == nil {
		Reasons = &RescheduleReason{
			JobID: fJob.JobUID,
		}
	}
	var rescheduleRecord RescheduleRecord

	rescheduleInfo := convertFaultTaskToRecords(fJob)
	now := time.Now()
	rescheduleRecord.ReasonOfTask = rescheduleInfo
	rescheduleRecord.RescheduleTimeStamp = now.Unix()
	// the time layout is the same with klog reschedule "Add Fault"
	rescheduleRecord.LogFileFormatTime = now.Format("I0102 15:04:05")

	// sort records by timestamp, make the newest records at index 0
	Reasons.RescheduleRecords = append([]RescheduleRecord{rescheduleRecord}, Reasons.RescheduleRecords...)
	Reasons.TotalRescheduleTimes += 1

	if len(Reasons.RescheduleRecords) > MaxRescheduleRecordsNum {
		Reasons.RescheduleRecords = Reasons.RescheduleRecords[:MaxRescheduleRecordsNum]
	}
	return Reasons
}

// convertFaultTaskToRecords convert []FaultTask into []RescheduleTaskReason
func convertFaultTaskToRecords(fJob *FaultJob) []RescheduleTaskReason {
	rescheduleInfo := make([]RescheduleTaskReason, 0)
	if fJob == nil {
		klog.V(util.LogErrorLev).Infof("cannot convertFaultTaskToRecords cause nil FaultJob, "+
			"err:%s", util.ArgumentError)
		return rescheduleInfo
	}
	for _, fTask := range fJob.FaultTasks {
		if !fTask.IsFaultTask {
			continue
		}
		reasonInfo := RescheduleTaskReason{
			RescheduleReason: fTask.faultType,
			PodName:          fTask.TaskName,
			NodeName:         fTask.NodeName,
			NodeRankIndex:    fTask.NodeRankIndex,
		}
		if len(rescheduleInfo) >= MaxRescheduleRecordsNum {
			klog.V(util.LogWarningLev).Infof(
				"there were more than %d task is fault task, will not record them "+
					"into configmap", MaxRescheduleRecordsNum)
			break
		}
		// to avoid too many fault task in one job, fulfill the configmap more than 1Mi
		rescheduleInfo = append(rescheduleInfo, reasonInfo)
	}
	return rescheduleInfo
}

func updateResetConfigMapWithGraceExit(client kubernetes.Interface, name, nameSpace string, exitCode int) {
	cm, err := k8s.GetConfigMapWithRetry(client, nameSpace, name)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("get reset cm err by:%s", err)
		return
	}
	cmData, ok := cm.Data[plugin.ResetInfoCMDataKey]
	if !ok {
		klog.V(util.LogWarningLev).Infof("get reset cm err by %s is not exist", plugin.ResetInfoCMDataKey)
		return
	}
	resetCm := plugin.TaskResetInfo{}
	err = json.Unmarshal([]byte(cmData), &resetCm)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("get reset cm unmarshal err:%s", err)
		return
	}
	resetCm.GracefulExit = exitCode
	checkCode := util.MakeDataHash(resetCm)
	str, err := json.Marshal(resetCm)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("get reset cm marshal err:%s", err)
		return
	}
	upCm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: nameSpace,
			Labels:    map[string]string{"reset": "true"},
		},
		Data: map[string]string{
			CmCheckCode:               checkCode,
			plugin.ResetInfoCMDataKey: string(str),
		},
	}
	_, err = client.CoreV1().ConfigMaps(nameSpace).
		Update(context.TODO(), upCm, metav1.UpdateOptions{})
	if err != nil {
		klog.V(util.LogWarningLev).Infof("set update reset cm err:%s", err)
		return
	}
	klog.V(util.LogInfoLev).Infof("set update reset cm<%s/%s> success, data: %v", upCm.Namespace, upCm.Name,
		upCm.Data)
}

func (reScheduler *ReScheduler) getNewCacheJobs(restartFaultJobs map[api.JobID]*FaultJob) map[api.JobID]*FaultJob {
	newCacheJobs := make(map[api.JobID]*FaultJob)
	for _, fJob := range reScheduler.FaultJobs {
		if _, ok := restartFaultJobs[fJob.JobUID]; ok {
			continue
		}
		newCacheJobs[fJob.JobUID] = fJob
	}
	return newCacheJobs
}

func (reScheduler *ReScheduler) reduceForSubHealthyNodes(smp map[string]float64) {
	if smp == nil {
		return
	}
	for nodeName, score := range smp {
		fNode, exist := reScheduler.FaultNodes[nodeName]
		if !exist || (!fNode.HasCardSubHealthFault && !fNode.HasSwitchSubHealthFault) {
			continue
		}
		smp[nodeName] = math.Max(0.0, score-util.AffScore1)
	}
}

// ScoreBestNPUNodes add scores on scoreMap for normal nodes used by re-scheduling tasks
func (reScheduler *ReScheduler) ScoreBestNPUNodes(task *api.TaskInfo, scoreMap map[string]float64) {
	if reScheduler == nil || task == nil || len(scoreMap) == 0 {
		klog.V(util.LogErrorLev).Infof("ScoreBestNPUNodes: %s, nil reScheduler or task or scoreMap",
			util.ArgumentError)
		return
	}
	klog.V(util.LogDebugLev).Infof("enter rescheduling ScoreBestNPUNodes %s...", task.Name)
	klog.V(util.LogDebugLev).Infof("node score map before add rescheduling weights %#v", scoreMap)
	defer klog.V(util.LogDebugLev).Infof("leave rescheduling ScoreBestNPUNodes ...")
	reScheduler.reduceForSubHealthyNodes(scoreMap)
	fJob := reScheduler.FaultJobs[task.Job] // 2. get faultJob object given the faultTask object
	if fJob == nil {
		klog.V(util.LogInfoLev).Infof("task %s is not in rescheduler cache", task.Name)
		return
	}
	if !fJob.IsFaultJob { // skip adding re-scheduling score for normal jobs
		klog.V(util.LogDebugLev).Infof("task %s belongs to job %s which is not a fault job", task.Name, fJob.JobName)
		return
	}

	reScheduler.reduceScoreForLastFaultNode(fJob, scoreMap)
	klog.V(util.LogDebugLev).Infof("node score map after reduce rescheduling weights %#v", scoreMap)
	return
}

func (reScheduler *ReScheduler) reduceScoreForLastFaultNode(faultJob *FaultJob, scoreMap map[string]float64) {
	faultNodeNames := reScheduler.getFaultNodeNameByFaultJob(faultJob)
	for _, faultNodeName := range faultNodeNames {
		if score, ok := scoreMap[faultNodeName]; ok {
			klog.V(util.LogDebugLev).Infof("fault node<%s> previous used score is reduce", faultNodeName)
			score -= util.AffScore8 * util.AffScore8
			if score < 0 {
				score = 0
			}
			scoreMap[faultNodeName] = score
		}
	}
}

// CheckNodeNPUByTask used in the predicate process of task and node
func (reScheduler *ReScheduler) CheckNodeNPUByTask(task *api.TaskInfo, vcNode *plugin.NPUNode) error {
	klog.V(util.LogDebugLev).Infof("enter rescheduling CheckNodeNPUByTask ...(%s, %s)", task.Name, vcNode.Name)
	defer klog.V(util.LogDebugLev).Infof("leave rescheduling CheckNodeNPUByTask ...(%s, %s)",
		task.Name, vcNode.Name)
	if vcNode == nil {
		return fmt.Errorf("node is not a vc node")
	}
	// 1. jobs should not be scheduled to faultNodes
	if err := reScheduler.checkNodeCurNodeIsFault(vcNode, task); err != nil {
		return err
	}
	klog.V(util.LogDebugLev).Infof("CheckNodeNPUByTask node %s passed rescheduling predicate for task %s",
		vcNode.Name, task.Name)
	return nil
}

func (reScheduler *ReScheduler) checkNodeCurNodeIsFault(vcNode *plugin.NPUNode, task *api.TaskInfo) error {
	if reScheduler == nil {
		return nil
	}
	schedulerJob, ok := reScheduler.Jobs[task.Job]
	if !ok {
		return fmt.Errorf("task corresponding job not in session")
	}
	fNode, exist := reScheduler.FaultNodes[vcNode.Name]
	if !exist {
		return fmt.Errorf("node corresponding not in session")
	}

	if fNode.NodeHealthState == NodeUnhealthy {
		return fmt.Errorf("node is unhealthy")
	}

	if !reScheduler.isJobCanAssignToSubHealthNode(schedulerJob.SubHealthyStrategy,
		fNode.HasCardSubHealthFault || fNode.HasSwitchSubHealthFault) {
		return fmt.Errorf("NodePredicate failed, cardSubHealthy=%v and"+
			"switchSubHealthy=%v, but sub-healthy strategy is %v", fNode.HasCardSubHealthFault,
			fNode.HasSwitchSubHealthFault, schedulerJob.SubHealthyStrategy)
	}
	if fNode.LinkDownTime == 0 {
		klog.V(util.LogInfoLev).Infof("node %s is not fault node, check success", vcNode.Name)
		return nil
	}
	if time.Now().Unix()-fNode.LinkDownTime < linkDownFaultTimeout {
		networkUnhealthyCardName := fmt.Sprintf("%s-%s", fNode.NPUName, CardNetworkUnhealthy)
		k := vcNode.Annotation[networkUnhealthyCardName]
		l1LinkCards := fNode.getL1LinkDownCards()
		if len(l1LinkCards) == 0 {
			return nil
		}
		if k == "" {
			vcNode.Annotation[networkUnhealthyCardName] = strings.Join(l1LinkCards, ",")
		} else {
			vcNode.Annotation[networkUnhealthyCardName] = strings.Join(l1LinkCards, ",") + "," + k
		}
	}
	klog.V(util.LogInfoLev).Infof("node %s is not fault node, check success", vcNode.Name)
	return nil
}

func (reScheduler *ReScheduler) isJobCanAssignToSubHealthNode(jobSubHealthStrategy string, nodeSubHealth bool) bool {
	if nodeSubHealth && jobSubHealthStrategy != util.SubHealthyIgnore {
		return false
	}
	return true
}

func (reScheduler ReScheduler) getFaultNodeNameByFaultJob(faultJob *FaultJob) []string {
	faultNodeNames := make([]string, 0)
	for _, fTask := range faultJob.FaultTasks {
		if fTask.IsFaultTask {
			faultNodeNames = append(faultNodeNames, fTask.NodeName)
		}
	}
	return faultNodeNames
}

func (reScheduler ReScheduler) setTaskCardHealthCode(fTask *FaultTask) error {
	klog.V(util.LogDebugLev).Infof("task %s setTaskCardHealthCode", fTask.TaskName)
	reasonList := make([]FaultReasonList, 0)
	if fTask.NodeName == "" {
		fTask.Reason = reasonList
		return fmt.Errorf("setTaskCardHealthCode fTask %s use node is nil", fTask.TaskName)
	}
	for _, fNode := range reScheduler.FaultNodes {
		if fNode.NodeName != fTask.NodeName {
			continue
		}
		if fNode.NodeHealthState == NodeUnhealthy {
			var reason FaultReasonList
			reason.NodeName = fNode.NodeName
			reason.FaultType = NodeUnhealthy
			reason.FaultCode = NodeFaultCode
			reason.FaultLevel = PreSeparateNPU
			reason.FaultHandling = PreSeparateNPU
			reason.LargeModelFaultLevel = PreSeparateNPU
			reasonList = append(reasonList, reason)
		}
		fTask.HasSubHealthFault = fNode.HasSwitchSubHealthFault || fTask.RelationFault == util.SubHealthFaultStrategy
		tmpReason := setTaskFaultReasonByFaultNode(fTask, fNode)
		reasonList = append(reasonList, tmpReason...)
		break
	}
	if fTask.IsSoftwareFault {
		reasonList = append(reasonList, getTaskSoftwareFaultReason(fTask))
	}
	fTask.Reason = reasonList
	return nil
}

func getTaskSoftwareFaultReason(fTask *FaultTask) FaultReasonList {
	return FaultReasonList{
		NodeName:      fTask.NodeName,
		TaskName:      fTask.TaskName,
		FaultRankList: fTask.initFaultRankIndex(),
	}
}

func setTaskFaultReasonByFaultNode(fTask *FaultTask, fNode *FaultNode) []FaultReasonList {
	reasonList := make([]FaultReasonList, 0)
	for _, cardName := range fTask.UseCardName {
		for _, fCard := range fNode.FaultDeviceList {
			if cardName != fCard.NPUName || fCard.FaultHandling == NotHandleFault {
				continue
			}
			if fCard.FaultHandling == SubHealthFault {
				fTask.HasSubHealthFault = true
			}
			var reason FaultReasonList
			reason.NodeName = fNode.NodeName
			reason.TaskName = fTask.TaskName
			reason.FaultRankList = fTask.initFaultRankIndex()
			reason.FaultDeviceList = fCard
			reasonList = append(reasonList, reason)
		}
	}
	return reasonList
}

func (reScheduler ReScheduler) updateJobHealthCode(fJob *FaultJob) {
	if fJob == nil {
		return
	}
	for index := range fJob.FaultTasks {
		if err := reScheduler.setTaskCardHealthCode(&fJob.FaultTasks[index]); err != nil {
			klog.V(util.LogInfoLev).Infof("setTaskCardHealthCode err:%s", err)
		}
	}
}

// getTaskHealthState return true when unhealthy
func (reScheduler ReScheduler) getTaskHealthState(fTask *FaultTask, task *api.TaskInfo,
	subHealthyStrategy string) (bool, string) {
	klog.V(util.LogDebugLev).Infof("task %s getTaskHealthState", fTask.TaskName)

	if fTask.NodeName == "" {
		return false, NodeHealthy // tasks has not yet been scheduled
	}

	if isFault, state := reScheduler.getTaskHealthStateByNode(fTask); isFault {
		return isFault, state
	}

	if isFault, state := reScheduler.getTaskHealthStateByPod(task); isFault && fTask.IsFaultRetryEnable {
		return isFault, state
	}

	if fTask.RelationFault == util.SeparateFaultStrategy {
		return true, util.RelationFault
	}

	return fTask.getTaskHealthStateBySubHealth(subHealthyStrategy)
}

func (reScheduler *ReScheduler) getTaskHealthStateByNode(fTask *FaultTask) (bool, string) {
	nodeUseCardHealthState := make([]string, 0)
	realFaultNode := reScheduler.GetRealFaultNodes()
	for _, fNode := range realFaultNode {
		if fNode.NodeName == fTask.NodeName {
			if !fNode.IsFaultNode { // if task used node isFaultNode is false, return healthy
				klog.V(util.LogInfoLev).Infof("task %s use healthy node %s, thus task sets %s", fTask.TaskName,
					fNode.NodeName, NodeHealthy)
				return false, NodeHealthy
			}
			if fNode.NodeHealthState == NodeUnhealthy { // if task used node is nodeUnhealthy, return
				klog.V(util.LogInfoLev).Infof("task %s use %s node %s, thus task sets %s", fTask.TaskName,
					NodeUnhealthy, fNode.NodeName, NodeUnhealthy)
				return true, NodeUnhealthy
			}
			nodeUseCardHealthState = fTask.getTaskUseFaultCardHealthState(fNode) // get fault NPUs on task used node
		}
	}
	if util.IsSliceContain(NodeCardUnhealthy, nodeUseCardHealthState) { // if has unhealthy npu, return in advance
		klog.V(util.LogInfoLev).Infof("task %s use %s node, thus task sets %s", fTask.TaskName,
			NodeCardUnhealthy, NodeCardUnhealthy)
		return true, NodeCardUnhealthy
	}
	if _, ok := reScheduler.Nodes[fTask.NodeName]; !ok && !*reScheduler.isFirstSession {
		klog.V(util.LogErrorLev).Infof("task %s use node(%s) which is not ready thus task sets %s", fTask.TaskName,
			fTask.NodeName, NodeUnhealthy)
		return true, NodeUnhealthy
	}
	klog.V(util.LogInfoLev).Infof("task %s all nodes healthy, thus task sets %s", fTask.TaskName, NodeHealthy)
	return false, NodeHealthy
}

func (reScheduler *ReScheduler) getTaskHealthStateByPod(task *api.TaskInfo) (bool, string) {
	if task.Pod.Status.Phase == v1.PodFailed {
		return true, PodFailed
	}
	return false, PodHealthy
}

func (reScheduler ReScheduler) getJobsToBeRestarted(realFaultJobs map[api.JobID]*FaultJob) map[api.JobID]*FaultJob {
	restartFaultJobs := make(map[api.JobID]*FaultJob)
	for _, fJob := range realFaultJobs {
		if fJob.DeleteExecutedFlag {
			continue
		}
		restartFaultJobs[fJob.JobUID] = fJob
	}
	return restartFaultJobs
}
