/*
Copyright(C)2024. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package chip310px2 is using for HuaWei 300I Duo Ascend pin affinity schedule.
*/
package chip310px2

import (
	"errors"
	"fmt"
	"reflect"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// New return npu plugin.
func New(name string) base.AscendHandler {
	chip := &chip310px2{}
	chip.SetPluginName(name)
	chip.SetAnnoName(util.NPU310PCardName)
	chip.SetAnnoPreVal(util.NPU310PCardNamePre)
	chip.SetMaxNodeNPUNum(maxNodeNPUNum)
	chip.SetMaxCardNPUNum(maxCardNPUNum)
	// reqNpuNum (odd) : nodeNpuNum, node include single chip
	chip.affScoreMapOddSingle = map[int]map[int]int{
		1: {1: affScore1, 2: affScore2, 3: affScore3, 4: affScore4, 5: affScore5, 6: affScore6, 7: affScore7},
		3: {3: affScore1, 4: affScore2, 5: affScore3, 6: affScore4, 7: affScore5},
		5: {5: affScore1, 6: affScore2, 7: affScore3},
		7: {7: affScore1},
	}
	// reqNpuNum (odd) : nodeNpuNum, node not include single chip
	chip.affScoreMapOdd = map[int]map[int]int{
		1: {2: affScore9, 4: affScore10, 6: affScore11, 8: affScore12},
		3: {4: affScore9, 6: affScore10, 8: affScore11},
		5: {6: affScore9, 8: affScore10},
		7: {8: affScore9},
	}
	// reqNpuNum (even) : nodeNpuNum
	chip.affScoreMapEven = map[int]map[int]int{
		2: {2: affScore1, 3: affScore2, 4: affScore3, 5: affScore4, 6: affScore5, 7: affScore6, 8: affScore7},
		4: {4: affScore1, 5: affScore2, 6: affScore3, 7: affScore4, 8: affScore5},
		6: {6: affScore1, 7: affScore2, 8: affScore3},
		8: {8: affScore1},
	}
	return chip
}

// ValidNPUJob check job req npu num
func (tp *chip310px2) ValidNPUJob() *api.ValidateResult {
	if tp == nil {
		return &api.ValidateResult{Pass: false, Reason: util.ArgumentError, Message: util.ArgumentError}
	}
	klog.V(util.LogInfoLev).Infof("ValidNPUJob %v.", tp.GetPluginName())
	klog.V(util.LogDebugLev).Infof("%s ValidNPUJob card-mode job<%s> has <%d> tasks.",
		tp.GetPluginName(), tp.Name, len(tp.Tasks))
	if tp.SchedulerJobAttr.Label[util.DistributedInferKey] == util.DistributedInferLabel &&
		tp.NPUTaskNum > 1 {
		err := fmt.Errorf("job <%s> task num <%d> is invalid", tp.Name, len(tp.Tasks))
		klog.V(util.LogErrorLev).Infof("%s ValidNPUJob err: %s", tp.GetPluginName(), err.Error())
		return &api.ValidateResult{
			Pass:    false,
			Reason:  "job task num is invalid",
			Message: err.Error(),
		}
	}
	return tp.NPUHandler.ValidNPUJob()
}

// CheckNodeNPUByTask check nod npu meet task req
func (tp *chip310px2) CheckNodeNPUByTask(task *api.TaskInfo, node plugin.NPUNode) error {
	if tp == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("%s CheckNodeNPUByTask %s.", SchedulerName, err.Error())
		return err
	}
	taskNPUNum, err := tp.GetTaskReqNPUNum(task)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s CheckNodeNPUByTask err: %s", tp.GetPluginName(), err.Error())
		return err
	}

	nodeTop, err := tp.GetUsableTopFromNode(node, tp.NPUTaskNum > 1)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s CheckNodeNPUByTask err: %s", tp.GetPluginName(), err.Error())
		return err
	}

	if node.Label[InferCardKey] != A300IDuoLabel {
		return fmt.Errorf("judgeNodeLabel node card label not right, is %s", node.Label[InferCardKey])
	}

	if err = tp.judgeNodeByTaskNPU(taskNPUNum, nodeTop); err != nil {
		klog.V(util.LogErrorLev).Infof("%s CheckNodeNPUByTask err: %s", tp.GetPluginName(), err.Error())
		return fmt.Errorf("checkNodeNPUByTask %s : %v", util.NodeNotMeetTopologyWarning, err)
	}

	return nil
}

func (tp *chip310px2) judgeNodeByTaskNPU(taskNPU int, nodeNPUTopology []int) error {
	if taskNPU < 1 || taskNPU > tp.MaxNodeNPUNum {
		return fmt.Errorf("judgeNodeByTaskNPU task req num<%d> is invalid", taskNPU)
	}
	if tp.SchedulerJobAttr.Label[util.DistributedInferKey] != util.DistributedInferLabel {
		return nil
	}
	cardNumGroups := tp.GetCardNumGroupsFromTop(nodeNPUTopology)
	fullCards, halfCards := 0, 0
	for _, cardNumGroup := range cardNumGroups {
		if len(cardNumGroup) == tp.MaxCardNPUNum {
			fullCards += 1
		}
		if len(cardNumGroup) == 1 {
			halfCards += 1
		}
	}
	// if taskNPU is even
	if taskNPU&1 == 0 {
		if taskNPU > fullCards*tp.MaxCardNPUNum {
			return fmt.Errorf("judgeNodeByTaskNPU task req num<%d>, node full cards is %d",
				taskNPU, fullCards)
		}
	}
	// if taskNPU is odd
	if taskNPU&1 == 1 {
		if (taskNPU-1 > fullCards*tp.MaxCardNPUNum) || (taskNPU > halfCards+fullCards*tp.MaxCardNPUNum) {
			return fmt.Errorf("judgeNodeByTaskNPU task req num<%d>, node full/half cards is %d/%d",
				taskNPU, fullCards, halfCards)
		}
	}
	return nil
}

