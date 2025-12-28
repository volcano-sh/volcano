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

import "sync"

// ClusterInfoWitchCm cluster info whit different configmap
type ClusterInfoWitchCm struct {
	deviceInfos       *DeviceInfosWithMutex
	nodeInfosFromCm   *NodeInfosFromCmWithMutex   // NodeInfos is get from kube-system/node-info- configmap
	switchInfosFromCm *SwitchInfosFromCmWithMutex // switchInfosFromCm is get from mindx-dl/device-info- configmap
}

// DeviceInfosWithMutex information for the current plugin
type DeviceInfosWithMutex struct {
	sync.Mutex
	Devices map[string]NodeDeviceInfoWithID
}

// NodeInfosFromCmWithMutex node info with mutex
type NodeInfosFromCmWithMutex struct {
	sync.Mutex
	Nodes map[string]NodeDNodeInfo
}

// SwitchInfosFromCmWithMutex SwitchInfos From Cm WithMutex
type SwitchInfosFromCmWithMutex struct {
	sync.Mutex
	Switches map[string]SwitchFaultInfo
}

// NodeDeviceInfo like node annotation.
type NodeDeviceInfo struct {
	DeviceList map[string]string
	UpdateTime int64
}

// NodeDeviceInfoWithID is node the information reported by cm.
type NodeDeviceInfoWithID struct {
	NodeDeviceInfo
	CacheUpdateTime int64
	SuperPodID      int32
}

// NodeDNodeInfo is node the information reported by noded
type NodeDNodeInfo struct {
	FaultDevList []struct {
		DeviceType string
		DeviceId   int
		FaultCode  []string
		FaultLevel string
	}
	NodeStatus string
}

// SwitchFaultInfo Switch Fault Info
type SwitchFaultInfo struct {
	FaultCode  []string
	FaultLevel string
	UpdateTime int64
	NodeStatus string
}

// NodeDeviceInfoWithDevPlugin a node has one by cm.
type NodeDeviceInfoWithDevPlugin struct {
	DeviceInfo  NodeDeviceInfo
	CheckCode   string
	SuperPodID  int32 `json:"SuperPodID,omitempty"`
	ServerIndex int32 `json:"ServerIndex,omitempty"`
}

// NodeInfoWithNodeD is node the node information and checkCode reported by noded
type NodeInfoWithNodeD struct {
	NodeInfo  NodeDNodeInfo
	CheckCode string
}

// NewClusterInfoWitchCm new empty cluster info with cm
func NewClusterInfoWitchCm() ClusterInfoWitchCm {
	return ClusterInfoWitchCm{
		deviceInfos: &DeviceInfosWithMutex{
			Mutex:   sync.Mutex{},
			Devices: map[string]NodeDeviceInfoWithID{},
		},
		nodeInfosFromCm: &NodeInfosFromCmWithMutex{
			Mutex: sync.Mutex{},
			Nodes: map[string]NodeDNodeInfo{},
		},
		switchInfosFromCm: &SwitchInfosFromCmWithMutex{
			Mutex:    sync.Mutex{},
			Switches: map[string]SwitchFaultInfo{},
		},
	}
}
