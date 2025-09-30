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
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
)

type chip310px2 struct {
	base.NPUHandler
	affScoreMapOddSingle map[int]map[int]int
	affScoreMapOdd       map[int]map[int]int
	affScoreMapEven      map[int]map[int]int
}

const (
	affScore0 = iota
	affScore1
	affScore2
	affScore3
	affScore4
	affScore5
	affScore6
	affScore7
	affScore8
	affScore9
	affScore10
	affScore11
	affScore12
	affScore13
	affScore14
	affScore15
	affScore16

	// SchedulerName chip310p plugin name
	SchedulerName  = "huawei.com/Ascend310Pduochip"
	maxNodeNPUNum  = 16
	maxCardNPUNum  = 2
	constNPUWeight = 8.0

	// InferCardKey the node label key of infer card
	InferCardKey = "infer-card-type"
	// A300IDuoLabel the value of the A300I Duo node label
	A300IDuoLabel = "card-300i-duo"
)
