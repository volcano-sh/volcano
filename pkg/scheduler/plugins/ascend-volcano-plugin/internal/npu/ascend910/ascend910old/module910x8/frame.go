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
Package module910x8 is using for HuaWei A800/9000 Ascend910 pin affinity schedule.
*/
package module910x8

import (
	"errors"
	"fmt"
	"reflect"

	"k8s.io/api/core/v1"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// New return npu plugin
func New(name string) base.AscendHandler {
	m := &module910x8{}
	m.SetPluginName(name)
	m.SetAnnoName(util.NPU910CardName)
	m.SetAnnoPreVal(util.NPU910CardNamePre)
	m.SetMaxNodeNPUNum(nodeNPUNumber)
	m.netUnhealthyKey = networkUnhealthyNPU
	m.SetNpuNumInvalidMap(map[int]struct{}{util.NPUIndex3: {}, util.NPUIndex5: {}, util.NPUIndex6: {},
		util.NPUIndex7: {}})
	m.SetIsNetworkFaultAttention(true)
	m.affScoreList = [][]int{
		{util.AffScore0, util.AffScore2, util.AffScore1, util.AffScore3},
		{util.AffScore4, util.AffScore0, util.AffScore2, util.AffScore1},
		{util.AffScore4, util.AffScore4, util.AffScore4, util.AffScore4},
		{util.AffScore4, util.AffScore4, util.AffScore4, util.AffScore0},
	}
	return m
}

// CheckNodeNPUByTask check nod npu meet task req
func (tp *module910x8) CheckNodeNPUByTask(task *api.TaskInfo, node plugin.NPUNode) error {
	if tp == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("CheckNodeNPUByTask err: %s", err.Error())
		return err
	}
	if err := checkNodeLabelOK(node); err != nil {
		return err
	}
	taskNPUNum, err := tp.GetTaskReqNPUNum(task)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s CheckNodeNPUByTask err: %s", tp.GetPluginName(), err.Error())
		return err
	}
	_, ok := tp.Jobs[task.Job]
	if !ok {
		err = fmt.Errorf("task<%s> is not npu task", task.Name)
		klog.V(util.LogErrorLev).Infof("%s CheckNodeNPUByTask err: %s", tp.GetPluginName(), err.Error())
		return err
	}
	nodeTop, err := tp.GetUsableTopFromNode(node, tp.NPUTaskNum > 1)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("task %s CheckNodeNPUByTask err: %s", task.Name, err.Error())
		return err
	}

	if err = judgeNodeAndTaskNPU(taskNPUNum, nodeTop); err != nil {
		klog.V(util.LogErrorLev).Infof("task %s CheckNodeNPUByTask err: %s", task.Name, err.Error())
		return fmt.Errorf("npu topology not meet job require,network unhealthy card is [ %s ]",
			node.Annotation[tp.netUnhealthyKey])
	}
	return nil
}

// ScoreBestNPUNodes core node by calculate task req npu num and node npu top
func (tp *module910x8) ScoreBestNPUNodes(task *api.TaskInfo, nodes []*api.NodeInfo, scoreMap map[string]float64) error {
	if tp == nil || task == nil || len(nodes) == 0 || len(scoreMap) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("ScoreBestNPUNodes %v.", err.Error())
		return err
	}
	taskNPUNum, err := tp.GetTaskReqNPUNum(task)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s ScoreBestNPUNodes err: %s", tp.GetPluginName(), err.Error())
		return err
	}
	for _, node := range nodes {
		if reflect.ValueOf(node).IsNil() {
			continue
		}
		nNode, ok := tp.Nodes[node.Name]
		if !ok {
			klog.V(util.LogWarningLev).Infof("%s ScoreBestNPUNodes node<%s> is not npu node",
				tp.GetPluginName(), node.Name)
			continue
		}
		cardIds, err := tp.GetUsableTopFromNode(nNode, tp.NPUTaskNum > 1)
		if err != nil {
			klog.V(util.LogWarningLev).Infof("%s ScoreBestNPUNodes err: %s", tp.GetPluginName(), err.Error())
			continue
		}
		bestScore, err := tp.getNodeBestScore(taskNPUNum, cardIds)
		if err != nil {
			klog.V(util.LogWarningLev).Infof("%s ScoreBestNPUNodes err: %s", tp.GetPluginName(), err.Error())
			continue
		}
		healthyNPUNum, ok := nNode.Allocate[v1.ResourceName(tp.GetAnnoName())]
		if !ok {
			klog.V(util.LogWarningLev).Infof("%s ScoreBestNPUNodes node<%s> get allocate npu failed",
				tp.GetPluginName(), node.Name)
			continue
		}
		sortScore := tp.MaxNodeNPUNum - len(cardIds)
		scoreMap[node.Name] = nodeWeight*float64(int(healthyNPUNum/util.NPUHexKilo)*npuNumPerHccs-bestScore) +
			float64(sortScore)
	}
	klog.V(util.LogInfoLev).Infof("%s ScoreBestNPUNodes task<%s> scoreMap<%v>", tp.GetPluginName(),
		task.Name, scoreMap)
	return nil
}

