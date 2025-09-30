/*
Copyright(C)2025. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package vnpu is using for Ascend vnpu affinity schedule.
*/
package vnpu

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/ascend910/ascend910b"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/vnpu"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

type virtual910NPU struct {
	ascend910b.Base910b
	vHandle *vnpu.VirtualNPU
}

// New return npu plugin.
func New(npuName string) base.AscendHandler {
	npuPlugin := &virtual910NPU{}
	npuPlugin.SetPluginName(npuName)
	npuPlugin.SetAnnoName(util.NPU910CardName)
	npuPlugin.SetMaxNodeNPUNum(util.NPUIndex16)
	npuPlugin.SetNpuNumInvalidMap(map[int]struct{}{util.NPUIndex9: {}, util.NPUIndex11: {}, util.NPUIndex13: {},
		util.NPUIndex15: {}})

	npuPlugin.vHandle = &vnpu.VirtualNPU{}
	npuPlugin.vHandle.InitVNPU()

	return npuPlugin
}

// PreStartAction pre-processing actions for vnpu
func (tp *virtual910NPU) PreStartAction(ssn *framework.Session) error {
	klog.V(util.LogDebugLev).Infof("Entering PreStartAction of %s", util.NPU310PCardName)
	defer klog.V(util.LogDebugLev).Infof("Leaving PreStartAction of %s", util.NPU310PCardName)
	if tp == nil || ssn == nil || tp.FrameAttr.KubeClient == nil {
		return fmt.Errorf("%s handler not enabled or ssn is nil: %s", util.NPU310PCardName, util.ArgumentError)
	}
	return tp.vHandle.PreStartAction(&tp.ScheduleEnv, ssn)
}

// ValidNPUJob check job req npu num and mode
func (tp *virtual910NPU) ValidNPUJob() *api.ValidateResult {
	return tp.ValidDyVNPUJob()
}

// CheckNodeNPUByTask check nod npu meet task req
func (tp *virtual910NPU) CheckNodeNPUByTask(task *api.TaskInfo, node plugin.NPUNode) error {
	if tp == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("CheckNodeNPUByTask err: %s", err.Error())
		return err
	}
	taskRes, err := tp.vHandle.GetTaskResource(task, node)
	if err != nil {
		return err
	}
	if node.Label[util.AcceleratorType] != util.Module910bx16AcceleratorType {
		return tp.vHandle.CheckNodeNPUByDyTask(task, node, taskRes)
	}
	return tp.checkNodeNPUForDyCut(task, node, taskRes)
}

func (tp *virtual910NPU) checkNodeNPUForDyCut(task *api.TaskInfo, node plugin.NPUNode, taskRes util.VResource) error {
	if !node.IsResourceWholeCard(taskRes.Aicore) {
		return tp.vHandle.CheckNodeNPUByDyTask(task, node, taskRes)
	}
	nodeTop := node.GetNodeTopForWholeCard()
	taskNPUNum := taskRes.Aicore / node.AiCorePerChip
	return tp.Judge910BNodeAndTaskNPU(taskNPUNum, nodeTop)
}

// ScoreBestNPUNodes score best npu nodes
func (tp *virtual910NPU) ScoreBestNPUNodes(task *api.TaskInfo, nodes []*api.NodeInfo, sMap map[string]float64) error {
	if tp == nil || task == nil || len(sMap) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("ScoreBestNPUNodes %s.", err)
		return err
	}
	klog.V(util.LogInfoLev).Infof("%s ScoreBestNPUNodes task<%s> sMap<%v>", tp.GetPluginName(),
		task.Name, sMap)
	return tp.vHandle.DynamicVNPU.ScoreBestNPUNodes(task, nodes, sMap)
}

// UseAnnotation select npu for task from node
func (tp *virtual910NPU) UseAnnotation(task *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	if tp == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("UseAnnotation %s.", err)
		return nil
	}
	taskRes, err := tp.vHandle.GetTaskResource(task, node)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s UseAnnotation job(%s) get require task resource failed: %s",
			tp.GetPluginName(), tp.Name, err)
		return &node
	}
	if node.Label[util.AcceleratorType] != util.Module910bx16AcceleratorType {
		return tp.vHandle.UseAnnotation(task, node, taskRes, tp.vHandle.VT)
	}
	return tp.useAnnotationForDyCut(task, node, taskRes)
}

func (tp *virtual910NPU) useAnnotationForDyCut(task *api.TaskInfo, node plugin.NPUNode,
	taskRes util.VResource) *plugin.NPUNode {
	if !node.IsResourceWholeCard(taskRes.Aicore) {
		return tp.vHandle.DynamicVNPU.UseAnnotation(task, node, taskRes, tp.vHandle.VT)
	}
	nodeTop := node.GetNodeTopForWholeCard()
	taskNPUNum := taskRes.Aicore / node.AiCorePerChip
	selectNpu, err := tp.SelectNPUByTaskNPUNumAndNodeTop(taskNPUNum, nodeTop)
	if err != nil {
		return nil
	}
	allocChipID := strings.Join(changeIntSliceToString(selectNpu), ",")
	tp.vHandle.SetNPUTopologyToPodFn(task, node, taskRes, allocChipID, tp.vHandle.VT)
	return tp.vHandle.UpdateNodeInfo(node, allocChipID, taskRes)

}

func changeIntSliceToString(npuTop []int) []string {
	s := make([]string, len(npuTop))
	for i, chipId := range npuTop {
		s[i] = strconv.Itoa(chipId)
	}
	return s
}
