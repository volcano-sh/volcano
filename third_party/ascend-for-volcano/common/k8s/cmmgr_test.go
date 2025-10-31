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
Package k8s is using for the k8s operation.
*/
package k8s

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/third_party/ascend-for-volcano/common/util"
	"volcano.sh/volcano/third_party/ascend-for-volcano/test"
)

func fakeClusterInfoCm[T any](cmPrefix string) *v1.ConfigMap {
	var info T
	tmpInfo := map[string]T{
		"node0": info,
	}
	bytes, _ := json.Marshal(tmpInfo)
	nodeName := cmPrefix + "node0"
	return test.FakeConfigmap(nodeName, util.MindXDlNameSpace, map[string]string{nodeName: string(bytes)})
}

func TestInitCmInformer(t *testing.T) {
	t.Run("01 k8s client is nil", func(t *testing.T) {
		InitCmInformer(nil, true)
	})
	t.Run("02 start device info informer success with clusterd", func(t *testing.T) {
		InitCmInformer(fake.NewSimpleClientset(), true)
	})
	needStartInformer = true
	t.Run("03 start device info informer success without clutserd", func(t *testing.T) {
		InitCmInformer(fake.NewSimpleClientset(), false)
	})
}

type UpdateConfigMapTestCase struct {
	name      string
	cmManager *ClusterInfoWitchCm
	obj       interface{}
	operator  string
	want      *ClusterInfoWitchCm
}

func buildUpdateConfigMapTestCases() []UpdateConfigMapTestCase {
	return []UpdateConfigMapTestCase{
		{
			name:      "01 will return empty when cm manager is nil",
			cmManager: nil,
			obj:       nil,
			want:      nil,
		},
		{
			name:      "02 will return empty when cm is nil",
			cmManager: &ClusterInfoWitchCm{},
			obj:       nil,
			want:      &ClusterInfoWitchCm{},
		},
		{
			name:      "03 will return cm mgr when cm is device info and operator is add",
			cmManager: &cmManager,
			obj:       FakeDeviceInfoCMDataByNode("node0", FakeDeviceList()),
			operator:  util.AddOperator,
			want:      &cmManager,
		},
		{
			name:      "04 will return cm mgr when cm is device info and operator is delete",
			cmManager: &cmManager,
			obj:       FakeDeviceInfoCMDataByNode("node0", FakeDeviceList()),
			operator:  util.DeleteOperator,
			want:      &cmManager,
		},
		{
			name:      "05 will return cm mgr when cm is noded info and operator is add",
			cmManager: &cmManager,
			obj:       test.FakeConfigmap(util.NodeDCmInfoNamePrefix+"node0", util.MindXDlNameSpace, FakeNodeInfos()),
			operator:  util.AddOperator,
			want:      &cmManager,
		},
		{
			name:      "06 will return cm mgr when cm is noded info and operator is delete",
			cmManager: &cmManager,
			obj:       test.FakeConfigmap(util.NodeDCmInfoNamePrefix+"node0", util.MindXDlNameSpace, FakeNodeInfos()),
			operator:  util.DeleteOperator,
			want:      &cmManager,
		},
		{
			name:      "06 will return cm mgr when cm is noded info and operator is add but cm date is nil",
			cmManager: &cmManager,
			obj:       test.FakeConfigmap(util.NodeDCmInfoNamePrefix+"node0", util.MindXDlNameSpace, nil),
			operator:  util.AddOperator,
			want:      &cmManager,
		},
	}
}

func TestUpdateConfigMap(t *testing.T) {
	for _, tt := range buildUpdateConfigMapTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			tt.cmManager.updateConfigMap(tt.obj, tt.operator)
			if !reflect.DeepEqual(tt.cmManager, tt.want) {
				t.Errorf("update cm failed, cm manager is different. cmMgr is %v want %v", tt.cmManager, tt.want)
			}
		})
	}
}

func buildUpdateConfigMapClusterTestCases01() []UpdateConfigMapTestCase {
	return []UpdateConfigMapTestCase{
		{
			name:      "01 will return empty when cm manager is nil",
			cmManager: nil,
			obj:       nil,
			want:      nil,
		},
		{
			name:      "02 will return empty when cm is nil",
			cmManager: &ClusterInfoWitchCm{},
			obj:       nil,
			want:      &ClusterInfoWitchCm{},
		},
		{
			name:      "03 obj is cluster device info add test",
			cmManager: &cmManager,
			obj:       fakeClusterInfoCm[NodeDeviceInfoWithID](util.ClusterDeviceInfo),
			operator:  util.AddOperator,
			want:      &cmManager,
		},
		{
			name:      "04 obj is cluster device info delete test",
			cmManager: &cmManager,
			obj:       fakeClusterInfoCm[NodeDeviceInfoWithID](util.ClusterDeviceInfo),
			operator:  util.DeleteOperator,
			want:      &cmManager,
		},
	}
}

