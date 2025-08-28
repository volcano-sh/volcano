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
Package module910x8 is using for HuaWei Ascend pin affinity schedule.
*/
package module910x8

import (
	"fmt"

	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

func judgeNodeAndTaskNPU(taskNPU int, nodeTop []int) error {
	var reFlag = false

	sNodeInf := initSelectNodeInf(nodeTop)

	switch taskNPU {
	case 0, 1, npuIndex2, npuNumPerHccs:
		reFlag = (sNodeInf.leftNPUNum >= taskNPU) || (sNodeInf.rightNPUNum >= taskNPU)
	case nodeNPUNumber:
		reFlag = sNodeInf.allNPUNum == nodeNPUNumber
	default:
		// single pod(task) cannot require npu not belong to mode
		// this kind job has been deal with job logical
		klog.V(util.LogErrorLev).Infof("judgeNodeAndTaskNPU err: task req npu is invalid.")
	}

	if reFlag {
		return nil
	}
	meetErr := fmt.Errorf("%v not meet req npu(%d)", nodeTop, taskNPU)
	klog.V(util.LogErrorLev).Infof("cardIDs:<%v> not meet task reqNum<%d>.", nodeTop, taskNPU)
	return meetErr
}

func getNPUAllocPriorityArray(taskNPUNumber int) ([]int, error) {
	var priorityArray []int

	switch taskNPUNumber {
	case 0:
		priorityArray = []int{0, 1, npuIndex2, npuIndex3, npuNumPerHccs}
	case 1:
		// priority:1>3>2>4
		priorityArray = []int{1, npuIndex3, npuIndex2, npuNumPerHccs}
	case npuIndex2:
		// priority：2>npuNumPerHccs>3
		priorityArray = []int{npuIndex2, npuNumPerHccs, npuIndex3}
	case npuNumPerHccs:
		// priority：4
		priorityArray = []int{npuNumPerHccs}
	case nodeNPUNumber:
		priorityArray = []int{nodeNPUNumber}
	default:
		// For normal,can not be here. The pre function validate job has done this.
		err := fmt.Errorf("illegal request npu number: %d", taskNPUNumber)
		if err != nil {
			klog.V(util.LogErrorLev).Infof("%s %s.", SchedulerName, err.Error())
			return priorityArray, err
		}
	}

	return priorityArray, nil
}
