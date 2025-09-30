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
Package card310px2 is using for HuaWei 300I Duo Ascend pin affinity schedule.
*/
package card310px2

import (
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
)

type card310px2 struct {
	base.NPUHandler
	affScoreList [][]int
}

const (
	affScore0 = iota
	affScore1
	affScore2
	affScore3
	affScore4

	// SchedulerName card310px2 plugin name
	SchedulerName  = "huawei.com/Ascend310Pduocard"
	maxNodeNPUNum  = 32
	maxCardNPUNum  = 2
	constNPUWeight = 8.0
)
