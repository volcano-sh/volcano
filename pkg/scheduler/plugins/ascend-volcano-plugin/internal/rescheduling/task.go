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
	"errors"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

func (fTask *FaultTask) getNodeRankIndex(task *api.TaskInfo) (string, error) {
	rankIndex, ok := task.Pod.Annotations[podRankIndex]
	if !ok {
		return "", errors.New("nil rankIndex")
	}

	index, err := strconv.Atoi(rankIndex)
	if err != nil {
		return "", fmt.Errorf("convert %s:%s", util.SafePrint(rankIndex), util.SafePrint(err))
	}

	if index > maxRankIndex || index < 0 {
		return "", fmt.Errorf("rankIndex:%d out of limit", index)
	}
	klog.V(util.LogInfoLev).Infof("task %s rankIndex read from pod: %s", task.Name, rankIndex)
	return rankIndex, nil
}

func (fTask *FaultTask) getTaskHealthStateBySubHealth(subHealthyStrategy string) (bool, string) {
	if subHealthyStrategy == util.SubHealthyIgnore || !fTask.HasSubHealthFault {
		return false, PodHealthy
	}
	return true, SubHealthFault
}

func (fTask *FaultTask) getUseCardName(task *api.TaskInfo, cardName string) ([]string, error) {
	strNpu, ok := task.Pod.Annotations[util.AscendNPUPodRealUse]
	if !ok {
		return nil, fmt.Errorf("%s has no NPU from %s", task.Name, cardName)
	}
	taskNPUs := strings.Split(strNpu, ",")
	var taskPhysicsNPUs []string
	for _, taskNPU := range taskNPUs {
		taskNPU = plugin.GetPhysicCardNameFromVChip(taskNPU) // transfer vnpu like Ascend310P-1c-400-1_0
		if util.IsSliceContain(taskNPU, taskPhysicsNPUs) {
			continue
		}
		taskPhysicsNPUs = append(taskPhysicsNPUs, taskNPU)
	}
	return taskPhysicsNPUs, nil
}

// DeleteRealPodByTask delete pod from kubernetes of tasks
func (fTask *FaultTask) DeleteRealPodByTask(kubeClient kubernetes.Interface, waitTime int64) error {
	deleteOptions := v1.DeleteOptions{
		GracePeriodSeconds: &waitTime,
		Preconditions:      v1.NewUIDPreconditions(string(fTask.TaskUID)),
	}

	err := kubeClient.CoreV1().Pods(fTask.TaskNamespace).Delete(
		context.TODO(), fTask.TaskName, deleteOptions)
	if err != nil {
		return err
	}

	klog.V(util.LogInfoLev).Infof("task %s[%v] force terminated and removed from etcd",
		fTask.TaskName, fTask.TaskUID)
	return nil
}

func (fTask *FaultTask) getTaskUseFaultCardHealthState(fNode *FaultNode) []string {
	var nodeUseCardHealthState []string
	for _, taskUseCard := range fTask.UseCardName {
		if util.IsSliceContain(taskUseCard, fNode.UnhealthyNPU) {
			nodeUseCardHealthState = append(nodeUseCardHealthState, NodeCardUnhealthy)
			continue
		}
	}
	return nodeUseCardHealthState
}

func (fTask *FaultTask) setUseCardName(value []string) {
	fTask.UseCardName = value
}

func (fTask *FaultTask) setIsFaultTask(value bool) {
	fTask.IsFaultTask = value
}

func (fTask *FaultTask) setFaultType(value string) {
	fTask.faultType = value
}

func (fTask *FaultTask) setNodeRankIndex(value string) {
	fTask.NodeRankIndex = value
}

func newFaultTaskDefault(task *api.TaskInfo, job *api.JobInfo, env plugin.ScheduleEnv) FaultTask {
	faultTask := FaultTask{
		Reason:             []FaultReasonList{},
		RelationFault:      getRelationFault(task),
		IsFaultRetryEnable: faultRetryTimeOfJob(job) != 0,
		IsSoftwareFault:    getTaskIsSoftwareFault(task),
		TaskName:           task.Name,
		TaskUID:            task.UID,
		TaskNamespace:      task.Namespace,
		NodeName:           task.NodeName,
		PodCreateTime:      task.Pod.CreationTimestamp.Unix(),
		faultType:          NodeHealthy,
	}
	if faultTask.NodeName == "" {
		faultTask.NodeName = env.SuperPodInfo.SuperPodMapFaultTaskNodes[job.UID][task.Name]
	}
	return faultTask
}

func (fTask *FaultTask) initFaultRankIndex() []string {
	taskRank, err := strconv.Atoi(fTask.NodeRankIndex)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("string convert failed by:%s", err)
		return []string{}
	}

	faultRank := make([]string, 0)
	npuNum := len(fTask.UseCardName)
	for i := taskRank * npuNum; i < (taskRank+1)*npuNum; i++ {
		faultRank = append(faultRank, strconv.Itoa(i))
	}
	return faultRank
}

func getRelationFault(task *api.TaskInfo) string {
	if len(task.Pod.Labels) == 0 {
		return ""
	}
	return task.Pod.Labels[taskFaultKey]
}

func getTaskIsSoftwareFault(task *api.TaskInfo) bool {
	if len(task.Pod.Labels) == 0 {
		return false
	}
	return task.Pod.Labels[taskFaultKey] == softwareKey
}
