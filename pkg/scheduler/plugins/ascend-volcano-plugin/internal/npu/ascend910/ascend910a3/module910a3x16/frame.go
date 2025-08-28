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
Package module910a3x16 is using for A3 x16 affinity schedule.
*/
package module910a3x16

import (
	"errors"
	"fmt"

	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/ascend910/ascend910a3"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// New return npu plugin
func New(name string) base.AscendHandler {
	m := &module910a3x16{}
	m.SetPluginName(name)
	m.SetAnnoName(util.NPU910CardName)
	m.SetAnnoPreVal(util.NPU910CardNamePre)
	m.SetMaxNodeNPUNum(ascend910a3.NodeNPUNumber)
	m.SetMaxCardNPUNum(ascend910a3.DieNPUNumber)
	m.SetIsNetworkFaultAttention(true)
	m.setAffinityScore()
	m.NetUnhealthyKey = ascend910a3.NetworkUnhealthyNPU
	return m
}

func (tp *module910a3x16) setAffinityScore() {
	tp.AffScoreList = make([][]int, tp.MaxNodeNPUNum)
	for i := 0; i < tp.MaxNodeNPUNum; i++ {
		tp.AffScoreList[i] = make([]int, tp.MaxNodeNPUNum)
		for j := 0; j < tp.MaxNodeNPUNum; j++ {
			if i > j {
				tp.AffScoreList[i][j] = util.AffScore15
			} else {
				tp.AffScoreList[i][j] = j - i
			}
		}
	}
}

// ValidNPUJob check job req npu num and mode
func (tp *module910a3x16) ValidNPUJob() *api.ValidateResult {
	// single node job, allow require npu [1, 2, 4, 6, 8, 10, 12, 14, 16]
	if tp.NPUTaskNum == 1 {
		if tp.ReqNPUNum == 1 || (tp.ReqNPUNum%tp.MaxCardNPUNum == 0 && tp.ReqNPUNum <= tp.MaxNodeNPUNum) {
			return nil
		}
		return &api.ValidateResult{
			Pass:    false,
			Reason:  ascend910a3.JobCheckFailedReason,
			Message: fmt.Sprintf("single node job require npu [1, 2*n], instead of %d", tp.ReqNPUNum),
		}
	}
	// distributed job required npu must be multiple of tp.MaxNodeNPUNum
	if tp.ReqNPUNum%tp.MaxNodeNPUNum != 0 {
		return &api.ValidateResult{
			Pass:   false,
			Reason: ascend910a3.JobCheckFailedReason,
			Message: fmt.Sprintf("distributed job require npu(%d) should be multiple of node npu(%d)",
				tp.ReqNPUNum, tp.MaxNodeNPUNum),
		}
	}
	return tp.CheckReqNPUEqualNodeNPU()
}

// CheckNodeNPUByTask check nod npu meet task req
func (tp *module910a3x16) CheckNodeNPUByTask(task *api.TaskInfo, node plugin.NPUNode) error {
	if tp == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("CheckNodeNPUByTask err: %v", err)
		return err
	}
	taskNPUNum, err := tp.GetTaskReqNPUNum(task)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s GetTaskReqNPUNum err: %v", task.Name, err)
		return err
	}
	nodeTop, err := tp.GetUsableTopFromNode(node, tp.NPUTaskNum > 1 || tp.IsInstanceOfJobGroup())
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s getUsableTopFromNode err: %v", task.Name, err)
		return err
	}
	if err = tp.JudgeNodeAndTaskNPU(taskNPUNum, nodeTop); err != nil {
		klog.V(util.LogErrorLev).Infof("%s JudgeNodeAndTaskNPU err: %v", task.Name, err)
		return fmt.Errorf("checkNodeNPUByTask %s err: %s", util.NodeNotMeetTopologyWarning, err)
	}
	return nil
}

