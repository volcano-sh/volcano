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
Package base is using for HuaWei Ascend pin affinity schedule.
*/
package base

import (
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

// AscendHandler ascend npu event handler
type AscendHandler interface {
	plugin.SchedulerPlugin
	SetSchedulerAttr(util.SchedulerJobAttr)
	SetSchedulerEnv(plugin.ScheduleEnv)
	SetMaxNodeNPUNum(int)
	SetMaxCardNPUNum(int)
	SetNpuNumInvalidMap(map[int]struct{})
	SetIsNetworkFaultAttention(bool)
}

// NPUHandler base npu handler
type NPUHandler struct {
	plugin.SchedulerBaseAttr
	util.SchedulerJobAttr
	plugin.ScheduleEnv
	IsNetworkFaultAttention bool
	NpuNumInvalidMap        map[int]struct{}
	MaxNodeNPUNum           int
	MaxCardNPUNum           int
}

const (
	nodeWeight          = 10.0
	networkUnhealthyNPU = "huawei.com/Ascend910-NetworkUnhealthy"
)

const (
	// nPUChipMemoryKey key of npu chip memory in node labels
	nPUChipMemoryKey = "mind-cluster/npu-chip-memory"
	// podUsedHardwareTypeKey key of pod used hardware type in pod annotation
	podUsedHardwareTypeKey = "mind-cluster/hardware-type"
	// hardwareType800IA2 hardware type 800I A2
	hardwareType800IA2 = "800I-A2"
	// hardwareType800IA3 hardware type 800I A3
	hardwareType800IA3 = "800I-A3"
	// serverUsageKey key of server usage in node labels
	serverUsageKey = "server-usage"
	// inferUsage infer value of server usage in node labels
	inferUsage = "infer"
)

const (
	jobGroupIDLabelKey = "jobID"
)
