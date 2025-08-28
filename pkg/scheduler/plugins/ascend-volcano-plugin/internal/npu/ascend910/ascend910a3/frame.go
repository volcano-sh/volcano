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
Package ascend910a3 is using for A3 affinity schedule.
*/
package ascend910a3

import (
	"fmt"
	"sort"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// GetNodeCardTopology get node card topology by npu index.
func (tp *Base910A3) GetNodeCardTopology(npuIndex []int) map[int][]int {
	cardTopology := make(map[int][]int)
	for _, index := range npuIndex {
		cardId := index / tp.MaxCardNPUNum
		_, ok := cardTopology[cardId]
		if !ok {
			cardTopology[cardId] = make([]int, 0, tp.MaxCardNPUNum)
		}
		cardTopology[cardId] = append(cardTopology[cardId], index)
	}
	return cardTopology
}

// CheckReqNPUEqualNodeNPU check the distributed job require npu num must equal node npu num.
func (tp *Base910A3) CheckReqNPUEqualNodeNPU() *api.ValidateResult {
	for _, task := range tp.Tasks {
		// npu num required by task in distributed job must be node npu num
		if task.ReqNPUNum == tp.MaxNodeNPUNum {
			continue
		}

		if task.ReqNPUNum == 0 && task.Annotation[taskSpec] == schedulerSpec {
			continue
		}
		return &api.ValidateResult{
			Pass:   false,
			Reason: JobCheckFailedReason,
			Message: fmt.Sprintf("distributed job require npu %d, instead of %d",
				tp.MaxNodeNPUNum, task.ReqNPUNum),
		}
	}
	return nil
}

// JudgeNodeAndTaskNPU judge node and task npu is meet.
func (tp *Base910A3) JudgeNodeAndTaskNPU(taskNPU int, nodeNPUTopology []int) error {
	if err := tp.NPUHandler.JudgeNodeAndTaskNPU(taskNPU, nodeNPUTopology); err != nil {
		return err
	}
	if taskNPU == 1 {
		return nil
	}
	fitDies := 0
	cardTopology := tp.GetNodeCardTopology(nodeNPUTopology)
	for _, card := range cardTopology {
		// whole card schedule
		if len(card) == tp.MaxCardNPUNum {
			fitDies += tp.MaxCardNPUNum
		}
	}
	if fitDies < taskNPU {
		return fmt.Errorf("top[%v] is not meet task req(%d)", nodeNPUTopology, taskNPU)
	}
	return nil
}

// SelectNPUFromNode select npu from node.
func (tp *Base910A3) SelectNPUFromNode(task *api.TaskInfo, node plugin.NPUNode, isDistributeJob bool) ([]int, error) {
	taskNPUNum, err := tp.GetTaskReqNPUNum(task)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s GetTaskReqNPUNum err: %#v", tp.GetPluginName(), err)
		return nil, err
	}
	npuTop, err := tp.GetUsableTopFromNode(node, isDistributeJob)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s getUsableTopFromNode err: %#v", tp.GetPluginName(), err)
		return nil, err
	}
	// distributed job schedule
	if tp.NPUTaskNum > 1 {
		// whole node schedule
		if len(npuTop) == tp.MaxNodeNPUNum {
			return npuTop, nil
		}
		return nil, fmt.Errorf("node<%s> top<%v> can not meet task req<%d>", node.Name, len(npuTop), taskNPUNum)
	}
	// single node job schedule
	return tp.selectNPUForStandaloneJob(taskNPUNum, npuTop, node)
}

func (tp *Base910A3) selectNPUForStandaloneJob(taskNPUNum int, npuTop []int,
	node plugin.NPUNode) ([]int, error) {
	sort.Ints(npuTop)
	klog.V(util.LogInfoLev).Infof("%s select %d NPU Node(%s) nodeTop<%v>", tp.GetPluginName(), taskNPUNum,
		node.Name, npuTop)
	cardTop := tp.GetNodeCardTopology(npuTop)

	cardTopSlice := make([][]int, 0)
	for _, card := range cardTop {
		cardTopSlice = append(cardTopSlice, card)
	}
	sort.Slice(cardTopSlice, func(i, j int) bool {
		return len(cardTopSlice[i]) < len(cardTopSlice[j])
	})
	klog.V(util.LogInfoLev).Infof("%s selectNPUFromNode cardTopSlice<%v>", tp.GetPluginName(), cardTopSlice)
	var selected []int
	for _, card := range cardTopSlice {
		if taskNPUNum == 0 {
			break
		}
		// single die schedule
		if taskNPUNum == 1 {
			selected = append(selected, card[0])
			break
		}
		// whole card schedule
		if len(card) == tp.MaxCardNPUNum {
			selected = append(selected, card...)
			taskNPUNum -= tp.MaxCardNPUNum
		}
	}
	return selected, nil
}
