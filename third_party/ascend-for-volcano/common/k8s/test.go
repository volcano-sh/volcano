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
Package k8s is using for the k8s operation.
*/
package k8s

import (
	"encoding/json"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/third_party/ascend-for-volcano/common/util"
)

const (
	annoCards           = "Ascend910-0,Ascend910-1,Ascend910-2,Ascend910-3,Ascend910-4,Ascend910-5,Ascend910-6,Ascend910-7"
	networkUnhealthyNPU = "huawei.com/Ascend910-NetworkUnhealthy"
	unhealthyNPU        = "huawei.com/Ascend910-Unhealthy"
)

// FakeDeviceInfoCMDataByNode fake device info cm data
func FakeDeviceInfoCMDataByNode(nodeName string, deviceList map[string]string) *v1.ConfigMap {
	cmName := util.DevInfoPreName + nodeName
	const testTime = 1657527526
	cmData := NodeDeviceInfoWithDevPlugin{
		DeviceInfo: NodeDeviceInfo{
			DeviceList: deviceList,
			UpdateTime: testTime,
		},
		CheckCode: "6b8de396fd9945be231d24720ca66ed950baf0a5972717f335aad7571cb6457a",
	}
	var data = make(map[string]string, 1)
	cmDataStr, err := json.Marshal(cmData)
	if err != nil {
		return nil
	}
	data["DeviceInfoCfg"] = string(cmDataStr)
	data[util.SwitchInfoCmKey] = FakeSwitchInfos()
	var faultNPUConfigMap = &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cmName,
			Namespace: "kube-system",
		},
		Data: data,
	}
	return faultNPUConfigMap
}

// FakeDeviceList fake device list
func FakeDeviceList() map[string]string {
	return map[string]string{
		util.NPU910CardName: annoCards,
		networkUnhealthyNPU: "",
		unhealthyNPU:        "",
	}
}

// FakeSwitchInfos fake switch infos
func FakeSwitchInfos() string {
	tmpSwitchInfo := SwitchFaultInfo{
		NodeStatus: util.NodeHealthyByNodeD,
	}
	if bytes, err := json.Marshal(tmpSwitchInfo); err == nil {
		return string(bytes)
	}
	return ""
}

// FakeNodeInfos fake node infos
func FakeNodeInfos() map[string]string {
	nodeInfos := NodeDNodeInfo{
		NodeStatus: util.NodeHealthyByNodeD,
	}
	tmpData := NodeInfoWithNodeD{
		NodeInfo:  nodeInfos,
		CheckCode: util.MakeDataHash(nodeInfos),
	}

	nodeInfoBytes, err := json.Marshal(tmpData)
	if err != nil {
		return nil
	}
	return map[string]string{
		util.NodeInfoCMKey: string(nodeInfoBytes),
	}
}
