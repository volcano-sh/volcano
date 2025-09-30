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
Package chip310x4 is using for HuaWei 310 Ascend pin affinity schedule.
*/
package chip310x4

import (
	"errors"
	"fmt"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// New return npu plugin.
func New(name string) base.AscendHandler {
	chip := &chip310x4{}
	chip.SetPluginName(name)
	chip.SetAnnoName(util.NPU310CardName)
	chip.SetAnnoPreVal(util.NPU310CardNamePre)
	chip.SetMaxNodeNPUNum(maxNodeNPUNum)
	chip.SetMaxCardNPUNum(maxCardNPUNum)
	return chip
}

// UseAnnotation select npu for task from node
func (tp *chip310x4) UseAnnotation(task *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	if tp == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("%s UseAnnotation %s.", SchedulerName, err.Error())
		return nil
	}
	klog.V(util.LogDebugLev).Infof("%s UseAnnotation task<%s> node<%s> resource<%s> Annotation: %s",
		tp.GetPluginName(), task.Name, node.Name, tp.GetAnnoName(), util.SafePrint(node.Annotation))
	selectedNPU, err := tp.SelectNPUFromNode(task, node)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s UseAnnotation failed, err:%s.", tp.GetPluginName(), err.Error())
		return nil
	}
	klog.V(util.LogInfoLev).Infof("%s UseAnnotation task<%s> select npu <%v>.",
		tp.GetPluginName(), task.Name, selectedNPU)

	tp.SetNPUTopologyToPodFn(task, selectedNPU, node)
	return tp.UpdateNodeInfo(node, selectedNPU)
}

// SelectNPUFromNode select npu from node for task
func (tp *chip310x4) SelectNPUFromNode(task *api.TaskInfo, node plugin.NPUNode) ([]int, error) {
	if tp == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("%s SelectNPUFromNode %s.", SchedulerName, err.Error())
		return nil, err
	}
	taskNPUNum, err := tp.GetTaskReqNPUNum(task)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s selectNPUFromNode %s", tp.GetPluginName(), err.Error())
		return nil, err
	}

	nodeTop, err := tp.GetUsableTopFromNode(node, tp.NPUTaskNum > 1)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s selectNPUFromNode %s", tp.GetPluginName(), err.Error())
		return nil, err
	}

	// secure request, avoid index out of range
	if err := tp.JudgeNodeAndTaskNPU(taskNPUNum, nodeTop); err != nil {
		klog.V(util.LogErrorLev).Infof("%s selectNPUFromNode %s", tp.GetPluginName(), err.Error())
		return nil, err
	}

	priorityArray := []int{1, util.NPUIndex2, util.NPUIndex3, util.NPUIndex4}

	cardNumGroups := tp.GetCardNumGroupsFromTop(nodeTop)
	npuNumberIndex := tp.getNPUIndex(cardNumGroups)
	var selectedNPU []int
	for _, priority := range priorityArray {
		curGroups, ok := npuNumberIndex[priority]
		if !ok {
			continue
		}
		if len(curGroups) >= taskNPUNum {
			selectedNPU = append(selectedNPU, curGroups[:taskNPUNum]...)
			return selectedNPU, nil
		}
		selectedNPU = append(selectedNPU, curGroups...)
		taskNPUNum -= len(curGroups)
	}
	err = fmt.Errorf("node<%s> top<%v> can not meet task<%s> req<%d>", node.Name, len(nodeTop),
		task.Name, taskNPUNum)
	klog.V(util.LogErrorLev).Infof("%s selectNPUFromNode err: %s", tp.GetPluginName(), err.Error())
	return nil, err
}
