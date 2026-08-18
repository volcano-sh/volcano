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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/record"

	scheduling "volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
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
					GlobalWeight:                 DefaultWeight,
					HyperNodeBinPackingCPU:       DefaultWeight,
					HyperNodeBinPackingMemory:    DefaultWeight,
					HyperNodeBinPackingResources: map[corev1.ResourceName]int{},
				},
				normalPodConfig: &normalPodConfig{
					hyperNodeBinPackingEnable: DefaultNormalPodEnable,
					hyperNodeBinPackingFading: DefaultNormalPodFading,
				},
				hyperNodesTier:         &hyperNodesTier{},
				hyperNodeResourceCache: make(map[string]*resourceStatus),
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
				hyperNodeResourceCache: make(map[string]*resourceStatus),
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
					GlobalWeight:              DefaultWeight,
					HyperNodeBinPackingCPU:    DefaultWeight,
					HyperNodeBinPackingMemory: DefaultWeight,
					HyperNodeBinPackingResources: map[corev1.ResourceName]int{
						"nvidia.com/gpuxxx": DefaultWeight,
						"example.com/foo":   DefaultWeight,
					},
				},
				normalPodConfig: &normalPodConfig{
					hyperNodeBinPackingEnable: DefaultNormalPodEnable,
					hyperNodeBinPackingFading: DefaultNormalPodFading,
				},
				hyperNodesTier:         &hyperNodesTier{},
				hyperNodeResourceCache: make(map[string]*resourceStatus),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			framework.RegisterPluginBuilder(PluginName, New)
			defer framework.CleanupPluginBuilders()
			builder, ok := framework.GetPluginBuilder(PluginName)
			if !ok {
				t.Errorf("expected to get a plugin named %s, but failed", PluginName)
				return
			}

			plugin := builder(tt.arguments)
			nta, ok := plugin.(*networkTopologyAwarePlugin)
			if !ok {
				t.Errorf("expected to get a plugin of type %T, but got %T", nta, plugin)
				return
			}
			assert.Equal(t, tt.expectedPlugin.weight, nta.weight)
			assert.Equal(t, tt.expectedPlugin.normalPodConfig, nta.normalPodConfig)
			assert.Equal(t, tt.expectedPlugin.hyperNodesTier, nta.hyperNodesTier)
			assert.Equal(t, tt.expectedPlugin.hyperNodeResourceCache, nta.hyperNodeResourceCache)
		})
	}
}

func TestValidateTopologyConstraintRejectsMissingBoundary(t *testing.T) {
	topology := &scheduling.NetworkTopologySpec{Mode: scheduling.HardNetworkTopologyMode}
	require.EqualError(t, validateTopologyConstraint(topology), "network topology constraint has no tier boundary")
}

func TestEmptyHardTopologyConstraintFailsClosedAtHooks(t *testing.T) {
	schedulerCache := &cache.SchedulerCache{
		Nodes:             map[string]*api.NodeInfo{},
		Jobs:              map[api.JobID]*api.JobInfo{},
		Queues:            map[api.QueueID]*api.QueueInfo{},
		HyperNodesInfo:    api.NewHyperNodesInfo(nil),
		InUseNodesInShard: sets.Set[string]{},
		StatusUpdater:     &util.FakeStatusUpdater{},
		Recorder:          record.NewFakeRecorder(100),
	}
	ssn := framework.OpenSession(schedulerCache, nil, nil)
	defer framework.CloseSession(ssn)

	root := api.NewHyperNodeInfo(api.BuildHyperNode("root", 1, nil))
	ssn.HyperNodes = api.HyperNodeInfoMap{root.Name: root}
	enabled := true
	ssn.Tiers = []conf.Tier{{Plugins: []conf.PluginOption{{
		Name:                     PluginName,
		EnabledHyperNodeGradient: &enabled,
	}}}}

	plugin, ok := New(framework.Arguments{}).(*networkTopologyAwarePlugin)
	require.True(t, ok)
	plugin.OnSessionOpen(ssn)

	emptyHard := &scheduling.NetworkTopologySpec{Mode: scheduling.HardNetworkTopologyMode}
	job := api.NewJobInfo(api.JobID("empty-hard-job"))
	job.NetworkTopology = emptyHard.DeepCopy()
	assert.Nil(t, ssn.HyperNodeGradientForJobFn(job, root, api.PurposeAllocate),
		"invalid Hard Job topology must not fall back to an unconstrained gradient")

	policy := &scheduling.SubGroupPolicySpec{NetworkTopology: emptyHard.DeepCopy()}
	subJob := api.NewSubJobInfo("group", "empty-hard-subjob", job.UID, policy, nil)
	assert.Nil(t, ssn.HyperNodeGradientForSubJobFn(subJob, root, api.PurposeAllocate),
		"invalid Hard SubJob topology must not fall back to an unconstrained gradient")
}

func TestReverseAndCapEvictionGradients(t *testing.T) {
	plugin := &networkTopologyAwarePlugin{maxHyperNodesForEviction: 3}
	hn := func(name string) *api.HyperNodeInfo { return &api.HyperNodeInfo{Name: name} }
	gradients := [][]*api.HyperNodeInfo{
		{hn("a"), hn("b")},
		{hn("c"), hn("d")},
	}

	evictResult := plugin.reverseAndCapEvictionGradients(gradients)
	assert.Equal(t, 2, len(evictResult))
	assert.Equal(t, []string{"c", "d"}, []string{evictResult[0][0].Name, evictResult[0][1].Name})
	assert.Equal(t, []string{"b"}, []string{evictResult[1][0].Name})

	noLimitPlugin := &networkTopologyAwarePlugin{maxHyperNodesForEviction: 0}
	assert.Equal(t, gradients, noLimitPlugin.reverseAndCapEvictionGradients(gradients))
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
			t.Errorf("case%d: task %s has err %v", i, test.Name, err)
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

func TestBatchNodeOrderFn(t *testing.T) {
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
				assert.NotNil(t, actualErr, fmt.Sprintf("task %s expects to get an error, but got nil", task.Name))
				return
			}
			if actualErr != nil {
				t.Errorf("task %s expects to get no error, but got %v", task.Name, actualErr)
				return
			}
			assert.Equal(t, len(tt.expectedScores), len(actualScores), fmt.Sprintf("task %s expects to get the same number of node scores", task.Name))
			for node, expectedScore := range tt.expectedScores {
				if math.Abs(actualScores[node]-expectedScore) > eps {
					t.Errorf("task %s on node %s expects to get score %v, but got %v", task.Name, node, expectedScore, actualScores[node])
				}
			}
		})
	}
}

