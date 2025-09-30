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
Package rescheduling is using for HuaWei Ascend pin fault rescheduling.
*/
package rescheduling

import (
	"reflect"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

type FaultNodeGetAllNPUCardsFromDeviceInfoArgs struct {
	node     *plugin.NPUNode
	cardName string
}

type FaultNodeGetAllNPUCardsFromDeviceInfoTests struct {
	fields  *FaultNode
	name    string
	args    FaultNodeGetAllNPUCardsFromDeviceInfoArgs
	want    []string
	wantErr bool
}

func buildFaultNodeGetAllNPUCardsFromDeviceInfoTests() []FaultNodeGetAllNPUCardsFromDeviceInfoTests {
	node2 := fakeNPUNodeWithDeviceInfo("node0")
	test1 := FaultNodeGetAllNPUCardsFromDeviceInfoTests{
		name:   "01-FaultNodeGetAllNPUCardsFromDeviceInfoTests() nil device info",
		fields: fakeTestFaultNodeNodeHealthy("node0"),
		args: FaultNodeGetAllNPUCardsFromDeviceInfoArgs{
			node: fakeNPUNodeNilDeviceInfo("node0"),
		},
		want:    []string{},
		wantErr: true,
	}
	test2 := FaultNodeGetAllNPUCardsFromDeviceInfoTests{
		name:   "02-FaultNodeGetAllNPUCardsFromDeviceInfoTests() succeed",
		fields: fakeTestFaultNodeNodeHealthy("node0"),
		args: FaultNodeGetAllNPUCardsFromDeviceInfoArgs{
			node: node2,
		},
		want:    []string{"Ascend910-0", "Ascend910-1", "Ascend910-2"},
		wantErr: false,
	}
	tests := []FaultNodeGetAllNPUCardsFromDeviceInfoTests{
		test1,
		test2,
	}
	return tests
}

// TestFaultNodeGetAllNPUCardsFromDeviceInfo test for get npu card
func TestFaultNodeGetAllNPUCardsFromDeviceInfo(t *testing.T) {
	tests := buildFaultNodeGetAllNPUCardsFromDeviceInfoTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fNode := tt.fields
			got, err := fNode.getAllNPUCardsFromDeviceInfo(tt.args.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("getAllNPUCardsFromDeviceInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getAllNPUCardsFromDeviceInfo() got = %v, want %v", got, tt.want)
			}
		})
	}
}
