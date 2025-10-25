package vnpu310p

import (
	"sort"

	"volcano.sh/volcano/pkg/scheduler/api/devices/ascend/ascend310p/vnpu"
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
	iIdleAiCore, ok := vNodes[i].Idle[vnpu.AscendNPUCore]
	if !ok {
		return false
	}
	jIdleAiCore, ok := vNodes[j].Idle[vnpu.AscendNPUCore]
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
