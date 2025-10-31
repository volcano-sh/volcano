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
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// for task status
const (
	TaskStatusInit = iota
	TaskStatusAllocate
	TaskStatusWrBack
	TaskStatusRunning
	TaskStatusFailed
)

// TaskAllocated Task allocated struct.
type TaskAllocated struct {
	// like ubuntu
	NodeName string
	// element like 1
	CardName []int
	// element like Ascend310P-2c-100-1
	PhysicsName []string
}

// VTask virtual NPU task struct.
type VTask struct {
	// TASK_STATUS_INIT...
	Status    int
	Allocated TaskAllocated
}

// NPUTask for npu task need.
type NPUTask struct {
	Name       string
	NameSpace  string
	ReqNPUName string
	ReqNPUNum  int
	Annotation map[string]string
	Label      map[string]string
	NodeName   string
	PodStatus  v1.PodPhase
	Index      int
	*VTask
}

// DeleteRealPodByTask generally used by force deletion
func (asTask *NPUTask) DeleteRealPodByTask(ssn *framework.Session, waitTime int64) error {
	if asTask == nil {
		klog.V(LogErrorLev).Infof("DeleteRealPodByTask failed: %s.", ArgumentError)
		return fmt.Errorf(ArgumentError)
	}
	taskInfo, getErr := GetTaskInfoByNameFromSSN(ssn, asTask.Name, asTask.NameSpace)
	if getErr != nil {
		klog.V(LogErrorLev).Infof("%s GetTaskInfoByNameFromSSN: %s", asTask.Name, SafePrint(getErr))
	}
	if taskInfo == nil || taskInfo.Pod == nil {
		klog.V(LogInfoLev).Infof("DeleteRealPodByTask pod does not exist")
		return fmt.Errorf("%s: taskInfo does not exist", ArgumentError)
	}

	deleteOptions := metav1.DeleteOptions{
		GracePeriodSeconds: &waitTime,
		Preconditions:      metav1.NewUIDPreconditions(string(taskInfo.Pod.UID)),
	}

	err := ssn.KubeClient().CoreV1().Pods(taskInfo.Pod.Namespace).Delete(
		context.TODO(), taskInfo.Pod.Name, deleteOptions)
	if err != nil {
		klog.V(LogErrorLev).Infof("Failed to delete %s: %s", taskInfo.Pod.UID, SafePrint(err))
		return err
	}

	klog.V(LogInfoLev).Infof("%s==%v force terminated and removed from etcd", taskInfo.Pod.Name, taskInfo.Pod.UID)
	return nil
}

// EvictJobByTask generally used by grace deletion
func (asTask *NPUTask) EvictJobByTask(ssn *framework.Session, reason string, taskName string) error {
	klog.V(LogDebugLev).Infof("enter EvictJobByTask...")
	if asTask == nil {
		klog.V(LogErrorLev).Infof("EvictJobByTask failed: %s.", ArgumentError)
		return fmt.Errorf(ArgumentError)
	}
	if ssn == nil {
		klog.V(LogErrorLev).Infof("EvictJobByTask failed: %s.", ArgumentError)
		return fmt.Errorf(ArgumentError)
	}
	taskInfo, getErr := GetTaskInfoByNameFromSSN(ssn, taskName, asTask.NameSpace)
	if getErr != nil {
		klog.V(LogErrorLev).Infof("%s GetTaskInfoByNameFromSSN: %s", taskName, SafePrint(getErr))
	}
	err := ssn.Evict(taskInfo, reason)
	if err != nil {
		klog.V(LogErrorLev).Infof("Failed to restart %s : %s", taskName, SafePrint(err))
		if updateErr := asTask.UpdatePodPendingReason(taskInfo, err.Error()); updateErr != nil {
			return updateErr
		}
		return err
	}
	klog.V(LogInfoLev).Infof("Evict %s : %s", taskName, SafePrint(taskInfo.UID))
	if updateErr := asTask.UpdatePodPendingReason(taskInfo, reason); updateErr != nil {
		return updateErr
	}
	return nil
}

