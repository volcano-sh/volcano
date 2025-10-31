/*
Copyright 2025 The Volcano Authors.

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

package vnpu310p

import (
	"sort"

	"volcano.sh/volcano/pkg/scheduler/api/devices/ascend/ascend310p/vnpu"
	"volcano.sh/volcano/third_party/ascend-for-volcano/common/util"
)

func orderVNodesByFreeResource(nodes []*vnpu.NodeInf) []*vnpu.NodeInf {
	tempVNodes := vNodesList(nodes)
	sort.Sort(tempVNodes)
	return tempVNodes
}

type vNodesList []*vnpu.NodeInf

// Len for order.
func (vNodes vNodesList) Len() int {
	return len(vNodes)
}

// Less for order.
func (vNodes vNodesList) Less(i, j int) bool {
	if i > vNodes.Len() || j > vNodes.Len() {
		return false
	}
	iIdleAiCore, ok := vNodes[i].Idle[util.AscendNPUCore]
	if !ok {
		return false
	}
	jIdleAiCore, ok := vNodes[j].Idle[util.AscendNPUCore]
	if !ok {
		return true
	}
	return iIdleAiCore < jIdleAiCore
}

// Swap for order.
func (vNodes vNodesList) Swap(i, j int) {
	if i > vNodes.Len() || j > vNodes.Len() {
		return
	}
	vNodes[i], vNodes[j] = vNodes[j], vNodes[i]
}
