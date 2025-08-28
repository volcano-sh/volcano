/*
Copyright(C)2023. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package ascend910b is using for HuaWei Ascend 910B pin affinity schedule.
*/
package ascend910b

import (
	"fmt"

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

func (ab *Base910b) initSelectNodeInf(npuTop []int) SelectNodeInf {
	var sNodeInf SelectNodeInf
	var leftHccsTop []int
	var rightHccsTop []int

	numHCCS := ab.MaxNodeNPUNum / util.NPUIndex2
	for _, cardID := range npuTop {
		if cardID < numHCCS {
			leftHccsTop = append(leftHccsTop, cardID)
		} else {
			rightHccsTop = append(rightHccsTop, cardID)
		}
	}

	sNodeInf.LeftNPUNum = len(leftHccsTop)
	sNodeInf.RightNPUNum = len(rightHccsTop)
	sNodeInf.AllNPUNum = sNodeInf.LeftNPUNum + sNodeInf.RightNPUNum

	if ab.NPUTaskNum > 1 {
		minLen := len(leftHccsTop)
		if minLen > len(rightHccsTop) {
			minLen = len(rightHccsTop)
		}
		sNodeInf.crossNPUNum = minLen * util.NPUIndex2
		return sNodeInf
	}
	for _, leftCardID := range leftHccsTop {
		for _, rightCardID := range rightHccsTop {
			if leftCardID+numHCCS == rightCardID {
				sNodeInf.crossNPUNum = sNodeInf.crossNPUNum + util.NPUIndex2
				break
			}
		}
	}
	return sNodeInf
}

// Judge910BNodeAndTaskNPU Judge 910BNode  wither meet npu task not.
func (ab *Base910b) Judge910BNodeAndTaskNPU(taskNPU int, nodeTop []int) error {
	dealReturnValue := func(value bool) error {
		if value {
			return nil
		}
		meetErr := fmt.Errorf("%v not meet req npu(%d)", nodeTop, taskNPU)
		klog.V(util.LogErrorLev).Infof("%s %v not meet task req:%d.", ab.GetPluginName(), nodeTop, taskNPU)
		return meetErr
	}

	sNodeInf := ab.initSelectNodeInf(nodeTop)
	if taskNPU == ab.MaxNodeNPUNum {
		return dealReturnValue(sNodeInf.AllNPUNum == ab.MaxNodeNPUNum)
	}

	if ab.IsVaildNpuNum(taskNPU) {
		return dealReturnValue((sNodeInf.LeftNPUNum >= taskNPU) || (sNodeInf.RightNPUNum >= taskNPU) ||
			(taskNPU > ab.MaxNodeNPUNum/util.NPUIndex2 && taskNPU <= sNodeInf.crossNPUNum))
	}
	return dealReturnValue(false)
}

// GetNodeBestScore Get node core
func (ab *Base910b) GetNodeBestScore(taskNPUNum int, npuTop []int) (int, error) {
	var bestScore = len(ab.AffScoreList)
	sNodeInf := ab.initSelectNodeInf(npuTop)
	if sNodeInf.AllNPUNum < 1 ||
		sNodeInf.AllNPUNum > ab.MaxNodeNPUNum {
		return 0, fmt.Errorf("node top %v is invalid for %v", npuTop, sNodeInf)
	}

	var err = fmt.Errorf("node %v is not meet task req %d", npuTop, taskNPUNum)
	if taskNPUNum == ab.MaxNodeNPUNum {
		if len(npuTop) == ab.MaxNodeNPUNum {
			return 0, nil
		}
		return 0, err
	}

	switch {
	case taskNPUNum > ab.MaxNodeNPUNum/util.NPUIndex2:
		bestScore = ab.AffScoreList[(taskNPUNum/util.NPUIndex2)-1][(sNodeInf.crossNPUNum/util.NPUIndex2)-1]
	case sNodeInf.RightNPUNum == 0:
		bestScore = ab.AffScoreList[taskNPUNum-1][sNodeInf.LeftNPUNum-1]
	case sNodeInf.LeftNPUNum == 0:
		bestScore = ab.AffScoreList[taskNPUNum-1][sNodeInf.RightNPUNum-1]
	default:
		bestScore = util.Min(ab.AffScoreList[taskNPUNum-1][sNodeInf.RightNPUNum-1],
			ab.AffScoreList[taskNPUNum-1][sNodeInf.LeftNPUNum-1])
	}
	if bestScore == len(ab.AffScoreList) {
		return 0, err
	}
	return bestScore, nil
}

// SelectNPUByTaskNPUNumAndNodeTop select npu by task num and node card topo
func (tp *Base910b) SelectNPUByTaskNPUNumAndNodeTop(taskNPUNum int, nodeTop []int) ([]int, error) {
	if taskNPUNum == tp.MaxNodeNPUNum {
		if len(nodeTop) == tp.MaxNodeNPUNum {
			return nodeTop, nil
		}
		err := fmt.Errorf("node top<%v> can not meet task req<%d>", nodeTop, taskNPUNum)
		klog.V(util.LogErrorLev).Infof("%s SelectNPUFromNode err: %s", tp.GetPluginName(), err.Error())
		return nil, err
	}
	priorityArray, err := tp.GetNPUAllocPriorityArray(taskNPUNum)
	if err != nil {
		klog.V(util.LogErrorLev).Info(err.Error())
		return nil, err
	}
	klog.V(util.LogInfoLev).Infof("SelectNPUFromNode %s[%d] priority:%v in %v.",
		tp.GetPluginName(), taskNPUNum, priorityArray, nodeTop)

	leftHccsArray, rightHccsArray, samePlaceHccsArray := tp.GetNodeHccsArray(nodeTop, tp.NPUTaskNum > 1)
	for _, priority := range priorityArray {
		if priority == len(leftHccsArray) {
			return leftHccsArray[:taskNPUNum], nil
		}
		if priority == len(rightHccsArray) {
			return rightHccsArray[:taskNPUNum], nil
		}
		if priority == len(samePlaceHccsArray) {
			return samePlaceHccsArray[:taskNPUNum], nil
		}
	}
	err = fmt.Errorf("node top<%v> can not meet task req<%d>", len(nodeTop), taskNPUNum)
	klog.V(util.LogErrorLev).Infof("%s SelectNPUFromNode err: %s", tp.GetPluginName(), err.Error())
	return nil, err
}

// GetNPUAllocPriorityArray get npu allocate array
func (tp *Base910b) GetNPUAllocPriorityArray(taskNPUNumber int) ([]int, error) {
	var err error
	if !tp.IsVaildNpuNum(taskNPUNumber) {
		err = fmt.Errorf("illegal request npu number: %d", taskNPUNumber)
		klog.V(util.LogErrorLev).Infof("%s %s.", tp.GetPluginName(), err)
		return nil, err
	}
	var priorityArray []int
	if taskNPUNumber == tp.MaxNodeNPUNum {
		return []int{tp.MaxNodeNPUNum}, nil
	}
	if taskNPUNumber <= tp.MaxNodeNPUNum/util.NPUIndex2 {
		for i := taskNPUNumber; i <= tp.MaxNodeNPUNum/util.NPUIndex2; i++ {
			priorityArray = append(priorityArray, i)
		}
		return priorityArray, nil
	}
	if taskNPUNumber > tp.MaxNodeNPUNum/util.NPUIndex2 {
		for i := taskNPUNumber; i <= tp.MaxNodeNPUNum; i = i + util.NPUIndex2 {
			priorityArray = append(priorityArray, i)
		}
		return priorityArray, nil
	}
	return priorityArray, nil
}

// GetNodeHccsArray get node hccs array
func (tp *Base910b) GetNodeHccsArray(nodeTop []int, isMultNpuReplica bool) ([]int, []int, []int) {
	var leftHccsArray []int
	var rightHccsArray []int

	idCutNum := tp.MaxNodeNPUNum / util.NPUIndex2
	for _, v := range nodeTop {
		if v < idCutNum {
			leftHccsArray = append(leftHccsArray, v)
			continue
		}
		rightHccsArray = append(rightHccsArray, v)
	}
	crossHccsArray := getCrossHccsArray(leftHccsArray, rightHccsArray, isMultNpuReplica, idCutNum)
	return leftHccsArray, rightHccsArray, crossHccsArray
}

func getCrossHccsArray(leftHccsArray, rightHccsArray []int, isMultNpuReplica bool, idCutNum int) []int {
	var crossHccsArray []int
	if isMultNpuReplica {
		minLen := len(leftHccsArray)
		if minLen > len(rightHccsArray) {
			minLen = len(rightHccsArray)
		}
		for i := 0; i < minLen; i++ {
			crossHccsArray = append(crossHccsArray, leftHccsArray[i], rightHccsArray[i])
		}
		return getCrossHccsArrayByCutNum(crossHccsArray, idCutNum)
	}
	for _, leftCardID := range leftHccsArray {
		for _, rightCardID := range rightHccsArray {
			if leftCardID+idCutNum == rightCardID {
				crossHccsArray = append(crossHccsArray, leftCardID, rightCardID)
				break
			}
		}
	}
	return getCrossHccsArrayByCutNum(crossHccsArray, idCutNum)
}

func getCrossHccsArrayByCutNum(crossHccsArray []int, idCutNum int) []int {
	// npu num must bigger than hccs's npu number, if task is cross hccs
	if len(crossHccsArray) <= idCutNum {
		return []int{}
	}
	return crossHccsArray
}
