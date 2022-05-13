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
				ResourceUsage:            &NodeUsage{},
				State:                    NodeState{Phase: Ready},
				Tasks: map[TaskID]*TaskInfo{
					"c1/p1": NewTaskInfo(case01Pod1),
					"c1/p2": NewTaskInfo(case01Pod2),
				},
				GPUDevices: make(map[int]*GPUDevice),
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
				ResourceUsage:            &NodeUsage{},
				State:                    NodeState{Phase: Ready},
				Tasks:                    map[TaskID]*TaskInfo{},
				GPUDevices:               make(map[int]*GPUDevice),
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
				ResourceUsage:            &NodeUsage{},
				State:                    NodeState{Phase: Ready},
				Tasks: map[TaskID]*TaskInfo{
					"c1/p1": NewTaskInfo(case01Pod1),
					"c1/p3": NewTaskInfo(case01Pod3),
				},
				GPUDevices: make(map[int]*GPUDevice),
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

func TestNodeInfo_SetNode(t *testing.T) {
	// case1
	case01Node1 := buildNode("n1", buildResourceList("10", "10G"))
	case01Node2 := buildNode("n1", buildResourceList("8", "8G"))
	case01Pod1 := buildPod("c1", "p1", "n1", v1.PodRunning, buildResourceList("1", "1G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("2", "2G"), []metav1.OwnerReference{}, make(map[string]string))
	case01Pod3 := buildPod("c1", "p3", "n1", v1.PodRunning, buildResourceList("6", "6G"), []metav1.OwnerReference{}, make(map[string]string))

	tests := []struct {
		name     string
		node     *v1.Node
		updated  *v1.Node
		pods     []*v1.Pod
		expected *NodeInfo
	}{
		{
			name:    "add 3 running non-owner pod",
			node:    case01Node1,
			updated: case01Node2,
			pods:    []*v1.Pod{case01Pod1, case01Pod2, case01Pod3},
			expected: &NodeInfo{
				Name:                     "n1",
				Node:                     case01Node1,
				Idle:                     buildResource("1", "1G"),
				Used:                     buildResource("9", "9G"),
				OversubscriptionResource: EmptyResource(),
				Releasing:                EmptyResource(),
				Pipelined:                EmptyResource(),
				Allocatable:              buildResource("10", "10G"),
				Capability:               buildResource("10", "10G"),
				ResourceUsage:            &NodeUsage{},
				State:                    NodeState{Phase: NotReady, Reason: "OutOfSync"},
				Tasks: map[TaskID]*TaskInfo{
					"c1/p1": NewTaskInfo(case01Pod1),
					"c1/p2": NewTaskInfo(case01Pod2),
					"c1/p3": NewTaskInfo(case01Pod3),
				},
				GPUDevices: make(map[int]*GPUDevice),
			},
		},
	}

	for i, test := range tests {
		ni := NewNodeInfo(test.node)
		for _, pod := range test.pods {
			pi := NewTaskInfo(pod)
			ni.AddTask(pi)
			ni.Name = pod.Spec.NodeName
		}

		// OutOfSync. e.g.: nvidia-device-plugin is down causes gpus turn from 8 to 0 (node.status.allocatable."nvidia.com/gpu": 0)
		ni.SetNode(test.updated)
		if !nodeInfoEqual(ni, test.expected) {
			t.Errorf("node info %d: \n expected\t%v, \n got\t\t%v \n",
				i, test.expected, ni)
		}

		// Recover. e.g.: nvidia-device-plugin is restarted successfully
		ni.SetNode(test.node)
		test.expected.State = NodeState{Phase: Ready}
		if !nodeInfoEqual(ni, test.expected) {
			t.Errorf("recovered %d: \n expected\t%v, \n got\t\t%v \n",
				i, test.expected, ni)
		}
	}
}