// UpdatePodPendingReason update pod pending reason.
func (asTask *NPUTask) UpdatePodPendingReason(taskInfo *api.TaskInfo, reasonTmp string) error {
	if asTask == nil || taskInfo == nil {
		klog.V(LogErrorLev).Infof("UpdatePodPendingReason failed: %s.", ArgumentError)
		return fmt.Errorf(ArgumentError)
	}
	if asTask.Name != taskInfo.Name {
		return fmt.Errorf("NPUTask %s and TaskInfo %s does not match", asTask.Name, taskInfo.Name)
	}
	condition := v1.PodCondition{
		Type:    v1.PodScheduled,
		Status:  v1.ConditionFalse,
		Reason:  v1.PodReasonUnschedulable,
		Message: reasonTmp,
	}
	for _, tmp := range taskInfo.Pod.Status.Conditions {
		if reflect.DeepEqual(tmp, condition) {
			return nil
		}
	}
	taskInfo.Pod.Status.Conditions = append(taskInfo.Pod.Status.Conditions, condition)
	return nil
}

// GetTaskInfoByNameFromSSN get corresponding api.TaskInfo object by given taskName
func GetTaskInfoByNameFromSSN(ssn *framework.Session, taskName, taskNamespace string) (*api.TaskInfo, error) {
	if ssn == nil {
		klog.V(LogErrorLev).Infof("UpdatePodPendingReason failed: %s.", ArgumentError)
		return nil, fmt.Errorf(ArgumentError)
	}
	if len(taskName) == 0 {
		klog.V(LogErrorLev).Infof("GetTaskInfoByNameFromSSN failed: taskName is empty")
		return nil, fmt.Errorf("getTaskInfoByNameFromSSN: taskName is empty")
	}
	for _, jobInfo := range ssn.Jobs {
		for _, taskInfo := range jobInfo.Tasks {
			if taskName == taskInfo.Name && taskNamespace == taskInfo.Namespace {
				return taskInfo, nil
			}
		}
	}
	return nil, fmt.Errorf("did not find task %s in session", taskName)
}

// ForceDeletePodByTaskInf Force delete pod by taskInf.
func (asTask *NPUTask) ForceDeletePodByTaskInf(ssn *framework.Session, reason string, nodeName string) error {
	if !asTask.IsTaskInItsNode(ssn, nodeName) {
		klog.V(LogErrorLev).Infof("%s not in %s, need force delete.", asTask.Name,
			asTask.VTask.Allocated.NodeName)
		deleteErr := asTask.DeleteRealPodByTask(ssn, 0)
		if deleteErr != nil {
			klog.V(LogErrorLev).Infof("GraceDeleteFaultJob %s: %s.", asTask.Name, SafePrint(deleteErr))
		}
		return deleteErr
	}
	if err := asTask.EvictJobByTask(ssn, reason, asTask.Name); err != nil {
		return err
	}
	return nil
}

// IsTaskInItsNode check if task is on the node
func (asTask *NPUTask) IsTaskInItsNode(ssn *framework.Session, nodeName string) bool {
	if ssn == nil || asTask.VTask == nil {
		klog.V(LogErrorLev).Infof("isTaskInItsNode %s.", ArgumentError)
		return false
	}
	nodeInf, ok := ssn.Nodes[nodeName]

	if !ok {
		klog.V(LogErrorLev).Infof("session has no node %v.", nodeName)
		return false
	}

	_, taskFullNameOK := nodeInf.Tasks[api.TaskID(asTask.NameSpace+"/"+asTask.Name)]
	if !taskFullNameOK {
		klog.V(LogErrorLev).Infof("node %s has no task %s.", nodeInf.Name, asTask.Name)
		return false
	}
	return true
}

func getVTaskUsePhysicsNamesByInfo(taskInf *api.TaskInfo) []string {
	value, ok := taskInf.Pod.Annotations[AscendNPUPodRealUse]
	if !ok {
		klog.V(LogErrorLev).Infof("%s's %#v has no %s.",
			taskInf.Name, taskInf.Pod.Annotations, AscendNPUPodRealUse)
		return nil
	}
	return strings.Split(value, ",")
}

