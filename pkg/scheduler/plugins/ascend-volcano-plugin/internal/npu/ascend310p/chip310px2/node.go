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
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

func (tp *chip310px2) getNPUIndex(cardNumGroups [][]int) map[int][]int {
	npuNumberIndex := make(map[int][]int, util.MapInitNum)
	for _, cardNumGroup := range cardNumGroups {
		index := len(cardNumGroup)
		npuNumberIndex[index] = append(npuNumberIndex[index], cardNumGroup...)
	}
	return npuNumberIndex
}
