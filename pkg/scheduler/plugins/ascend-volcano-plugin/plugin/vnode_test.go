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
Package plugin is using for HuaWei Ascend pin affinity schedule.
*/
package plugin

import (
	"testing"

	"k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

const (
	ascend310PCard = "Ascend310P-8"
	num4           = 4
)

func mockNode() NPUNode {
	return NPUNode{
		CommonNode: mockCommonNode(),
		VNode:      mockVNode(),
	}
}

func mockCommonNode() CommonNode {
	return CommonNode{
		Name:           "node1",
		Capability:     map[v1.ResourceName]float64{util.NPU310PCardName: num4 * util.NPUHexKilo},
		Allocate:       map[v1.ResourceName]float64{util.NPU310PCardName: num4 * util.NPUHexKilo},
		Idle:           map[v1.ResourceName]float64{util.NPU310PCardName: num4 * util.NPUHexKilo},
		BaseDeviceInfo: "",
		Annotation:     map[string]string{util.NPU310PCardName: "Ascend310P-0,Ascend310P-1"},
		Label:          map[string]string{util.Accelerator: "huawei-Ascend310P"},
		Address:        "",
		SuperPodID:     0,
	}
}

func mockVNode() VNode {
	return VNode{
		Chips: map[int]*VChip{
			util.NPUIndex1: {TotalRes: util.VResource{Aicore: util.NPUIndex1},
				FreeRes: util.VResource{Aicore: util.NPUIndex1, Aicpu: util.NPUIndex1},
				Name:    ascend310PCard, Kind: Ascend310P, ID: []string{"Ascend310P-2c.1cpu-105-0_3"}},
			0: {TotalRes: util.VResource{Aicore: util.NPUIndex8},
				FreeRes: util.VResource{Aicore: util.NPUIndex8, Aicpu: util.NPUIndex8},
				Name:    ascend310PCard, Kind: Ascend310P, ID: []string{"Ascend310P-2c.1cpu-105-0_3"}},
		},
		ChipKind:         Ascend310P,
		UnhealthyChipIds: map[int]struct{}{},
		ServerType:       ascend310PCard,
		TotalChipNum:     util.NPUIndex8,
		AiCorePerChip:    util.NPUIndex8,
		FreeChipNum:      util.NPUIndex8,
		TotalRes:         util.VResource{Aicore: util.CoreNum25},
		ValidVNode:       true,
		ChipType:         ChipTypeB1,
	}
}

type testNodeMeetRes struct {
	name    string
	node    NPUNode
	taskRes util.VResource
	want    bool
}

func buildTestNodeMeetResCases() []testNodeMeetRes {
	node := mockNode()
	node.Chips[0].SegmentFlag = true
	return []testNodeMeetRes{
		{
			name:    "01 will return true when node is empty",
			node:    NPUNode{},
			taskRes: util.VResource{},
			want:    true,
		},
		{
			name:    "02 will return false when node resource is enough and task is vnpu",
			node:    node,
			taskRes: util.VResource{Aicore: util.NPUIndex4, Aicpu: util.NPUIndex3},
			want:    false,
		},
		{
			name:    "03 will return false when node resource is enough and task is whole card",
			node:    mockNode(),
			taskRes: util.VResource{Aicore: util.NPUIndex8, Aicpu: util.NPUIndex7},
			want:    false,
		},
	}
}

func TestNPUNodeIsNodeNotMeetRes(t *testing.T) {
	for _, tt := range buildTestNodeMeetResCases() {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.node.IsNodeNotMeetRes(tt.taskRes); got != tt.want {
				t.Errorf("IsNodeNotMeetRes() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVNodeSelectChipFromNode(t *testing.T) {
	for _, tt := range buildTestNodeMeetResCases() {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.node.VNode.SelectChipFromNode(tt.taskRes)
			if (err != nil) != tt.want {
				t.Errorf("SelectChipFromNode() error = %v, wantErr %v", err, tt.want)
				return
			}
		})
	}
	t.Run("test vNode is nil will return error", func(t *testing.T) {
		var node *VNode
		_, err := node.SelectChipFromNode(util.VResource{})
		if (err != nil) != true {
			t.Errorf("SelectChipFromNode() error = %v, wantErr %v", err, true)
			return
		}
	})
}

func TestVNodeIsResourceWholeCard(t *testing.T) {
	t.Run("test vnode is nil", func(t *testing.T) {
		var node *VNode
		if got := node.IsResourceWholeCard(0); got != false {
			t.Errorf("IsResourceWholeCard() = %v, want %v", got, false)
		}
	})
}

type getNodeTopForWholeCardTest struct {
	name  string
	vNode *VNode
	want  []int
}

func buildGetNodeTopForWholeCardTestCases() []getNodeTopForWholeCardTest {
	fakeNode := mockVNode()
	return []getNodeTopForWholeCardTest{
		{
			name:  "01 will return nil when vnode is empty",
			vNode: &VNode{},
			want:  nil,
		},
		{
			name:  "02 will return card id  when vnode is normal node",
			vNode: &fakeNode,
			want:  []int{util.NPUIndex1, util.NPUIndex0},
		},
	}
}

func TestGetNodeTopForWholeCard(t *testing.T) {
	for _, tt := range buildGetNodeTopForWholeCardTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.vNode.GetNodeTopForWholeCard(); !checkCardIdEqual(got, tt.want) {
				t.Errorf("GetNodeTopForWholeCard() = %v, want %v", got, tt.want)
			}
		})
	}
}

func checkCardIdEqual(keyCardIDs, targetCardIds []int) bool {
	keyIds := make(map[int]struct{}, len(keyCardIDs))
	for _, id := range keyCardIDs {
		keyIds[id] = struct{}{}
	}
	for _, id := range targetCardIds {
		if _, ok := keyIds[id]; !ok {
			return false
		}
	}
	return true
}
