/*
Copyright(C)2020-2025. Huawei Technologies Co.,Ltd. All rights reserved.

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
Package vnpu is using for HuaWei Ascend pin vnpu allocation.
*/
package vnpu

import (
	"errors"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

type commonArgs struct {
	name      string
	namespace string
	event     v1.Event
	ssn       *framework.Session
}

type commonTestCase struct {
	name    string
	args    commonArgs
	wantRes bool
}

func buildGetSegmentFailureTaskIDsTestCases() []commonTestCase {
	return []commonTestCase{
		{
			name: "01 will return nil when ssn is nil",
			args: commonArgs{
				name:      "test",
				namespace: "test",
				event:     v1.Event{},
				ssn:       nil,
			},
			wantRes: false,
		},
		{
			name: "02 will return nil when getNamespaceEvents failed",
			args: commonArgs{
				name:      "test",
				namespace: "test",
				event:     v1.Event{},
				ssn:       test.FakeNormalSSN(nil),
			},
			wantRes: false,
		},
	}
}

func TestGetSegmentFailureTaskIDs(t *testing.T) {
	tests := buildGetSegmentFailureTaskIDsTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patch := gomonkey.ApplyFunc(getNamespaceEvents,
				func(ssn *framework.Session, namespace string) (*v1.EventList, error) {
					return nil, errors.New("getNamespaceEvents get error")
				})

			defer patch.Reset()
			res := GetSegmentFailureTaskIDs(tt.args.ssn, tt.args.namespace)
			if (res != nil) != tt.wantRes {
				t.Errorf("GetSegmentFailureTaskIDs() res = %v, wantErr %v", res, tt.wantRes)
			}
		})
	}
}

func buildIsEventSegmentFailurePodTestCases() []commonTestCase {
	return []commonTestCase{
		{
			name:    "01 will return false when event.InvolvedObject.Kind is nil",
			args:    commonArgs{event: v1.Event{}},
			wantRes: false,
		},
		{
			name: "02 will return false when event.Type is nil",
			args: commonArgs{event: v1.Event{
				InvolvedObject: v1.ObjectReference{
					Kind: podObjectType,
				},
			}},
			wantRes: false,
		},
		{
			name: "03 will return true when event is segment failure pod event",
			args: commonArgs{event: v1.Event{
				InvolvedObject: v1.ObjectReference{
					Kind: podObjectType,
				},
				Type:    PodEventTypeAllocateFailed,
				Reason:  PodEventReasonAllocateFailed,
				Message: PodEventMsgNoResourceFailed,
			}},
			wantRes: true,
		},
	}
}

func TestIsEventSegmentFailurePod(t *testing.T) {
	tests := buildIsEventSegmentFailurePodTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := isEventSegmentFailurePod(tt.args.event)
			if res != tt.wantRes {
				t.Errorf("GetSegmentFailureTaskIDs() res = %v, wantErr %v", res, tt.wantRes)
			}
		})
	}
}

func TestGetVNPUTaskDVPP(t *testing.T) {
	tests := []struct {
		name   string
		asTask util.NPUTask
		want   string
	}{
		{
			name:   "01 will return null when task label is nil",
			asTask: util.NPUTask{},
			want:   plugin.AscendDVPPEnabledNull,
		},
		{
			name:   "02 will return value when task label is nil",
			asTask: util.NPUTask{Label: map[string]string{plugin.AscendVNPUDVPP: plugin.AscendDVPPEnabledOff}},
			want:   plugin.AscendDVPPEnabledOff,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetVNPUTaskDVPP(tt.asTask); got != tt.want {
				t.Errorf("GetVNPUTaskDVPP() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetVNPUTaskCpuLevel(t *testing.T) {
	tests := []struct {
		name   string
		asTask util.NPUTask
		want   string
	}{
		{
			name:   "01 will return null when task label is nil",
			asTask: util.NPUTask{},
			want:   plugin.AscendVNPULevelLow,
		},
		{
			name:   "02 will return value when task label is nil",
			asTask: util.NPUTask{Label: map[string]string{plugin.AscendVNPULevel: plugin.AscendVNPULevelLow}},
			want:   plugin.AscendVNPULevelLow,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetVNPUTaskCpuLevel(tt.asTask); got != tt.want {
				t.Errorf("GetVNPUTaskCpuLevel() = %v, want %v", got, tt.want)
			}
		})
	}
}
