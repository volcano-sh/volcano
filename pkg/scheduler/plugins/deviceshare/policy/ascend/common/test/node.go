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
Package test is using for HuaWei Ascend pin scheduling test.
*/
package test

import (
	"strconv"
	"strings"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/scheduler/api"
)

// BuildNPUNode built NPU node object
func BuildNPUNode(node NPUNode) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:        node.Name,
			Labels:      node.Labels,
			Annotations: node.Annotation,
		},
		Status: v1.NodeStatus{
			Capacity:    node.Capacity,
			Allocatable: node.Allocatable,
			Addresses: []v1.NodeAddress{
				{Type: v1.NodeInternalIP, Address: fakeIpPrefix + strings.TrimLeft(node.Name, "node")},
			},
		},
	}
}

// FakeNormalTestNode fake normal test node.
func FakeNormalTestNode(name string) *api.NodeInfo {
	node := NPUNode{
		Name:        name,
		Capacity:    fakeNodeResourceList(),
		Allocatable: fakeNodeResourceList(),
		Labels:      make(map[string]string, npuIndex3),
		Selector:    make(map[string]string, npuIndex3),
		Annotation:  make(map[string]string, npuIndex3),
		Other:       make(map[string]interface{}, npuIndex3),
	}
	nodeInfo := api.NewNodeInfo(BuildNPUNode(node))
	SetFakeDefaultNodeSource(nodeInfo)
	addFakeNodeSource(nodeInfo, NPU910CardName, NPUIndex8)
	addFakeNodeSource(nodeInfo, npuCoreName, npuCodex200)
	return nodeInfo
}

// FakeNormalTestNodes fake normal test nodes.
func FakeNormalTestNodes(num int) []*api.NodeInfo {
	var nodes []*api.NodeInfo

	for i := 0; i < num; i++ {
		strNum := strconv.Itoa(i)
		nodeInfo := FakeNormalTestNode("node" + strNum)
		nodes = append(nodes, nodeInfo)
	}

	return nodes
}

// SetFakeDefaultNodeSource Set fake node the idle, Capability, Allocatable source.
func SetFakeDefaultNodeSource(nodeInf *api.NodeInfo) {
	tmpResource := api.Resource{
		Memory:          NPUIndex8 * NPUHexKilo,
		MilliCPU:        NPUIndex8 * NPUHexKilo,
		ScalarResources: map[v1.ResourceName]float64{},
	}
	nodeInf.Idle = &tmpResource
	nodeInf.Capacity = &tmpResource
	nodeInf.Allocatable = &tmpResource
}

// addFakeNodeSource add fake node the idle, Capability, Allocatable source.
func addFakeNodeSource(nodeInf *api.NodeInfo, name string, value int) {
	nodeInf.Idle.ScalarResources[v1.ResourceName(name)] = float64(value) * NPUHexKilo
	nodeInf.Capacity.ScalarResources[v1.ResourceName(name)] = float64(value) * NPUHexKilo
	nodeInf.Allocatable.ScalarResources[v1.ResourceName(name)] = float64(value) * NPUHexKilo
}

func fakeNodeResourceList() v1.ResourceList {
	fakeResourceList := v1.ResourceList{}
	AddResource(fakeResourceList, v1.ResourceCPU, fakeResourceNum)
	AddResource(fakeResourceList, v1.ResourceMemory, fakeResourceNum)
	AddResource(fakeResourceList, NPU910CardName, fakeNPUNum)
	AddResource(fakeResourceList, npuCoreName, fakeNpuCodeNum)
	return fakeResourceList
}
