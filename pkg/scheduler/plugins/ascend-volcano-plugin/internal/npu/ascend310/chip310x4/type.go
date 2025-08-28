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
Package chip310x4 is using for HuaWei 310 Ascend pin affinity schedule.
*/
package chip310x4

import (
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/base"
)

type chip310x4 struct {
	base.NPUHandler
}

const (
	// SchedulerName chip310 plugin name
	SchedulerName = "huawei.com/Ascend310chip"
	maxNodeNPUNum = 64
	maxCardNPUNum = 4
)
