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
Package ascend910b is using for HuaWei Ascend pin affinity schedule.
*/
package ascend910b

import (
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
)

// SelectNodeInf for node hccs.
type SelectNodeInf struct {
	AllNPUNum   int
	LeftNPUNum  int
	RightNPUNum int
	crossNPUNum int
}

// Base910b for Ascend 910B base.
type Base910b struct {
	base.NPUHandler
	AffScoreList [][]int
}
