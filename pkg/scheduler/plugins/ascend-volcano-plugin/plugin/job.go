/*
Copyright(C)2020-2023. Huawei Technologies Co.,Ltd. All rights reserved.

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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

// init the SchedulerJob's init.
func (sJob *SchedulerJob) init(vcJob *api.JobInfo, sHandle *ScheduleHandler) error {
	if sJob == nil || vcJob == nil {
		klog.V(util.LogErrorLev).Infof("SchedulerJob_Init: parameter is nil.")
		return errors.New("parameter is nil")
	}
	if initErr := sJob.initByJobInfo(vcJob); initErr != nil {
		klog.V(util.LogDebugLev).Infof("%s initByJobInfo %s", vcJob.UID, initErr)
		return initErr
	}

	if !sJob.isJobSupportByPlugin() {
		klog.V(util.LogDebugLev).Infof("%s IsJobSupportByPlugin not has suitable plugin.", sJob.Name)
		return fmt.Errorf("%s's plugin not regist", sJob.Name)
	}

	sJob.initSelfPluginByJobInfo(sHandle)
	return nil
}

// Determine if the selectors are exactly equal.
func isSelectorContains(defValue, jobValue string) bool {
	for _, v := range strings.Split(defValue, "|") {
		if strings.EqualFold(v, jobValue) {
			return true
		}
	}

	return false
}

// Determine if the two string has same element.
func isEachStringContainsSameElement(first, second, seq string) bool {
	if first == second {
		return true
	}
	fList := strings.Split(first, seq)
	sList := strings.Split(second, seq)
	for _, vFirst := range fList {
		for _, vSecond := range sList {
			if strings.EqualFold(vFirst, vSecond) {
				return true
			}
		}
	}
	return false
}

// getTaskSelectors get task's selector.
func getTaskSelectors(task *api.TaskInfo) map[string]string {
	if task == nil {
		klog.V(util.LogErrorLev).Infof("getTaskSelectors task nil.")
		return nil
	}
	return task.Pod.Spec.NodeSelector
}

// getTaskLabels get task's Labels.
func getTaskLabels(task *api.TaskInfo) map[string]string {
	if task == nil {
		klog.V(util.LogErrorLev).Infof("getTaskLabels task nil.")
		return nil
	}
	return task.Pod.Labels
}

// getJobSelectorFromVcJob get job selector.
func getJobSelectorFromVcJob(job *api.JobInfo) map[string]string {
	if job == nil {
		klog.V(util.LogErrorLev).Infof("getJobSelectorFromVcJob job nil.")
		return nil
	}
	var jobLabel = make(map[string]string, util.MapInitNum)
	for _, task := range job.Tasks {
		taskSelectors := task.Pod.Spec.NodeSelector
		for k, v := range taskSelectors {
			label, ok := jobLabel[k]
			if !ok {
				// no task selector
				jobLabel[k] = v
				continue
			}
			if isSelectorContains(label, v) {
				// has task selector
				continue
			}
			// use '|' to join tasks
			jobLabel[k] = label + "|" + v
		}
	}
	return jobLabel
}

// getJobLabelFromVcJob get job's label, not task's.
func getJobLabelFromVcJob(job *api.JobInfo) map[string]string {
	if job == nil {
		klog.V(util.LogErrorLev).Infof("getJobLabelFromVcJob job nil.")
		return nil
	}
	resLabel := make(map[string]string, util.MapInitNum)
	if job.PodGroup != nil {
		for labelKey, labelValue := range job.PodGroup.Labels {
			resLabel[labelKey] = labelValue
		}
	}

	for _, task := range job.Tasks {
		taskSelector := getTaskLabels(task)
		for k, v := range taskSelector {
			label, ok := resLabel[k]
			if !ok {
				// no task selector
				resLabel[k] = v
				continue
			}
			if isSelectorContains(label, v) {
				// has task selector
				continue
			}
			// use '|' to join tasks
			resLabel[k] = label + "|" + v
		}
	}
	return resLabel
}

// GetVCJobReqNPUTypeFromJobInfo get job request resource, only NPU.
func GetVCJobReqNPUTypeFromJobInfo(vcJob *api.JobInfo) (string, int, error) {
	if vcJob == nil || vcJob.TotalRequest == nil {
		klog.V(util.LogInfoLev).Infof("GetVCJobReqNPUTypeFromJobInfo nil job's parameter.")
		return "", 0.0, errors.New("nil parameter")
	}

	vcMinResource := getVcjobMinResource(vcJob)
	for k, v := range vcMinResource.ScalarResources {
		// must contain "huawei.com/"
		if strings.Contains(string(k), util.HwPreName) {
			return string(k), int(v / util.NPUHexKilo), nil
		}
	}
	klog.V(util.LogDebugLev).Infof("GetVCJobReqNPUTypeFromJobInfo %+v.", vcMinResource.ScalarResources)
	return "", 0.0, errors.New("nil NPU")
}

func getVcjobMinResource(job *api.JobInfo) *api.Resource {
	if job.PodGroup.Spec.MinResources == nil {
		return api.EmptyResource()
	}
	return api.NewResource(*job.PodGroup.Spec.MinResources)
}

// getVCTaskReqNPUTypeFromTaskInfo get task request resource, only NPU.
func getVCTaskReqNPUTypeFromTaskInfo(vcTask *api.TaskInfo) (string, int) {
	if vcTask == nil || vcTask.Resreq == nil {
		klog.V(util.LogInfoLev).Infof("getVCTaskReqNPUTypeFromTaskInfo nil job's parameter.")
		return "", 0
	}
	for k, v := range vcTask.Resreq.ScalarResources {
		// must contain "huawei.com/"
		if strings.Contains(string(k), util.HwPreName) {
			return string(k), int(v / util.NPUHexKilo)
		}
		continue
	}
	klog.V(util.LogInfoLev).Infof("getVCTaskReqNPUTypeFromTaskInfo %+v.", vcTask.Resreq.ScalarResources)
	return "", 0
}

// GetJobNPUTasks get NPUTask from jobInfo.
func GetJobNPUTasks(vcJob *api.JobInfo) map[api.TaskID]util.NPUTask {
	if vcJob == nil {
		return nil
	}
	if len(vcJob.Tasks) == 0 {
		klog.V(util.LogDebugLev).Infof("GetJobNPUTasks %s not init has no task.", vcJob.Name)
		return nil
	}
	resultMap := make(map[api.TaskID]util.NPUTask, util.MapInitNum)
	for taskID, taskInf := range vcJob.Tasks {
		initVcJobHcclIndex(taskInf)
		name, num := getVCTaskReqNPUTypeFromTaskInfo(taskInf)
		resultMap[taskID] = util.NPUTask{
			Name:       taskInf.Name,
			NameSpace:  taskInf.Namespace,
			ReqNPUName: name,
			ReqNPUNum:  num,
			Label:      getTaskLabels(taskInf),
			VTask:      &util.VTask{},
			NodeName:   taskInf.NodeName,
			Annotation: taskInf.Pod.Annotations,
			PodStatus:  taskInf.Pod.Status.Phase,
		}
	}
	return resultMap
}

// initSelfPluginByJobInfo init job's policyHandler, the deal plugin.
func (sJob *SchedulerJob) initSelfPluginByJobInfo(sHandle *ScheduleHandler) {
	if sJob == nil {
		return
	}

	sJob.policyHandler = sHandle.PolicyBuilder()
}

// isJobInitial Determine if the task is ready.
func initVcJobHcclIndex(taskInf *api.TaskInfo) {
	if taskInf.Pod.Annotations == nil {
		taskInf.Pod.Annotations = make(map[string]string)
	}
	if _, ok := taskInf.Pod.Annotations[podRankIndex]; ok {
		return
	}
	for _, c := range taskInf.Pod.Spec.Containers {
		for _, env := range c.Env {
			if env.Name == vcTaskIndex {
				taskInf.Pod.Annotations[podRankIndex] = env.Value
				return
			}
		}
	}
}

// isJobInitial Determine if the task is ready.
func isJobInitial(job *api.JobInfo) bool {
	return job.ValidTaskNum() >= job.MinAvailable && getJobTerminatingPodNum(job) == 0
}

func getJobTerminatingPodNum(job *api.JobInfo) int {
	tNum := 0
	for _, task := range job.Tasks {
		if task.Pod != nil && task.Pod.DeletionTimestamp != nil {
			tNum++
		}
	}
	return tNum
}

func (sJob *SchedulerJob) recordTorJobServerList(sHandle *ScheduleHandler) {
	if sJob == nil || sHandle == nil || sHandle.Tors == nil || !sJob.IsTorAffinityJob() {
		return
	}
	if _, found := sHandle.ServerListRecordFlag[sJob.Name]; found {
		return
	}
	torShareMap := sHandleTorsToTorShareMap(sHandle)
	jobLog := make(map[string]TorShare)
	var nodeJobs []NodeJobInfo
	for ip, v := range torShareMap {
		nodeJobs = []NodeJobInfo{}
		for _, nodeJob := range v.NodeJobs {
			if isContain(sJob.ReferenceName, nodeJob.JobName) {
				nodeJobs = append(nodeJobs, nodeJob)
			}
		}
		if len(nodeJobs) > 0 {
			jobLog[ip] = TorShare{
				IsHealthy:   v.IsHealthy,
				IsSharedTor: v.IsSharedTor,
				NodeJobs:    nodeJobs,
			}
		}
	}
	dataByte, err := json.Marshal(jobLog)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("failed to convert jobLog to dataByte %v", err)
		return
	}
	sHandle.ServerListRecordFlag[sJob.Name] = struct{}{}
	klog.V(util.LogWarningLev).Infof("record job %s , global tors info  %s", sJob.ReferenceName, string(dataByte))
}

func (sJob *SchedulerJob) updateResetConfigMap(sHandle *ScheduleHandler) {
	if sJob == nil || sHandle == nil {
		return
	}
	if k, ok := sJob.Label[util.SinglePodTag]; !ok || k != util.EnableFunc {
		return
	}
	if _, found := sHandle.ResetCMSetFlag[sJob.Name]; found {
		return
	}
	klog.V(util.LogWarningLev).Infof("job <%v> scheduling as pod scheduling is %v", sJob.Name,
		sHandle.PodScheduleFlag[sJob.Name])
	if k, ok := sJob.Label[util.ProcessRecoverEnable]; ok && k == util.EnableFunc {
		return
	}
	cm, err := k8s.GetConfigMapWithRetry(sHandle.FrameAttr.KubeClient, sJob.NameSpace,
		ResetInfoCMNamePrefix+sJob.ReferenceName)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("get reset cm err by:%s", err)
		return
	}
	cmData, ok := cm.Data[ResetInfoCMDataKey]
	if !ok {
		klog.V(util.LogWarningLev).Infof("get reset cm err by %s is not exist", ResetInfoCMDataKey)
		return
	}
	resetCm := TaskResetInfo{}
	err = json.Unmarshal([]byte(cmData), &resetCm)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("get reset cm unmarshal err:%s", err)
		return
	}
	if upErr := updateResetCm(sJob, sHandle.FrameAttr.KubeClient, resetCm,
		sHandle.PodScheduleFlag[sJob.Name]); upErr != nil {
		klog.V(util.LogWarningLev).Infof("update cm err:%s", upErr)
		return
	}
	sHandle.ResetCMSetFlag[sJob.Name] = struct{}{}
}

func updateResetCm(sJob *SchedulerJob, k8sClient kubernetes.Interface, resetCm TaskResetInfo, isSinglePod bool) error {
	resetCm.RankList = []*TaskDevInfo{}
	resetCm.UpdateTime = time.Now().Unix()
	resetCm.RetryTime++
	if !isSinglePod {
		resetCm.RetryTime = 0
	}
	checkCode := util.MakeDataHash(resetCm)
	str, err := json.Marshal(resetCm)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("get reset cm marshal err:%s", err)
		return err
	}
	upCm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ResetInfoCMNamePrefix + sJob.ReferenceName,
			Namespace: sJob.NameSpace,
			Labels:    map[string]string{"reset": "true"},
		},
		Data: map[string]string{
			util.CmCheckCode:   checkCode,
			ResetInfoCMDataKey: string(str),
			ResetInfoTypeKey:   PodRescheduleRestartType,
		},
	}
	_, err = k8sClient.CoreV1().ConfigMaps(sJob.NameSpace).
		Update(context.TODO(), upCm, metav1.UpdateOptions{})
	if err != nil {
		klog.V(util.LogWarningLev).Infof("set update reset cm err:%s", err)
		return err
	}
	klog.V(util.LogDebugLev).Infof("set update reset cm<%s/%s> success, data: %v", upCm.Namespace, upCm.Name,
		upCm.Data)
	return nil
}

// setJobType get job type, used in vJob temporary.
func (sJob *SchedulerJob) initVTasks(vcJob *api.JobInfo) {
	for tID, t := range vcJob.Tasks {
		tmpTask, ok := sJob.SchedulerJobAttr.NPUJob.Tasks[tID]
		if !ok {
			klog.V(util.LogDebugLev).Infof("%s not in frame tasks.", tID)
			continue
		}
		if initErr := tmpTask.InitVTask(t); initErr != nil {
			klog.V(util.LogErrorLev).Infof("Init vTask %s %s.", tID, initErr)
			continue
		}
		sJob.SchedulerJobAttr.NPUJob.Tasks[tID] = tmpTask
	}
}

func (sJob *SchedulerJob) initByJobInfo(vcJob *api.JobInfo) error {
	name, num, err := GetVCJobReqNPUTypeFromJobInfo(vcJob)
	if err != nil {
		return err
	}
	sJob.initCommonJob(vcJob)
	if sJob.Owner.Kind == ReplicaSetType {
		num *= int(*sJob.Owner.Replicas)
		sJob.Annotation = sJob.Owner.Annotations
	}
	sJob.initNPUJob(vcJob, name, num)
	return nil
}

func (sJob *SchedulerJob) initNPUJob(vcJob *api.JobInfo, npuName string, npuNum int) {
	sJob.SchedulerJobAttr.NPUJob = &util.NPUJob{ReqNPUName: npuName, ReqNPUNum: npuNum, Tasks: GetJobNPUTasks(vcJob)}
	sJob.setJobSubHealthyStrategy()
	sJob.setSpBlock()
	sJob.setNPUTaskNumInJob()
	sJob.setSchedulingTaskNum(vcJob)
	sJob.initVTasks(vcJob)
}

// setNPUTaskNumInJob set the NPU task number in one job. for some task has no NPU.
func (sJob *SchedulerJob) setNPUTaskNumInJob() {
	taskNum := 0
	for _, task := range sJob.Tasks {
		if task.IsNPUTask() {
			taskNum++
		}
	}
	sJob.NPUTaskNum = taskNum
}

func (sJob *SchedulerJob) initCommonJob(vcJob *api.JobInfo) {
	sJob.SchedulerJobAttr.ComJob = util.ComJob{
		Name: vcJob.UID, NameSpace: vcJob.Namespace,
		ReferenceName: util.ReferenceNameOfJob(vcJob),
		Selector:      getJobSelectorFromVcJob(vcJob),
		Label:         getJobLabelFromVcJob(vcJob),
		Status:        string(vcJob.PodGroup.Status.Phase),
		Annotation:    vcJob.PodGroup.Annotations,
	}
}

func (sJob *SchedulerJob) setJobSubHealthyStrategy() {
	subHealthyStrategy, exist := sJob.Label[util.SubHealthyStrategyLabel]
	if !exist || !util.CheckStrInSlice(subHealthyStrategy,
		[]string{util.SubHealthyIgnore, util.SubHealthyGraceExit, util.SubHealthyForceExit}) {
		subHealthyStrategy = util.SubHealthyIgnore
		klog.V(util.LogDebugLev).Infof("job=%s get label error, use default strategy=%s",
			sJob.Name, subHealthyStrategy)
	}
	sJob.SubHealthyStrategy = subHealthyStrategy
}

func (sJob *SchedulerJob) setSpBlock() {
	spBlockStr, ok := sJob.Annotation[util.SuperPodAnnoKey]
	if !ok {
		return
	}
	spBlock, err := strconv.Atoi(spBlockStr)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("get job %s spBlock %s failed %v", sJob.Name, spBlockStr, err)
		return
	}
	sJob.SpBlockNPUNum = spBlock
}

func (sJob *SchedulerJob) setSchedulingTaskNum(vcJob *api.JobInfo) {
	if vcJob.MinAvailable != int32(len(vcJob.Tasks)) {
		sJob.SchedulingTaskNum = defaultSchedulingTaskNum
		return
	}
	sJob.SchedulingTaskNum = sJob.GetSchedulingTaskNum()
}

// UpdateJobPendingMessage update job pending message
func (sJob *SchedulerJob) UpdateJobPendingMessage(message, nodeName string) {
	if _, ok := sJob.Reason[message]; !ok {
		sJob.Reason[message] = make(sets.String)
	}
	sJob.Reason[message].Insert(nodeName)
}

// isNPUJob check SchedulerJob is npu job
func (sJob SchedulerJob) isNPUJob() bool {
	return sJob.policyHandler != nil
}

// validJobFn valid job.
func (sJob SchedulerJob) validJobFn() *api.ValidateResult {
	if sJob.Owner.Kind == ReplicaSetType {
		if len(sJob.Tasks) < int(*sJob.Owner.Replicas) {
			return &api.ValidateResult{
				Message: fmt.Sprintf("job %s task num %d less than replicas %d", sJob.Name, len(sJob.Tasks), *sJob.Owner.Replicas),
				Reason:  "job is not ready",
				Pass:    false,
			}
		}
		i := 0
		for id := range sJob.Tasks {
			task := sJob.Tasks[id]
			task.Index = i
			sJob.Tasks[id] = task
			i++
		}
	}
	if sJob.policyHandler == nil {
		klog.V(util.LogWarningLev).Infof("%s validNPUJob pass by job<%s> policyHandler is nil.", PluginName, sJob.Name)
		return nil
	}
	if result := sJob.policyHandler.ValidNPUJob(); result != nil {
		klog.V(util.LogErrorLev).Infof("%s validNPUJob failed:%s.", PluginName, result.Message)
		return result
	}
	klog.V(util.LogInfoLev).Infof("%s valid ok.", sJob.Name)
	return nil
}

// PreCheckNodePredicate PreCheck Predicate nodes.
func (sJob SchedulerJob) preCheckNodePredicate(taskInfo *api.TaskInfo, vcNode NPUNode) error {
	nodeHealthyStatusByNodeD := vcNode.Annotation[util.NodedNodeHealtyStatuskey]
	if nodeHealthyStatusByNodeD == util.PreSeparateFaultCode {
		klog.V(util.LogDebugLev).Infof("NodePredicate %s failed, cause node is %s.", vcNode.Name,
			nodeHealthyStatusByNodeD)
		return fmt.Errorf("node is %s, due to nodeD reported node status", nodeHealthyStatusByNodeD)
	}
	if err := vcNode.checkNPUResourceStable(sJob); err != nil {
		return err
	}
	if err := sJob.checkNodeNum(taskInfo, vcNode); err != nil {
		return err
	}
	return nil
}

func (sJob SchedulerJob) isPodScheduling() bool {
	return sJob.SchedulingTaskNum != defaultSchedulingTaskNum && sJob.SchedulingTaskNum != 0
}

func (sJob SchedulerJob) preCheckForTorHasAcrossJob(isNSLBv2 bool, jobName api.JobID) bool {
	if sJob.Name == jobName {
		return false
	}
	if sJob.Status != util.PodGroupRunning {
		return false
	}
	if !sJob.IsLargeModelJob() && isNSLBv2 {
		return false
	}
	return true
}

func updatePodsPendingReason(job *api.JobInfo, tID api.TaskID, reason string) {
	if tID != "" {
		if t, ok := job.Tasks[tID]; ok {
			updatePodPendingReason(t, reason)
			return
		}
		return
	}

	for _, task := range job.Tasks {
		updatePodPendingReason(task, reason)
	}
}

// SetJobPendingReason set the pod and podGroup pending reason.
func (sHandle *ScheduleHandler) SetJobPendingReason(vcJob *api.JobInfo, reason interface{}) error {
	if sHandle == nil || vcJob == nil {
		klog.V(util.LogErrorLev).Infof("SetJobPendingReason not init jobs.")
		return errors.New(util.ArgumentError)
	}
	var reasonTmp string

	switch value := reason.(type) {
	case string:
		// job failed
		vcJob.JobFitErrors = value
		reasonTmp = value
		// for write pending reason into pod
		updatePodsPendingReason(vcJob, "", reasonTmp)
	case map[api.TaskID]*api.FitErrors:
		vcJob.NodesFitErrors = value
		for tID, nodeErrors := range value {
			// for write pending reason into pod
			updatePodsPendingReason(vcJob, tID, nodeErrors.Error())
			reasonTmp += nodeErrors.Error()
		}
		if sJob, ok := sHandle.Jobs[vcJob.UID]; ok {
			sHandle.recordJobPendingMessage(sJob)
		}
	default:
		return fmt.Errorf("assert reason(%T) failed", reason)
	}
	// for write pending reason into vcjob
	sHandle.updatePodGroupPendingReason(vcJob, reasonTmp)
	return nil
}

// updatePodGroupPendingReason update pg
func (sHandle *ScheduleHandler) updatePodGroupPendingReason(job *api.JobInfo, reason string) {
	job.JobFitErrors = reason

	if len(job.PodGroup.Status.Conditions) == 0 {
		return
	}

	jc := job.PodGroup.Status.Conditions[0].DeepCopy()
	jc.Type = util.PodGroupUnschedulableType
	jc.Status = v1.ConditionTrue
	jc.LastTransitionTime = metav1.Now()
	jc.TransitionID = string(sHandle.FrameAttr.UID)
	jc.Reason = reason
	jc.Message = reason

	for k, value := range job.PodGroup.Status.Conditions {
		if strings.Contains(value.Message, reason) {
			job.PodGroup.Status.Conditions[k].LastTransitionTime = jc.LastTransitionTime
			job.PodGroup.Status.Conditions[k].TransitionID = jc.TransitionID
			return
		}
	}

	job.PodGroup.Status.Conditions = append(job.PodGroup.Status.Conditions, *jc)
}

// recordJobPendingMessage record the job pending message to log
func (sHandle *ScheduleHandler) recordJobPendingMessage(vcJob SchedulerJob) {
	if util.MakeDataHash(sHandle.PendingMessage[vcJob.Name]) == util.MakeDataHash(vcJob.Reason) {
		return
	}
	for reason, nodes := range vcJob.Reason {
		nodeNames := ""
		for nodeName := range nodes {
			nodeNames += nodeName + " "
		}
		klog.V(util.LogWarningLev).Infof("job %s schedule failed by:%s node list is %s",
			vcJob.Name, reason, nodeNames)
	}
	sHandle.PendingMessage[vcJob.Name] = vcJob.Reason
}

// JobValid the job valid, used by volcano frame.
func (sHandle *ScheduleHandler) JobValid(obj interface{}) *api.ValidateResult {
	klog.V(util.LogInfoLev).Infof("enter job valid")
	defer klog.V(util.LogInfoLev).Infof("leave job valid")

	if sHandle == nil || *sHandle.FrameAttr.IsFirstSession {
		return &api.ValidateResult{Pass: false, Reason: objectNilError,
			Message: fmt.Sprintf("validJobFn [%#v] failed:%s", obj, objectNilError)}
	}
	job, ok := obj.(*api.JobInfo)
	if !ok {
		reason := "job convert failed"
		klog.V(util.LogErrorLev).Infof("%s :%#v.", reason, obj)
		return &api.ValidateResult{Pass: false, Reason: reason,
			Message: fmt.Sprintf("validJobFn [%#v] failed:%s", obj, reason)}
	}

	if !isJobInitial(job) {
		reason := "job is not ready"
		klog.V(util.LogErrorLev).Infof("%s job(%s) not ready:%s.", PluginName, job.Name,
			job.PodGroup.Status.Phase)
		return &api.ValidateResult{Pass: false, Reason: reason,
			Message: fmt.Sprintf("validJobFn [%#v] failed:%s", obj, reason)}
	}
	var result *api.ValidateResult
	defer func() {
		if result != nil && !result.Pass {
			if setErr := sHandle.SetJobPendingReason(job, result.Message); setErr != nil {
				klog.V(util.LogErrorLev).Infof("%s setJobFailed err: %s.", PluginName, util.SafePrint(setErr))
			}
		}
	}()
	if result = validVirtualDevJob(job); result != nil {
		return result
	}

	vcJob, ok := sHandle.Jobs[job.UID]
	if !ok {
		klog.V(util.LogDebugLev).Infof("%s %s not support or init", PluginName, job.Name)
		return nil
	}

	result = vcJob.validJobFn()
	return result
}

// SetJobPendReasonByNodesCase In nodes select case, set node failed and add failed reason.
func (sHandle ScheduleHandler) SetJobPendReasonByNodesCase(job *api.JobInfo) {
	if int32(len(job.Tasks)-len(job.NodesFitErrors)) >= job.MinAvailable {
		klog.V(util.LogDebugLev).Infof("%s not block by nodes(tasks:%d -> jobMin:%d -> nodeErrs:%d).", job.Name,
			len(job.Tasks), job.MinAvailable, len(job.NodesFitErrors))
		return
	}
	if setErr := sHandle.SetJobPendingReason(job, job.NodesFitErrors); setErr != nil {
		klog.V(util.LogErrorLev).Infof("%s setJobFailed err:%s.", PluginName, setErr)
	}
}

// checkNodeNum Check whether the number of cards on the node meets the task requirements.
func (sJob *SchedulerJob) checkNodeNum(taskInfo *api.TaskInfo, vcNode NPUNode) error {
	if sJob == nil || taskInfo == nil {
		return errors.New(objectNilError)
	}
	vcTask, ok := sJob.NPUJob.Tasks[taskInfo.UID]
	if !ok {
		klog.V(util.LogErrorLev).Infof("checkNodeNum %+v.", sJob.SchedulerJobAttr.NPUJob)
		return fmt.Errorf("no %s in SchedulerJob", taskInfo.UID)
	}
	nodeNPUNum, ok := vcNode.Idle[v1.ResourceName(vcTask.ReqNPUName)]
	if !ok {
		return fmt.Errorf("not have %s", vcTask.ReqNPUName)
	}
	if int(nodeNPUNum/util.NPUHexKilo) < vcTask.ReqNPUNum {
		return fmt.Errorf("node not meet task request %s:%d", vcTask.ReqNPUName, vcTask.ReqNPUNum)
	}
	return nil
}

// isJobSupportByPlugin judge job whether has it's plugin.
func (sJob SchedulerJob) isJobSupportByPlugin() bool {
	name := sJob.GetPluginNameByReq()
	if name == "" {
		return false
	}
	return true
}

// GetJobInfoAllocatedTaskNum get job allocated task num
func GetJobInfoAllocatedTaskNum(jobInfo *api.JobInfo) int32 {
	allocated := int32(0)
	if jobInfo == nil {
		return allocated
	}
	for _, task := range jobInfo.Tasks {
		if task.NodeName != "" {
			allocated++
		}
	}
	return allocated
}

func validVirtualDevJob(job *api.JobInfo) *api.ValidateResult {
	npuName, rNpuNum, err := GetVCJobReqNPUTypeFromJobInfo(job)
	if err != nil {
		return nil
	}
	if (ascend910VirtualDevNameReg.MatchString(npuName) || ascend310VirtualDevNameReg.MatchString(npuName)) &&
		(rNpuNum > util.NPUIndex1 || len(job.Tasks) > util.NPUIndex1) {
		err := fmt.Errorf("job %s task num is <%v> request <%v> more than 1 virtual device"+
			"and 1 replicas , keep job pending ", job.Name, len(job.Tasks), rNpuNum)
		klog.V(util.LogDebugLev).Infof(err.Error())
		return &api.ValidateResult{Pass: false, Reason: err.Error(), Message: err.Error()}
	}
	return nil
}
