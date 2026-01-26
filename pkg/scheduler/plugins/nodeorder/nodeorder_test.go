/*
Copyright 2025 The Kubernetes Authors.

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

package nodeorder

import (
	"os"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8sframework "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/imagelocality"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/interpodaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/nodeaffinity"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/noderesources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/podtopologyspread"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration"
	k8smetrics "k8s.io/kubernetes/pkg/scheduler/metrics"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestMain(m *testing.M) {
	options.Default()
	k8smetrics.Register()
	os.Exit(m.Run())
}

type nodeOrderTestCase struct {
	uthelper.TestCommonStruct
	LeastRequestedWeight    int
	MostRequestedWeight     int
	BalancedResourceWeight  int
	NodeAffinityWeight      int
	TaintTolerationWeight   int
	PodAffinityWeight       int
	PodTopologySpreadWeight int
	ImageLocalityWeight     int
}

func TestNodeOrderPlugin(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		PluginName:      New,
		gang.PluginName: gang.New,
	}

	tests := []nodeOrderTestCase{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "leastAllocated strategy",
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					util.BuildNode("n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("c1", 1, nil),
				},
				ExpectBindsNum: 1,
				ExpectBindMap: map[string]string{
					"c1/p1": "n2",
				},
			},
			LeastRequestedWeight: 1,
			MostRequestedWeight:  0,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "mostAllocated strategy",
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					util.BuildNode("n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("c1", 1, nil),
				},
				ExpectBindsNum: 1,
				ExpectBindMap: map[string]string{
					"c1/p1": "n1",
				},
			},
			LeastRequestedWeight: 0,
			MostRequestedWeight:  1,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "balanced allocation prefers balanced node",
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					// Request 1 CPU and 1Gi memory
					util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					// n1: 2 CPU, 2Gi memory - balanced
					util.BuildNode("n1", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					// n2: 4 CPU, 2Gi memory - imbalanced (more CPU)
					util.BuildNode("n2", api.BuildResourceList("4", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("c1", 1, nil),
				},
				ExpectBindsNum: 1,
				ExpectBindMap: map[string]string{
					"c1/p1": "n1", // n1 is more balanced
				},
			},
			BalancedResourceWeight: 10, // High weight for balanced allocation
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "node affinity prefers labeled node",
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					util.BuildPodWithAffinity("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string), &v1.Affinity{
						NodeAffinity: &v1.NodeAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
								{
									Weight: 100,
									Preference: v1.NodeSelectorTerm{
										MatchExpressions: []v1.NodeSelectorRequirement{
											{
												Key:      "zone",
												Operator: v1.NodeSelectorOpIn,
												Values:   []string{"zone1"},
											},
										},
									},
								},
							},
						},
					}),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"zone": "zone1"}),
					util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"zone": "zone2"}),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("c1", 1, nil),
				},
				ExpectBindsNum: 1,
				ExpectBindMap: map[string]string{
					"c1/p1": "n1", // n1 matches the preferred zone
				},
			},
			NodeAffinityWeight: 10, // High weight for node affinity
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "taint toleration prefers non-tainted node",
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					// Pod without toleration should prefer non-tainted node (n1) over tainted node (n2)
					// n2 has PreferNoSchedule taint
					util.BuildPodWithTolerations("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string), nil),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					{
						ObjectMeta: util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)).ObjectMeta,
						Spec: v1.NodeSpec{
							Taints: []v1.Taint{
								{
									Key:    "key1",
									Value:  "value1",
									Effect: v1.TaintEffectPreferNoSchedule,
								},
							},
						},
						Status: util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)).Status,
					},
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("c1", 1, nil),
				},
				ExpectBindsNum: 1,
				ExpectBindMap: map[string]string{
					"c1/p1": "n1", // n1 has no taints
				},
			},
			TaintTolerationWeight: 10, // High weight for taint toleration
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "inter-pod affinity prefers node with matching pod",
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					// Existing pod on n1
					util.BuildPod("c1", "p1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"service": "s1"}, make(map[string]string)),
					// Noise pod on n3 (different service, shouldn't attract)
					util.BuildPod("c1", "p-noise", "n3", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"service": "s2"}, make(map[string]string)),
					// Pending pod with affinity
					util.BuildPodWithAffinity("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string), &v1.Affinity{
						PodAffinity: &v1.PodAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []v1.WeightedPodAffinityTerm{
								{
									Weight: 100,
									PodAffinityTerm: v1.PodAffinityTerm{
										LabelSelector: &metav1.LabelSelector{
											MatchLabels: map[string]string{"service": "s1"},
										},
										TopologyKey: "kubernetes.io/hostname",
									},
								},
							},
						},
					}),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"kubernetes.io/hostname": "n1"}),
					util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"kubernetes.io/hostname": "n2"}),
					util.BuildNode("n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"kubernetes.io/hostname": "n3"}),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("c1", 1, nil),
				},
				ExpectBindsNum: 1, // p2 binds
				ExpectBindMap: map[string]string{
					"c1/p2": "n1", // n1 has the matching pod
				},
			},
			PodAffinityWeight: 10,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "pod topology spread prefers node to reduce skew",
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					// Existing pod on n1 (zone1)
					util.BuildPod("c1", "p1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"app": "foo"}, make(map[string]string)),
					// Existing pod on n1 (zone1)
					util.BuildPod("c1", "p2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"app": "foo"}, make(map[string]string)),
					// Existing pod on n2 (zone2)
					util.BuildPod("c1", "p3", "n2", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"app": "foo"}, make(map[string]string)),
					// Pending pod
					util.BuildPodWithTopologySpreadConstraints("c1", "p4", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"app": "foo"}, make(map[string]string), []v1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "zone",
							WhenUnsatisfiable: v1.ScheduleAnyway,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{"app": "foo"},
							},
						},
					}),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"zone": "zone1"}),
					util.BuildNode("n2", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"zone": "zone2"}),
					util.BuildNode("n3", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"zone": "zone3"}),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("c1", 1, nil),
				},
				ExpectBindsNum: 1,
				ExpectBindMap: map[string]string{
					"c1/p4": "n3", // n3 (zone3) is empty, n2 (zone2) has 1 pod and n1(zone1) has 2 pods. spread is better on n3.
				},
			},
			PodTopologySpreadWeight: 10,
		},
	}

	trueValue := true

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:            gang.PluginName,
							EnabledJobReady: &trueValue,
						},
						{
							Name:             PluginName,
							EnabledNodeOrder: &trueValue,
							Arguments: framework.Arguments{
								LeastRequestedWeight:    test.LeastRequestedWeight,
								MostRequestedWeight:     test.MostRequestedWeight,
								BalancedResourceWeight:  test.BalancedResourceWeight,
								NodeAffinityWeight:      test.NodeAffinityWeight,
								TaintTolerationWeight:   test.TaintTolerationWeight,
								PodAffinityWeight:       test.PodAffinityWeight,
								PodTopologySpreadWeight: test.PodTopologySpreadWeight,
								ImageLocalityWeight:     test.ImageLocalityWeight,
							},
						},
					},
				},
			}

			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()

			action := allocate.New()
			test.Run([]framework.Action{action})

			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInitPlugin(t *testing.T) {
	tests := []struct {
		name                    string
		weight                  priorityWeight
		expectNodeOrderKeys     []string
		expectNoNodeOrderKeys   []string
		expectScorePlugins      []string
		expectNotInScorePlugins []string
	}{
		{
			name:   "default weights",
			weight: calculateWeight(nil),
			expectNodeOrderKeys: []string{
				noderesources.Name + "_LeastAllocated",
				noderesources.BalancedAllocationName,
				nodeaffinity.Name,
				imagelocality.Name,
			},
			expectNoNodeOrderKeys: []string{
				noderesources.Name + "_MostAllocated",
			},
			expectScorePlugins: []string{
				interpodaffinity.Name,
				tainttoleration.Name,
				podtopologyspread.Name,
			},
			expectNotInScorePlugins: []string{},
		},
		{
			name:                  "all weights zero",
			weight:                priorityWeight{},
			expectNodeOrderKeys:   []string{},
			expectNoNodeOrderKeys: []string{noderesources.Name + "_LeastAllocated", noderesources.Name + "_MostAllocated", noderesources.BalancedAllocationName, nodeaffinity.Name, imagelocality.Name},
			expectScorePlugins:    []string{},
			expectNotInScorePlugins: []string{
				interpodaffinity.Name,
				tainttoleration.Name,
				podtopologyspread.Name,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pp := New(nil).(*NodeOrderPlugin)
			pp.weight = tt.weight

			nodeMap := map[string]k8sframework.NodeInfo{}
			client := k8sfake.NewSimpleClientset()
			informerFactory := informers.NewSharedInformerFactory(client, 0)
			pp.Handle = k8s.NewFramework(
				nodeMap,
				k8s.WithClientSet(client),
				k8s.WithInformerFactory(informerFactory),
			)

			pp.InitPlugin()

			// Verify NodeOrderScorePlugins
			for _, key := range tt.expectNodeOrderKeys {
				if _, exists := pp.NodeOrderScorePlugins[key]; !exists {
					t.Errorf("expected %s in NodeOrderScorePlugins, but not found", key)
				}
			}
			for _, key := range tt.expectNoNodeOrderKeys {
				if _, exists := pp.NodeOrderScorePlugins[key]; exists {
					t.Errorf("expected %s not in NodeOrderScorePlugins, but found", key)
				}
			}

			// Verify ScorePlugins
			for _, pluginName := range tt.expectScorePlugins {
				if _, exists := pp.ScorePlugins[pluginName]; !exists {
					t.Errorf("expected %s in ScorePlugins, but not found", pluginName)
				}
			}
			for _, pluginName := range tt.expectNotInScorePlugins {
				if _, exists := pp.ScorePlugins[pluginName]; exists {
					t.Errorf("expected %s not in ScorePlugins, but found", pluginName)
				}
			}
		})
	}
}
