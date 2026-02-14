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
	"k8s.io/apimachinery/pkg/api/resource"
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
				node := api.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				})
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
				node1 := api.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				})
				snapshot.addOrUpdateNode(node1)

				// Update node with pods
				node1Updated := api.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				})

				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "test",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("100m"),
										v1.ResourceMemory: resource.MustParse("100Mi"),
									},
								},
							},
						},
					},
				}
				ti := api.NewTaskInfo(pod)
				node1Updated.AddTask(ti)

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

				node1 := api.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				})
				node2 := api.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-2",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("8"),
							v1.ResourceMemory: resource.MustParse("16Gi"),
						},
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("8"),
							v1.ResourceMemory: resource.MustParse("16Gi"),
						},
					},
				})
				nodes := []*api.NodeInfo{
					node1,
					node2,
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
						Containers: []v1.Container{
							{
								Name: "test",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("100m"),
										v1.ResourceMemory: resource.MustParse("100Mi"),
									},
								},
							},
						},
						Affinity: &v1.Affinity{
							PodAffinity: &v1.PodAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"app": "test",
											},
										},
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
					},
				}
				node := api.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
						Allocatable: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				})
				ti := api.NewTaskInfo(pod)
				node.AddTask(ti)

				return snapshot, []*api.NodeInfo{node}
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				return len(s.fwkInfo.havePodsWithAffinityNodeInfoList) == 1 &&
					s.fwkInfo.havePodsWithAffinityNodeInfoList[0].Node().Name == "node-1"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, nodes := tt.setup()
			if snapshot == nil || nodes == nil {
				t.Fatal("setup failed")
			}

			snapshot.AddOrUpdateNodes(nodes)

			if !tt.checkResult(snapshot) {
				t.Errorf("AddOrUpdateNodes() = unexpected result")
			}
		})
	}
}

func TestSnapshot_DeleteNode(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() (*Snapshot, string)
		wantErr     bool
		checkResult func(*Snapshot) bool
	}{
		{
			name: "delete existing node",
			setup: func() (*Snapshot, string) {
				snapshot := NewEmptySnapshot()
				node := api.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				})
				snapshot.addOrUpdateNode(node)
				return snapshot, "node-1"
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				return len(s.GetFwkNodeInfoList()) == 0 &&
					len(s.GetVolcanoNodeInfoList()) == 0 &&
					s.nodeNameToIndex["node-1"] == 0 &&
					len(s.nodeNameToIndex) == 0
			},
		},
		{
			name: "delete node with affinity",
			setup: func() (*Snapshot, string) {
				snapshot := NewEmptySnapshot()

				// Create pod with affinity
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-affinity",
						Namespace: "default",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "test",
								Resources: v1.ResourceRequirements{
									Requests: v1.ResourceList{
										v1.ResourceCPU:    resource.MustParse("100m"),
										v1.ResourceMemory: resource.MustParse("100Mi"),
									},
								},
							},
						},
						Affinity: &v1.Affinity{
							PodAffinity: &v1.PodAffinity{
								RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
									{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{
												"app": "test",
											},
										},
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
					},
				}

				node := api.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				})

				ti := api.NewTaskInfo(pod)
				node.AddTask(ti)

				snapshot.addOrUpdateNode(node)
				return snapshot, "node-1"
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				return len(s.fwkInfo.havePodsWithAffinityNodeInfoList) == 0 &&
					len(s.affinityNodeIndex) == 0
			},
		},
		{
			name: "delete middle node from multiple nodes",
			setup: func() (*Snapshot, string) {
				snapshot := NewEmptySnapshot()

				// Add 3 nodes
				for i := 1; i <= 3; i++ {
					node := api.NewNodeInfo(&v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("node-%d", i),
						},
						Status: v1.NodeStatus{
							Capacity: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
						},
					})
					snapshot.addOrUpdateNode(node)
				}
				return snapshot, "node-2"
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				// After deleting node-2, node-3 should move to index 1
				return len(s.GetFwkNodeInfoList()) == 2 &&
					s.nodeNameToIndex["node-1"] == 0 &&
					s.nodeNameToIndex["node-3"] == 1 &&
					s.GetFwkNodeInfoList()[0].Node().Name == "node-1" &&
					s.GetFwkNodeInfoList()[1].Node().Name == "node-3"
			},
		},
		{
			name: "delete non-existing node",
			setup: func() (*Snapshot, string) {
				snapshot := NewEmptySnapshot()
				node := api.NewNodeInfo(&v1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
					},
					Status: v1.NodeStatus{
						Capacity: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("4"),
							v1.ResourceMemory: resource.MustParse("8Gi"),
						},
					},
				})
				snapshot.addOrUpdateNode(node)
				return snapshot, "node-nonexistent"
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				// Should not affect existing node
				return len(s.GetFwkNodeInfoList()) == 1 &&
					s.GetFwkNodeInfoList()[0].Node().Name == "node-1"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, nodeName := tt.setup()
			if snapshot == nil {
				t.Fatal("setup failed")
			}

			snapshot.DeleteNode(nodeName)

			if !tt.checkResult(snapshot) {
				t.Errorf("DeleteNode() = unexpected result")
			}
		})
	}
}