func buildUpdateConfigMapClusterTestCases02() []UpdateConfigMapTestCase {
	return []UpdateConfigMapTestCase{
		{
			name:      "05 obj is cluster noded info delete test",
			cmManager: &cmManager,
			obj:       fakeClusterInfoCm[NodeDNodeInfo](util.ClusterNodeInfo),
			operator:  util.DeleteOperator,
			want:      &cmManager,
		},
		{
			name:      "06 obj is cluster noded info add test",
			cmManager: &cmManager,
			obj:       fakeClusterInfoCm[NodeDNodeInfo](util.ClusterNodeInfo),
			operator:  util.AddOperator,
			want:      &cmManager,
		},
		{
			name:      "07 obj is cluster switch info delete test",
			cmManager: &cmManager,
			obj:       fakeClusterInfoCm[SwitchFaultInfo](util.ClusterSwitchInfo),
			operator:  util.DeleteOperator,
			want:      &cmManager,
		},
		{
			name:      "08 obj is cluster switch info add test",
			cmManager: &cmManager,
			obj:       fakeClusterInfoCm[SwitchFaultInfo](util.ClusterSwitchInfo),
			operator:  util.AddOperator,
			want:      &cmManager,
		},
		{
			name:      "09 obj is empty cluster node info add test",
			cmManager: &cmManager,
			obj:       test.FakeConfigmap(util.ClusterNodeInfo, util.MindXDlNameSpace, map[string]string{util.ClusterNodeInfo: ""}),
			operator:  util.AddOperator,
			want:      &cmManager,
		},
	}
}

func TestUpdateConfigMapCluster(t *testing.T) {
	tests := append(buildUpdateConfigMapClusterTestCases01(), buildUpdateConfigMapClusterTestCases02()...)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cmManager.updateConfigMapCluster(tt.obj, tt.operator)
			if !reflect.DeepEqual(tt.cmManager, tt.want) {
				t.Errorf("update cm failed, cm manager is different. cmMgr is %v want %v", tt.cmManager, tt.want)
			}
		})
	}
}

func TestGetCmInfos(t *testing.T) {
	nodeList := []*api.NodeInfo{{Name: testName}}
	t.Run("GetSwitchInfos test, get empty switch info", func(t *testing.T) {
		if got := GetSwitchInfos(nodeList); !reflect.DeepEqual(got, map[string]SwitchFaultInfo{testName: {}}) {
			t.Errorf("GetSwitchInfos() = %v, want %v", got, map[string]SwitchFaultInfo{testName: {}})
		}
	})

	t.Run("GetNodeDInfos test, get empty nodeD info", func(t *testing.T) {
		if got := GetNodeDInfos(nodeList); !reflect.DeepEqual(got, map[string]NodeDNodeInfo{testName: {}}) {
			t.Errorf("GetSwitchInfos() = %v, want %v", got, map[string]NodeDNodeInfo{testName: {}})
		}
	})
	tmpDeviceList := NodeDeviceInfo{DeviceList: make(map[string]string)}
	tmpDeviceInfos := map[string]NodeDeviceInfoWithID{testName: {NodeDeviceInfo: tmpDeviceList}}
	needStartInformer = false
	t.Run("Get device info test, get empty device info with use clusterD", func(t *testing.T) {
		if got := GetDeviceInfosAndSetInformerStart(nodeList, true, true); !reflect.DeepEqual(got, tmpDeviceInfos) {
			t.Errorf("Get device info = %v, want %v", got, tmpDeviceInfos)
		}
	})

	t.Run("Get device info test, get empty device info without use clusterD", func(t *testing.T) {
		if got := GetDeviceInfosAndSetInformerStart(nodeList, false, true); !reflect.DeepEqual(got, tmpDeviceInfos) {
			t.Errorf("Get device info = %v, want %v", got, tmpDeviceInfos)
		}
	})

	t.Run("Get device info test, get empty device info without use clusterD", func(t *testing.T) {
		if got := GetDeviceInfosAndSetInformerStart(nodeList, false, true); !reflect.DeepEqual(got, tmpDeviceInfos) {
			t.Errorf("Get device info = %v, want %v", got, tmpDeviceInfos)
		}
	})
}