// UseAnnotation select npu for task from node
func (tp *module910x8) UseAnnotation(task *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	if tp == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("UseAnnotation %s.", err.Error())
		return nil
	}
	klog.V(util.LogDebugLev).Infof("%s UseAnnotation task<%s> node<%s> resource<%s> Annotation: %s",
		tp.GetPluginName(), task.Name, node.Name, tp.GetAnnoName(), util.SafePrint(node.Annotation))
	selectedNPU, err := tp.selectNPUFromNode(task, node)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("task %s UseAnnotation err:%s.", task.Name, err.Error())
		return nil
	}
	klog.V(util.LogInfoLev).Infof("%s UseAnnotation task<%s> select npu <%v>.",
		tp.GetPluginName(), task.Name, selectedNPU)

	tp.SetNPUTopologyToPodFn(task, selectedNPU, node)
	newNode := tp.NPUHandler.UpdateNodeInfo(node, selectedNPU)
	return newNode
}

func (tp *module910x8) selectNPUFromNode(task *api.TaskInfo, node plugin.NPUNode) ([]int, error) {
	taskNPUNum, err := tp.GetTaskReqNPUNum(task)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s GetTaskReqNPUNum err: %s", tp.GetPluginName(), err.Error())
		return nil, err
	}
	nodeTop, err := tp.GetUsableTopFromNode(node, tp.NPUTaskNum > 1)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s GetUsableTopFromNode err: %s", tp.GetPluginName(), err.Error())
		return nil, err
	}
	if taskNPUNum == nodeNPUNumber {
		if len(nodeTop) == nodeNPUNumber {
			return nodeTop, nil
		}
		err = fmt.Errorf("node<%s> top<%v> can not meet task req<%d>", node.Name, nodeTop, taskNPUNum)
		klog.V(util.LogErrorLev).Infof("%s selectNPUFromNode err: %s", tp.GetPluginName(), err.Error())
		return nil, err
	}
	priorityArray, err := getNPUAllocPriorityArray(taskNPUNum)
	if err != nil {
		klog.V(util.LogErrorLev).Info(err.Error())
		return nil, err
	}
	klog.V(util.LogInfoLev).Infof("%s selectNPUFromNode %s[%d] priority:%v in %v.", tp.GetPluginName(),
		task.Name, taskNPUNum, priorityArray, nodeTop)

	leftHccsArray, rightHccsArray := getNodeHccsArray(nodeTop)
	for _, priority := range priorityArray {
		if priority == len(leftHccsArray) {
			return leftHccsArray[:taskNPUNum], nil
		}
		if priority == len(rightHccsArray) {
			return rightHccsArray[:taskNPUNum], nil
		}
	}
	err = fmt.Errorf("node<%s> top<%v> can not meet task req<%d>", node.Name, len(nodeTop), taskNPUNum)
	klog.V(util.LogErrorLev).Infof("%s selectNPUFromNode err: %s", tp.GetPluginName(), err.Error())
	return nil, err
}