func TestHyperNodeOrderFn(t *testing.T) {
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
				assert.NotNil(t, actualErr, fmt.Sprintf("subjob %s expects to get an error, but got nil", subJob.UID))
				return
			}
			if actualErr != nil {
				t.Errorf("subjob %s expects to get no error, but got %v", subJob.UID, actualErr)
				return
			}
			assert.Equal(t, len(tt.expectedScores), len(actualScores[PluginName]), fmt.Sprintf("subjob %s expects to get the same number of hypernode scores", subJob.UID))
			for hypernode, expectedScore := range tt.expectedScores {
				if math.Abs(actualScores[PluginName][hypernode]-expectedScore) > eps {
					t.Errorf("subjob %s on hypernode %s expects to get score %v, but got %v", subJob.UID, hypernode, expectedScore, actualScores[PluginName][hypernode])
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

func Test_initHyperNodeResourceCache(t *testing.T) {
	tests := []struct {
		name             string
		nodeNumber       int
		hyperNodeNumbers []int
		arguments        framework.Arguments
		expectedCache    map[string]*resourceStatus
	}{
		{
			name:             "evaluate the time efficiency of the initHyperNodeResourceCache function in large scale",
			nodeNumber:       2000,
			hyperNodeNumbers: []int{200, 40, 5, 1},
			arguments: framework.Arguments{
				"weight":                                      10,
				"hypernode.binpack.cpu":                       5,
				"hypernode.binpack.memory":                    5,
				"hypernode.binpack.resources":                 "example.com/foo",
				"hypernode.binpack.resources.example.com/foo": 5,
				"hypernode.binpack.normal-pod.enable":         true,
				"hypernode.binpack.normal-pod.fading":         0.8,
			},
			expectedCache: map[string]*resourceStatus{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := networkTopologyAwarePlugin{
				pluginArguments:        tt.arguments,
				weight:                 getPriorityWeight(tt.arguments),
				normalPodConfig:        getNormalPodConfig(tt.arguments),
				hyperNodesTier:         &hyperNodesTier{},
				hyperNodeResourceCache: make(map[string]*resourceStatus),
			}
			ssn := &framework.Session{}

			nodes := make(map[string]*api.NodeInfo)
			for i := 0; i < tt.nodeNumber; i++ {
				nodes[fmt.Sprintf("node-%d", i)] = &api.NodeInfo{
					Name: fmt.Sprintf("node-%d", i),
					Allocatable: &api.Resource{
						MilliCPU: 100000,
						Memory:   8000,
						ScalarResources: map[corev1.ResourceName]float64{
							"example.com/foo": 10,
						},
					},
					Used: &api.Resource{
						MilliCPU: 50000,
						Memory:   4000,
						ScalarResources: map[corev1.ResourceName]float64{
							"example.com/foo": 5,
						},
					},
					Idle: &api.Resource{
						MilliCPU: 50000,
						Memory:   4000,
						ScalarResources: map[corev1.ResourceName]float64{
							"example.com/foo": 5,
						},
					},
					Releasing: api.EmptyResource(),
					Pipelined: api.EmptyResource(),
				}
			}
			ssn.Nodes = nodes

			hyperNodes := make(map[string]*api.HyperNodeInfo)
			hyperNodesSetByTier := make(map[int]sets.Set[string])
			for tier := 1; tier <= len(tt.hyperNodeNumbers); tier++ {
				hyperNodeSet := make(sets.Set[string])
				for index := 0; index < tt.hyperNodeNumbers[tier-1]; index++ {
					hyperNode := fmt.Sprintf("hypernode-tier-%d-index-%d", tier, index)
					hyperNodes[hyperNode] = &api.HyperNodeInfo{}
					hyperNodeSet.Insert(hyperNode)

					tt.expectedCache[hyperNode] = &resourceStatus{
						allocatable: &api.Resource{
							MilliCPU: float64(tt.nodeNumber/tt.hyperNodeNumbers[tier-1]) * 100000,
							Memory:   float64(tt.nodeNumber/tt.hyperNodeNumbers[tier-1]) * 8000,
							ScalarResources: map[corev1.ResourceName]float64{
								corev1.ResourceName("example.com/foo"): float64(tt.nodeNumber/tt.hyperNodeNumbers[tier-1]) * 10,
							},
						},
						used: &api.Resource{
							MilliCPU: float64(tt.nodeNumber/tt.hyperNodeNumbers[tier-1]) * 50000,
							Memory:   float64(tt.nodeNumber/tt.hyperNodeNumbers[tier-1]) * 4000,
							ScalarResources: map[corev1.ResourceName]float64{
								corev1.ResourceName("example.com/foo"): float64(tt.nodeNumber/tt.hyperNodeNumbers[tier-1]) * 5,
							},
						},
					}
				}
				hyperNodesSetByTier[tier] = hyperNodeSet
			}
			ssn.HyperNodes = hyperNodes
			ssn.HyperNodesSetByTier = hyperNodesSetByTier

			realNodeSet := make(map[string]sets.Set[string])
			for tier := 1; tier <= len(tt.hyperNodeNumbers); tier++ {
				for hyperNodeIndex := 0; hyperNodeIndex < tt.hyperNodeNumbers[tier-1]; hyperNodeIndex++ {
					realNodeSet[fmt.Sprintf("hypernode-tier-%d-index-%d", tier, hyperNodeIndex)] = make(sets.Set[string])
				}
				for i := 0; i < tt.nodeNumber; i++ {
					realNodeSet[fmt.Sprintf("hypernode-tier-%d-index-%d", tier, i%tt.hyperNodeNumbers[tier-1])].Insert(fmt.Sprintf("node-%d", i))
				}
			}
			ssn.RealNodesSet = realNodeSet

			start := time.Now()
			plugin.initHyperNodeResourceCache(ssn)
			elapsed := time.Since(start)

			for hyperNode := range plugin.hyperNodeResourceCache {
				for _, resource := range []corev1.ResourceName{corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourceName("example.com/foo")} {
					assert.Equal(t, tt.expectedCache[hyperNode].allocatable.Get(resource), plugin.hyperNodeResourceCache[hyperNode].allocatable.Get(resource))
					assert.Equal(t, tt.expectedCache[hyperNode].used.Get(resource), plugin.hyperNodeResourceCache[hyperNode].used.Get(resource))
				}
			}

			fmt.Printf("The time cost for invoking the initHyperNodeResourceCache function is %v.\n", elapsed)
		})
	}
}

func Test_batchNodeOrderFnForNormalPods(t *testing.T) {
	tests := []struct {
		name             string
		nodeNumber       int
		hyperNodeNumbers []int
		arguments        framework.Arguments
		expectedScores   map[string]float64
		expectErr        bool
	}{
		{
			name:             "evaluate the time efficiency of the batchNodeOrderFnForNormalPods function in large scale",
			nodeNumber:       2000,
			hyperNodeNumbers: []int{200, 40, 5, 1},
			arguments: framework.Arguments{
				"weight":                                      10,
				"hypernode.binpack.cpu":                       5,
				"hypernode.binpack.memory":                    5,
				"hypernode.binpack.resources":                 "example.com/foo",
				"hypernode.binpack.resources.example.com/foo": 5,
				"hypernode.binpack.normal-pod.enable":         true,
				"hypernode.binpack.normal-pod.fading":         1,
			},
			expectedScores: make(map[string]float64),
			expectErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := networkTopologyAwarePlugin{
				pluginArguments:        tt.arguments,
				weight:                 getPriorityWeight(tt.arguments),
				normalPodConfig:        getNormalPodConfig(tt.arguments),
				hyperNodesTier:         &hyperNodesTier{},
				hyperNodeResourceCache: make(map[string]*resourceStatus),
			}
			ssn := &framework.Session{}

			nodes := make(map[string]*api.NodeInfo)
			nodeList := make([]*api.NodeInfo, 0, tt.nodeNumber)
			for i := 0; i < tt.nodeNumber; i++ {
				node := api.NodeInfo{
					Name: fmt.Sprintf("node-%d", i),
					Allocatable: &api.Resource{
						MilliCPU: 100000,
						Memory:   8000,
						ScalarResources: map[corev1.ResourceName]float64{
							"example.com/foo": 10,
						},
					},
					Used: &api.Resource{
						MilliCPU: 50000,
						Memory:   4000,
						ScalarResources: map[corev1.ResourceName]float64{
							"example.com/foo": 5,
						},
					},
					Idle: &api.Resource{
						MilliCPU: 50000,
						Memory:   4000,
						ScalarResources: map[corev1.ResourceName]float64{
							"example.com/foo": 5,
						},
					},
					Releasing: api.EmptyResource(),
					Pipelined: api.EmptyResource(),
				}
				tt.expectedScores[node.Name] = 0.515375
				nodes[fmt.Sprintf("node-%d", i)] = &node
				nodeList = append(nodeList, &node)
			}
			ssn.Nodes = nodes
			ssn.NodeList = nodeList

			hyperNodes := make(map[string]*api.HyperNodeInfo)
			hyperNodesSetByTier := make(map[int]sets.Set[string])
			for tier := 1; tier <= len(tt.hyperNodeNumbers); tier++ {
				hyperNodeSet := make(sets.Set[string])
				for index := 0; index < tt.hyperNodeNumbers[tier-1]; index++ {
					hyperNode := fmt.Sprintf("hypernode-tier-%d-index-%d", tier, index)
					hyperNodes[hyperNode] = &api.HyperNodeInfo{}
					hyperNodeSet.Insert(hyperNode)
				}
				hyperNodesSetByTier[tier] = hyperNodeSet
			}
			ssn.HyperNodes = hyperNodes
			ssn.HyperNodesSetByTier = hyperNodesSetByTier

			realNodeSet := make(map[string]sets.Set[string])
			for tier := 1; tier <= len(tt.hyperNodeNumbers); tier++ {
				for hyperNodeIndex := 0; hyperNodeIndex < tt.hyperNodeNumbers[tier-1]; hyperNodeIndex++ {
					realNodeSet[fmt.Sprintf("hypernode-tier-%d-index-%d", tier, hyperNodeIndex)] = make(sets.Set[string])
				}
				for i := 0; i < tt.nodeNumber; i++ {
					realNodeSet[fmt.Sprintf("hypernode-tier-%d-index-%d", tier, i%tt.hyperNodeNumbers[tier-1])].Insert(fmt.Sprintf("node-%d", i))
				}
			}
			ssn.RealNodesSet = realNodeSet

			task := &api.TaskInfo{
				Name: "task-without-network-topology",
				Resreq: &api.Resource{
					MilliCPU: 50000,
					Memory:   4000,
					ScalarResources: map[corev1.ResourceName]float64{
						"example.com/foo": 5,
					},
				},
			}

			plugin.hyperNodesTier.minTier = 1
			plugin.hyperNodesTier.maxTier = len(tt.hyperNodeNumbers)
			plugin.initHyperNodeResourceCache(ssn)

			start := time.Now()
			actualScores, actualErr := plugin.batchNodeOrderFnForNormalPods(ssn, task, ssn.NodeList)
			elapsed := time.Since(start)

			if tt.expectErr {
				assert.NotNil(t, actualErr, fmt.Sprintf("task %s expects to get an error, but got nil", task.Name))
				return
			}
			if actualErr != nil {
				t.Errorf("task %s expects to get no error, but got %v", task.Name, actualErr)
				return
			}
			assert.Equal(t, len(tt.expectedScores), len(actualScores), fmt.Sprintf("task %s expects to get the same number of node scores", task.Name))
			for node, expectedScore := range tt.expectedScores {
				if math.Abs(actualScores[node]-expectedScore) > eps {
					t.Errorf("task %s on node %s expects to get score %v, but got %v", task.Name, node, expectedScore, actualScores[node])
				}
			}

			fmt.Printf("The time cost for invoking the batchNodeOrderFnForNormalPods function is %v.\n", elapsed)
		})
	}
}

func TestBatchNodeOrderFnForNormalPodsUsesTreeLocalTiers(t *testing.T) {
	newHyperNode := func(name string, tier int, children ...string) *api.HyperNodeInfo {
		info := api.NewHyperNodeInfo(api.BuildHyperNode(name, tier, nil))
		info.Children.Insert(children...)
		return info
	}

	hyperNodes := api.HyperNodeInfoMap{
		"shallow-leaf": newHyperNode("shallow-leaf", 1),
		"shallow-root": newHyperNode("shallow-root", 2, "shallow-leaf"),
		"deep-leaf":    newHyperNode("deep-leaf", 1),
		"deep-mid":     newHyperNode("deep-mid", 2, "deep-leaf"),
		"deep-root":    newHyperNode("deep-root", 3, "deep-mid"),
		framework.ClusterTopHyperNode: newHyperNode(
			framework.ClusterTopHyperNode, 4, "shallow-root", "deep-root"),
	}
	for _, parent := range hyperNodes {
		for child := range parent.Children {
			hyperNodes[child].Parent = parent.Name
		}
	}

	ssn := &framework.Session{
		HyperNodes: hyperNodes,
		HyperNodesSetByTier: map[int]sets.Set[string]{
			1: sets.New[string]("shallow-leaf", "deep-leaf"),
			2: sets.New[string]("shallow-root", "deep-mid"),
			3: sets.New[string]("deep-root"),
			4: sets.New[string](framework.ClusterTopHyperNode),
		},
		RealNodesSet: map[string]sets.Set[string]{
			"shallow-leaf":                sets.New[string]("shallow-node"),
			"shallow-root":                sets.New[string]("shallow-node"),
			"deep-leaf":                   sets.New[string]("deep-node"),
			"deep-mid":                    sets.New[string]("deep-node"),
			"deep-root":                   sets.New[string]("deep-node"),
			framework.ClusterTopHyperNode: sets.New[string]("shallow-node", "deep-node", "outside-node"),
		},
	}

	plugin := &networkTopologyAwarePlugin{
		weight: &priorityWeight{
			HyperNodeBinPackingCPU: 1,
		},
		normalPodConfig: &normalPodConfig{
			hyperNodeBinPackingEnable: true,
			hyperNodeBinPackingFading: 0.5,
		},
		hyperNodesTier:         &hyperNodesTier{minTier: 1, maxTier: 4},
		hyperNodeResourceCache: map[string]*resourceStatus{},
	}
	for name := range hyperNodes {
		plugin.hyperNodeResourceCache[name] = &resourceStatus{
			allocatable: &api.Resource{MilliCPU: 100},
			used:        &api.Resource{MilliCPU: 40},
		}
	}

	task := &api.TaskInfo{
		Name:   "normal-pod",
		Resreq: &api.Resource{MilliCPU: 10},
	}
	nodes := []*api.NodeInfo{{Name: "shallow-node"}, {Name: "deep-node"}, {Name: "outside-node"}}

	scores, err := plugin.batchNodeOrderFnForNormalPods(ssn, task, nodes)
	assert.NoError(t, err)
	assert.InDelta(t, 0.5, scores["shallow-node"], 1e-9)
	assert.InDelta(t, 0.5, scores["deep-node"], 1e-9)
	assert.InDelta(t, scores["shallow-node"], scores["deep-node"], 1e-9,
		"equivalent local topology utilization must not favor the shallower tree")
	assert.Equal(t, FullScore, scores["outside-node"],
		"a Node outside all real topology trees should retain the preferred fallback score")

	plugin.hyperNodeResourceCache["shallow-leaf"].used.MilliCPU = 10
	plugin.hyperNodeResourceCache["shallow-root"].used.MilliCPU = 30
	plugin.hyperNodeResourceCache[framework.ClusterTopHyperNode].used.MilliCPU = 70
	scores, err = plugin.batchNodeOrderFnForNormalPods(ssn, task, nodes[:1])
	assert.NoError(t, err)
	assert.InDelta(t, (0.2+0.4*0.5+0.8*0.25)/(1+0.5+0.25), scores["shallow-node"], 1e-9,
		"the virtual cluster root should remain the final fading level of a real tree")
}

func TestBatchNodeOrderFnForNetworkAwarePodsUsesTreeLocalLeaf(t *testing.T) {
	newHyperNode := func(name string, tier int, children ...string) *api.HyperNodeInfo {
		info := api.NewHyperNodeInfo(api.BuildHyperNode(name, tier, nil))
		info.Children.Insert(children...)
		return info
	}

	shallowNode := &api.NodeInfo{Name: "shallow-node"}
	deepNode := &api.NodeInfo{Name: "deep-node"}
	hyperNodes := api.HyperNodeInfoMap{
		"shallow-leaf": newHyperNode("shallow-leaf", 1),
		"shallow-root": newHyperNode("shallow-root", 2, "shallow-leaf"),
		"deep-leaf":    newHyperNode("deep-leaf", 0),
		"deep-mid":     newHyperNode("deep-mid", 1, "deep-leaf"),
		"deep-root":    newHyperNode("deep-root", 2, "deep-mid"),
		framework.ClusterTopHyperNode: newHyperNode(
			framework.ClusterTopHyperNode, 3, "shallow-root", "deep-root"),
	}
	for _, parent := range hyperNodes {
		for child := range parent.Children {
			hyperNodes[child].Parent = parent.Name
		}
	}

	ssn := &framework.Session{
		HyperNodes:      hyperNodes,
		HyperNodesTiers: []int{0, 1, 2, 3},
		HyperNodesSetByTier: map[int]sets.Set[string]{
			0: sets.New[string]("deep-leaf"),
			1: sets.New[string]("shallow-leaf", "deep-mid"),
			2: sets.New[string]("shallow-root", "deep-root"),
			3: sets.New[string](framework.ClusterTopHyperNode),
		},
		RealNodesList: map[string][]*api.NodeInfo{
			"shallow-leaf":                {shallowNode},
			"shallow-root":                {shallowNode},
			"deep-leaf":                   {deepNode},
			"deep-mid":                    {deepNode},
			"deep-root":                   {deepNode},
			framework.ClusterTopHyperNode: {shallowNode, deepNode},
		},
		RealNodesSet: map[string]sets.Set[string]{
			"shallow-leaf":                sets.New[string](shallowNode.Name),
			"shallow-root":                sets.New[string](shallowNode.Name),
			"deep-leaf":                   sets.New[string](deepNode.Name),
			"deep-mid":                    sets.New[string](deepNode.Name),
			"deep-root":                   sets.New[string](deepNode.Name),
			framework.ClusterTopHyperNode: sets.New[string](shallowNode.Name, deepNode.Name),
		},
	}
	plugin := &networkTopologyAwarePlugin{}
	task := &api.TaskInfo{
		TransactionContext: api.TransactionContext{JobAllocatedHyperNode: "shallow-leaf"},
	}

	scores, err := plugin.batchNodeOrderFnForNetworkAwarePods(ssn, task, &api.SubJobInfo{}, []*api.NodeInfo{shallowNode})
	assert.NoError(t, err)
	assert.Equal(t, FullScore, scores[shallowNode.Name],
		"the shallow Node must resolve its own leaf even when another tree has a lower numeric tier")
}

func TestHyperNodeGradientWithMixedDepthTopologies(t *testing.T) {
	const (
		hyperNodeTierName    = "volcano.sh/hypernode"
		hyperClusterTierName = "volcano.sh/hypercluster"
		superPodTierName     = "volcano.sh/superpod"
	)

	newHyperNodeInfo := func(name string, tier int, tierName string, children ...string) *api.HyperNodeInfo {
		members := make([]api.MemberConfig, 0, len(children))
		for _, child := range children {
			members = append(members, api.MemberConfig{
				Name:     child,
				Type:     topologyv1alpha1.MemberTypeHyperNode,
				Selector: "exact",
			})
		}

		hyperNode := api.BuildHyperNode(name, tier, members)
		hyperNode.Spec.TierName = tierName
		info := api.NewHyperNodeInfo(hyperNode)
		info.Children.Insert(children...)
		return info
	}

	hyperNodes := api.HyperNodeInfoMap{
		// shallow has two topology tiers: hypernode -> hypercluster.
		"shallow-hypernode-0": newHyperNodeInfo("shallow-hypernode-0", 1, hyperNodeTierName),
		"shallow-hypernode-1": newHyperNodeInfo("shallow-hypernode-1", 1, hyperNodeTierName),
		"shallow-hypercluster": newHyperNodeInfo(
			"shallow-hypercluster", 2, hyperClusterTierName, "shallow-hypernode-0", "shallow-hypernode-1"),

		// deep inserts superpod below hypernode, shifting the same semantic tiers up by one.
		"deep-superpod-0": newHyperNodeInfo("deep-superpod-0", 1, superPodTierName),
		"deep-superpod-1": newHyperNodeInfo("deep-superpod-1", 1, superPodTierName),
		"deep-hypernode": newHyperNodeInfo(
			"deep-hypernode", 2, hyperNodeTierName, "deep-superpod-0", "deep-superpod-1"),
		"deep-hypercluster": newHyperNodeInfo(
			"deep-hypercluster", 3, hyperClusterTierName, "deep-hypernode"),
		framework.ClusterTopHyperNode: newHyperNodeInfo(
			framework.ClusterTopHyperNode, 4, "", "shallow-hypercluster", "deep-hypercluster"),
	}
	for _, parent := range hyperNodes {
		for child := range parent.Children {
			hyperNodes[child].Parent = parent.Name
		}
	}

	networkTopology := &scheduling.NetworkTopologySpec{
		Mode:            scheduling.HardNetworkTopologyMode,
		HighestTierName: hyperNodeTierName,
	}

	plugin := &networkTopologyAwarePlugin{
		hyperNodeResourceCache: make(map[string]*resourceStatus),
	}
	ssn := &framework.Session{HyperNodes: hyperNodes}

	collectNames := func(gradients [][]*api.HyperNodeInfo) sets.Set[string] {
		names := sets.New[string]()
		for _, gradient := range gradients {
			for _, hyperNode := range gradient {
				names.Insert(hyperNode.Name)
			}
		}
		return names
	}
	gradientNames := func(gradients [][]*api.HyperNodeInfo) [][]string {
		result := make([][]string, 0, len(gradients))
		for _, gradient := range gradients {
			names := make([]string, 0, len(gradient))
			for _, hyperNode := range gradient {
				names = append(names, hyperNode.Name)
			}
			result = append(result, names)
		}
		return result
	}

	gradients, err := plugin.hyperNodeGradientFn(
		ssn, hyperNodes[framework.ClusterTopHyperNode], networkTopology, "", nil, api.PurposeAllocate)
	assert.NoError(t, err)
	candidates := collectNames(gradients)

	// highestTierName=hypernode has a different numeric tier in each topology.
	// Correct behavior must honor the semantic boundary independently per subtree.
	assert.True(t, candidates.Has("shallow-hypernode-0"), "shallow hypernode tier should remain eligible")
	assert.False(t, candidates.Has("shallow-hypercluster"),
		"shallow must not cross the hypernode boundary into hypercluster")
	assert.True(t, candidates.Has("deep-hypernode"),
		"deep must allow its tier-2 hypernode even though shallow uses tier 1 for the same tier name")
	assert.True(t, candidates.Has("deep-superpod-0"), "deep descendants below the boundary should remain eligible")
	assert.False(t, candidates.Has("deep-hypercluster"),
		"deep must not cross the hypernode boundary into hypercluster")
	assert.Equal(t, [][]string{
		{"deep-superpod-0", "deep-superpod-1"},
		{"deep-hypernode"},
		{"shallow-hypernode-0", "shallow-hypernode-1"},
	}, gradientNames(gradients), "each tree should contribute its own ascending local-tier gradients")

	t.Run("numeric tiers remain isolated by real tree", func(t *testing.T) {
		highestTierAllowed := 2
		topology := &scheduling.NetworkTopologySpec{
			Mode:               scheduling.HardNetworkTopologyMode,
			HighestTierAllowed: &highestTierAllowed,
		}
		gradients, err := plugin.hyperNodeGradientFn(
			ssn, hyperNodes[framework.ClusterTopHyperNode], topology, "", nil, api.PurposeAllocate)
		assert.NoError(t, err)
		assert.Equal(t, [][]string{
			{"deep-superpod-0", "deep-superpod-1"},
			{"deep-hypernode"},
			{"shallow-hypernode-0", "shallow-hypernode-1"},
			{"shallow-hypercluster"},
		}, gradientNames(gradients))
	})

	t.Run("eviction keeps each real tree in wider-to-narrower order", func(t *testing.T) {
		originalLimit := plugin.maxHyperNodesForEviction
		plugin.maxHyperNodesForEviction = len(hyperNodes)
		defer func() {
			plugin.maxHyperNodesForEviction = originalLimit
		}()

		topology := &scheduling.NetworkTopologySpec{
			Mode:            scheduling.HardNetworkTopologyMode,
			HighestTierName: hyperClusterTierName,
		}
		gradients, err := plugin.hyperNodeGradientFn(
			ssn, hyperNodes[framework.ClusterTopHyperNode], topology, "", nil, api.PurposeEvict)
		assert.NoError(t, err)
		gradients = plugin.reverseAndCapEvictionGradients(gradients)

		positions := map[string]int{}
		position := 0
		for _, gradient := range gradients {
			containsShallow := false
			containsDeep := false
			for _, hyperNode := range gradient {
				containsShallow = containsShallow || strings.HasPrefix(hyperNode.Name, "shallow-")
				containsDeep = containsDeep || strings.HasPrefix(hyperNode.Name, "deep-")
				positions[hyperNode.Name] = position
				position++
			}
			assert.False(t, containsShallow && containsDeep, "local tiers from separate real trees must not be merged")
		}
		assert.Len(t, positions, len(hyperNodes)-1, "the virtual root is outside the hard boundary")
		assert.Less(t, positions["shallow-hypercluster"], positions["shallow-hypernode-0"])
		assert.Less(t, positions["shallow-hypercluster"], positions["shallow-hypernode-1"])
		assert.Less(t, positions["deep-hypercluster"], positions["deep-hypernode"])
		assert.Less(t, positions["deep-hypernode"], positions["deep-superpod-0"])
		assert.Less(t, positions["deep-hypernode"], positions["deep-superpod-1"])
	})

	t.Run("branch without requested tier name is excluded", func(t *testing.T) {
		topology := &scheduling.NetworkTopologySpec{
			Mode:            scheduling.HardNetworkTopologyMode,
			HighestTierName: superPodTierName,
		}
		gradients, err := plugin.hyperNodeGradientFn(
			ssn, hyperNodes[framework.ClusterTopHyperNode], topology, "", nil, api.PurposeAllocate)
		assert.NoError(t, err)
		candidates := collectNames(gradients)
		assert.True(t, candidates.Has("deep-superpod-0"))
		assert.False(t, candidates.Has("shallow-hypernode-0"))
		assert.False(t, candidates.Has("shallow-hypercluster"))
	})

	t.Run("partially running job resolves boundary from allocated branch", func(t *testing.T) {
		gradients, err := plugin.hyperNodeGradientFn(
			ssn, hyperNodes[framework.ClusterTopHyperNode], networkTopology, "deep-superpod-0", nil, api.PurposeAllocate)
		assert.NoError(t, err)
		assert.Equal(t, [][]string{
			{"deep-superpod-0", "deep-superpod-1"},
			{"deep-hypernode"},
		}, gradientNames(gradients), "a partially running job must remain in the allocated tree")
		candidates := collectNames(gradients)
		assert.True(t, candidates.Has("deep-hypernode"))
		assert.True(t, candidates.Has("deep-superpod-1"))
		assert.False(t, candidates.Has("deep-hypercluster"))
		assert.False(t, candidates.Has("shallow-hypernode-0"))
	})

	t.Run("real shared root keeps branch-local boundaries", func(t *testing.T) {
		originalTop := hyperNodes[framework.ClusterTopHyperNode]
		originalShallowParent := hyperNodes["shallow-hypercluster"].Parent
		originalDeepParent := hyperNodes["deep-hypercluster"].Parent
		hyperNodes["fabric-root"] = newHyperNodeInfo(
			"fabric-root", 4, "volcano.sh/fabric", "shallow-hypercluster", "deep-hypercluster")
		hyperNodes[framework.ClusterTopHyperNode] = newHyperNodeInfo(
			framework.ClusterTopHyperNode, 5, "", "fabric-root")
		hyperNodes["fabric-root"].Parent = framework.ClusterTopHyperNode
		hyperNodes["shallow-hypercluster"].Parent = "fabric-root"
		hyperNodes["deep-hypercluster"].Parent = "fabric-root"
		defer func() {
			hyperNodes[framework.ClusterTopHyperNode] = originalTop
			hyperNodes["shallow-hypercluster"].Parent = originalShallowParent
			hyperNodes["deep-hypercluster"].Parent = originalDeepParent
			delete(hyperNodes, "fabric-root")
		}()

		gradients, err := plugin.hyperNodeGradientFn(
			ssn, hyperNodes[framework.ClusterTopHyperNode], networkTopology, "", nil, api.PurposeAllocate)
		assert.NoError(t, err)
		candidates := collectNames(gradients)
		assert.True(t, candidates.Has("shallow-hypernode-0"))
		assert.True(t, candidates.Has("deep-hypernode"))
		assert.False(t, candidates.Has("fabric-root"))
		assert.False(t, candidates.Has("shallow-hypercluster"))
		assert.False(t, candidates.Has("deep-hypercluster"))
	})

	t.Run("missing tier name fails closed", func(t *testing.T) {
		topology := &scheduling.NetworkTopologySpec{
			Mode:            scheduling.HardNetworkTopologyMode,
			HighestTierName: "volcano.sh/not-found",
		}
		gradients, err := plugin.hyperNodeGradientFn(
			ssn, hyperNodes[framework.ClusterTopHyperNode], topology, "", nil, api.PurposeAllocate)
		assert.Error(t, err)
		assert.Nil(t, gradients)
	})

	t.Run("duplicate tier name in one ancestor chain is rejected", func(t *testing.T) {
		originalName := hyperNodes["deep-superpod-0"].HyperNode.Spec.TierName
		hyperNodes["deep-superpod-0"].HyperNode.Spec.TierName = hyperNodeTierName
		duplicate := api.NewHyperNodeInfo(hyperNodes["deep-superpod-0"].HyperNode, api.ParentOpt("deep-hypernode"))
		hyperNodes["deep-superpod-0"] = duplicate
		defer func() {
			hyperNodes["deep-superpod-0"].HyperNode.Spec.TierName = originalName
		}()

		gradients, err := plugin.hyperNodeGradientFn(
			ssn, hyperNodes[framework.ClusterTopHyperNode], networkTopology, "", nil, api.PurposeAllocate)
		assert.Error(t, err)
		assert.Nil(t, gradients)
	})
}

func TestHyperNodeGradientHardNumericClusterBoundaryIsTreeLocal(t *testing.T) {
	newHyperNode := func(name string, tier int, children ...string) *api.HyperNodeInfo {
		info := api.NewHyperNodeInfo(api.BuildHyperNode(name, tier, nil))
		info.Children.Insert(children...)
		return info
	}

	const (
		shallowRoot = "shallow-root"
		deepRoot    = "deep-root"
	)
	clusterRoot := framework.ClusterTopHyperNode
	hyperNodes := api.HyperNodeInfoMap{
		shallowRoot: newHyperNode(shallowRoot, 1),
		deepRoot:    newHyperNode(deepRoot, 1),
		clusterRoot: newHyperNode(clusterRoot, 2, shallowRoot, deepRoot),
	}
	for _, parent := range hyperNodes {
		for child := range parent.Children {
			hyperNodes[child].Parent = parent.Name
		}
	}

	ssn := &framework.Session{
		HyperNodes: hyperNodes,
		RealNodesSet: map[string]sets.Set[string]{
			shallowRoot: sets.New[string]("shallow-node"),
			deepRoot:    sets.New[string]("deep-node"),
			clusterRoot: sets.New[string]("shallow-node", "deep-node"),
		},
	}
	plugin := &networkTopologyAwarePlugin{
		hyperNodeResourceCache: map[string]*resourceStatus{
			shallowRoot: {idle: &api.Resource{MilliCPU: 2000}, futureIdle: &api.Resource{MilliCPU: 2000}},
			deepRoot:    {idle: &api.Resource{MilliCPU: 2000}, futureIdle: &api.Resource{MilliCPU: 2000}},
			clusterRoot: {idle: &api.Resource{MilliCPU: 4000}, futureIdle: &api.Resource{MilliCPU: 4000}},
		},
	}
	required := &api.Resource{MilliCPU: 4000}
	highestTierAllowed := 2
	topology := &scheduling.NetworkTopologySpec{
		Mode:               scheduling.HardNetworkTopologyMode,
		HighestTierAllowed: &highestTierAllowed,
	}

	gradients, err := plugin.hyperNodeGradientFn(ssn, hyperNodes[clusterRoot], topology, "", required, api.PurposeAllocate)
	require.NoError(t, err)
	assert.Empty(t, gradients,
		"native Hard at the virtual-root numeric boundary must not aggregate sibling tree capacity")

	hardSubJob := &api.SubJobInfo{
		UID: "hard-subjob",
		NetworkTopology: &scheduling.NetworkTopologySpec{
			Mode:               scheduling.HardNetworkTopologyMode,
			HighestTierAllowed: &highestTierAllowed,
		},
	}
	subJobGradients, err := plugin.hyperNodeGradientFn(
		ssn, hyperNodes[clusterRoot], hardSubJob.HardTopologyConstraint(), "", required, api.PurposeAllocate, hardSubJob.IsSoftTopologyConverted())
	require.NoError(t, err)
	assert.Empty(t, subJobGradients,
		"native Hard SubJob at the virtual-root numeric boundary must not aggregate sibling tree capacity")

	plugin.hyperNodeResourceCache[deepRoot].idle = &api.Resource{MilliCPU: 4000}
	plugin.hyperNodeResourceCache[deepRoot].futureIdle = &api.Resource{MilliCPU: 4000}
	gradients, err = plugin.hyperNodeGradientFn(ssn, hyperNodes[clusterRoot], topology, "", required, api.PurposeAllocate)
	require.NoError(t, err)
	assert.Equal(t, [][]*api.HyperNodeInfo{{hyperNodes[deepRoot]}}, gradients,
		"native Hard must select only the single feasible real topology tree")
	subJobGradients, err = plugin.hyperNodeGradientFn(
		ssn, hyperNodes[clusterRoot], hardSubJob.HardTopologyConstraint(), "", required, api.PurposeAllocate, hardSubJob.IsSoftTopologyConverted())
	require.NoError(t, err)
	assert.Equal(t, [][]*api.HyperNodeInfo{{hyperNodes[deepRoot]}}, subJobGradients,
		"native Hard SubJob must select only the single feasible real topology tree")

	gradients, err = plugin.hyperNodeGradientFn(
		ssn, hyperNodes[clusterRoot], topology, deepRoot, required, api.PurposeAllocate)
	require.NoError(t, err)
	assert.Equal(t, [][]*api.HyperNodeInfo{{hyperNodes[deepRoot]}}, gradients,
		"a partially allocated native Hard Job must remain in its original real topology tree")

	subJobGradients, err = plugin.hyperNodeGradientFn(
		ssn, hyperNodes[clusterRoot], hardSubJob.HardTopologyConstraint(), deepRoot, required, api.PurposeAllocate, hardSubJob.IsSoftTopologyConverted())
	require.NoError(t, err)
	assert.Equal(t, [][]*api.HyperNodeInfo{{hyperNodes[deepRoot]}}, subJobGradients,
		"a partially allocated native Hard SubJob must remain in its original real topology tree")
}

func TestHyperNodeGradientConvertedSoftKeepsVirtualRootCompatibility(t *testing.T) {
	newHyperNode := func(name string, tier int, children ...string) *api.HyperNodeInfo {
		info := api.NewHyperNodeInfo(api.BuildHyperNode(name, tier, nil))
		info.Children.Insert(children...)
		return info
	}
	clusterRoot := framework.ClusterTopHyperNode
	hyperNodes := api.HyperNodeInfoMap{
		"tree-a":    newHyperNode("tree-a", 1),
		"tree-b":    newHyperNode("tree-b", 1),
		clusterRoot: newHyperNode(clusterRoot, 2, "tree-a", "tree-b"),
	}
	for _, parent := range hyperNodes {
		for child := range parent.Children {
			hyperNodes[child].Parent = parent.Name
		}
	}
	ssn := &framework.Session{
		HyperNodes: hyperNodes,
		RealNodesSet: map[string]sets.Set[string]{
			"tree-a":    sets.New[string]("node-a"),
			"tree-b":    sets.New[string]("node-b"),
			clusterRoot: sets.New[string]("node-a", "node-b"),
		},
	}
	plugin := &networkTopologyAwarePlugin{
		hyperNodeResourceCache: map[string]*resourceStatus{
			"tree-a":    {idle: &api.Resource{MilliCPU: 2000}, futureIdle: &api.Resource{MilliCPU: 2000}},
			"tree-b":    {idle: &api.Resource{MilliCPU: 2000}, futureIdle: &api.Resource{MilliCPU: 2000}},
			clusterRoot: {idle: &api.Resource{MilliCPU: 4000}, futureIdle: &api.Resource{MilliCPU: 4000}},
		},
	}
	required := &api.Resource{MilliCPU: 4000}
	subJob := &api.SubJobInfo{
		UID:             "soft-subjob",
		NetworkTopology: &scheduling.NetworkTopologySpec{Mode: scheduling.SoftNetworkTopologyMode},
	}
	subJob.ConvertToHardTopology(hyperNodes[clusterRoot].Tier())
	require.True(t, subJob.IsSoftTopologyConverted())
	require.NotNil(t, subJob.HardTopologyConstraint())

	gradients, err := plugin.hyperNodeGradientFn(ssn, hyperNodes[clusterRoot], subJob.HardTopologyConstraint(), "", required, api.PurposeAllocate, subJob.IsSoftTopologyConverted())
	require.NoError(t, err)
	candidates := sets.New[string]()
	for _, gradient := range gradients {
		for _, hyperNode := range gradient {
			candidates.Insert(hyperNode.Name)
		}
	}
	assert.Contains(t, candidates, clusterRoot,
		"a Soft constraint converted by the framework keeps the legacy virtual-root candidate")
}

func TestHyperNodeGradientWithSingleTierTopology(t *testing.T) {
	const (
		hyperNodeTierName = "volcano.sh/hypernode"
		superPodTierName  = "volcano.sh/superpod"
	)
	newHyperNodeInfo := func(name string, tier int, tierName string, children ...string) *api.HyperNodeInfo {
		members := make([]api.MemberConfig, 0, len(children))
		for _, child := range children {
			members = append(members, api.MemberConfig{
				Name:     child,
				Type:     topologyv1alpha1.MemberTypeHyperNode,
				Selector: "exact",
			})
		}
		hyperNode := api.BuildHyperNode(name, tier, members)
		hyperNode.Spec.TierName = tierName
		info := api.NewHyperNodeInfo(hyperNode)
		info.Children.Insert(children...)
		return info
	}

	hyperNodes := api.HyperNodeInfoMap{
		"single-tier":    newHyperNodeInfo("single-tier", 1, hyperNodeTierName),
		"deep-leaf":      newHyperNodeInfo("deep-leaf", 1, superPodTierName),
		"deep-hypernode": newHyperNodeInfo("deep-hypernode", 2, hyperNodeTierName, "deep-leaf"),
		"deep-root":      newHyperNodeInfo("deep-root", 3, "volcano.sh/hypercluster", "deep-hypernode"),
		framework.ClusterTopHyperNode: newHyperNodeInfo(
			framework.ClusterTopHyperNode, 4, "", "single-tier", "deep-root"),
	}
	for _, parent := range hyperNodes {
		for child := range parent.Children {
			hyperNodes[child].Parent = parent.Name
		}
	}

	plugin := &networkTopologyAwarePlugin{}
	ssn := &framework.Session{HyperNodes: hyperNodes}
	topology := &scheduling.NetworkTopologySpec{
		Mode:            scheduling.HardNetworkTopologyMode,
		HighestTierName: hyperNodeTierName,
	}
	gradientNames := func(gradients [][]*api.HyperNodeInfo) [][]string {
		result := make([][]string, 0, len(gradients))
		for _, gradient := range gradients {
			names := make([]string, 0, len(gradient))
			for _, hyperNode := range gradient {
				names = append(names, hyperNode.Name)
			}
			result = append(result, names)
		}
		return result
	}

	gradients, err := plugin.hyperNodeGradientFn(
		ssn, hyperNodes[framework.ClusterTopHyperNode], topology, "", nil, api.PurposeAllocate)
	require.NoError(t, err)
	assert.Equal(t, [][]string{
		{"deep-leaf"},
		{"deep-hypernode"},
		{"single-tier"},
	}, gradientNames(gradients),
		"a one-level tree must contribute its tier-1 candidate without inheriting a sibling tree's depth")

	highestTierAllowed := 1
	gradients, err = plugin.hyperNodeGradientFn(
		ssn, hyperNodes[framework.ClusterTopHyperNode], &scheduling.NetworkTopologySpec{
			Mode:               scheduling.HardNetworkTopologyMode,
			HighestTierAllowed: &highestTierAllowed,
		}, "", nil, api.PurposeAllocate)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"deep-leaf"}, {"single-tier"}}, gradientNames(gradients),
		"numeric tier 1 must be evaluated independently in each real tree")
}

