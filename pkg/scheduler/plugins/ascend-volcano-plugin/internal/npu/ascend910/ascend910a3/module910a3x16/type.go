/*
Copyright(C)2025. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package module910a3x16 is using for A3 x16 affinity schedule.
*/
package module910a3x16

import (
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/internal/npu/ascend910/ascend910a3"
)

type module910a3x16 struct {
	ascend910a3.Base910A3
}

const (
	// SchedulerName name of scheduler for a3x16
	SchedulerName = "huawei.com/Ascend910module-a3-16"
)