const (
	nodeName = "nodeName"

	devName0   = "Ascend910-0"
	devName1   = "Ascend910-1"
	devName2   = "Ascend910-2"
	devName3   = "Ascend910-3"
	devName4   = "Ascend910-4"
	devName5   = "Ascend910-5"
	devName6   = "Ascend910-6"
	devName7   = "Ascend910-7"
	ip0        = "192.168.1.0"
	ip1        = "192.168.1.1"
	ip2        = "192.168.1.2"
	ip3        = "192.168.1.3"
	ip4        = "192.168.1.4"
	ip5        = "192.168.1.5"
	ip6        = "192.168.1.6"
	ip7        = "192.168.1.7"
	superPodID = 0
)

var (
	baseDeviceMap = map[string]*util.NpuBaseInfo{
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
		devName4: {
			IP:            ip4,
			SuperDeviceID: superPodID,
		},
		devName5: {
			IP:            ip5,
			SuperDeviceID: superPodID,
		},
		devName6: {
			IP:            ip6,
			SuperDeviceID: superPodID,
		},
		devName7: {
			IP:            ip7,
			SuperDeviceID: superPodID,
		},
	}
)

type calAvailCardTestCase struct {
	name                  string
	selfMaintainAvailCard bool
	nodeDevList           string
	podUsedCard1          []string
	podUsedCard2          []string
	want                  string
	mock                  *gomonkey.Patches
}

func buildCalAvailCardTestCase() []calAvailCardTestCase {
	return []calAvailCardTestCase{
		{
			name:                  "01 the card used by the pod are consecutive",
			nodeDevList:           annoCards,
			podUsedCard1:          []string{devName0, devName1, devName2},
			podUsedCard2:          []string{devName3, devName4, devName5},
			selfMaintainAvailCard: true,
			want:                  strings.Join([]string{devName6, devName7}, ","),
		},
		{
			name:                  "02 the card used by the pod are out of order",
			nodeDevList:           annoCards,
			podUsedCard1:          []string{devName5, devName1},
			podUsedCard2:          []string{devName4, devName3},
			selfMaintainAvailCard: true,
			want:                  strings.Join([]string{devName0, devName2, devName6, devName7}, ","),
		},
		{
			name:                  "03 the card used by the pod are duplicated",
			nodeDevList:           annoCards,
			podUsedCard1:          []string{devName5, devName1},
			podUsedCard2:          []string{devName4, devName4},
			selfMaintainAvailCard: true,
			want:                  strings.Join([]string{devName0, devName2, devName3, devName6, devName7}, ","),
		},
		{
			name:                  "04 the card used by the pod is empty",
			nodeDevList:           annoCards,
			podUsedCard1:          []string{devName5, devName7, devName1},
			podUsedCard2:          []string{},
			selfMaintainAvailCard: true,
			want:                  strings.Join([]string{devName0, devName2, devName3, devName4, devName6}, ","),
		},
		{
			name:                  "05 selfMaintainAvailCard is false",
			nodeDevList:           annoCards,
			podUsedCard1:          []string{devName0, devName1, devName2},
			podUsedCard2:          []string{devName3, devName4, devName5},
			selfMaintainAvailCard: false,
			want:                  FakeDeviceList()[util.NPU910CardName],
		},
	}
}

func TestInitNodeDeviceInfoByCmMgr(t *testing.T) {
	baseDevInfo, err := json.Marshal(baseDeviceMap)
	if err != nil {
		return
	}
	devInfo := NodeDeviceInfoWithID{
		NodeDeviceInfo: NodeDeviceInfo{
			UpdateTime: time.Now().Unix() + util.NPUIndex3,
			DeviceList: FakeDeviceList(),
		},
	}
	for _, tt := range buildCalAvailCardTestCase() {
		t.Run(tt.name, func(t *testing.T) {
			nodeInfo := &api.NodeInfo{
				Name: testName,
				Node: &v1.Node{
					TypeMeta: metav1.TypeMeta{},
					ObjectMeta: metav1.ObjectMeta{
						Name:        nodeName,
						Annotations: map[string]string{util.BaseDeviceInfoKey: string(baseDevInfo)},
					},
				},
				Tasks: test.FakeTaskInfo(tt.podUsedCard1, tt.podUsedCard2),
			}

			got := initNodeDeviceInfoByCmMgr(nodeInfo, devInfo, tt.selfMaintainAvailCard)
			if !reflect.DeepEqual(got.DeviceList[util.NPU910CardName], tt.want) {
				t.Errorf("get device list = %v, want %v", got.DeviceList, tt.want)
			}
		})
	}
}