// TestHyperNodeGradientPreFiltering tests the pre-filtering logic in hyperNodeGradientFn.
// It verifies HyperNodes are correctly filtered for allocation and eviction purposes.
func TestHyperNodeGradientPreFiltering(t *testing.T) {
	const (
		nodeCount       = 1000
		nodesPerTier1HN = 10
		tier1HNCount    = 100
		// Single node resources: 4 CPU (4000m), 8Gi Memory
		nodeCPU    = 4000
		nodeMemory = 8 * 1024 * 1024 * 1024 // 8Gi in bytes
	)

	tests := []struct {
		name                string
		isSubJob            bool
		purpose             api.SearchPurpose
		highestAllowedTier  int
		minResource         *api.Resource
		idleResource        *api.Resource
		futureIdleResource  *api.Resource
		expectTier1Selected bool
	}{
		// Job scenarios (highestAllowedTier = 2)
		{
			name:               "Job - idle sufficient, futureIdle sufficient",
			isSubJob:           false,
			purpose:            api.PurposeAllocate,
			highestAllowedTier: 2,
			minResource: &api.Resource{
				MilliCPU: 20000,
				Memory:   40 * 1024 * 1024 * 1024,
			},
			idleResource: &api.Resource{
				MilliCPU: 30000,
				Memory:   60 * 1024 * 1024 * 1024,
			},
			futureIdleResource: &api.Resource{
				MilliCPU: 35000,
				Memory:   70 * 1024 * 1024 * 1024,
			},
			expectTier1Selected: true,
		},
		{
			name:               "Job - idle sufficient, futureIdle insufficient",
			isSubJob:           false,
			purpose:            api.PurposeAllocate,
			highestAllowedTier: 2,
			minResource: &api.Resource{
				MilliCPU: 20000,
				Memory:   40 * 1024 * 1024 * 1024,
			},
			idleResource: &api.Resource{
				MilliCPU: 30000,
				Memory:   60 * 1024 * 1024 * 1024,
			},
			futureIdleResource: &api.Resource{
				MilliCPU: 15000,
				Memory:   30 * 1024 * 1024 * 1024,
			},
			expectTier1Selected: true,
		},
		{
			name:               "Job - idle insufficient, futureIdle sufficient",
			isSubJob:           false,
			purpose:            api.PurposeAllocate,
			highestAllowedTier: 2,
			minResource: &api.Resource{
				MilliCPU: 20000,
				Memory:   40 * 1024 * 1024 * 1024,
			},
			idleResource: &api.Resource{
				MilliCPU: 15000,
				Memory:   30 * 1024 * 1024 * 1024,
			},
			futureIdleResource: &api.Resource{
				MilliCPU: 30000,
				Memory:   60 * 1024 * 1024 * 1024,
			},
			expectTier1Selected: true,
		},
		{
			name:               "Job - idle insufficient, futureIdle insufficient",
			isSubJob:           false,
			purpose:            api.PurposeAllocate,
			highestAllowedTier: 2,
			minResource: &api.Resource{
				MilliCPU: 20000,
				Memory:   40 * 1024 * 1024 * 1024,
			},
			idleResource: &api.Resource{
				MilliCPU: 15000,
				Memory:   30 * 1024 * 1024 * 1024,
			},
			futureIdleResource: &api.Resource{
				MilliCPU: 15000,
				Memory:   30 * 1024 * 1024 * 1024,
			},
			expectTier1Selected: false,
		},
		// SubJob scenarios (highestAllowedTier = 1)
		{
			name:               "SubJob - idle sufficient, futureIdle sufficient",
			isSubJob:           true,
			purpose:            api.PurposeAllocate,
			highestAllowedTier: 1,
			minResource: &api.Resource{
				MilliCPU: 10000,
				Memory:   20 * 1024 * 1024 * 1024,
			},
			idleResource: &api.Resource{
				MilliCPU: 20000,
				Memory:   40 * 1024 * 1024 * 1024,
			},
			futureIdleResource: &api.Resource{
				MilliCPU: 25000,
				Memory:   50 * 1024 * 1024 * 1024,
			},
			expectTier1Selected: true,
		},
		{
			name:               "SubJob - idle sufficient, futureIdle insufficient",
			isSubJob:           true,
			purpose:            api.PurposeAllocate,
			highestAllowedTier: 1,
			minResource: &api.Resource{
				MilliCPU: 10000,
				Memory:   20 * 1024 * 1024 * 1024,
			},
			idleResource: &api.Resource{
				MilliCPU: 20000,
				Memory:   40 * 1024 * 1024 * 1024,
			},
			futureIdleResource: &api.Resource{
				MilliCPU: 5000,
				Memory:   10 * 1024 * 1024 * 1024,
			},
			expectTier1Selected: true,
		},
		{
			name:               "SubJob - idle insufficient, futureIdle sufficient",
			isSubJob:           true,
			purpose:            api.PurposeAllocate,
			highestAllowedTier: 1,
			minResource: &api.Resource{
				MilliCPU: 10000,
				Memory:   20 * 1024 * 1024 * 1024,
			},
			idleResource: &api.Resource{
				MilliCPU: 5000,
				Memory:   10 * 1024 * 1024 * 1024,
			},
			futureIdleResource: &api.Resource{
				MilliCPU: 20000,
				Memory:   40 * 1024 * 1024 * 1024,
			},
			expectTier1Selected: true,
		},
		{
			name:               "SubJob - idle insufficient, futureIdle insufficient",
			isSubJob:           true,
			purpose:            api.PurposeAllocate,
			highestAllowedTier: 1,
			minResource: &api.Resource{
				MilliCPU: 10000,
				Memory:   20 * 1024 * 1024 * 1024,
			},
			idleResource: &api.Resource{
				MilliCPU: 5000,
				Memory:   10 * 1024 * 1024 * 1024,
			},
			futureIdleResource: &api.Resource{
				MilliCPU: 5000,
				Memory:   10 * 1024 * 1024 * 1024,
			},
			expectTier1Selected: false,
		},
		{
			name:               "Evict purpose - idle insufficient, futureIdle insufficient",
			isSubJob:           false,
			purpose:            api.PurposeEvict,
			highestAllowedTier: 2,
			minResource: &api.Resource{
				MilliCPU: 20000,
				Memory:   40 * 1024 * 1024 * 1024,
			},
			idleResource: &api.Resource{
				MilliCPU: 15000,
				Memory:   30 * 1024 * 1024 * 1024,
			},
			futureIdleResource: &api.Resource{
				MilliCPU: 15000,
				Memory:   30 * 1024 * 1024 * 1024,
			},
			expectTier1Selected: true,
		},
		{
			name:               "Evict purpose - allocatable insufficient",
			isSubJob:           false,
			purpose:            api.PurposeEvict,
			highestAllowedTier: 2,
			minResource: &api.Resource{
				MilliCPU: 50000,
				Memory:   100 * 1024 * 1024 * 1024,
			},
			idleResource: &api.Resource{
				MilliCPU: 15000,
				Memory:   30 * 1024 * 1024 * 1024,
			},
			futureIdleResource: &api.Resource{
				MilliCPU: 15000,
				Memory:   30 * 1024 * 1024 * 1024,
			},
			expectTier1Selected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build plugin
			plugin := &networkTopologyAwarePlugin{
				pluginArguments:        framework.Arguments{},
				weight:                 getPriorityWeight(framework.Arguments{}),
				normalPodConfig:        getNormalPodConfig(framework.Arguments{}),
				hyperNodesTier:         &hyperNodesTier{minTier: 1, maxTier: 2},
				hyperNodeResourceCache: make(map[string]*resourceStatus),
			}

			// Build 1000 nodes
			nodes := make(map[string]*api.NodeInfo)
			for i := 0; i < nodeCount; i++ {
				nodeName := fmt.Sprintf("node-%d", i)
				nodes[nodeName] = &api.NodeInfo{
					Name: nodeName,
					Allocatable: &api.Resource{
						MilliCPU: nodeCPU,
						Memory:   nodeMemory,
					},
					Used:      api.EmptyResource(),
					Releasing: api.EmptyResource(),
					Pipelined: api.EmptyResource(),
					Idle: &api.Resource{
						MilliCPU: nodeCPU,
						Memory:   nodeMemory,
					},
				}
			}

			// Build HyperNodes topology
			hyperNodesMap := make(map[string]*api.HyperNodeInfo)
			realNodesSet := make(map[string]sets.Set[string])
			hyperNodesSetByTier := map[int]sets.Set[string]{
				1: sets.New[string](),
				2: sets.New[string](),
			}

			// Build tier-1 HyperNodes
			tier1Children := make([]api.MemberConfig, 0, tier1HNCount)
			for i := 0; i < tier1HNCount; i++ {
				hnName := fmt.Sprintf("hn-1-%d", i)
				hyperNodesSetByTier[1].Insert(hnName)
				tier1Children = append(tier1Children, api.MemberConfig{
					Name:     hnName,
					Type:     topologyv1alpha1.MemberTypeHyperNode,
					Selector: "exact",
				})

				nodeMembers := make([]api.MemberConfig, 0, nodesPerTier1HN)
				nodeSet := sets.New[string]()
				for j := 0; j < nodesPerTier1HN; j++ {
					nodeName := fmt.Sprintf("node-%d", i*nodesPerTier1HN+j)
					nodeMembers = append(nodeMembers, api.MemberConfig{
						Name:     nodeName,
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					})
					nodeSet.Insert(nodeName)
				}
				realNodesSet[hnName] = nodeSet
				hyperNodesMap[hnName] = api.NewHyperNodeInfo(api.BuildHyperNode(hnName, 1, nodeMembers))
			}

			// Build tier-2 HyperNode
			tier2HNName := "hn-2-0"
			hyperNodesSetByTier[2].Insert(tier2HNName)
			hyperNodesMap[tier2HNName] = api.NewHyperNodeInfo(api.BuildHyperNode(tier2HNName, 2, tier1Children))
			// Set Children for tier-2 HyperNode
			for i := 0; i < tier1HNCount; i++ {
				hyperNodesMap[tier2HNName].Children.Insert(fmt.Sprintf("hn-1-%d", i))
			}
			allNodes := sets.New[string]()
			for i := 0; i < nodeCount; i++ {
				allNodes.Insert(fmt.Sprintf("node-%d", i))
			}
			realNodesSet[tier2HNName] = allNodes

			// Build session
			ssn := &framework.Session{
				Nodes:               nodes,
				HyperNodes:          hyperNodesMap,
				RealNodesSet:        realNodesSet,
				HyperNodesSetByTier: hyperNodesSetByTier,
				HyperNodesTiers:     []int{1, 2},
			}

			// Initialize hyperNodeResourceCache
			plugin.initHyperNodeResourceCache(ssn)

			// Override resource status for the first tier-1 HyperNode
			testHN := "hn-1-0"
			plugin.hyperNodeResourceCache[testHN].idle = tt.idleResource
			plugin.hyperNodeResourceCache[testHN].futureIdle = tt.futureIdleResource

			// Call hyperNodeGradientFn
			result, err := plugin.hyperNodeGradientFn(
				ssn,
				hyperNodesMap[tier2HNName],
				&scheduling.NetworkTopologySpec{HighestTierAllowed: &tt.highestAllowedTier},
				"",
				tt.minResource,
				tt.purpose,
			)

			assert.NoError(t, err)

			// Check if the test HyperNode is in the result
			found := false
			for _, tierHNs := range result {
				for _, hn := range tierHNs {
					if hn.Name == testHN {
						found = true
						break
					}
				}
				if found {
					break
				}
			}

			if tt.expectTier1Selected {
				assert.True(t, found, "expected HyperNode %s to be selected, but it was filtered", testHN)
			} else {
				assert.False(t, found, "expected HyperNode %s to be filtered, but it was selected", testHN)
			}

			// Verify gradient is sorted by tier (ascending)
			if len(result) > 1 {
				for i := 0; i < len(result)-1; i++ {
					if len(result[i]) > 0 && len(result[i+1]) > 0 {
						assert.LessOrEqual(t, result[i][0].Tier(), result[i+1][0].Tier(),
							"gradient should be sorted by tier in ascending order")
					}
				}
			}
		})
	}
}

