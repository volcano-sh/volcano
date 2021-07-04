/*
 Copyright 2021 The Volcano Authors.

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

package api

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func nodeInfoEqual(l, r *NodeInfo) bool {
	return reflect.DeepEqual(l, r)
}

func TestNodeInfo_AddPod(t *testing.T) {
	// case1
	case01Node := buildNode("n1", buildResourceList("8000m", "10G"))
	case01Pod1 := buildPod("c1", "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("2000m", "2G"), []metav1.OwnerReference{}, make(map[string]string))
	// case2
	case02Node := buildNode("n2", buildResourceList("2000m", "1G"))
	case02Pod1 := buildPod("c2", "p1", "n2", v1.PodUnknown, buildResourceList("1000m", "2G"), []metav1.OwnerReference{}, make(map[string]string))

	tests := []struct {
		name            string
		node            *v1.Node
		pods            []*v1.Pod
		expected        *NodeInfo
		expectedFailure bool
	}{
		{
			name: "add 2 running non-owner pod",
			node: case01Node,
			pods: []*v1.Pod{case01Pod1, case01Pod2},
			expected: &NodeInfo{
				Name:                     "n1",
				Node:                     case01Node,
				Idle:                     buildResource("5000m", "7G"),
				Used:                     buildResource("3000m", "3G"),
				Releasing:                EmptyResource(),
				Pipelined:                EmptyResource(),
				OversubscriptionResource: EmptyResource(),
				Allocatable:              buildResource("8000m", "10G"),
				Capability:               buildResource("8000m", "10G"),
				State:                    NodeState{Phase: Ready},
				Tasks: map[TaskID]*TaskInfo{
					"c1/p1": NewTaskInfo(case01Pod1),
					"c1/p2": NewTaskInfo(case01Pod2),
				},
				GPUDevices:   make(map[int]*GPUDevice),
				bindingTasks: make(map[TaskID]string),
			},
		},
		{
			name: "add 1 unknown pod",
			node: case02Node,
			pods: []*v1.Pod{case02Pod1},
			expected: &NodeInfo{
				Name:                     "n2",
				Node:                     case02Node,
				Idle:                     buildResource("2000m", "1G"),
				Used:                     EmptyResource(),
				Releasing:                EmptyResource(),
				Pipelined:                EmptyResource(),
				OversubscriptionResource: EmptyResource(),
				Allocatable:              buildResource("2000m", "1G"),
				Capability:               buildResource("2000m", "1G"),
				State:                    NodeState{Phase: Ready},
				Tasks:                    map[TaskID]*TaskInfo{},
				GPUDevices:               make(map[int]*GPUDevice),
				bindingTasks:             make(map[TaskID]string),
			},
			expectedFailure: true,
		},
	}

	for i, test := range tests {
		ni := NewNodeInfo(test.node)

		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			err := ni.AddTask(pi)
			if err != nil && !test.expectedFailure {
				t.Errorf("node info %d: \n expected success, \n but got err %v \n", i, err)
			}
			if err == nil && test.expectedFailure {
				t.Errorf("node info %d: \n expected failure, \n but got success \n", i)
			}
		}

		if !nodeInfoEqual(ni, test.expected) {
			t.Errorf("node info %d: \n expected %v, \n got %v \n",
				i, test.expected, ni)
		}
	}
}

func TestNodeInfo_RemovePod(t *testing.T) {
	// case1
	case01Node := buildNode("n1", buildResourceList("8000m", "10G"))
	case01Pod1 := buildPod("c1", "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("2000m", "2G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod3 := buildPod("c1", "p3", "n1", v1.PodRunning, buildResourceList("3000m", "3G"), []metav1.OwnerReference{}, make(map[string]string))

	tests := []struct {
		name     string
		node     *v1.Node
		pods     []*v1.Pod
		rmPods   []*v1.Pod
		expected *NodeInfo
	}{
		{
			name:   "add 3 running non-owner pod, remove 1 running non-owner pod",
			node:   case01Node,
			pods:   []*v1.Pod{case01Pod1, case01Pod2, case01Pod3},
			rmPods: []*v1.Pod{case01Pod2},
			expected: &NodeInfo{
				Name:                     "n1",
				Node:                     case01Node,
				Idle:                     buildResource("4000m", "6G"),
				Used:                     buildResource("4000m", "4G"),
				OversubscriptionResource: EmptyResource(),
				Releasing:                EmptyResource(),
				Pipelined:                EmptyResource(),
				Allocatable:              buildResource("8000m", "10G"),
				Capability:               buildResource("8000m", "10G"),
				State:                    NodeState{Phase: Ready},
				Tasks: map[TaskID]*TaskInfo{
					"c1/p1": NewTaskInfo(case01Pod1),
					"c1/p3": NewTaskInfo(case01Pod3),
				},
				GPUDevices:   make(map[int]*GPUDevice),
				bindingTasks: make(map[TaskID]string),
			},
		},
	}

	for i, test := range tests {
		ni := NewNodeInfo(test.node)

		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ni.AddTask(pi)
		}

		for _, pod := range test.rmPods {
			pi := NewTaskInfo(pod)
			ni.RemoveTask(pi)
		}

		if !nodeInfoEqual(ni, test.expected) {
			t.Errorf("node info %d: \n expected %v, \n got %v \n",
				i, test.expected, ni)
		}
	}
}

func TestOrderNodeAdd(t *testing.T) {
	n := NewOrderNodes()
	node1 := buildNode("n1", buildResourceList("2000m", "10G"))
	ni1 := NewNodeInfo(node1)
	n.AddIfNotPresent("n1", ni1)

	node, ok := n.CheckAndGet("n1")
	if !ok {
		t.Fatalf("Expect: true, Got, false")
	}

	if !reflect.DeepEqual(node.Node, node1) {
		t.Fatalf("\nExpect: %v\n   Got, %v.", node1, node.Node)
	}

	if inx := n.Index("n1"); inx != 0 {
		t.Fatalf("Got index: %v, expect: %v", inx, 0)
	}
}

func TestOrderNodeUpdate(t *testing.T) {
	n := NewOrderNodes()
	node1 := buildNode("n1", buildResourceList("2000m", "10G"))
	ni1 := NewNodeInfo(node1)
	n.AddIfNotPresent("n1", ni1)

	node, ok := n.CheckAndGet("n1")
	if !ok {
		t.Fatalf("Expect: true, Got, false")
	}

	if !reflect.DeepEqual(node.Node, node1) {
		t.Fatalf("\nExpect: %v\n   Got, %v.", node1, node.Node)
	}

	node2 := buildNode("n2", buildResourceList("1000m", "1G"))
	ni2 := NewNodeInfo(node2)
	n.Update("n1", ni2)
	newNode := n.Get("n1")

	if !reflect.DeepEqual(newNode.Node, node2) {
		t.Fatalf("\nExpect: %v\n   Got, %v.", node2, newNode.Node)
	}
}

func TestOrderNodeIterate(t *testing.T) {

	node1 := buildNode("n1", buildResourceList("1000m", "1G"))
	node2 := buildNode("n2", buildResourceList("2000m", "2G"))
	node3 := buildNode("n3", buildResourceList("3000m", "3G"))

	tests := []struct {
		names        []string
		nodes        []*NodeInfo
		expectedName []string
	}{
		{
			names: []string{"n1", "n2", "n3", "n4"},
			nodes: []*NodeInfo{
				NewNodeInfo(node1),
				NewNodeInfo(node2),
				NewNodeInfo(node3),
				nil,
			},
		},
	}

	for _, test := range tests {
		n := NewOrderNodes()
		for j, node := range test.nodes {
			n.AddIfNotPresent(test.names[j], node)
		}

		res := n.IterateList()
		if !reflect.DeepEqual(n.IterateList(), test.nodes) {
			t.Fatalf("Expect:%v, Got:%v", test.nodes, res)
		}
	}
}

func TestNodeOperation(t *testing.T) {
	// case 1
	node1 := buildNode("n1", buildResourceList("2000m", "10G"))
	node2 := buildNode("n2", buildResourceList("4000m", "16G"))
	node3 := buildNode("n3", buildResourceList("3000m", "12G"))
	nodeInfo1 := NewNodeInfo(node1)
	nodeInfo2 := NewNodeInfo(node2)
	nodeInfo3 := NewNodeInfo(node3)
	tests := []struct {
		NodeList    []string
		deletedNode *v1.Node
		nodes       []*v1.Node
		expected    []*NodeInfo
		delExpect   []*NodeInfo
	}{
		{
			NodeList:    []string{"n1", "n2", "n3"},
			deletedNode: node2,
			nodes:       []*v1.Node{node1, node2, node3},
			expected: []*NodeInfo{
				nodeInfo1,
				nodeInfo2,
				nodeInfo3,
			},
			delExpect: []*NodeInfo{
				nodeInfo1,
				nodeInfo3,
			},
		},
		{
			NodeList:    []string{"n1", "n2", "n3"},
			deletedNode: node1,
			nodes:       []*v1.Node{node1, node2, node3},
			expected: []*NodeInfo{
				nodeInfo1,
				nodeInfo2,
				nodeInfo3,
			},
			delExpect: []*NodeInfo{
				nodeInfo2,
				nodeInfo3,
			},
		},
		{
			NodeList:    []string{"n1", "n2", "n3"},
			deletedNode: node3,
			nodes:       []*v1.Node{node1, node2, node3},
			expected: []*NodeInfo{
				nodeInfo1,
				nodeInfo2,
				nodeInfo3,
			},
			delExpect: []*NodeInfo{
				nodeInfo1,
				nodeInfo2,
			},
		},
	}

	for i, test := range tests {
		cache := NewOrderNodes()

		for j, name := range test.NodeList {
			cache.AddIfNotPresent(name, NewNodeInfo(test.nodes[j]))
		}

		got := cache.IterateList()
		if !reflect.DeepEqual(got, test.expected) {
			t.Errorf("case %d: \n expected %v, \n got %v \n",
				i, test.expected, got)
		}

		// delete node
		cache.Delete(test.deletedNode.Name)
		got = cache.IterateList()
		if !reflect.DeepEqual(got, test.delExpect) {
			t.Errorf("case %d: \n expected %v, \n got %v \n",
				i, test.delExpect, got)
		}
	}
}
