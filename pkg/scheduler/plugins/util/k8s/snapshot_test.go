/*
Copyright 2024 The Volcano Authors.

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

package k8s

import (
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestSnapshot(t *testing.T) {
	var (
		nodeName = "test-node"
		pod1     = util.BuildPod("node_info_cache_test", "test-1", nodeName,
			v1.PodRunning, api.BuildResourceList("200m", "1Ki"), "pg2",
			make(map[string]string), make(map[string]string))
		pod2 = util.BuildPod("node_info_cache_test", "test-2", nodeName,
			v1.PodRunning, api.BuildResourceList("200m", "1Ki"), "pg2",
			map[string]string{"test": "test"}, make(map[string]string))
	)

	nodeInfo := framework.NewNodeInfo(pod1, pod2)

	tests := []struct {
		name                                          string
		nodeInfoMap                                   map[string]*framework.NodeInfo
		expectedNodeInfos                             []fwk.NodeInfo
		expectedPodsWithAffinityNodeInfos             []fwk.NodeInfo
		expectedPodsWithRequiredAntiAffinityNodeInfos []fwk.NodeInfo
		expectedPods                                  []*v1.Pod
		expectErr                                     error
	}{
		{
			name:                              "test snapshot operation",
			nodeInfoMap:                       map[string]*framework.NodeInfo{nodeName: nodeInfo},
			expectedNodeInfos:                 []fwk.NodeInfo{nodeInfo.Snapshot()},
			expectedPodsWithAffinityNodeInfos: []fwk.NodeInfo{}, // pods don't have affinity
			expectedPodsWithRequiredAntiAffinityNodeInfos: []fwk.NodeInfo{}, // pods don't have anti-affinity
			expectedPods: []*v1.Pod{pod2},
			expectErr:    fmt.Errorf("nodeinfo not found for node name %q", nodeName),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := NewSnapshot(util.ConvertNodeInfoSliceToInterface(tc.nodeInfoMap))
			nodeInfoList, err := snapshot.List()
			if !reflect.DeepEqual(nodeInfoList, tc.expectedNodeInfos) || err != nil {
				t.Fatalf("unexpected list nodeInfos value\ngot: %+v\nwant: %+v\nerr: %s", nodeInfoList, tc.expectedNodeInfos, err)
			}

			_, err = snapshot.Get(nodeName)
			if !reflect.DeepEqual(tc.expectErr, err) {
				t.Fatalf("unexpected get nodeInfos by nodeName value (+got: %T/-want: %T)", err, tc.expectErr)
			}

			nodeInfoList, err = snapshot.HavePodsWithAffinityList()
			if !reflect.DeepEqual(tc.expectedPodsWithAffinityNodeInfos, nodeInfoList) || err != nil {
				t.Fatalf("unexpected havePodsWithAffinityNodeInfoList\ngot:  %+v\nwant: %+v\nerr: %v", nodeInfoList, tc.expectedPodsWithAffinityNodeInfos, err)
			}

			nodeInfoList, err = snapshot.HavePodsWithRequiredAntiAffinityList()
			if !reflect.DeepEqual(tc.expectedPodsWithRequiredAntiAffinityNodeInfos, nodeInfoList) || err != nil {
				t.Fatalf("unexpected havePodsWithRequiredAntiAffinityNodeInfoList\ngot:  %+v\nwant: %+v\nerr: %v", nodeInfoList, tc.expectedPodsWithRequiredAntiAffinityNodeInfos, err)
			}

			sel, _ := labels.Parse("test==test")
			pods, err := snapshot.Pods().List(sel)
			if !reflect.DeepEqual(tc.expectedPods, pods) || err != nil {
				t.Fatalf("unexpected list pods value\ngot:  %+v\nwant: %+v\nerr: %v", pods, tc.expectedPods, err)
			}

			pods, err = snapshot.Pods().FilteredList(func(pod *v1.Pod) bool {
				return true
			}, sel)
			if !reflect.DeepEqual(tc.expectedPods, pods) || err != nil {
				t.Fatalf("unexpected list filtered pods value\ngot:  %+v\nwant: %+v\nerr: %v", pods, tc.expectedPods, err)
			}

			nodeInfos, err := snapshot.NodeInfos().List()
			if !reflect.DeepEqual(tc.expectedNodeInfos, nodeInfos) || err != nil {
				t.Fatalf("unexpected NodeInfos list value\ngot:  %+v\nwant: %+v\nerr: %v", nodeInfos, tc.expectedNodeInfos, err)
			}
		})
	}
}

func TestSnapshot_AddOrUpdateNodes(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() (*Snapshot, []*api.NodeInfo)
		wantErr     bool
		checkResult func(*Snapshot) bool
	}{
		{
			name: "add new node",
			setup: func() (*Snapshot, []*api.NodeInfo) {
				snapshot := NewEmptySnapshot()
				node := &api.NodeInfo{
					Name: "node-1",
					Node: &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
						},
					},
				}
				return snapshot, []*api.NodeInfo{node}
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				return len(s.GetFwkNodeInfoList()) == 1 &&
					s.GetFwkNodeInfoList()[0].Node().Name == "node-1" &&
					len(s.GetVolcanoNodeInfoList()) == 1 &&
					s.GetVolcanoNodeInfoList()[0].Name == "node-1"
			},
		},
		{
			name: "update existing node",
			setup: func() (*Snapshot, []*api.NodeInfo) {
				snapshot := NewEmptySnapshot()
				// Add initial node
				node1 := &api.NodeInfo{
					Name: "node-1",
					Node: &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
						},
					},
				}
				snapshot.addOrUpdateNode(node1)

				// Update node with pods
				node1Updated := &api.NodeInfo{
					Name: "node-1",
					Node: &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
						},
					},
				}
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "default",
					},
				}
				ti := api.NewTaskInfo(pod)
				err := node1Updated.AddTask(ti)
				if err != nil {
					return nil, nil
				}

				return snapshot, []*api.NodeInfo{node1Updated}
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				if len(s.volcanoInfo.nodeInfoList) != 1 {
					return false
				}
				nodeInfo, err := s.GetVolcanoNodeInfo("node-1")
				if err != nil {
					return false
				}
				return len(nodeInfo.Pods()) == 1
			},
		},
		{
			name: "add multiple nodes",
			setup: func() (*Snapshot, []*api.NodeInfo) {
				snapshot := NewEmptySnapshot()
				nodes := []*api.NodeInfo{
					{
						Name: "node-1",
						Node: &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "node-1",
							},
						},
					},
					{
						Name: "node-2",
						Node: &v1.Node{
							ObjectMeta: metav1.ObjectMeta{
								Name: "node-2",
							},
						},
					},
				}
				return snapshot, nodes
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				return len(s.GetFwkNodeInfoList()) == 2 &&
					len(s.GetVolcanoNodeInfoList()) == 2 &&
					s.nodeNameToIndex["node-1"] == 0 &&
					s.nodeNameToIndex["node-2"] == 1
			},
		},
		{
			name: "node with pods having affinity",
			setup: func() (*Snapshot, []*api.NodeInfo) {
				snapshot := NewEmptySnapshot()

				// Create a pod with affinity
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-with-affinity",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						Affinity: &v1.Affinity{
							NodeAffinity: &v1.NodeAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
									NodeSelectorTerms: []v1.NodeSelectorTerm{
										{
											MatchExpressions: []v1.NodeSelectorRequirement{
												{
													Key:      "kubernetes.io/hostname",
													Operator: v1.NodeSelectorOpIn,
													Values:   []string{"node-1"},
												},
											},
										},
									},
								},
							},
						},
					},
				}

				node := &api.NodeInfo{
					Name: "node-1",
					Node: &v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: "node-1",
						},
					},
				}
				ti := api.NewTaskInfo(pod)
				err := node.AddTask(ti)
				if err != nil {
					return nil, nil
				}

				return snapshot, []*api.NodeInfo{node}
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				return len(s.havePodsWithAffinityNodeInfoList) == 1 &&
					s.havePodsWithAffinityNodeInfoList[0].Node().Name == "node-1"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, nodes := tt.setup()

			snapshot.AddOrUpdateNodes(nodes)

			if !tt.checkResult(snapshot) {
				t.Errorf("AddOrUpdateNodes() = unexpected result")
			}
		})
	}
}