func TestHyperNodeGradientForSubJobFn_NoSubJobPolicyRespectsHardTopology(t *testing.T) {
	schedulerCache := &cache.SchedulerCache{
		Nodes:             map[string]*api.NodeInfo{},
		Jobs:              map[api.JobID]*api.JobInfo{},
		Queues:            map[api.QueueID]*api.QueueInfo{},
		HyperNodesInfo:    api.NewHyperNodesInfo(nil),
		InUseNodesInShard: sets.Set[string]{},
		StatusUpdater:     &util.FakeStatusUpdater{},
		Recorder:          record.NewFakeRecorder(100),
	}
	ssn := framework.OpenSession(schedulerCache, nil, nil)
	defer framework.CloseSession(ssn)

	newNode := func(name string) *api.NodeInfo {
		return &api.NodeInfo{
			Name:        name,
			Allocatable: &api.Resource{MilliCPU: 8000},
			Used:        api.EmptyResource(),
			Releasing:   api.EmptyResource(),
			Pipelined:   api.EmptyResource(),
			Idle:        &api.Resource{MilliCPU: 8000},
		}
	}
	nodeA1 := newNode("node-a-1")
	nodeA2 := newNode("node-a-2")
	nodeD1 := newNode("node-d-1")
	nodeD2 := newNode("node-d-2")
	allNodes := []*api.NodeInfo{nodeA1, nodeA2, nodeD1, nodeD2}
	ssn.Nodes = map[string]*api.NodeInfo{
		nodeA1.Name: nodeA1,
		nodeA2.Name: nodeA2,
		nodeD1.Name: nodeD1,
		nodeD2.Name: nodeD2,
	}

	rootName := framework.ClusterTopHyperNode
	zoneA := api.NewHyperNodeInfo(api.BuildHyperNode("zone-a", 1, nil))
	zoneD := api.NewHyperNodeInfo(api.BuildHyperNode("zone-d", 1, nil))
	region := api.NewHyperNodeInfo(api.BuildHyperNode("region", 2, nil))
	root := api.NewHyperNodeInfo(api.BuildHyperNode(rootName, 3, nil))

	zoneA.Parent = region.Name
	zoneD.Parent = region.Name
	region.Parent = rootName
	region.Children = sets.New[string](zoneA.Name, zoneD.Name)
	root.Children = sets.New[string](region.Name)

	ssn.HyperNodes = map[string]*api.HyperNodeInfo{
		zoneA.Name:  zoneA,
		zoneD.Name:  zoneD,
		region.Name: region,
		rootName:    root,
	}
	ssn.HyperNodesSetByTier = map[int]sets.Set[string]{
		1: sets.New[string](zoneA.Name, zoneD.Name),
		2: sets.New[string](region.Name),
		3: sets.New[string](rootName),
	}
	ssn.HyperNodesTiers = []int{1, 2, 3}
	ssn.RealNodesSet = map[string]sets.Set[string]{
		zoneA.Name:  sets.New[string](nodeA1.Name, nodeA2.Name),
		zoneD.Name:  sets.New[string](nodeD1.Name, nodeD2.Name),
		region.Name: sets.New[string](nodeA1.Name, nodeA2.Name, nodeD1.Name, nodeD2.Name),
		rootName:    sets.New[string](nodeA1.Name, nodeA2.Name, nodeD1.Name, nodeD2.Name),
	}
	ssn.RealNodesList = map[string][]*api.NodeInfo{
		zoneA.Name:  {nodeA1, nodeA2},
		zoneD.Name:  {nodeD1, nodeD2},
		region.Name: allNodes,
		rootName:    allNodes,
	}

	highestTierAllowed := 1
	jobID := api.JobID("job-hard-no-subjob-policy")
	job := api.NewJobInfo(jobID)
	job.PodGroup = &api.PodGroup{
		PodGroup: scheduling.PodGroup{
			Spec: scheduling.PodGroupSpec{
				NetworkTopology: &scheduling.NetworkTopologySpec{
					Mode:               scheduling.HardNetworkTopologyMode,
					HighestTierAllowed: &highestTierAllowed,
				},
			},
		},
	}

	subJobPolicy := &scheduling.SubGroupPolicySpec{
		NetworkTopology: &scheduling.NetworkTopologySpec{
			Mode:               scheduling.HardNetworkTopologyMode,
			HighestTierAllowed: &highestTierAllowed,
		},
	}
	subJob := api.NewSubJobInfo(api.SubJobGID("gid"), api.SubJobID("sid"), jobID, subJobPolicy, nil)
	task := &api.TaskInfo{
		UID:        api.TaskID("pending-task"),
		Resreq:     &api.Resource{MilliCPU: 20000},
		InitResreq: &api.Resource{MilliCPU: 20000},
		TransactionContext: api.TransactionContext{
			Status: api.Pending,
		},
	}
	subJob.TaskStatusIndex[api.Pending] = api.TasksMap{task.UID: task}
	subJob.Tasks[task.UID] = task
	job.SubJobs[subJob.UID] = subJob
	ssn.Jobs[jobID] = job

	enabled := true
	ssn.Tiers = []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                     PluginName,
					EnabledHyperNodeGradient: &enabled,
				},
			},
		},
	}

	plugin, ok := New(framework.Arguments{}).(*networkTopologyAwarePlugin)
	if !ok {
		t.Fatalf("expected networkTopologyAwarePlugin type assertion to succeed")
	}
	plugin.OnSessionOpen(ssn)

	gradients := ssn.HyperNodeGradientForSubJobFn(subJob, ssn.HyperNodes[rootName], api.PurposeEvict)
	assert.Empty(t, gradients, "hard topology without feasible tier-1 domain should not fallback to root")
}