// ScoreBestNPUNodes score node by calculate task req npu num and node npu top
func (tp *chip310px2) ScoreBestNPUNodes(task *api.TaskInfo, nodes []*api.NodeInfo, scoreMap map[string]float64) error {
	if tp == nil || task == nil || len(nodes) == 0 || len(scoreMap) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("%s ScoreBestNPUNodes %s.", SchedulerName, err.Error())
		return err
	}
	if tp.SchedulerJobAttr.Label[util.DistributedInferKey] != util.DistributedInferLabel {
		return nil
	}
	taskNPUNum, err := tp.GetTaskReqNPUNum(task)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s ScoreBestNPUNodes err: %s", tp.GetPluginName(), err.Error())
		return err
	}
	if taskNPUNum < 1 || taskNPUNum > tp.MaxNodeNPUNum {
		err = fmt.Errorf("task<%s> req npu num<%d> is invalid", task.Name, taskNPUNum)
		klog.V(util.LogErrorLev).Infof("%s ScoreBestNPUNodes err: %s", tp.GetPluginName(), err.Error())
		return err
	}
	for _, node := range nodes {
		if reflect.ValueOf(node).IsNil() {
			klog.V(util.LogWarningLev).Infof("%s ScoreBestNPUNodes get node nil.", tp.GetPluginName())
			continue
		}
		nNode, ok := tp.Nodes[node.Name]
		if !ok {
			continue
		}
		nodeTop, err := tp.GetUsableTopFromNode(nNode, tp.NPUTaskNum > 1)
		if err != nil {
			klog.V(util.LogWarningLev).Infof("%s ScoreBestNPUNodes err: %s.", tp.GetPluginName(), err.Error())
			continue
		}
		cardNumGroups := tp.GetCardNumGroupsFromTop(nodeTop)
		bestScore := tp.getBestScore(taskNPUNum, nodeTop, cardNumGroups)
		scoreMap[node.Name] = float64(constNPUWeight * (affScore16 - bestScore))
	}
	return nil
}

func (tp *chip310px2) getBestScore(taskNPUNum int, nodeTop []int, cardNumGroups [][]int) int {
	// if taskNPU is even
	if taskNPUNum&1 == 0 {
		score, ok := tp.affScoreMapEven[taskNPUNum][len(nodeTop)]
		if ok {
			return score
		}
	}
	if taskNPUNum&1 == 1 {
		if tp.containSingleChip(cardNumGroups) {
			score, ok := tp.affScoreMapOddSingle[taskNPUNum][len(nodeTop)]
			if ok {
				return score
			}
		}
		score, ok := tp.affScoreMapOdd[taskNPUNum][len(nodeTop)]
		if ok {
			return score
		}
	}
	return affScore16
}

// containSingleChip if node contains single chip
func (tp *chip310px2) containSingleChip(cardNumGroups [][]int) bool {
	for _, cardNumGroup := range cardNumGroups {
		if len(cardNumGroup) == 1 {
			return true
		}
	}
	return false
}

// UseAnnotation select npu for task from node
func (tp *chip310px2) UseAnnotation(task *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
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
func (tp *chip310px2) SelectNPUFromNode(task *api.TaskInfo, node plugin.NPUNode) ([]int, error) {
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
	return tp.getSelectNpu(task, node, nodeTop, taskNPUNum, err)
}

func (tp *chip310px2) getSelectNpu(task *api.TaskInfo, node plugin.NPUNode, nodeTop []int,
	taskNPUNum int, err error) ([]int, error) {
	priorityArray := []int{1, util.NPUIndex2}
	cardNumGroups := tp.GetCardNumGroupsFromTop(nodeTop)
	npuNumberIndex := tp.getNPUIndex(cardNumGroups)
	var selectedNPU []int
	if tp.SchedulerJobAttr.Label[util.DistributedInferKey] == util.DistributedInferLabel {
		priorityArray = []int{util.NPUIndex2}
		if taskNPUNum&1 == 1 {
			firstGroups, ok := npuNumberIndex[1]
			if ok {
				selectedNPU = append(selectedNPU, firstGroups[:1]...)
				taskNPUNum -= 1
			}
			if taskNPUNum == 0 {
				return selectedNPU, nil
			}
		}
	}
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
	err = fmt.Errorf("node<%s> top<%v> can not meet task<%s> req<%d>", node.Name, nodeTop,
		task.Name, taskNPUNum)
	klog.V(util.LogErrorLev).Infof("%s selectNPUFromNode err: %s", tp.GetPluginName(), err.Error())
	return nil, err
}

// ReleaseAnnotation Release used resource.
func (tp *chip310px2) ReleaseAnnotation(_ *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	return &node
}