// ScoreBestNPUNodes score best npu nodes
func (tp *module910a3x16) ScoreBestNPUNodes(task *api.TaskInfo, nodes []*api.NodeInfo,
	sMap map[string]float64) error {
	if tp == nil || task == nil || len(nodes) == 0 || len(sMap) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("ScoreBestNPUNodes %s.", err.Error())
		return err
	}
	taskNPUNum, getErr := tp.GetTaskReqNPUNum(task)
	if getErr != nil {
		klog.V(util.LogErrorLev).Infof("%s GetTaskReqNPUNum %s: %s",
			tp.GetPluginName(), task.Name, getErr.Error())
		return getErr
	}
	for _, node := range nodes {
		if node == nil {
			continue
		}
		bestScore, getSuccess := tp.getBestScoreAndHealthyNPUNum(task, node, taskNPUNum)
		if !getSuccess {
			continue
		}

		sMap[node.Name] = float64(tp.MaxNodeNPUNum *
			(tp.MaxCardNPUNum*tp.MaxNodeNPUNum - bestScore))
		// fast break for large k8s cluster, if best score is 0, we can not find better score than 0
		if bestScore == util.AffScore0 {
			break
		}
	}
	klog.V(util.LogInfoLev).Infof("%s ScoreBestNPUNodes task<%s> sMap<%v>", tp.GetPluginName(),
		task.Name, sMap)
	return nil
}

func (tp *module910a3x16) getBestScoreAndHealthyNPUNum(task *api.TaskInfo,
	node *api.NodeInfo, taskNPUNum int) (int, bool) {
	var bestScore = 0
	nNode, ok := tp.Nodes[node.Name]
	if !ok {
		klog.V(util.LogWarningLev).Infof("%s %s ScoreBestNPUNodes %s is not npu node",
			tp.GetPluginName(), task.Name, node.Name)
		return bestScore, false
	}
	cardIds, err := tp.GetUsableTopFromNode(nNode, tp.NPUTaskNum > 1)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("%s ScoreBestNPUNodes getErr: %#v", tp.GetPluginName(), err)
		return bestScore, false
	}
	bestScore, err = tp.getNodeBestScore(taskNPUNum, cardIds)
	if err != nil {
		klog.V(util.LogWarningLev).Infof("%s ScoreBestNPUNodes getErr: %#v", tp.GetPluginName(), err)
		return bestScore, false
	}

	if taskNPUNum != 1 {
		return bestScore, true
	}

	// single die schedule, we should select has single die node first
	npuTopology := tp.GetNodeCardTopology(cardIds)
	hasSingleDie := false
	for _, npu := range npuTopology {
		if len(npu) != tp.MaxCardNPUNum {
			hasSingleDie = true
			break
		}
	}
	if !hasSingleDie {
		bestScore += tp.MaxNodeNPUNum
	}
	return bestScore, true
}

func (tp *module910a3x16) getNodeBestScore(taskNPUNum int, npuTop []int) (int, error) {
	if taskNPUNum < 1 || taskNPUNum > tp.MaxNodeNPUNum {
		return 0, fmt.Errorf("task req npu num<%d> is invalid", taskNPUNum)
	}
	npuNum := len(npuTop)
	if npuNum < 1 || npuNum > tp.MaxNodeNPUNum {
		return 0, fmt.Errorf("node npu num<%d> is invalid", npuNum)
	}
	return tp.AffScoreList[taskNPUNum-1][npuNum-1], nil
}

// UseAnnotation select npu for task from node
func (tp *module910a3x16) UseAnnotation(task *api.TaskInfo, node plugin.NPUNode) *plugin.NPUNode {
	if tp == nil || task == nil || len(node.Annotation) == 0 {
		err := errors.New(util.ArgumentError)
		klog.V(util.LogErrorLev).Infof("UseAnnotation %s.", err)
		return nil
	}
	klog.V(util.LogDebugLev).Infof("%s UseAnnotation task<%s> node<%s> resource<%s> Annotation: %#v",
		tp.GetPluginName(), task.Name, node.Name, tp.GetAnnoName(), node.Annotation)
	selectedNPU, err := tp.SelectNPUFromNode(task, node, tp.NPUTaskNum > 1)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("%s UseAnnotation err:%v.", task.Name, err)
		return nil
	}
	klog.V(util.LogInfoLev).Infof("%s UseAnnotation %s select %v from node %s.", tp.GetPluginName(), task.Name,
		selectedNPU, node.Name)

	tp.SetNPUTopologyToPodFn(task, selectedNPU, node)
	newNode := tp.UpdateNodeInfo(node, selectedNPU)
	return newNode
}
