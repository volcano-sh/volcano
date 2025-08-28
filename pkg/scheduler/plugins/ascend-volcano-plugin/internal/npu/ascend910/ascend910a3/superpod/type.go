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
Package superpod is using for HuaWei Atlas 900 A3 SuperPod affinity schedule.
*/
package superpod

import (
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/ascend910/ascend910a3"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

type module910SuperPod struct {
	ascend910a3.Base910A3
	nodeVPodId map[string]string
	spBlock    int
}

const (
	// SchedulerName name of scheduler
	SchedulerName = "huawei.com/Ascend910super-pod"
	// SuperPodAnnoKey annotation key of super pod
	SuperPodAnnoKey            = "sp-block"
	jobCheckFailedReason       = "npu num is invalid"
	spBlockInvalidReason       = "sp-block is invalid"
	getNPUFromPodFailedPattern = "%s getUsableTopFromNode err: %s"
	scoreForNode               = 100000000
	taskSpec                   = "volcano.sh/task-spec"
	schedulerSpec              = "scheduler"
	delayingTime               = 10
)

const (
	superPodAffinity = "super-pod-affinity"
	hardRequire      = "hard"
	softRequire      = "soft"
	superPodRankKey  = "super-pod-rank"
)

type superPodInfo struct {
	firstLevel     remainderTop
	countVSuperPod int
}

type remainderTop = [][][]superPod

type superPod = map[string]plugin.NPUNode

type vPodIdRecorder struct {
	unReadyId  []string
	leftIndex  int
	rightIndex int
}

type ResourceFitPolicy string

const (
	// idlestResourceFitPolicy
	idlestResourceFitPolicy ResourceFitPolicy = "idlest"
	// busiestResourceFitPolicy soft policy
	busiestResourceFitPolicy ResourceFitPolicy = "busiest"
)
