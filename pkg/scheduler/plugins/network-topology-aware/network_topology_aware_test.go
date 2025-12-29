/*
Copyright 2025 The Volcano Authors.

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

package networktopologyaware

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	eps = 1e-1
)

func TestNew(t *testing.T) {
	tests := []struct {
		name           string
		arguments      framework.Arguments
		expectedPlugin *networkTopologyAwarePlugin
	}{
		{
			name:      "build plugin with no arguments",
			arguments: framework.Arguments{},
			expectedPlugin: &networkTopologyAwarePlugin{
				weight: &priorityWeight{
					GlobalWeight:                 1,
					HyperNodeBinPackingCPU:       1,
					HyperNodeBinPackingMemory:    1,
					HyperNodeBinPackingResources: map[corev1.ResourceName]int{},
				},
				normalPodConfig: &normalPodConfig{
					hyperNodeBinPackingEnable: true,
					hyperNodeBinPackingFading: 0.8,
				},
				hyperNodesTier:         &hyperNodesTier{},
				hyperNodeResourceCache: make(map[string]map[corev1.ResourceName]*resourceStatus),
			},
		},
		{
			name: "build plugin with customized valid arguments",
			arguments: framework.Arguments{
				"weight":                                        2,
				"hypernode.binpack.cpu":                         3,
				"hypernode.binpack.memory":                      4,
				"hypernode.binpack.resources":                   "nvidia.com/gpu, example.com/foo",
				"hypernode.binpack.resources.nvidia.com/gpuxxx": 5,
				"hypernode.binpack.resources.example.com/foo":   6,
				"hypernode.binpack.normal-pod.enable":           false,
				"hypernode.binpack.normal-pod.fading":           0,
			},
			expectedPlugin: &networkTopologyAwarePlugin{
				weight: &priorityWeight{
					GlobalWeight:              2,
					HyperNodeBinPackingCPU:    3,
					HyperNodeBinPackingMemory: 4,
					HyperNodeBinPackingResources: map[corev1.ResourceName]int{
						"nvidia.com/gpu":  1,
						"example.com/foo": 6,
					},
				},
				normalPodConfig: &normalPodConfig{
					hyperNodeBinPackingEnable: false,
					hyperNodeBinPackingFading: 0,
				},
				hyperNodesTier:         &hyperNodesTier{},
				hyperNodeResourceCache: make(map[string]map[corev1.ResourceName]*resourceStatus),
			},
		}, {
			name: "build plugin with customized invalid arguments",
			arguments: framework.Arguments{
				"weight":                                      -1,
				"hypernode.binpack.cpu":                       -1,
				"hypernode.binpack.memory":                    -1,
				"hypernode.binpack.resources":                 "nvidia.com/gpuxxx, example.com/foo",
				"hypernode.binpack.resources.nvidia.com/gpu":  -1,
				"hypernode.binpack.resources.example.com/foo": -1,
				"hypernode.binpack.normal-pod.enable":         "a",
				"hypernode.binpack.normal-pod.fading":         -1,
			},
			expectedPlugin: &networkTopologyAwarePlugin{
				weight: &priorityWeight{
					GlobalWeight:              1,
					HyperNodeBinPackingCPU:    1,
					HyperNodeBinPackingMemory: 1,
					HyperNodeBinPackingResources: map[corev1.ResourceName]int{
						"nvidia.com/gpuxxx": 1,
						"example.com/foo":   1,
					},
				},
				normalPodConfig: &normalPodConfig{
					hyperNodeBinPackingEnable: true,
					hyperNodeBinPackingFading: 0.8,
				},
				hyperNodesTier:         &hyperNodesTier{},
				hyperNodeResourceCache: make(map[string]map[corev1.ResourceName]*resourceStatus),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			framework.RegisterPluginBuilder(PluginName, New)
			defer framework.CleanupPluginBuilders()
			builder, ok := framework.GetPluginBuilder(PluginName)
			if !ok {
				t.Fatalf("should have plugin named %s", PluginName)
			}

			plugin := builder(tt.arguments)
			ntap, ok := plugin.(*networkTopologyAwarePlugin)
			if !ok {
				t.Fatalf("plugin should be %T, but not %T", ntap, plugin)
			}
			assert.Equal(t, tt.expectedPlugin.weight, ntap.weight, "the weight should be initialized properly")
			assert.Equal(t, tt.expectedPlugin.normalPodConfig, ntap.normalPodConfig, "the normalPodConfig should be initialized properly")
			assert.Equal(t, tt.expectedPlugin.hyperNodesTier, ntap.hyperNodesTier, "the hyperNodesTier should be initialized properly")
			assert.Equal(t, tt.expectedPlugin.hyperNodeResourceCache, ntap.hyperNodeResourceCache, "the hyperNodeResourceCache should be initialized properly")
		})
	}
}

func TestNetworkTopologyAwareNodeScore_Hard(t *testing.T) {
	tests := []struct {
		name string
		uthelper.TestCommonStruct
		arguments  framework.Arguments
		scoreNodes []*api.NodeInfo
		tasks      map[string]string
		expected   map[string]float64
	}{
		{
			name: "Tasks in job first scheduler, score all nodes zero",
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p4", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s3-n1",
				},
				{
					Name: "s4-n1",
				},
				{
					Name: "s5-n1",
				},
			},
			expected: map[string]float64{
				"s3-n1": 0.0,
				"s4-n1": 0.0,
				"s5-n1": 0.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score zero when the hyperNode of node has empty LCA hyperNode with jobAllocatedHyperNode",
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s5-n1",
				},
				{
					Name: "s6-n1",
				},
			},
			expected: map[string]float64{
				"s5-n1": 0.0,
				"s6-n1": 0.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score nodes according to node hypernode LCA hyperNode tier",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s3-n1",
				},
				{
					Name: "s4-n1",
				},
				{
					Name: "s5-n1",
				},
			},
			expected: map[string]float64{
				"s3-n1": 100.0,
				"s4-n1": 66.6,
				"s5-n1": 33.3,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier of the hyperNode and jobAllocatedHyperNode when hyperNodesInfo has two tier",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s1", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s1-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s1-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s1-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s2-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s2-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s1", "s2"),
					2: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 2, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
						{
							Name:     "s1-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s1-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 1, []api.MemberConfig{
						{
							Name:     "s2-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s2-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s1-n1", "s1-n2", "s2-n1", "s2-n2"),
					"s1": sets.New[string]("s1-n1", "s1-n2"),
					"s2": sets.New[string]("s2-n1", "s2-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s1-n1",
				},
				{
					Name: "s2-n1",
				},
			},
			expected: map[string]float64{
				"s1-n1": 100.0,
				"s2-n1": 50.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier of the hyperNode and jobAllocatedHyperNode when hyperNodesInfo has one tier",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s1", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s1-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s1-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s1-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s2-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s2-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s1", "s2"),
				},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
						{
							Name:     "s1-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s1-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 1, []api.MemberConfig{
						{
							Name:     "s2-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s2-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s1": sets.New[string]("s1-n1", "s1-n2"),
					"s2": sets.New[string]("s2-n1", "s2-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s1-n1",
				},
				{
					Name: "s2-n1",
				},
			},
			expected: map[string]float64{
				"s1-n1": 100.0,
				"s2-n1": 0.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier of the hyperNode and jobAllocatedHyperNode with plugin weight 2",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 2,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s3-n1",
				},
				{
					Name: "s4-n1",
				},
				{
					Name: "s5-n1",
				},
			},
			expected: map[string]float64{
				"s3-n1": 200.0,
				"s4-n1": 133.3,
				"s5-n1": 66.6,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier and task num of the hyperNode when there are at least two hyperNodes have max hyperNode tier score",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s1", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "s4-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p4", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s3-n1",
				},
				{
					Name: "s4-n1",
				},
				{
					Name: "s5-n1",
				},
			},
			tasks: map[string]string{
				"task1": "s3-n1",
				"task2": "s3-n2",
				"task3": "s4-n1",
				"test4": "",
			},
			expected: map[string]float64{
				"s3-n1": 116.6,
				"s4-n1": 91.6,
				"s5-n1": 33.3,
			},
		},
	}
	trueValue := true
	plugins := map[string]framework.PluginBuilder{
		PluginName: New,
	}

	for i, test := range tests {
		test.Plugins = plugins
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:                     PluginName,
						EnabledHyperNodeOrder:    &trueValue,
						EnabledNodeOrder:         &trueValue,
						EnabledHyperNodeGradient: &trueValue,
						Arguments:                test.arguments,
					},
				},
			},
		}
		// create session
		ssn := test.RegisterSession(tiers, nil)
		defer test.Close()

		nodeScores, err := ssn.BatchNodeOrderFn(parseTask(ssn.Jobs), test.scoreNodes)
		if err != nil {
			t.Errorf("case%d: task %s  has err %v", i, test.Name, err)
			continue
		}
		for node, expected := range test.expected {
			if math.Abs(nodeScores[node]-expected) > eps {
				t.Errorf("case%d: task %s on node %s expect have score %v, but get %v", i+1, test.name, node, expected, nodeScores[node])
			}
		}
	}
}

func TestNetworkTopologyAwareNodeScore_Soft(t *testing.T) {
	tests := []struct {
		name string
		uthelper.TestCommonStruct
		arguments  framework.Arguments
		scoreNodes []*api.NodeInfo
		tasks      map[string]string
		expected   map[string]float64
	}{
		{
			name: "Tasks in job first scheduler, score all nodes zero",
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p4", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s3-n1",
				},
				{
					Name: "s4-n1",
				},
				{
					Name: "s5-n1",
				},
			},
			expected: map[string]float64{
				"s3-n1": 0.0,
				"s4-n1": 0.0,
				"s5-n1": 0.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score zero when the hyperNode of node has empty LCA hyperNode with jobAllocatedHyperNode",
			TestCommonStruct: uthelper.TestCommonStruct{
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s5-n1",
				},
				{
					Name: "s6-n1",
				},
			},
			expected: map[string]float64{
				"s5-n1": 0.0,
				"s6-n1": 0.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score nodes according to node hypernode LCA hyperNode tier",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s3-n1",
				},
				{
					Name: "s4-n1",
				},
				{
					Name: "s5-n1",
				},
			},
			expected: map[string]float64{
				"s3-n1": 100.0,
				"s4-n1": 66.6,
				"s5-n1": 33.3,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier of the hyperNode and jobAllocatedHyperNode when hyperNodesInfo has two tier",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s1", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s1-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s1-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s1-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s2-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s2-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s1", "s2"),
					2: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 2, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
						{
							Name:     "s1-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s1-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 1, []api.MemberConfig{
						{
							Name:     "s2-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s2-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s1-n1", "s1-n2", "s2-n1", "s2-n2"),
					"s1": sets.New[string]("s1-n1", "s1-n2"),
					"s2": sets.New[string]("s2-n1", "s2-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s1-n1",
				},
				{
					Name: "s2-n1",
				},
			},
			expected: map[string]float64{
				"s1-n1": 100.0,
				"s2-n1": 50.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier of the hyperNode and jobAllocatedHyperNode when hyperNodesInfo has one tier",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s1", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s1-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s1-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s1-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s2-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s2-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s1", "s2"),
				},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
						{
							Name:     "s1-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s1-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 1, []api.MemberConfig{
						{
							Name:     "s2-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s2-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s1": sets.New[string]("s1-n1", "s1-n2"),
					"s2": sets.New[string]("s2-n1", "s2-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s1-n1",
				},
				{
					Name: "s2-n1",
				},
			},
			expected: map[string]float64{
				"s1-n1": 100.0,
				"s2-n1": 0.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier of the hyperNode and jobAllocatedHyperNode with plugin weight 2",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 2,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s3-n1",
				},
				{
					Name: "s4-n1",
				},
				{
					Name: "s5-n1",
				},
			},
			expected: map[string]float64{
				"s3-n1": 200.0,
				"s4-n1": 133.3,
				"s5-n1": 66.6,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier and task num of the hyperNode when there are at least two hyperNodes have max hyperNode tier score",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s1", "q1", 3, nil, schedulingv1.PodGroupInqueue, "soft", 0),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("c1", "p1", "s3-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
					util.BuildPod("c1", "p2", "s3-n2", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p3", "s4-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("c1", "p4", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight": 1,
			},
			scoreNodes: []*api.NodeInfo{
				{
					Name: "s3-n1",
				},
				{
					Name: "s4-n1",
				},
				{
					Name: "s5-n1",
				},
			},
			tasks: map[string]string{
				"task1": "s3-n1",
				"task2": "s3-n2",
				"task3": "s4-n1",
				"test4": "",
			},
			expected: map[string]float64{
				"s3-n1": 116.6,
				"s4-n1": 91.6,
				"s5-n1": 33.3,
			},
		},
	}
	trueValue := true
	plugins := map[string]framework.PluginBuilder{
		PluginName: New,
	}

	for i, test := range tests {
		test.Plugins = plugins
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:                     PluginName,
						EnabledHyperNodeOrder:    &trueValue,
						EnabledNodeOrder:         &trueValue,
						EnabledHyperNodeGradient: &trueValue,
						Arguments:                test.arguments,
					},
				},
			},
		}
		// create session
		ssn := test.RegisterSession(tiers, nil)
		defer test.Close()

		nodeScores, err := ssn.BatchNodeOrderFn(parseTask(ssn.Jobs), test.scoreNodes)
		if err != nil {
			t.Errorf("case%d: task %s  has err %v", i, test.Name, err)
			continue
		}
		for node, expected := range test.expected {
			if math.Abs(nodeScores[node]-expected) > eps {
				t.Errorf("case%d: task %s on node %s expect have score %v, but get %v", i+1, test.name, node, expected, nodeScores[node])
			}
		}
	}
}

func parseTask(jobInfoMap map[api.JobID]*api.JobInfo) *api.TaskInfo {
	var job *api.JobInfo
	for _, jobInfo := range jobInfoMap {
		job = jobInfo
	}
	if job == nil {
		return nil
	}
	jobAllocatedHyperNode := job.PodGroup.GetAnnotations()[api.JobAllocatedHyperNode]
	for _, task := range job.Tasks {
		if task.Pod.Status.Phase == corev1.PodPending {
			task.JobAllocatedHyperNode = jobAllocatedHyperNode
			return task
		}
	}
	return nil
}

func Test_batchNodeOrderFn(t *testing.T) {
	tests := []struct {
		name             string
		testCommonStruct uthelper.TestCommonStruct
		arguments        framework.Arguments
		expectedScores   map[string]float64
		expectErr        bool
	}{
		{
			name: "score empty nodes for single task with no network topology and no hypernode-level binpacking",
			testCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{
					PluginName: New,
				},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("ns1", "p1", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", nil, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"hypernode.binpack.normal-pod.enable": false,
			},
			expectedScores: map[string]float64{},
			expectErr:      false,
		}, {
			name: "score empty nodes for single task with no network topology and default plugin arguments",
			testCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{
					PluginName: New,
				},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("ns1", "p1", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", nil, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{},
			expectedScores: map[string]float64{
				"s3-n1": 25.8,
				"s3-n2": 25.8,
				"s4-n1": 21.6,
				"s4-n2": 21.6,
				"s5-n1": 19.9,
				"s5-n2": 19.9,
				"s6-n1": 15.7,
				"s6-n2": 15.7,
			},
			expectErr: false,
		},
		{
			name: "score empty nodes for single task with no network topology and customized plugin arguments",
			testCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{
					PluginName: New,
				},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("ns1", "p1", "", corev1.PodPending,
						api.BuildResourceList("2", "4G", api.ScalarResource{Name: "example.com/foo", Value: "8"}), "pg1", nil, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "8"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "8"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "8"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "8"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "16"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "16"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "16"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "16"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight":                                      10,
				"hypernode.binpack.cpu":                       1,
				"hypernode.binpack.memory":                    4,
				"hypernode.binpack.resources":                 "nvidia.com/gpu, example.com/foo",
				"hypernode.binpack.resources.nvidia.com/gpu":  2,
				"hypernode.binpack.resources.example.com/foo": 3,
				"hypernode.binpack.normal-pod.enable":         true,
				"hypernode.binpack.normal-pod.fading":         0,
			},
			expectedScores: map[string]float64{
				"s3-n1": 500.0,
				"s3-n2": 500.0,
				"s4-n1": 468.75,
				"s4-n2": 468.75,
				"s5-n1": 281.3,
				"s5-n2": 281.3,
				"s6-n1": 250.0,
				"s6-n2": 250.0,
			},
			expectErr: false,
		}, {
			name: "score non-empty nodes for single task with no network topology and customized plugin arguments",
			testCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{
					PluginName: New,
				},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("ns1", "p1", "", corev1.PodPending,
						api.BuildResourceList("2", "4G", api.ScalarResource{Name: "example.com/foo", Value: "8"}), "pg1", nil, nil),
					util.BuildPod("ns1", "p2", "s3-n1", corev1.PodRunning,
						api.BuildResourceList("2", "4G", api.ScalarResource{Name: "example.com/foo", Value: "8"}), "pg1", nil, nil),
					util.BuildPod("ns1", "p3", "s3-n2", corev1.PodRunning,
						api.BuildResourceList("2", "4G", api.ScalarResource{Name: "example.com/foo", Value: "8"}), "pg1", nil, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "8"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "8"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "8"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "8"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "16"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "16"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "16"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "16"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight":                                      10,
				"hypernode.binpack.cpu":                       1,
				"hypernode.binpack.memory":                    4,
				"hypernode.binpack.resources":                 "nvidia.com/gpu, example.com/foo",
				"hypernode.binpack.resources.nvidia.com/gpu":  2,
				"hypernode.binpack.resources.example.com/foo": 3,
				"hypernode.binpack.normal-pod.enable":         true,
				"hypernode.binpack.normal-pod.fading":         0,
			},
			expectedScores: map[string]float64{
				"s3-n1": 0.0,
				"s3-n2": 0.0,
				"s4-n1": 468.8,
				"s4-n2": 468.8,
				"s5-n1": 281.3,
				"s5-n2": 281.3,
				"s6-n1": 250.0,
				"s6-n2": 250.0,
			},
			expectErr: false,
		}, {
			name: "score nodes for tasks with network topology",
			testCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{
					PluginName: New,
				},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithSubGroupPolicy("pg1", "ns1", "s1", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2,
						[]schedulingv1.SubGroupPolicySpec{
							util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 2),
						}),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("ns1", "p1", "", corev1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("ns1", "p2", "s4-n1", corev1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{},
			expectedScores: map[string]float64{
				"s3-n1": 66.7,
				"s3-n2": 66.7,
				"s4-n1": 116.7,
				"s4-n2": 116.7,
				"s5-n1": 33.3,
				"s5-n2": 33.3,
				"s6-n1": 33.3,
				"s6-n2": 33.3,
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trueValue := true
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:             PluginName,
							EnabledNodeOrder: &trueValue,
							Arguments:        tt.arguments,
						},
					},
				},
			}

			ssn := tt.testCommonStruct.RegisterSession(tiers, nil)
			defer tt.testCommonStruct.Close()

			task := parseTask(ssn.Jobs)
			actualScores, actualErr := ssn.BatchNodeOrderFn(task, ssn.NodeList)
			if tt.expectErr {
				assert.NotNil(t, actualErr, fmt.Sprintf("task %s expect to get an error, but got nil", task.Name))
				return
			}
			assert.Equal(t, len(tt.expectedScores), len(actualScores), fmt.Sprintf("task %s expect to get the same number of node scores", task.Name))
			for node, expectedScore := range tt.expectedScores {
				if math.Abs(actualScores[node]-expectedScore) > eps {
					t.Errorf("task %s on node %s expect have score %v, but got %v", task.Name, node, expectedScore, actualScores[node])
				}
			}
		})
	}
}

func Test_HyperNodeOrderFn(t *testing.T) {
	tests := []struct {
		name             string
		testCommonStruct uthelper.TestCommonStruct
		arguments        framework.Arguments
		expectedScores   map[string]float64
		expectErr        bool
	}{
		{
			name: "score hypernodes for a subjob with default plugin arguments",
			testCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{
					PluginName: New,
				},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithSubGroupPolicy("pg1", "ns1", "", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2,
						[]schedulingv1.SubGroupPolicySpec{
							util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "", 0),
						}),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("ns1", "p1", "", corev1.PodPending, api.BuildResourceList("4", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("ns1", "p2", "", corev1.PodPending, api.BuildResourceList("4", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{},
			expectedScores: map[string]float64{
				"s0":                          25.0,
				"s1":                          58.3,
				"s2":                          45.8,
				"s3":                          0.0,
				"s4":                          100.0,
				"s5":                          0.0,
				"s6":                          75.0,
				framework.ClusterTopHyperNode: 25.0,
			},
			expectErr: false,
		}, {
			name: "score hypernodes for a subjob with customized plugin arguments",
			testCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{
					PluginName: New,
				},
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroupWithSubGroupPolicy("pg1", "ns1", "", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2,
						[]schedulingv1.SubGroupPolicySpec{
							util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "", 0),
						}),
				},
				Pods: []*corev1.Pod{
					util.BuildPod("ns1", "p1", "", corev1.PodPending, api.BuildResourceList("4", "4G", api.ScalarResource{Name: "example.com/foo", Value: "8"}),
						"pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
					util.BuildPod("ns1", "p2", "", corev1.PodPending, api.BuildResourceList("4", "4G", api.ScalarResource{Name: "example.com/foo", Value: "8"}),
						"pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("s3-n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "8"}}...), nil),
					util.BuildNode("s3-n2", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "0"}}...), nil),
					util.BuildNode("s4-n1", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "8"}}...), nil),
					util.BuildNode("s4-n2", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "0"}}...), nil),
					util.BuildNode("s5-n1", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "16"}}...), nil),
					util.BuildNode("s5-n2", api.BuildResourceList("2", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "0"}}...), nil),
					util.BuildNode("s6-n1", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "16"}}...), nil),
					util.BuildNode("s6-n2", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "example.com/foo", Value: "0"}}...), nil),
				},
				HyperNodesSetByTier: map[int]sets.Set[string]{
					1: sets.New[string]("s3", "s4", "s5", "s6"),
					2: sets.New[string]("s1", "s2"),
					3: sets.New[string]("s0")},
				HyperNodesMap: map[string]*api.HyperNodeInfo{
					"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 3, []api.MemberConfig{
						{
							Name:     "s1",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s2",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 2, []api.MemberConfig{
						{
							Name:     "s3",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s4",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
						{
							Name:     "s5",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
						{
							Name:     "s6",
							Type:     topologyv1alpha1.MemberTypeHyperNode,
							Selector: "exact",
						},
					})),
					"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 1, []api.MemberConfig{
						{
							Name:     "s3-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s3-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s4": api.NewHyperNodeInfo(api.BuildHyperNode("s4", 1, []api.MemberConfig{
						{
							Name:     "s4-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s4-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s5": api.NewHyperNodeInfo(api.BuildHyperNode("s5", 1, []api.MemberConfig{
						{
							Name:     "s5-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s5-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
					"s6": api.NewHyperNodeInfo(api.BuildHyperNode("s6", 1, []api.MemberConfig{
						{
							Name:     "s6-n1",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
						{
							Name:     "s6-n2",
							Type:     topologyv1alpha1.MemberTypeNode,
							Selector: "exact",
						},
					})),
				},
				HyperNodes: map[string]sets.Set[string]{
					"s0": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2", "s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s1": sets.New[string]("s3-n1", "s3-n2", "s4-n1", "s4-n2"),
					"s2": sets.New[string]("s5-n1", "s5-n2", "s6-n1", "s6-n2"),
					"s3": sets.New[string]("s3-n1", "s3-n2"),
					"s4": sets.New[string]("s4-n1", "s4-n2"),
					"s5": sets.New[string]("s5-n1", "s5-n2"),
					"s6": sets.New[string]("s6-n1", "s6-n2"),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			},
			arguments: framework.Arguments{
				"weight":                                      10,
				"hypernode.binpack.cpu":                       3,
				"hypernode.binpack.memory":                    4,
				"hypernode.binpack.resources":                 "nvidia.com/gpu, example.com/foo",
				"hypernode.binpack.resources.nvidia.com/gpu":  2,
				"hypernode.binpack.resources.example.com/foo": 3,
				"hypernode.binpack.normal-pod.enable":         true,
				"hypernode.binpack.normal-pod.fading":         0,
			},
			expectedScores: map[string]float64{
				"s0":                          266.7,
				"s1":                          700.0,
				"s2":                          450.0,
				"s3":                          0.0,
				"s4":                          0.0,
				"s5":                          0.0,
				"s6":                          800.0,
				framework.ClusterTopHyperNode: 266.7,
			},
			expectErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trueValue := true
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:                  PluginName,
							EnabledHyperNodeOrder: &trueValue,
							Arguments:             tt.arguments,
						},
					},
				},
			}

			ssn := tt.testCommonStruct.RegisterSession(tiers, nil)
			defer tt.testCommonStruct.Close()

			subJob := parseSubJob(ssn.Jobs)
			actualScores, actualErr := ssn.HyperNodeOrderMapFn(subJob, ssn.RealNodesList)
			if tt.expectErr {
				assert.NotNil(t, actualErr, fmt.Sprintf("subjob %s expect to get an error, but got nil", subJob.UID))
				return
			}
			assert.Equal(t, len(tt.expectedScores), len(actualScores[PluginName]), fmt.Sprintf("subjob %s expect to get the same number of hypernode scores", subJob.UID))
			for hypernode, expectedScore := range tt.expectedScores {
				if math.Abs(actualScores[PluginName][hypernode]-expectedScore) > eps {
					t.Errorf("subjob %s on hypernode %s expect have score %v, but got %v", subJob.UID, hypernode, expectedScore, actualScores[PluginName][hypernode])
				}
			}
		})
	}
}

func parseSubJob(jobInfoMap map[api.JobID]*api.JobInfo) *api.SubJobInfo {
	var job *api.JobInfo
	for _, jobInfo := range jobInfoMap {
		job = jobInfo
	}
	if job == nil {
		return nil
	}
	for _, subJob := range job.SubJobs {
		if subJob.UID == "ns1/pg1/task1-worker" {
			return subJob
		}
	}
	return nil
}
