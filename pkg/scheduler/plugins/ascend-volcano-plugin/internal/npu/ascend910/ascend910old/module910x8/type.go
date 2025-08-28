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
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
)

type module910x8 struct {
	base.NPUHandler
	netUnhealthyKey string
	affScoreList    [][]int
}

const (
	// SchedulerName module910x8 plugin name
	SchedulerName = "huawei.com/Ascend910module"
	npuIndex2     = 2
	npuIndex3     = 3
	npuNumPerHccs = 4
	nodeNPUNumber = 8
	nodeWeight    = 8.0

	networkUnhealthyNPU = "huawei.com/Ascend910-NetworkUnhealthy"
)

type selectNodeInf struct {
	nodeName    string
	allNPUNum   int
	leftNPUNum  int
	rightNPUNum int
}