func TestNetworkTopologyAwareScoreMixedTreeUsesLocalDepth(t *testing.T) {
	newHyperNode := func(name string, tier int, children ...string) *api.HyperNodeInfo {
		info := api.NewHyperNodeInfo(api.BuildHyperNode(name, tier, nil))
		info.Children.Insert(children...)
		return info
	}

	hyperNodes := api.HyperNodeInfoMap{
		"shallow-leaf-0": newHyperNode("shallow-leaf-0", 1),
		"shallow-leaf-1": newHyperNode("shallow-leaf-1", 1),
		"shallow-root":   newHyperNode("shallow-root", 2, "shallow-leaf-0", "shallow-leaf-1"),
		"deep-leaf-0":    newHyperNode("deep-leaf-0", 1),
		"deep-leaf-1":    newHyperNode("deep-leaf-1", 1),
		"deep-middle":    newHyperNode("deep-middle", 2, "deep-leaf-0", "deep-leaf-1"),
		"deep-root":      newHyperNode("deep-root", 3, "deep-middle"),
		framework.ClusterTopHyperNode: newHyperNode(
			framework.ClusterTopHyperNode, 4, "shallow-root", "deep-root"),
	}
	for _, parent := range hyperNodes {
		for child := range parent.Children {
			hyperNodes[child].Parent = parent.Name
		}
	}

	plugin := &networkTopologyAwarePlugin{
		hyperNodesTier: &hyperNodesTier{minTier: 1, maxTier: 4},
	}
	ssn := &framework.Session{HyperNodes: hyperNodes}

	assert.InDelta(t, 0.5,
		plugin.networkTopologyAwareScore("shallow-leaf-1", "shallow-leaf-0", ssn), 1e-9,
		"the deeper deep tree must not inflate a shallow-local LCA score")
	assert.InDelta(t, 2.0/3.0,
		plugin.networkTopologyAwareScore("deep-leaf-1", "deep-leaf-0", ssn), 1e-9,
		"deep should retain its own three-level distance scale")
	assert.Equal(t, ZeroScore,
		plugin.networkTopologyAwareScore("deep-leaf-0", "shallow-leaf-0", ssn),
		"nodes in another connected tree must not receive an affinity score")
}
