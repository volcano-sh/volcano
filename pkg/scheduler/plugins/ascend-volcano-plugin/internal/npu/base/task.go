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
Package base is using for HuaWei Ascend pin affinity schedule.
*/
package base

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"k8s.io/api/core/v1"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// GetTaskReqNPUNum get task require npu num
func (tp *NPUHandler) GetTaskReqNPUNum(task *api.TaskInfo) (int, error) {
	if tp == nil || task == nil {
		return 0, errors.New(util.ArgumentError)
	}
	nJob, jOK := tp.Jobs[task.Job]
	if !jOK {
		err := fmt.Errorf("%s is not npu job", task.Job)
		klog.V(util.LogErrorLev).Infof("GetTaskReqNPUNum err: %s,%s,%#v", err, util.SafePrint(task.Job), tp.Jobs)
		return 0, err
	}
	nTask, tOK := nJob.Tasks[task.UID]
	if !tOK {
		err := fmt.Errorf("task<%s> is not npu task", task.Name)
		klog.V(util.LogErrorLev).Infof("GetTaskReqNPUNum err: %s,%s,%#v", err, util.SafePrint(task.UID), tp.Tasks)
		return 0, err
	}
	klog.V(util.LogDebugLev).Infof("GetTaskReqNPUNum task req npu<%s>-<%d> ", nTask.ReqNPUName, nTask.ReqNPUNum)
	return nTask.ReqNPUNum, nil
}

// SetNPUTopologyToPodFn set task select npu to pod annotation
func (tp *NPUHandler) SetNPUTopologyToPodFn(task *api.TaskInfo, top []int, node plugin.NPUNode) {
	if tp == nil || task == nil || task.Pod == nil || task.Pod.Annotations == nil || len(top) == 0 {
		return
	}
	topologyStr := util.ChangeIntArrToStr(top, tp.GetAnnoPreVal())
	task.Pod.Annotations[tp.GetAnnoName()] = topologyStr
	// to device-plugin judge pending pod.
	tmp := strconv.FormatInt(time.Now().UnixNano(), util.Base10)
	task.Pod.Annotations[util.PodPredicateTime] = tmp
	klog.V(util.LogInfoLev).Infof("%s setNPUTopologyToPod %s==%v top:%s.", tp.GetPluginName(),
		task.Name, tmp, topologyStr)
	tp.setHardwareTypeToPod(task, node)
	tp.setRealUsedNpuToPod(task, top, topologyStr, node)
	tp.setDeployRankIndex(task)
}

func (tp *NPUHandler) setHardwareTypeToPod(task *api.TaskInfo, node plugin.NPUNode) {
	memory, ok := node.Label[nPUChipMemoryKey]
	if !ok {
		klog.V(util.LogDebugLev).Infof("task(%s/%s) node.Label[%s] not exist",
			task.Namespace, task.Name, nPUChipMemoryKey)
		return
	}
	accelerator, ok := node.Label[util.AcceleratorType]
	if !ok {
		klog.V(util.LogDebugLev).Infof("task(%s/%s) node.Label[%s] not exist",
			task.Namespace, task.Name, util.AcceleratorType)
		return
	}

	usage, ok := node.Label[serverUsageKey]
	if !ok {
		klog.V(util.LogDebugLev).Infof("task(%s/%s) node.Label[%s] not exist",
			task.Namespace, task.Name, serverUsageKey)
		return
	}
	// Special requirements for large EP scenarios
	if accelerator == util.Module910bx8AcceleratorType && usage == inferUsage {
		task.Pod.Annotations[podUsedHardwareTypeKey] = fmt.Sprintf("%s-%s", hardwareType800IA2, memory)
		return
	}

	if accelerator == util.ModuleA3x16AcceleratorType && usage == inferUsage {
		task.Pod.Annotations[podUsedHardwareTypeKey] = fmt.Sprintf("%s-%s", hardwareType800IA3, memory)
	}
}

func (tp *NPUHandler) setRealUsedNpuToPod(task *api.TaskInfo, top []int, topologyStr string, node plugin.NPUNode) {
	nodeAllocNum := node.Allocate[v1.ResourceName(tp.GetAnnoName())] / util.NPUHexKilo

	if _, ok := task.Pod.Labels[util.OperatorNameLabelKey]; !ok &&
		(len(top) != int(nodeAllocNum) || len(node.BaseDeviceInfo) == 0) {
		return
	}
	ipMap := make(map[string]*util.NpuBaseInfo)
	err := json.Unmarshal([]byte(node.BaseDeviceInfo), &ipMap)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("setNPUTopologyToPodFn unmarshal device ips err: %s", err)
		return
	}
	if _, ok := task.Pod.Labels[util.OperatorNameLabelKey]; !ok && len(ipMap) != len(top) {
		klog.V(util.LogDebugLev).Infof("device-ips(%d) not equal require npu(%d)", len(ipMap), len(top))
		return
	}
	klog.V(util.LogInfoLev).Info("pod had used all card of node, set configuration in annotation")
	inst := util.Instance{
		PodName:    task.Name,
		ServerID:   node.Address,
		SuperPodId: node.SuperPodID,
		Devices:    make([]util.Device, 0, len(top)),
	}
	sort.Ints(top)
	for _, v := range top {
		deviceName := fmt.Sprintf("%s%d", tp.GetAnnoPreVal(), v)
		inst.Devices = append(inst.Devices, util.Device{
			DeviceID:      strconv.Itoa(v),
			DeviceIP:      ipMap[deviceName].IP,
			SuperDeviceID: strconv.Itoa(int(ipMap[deviceName].SuperDeviceID)),
		})
	}
	marshedInst, err := json.Marshal(inst)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("setNPUTopologyToPodFn marshal err: %s", err.Error())
		return
	}
	task.Pod.Annotations[util.AscendNPUPodRealUse] = topologyStr
	task.Pod.Annotations[util.Pod910DeviceKey] = string(marshedInst)
}

func (tp *NPUHandler) setDeployRankIndex(task *api.TaskInfo) {
	job, ok := tp.Jobs[task.Job]
	if !ok {
		klog.V(util.LogWarningLev).Infof("get job of task %s failed", task.Name)
		return
	}
	if job.Owner.Kind == plugin.ReplicaSetType {
		task.Pod.Annotations[plugin.PodRankIndexKey] = strconv.Itoa(job.Tasks[task.UID].Index)
		klog.V(util.LogInfoLev).Infof("set deploy pod %s rank index to %s", task.Name,
			task.Pod.Annotations[plugin.PodRankIndexKey])
	}
}
