/*
Copyright(C)2020-2022. Huawei Technologies Co.,Ltd. All rights reserved.
*/

/*
Package rescheduling is using for HuaWei Ascend pin fault rescheduling.
*/
package plugin

import (
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

type IsPodWholeCardArgs struct {
	realCardName string
}

type IsPodWholeCardTest struct {
	name string
	args IsPodWholeCardArgs
	want bool
}

func buildIsPodWholeCardTest() []IsPodWholeCardTest {
	tests := []IsPodWholeCardTest{
		{
			name: "01-isPodWholeCardFromAscendCore-is whole card",
			args: IsPodWholeCardArgs{
				realCardName: "0,1",
			},
			want: true,
		},
		{
			name: "02-isPodWholeCardFromAscendCore-not whold card",
			args: IsPodWholeCardArgs{realCardName: "0-vir04"},
			want: false,
		},
	}
	return tests
}

func TestIsPodWholeCard(t *testing.T) {
	tests := buildIsPodWholeCardTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPodWholeCardFromAscendCore(tt.args.realCardName); got != tt.want {
				t.Errorf("IsPodWholeCard() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetResourceFromTemplate(t *testing.T) {
	tests := []struct {
		name           string
		nodeType       string
		templateString string
		taskTemplate   map[string]map[string]util.VResource
		want           *util.VResource
	}{
		{
			name:           "01-getResourceFromTemplate return nil when not exist ascend310p vnpu template",
			nodeType:       Ascend310P,
			templateString: "",
			taskTemplate:   map[string]map[string]util.VResource{},
			want:           nil,
		},
		{
			name:           "02-getResourceFromTemplate return nil when not exist ascend310p vnpu template",
			nodeType:       Ascend310P,
			templateString: Ascend310P,
			taskTemplate:   map[string]map[string]util.VResource{Ascend310P: {}},
			want:           nil,
		},
		{
			name:           "03-getResourceFromTemplate return nil when not exist ascend310p vnpu template",
			nodeType:       Ascend310P,
			templateString: AscendVNPUDVPP,
			taskTemplate: map[string]map[string]util.VResource{
				Ascend310P: {AscendVNPUDVPP: {Aicore: 1, Aicpu: 1, DVPP: "0"}}},
			want: &util.VResource{Aicore: 1, Aicpu: 1, DVPP: "0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getResourceFromTemplate(tt.nodeType,
				tt.templateString, tt.taskTemplate); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getResourceFromTemplate() = %v, want %v", got, tt.want)
			}
		})
	}
}

const (
	fakeCardName01       = Ascend310P + "-0"
	fakeCardName02       = Ascend310P + "-0-vir04"
	fakeCardName03       = Ascend310P + "-1c-400-3_0"
	fakePhysicCardName03 = Ascend310P + "-3"
	fakeCardName04       = Ascend310P + "-1c-400-3"
	fakeCardName05       = Ascend310P + "-s"
)

func TestIsPodWholeCardFromAscendReal(t *testing.T) {
	tests := []struct {
		name         string
		realCardName string
		want         bool
	}{
		{
			name:         "01-isPodWholeCardFromAscendReal return false when card is empty",
			realCardName: "",
			want:         false,
		},
		{
			name:         "02-isPodWholeCardFromAscendReal return false when card is not whole card",
			realCardName: Ascend310P,
			want:         false,
		},
		{
			name:         "03-isPodWholeCardFromAscendReal return true when card is whole card",
			realCardName: fakeCardName01,
			want:         true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPodWholeCardFromAscendReal(tt.realCardName); got != tt.want {
				t.Errorf("isPodWholeCardFromAscendReal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPhysicCardNameFromVChip(t *testing.T) {
	tests := []struct {
		name         string
		realCardName string
		want         string
	}{
		{
			name:         "01-GetPhysicCardNameFromVChip return card name when card is whole card",
			realCardName: fakeCardName01,
			want:         fakeCardName01,
		},
		{
			name:         "02-GetPhysicCardNameFromVChip return empty when card is wrong",
			realCardName: fakeCardName02,
			want:         "",
		},
		{
			name:         "03-GetPhysicCardNameFromVChip return card name when card is whole card",
			realCardName: fakeCardName03,
			want:         fakePhysicCardName03,
		},
		{
			name:         "04-GetPhysicCardNameFromVChip return empty when card is wrong",
			realCardName: fakeCardName04,
			want:         "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetPhysicCardNameFromVChip(tt.realCardName); got != tt.want {
				t.Errorf("GetPhysicCardNameFromVChip() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetVNPUCardIDFromAscendCore(t *testing.T) {
	tests := []struct {
		name        string
		coreNameStr string
		want        int
		wantErr     bool
	}{
		{
			name:        "01-getVNPUCardIDFromAscendCore return error when card is empty",
			coreNameStr: fakeCardName01,
			want:        0,
			wantErr:     true,
		},
		{
			name:        "02-getVNPUCardIDFromAscendCore return error when card is wrong",
			coreNameStr: Ascend310P,
			want:        0,
			wantErr:     true,
		},
		{
			name:        "03-getVNPUCardIDFromAscendCore return card id when card is whole card",
			coreNameStr: "1-vir",
			want:        1,
			wantErr:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getVNPUCardIDFromAscendCore(tt.coreNameStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("getVNPUCardIDFromAscendCore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getVNPUCardIDFromAscendCore() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetWholeCardIDFromAscendReal(t *testing.T) {
	tests := []struct {
		name        string
		cardNameStr string
		want        int
		wantErr     bool
	}{
		{
			name:        "01-getWholeCardIDFromAscendReal return error when card is wrong",
			cardNameStr: Ascend310P,
			want:        util.ErrorInt,
			wantErr:     true,
		},
		{
			name:        "02-getWholeCardIDFromAscendReal return card id when card is wrong",
			cardNameStr: fakeCardName05,
			want:        util.ErrorInt,
			wantErr:     true,
		},
		{
			name:        "03-getWholeCardIDFromAscendReal return card id when card is whole card",
			cardNameStr: fakeCardName01,
			want:        0,
			wantErr:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getWholeCardIDFromAscendReal(tt.cardNameStr)
			if (err != nil) != tt.wantErr {
				t.Errorf("getWholeCardIDFromAscendReal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getWholeCardIDFromAscendReal() got = %v, want %v", got, tt.want)
			}
		})
	}
}

type cardPhysicsIDFromAscendCoreParam struct {
	name        string
	pod         *v1.Pod
	isWholeCard bool
	want        []int
	wantErr     bool
}

func buildCardPhysicsIDFromAscendCoreParam() []cardPhysicsIDFromAscendCoreParam {
	return []cardPhysicsIDFromAscendCoreParam{
		{
			name: "01-getCardPhysicsIDFromAscendCore return error when pod is nil",
			pod:  nil, isWholeCard: false, want: []int{}, wantErr: true,
		},
		{
			name: "02-getCardPhysicsIDFromAscendCore return error when pod not exist npu-core annotation",
			pod:  &v1.Pod{}, isWholeCard: true, want: []int{}, wantErr: true,
		},
		{
			name: "03-getCardPhysicsIDFromAscendCore return card id when vnpu shcedule",
			pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{util.AscendNPUCore: "0-vir"}}},
			isWholeCard: false, want: []int{0}, wantErr: false,
		},
		{
			name: "04-getCardPhysicsIDFromAscendCore return error when card name is wrong",
			pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{util.AscendNPUCore: Ascend910}}},
			isWholeCard: false, want: []int{}, wantErr: true,
		},
		{
			name: "05-getCardPhysicsIDFromAscendCore return card id when card is whole card",
			pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{util.AscendNPUCore: "0,1"}}},
			isWholeCard: true, want: []int{0, 1}, wantErr: false,
		},
		{
			name: "06-getCardPhysicsIDFromAscendCore return card id when card is wrong",
			pod: &v1.Pod{ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{util.AscendNPUCore: "x,1"}}},
			isWholeCard: true, want: []int{}, wantErr: true,
		},
	}
}

func TestGetCardPhysicsIDFromAscendCore(t *testing.T) {
	tests := buildCardPhysicsIDFromAscendCoreParam()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getCardPhysicsIDFromAscendCore(tt.pod, tt.isWholeCard)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCardPhysicsIDFromAscendCore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getCardPhysicsIDFromAscendCore() got = %v, want %v", got, tt.want)
			}
		})
	}
}
