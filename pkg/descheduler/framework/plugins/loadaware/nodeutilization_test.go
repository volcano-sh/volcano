/*
Copyright 2021 The Kubernetes Authors.

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

package loadaware

import (
	"math"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	lowPriority      = int32(0)
	highPriority     = int32(10000)
	extendedResource = v1.ResourceName("example.com/foo")
	testNode1        = NodeInfo{
		NodeUsage: NodeUsage{
			node: &v1.Node{
				Status: v1.NodeStatus{
					Capacity: v1.ResourceList{
						v1.ResourceCPU:    *resource.NewMilliQuantity(2000, resource.DecimalSI),
						v1.ResourceMemory: *resource.NewQuantity(3977868*1024, resource.BinarySI),
						v1.ResourcePods:   *resource.NewQuantity(29, resource.BinarySI),
					},
					Allocatable: v1.ResourceList{
						v1.ResourceCPU:    *resource.NewMilliQuantity(1930, resource.DecimalSI),
						v1.ResourceMemory: *resource.NewQuantity(3287692*1024, resource.BinarySI),
						v1.ResourcePods:   *resource.NewQuantity(29, resource.BinarySI),
					},
				},
				ObjectMeta: metav1.ObjectMeta{Name: "node1"},
			},
			usage: map[v1.ResourceName]*resource.Quantity{
				v1.ResourceCPU:    resource.NewMilliQuantity(1730, resource.DecimalSI),
				v1.ResourceMemory: resource.NewQuantity(3038982964, resource.BinarySI),
				v1.ResourcePods:   resource.NewQuantity(25, resource.BinarySI),
			},
		},
	}
	testNode2 = NodeInfo{
		NodeUsage: NodeUsage{
			node: &v1.Node{
				Status: v1.NodeStatus{
					Capacity: v1.ResourceList{
						v1.ResourceCPU:    *resource.NewMilliQuantity(2000, resource.DecimalSI),
						v1.ResourceMemory: *resource.NewQuantity(3977868*1024, resource.BinarySI),
						v1.ResourcePods:   *resource.NewQuantity(29, resource.BinarySI),
					},
					Allocatable: v1.ResourceList{
						v1.ResourceCPU:    *resource.NewMilliQuantity(1930, resource.DecimalSI),
						v1.ResourceMemory: *resource.NewQuantity(3287692*1024, resource.BinarySI),
						v1.ResourcePods:   *resource.NewQuantity(29, resource.BinarySI),
					},
				},
				ObjectMeta: metav1.ObjectMeta{Name: "node2"},
			},
			usage: map[v1.ResourceName]*resource.Quantity{
				v1.ResourceCPU:    resource.NewMilliQuantity(1220, resource.DecimalSI),
				v1.ResourceMemory: resource.NewQuantity(3038982964, resource.BinarySI),
				v1.ResourcePods:   resource.NewQuantity(11, resource.BinarySI),
			},
		},
	}
	testNode3 = NodeInfo{
		NodeUsage: NodeUsage{
			node: &v1.Node{
				Status: v1.NodeStatus{
					Capacity: v1.ResourceList{
						v1.ResourceCPU:    *resource.NewMilliQuantity(2000, resource.DecimalSI),
						v1.ResourceMemory: *resource.NewQuantity(3977868*1024, resource.BinarySI),
						v1.ResourcePods:   *resource.NewQuantity(29, resource.BinarySI),
					},
					Allocatable: v1.ResourceList{
						v1.ResourceCPU:    *resource.NewMilliQuantity(1930, resource.DecimalSI),
						v1.ResourceMemory: *resource.NewQuantity(3287692*1024, resource.BinarySI),
						v1.ResourcePods:   *resource.NewQuantity(29, resource.BinarySI),
					},
				},
				ObjectMeta: metav1.ObjectMeta{Name: "node3"},
			},
			usage: map[v1.ResourceName]*resource.Quantity{
				v1.ResourceCPU:    resource.NewMilliQuantity(1530, resource.DecimalSI),
				v1.ResourceMemory: resource.NewQuantity(5038982964, resource.BinarySI),
				v1.ResourcePods:   resource.NewQuantity(20, resource.BinarySI),
			},
		},
	}
)

func TestResourceUsagePercentages(t *testing.T) {
	resourceUsagePercentage := resourceUsagePercentages(NodeUsage{
		node: &v1.Node{
			Status: v1.NodeStatus{
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    *resource.NewMilliQuantity(2000, resource.DecimalSI),
					v1.ResourceMemory: *resource.NewQuantity(3977868*1024, resource.BinarySI),
					v1.ResourcePods:   *resource.NewQuantity(29, resource.BinarySI),
				},
				Allocatable: v1.ResourceList{
					v1.ResourceCPU:    *resource.NewMilliQuantity(1930, resource.DecimalSI),
					v1.ResourceMemory: *resource.NewQuantity(3287692*1024, resource.BinarySI),
					v1.ResourcePods:   *resource.NewQuantity(29, resource.BinarySI),
				},
			},
		},
		usage: map[v1.ResourceName]*resource.Quantity{
			v1.ResourceCPU:    resource.NewMilliQuantity(1220, resource.DecimalSI),
			v1.ResourceMemory: resource.NewQuantity(3038982964, resource.BinarySI),
			v1.ResourcePods:   resource.NewQuantity(11, resource.BinarySI),
		},
	})

	expectedUsageInIntPercentage := map[v1.ResourceName]float64{
		v1.ResourceCPU:    63,
		v1.ResourceMemory: 90,
		v1.ResourcePods:   37,
	}

	for resourceName, percentage := range expectedUsageInIntPercentage {
		if math.Floor(resourceUsagePercentage[resourceName]) != percentage {
			t.Errorf("Incorrect percentange computation, expected %v, got math.Floor(%v) instead", percentage, resourceUsagePercentage[resourceName])
		}
	}

	t.Logf("resourceUsagePercentage: %#v\n", resourceUsagePercentage)
}

func TestSortNodesByUsageDescendingOrder(t *testing.T) {
	nodeList := []NodeInfo{testNode1, testNode2, testNode3}
	expectedNodeList := []NodeInfo{testNode3, testNode1, testNode2} // testNode3 has the highest usage
	sortNodesByUsage(nodeList, false)                               // ascending=false, sort nodes in descending order

	for i := 0; i < len(expectedNodeList); i++ {
		if nodeList[i].NodeUsage.node.Name != expectedNodeList[i].NodeUsage.node.Name {
			t.Errorf("Expected %v, got %v", expectedNodeList[i].NodeUsage.node.Name, nodeList[i].NodeUsage.node.Name)
		}
	}
}

func TestSortNodesByUsageAscendingOrder(t *testing.T) {
	nodeList := []NodeInfo{testNode1, testNode2, testNode3}
	expectedNodeList := []NodeInfo{testNode2, testNode1, testNode3}
	sortNodesByUsage(nodeList, true) // ascending=true, sort nodes in ascending order

	for i := 0; i < len(expectedNodeList); i++ {
		if nodeList[i].NodeUsage.node.Name != expectedNodeList[i].NodeUsage.node.Name {
			t.Errorf("Expected %v, got %v", expectedNodeList[i].NodeUsage.node.Name, nodeList[i].NodeUsage.node.Name)
		}
	}
}