func TestSnapshot_RemoveDeletedNodesFromSnapshot(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() (*Snapshot, map[string]bool)
		wantErr     bool
		checkResult func(*Snapshot) bool
	}{
		{
			name: "remove single deleted node",
			setup: func() (*Snapshot, map[string]bool) {
				snapshot := NewEmptySnapshot()

				// Add 3 nodes
				for i := 1; i <= 3; i++ {
					node := api.NewNodeInfo(&v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("node-%d", i),
						},
						Status: v1.NodeStatus{
							Capacity: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
						},
					})
					snapshot.addOrUpdateNode(node)
				}

				// Current nodes: node-1 and node-3 only
				currentNodes := map[string]bool{
					"node-1": true,
					"node-3": true,
				}
				return snapshot, currentNodes
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				return len(s.GetFwkNodeInfoList()) == 2 &&
					s.nodeNameToIndex["node-1"] == 0 &&
					s.nodeNameToIndex["node-3"] == 1 &&
					len(s.nodeNameToIndex) == 2
			},
		},
		{
			name: "remove multiple deleted nodes",
			setup: func() (*Snapshot, map[string]bool) {
				snapshot := NewEmptySnapshot()

				// Add 5 nodes
				for i := 1; i <= 5; i++ {
					node := api.NewNodeInfo(&v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("node-%d", i),
						},
						Status: v1.NodeStatus{
							Capacity: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
						},
					})
					snapshot.addOrUpdateNode(node)
				}

				// Keep only node-2 and node-4
				currentNodes := map[string]bool{
					"node-2": true,
					"node-4": true,
				}
				return snapshot, currentNodes
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				return len(s.GetFwkNodeInfoList()) == 2 &&
					s.nodeNameToIndex["node-2"] == 1 &&
					s.nodeNameToIndex["node-4"] == 0 &&
					len(s.nodeNameToIndex) == 2
			},
		},
		{
			name: "remove nodes with affinity",
			setup: func() (*Snapshot, map[string]bool) {
				snapshot := NewEmptySnapshot()

				// Add 3 nodes with affinity pods
				for i := 1; i <= 3; i++ {
					pod := &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("pod-%d", i),
							Namespace: "default",
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name: "test",
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											v1.ResourceCPU:    resource.MustParse("100m"),
											v1.ResourceMemory: resource.MustParse("100Mi"),
										},
									},
								},
							},
							Affinity: &v1.Affinity{
								PodAffinity: &v1.PodAffinity{
									RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
										{
											LabelSelector: &metav1.LabelSelector{
												MatchLabels: map[string]string{
													"app": "test",
												},
											},
											TopologyKey: "kubernetes.io/hostname",
										},
									},
								},
							},
						},
					}

					node := api.NewNodeInfo(&v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("node-%d", i),
						},
						Status: v1.NodeStatus{
							Capacity: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
						},
					})

					ti := api.NewTaskInfo(pod)
					node.AddTask(ti)

					snapshot.addOrUpdateNode(node)
				}

				// Keep only node-1
				currentNodes := map[string]bool{
					"node-1": true,
				}
				return snapshot, currentNodes
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				return len(s.GetFwkNodeInfoList()) == 1 &&
					len(s.fwkInfo.havePodsWithAffinityNodeInfoList) == 1 &&
					s.affinityNodeIndex["node-1"] == 0 &&
					len(s.affinityNodeIndex) == 1
			},
		},
		{
			name: "no nodes to remove",
			setup: func() (*Snapshot, map[string]bool) {
				snapshot := NewEmptySnapshot()

				// Add 2 nodes
				for i := 1; i <= 2; i++ {
					node := api.NewNodeInfo(&v1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name: fmt.Sprintf("node-%d", i),
						},
						Status: v1.NodeStatus{
							Capacity: v1.ResourceList{
								v1.ResourceCPU:    resource.MustParse("4"),
								v1.ResourceMemory: resource.MustParse("8Gi"),
							},
						},
					})
					snapshot.addOrUpdateNode(node)
				}

				// All nodes still exist
				currentNodes := map[string]bool{
					"node-1": true,
					"node-2": true,
				}
				return snapshot, currentNodes
			},
			wantErr: false,
			checkResult: func(s *Snapshot) bool {
				return len(s.GetFwkNodeInfoList()) == 2 &&
					len(s.nodeNameToIndex) == 2
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, currentNodeNames := tt.setup()
			if snapshot == nil {
				t.Fatal("setup failed")
			}

			snapshot.RemoveDeletedNodesFromSnapshot(currentNodeNames)

			if !tt.checkResult(snapshot) {
				t.Errorf("RemoveDeletedNodesFromSnapshot() = unexpected result")
			}
		})
	}
}

