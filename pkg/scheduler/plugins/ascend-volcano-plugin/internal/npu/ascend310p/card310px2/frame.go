/* Copyright(C)2024. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package card310px2 is using for HuaWei 300I Duo Ascend pin affinity schedule.
*/
package card310px2

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
	card := &card310px2{}
	card.SetMaxCardNPUNum(maxCardNPUNum)
	card.SetMaxNodeNPUNum(maxNodeNPUNum)
	card.SetPluginName(name)
	card.SetAnnoName(util.NPU310PCardName)
	card.SetAnnoPreVal(util.NPU310PCardNamePre)
	card.affScoreList = [][]int{
		{affScore0, affScore1},
		{affScore2, affScore0},
	}
	return card
}

// ValidNPUJob check job req npu num
func (tp *card310px2) ValidNPUJob() *api.ValidateResult {
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
func (tp *card310px2) CheckNodeNPUByTask(task *api.TaskInfo, node plugin.NPUNode) error {
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

	if err = tp.JudgeNodeAndTaskNPU(taskNPUNum, nodeTop); err != nil {
		klog.V(util.LogInfoLev).Infof("%s CheckNodeNPUByTask err: %s", tp.GetPluginName(), err.Error())
		return fmt.Errorf("checkNodeNPUByTask %s : %v", util.NodeNotMeetTopologyWarning, err)
	}

	return nil
}

// ScoreBestNPUNodes score node by calculate task req npu num and node npu top
func (tp *card310px2) ScoreBestNPUNodes(task *api.TaskInfo, nodes []*api.NodeInfo, scoreMap map[string]float64) error {
	if tp == nil || task == nil || len(nodes) == 0 || len(scoreMap) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("%s ScoreBestNPUNodes %s.", SchedulerName, err.Error())
		return err
	}
	taskNPUNum, err := tp.GetTaskReqNPUNum(task)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s ScoreBestNPUNodes err: %s", tp.GetPluginName(), err.Error())
		return err
	}

	if taskNPUNum < 1 || taskNPUNum > tp.MaxCardNPUNum {
		err = fmt.Errorf("task<%s> req npu num<%d> is invalid", task.Name, taskNPUNum)
		klog.V(util.LogErrorLev).Infof("%s ScoreBestNPUNodes err: %s", tp.GetPluginName(), err.Error())
		return err
	}

	tp.getScore(nodes, taskNPUNum, scoreMap)
	return nil
}

func (tp *card310px2) getScore(nodes []*api.NodeInfo, taskNPUNum int, scoreMap map[string]float64) {
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
		bestScore := affScore2
		for _, cardNumGroup := range cardNumGroups {
			num := len(cardNumGroup)
			if num == 0 {
				continue
			}
			bestScore = util.Min(bestScore, tp.affScoreList[taskNPUNum-1][num-1])
			if bestScore == 0 {
				break
			}
		}
		scoreMap[node.Name] = float64(constNPUWeight * (tp.MaxCardNPUNum - bestScore))
	}
}

// UseAnnotation select npu for task from node
func (tp *card310px2) UseAnnotation(task *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	if tp == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("%s UseAnnotation %s.", SchedulerName, err.Error())
		return nil
	}
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
func (tp *card310px2) SelectNPUFromNode(task *api.TaskInfo, node plugin.NPUNode) ([]int, error) {
	if tp == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("%s SelectNPUFromNode %s.", SchedulerName, err.Error())
		return nil, err
	}
	taskNPUNum, err := tp.GetTaskReqNPUNum(task)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s selectNPUFromNode err: %s", tp.GetPluginName(), err.Error())
		return nil, err
	}

	nodeTop, err := tp.GetUsableTopFromNode(node, tp.NPUTaskNum > 1)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s selectNPUFromNode err: %s", tp.GetPluginName(), err.Error())
		return nil, err
	}

	priorityArray, err := getNPUAllocPriorityArray(taskNPUNum)
	if err != nil {
		return nil, err
	}
	klog.V(util.LogInfoLev).Infof("%s %s[%d] priority:%v in %v.", tp.GetPluginName(),
		task.Name, taskNPUNum, priorityArray, nodeTop)

	cardNumGroups := tp.GetCardNumGroupsFromTop(nodeTop)

	for _, priority := range priorityArray {
		for _, cardNumGroup := range cardNumGroups {
			if priority == len(cardNumGroup) {
				selectedNPU := cardNumGroup[:taskNPUNum]
				klog.V(util.LogInfoLev).Infof("%s %s req:%d alloc %v.",
					tp.GetPluginName(), task.Name, taskNPUNum, selectedNPU)
				return selectedNPU, nil
			}
		}
	}
	err = fmt.Errorf("node top<%v> not meet task req<%d>", nodeTop, taskNPUNum)
	klog.V(util.LogErrorLev).Infof("%s selectNPUFromNode err: %s.", tp.GetPluginName(), err.Error())
	return nil, err
}

// ReleaseAnnotation Release used resource.
func (tp *card310px2) ReleaseAnnotation(_ *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	return &node
}