// GetVTaskUseTemplate the format is : 0-vir04-3c_ndvpp,0-vir0
func GetVTaskUseTemplate(taskInf *api.TaskInfo) (string, error) {
	value, ok := taskInf.Pod.Annotations[AscendNPUCore]
	if !ok {
		return "", fmt.Errorf("%s's anno has no %s", taskInf.Name, AscendNPUCore)
	}
	if !strings.Contains(value, "vir") {
		return "", fmt.Errorf("%s not dyCut task :%s", taskInf.Name, value)
	}

	temps := strings.Split(value, "-")
	return strings.Join(temps[1:], "-"), nil
}

func (vt *VTask) setVTaskUseCardIDs() {
	if len(vt.Allocated.PhysicsName) == 0 {
		klog.V(LogErrorLev).Infof("%#v nil PhysicsName.", vt.Allocated)
		return
	}
	ids := make([]int, 0)
	for _, value := range vt.Allocated.PhysicsName {
		// value like Ascend310P-2c-100-1_1
		tmps := strings.Split(value, "-")
		realV := strings.Split(tmps[len(tmps)-1], "_")
		if len(realV) == 0 {
			klog.V(LogErrorLev).Infof("get card id from %s==>%#v error.", value, tmps)
			continue
		}
		vInt, err := strconv.Atoi(realV[0])
		if err != nil {
			klog.V(LogErrorLev).Infof("setVTaskUseCardIDs %s.", err)
			continue
		}
		ids = append(ids, vInt)
	}
	vt.Allocated.CardName = ids
}

func (asTask *NPUTask) setVTaskAllocated(taskInf *api.TaskInfo) {
	switch asTask.Status {
	case TaskStatusRunning, TaskStatusWrBack, TaskStatusFailed:
		asTask.VTask.Allocated.NodeName = taskInf.NodeName
		asTask.VTask.Allocated.PhysicsName = getVTaskUsePhysicsNamesByInfo(taskInf)
		asTask.VTask.setVTaskUseCardIDs()
	case TaskStatusAllocate:
		asTask.VTask.Allocated.NodeName = taskInf.NodeName
	default:
		klog.V(LogDebugLev).Infof("setVTaskAllocated %s status %v.", asTask.Name, asTask.Status)
		return
	}
	return
}

func (asTask *NPUTask) setVTaskStatusFromInfo(taskInf *api.TaskInfo) error {
	if _, ok := taskInf.Pod.Annotations[AscendNPUCore]; !ok {
		asTask.Status = TaskStatusInit
		return nil
	}
	asTask.Status = TaskStatusAllocate
	if _, ok := taskInf.Pod.Annotations[AscendNPUPodRealUse]; !ok {
		return nil
	}
	asTask.Status = TaskStatusWrBack
	if taskInf.Status == api.Running {
		asTask.Status = TaskStatusRunning
		return nil
	}
	if taskInf.Status == api.Failed || taskInf.Status == api.Releasing {
		asTask.Status = TaskStatusFailed
		return nil
	}
	return nil
}

// InitVTask init vNPU task.
func (asTask *NPUTask) InitVTask(taskInf *api.TaskInfo) error {
	if setErr := asTask.setVTaskStatusFromInfo(taskInf); setErr != nil {
		return setErr
	}
	asTask.setVTaskAllocated(taskInf)
	return nil
}

// IsVNPUTask Determine whether is the NPU virtual task.
// Dynamic segmentation: huawei.com/npu-core.
// no segmentation: huawei.com/Ascend910.
func (asTask *NPUTask) IsVNPUTask() bool {
	if asTask == nil {
		return false
	}
	if len(strings.Split(asTask.ReqNPUName, "-")) > 1 {
		return true
	}
	return false
}

// IsNPUTask Determine whether is the NPU task.
// Dynamic segmentation: huawei.com/npu-core.
// static segmentation: huawei.com/Ascend910-Y.
// no segmentation: huawei.com/Ascend910.
func (asTask *NPUTask) IsNPUTask() bool {
	return strings.Contains(asTask.ReqNPUName, HwPreName)
}