func TestUtilSwapDelete(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() (map[string]int, *[]fwk.NodeInfo, string)
		wantErr     bool
		checkResult func(map[string]int, *[]fwk.NodeInfo) bool
	}{
		{
			name: "delete first element from list",
			setup: func() (map[string]int, *[]fwk.NodeInfo, string) {
				indexMap := map[string]int{
					"node-1": 0,
					"node-2": 1,
					"node-3": 2,
				}

				list := &[]fwk.NodeInfo{
					framework.NewNodeInfo(),
					framework.NewNodeInfo(),
					framework.NewNodeInfo(),
				}
				(*list)[0].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
				(*list)[1].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}})
				(*list)[2].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-3"}})

				return indexMap, list, "node-1"
			},
			wantErr: false,
			checkResult: func(indexMap map[string]int, list *[]fwk.NodeInfo) bool {
				return len(*list) == 2 &&
					(*list)[0].Node().Name == "node-3" &&
					(*list)[1].Node().Name == "node-2" &&
					indexMap["node-3"] == 0 &&
					indexMap["node-2"] == 1 &&
					len(indexMap) == 2
			},
		},
		{
			name: "delete middle element from list",
			setup: func() (map[string]int, *[]fwk.NodeInfo, string) {
				indexMap := map[string]int{
					"node-1": 0,
					"node-2": 1,
					"node-3": 2,
				}

				list := &[]fwk.NodeInfo{
					framework.NewNodeInfo(),
					framework.NewNodeInfo(),
					framework.NewNodeInfo(),
				}
				(*list)[0].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
				(*list)[1].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}})
				(*list)[2].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-3"}})

				return indexMap, list, "node-2"
			},
			wantErr: false,
			checkResult: func(indexMap map[string]int, list *[]fwk.NodeInfo) bool {
				return len(*list) == 2 &&
					(*list)[0].Node().Name == "node-1" &&
					(*list)[1].Node().Name == "node-3" &&
					indexMap["node-1"] == 0 &&
					indexMap["node-3"] == 1 &&
					len(indexMap) == 2
			},
		},
		{
			name: "delete last element from list",
			setup: func() (map[string]int, *[]fwk.NodeInfo, string) {
				indexMap := map[string]int{
					"node-1": 0,
					"node-2": 1,
					"node-3": 2,
				}

				list := &[]fwk.NodeInfo{
					framework.NewNodeInfo(),
					framework.NewNodeInfo(),
					framework.NewNodeInfo(),
				}
				(*list)[0].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
				(*list)[1].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}})
				(*list)[2].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-3"}})

				return indexMap, list, "node-3"
			},
			wantErr: false,
			checkResult: func(indexMap map[string]int, list *[]fwk.NodeInfo) bool {
				return len(*list) == 2 &&
					(*list)[0].Node().Name == "node-1" &&
					(*list)[1].Node().Name == "node-2" &&
					indexMap["node-1"] == 0 &&
					indexMap["node-2"] == 1 &&
					len(indexMap) == 2
			},
		},
		{
			name: "delete non-existing element",
			setup: func() (map[string]int, *[]fwk.NodeInfo, string) {
				indexMap := map[string]int{
					"node-1": 0,
					"node-2": 1,
				}

				list := &[]fwk.NodeInfo{
					framework.NewNodeInfo(),
					framework.NewNodeInfo(),
				}
				(*list)[0].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})
				(*list)[1].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-2"}})

				return indexMap, list, "node-nonexistent"
			},
			wantErr: false,
			checkResult: func(indexMap map[string]int, list *[]fwk.NodeInfo) bool {
				return len(*list) == 2 &&
					indexMap["node-1"] == 0 &&
					indexMap["node-2"] == 1 &&
					len(indexMap) == 2
			},
		},
		{
			name: "delete single element list",
			setup: func() (map[string]int, *[]fwk.NodeInfo, string) {
				indexMap := map[string]int{
					"node-1": 0,
				}

				list := &[]fwk.NodeInfo{
					framework.NewNodeInfo(),
				}
				(*list)[0].SetNode(&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}})

				return indexMap, list, "node-1"
			},
			wantErr: false,
			checkResult: func(indexMap map[string]int, list *[]fwk.NodeInfo) bool {
				return len(*list) == 0 &&
					len(indexMap) == 0
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexMap, list, nodeName := tt.setup()
			if list == nil {
				t.Fatal("setup failed")
			}

			utilSwapDelete(indexMap, list, nodeName)

			if !tt.checkResult(indexMap, list) {
				t.Errorf("utilSwapDelete() = unexpected result")
			}
		})
	}
}
