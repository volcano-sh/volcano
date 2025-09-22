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
	"math"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

const (
	eps = 1e-1
)

func TestArguments(t *testing.T) {
	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	arguments := framework.Arguments{
		"weight": 2,
	}

	builder, ok := framework.GetPluginBuilder(PluginName)
	if !ok {
		t.Fatalf("should have plugin named %s", PluginName)
	}

	plugin := builder(arguments)
	networkTopologyAware, ok := plugin.(*networkTopologyAwarePlugin)
	if !ok {
		t.Fatalf("plugin should be %T, but not %T", networkTopologyAware, plugin)
	}
	weight := calculateWeight(networkTopologyAware.pluginArguments)
	if weight != 2 {
		t.Errorf("weight should be 2, but get %v", weight)
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
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("hard").
						HighestTierAllowed(3).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p4").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s3-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s3-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().State(schedulingv1.QueueStateOpen).Name("q1").Weight(1).Capability(nil).Obj(),
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
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s3").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("hard").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s3-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("s3-n2").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p3").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s3-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s3-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
			tasks: map[string]string{},
		},
		{
			name: "Tasks in job rescheduled, score nodes according to node hypernode LCA hyperNode tier",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s3").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("hard").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s3-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("s3-n2").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p3").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s3-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s3-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
				"s4-n1": 50.0,
				"s5-n1": 0.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier of the hyperNode and jobAllocatedHyperNode when hyperNodesInfo has two tier",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s1").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("hard").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s1-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s1-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s1-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s2-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s2-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().State(schedulingv1.QueueStateOpen).Name("q1").Weight(1).Capability(nil).Obj(),
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
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier of the hyperNode and jobAllocatedHyperNode when hyperNodesInfo has one tier",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s1").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("hard").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s1-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s1-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s1-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s2-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s2-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s3").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("hard").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s3-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("s3-n2").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p3").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s3-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s3-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
				"s4-n1": 100.0,
				"s5-n1": 0.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier and task num of the hyperNode when there are at least two hyperNodes have max hyperNode tier score",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s1").
						Queue("q1").
						MinMember(3).
						MinTaskMember(nil).
						Mode("hard").
						HighestTierAllowed(3).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s3-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("s3-n2").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p3").
						NodeName("s4-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p4").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s3-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s3-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
				"s3-n1": 100.0,
				"s4-n1": 75.0,
				"s5-n1": 0.0,
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
						Name:                  PluginName,
						EnabledHyperNodeOrder: &trueValue,
						EnabledNodeOrder:      &trueValue,
						Arguments:             test.arguments,
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

func parseJob(jobInfoMap map[api.JobID]*api.JobInfo) *api.JobInfo {
	for _, job := range jobInfoMap {
		return job
	}
	return nil
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
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("soft").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p4").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s3-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s3-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s3").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("soft").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s3-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("s3-n2").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p3").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s3-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s3-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s3").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("soft").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s3-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("s3-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p3").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s3-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s3-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
				"s4-n1": 50.0,
				"s5-n1": 0.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier of the hyperNode and jobAllocatedHyperNode when hyperNodesInfo has two tier",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s1").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("soft").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s1-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s1-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s1-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s2-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s2-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier of the hyperNode and jobAllocatedHyperNode when hyperNodesInfo has one tier",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s1").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("soft").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s1-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{

					util.MakeNode().
						Name("s1-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s1-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s2-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s2-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s3").
						Queue("q1").
						MinMember(1).
						MinTaskMember(nil).
						Mode("soft").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{

					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s3-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("s3-n2").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p3").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{

					util.MakeNode().
						Name("s3-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s3-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
				"s4-n1": 100.0,
				"s5-n1": 0.0,
			},
		},
		{
			name: "Tasks in job rescheduled, score hyperNodes according to node LCA hyperNode tier and task num of the hyperNode when there are at least two hyperNodes have max hyperNode tier score",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						HyperNodeName("s1").
						Queue("q1").
						MinMember(3).
						MinTaskMember(nil).
						Mode("soft").
						HighestTierAllowed(0).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("s3-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "master"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p2").
						NodeName("s3-n2").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p3").
						NodeName("s4-n1").
						PodPhase(v1.PodRunning).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
					util.MakePod().
						Namespace("c1").
						Name("p4").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("2", "4G")).
						GroupName("pg1").
						Labels(map[string]string{"volcano.sh/task-spec": "worker"}).
						NodeSelector(nil).
						Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("s3-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),

					util.MakeNode().
						Name("s3-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),

					util.MakeNode().
						Name("s4-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s4-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s5-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
					util.MakeNode().
						Name("s6-n2").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(nil).
						Obj(),
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
					util.MakeQueue().Name("q1").State(schedulingv1.QueueStateOpen).Weight(1).Capability(nil).Obj(),
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
				"s3-n1": 100.0,
				"s4-n1": 75.0,
				"s5-n1": 0.0,
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
						Name:                  PluginName,
						EnabledHyperNodeOrder: &trueValue,
						EnabledNodeOrder:      &trueValue,
						Arguments:             test.arguments,
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
		if task.Pod.Status.Phase == v1.PodPending {
			task.JobAllocatedHyperNode = jobAllocatedHyperNode
			return task
		}
	}
	return nil
}
