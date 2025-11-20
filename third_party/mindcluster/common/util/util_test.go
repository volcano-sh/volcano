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
Package util is using for the total variable.
*/
package util

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	two   = 2
	three = 3
	four  = 4
	five  = 5
)

func TestChangeTopToIntArray(t *testing.T) {
	type args struct {
		topStr         string
		npuCardPreName string
	}
	tests := []struct {
		name string
		args args
		want []int
	}{
		{
			name: "01-ChangeTopToIntArray get int array",
			args: args{
				topStr:         fmt.Sprintf("%s0,%s1", NPU310PCardNamePre, NPU310PCardNamePre),
				npuCardPreName: NPU310PCardNamePre,
			},
			want: []int{0, 1},
		},
		{
			name: "02-ChangeTopToIntArray topStr is empty",
			args: args{
				topStr:         "",
				npuCardPreName: NPU310PCardNamePre,
			},
			want: []int{},
		},
		{
			name: "03-ChangeTopToIntArray string to int error",
			args: args{
				topStr:         fmt.Sprintf("%s0ab", NPU310PCardNamePre),
				npuCardPreName: NPU310PCardNamePre,
			},
			want: nil,
		},
		{
			name: "04-ChangeTopToIntArray get int array",
			args: args{
				topStr: fmt.Sprintf("%s0,%s1,%s2,%s3,%s4,%s5", NPU310PCardNamePre, NPU310PCardNamePre,
					NPU310PCardNamePre, NPU310PCardNamePre, NPU310PCardNamePre, NPU310PCardNamePre),
				npuCardPreName: NPU310PCardNamePre,
			},
			want: []int{0, 1, two, three, four, five},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ChangeTopToIntArray(tt.args.topStr, tt.args.npuCardPreName); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ChangeTopToIntArray() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsMapHasNPUResource(t *testing.T) {
	type args struct {
		resMap  map[v1.ResourceName]float64
		npuName string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "01-ChangeTopToIntArray has npu resource",
			args: args{
				resMap:  map[v1.ResourceName]float64{NPU910CardName: 1},
				npuName: NPU910CardName,
			},
			want: true,
		},
		{
			name: "02-ChangeTopToIntArray not exist npu resource",
			args: args{
				resMap:  map[v1.ResourceName]float64{},
				npuName: NPU910CardName,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsMapHasNPUResource(tt.args.resMap, tt.args.npuName); got != tt.want {
				t.Errorf("IsMapHasNPUResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChangeIntArrToStr(t *testing.T) {
	type args struct {
		top            []int
		npuCardPreName string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "01-ChangeIntArrToStr get int array",
			args: args{
				top:            []int{0, 1},
				npuCardPreName: NPU310CardNamePre,
			},
			want: fmt.Sprintf("%s0,%s1", NPU310CardNamePre, NPU310CardNamePre),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ChangeIntArrToStr(tt.args.top, tt.args.npuCardPreName); got != tt.want {
				t.Errorf("ChangeIntArrToStr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMax(t *testing.T) {
	type args struct {
		x int
		y int
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			name: "01-Max return y when x < y",
			args: args{
				x: 0,
				y: 1,
			},
			want: 1,
		},
		{
			name: "02-Max return x when x > y",
			args: args{
				x: 1,
				y: 0,
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Max(tt.args.x, tt.args.y); got != tt.want {
				t.Errorf("Max() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMin(t *testing.T) {
	type args struct {
		x int
		y int
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			name: "01-Min return x when x < y",
			args: args{
				x: 0,
				y: 1,
			},
			want: 0,
		},
		{
			name: "02-Min return y when x > y",
			args: args{
				x: 1,
				y: 0,
			},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Min(tt.args.x, tt.args.y); got != tt.want {
				t.Errorf("Min() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsSliceContain(t *testing.T) {
	type args struct {
		keyword     interface{}
		targetSlice interface{}
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "01-IsSliceContain targetSlice is nil",
			args: args{
				keyword:     1,
				targetSlice: nil,
			},
			want: false,
		},
		{
			name: "02-IsSliceContain targetSlice is not slice or array",
			args: args{
				keyword:     1,
				targetSlice: 1,
			},
			want: false,
		},
		{
			name: "03-IsSliceContain slice contains keyword",
			args: args{
				keyword:     1,
				targetSlice: []int{0, 1},
			},
			want: true,
		},
		{
			name: "04-IsSliceContain slice not contains keyword",
			args: args{
				keyword:     1,
				targetSlice: []int{0},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSliceContain(tt.args.keyword, tt.args.targetSlice); got != tt.want {
				t.Errorf("IsSliceContain() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoveSliceDuplicateElement(t *testing.T) {
	type args struct {
		languages []string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			name: "01-RemoveSliceDuplicateElement remove duplicate",
			args: args{
				languages: []string{"Go", "Python", "Python", "Go", "C++", "Java"},
			},
			want: []string{"Go", "Python", "C++", "Java"},
		},
		{
			name: "02-RemoveSliceDuplicateElement empty slice",
			args: args{
				languages: []string{},
			},
			want: []string{},
		},
		{
			name: "03-RemoveSliceDuplicateElement slice is nil",
			args: args{
				languages: nil,
			},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RemoveSliceDuplicateElement(tt.args.languages); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RemoveSliceDuplicateElement() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConvertErrSliceToError(t *testing.T) {
	type args struct {
		reErrors []error
	}
	tests := []struct {
		name string
		args args
		want error
	}{
		{
			name: "01-ConvertErrSliceToError",
			args: args{reErrors: []error{errors.New("test0"), errors.New("test1")}},
			want: errors.New("test0 test1"),
		},
		{
			name: "02-ConvertErrSliceToError",
			args: args{reErrors: []error{}},
			want: nil,
		},
		{
			name: "03-ConvertErrSliceToError",
			args: args{reErrors: nil},
			want: nil,
		},
		{
			name: "04-ConvertErrSliceToError",
			args: args{reErrors: []error{nil, nil}},
			want: nil,
		},
		{
			name: "05-ConvertErrSliceToError",
			args: args{reErrors: []error{nil, errors.New("test0")}},
			want: errors.New("test0"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ConvertErrSliceToError(tt.args.reErrors)
			if tt.want != nil && err == nil {
				t.Errorf("ConvertErrSliceToError() error = %v, wantErr %v", err, tt.want)
			}
			if tt.want == nil && err != nil {
				t.Errorf("ConvertErrSliceToError() error = %v, wantErr %v", err, tt.want)
			}
			if tt.want != nil && err != nil && tt.want.Error() != err.Error() {
				t.Errorf("ConvertErrSliceToError() error = %v, wantErr %v", err, tt.want)
			}
		})
	}
}

func TestChangeNodesToNodeMaps(t *testing.T) {
	type args struct {
		nodes []*api.NodeInfo
	}
	tests := []struct {
		name string
		args args
		want map[string]*api.NodeInfo
	}{
		{
			name: "01-ChangeNodesToNodeMaps",
			args: args{nodes: []*api.NodeInfo{{Name: "node0"}, {Name: "node1"}}},
			want: map[string]*api.NodeInfo{"node0": {Name: "node0"}, "node1": {Name: "node1"}},
		},
		{
			name: "02-ChangeNodesToNodeMaps empty slice",
			args: args{nodes: []*api.NodeInfo{}},
			want: nil,
		},
		{
			name: "03-ChangeNodesToNodeMaps nil slice",
			args: args{nodes: nil},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ChangeNodesToNodeMaps(tt.args.nodes); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ChangeNodesToNodeMaps() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetNpuNameFromJobRequire(t *testing.T) {
	type args struct {
		npuName string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "01-GetNpuNameFromJobRequire get npu name",
			args: args{npuName: AscendNPUCore},
			want: NPU310PCardName,
		},
		{
			name: "02-GetNpuNameFromJobRequire get npu name",
			args: args{npuName: NPU310CardName},
			want: NPU310CardName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetNpuNameFromJobRequire(tt.args.npuName); got != tt.want {
				t.Errorf("GetNpuNameFromJobRequire() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckStrInSlice(t *testing.T) {
	type args struct {
		str   string
		slice []string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "01-CheckStrInSlice in slice",
			args: args{str: "a", slice: []string{"a", "b", "c"}},
			want: true,
		},
		{
			name: "02-CheckStrInSlice not in slice",
			args: args{str: "d", slice: []string{"a", "b", "c"}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CheckStrInSlice(tt.args.str, tt.args.slice); got != tt.want {
				t.Errorf("CheckStrInSlice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsNodeReady(t *testing.T) {
	type args struct {
		node *v1.Node
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "01-IsNodeReady ready",
			args: args{node: &v1.Node{Status: v1.NodeStatus{Conditions: []v1.NodeCondition{
				{Type: v1.NodeReady, Status: v1.ConditionTrue}}}}},
			want: true,
		},
		{
			name: "02-IsNodeReady not ready",
			args: args{node: &v1.Node{Status: v1.NodeStatus{Conditions: []v1.NodeCondition{
				{Type: v1.NodeReady, Status: v1.ConditionFalse}}}}},
			want: false,
		},
		{
			name: "03-IsNodeReady not ready",
			args: args{node: &v1.Node{Status: v1.NodeStatus{Conditions: []v1.NodeCondition{
				{Type: v1.NodeReady, Status: v1.ConditionUnknown}}}}},
			want: false,
		},
		{
			name: "04-IsNodeReady not ready",
			args: args{node: &v1.Node{Status: v1.NodeStatus{Conditions: []v1.NodeCondition{
				{Type: v1.NodeMemoryPressure, Status: v1.ConditionUnknown}}}}},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNodeReady(tt.args.node); got != tt.want {
				t.Errorf("IsNodeReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoveCommonElement(t *testing.T) {
	type args struct {
		s1 []int
		s2 []int
	}
	tests := []struct {
		name string
		args args
		want []int
	}{
		{
			name: "01-RemoveCommonElement",
			args: args{s1: []int{0, 1}, s2: []int{0, 1}},
			want: []int{},
		},
		{
			name: "02-RemoveCommonElement",
			args: args{s1: []int{0, 1}, s2: []int{1}},
			want: []int{0},
		},
		{
			name: "03-RemoveCommonElement",
			args: args{s1: []int{0, 1}, s2: []int{three}},
			want: []int{0, 1},
		},
		{
			name: "04-RemoveCommonElement nil",
			args: args{s1: []int{0, 1}, s2: nil},
			want: []int{0, 1},
		},
		{
			name: "05-RemoveCommonElement nil",
			args: args{s1: nil, s2: []int{0, 1}},
			want: []int{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RemoveCommonElement(tt.args.s1, tt.args.s2); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("RemoveCommonElement() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestVResourceAdd(t *testing.T) {
	tests := []struct {
		name string
		res  VResource
		add  VResource
		want VResource
	}{
		{
			name: "01-Add return aicore=1 and aicpu=1",
			res:  VResource{Aicore: 0, Aicpu: 0},
			add:  VResource{Aicore: 1, Aicpu: 1},
			want: VResource{Aicore: 1, Aicpu: 1},
		},
		{
			name: "02-Add return aicore=2 and aicpu=2",
			res:  VResource{Aicore: 1, Aicpu: 1},
			add:  VResource{Aicore: 1, Aicpu: 1},
			want: VResource{Aicore: two, Aicpu: two},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.res.Add(tt.add)
			if !reflect.DeepEqual(tt.res, tt.want) {
				t.Errorf("VResource_Add() = %v, want %v", tt.res, tt.want)
			}
		})
	}
}

func TestVResourceSub(t *testing.T) {
	tests := []struct {
		name string
		res  VResource
		sub  VResource
		want VResource
	}{
		{
			name: "01-Sub return aicore=1 and aicpu=1",
			res:  VResource{Aicore: 1, Aicpu: 1},
			sub:  VResource{Aicore: 1, Aicpu: 1},
			want: VResource{Aicore: 0, Aicpu: 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.res.Sub(tt.sub)
			if !reflect.DeepEqual(tt.res, tt.want) {
				t.Errorf("VResource_Sub() = %v, want %v", tt.res, tt.want)
			}
		})
	}
}

func TestVResourceBeGreater(t *testing.T) {
	tests := []struct {
		name string
		res  VResource
		vr   VResource
		want bool
	}{
		{
			name: "01-BeGreater return true when res less than vr",
			res:  VResource{Aicore: 0, Aicpu: 0},
			vr:   VResource{Aicore: 1, Aicpu: 1},
			want: false,
		},
		{
			name: "02-BeGreater return false when res greater than vr",
			res:  VResource{Aicore: 1, Aicpu: 1},
			vr:   VResource{Aicore: 0, Aicpu: 0},
			want: true,
		},
		{
			name: "03-BeGreater return true when res equal to vr",
			res:  VResource{Aicore: 1, Aicpu: 1},
			vr:   VResource{Aicore: 1, Aicpu: 1},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.res.BeGreater(tt.vr); got != tt.want {
				t.Errorf("BeGreater() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMakeDataHash(t *testing.T) {
	expectHash := func(data string) string {
		sum := sha256.Sum256([]byte(data))
		return hex.EncodeToString(sum[:])
	}
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{
			name:  "01 nil input",
			input: nil,
			want:  expectHash("null"),
		},
		{
			name:  "02 empty struct",
			input: struct{}{},
			want:  expectHash("{}"),
		},
		{
			name:  "03 simple map",
			input: map[string]string{"key": "value"},
			want:  expectHash(`{"key":"value"}`),
		},
		{
			name:  "04 unserializable type",
			input: make(chan int),
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MakeDataHash(tt.input)
			if got != tt.want {
				t.Errorf("Expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestGetDeviceType(t *testing.T) {
	tests := []struct {
		name    string
		devList map[string]string
		want    string
	}{
		{
			name:    "01 device type is Ascend910",
			devList: map[string]string{HwPreName + Ascend910: "Ascend910-0"},
			want:    Ascend910,
		},
		{
			name:    "02 device type is Ascend310",
			devList: map[string]string{HwPreName + Ascend310: "Ascend310-0"},
			want:    Ascend310,
		},
		{
			name:    "03 device type is Ascend310P",
			devList: map[string]string{HwPreName + Ascend310P: "Ascend310P-0"},
			want:    Ascend310P,
		},
		{
			name:    "04 device type is invalid",
			devList: map[string]string{"invalid key": "Ascend910-0"},
			want:    Ascend910,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetDeviceType(tt.devList); got != tt.want {
				t.Errorf("GetDeviceType() = %v, want %v", got, tt.want)
			}
		})
	}
}

const (
	nodeName  = "nodeName"
	defaultNS = "default"
	taskName1 = "task1"
	taskName2 = "task2"
	taskUid1  = "task-uid1"
	taskUid2  = "task-uid2"
	podName1  = "pod1"
	podName2  = "pod2"

	devName0   = "Ascend910-0"
	devName1   = "Ascend910-1"
	devName2   = "Ascend910-2"
	devName3   = "Ascend910-2c-180-3"
	ip0        = "192.168.1.0"
	ip1        = "192.168.1.1"
	ip2        = "192.168.1.2"
	ip3        = "192.168.1.3"
	superPodID = 0
)

var (
	baseDeviceMap = map[string]*NpuBaseInfo{
		devName0: {
			IP:            ip0,
			SuperDeviceID: superPodID,
		},
		devName1: {
			IP:            ip1,
			SuperDeviceID: superPodID,
		},
		devName2: {
			IP:            ip2,
			SuperDeviceID: superPodID,
		},
		devName3: {
			IP:            ip3,
			SuperDeviceID: superPodID,
		},
	}
)

func TestGetNodeDevListFromAnno(t *testing.T) {
	baseDevInfo, err := json.Marshal(baseDeviceMap)
	if err != nil {
		return
	}
	nodeInfo := &api.NodeInfo{
		Node: &v1.Node{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{BaseDeviceInfoKey: string(baseDevInfo)},
			},
		},
	}
	t.Run("test func GetNodeDevListFromAnno success. If vnpu is included, it will be removed", func(t *testing.T) {
		expDevList := []string{devName0, devName1, devName2}
		got, err := GetNodeDevListFromAnno(nodeInfo)
		sort.Strings(got)
		if !reflect.DeepEqual(got, expDevList) || !reflect.DeepEqual(err, nil) {
			t.Errorf("Get node device list = %v, want %v. err = %v, want nil", got, expDevList, err)
		}
	})
	t.Run("test func GetNodeDevListFromAnno failed, annotation does not exist", func(t *testing.T) {
		invalidNodeInfo := &api.NodeInfo{
			Node: &v1.Node{
				TypeMeta: metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
		}
		_, err = GetNodeDevListFromAnno(invalidNodeInfo)
		expErr := fmt.Errorf("node annotation[%s] does not exist", BaseDeviceInfoKey)
		if !reflect.DeepEqual(err, expErr) {
			t.Errorf("Get node device list, err = %v, want %v", err, expErr)
		}
	})
	t.Run("test func GetNodeDevListFromAnno failed, unmarshal error", func(t *testing.T) {
		p1 := gomonkey.ApplyFunc(json.Unmarshal, func(data []byte, v any) error {
			return errors.New("test error")
		})
		defer p1.Reset()
		_, err = GetNodeDevListFromAnno(nodeInfo)
		expErr := errors.New("unmarshal node device list failed")
		if !reflect.DeepEqual(err, expErr) {
			t.Errorf("Get node device list, err = %v, want %v", err, expErr)
		}
	})
}

func TestGetActivePodUsedDevFromNode(t *testing.T) {
	tests := []struct {
		name     string
		nodeInfo *api.NodeInfo
		want     []string
	}{
		{
			name:     "01 test func GetActivePodUsedDevFromNode success",
			nodeInfo: fakeNodeInfo(pod1, pod2),
			want:     []string{devName0, devName2},
		},
		{
			name:     "02 test func GetActivePodUsedDevFromNode success, pod success",
			nodeInfo: fakeNodeInfo(pod1, successPod),
			want:     []string{devName0},
		},
		{
			name:     "03 test func GetActivePodUsedDevFromNode failed, pod name and namespace is invalid",
			nodeInfo: fakeNodeInfo(invalidNamePod, invalidNSPod),
			want:     []string{},
		},
		{
			name:     "04 test func GetActivePodUsedDevFromNode failed, annotation does not exist",
			nodeInfo: fakeNodeInfo(pod1, annoDoesNotExistPod),
			want:     []string{devName0},
		},
		{
			name:     "05 test func GetActivePodUsedDevFromNode failed, annotation does not exist",
			nodeInfo: fakeNodeInfo(pod1, annoDoesNotExistPod),
			want:     []string{devName0},
		},
		{
			name:     "06 test func GetActivePodUsedDevFromNode failed, annotation does not exist",
			nodeInfo: fakeNodeInfo(pod1, invalidAnnoPod),
			want:     []string{devName0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetActivePodUsedDevFromNode(tt.nodeInfo, Ascend910)
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Get active pod used device list = %v, want %v", got, tt.want)
			}
		})
	}
}

var (
	pod1 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName1,
			Namespace:   defaultNS,
			Annotations: map[string]string{HwPreName + Ascend910: strings.Join([]string{devName0}, ",")},
		},
	}
	pod2 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName2,
			Namespace:   defaultNS,
			Annotations: map[string]string{HwPreName + Ascend910: strings.Join([]string{devName2}, ",")},
		},
	}
	successPod = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName2,
			Namespace:   defaultNS,
			Annotations: map[string]string{HwPreName + Ascend910: strings.Join([]string{devName2}, ",")},
		},
		Status: v1.PodStatus{Phase: v1.PodFailed},
	}

	invalidNamePod = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "???",
			Namespace:   defaultNS,
			Annotations: map[string]string{HwPreName + Ascend910: strings.Join([]string{devName0}, ",")},
		},
	}
	invalidNSPod = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName1,
			Namespace:   "???",
			Annotations: map[string]string{HwPreName + Ascend910: strings.Join([]string{devName0}, ",")},
		},
	}
	annoDoesNotExistPod = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName1,
			Namespace: defaultNS,
		},
	}
	invalidAnnoPod = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        podName1,
			Namespace:   defaultNS,
			Annotations: map[string]string{HwPreName + Ascend910: ""},
		},
	}
)

func fakeNodeInfo(pod1, pod2 *v1.Pod) *api.NodeInfo {
	taskInfo1 := &api.TaskInfo{
		UID:       taskUid1,
		Name:      taskName1,
		Namespace: defaultNS,
		Pod:       pod1,
	}
	taskInfo2 := &api.TaskInfo{
		UID:       taskUid2,
		Name:      taskName2,
		Namespace: defaultNS,
		Pod:       pod2,
	}

	baseDevInfo, err := json.Marshal(baseDeviceMap)
	if err != nil {
		return nil
	}
	nodeInfo := &api.NodeInfo{
		Node: &v1.Node{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{BaseDeviceInfoKey: string(baseDevInfo)},
			},
		},
		Tasks: map[api.TaskID]*api.TaskInfo{taskInfo1.UID: taskInfo1, taskInfo2.UID: taskInfo2},
	}
	return nodeInfo
}

// FakeDeviceList fake device list
func FakeDeviceList() map[string]string {
	return map[string]string{
		NPU910CardName:         availNPU,
		networkUnhealthyNPUKey: networkUnhealthyNPUValue,
		unhealthyNPUKey:        unhealthyNPUValue,
		recoveringNPUKey:       unhealthyNPUValue,
	}
}

const (
	availNPU                 = "Ascend910-2,Ascend910-3,Ascend910-4,Ascend910-5,Ascend910-6,Ascend910-7"
	networkUnhealthyNPUKey   = "huawei.com/Ascend910-NetworkUnhealthy"
	networkUnhealthyNPUValue = "Ascend910-1"
	unhealthyNPUKey          = "huawei.com/Ascend910-Unhealthy"
	recoveringNPUKey         = "huawei.com/Ascend910-Recovering"
	unhealthyNPUValue        = "Ascend910-0"
	recoveringNPUValue       = "Ascend910-0"
)

func TestGetAvailableDevInfo(t *testing.T) {
	t.Run("test func GetAvailableDevInfo success", func(t *testing.T) {
		availDevKey, availDevList := GetAvailableDevInfo(FakeDeviceList())
		if !reflect.DeepEqual(availDevKey, NPU910CardName) || !reflect.DeepEqual(strings.Join(availDevList, ","), availNPU) {
			t.Errorf("get available device info key = %v, want %v; value = %v, want = %v",
				availDevKey, NPU910CardName, availDevList, availNPU)
		}
	})
}

func TestGetUnhealthyDevInfo(t *testing.T) {
	t.Run("test func GetUnhealthyDevInfo success", func(t *testing.T) {
		unHealthyKey, unHealthyDevList := GetUnhealthyDevInfo(FakeDeviceList())
		if !reflect.DeepEqual(unHealthyKey, unhealthyNPUKey) || !reflect.DeepEqual(strings.Join(unHealthyDevList,
			","), unhealthyNPUValue) {
			t.Errorf("get available device info key = %v, want %v; value = %v, want = %v",
				unHealthyKey, NPU910CardName, unHealthyDevList, availNPU)
		}
	})
}
func TestGetRecoveringDevInfo(t *testing.T) {
	t.Run("test func GetRecoveringDevInfo success", func(t *testing.T) {
		recoveringKey, recoveringList := GetRecoveringDevInfo(FakeDeviceList())
		if !reflect.DeepEqual(recoveringKey, recoveringNPUKey) || !reflect.DeepEqual(strings.Join(recoveringList,
			","), recoveringNPUValue) {
			t.Errorf("get available device info key = %v, want %v; value = %v, want = %v",
				recoveringKey, NPU910CardName, recoveringList, availNPU)
		}
	})
}

func TestIsNPUTask(t *testing.T) {
	tests := []struct {
		name     string
		taskInfo *api.TaskInfo
		want     bool
	}{
		{
			name: "01 test func IsNPUTask, npu resource available",
			taskInfo: &api.TaskInfo{
				Resreq: &api.Resource{ScalarResources: map[v1.ResourceName]float64{NPU910CardName: 8}},
			},
			want: true,
		},
		{
			name:     "02 test func IsNPUTask, no npu resource available",
			taskInfo: &api.TaskInfo{Resreq: &api.Resource{}},
			want:     false,
		},
		{
			name:     "03 test func IsNPUTask, task is nil",
			taskInfo: nil,
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isNpuTask := IsNPUTask(tt.taskInfo)
			if !(isNpuTask == tt.want) {
				t.Errorf("is npu task = %v, want %v", isNpuTask, tt.want)
			}
		})
	}
}

func TestIsStrategyInSubHealthyStrategse(t *testing.T) {
	tests := []struct {
		name     string
		strategy string
		want     bool
	}{
		{
			name:     "01 test func IsStrategyInSubHealthyStrategse, strategy is in sub healthy strategies",
			strategy: SubHealthyIgnore,
			want:     true,
		},
		{
			name:     "02 test func IsStrategyInSubHealthyStrategse, strategy is not in sub healthy strategies",
			strategy: "otherStrategy",
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isStrategyInSubHealthyStrategse := IsStrategyInSubHealthyStrategs(tt.strategy)
			if !(isStrategyInSubHealthyStrategse == tt.want) {
				t.Errorf("is strategy in sub healthy strategies = %v, want %v", isStrategyInSubHealthyStrategse, tt.want)
			}
		})
	}
}
