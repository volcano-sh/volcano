/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Rewritten tests using TestCommonStruct framework with comprehensive allocation scenarios
- Added TestAllocateWithNetWorkTopologies, BenchmarkAllocate, and other advanced test cases

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

package allocate

import (
	"fmt"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"k8s.io/kubernetes/pkg/features"
	"k8s.io/utils/ptr"
	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/binpack"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	networktopologyaware "volcano.sh/volcano/pkg/scheduler/plugins/network-topology-aware"
	"volcano.sh/volcano/pkg/scheduler/plugins/nodeorder"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestMain(m *testing.M) {
	options.Default()
	os.Exit(m.Run())
}

func TestParseArgs(t *testing.T) {
	test := uthelper.TestCommonStruct{Name: "set cache false"}

	action := New()
	test.RegisterSession(nil, []conf.Configuration{{Name: action.Name(),
		Arguments: map[string]interface{}{conf.EnablePredicateErrCacheKey: false}}})
	test.Run([]framework.Action{action})
	assert.False(t, action.enablePredicateErrorCache)
}

func TestAllocate(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		drf.PluginName:        drf.New,
		proportion.PluginName: proportion.New,
		predicates.PluginName: predicates.New,
		nodeorder.PluginName:  nodeorder.New,
		gang.PluginName:       gang.New,
	}
	tests := []uthelper.TestCommonStruct{
		{
			Name: "prepredicate failed: node selector does not match",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 1, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p2": "n1",
			},
			ExpectBindsNum: 1,
		},
		{
			Name: "prepredicate failed and tasks are not used up, continue on until min member meet",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 2, map[string]int32{"master": 1, "worker": 1}, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p0", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "master"}),
				util.BuildNode("n2", api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p0": "n1",
				"c1/p2": "n2",
			},
			ExpectBindsNum: 2,
		},
		{
			Name: "master's min member can not be allocated, break from allocating",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 2, map[string]int32{"master": 2, "worker": 0}, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p0", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "master"}),
				util.BuildNode("n2", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			ExpectBindMap:  map[string]string{},
			ExpectBindsNum: 0,
		},
		{
			Name: "one Job with two Pods on one node",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p1": "n1",
				"c1/p2": "n1",
			},
			ExpectBindsNum: 2,
		},
		{
			Name: "two Jobs on one node",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				util.BuildPodGroup("pg2", "c2", "c2", 0, nil, schedulingv1.PodGroupInqueue),
			},

			// pod name should be like "*-*-{index}",
			// due to change of TaskOrderFn
			Pods: []*v1.Pod{
				// pending pod with owner1, under c1
				util.BuildPod("c1", "pg1-p-1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				// pending pod with owner1, under c1
				util.BuildPod("c1", "pg1-p-2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				// pending pod with owner2, under c2
				util.BuildPod("c2", "pg2-p-1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				// pending pod with owner2, under c2
				util.BuildPod("c2", "pg2-p-2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
				util.BuildQueue("c2", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c2/pg2-p-1": "n1",
				"c1/pg1-p-1": "n1",
			},
			ExpectBindsNum: 2,
		},
		{
			Name: "high priority queue should not block others",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				util.BuildPodGroup("pg2", "c1", "c2", 0, nil, schedulingv1.PodGroupInqueue),
			},

			Pods: []*v1.Pod{
				// pending pod with owner1, under ns:c1/q:c1
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("3", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				// pending pod with owner2, under ns:c1/q:c2
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
				util.BuildQueue("c2", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p2": "n1",
			},
			ExpectBindsNum: 1,
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:               drf.PluginName,
					EnabledPreemptable: &trueValue,
					EnabledJobOrder:    &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledQueueOrder:  &trueValue,
					EnabledReclaimable: &trueValue,
					EnabledAllocatable: &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:             nodeorder.PluginName,
					EnabledNodeOrder: &trueValue,
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// BenchmarkAllocate can help analyze the performance differences before and after changes to the scheduling framework. Currently, it is hardcoded to schedule 1000 pods
func BenchmarkAllocate(b *testing.B) {
	plugins := map[string]framework.PluginBuilder{
		drf.PluginName:        drf.New,
		proportion.PluginName: proportion.New,
		predicates.PluginName: predicates.New,
		nodeorder.PluginName:  nodeorder.New,
		gang.PluginName:       gang.New,
	}

	// Create 1 pod group
	podGroups := []*schedulingv1.PodGroup{
		util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
	}

	// Create 1000 pods
	numPods := 1000
	pods := make([]*v1.Pod, 0, numPods)
	for i := 0; i < numPods; i++ {
		podName := fmt.Sprintf("p%d", i+1)
		pods = append(pods, util.BuildPod("c1", podName, "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)))
	}

	nodes := []*v1.Node{
		util.BuildNode("n1", api.BuildResourceList("2000", "4000Gi", []api.ScalarResource{{Name: "pods", Value: "2000"}}...), make(map[string]string)),
	}
	queues := []*schedulingv1.Queue{
		util.BuildQueue("c1", 1, nil),
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:               drf.PluginName,
					EnabledPreemptable: &trueValue,
					EnabledJobOrder:    &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledQueueOrder:  &trueValue,
					EnabledReclaimable: &trueValue,
					EnabledAllocatable: &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:             nodeorder.PluginName,
					EnabledNodeOrder: &trueValue,
				},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testStruct := uthelper.TestCommonStruct{
			Name:      "benchmark-allocate",
			Plugins:   plugins,
			PodGroups: podGroups,
			Pods:      pods,
			Nodes:     nodes,
			Queues:    queues,
		}
		testStruct.RegisterSession(tiers, nil)
		action := New()
		testStruct.Run([]framework.Action{action})
		testStruct.Close()
	}
}

func TestAllocateWithNetWorkTopologies(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		predicates.PluginName:           predicates.New,
		gang.PluginName:                 gang.New,
		networktopologyaware.PluginName: networktopologyaware.New,
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "soft network topology constrain, can allocate job when resources are enough",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "soft", 0),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{0: sets.New[string]("s0", "s1"), 1: sets.New[string]("s2")},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			MinimalBindCheck: true,
			ExpectBindsNum:   3,
		},
		{
			Name: "soft network topology constrain, can allocate job when minavailable < replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "soft network topology constrain, two available hyperNodes, can allocate job to nodes with affinity",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "master"}),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{0: sets.New[string]("s0", "s1")},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1"),
				"s1": sets.New[string]("s1-n2"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p1": "s1-n2",
			},
			ExpectBindsNum: 1,
		},
		{
			Name: "soft network topology constrain and tasks in job rescheduled, can allocate job when resources are enough",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s0", "q1", 2, nil, schedulingv1.PodGroupInqueue, "soft", 0),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "soft network topology constrain and tasks in job rescheduled, can allocate job when resources are enough and minavailable = replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s0", "q1", 3, nil, schedulingv1.PodGroupInqueue, "soft", 0),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "soft network topology constrain and tasks in job rescheduled, can allocate job when cross highestTierAllowed tier and hyperNodesInfo has three tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 2, nil, schedulingv1.PodGroupInqueue, "soft", 0),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
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
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain, can not allocate job when cross highestTierAllowed tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 1),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},

			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain, can allocate job when highestTierAllowed not reached",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   3,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain, can allocate job when multi hyperNodes are available",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 1),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain, can allocate job when minavailable < replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 1),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain, two available hyperNodes, can allocate job to nodes with affinity",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 1),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "master"}),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{0: sets.New[string]("s0", "s1")},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1"),
				"s1": sets.New[string]("s1-n2"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p1": "s1-n2",
			},
			ExpectBindsNum: 1,
		},
		{
			Name: "hard network topology constrain and tasks in job rescheduled, can allocate job when highestTierAllowed not reached",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s0", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain and tasks in job rescheduled, can allocate job when highestTierAllowed not reached and minavailable = replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s0", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain and tasks in job rescheduled, can allocate job when highestTierAllowed not reached and hyperNodesInfo has three tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
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
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain and tasks in job rescheduled, can not allocate job when cross highestTierAllowed tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s0", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 1),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain and tasks in job rescheduled, can not allocate job when cross highestTierAllowed tier and hyperNodesInfo has three tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s3", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 1),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
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
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain and tasks in job rescheduled, can allocate job when LCAHyperNode is empty",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "s0", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain, can not allocate job when cross highestTierName tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupUsingNetWorkTopologiesWithTierName("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", "volcano.sh/task-spec"),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier:   map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesTierNameMap: api.HyperNodeTierNameMap{"volcano.sh/task-spec": 1, "volcano.sh/job-spec": 2},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s2", 2, "volcano.sh/job-spec", []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain, can allocate job when the tier corresponding to highestTierName not reached",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupUsingNetWorkTopologiesWithTierName("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", "volcano.sh/job-spec"),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier:   map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesTierNameMap: api.HyperNodeTierNameMap{"volcano.sh/task-spec": 1, "volcano.sh/job-spec": 2},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s2", 2, "volcano.sh/job-spec", []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   3,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain, can allocate job when multi hyperNodes are available",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupUsingNetWorkTopologiesWithTierName("pg1", "c1", "", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", "volcano.sh/task-spec"),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier:   map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesTierNameMap: api.HyperNodeTierNameMap{"volcano.sh/task-spec": 1, "volcano.sh/job-spec": 2},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s2", 2, "volcano.sh/job-spec", []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain and task in job rescheduled, can allocate job when the tier corresponding to highestTierName not reached",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupUsingNetWorkTopologiesWithTierName("pg1", "c1", "s0", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", "volcano.sh/job-spec"),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier:   map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesTierNameMap: api.HyperNodeTierNameMap{"volcano.sh/task-spec": 1, "volcano.sh/job-spec": 2},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s2", 2, "volcano.sh/job-spec", []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain and task in job rescheduled, can allocate job when the tier corresponding to highestTierName not reached and minavailable = replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupUsingNetWorkTopologiesWithTierName("pg1", "c1", "s0", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", "volcano.sh/job-spec"),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier:   map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesTierNameMap: api.HyperNodeTierNameMap{"volcano.sh/task-spec": 1, "volcano.sh/job-spec": 2},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s2", 2, "volcano.sh/job-spec", []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain and task in job rescheduled, can allocate job when the tier corresponding to highestTierName not reached and hyperNodesInfo has three tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupUsingNetWorkTopologiesWithTierName("pg1", "c1", "s3", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", "volcano.sh/job-spec"),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
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
			HyperNodesTierNameMap: api.HyperNodeTierNameMap{"volcano.sh/task-spec": 1, "volcano.sh/job-spec": 2, "volcano.sh/hypernode-spec": 3},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s0", 3, "volcano.sh/hypernode-spec", []api.MemberConfig{
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
				"s1": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 2, "volcano.sh/job-spec", []api.MemberConfig{
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
				"s2": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s2", 2, "volcano.sh/job-spec", []api.MemberConfig{
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
				"s3": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s3", 1, "volcano.sh/task-spec", []api.MemberConfig{
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
				"s4": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s4", 1, "volcano.sh/task-spec", []api.MemberConfig{
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
				"s5": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s5", 1, "volcano.sh/task-spec", []api.MemberConfig{
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
				"s6": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s6", 1, "volcano.sh/task-spec", []api.MemberConfig{
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
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology constrain and tasks in job rescheduled, can not allocate job when cross the tier corresponding to highestTierName",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupUsingNetWorkTopologiesWithTierName("pg1", "c1", "s0", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", "volcano.sh/task-spec"),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier:   map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesTierNameMap: api.HyperNodeTierNameMap{"volcano.sh/task-spec": 1, "volcano.sh/job-spec": 2},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s0", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s2", 2, "volcano.sh/job-spec", []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "hard network topology with highestTierName constrain and tasks in job rescheduled, can allocate job when here is no corresponding highestTierAllowed for highestTierName",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupUsingNetWorkTopologiesWithTierName("pg1", "c1", "s0", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", "volcano.sh/hypercluster"),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier:   map[int]sets.Set[string]{1: sets.New[string]("s0", "s1")},
			HyperNodesTierNameMap: api.HyperNodeTierNameMap{"volcano.sh/task-spec": 1},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s0", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNodeWithTierName("s1", 1, "volcano.sh/task-spec", []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:                     networktopologyaware.PluginName,
					EnabledNodeOrder:         &trueValue,
					EnabledHyperNodeOrder:    &trueValue,
					EnabledHyperNodeGradient: &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNodeLevelScoreWithNetWorkTopologies(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		predicates.PluginName:           predicates.New,
		gang.PluginName:                 gang.New,
		binpack.PluginName:              binpack.New,
		networktopologyaware.PluginName: networktopologyaware.New,
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "hard network topology constrain, allocate job to highest score hypeNode with node level binpack",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 1),
				util.BuildPodGroupWithNetWorkTopologies("pg2", "c1", "", "q1", 2, nil, schedulingv1.PodGroupRunning, "", 1),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("4", "8Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),

				util.BuildPod("c1", "p3", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg2", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p4", "s0-n2", v1.PodRunning, api.BuildResourceList("4", "8Gi"), "pg2", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("8", "16Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("8", "16Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum: 2,
			// "s0-n1" and "s0-n2" nodes have running pods, so get higher score when enable binpack.
			ExpectBindMap: map[string]string{
				"c1/p1": "s0-n1",
				"c1/p2": "s0-n2",
			},
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:             binpack.PluginName,
					EnabledNodeOrder: &trueValue,
				},
				{
					Name:                     networktopologyaware.PluginName,
					EnabledNodeOrder:         &trueValue,
					EnabledHyperNodeOrder:    &trueValue,
					EnabledHyperNodeGradient: &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestHyperNodeBinpackWithNetWorkTopologies(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		predicates.PluginName:           predicates.New,
		gang.PluginName:                 gang.New,
		networktopologyaware.PluginName: networktopologyaware.New,
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "hard network topology constrain, allocate task to node under LCAHyperNode",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 4, nil, schedulingv1.PodGroupInqueue, "hard", 2),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("4", "8Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("4", "8Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("4", "8Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("4", "8Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s2-n5", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s2-n6", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				}), api.ParentOpt("s3")),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				}), api.ParentOpt("s3")),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 1, []api.MemberConfig{
					{
						Name:     "s2-n5",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s2-n6",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				}), api.ParentOpt("s3")),
				"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
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
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1", "s2"), 2: sets.New[string]("s3")},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s2-n5", "s2-n6"),
				"s3": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s2-n5", "s1-n4", "s2-n6"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum: 4,
			// "s1-n3" and "s1-n4" nodes with same LCAHyperNode "s1", so when c1/p3 assigned to "s1-n3", c1/p3 will be assigned to "s1-n4"
			ExpectBindNumsInHyperNode: []int{2, 2},
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:                     networktopologyaware.PluginName,
					EnabledNodeOrder:         &trueValue,
					EnabledHyperNodeOrder:    &trueValue,
					EnabledHyperNodeGradient: &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckBindInHyperNode(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFareShareAllocate(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		drf.PluginName:        drf.New,
		proportion.PluginName: proportion.New,
		predicates.PluginName: predicates.New,
		nodeorder.PluginName:  nodeorder.New,
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "queue with low DRF share value has high priority, should allocate first",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg-small-1", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupRunning),
				util.BuildPodGroup("pg-large-1", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupInqueue),
				util.BuildPodGroup("pg-large-2", "ns-1", "q-2", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{ // allocate order: q-2/pg-large-2, q-1/pg-large-1
				util.BuildPod("ns-1", "pod-small-1", "node-1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg-small-1", make(map[string]string), make(map[string]string)),
				util.BuildPod("ns-1", "pod-large-1", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg-large-1", make(map[string]string), make(map[string]string)),
				util.BuildPod("ns-1", "pod-large-2", "", v1.PodPending, api.BuildResourceList("3", "2G"), "pg-large-2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("node-1", api.BuildResourceList("5", "5G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q-1", 1, nil),
				util.BuildQueue("q-2", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"ns-1/pod-large-2": "node-1",
			},
			ExpectBindsNum: 1,
		},
		{
			Name: "queue's DRF share value will be updated and its priority will change before it is put back into the priority queue",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg-small-1", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupRunning),
				util.BuildPodGroup("pg-large-1", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupInqueue),
				util.BuildPodGroup("pg-small-2", "ns-1", "q-2", 0, nil, schedulingv1.PodGroupInqueue),
				util.BuildPodGroup("pg-large-2", "ns-1", "q-2", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{ // allocate order: q-2/pg-large-2, q-1/pg-large-1, q-2/pg-small-2
				util.BuildPod("ns-1", "pod-small-1", "node-1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg-small-1", make(map[string]string), make(map[string]string)),
				util.BuildPod("ns-1", "pod-large-1", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg-large-1", make(map[string]string), make(map[string]string)),
				util.BuildPod("ns-1", "pod-large-2", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg-large-2", make(map[string]string), make(map[string]string)),
				util.BuildPod("ns-1", "pod-small-2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg-small-2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("node-1", api.BuildResourceList("5", "5G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q-1", 1, nil),
				util.BuildQueue("q-2", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"ns-1/pod-large-1": "node-1",
				"ns-1/pod-large-2": "node-1",
			},
			ExpectBindsNum: 2,
		},
		{
			Name: "queue's one jobs has no pending tasks, should be put back to queues for next job",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg-1", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupRunning),
				util.BuildPodGroup("pg-2", "ns-1", "q-1", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPod("ns-1", "pod-1", "node-1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg-1", nil, nil),
				util.BuildPod("ns-1", "pod-2", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg-2", nil, nil),
			},
			Nodes:  []*v1.Node{util.BuildNode("node-1", api.BuildResourceList("5", "5G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))},
			Queues: []*schedulingv1.Queue{util.BuildQueue("q-1", 1, nil)},
			ExpectBindMap: map[string]string{
				"ns-1/pod-2": "node-1",
			},
			ExpectBindsNum: 1,
		},
	}
	trueValue := true
	falseValue := false
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               drf.PluginName,
					EnabledPreemptable: &trueValue,
					EnabledJobOrder:    &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledQueueOrder:  &trueValue,
					EnabledReclaimable: &trueValue,
					EnabledAllocatable: &falseValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:             nodeorder.PluginName,
					EnabledNodeOrder: &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAllocateWithPVC(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		gang.PluginName:       gang.New,
		predicates.PluginName: predicates.New,
	}
	options.ServerOpts = &options.ServerOption{
		MinNodesToFind:             100,
		MinPercentageOfNodesToFind: 5,
		PercentageOfNodesToFind:    100,
	}

	sc := util.BuildStorageClass("sc", "ignore-provisioner", storagev1.VolumeBindingWaitForFirstConsumer)
	pvc1 := util.BuildPVC("c1", "pvc1", v1.ResourceList{v1.ResourceStorage: resource.MustParse("1Gi")}, "sc")
	pvc2 := util.BuildPVC("c1", "pvc2", v1.ResourceList{v1.ResourceStorage: resource.MustParse("1Gi")}, "sc")
	pv1 := util.BuildPV("pv1", "sc", v1.ResourceList{v1.ResourceStorage: resource.MustParse("2Gi")})
	pv2 := util.BuildPV("pv2", "sc", v1.ResourceList{v1.ResourceStorage: resource.MustParse("2Gi")})

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobReady:     &trueValue,
					EnabledPredicate:    &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledTaskOrder:    &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
			},
		},
	}

	ignoreProvisioners := sets.New[string]("ignore-provisioner")

	tests := []uthelper.TestCommonStruct{
		{
			Name: "static pv matched but node without enough resource",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 2, map[string]int32{"": 2}, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPodWithPVC("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), pvc1, "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPodWithPVC("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), pvc2, "pg1", make(map[string]string), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			SCs:  []*storagev1.StorageClass{sc},
			PVs:  []*v1.PersistentVolume{pv1},
			PVCs: []*v1.PersistentVolumeClaim{pvc1, pvc2},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupInqueue,
			},
			IgnoreProvisioners: ignoreProvisioners,
		},
		// This test case may have error logs, mainly because of the binding PV and PVC depends on pv-controller.
		// The mock pv-controller in the UT is too complex and requires accurate timing to trigger the binding of PV and PVC,
		// so here the UT only verifies the status of podgroup
		{
			Name: "static pv matched and node with enough resources",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 2, map[string]int32{"": 2}, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPodWithPVC("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), pvc1, "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPodWithPVC("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), pvc2, "pg1", make(map[string]string), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			SCs:                []*storagev1.StorageClass{sc},
			PVs:                []*v1.PersistentVolume{pv1, pv2},
			PVCs:               []*v1.PersistentVolumeClaim{pvc1, pvc2},
			IgnoreProvisioners: ignoreProvisioners,
			ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
				"c1/pg1": {api.Binding: 2},
			},
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			predicates.ResetVolumeBindingPluginForTest()
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			action := New()
			test.Run([]framework.Action{action})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAllocateWithDRA(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.DynamicResourceAllocation, true)

	plugins := map[string]framework.PluginBuilder{
		gang.PluginName:       gang.New,
		predicates.PluginName: predicates.New,
	}

	options.ServerOpts = &options.ServerOption{
		MinNodesToFind:             100,
		MinPercentageOfNodesToFind: 5,
		PercentageOfNodesToFind:    100,
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobReady:     &trueValue,
					EnabledPredicate:    &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledTaskOrder:    &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
					Arguments: framework.Arguments{
						predicates.DynamicResourceAllocationEnable: trueValue,
					},
				},
			},
		},
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "Allocate normal resourceClaim successfully",
			ResourceClaims: []*resourcev1.ResourceClaim{
				util.BuildResourceClaim("c1", "claim1",
					[]resourcev1.DeviceRequest{util.BuildDeviceRequest("gpu", "gpu.example.com", nil, nil, nil)},
					nil, nil),
			},
			ResourceSlices: []*resourcev1.ResourceSlice{
				util.BuildResourceSlice("n1-slice1", "gpu.example.com", "n1", resourcev1.ResourcePool{Name: "gpu-worker", Generation: 1, ResourceSliceCount: 1},
					[]resourcev1.Device{
						util.BuildDevice("gpu-1", nil, nil),
					}),
			},
			DeviceClasses: []*resourcev1.DeviceClass{
				util.BuildDeviceClass("gpu.example.com", []resourcev1.DeviceSelector{
					{CEL: &resourcev1.CELDeviceSelector{
						Expression: fmt.Sprintf(`device.driver == 'gpu.example.com'`),
					}},
				}, nil),
			},
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 1, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPodWithResourceClaim("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string),
					[]v1.ResourceClaim{{Name: "gpu", Request: "gpu"}}, []v1.PodResourceClaim{{Name: "gpu", ResourceClaimName: ptr.To("claim1")}}),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
				"c1/pg1": {api.Binding: 1},
			},
			ExpectBindMap: map[string]string{
				"c1/p1": "n1",
			},
			ExpectBindsNum: 1,
		},
		{
			Name: "claim cel runtime errors",
			ResourceClaims: []*resourcev1.ResourceClaim{
				util.BuildResourceClaim("c1", "claim1",
					[]resourcev1.DeviceRequest{util.BuildDeviceRequest("gpu", "gpu.example.com", nil, nil, nil)},
					nil, nil),
			},
			ResourceSlices: []*resourcev1.ResourceSlice{
				util.BuildResourceSlice("n1-slice1", "gpu.example.com", "n1", resourcev1.ResourcePool{Name: "gpu-worker", Generation: 1, ResourceSliceCount: 1},
					[]resourcev1.Device{
						util.BuildDevice("gpu-1", nil, nil),
					}),
			},
			DeviceClasses: []*resourcev1.DeviceClass{
				util.BuildDeviceClass("gpu.example.com", []resourcev1.DeviceSelector{
					{CEL: &resourcev1.CELDeviceSelector{
						Expression: fmt.Sprintf(`device.attributes["%s"].%s`, "some-driver", resourcev1.QualifiedName("healthy")),
					}},
				}, nil),
			},
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 1, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPodWithResourceClaim("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string),
					[]v1.ResourceClaim{{Name: "gpu", Request: "gpu"}}, []v1.PodResourceClaim{{Name: "gpu", ResourceClaimName: ptr.To("claim1")}}),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupInqueue,
			},
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			action := New()
			test.Run([]framework.Action{action})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func lessFn(a, b interface{}) bool {
	return a.(int) < b.(int)
}

func drain(q *util.PriorityQueue) []interface{} {
	items := make([]interface{}, 0, q.Len())
	for q.Len() > 0 {
		items = append(items, q.Pop())
	}
	return items
}

func TestWorksheetShallowCopy(t *testing.T) {
	// Test JobWorksheet shallow copy
	w1 := &JobWorksheet{
		subJobs:          util.NewPriorityQueue(lessFn),
		subJobWorksheets: make(map[api.SubJobID]*SubJobWorksheet),
	}
	w2 := &JobWorksheet{
		subJobs:          util.NewPriorityQueue(lessFn),
		subJobWorksheets: make(map[api.SubJobID]*SubJobWorksheet),
	}
	w2.subJobs.Push(1)
	w1.ShallowCopyFrom(w2)
	if diff := cmp.Diff(drain(w1.subJobs.Clone()), drain(w2.subJobs.Clone())); diff != "" {
		t.Errorf("Expected w1 to be equal to w2 after shallow copy, but they are not.")
	}

	// Test SubJobWorksheet shallow copy for leaf node
	leafWorksheet1 := &SubJobWorksheet{
		isLeaf: true,
		tasks:  util.NewPriorityQueue(lessFn),
	}
	leafWorksheet2 := &SubJobWorksheet{
		isLeaf: true,
		tasks:  util.NewPriorityQueue(lessFn),
	}
	leafWorksheet2.tasks.Push(2)
	leafWorksheet1.ShallowCopyFrom(leafWorksheet2)

	if leafWorksheet1.isLeaf != leafWorksheet2.isLeaf {
		t.Errorf("Expected leafWorksheet1.isLeaf to be %v, got %v", leafWorksheet2.isLeaf, leafWorksheet1.isLeaf)
	}
	if diff := cmp.Diff(drain(leafWorksheet1.tasks.Clone()), drain(leafWorksheet2.tasks.Clone())); diff != "" {
		t.Errorf("Expected leafWorksheet1.tasks to be equal to leafWorksheet2.tasks after shallow copy, but they are not.")
	}

	// Test SubJobWorksheet shallow copy for non-leaf node
	childWorksheet := &SubJobWorksheet{
		isLeaf: true,
		tasks:  util.NewPriorityQueue(lessFn),
	}
	childWorksheet.tasks.Push(100)

	nonLeafWorksheet1 := &SubJobWorksheet{
		isLeaf:           false,
		subJobs:          util.NewPriorityQueue(lessFn),
		subJobWorksheets: make(map[api.SubJobID]*SubJobWorksheet),
	}
	nonLeafWorksheet2 := &SubJobWorksheet{
		isLeaf:  false,
		subJobs: util.NewPriorityQueue(lessFn),
		subJobWorksheets: map[api.SubJobID]*SubJobWorksheet{
			"child1": childWorksheet,
		},
	}
	nonLeafWorksheet2.subJobs.Push(3)
	nonLeafWorksheet1.ShallowCopyFrom(nonLeafWorksheet2)

	if nonLeafWorksheet1.isLeaf != nonLeafWorksheet2.isLeaf {
		t.Errorf("Expected nonLeafWorksheet1.isLeaf to be %v, got %v", nonLeafWorksheet2.isLeaf, nonLeafWorksheet1.isLeaf)
	}
	if diff := cmp.Diff(drain(nonLeafWorksheet1.subJobs.Clone()), drain(nonLeafWorksheet2.subJobs.Clone())); diff != "" {
		t.Errorf("Expected nonLeafWorksheet1.subJobs to be equal to nonLeafWorksheet2.subJobs after shallow copy, but they are not.")
	}
	if len(nonLeafWorksheet1.subJobWorksheets) != len(nonLeafWorksheet2.subJobWorksheets) {
		t.Errorf("Expected nonLeafWorksheet1.subJobWorksheets to have %d entries, got %d", len(nonLeafWorksheet2.subJobWorksheets), len(nonLeafWorksheet1.subJobWorksheets))
	}
	if nonLeafWorksheet1.subJobWorksheets["child1"] != nonLeafWorksheet2.subJobWorksheets["child1"] {
		t.Errorf("Expected nonLeafWorksheet1.subJobWorksheets to point to same child worksheet (shallow copy)")
	}

	// Test shallow copy from nil
	nilTestWorksheet := &SubJobWorksheet{
		isLeaf: true,
		tasks:  util.NewPriorityQueue(lessFn),
	}
	nilTestWorksheet.tasks.Push(999)
	nilTestWorksheet.ShallowCopyFrom(nil)
	if nilTestWorksheet.tasks.Len() != 1 {
		t.Errorf("ShallowCopyFrom(nil) should not modify worksheet, but tasks changed")
	}
}

func TestWorksheetEmpty(t *testing.T) {
	// Test JobWorksheet.Empty() with nil subJobs
	w := &JobWorksheet{
		subJobs:          nil,
		subJobWorksheets: make(map[api.SubJobID]*SubJobWorksheet),
	}
	if !w.Empty() {
		t.Errorf("Expected w to be empty when subJobs is nil, but it is not.")
	}

	// Test JobWorksheet.Empty() with empty subJobs queue
	w2 := &JobWorksheet{
		subJobs:          util.NewPriorityQueue(lessFn),
		subJobWorksheets: make(map[api.SubJobID]*SubJobWorksheet),
	}
	if !w2.Empty() {
		t.Errorf("Expected w2 to be empty when subJobs queue is empty, but it is not.")
	}

	// Test JobWorksheet.Empty() with non-empty subJobs
	w3 := &JobWorksheet{
		subJobs:          util.NewPriorityQueue(lessFn),
		subJobWorksheets: make(map[api.SubJobID]*SubJobWorksheet),
	}
	w3.subJobs.Push(1)
	if w3.Empty() {
		t.Errorf("Expected w3 to be non-empty when subJobs has items, but Empty() returned true.")
	}

	// Test SubJobWorksheet.Empty() for leaf node with nil tasks
	leafEmpty := &SubJobWorksheet{
		isLeaf: true,
		tasks:  nil,
	}
	if !leafEmpty.Empty() {
		t.Errorf("Expected leafEmpty to be empty when isLeaf=true and tasks is nil, but it is not.")
	}

	// Test SubJobWorksheet.Empty() for leaf node with empty tasks queue
	leafEmpty2 := &SubJobWorksheet{
		isLeaf: true,
		tasks:  util.NewPriorityQueue(lessFn),
	}
	if !leafEmpty2.Empty() {
		t.Errorf("Expected leafEmpty2 to be empty when isLeaf=true and tasks queue is empty, but it is not.")
	}

	// Test SubJobWorksheet.Empty() for leaf node with non-empty tasks
	leafNonEmpty := &SubJobWorksheet{
		isLeaf: true,
		tasks:  util.NewPriorityQueue(lessFn),
	}
	leafNonEmpty.tasks.Push(100)
	if leafNonEmpty.Empty() {
		t.Errorf("Expected leafNonEmpty to be non-empty when isLeaf=true and tasks has items, but Empty() returned true.")
	}

	// Test SubJobWorksheet.Empty() for non-leaf node with nil subJobs
	nonLeafEmpty := &SubJobWorksheet{
		isLeaf:           false,
		subJobs:          nil,
		subJobWorksheets: make(map[api.SubJobID]*SubJobWorksheet),
	}
	if !nonLeafEmpty.Empty() {
		t.Errorf("Expected nonLeafEmpty to be empty when isLeaf=false and subJobs is nil, but it is not.")
	}

	// Test SubJobWorksheet.Empty() for non-leaf node with empty subJobs queue
	nonLeafEmpty2 := &SubJobWorksheet{
		isLeaf:           false,
		subJobs:          util.NewPriorityQueue(lessFn),
		subJobWorksheets: make(map[api.SubJobID]*SubJobWorksheet),
	}
	if !nonLeafEmpty2.Empty() {
		t.Errorf("Expected nonLeafEmpty2 to be empty when isLeaf=false and subJobs queue is empty, but it is not.")
	}

	// Test SubJobWorksheet.Empty() for non-leaf node with non-empty subJobs
	nonLeafNonEmpty := &SubJobWorksheet{
		isLeaf:  false,
		subJobs: util.NewPriorityQueue(lessFn),
		subJobWorksheets: map[api.SubJobID]*SubJobWorksheet{
			"child": {isLeaf: true, tasks: util.NewPriorityQueue(lessFn)},
		},
	}
	nonLeafNonEmpty.subJobs.Push(1)
	if nonLeafNonEmpty.Empty() {
		t.Errorf("Expected nonLeafNonEmpty to be non-empty when isLeaf=false and subJobs has items, but Empty() returned true.")
	}
}

func TestWorksheetClone(t *testing.T) {
	// Test JobWorksheet.Clone() with basic structure
	w := &JobWorksheet{
		subJobs:          util.NewPriorityQueue(lessFn),
		subJobWorksheets: make(map[api.SubJobID]*SubJobWorksheet),
	}
	w.subJobs.Push(1)
	wClone := w.Clone()
	if diff := cmp.Diff(drain(w.Clone().subJobs), drain(wClone.subJobs)); diff != "" {
		t.Errorf("Cloned subJobs queue mismatch (-want +got): %s", diff)
	}

	// Test JobWorksheet.Clone() with SubJobWorksheets
	w2 := &JobWorksheet{
		subJobs: util.NewPriorityQueue(lessFn),
		subJobWorksheets: map[api.SubJobID]*SubJobWorksheet{
			"subJobID": {
				isLeaf: true,
				tasks:  util.NewPriorityQueue(lessFn),
			},
		},
	}
	w2.subJobs.Push(10)
	w2.subJobWorksheets["subJobID"].tasks.Push(100)
	w2Clone := w2.Clone()
	if diff := cmp.Diff(drain(w2.Clone().subJobs), drain(w2Clone.subJobs)); diff != "" {
		t.Errorf("Cloned w2.subJobs queue mismatch (-want +got): %s", diff)
	}
	if diff := cmp.Diff(drain(w2.subJobWorksheets["subJobID"].Clone().tasks), drain(w2Clone.subJobWorksheets["subJobID"].tasks)); diff != "" {
		t.Errorf("Cloned w2.subJobWorksheets tasks queue mismatch (-want +got): %s", diff)
	}
	// Verify it's a deep clone (not sharing the same worksheet)
	if w2.subJobWorksheets["subJobID"] == w2Clone.subJobWorksheets["subJobID"] {
		t.Errorf("Clone should create deep copy, but subJobWorksheet points to same memory")
	}

	// Test SubJobWorksheet.Clone() for leaf node
	leafWorksheet := &SubJobWorksheet{
		isLeaf: true,
		tasks:  util.NewPriorityQueue(lessFn),
	}
	leafWorksheet.tasks.Push(200)
	leafClone := leafWorksheet.Clone()
	if leafClone.isLeaf != leafWorksheet.isLeaf {
		t.Errorf("Expected cloned isLeaf to be %v, got %v", leafWorksheet.isLeaf, leafClone.isLeaf)
	}
	if diff := cmp.Diff(drain(leafWorksheet.Clone().tasks), drain(leafClone.tasks)); diff != "" {
		t.Errorf("Cloned leaf worksheet tasks queue mismatch (-want +got): %s", diff)
	}
	// Verify it's a deep clone (not sharing the same PriorityQueue)
	if leafWorksheet.tasks == leafClone.tasks {
		t.Errorf("Clone should create deep copy, but tasks queue points to same memory")
	}

	// Test SubJobWorksheet.Clone() for non-leaf node with children
	child1 := &SubJobWorksheet{
		isLeaf: true,
		tasks:  util.NewPriorityQueue(lessFn),
	}
	child1.tasks.Push(11)
	child2 := &SubJobWorksheet{
		isLeaf: true,
		tasks:  util.NewPriorityQueue(lessFn),
	}
	child2.tasks.Push(22)

	nonLeafWorksheet := &SubJobWorksheet{
		isLeaf:  false,
		subJobs: util.NewPriorityQueue(lessFn),
		subJobWorksheets: map[api.SubJobID]*SubJobWorksheet{
			"child1": child1,
			"child2": child2,
		},
	}
	nonLeafWorksheet.subJobs.Push(1)
	nonLeafWorksheet.subJobs.Push(2)
	nonLeafClone := nonLeafWorksheet.Clone()

	if nonLeafClone.isLeaf != nonLeafWorksheet.isLeaf {
		t.Errorf("Expected cloned isLeaf to be %v, got %v", nonLeafWorksheet.isLeaf, nonLeafClone.isLeaf)
	}
	if diff := cmp.Diff(drain(nonLeafWorksheet.Clone().subJobs), drain(nonLeafClone.subJobs)); diff != "" {
		t.Errorf("Cloned non-leaf worksheet subJobs queue mismatch (-want +got): %s", diff)
	}
	if len(nonLeafWorksheet.subJobWorksheets) != len(nonLeafClone.subJobWorksheets) {
		t.Errorf("Expected cloned subJobWorksheets to have %d entries, got %d",
			len(nonLeafWorksheet.subJobWorksheets), len(nonLeafClone.subJobWorksheets))
	}
	// Verify it's a deep clone (children are cloned, not referenced)
	if nonLeafWorksheet.subJobWorksheets["child1"] == nonLeafClone.subJobWorksheets["child1"] {
		t.Errorf("Clone should create deep copy, but child1 points to same memory")
	}
	if nonLeafWorksheet.subJobWorksheets["child2"] == nonLeafClone.subJobWorksheets["child2"] {
		t.Errorf("Clone should create deep copy, but child2 points to same memory")
	}

	// Test multi-level hierarchical cloning (grandchild structure)
	grandchild := &SubJobWorksheet{
		isLeaf: true,
		tasks:  util.NewPriorityQueue(lessFn),
	}
	grandchild.tasks.Push(999)

	childNonLeaf := &SubJobWorksheet{
		isLeaf:  false,
		subJobs: util.NewPriorityQueue(lessFn),
		subJobWorksheets: map[api.SubJobID]*SubJobWorksheet{
			"grandchild": grandchild,
		},
	}
	childNonLeaf.subJobs.Push(99)

	multiLevelWorksheet := &SubJobWorksheet{
		isLeaf:  false,
		subJobs: util.NewPriorityQueue(lessFn),
		subJobWorksheets: map[api.SubJobID]*SubJobWorksheet{
			"childNonLeaf": childNonLeaf,
		},
	}
	multiLevelWorksheet.subJobs.Push(9)
	multiLevelClone := multiLevelWorksheet.Clone()

	// Verify all levels are cloned independently
	if multiLevelWorksheet.subJobWorksheets["childNonLeaf"] == multiLevelClone.subJobWorksheets["childNonLeaf"] {
		t.Errorf("Multi-level clone should create deep copy, but childNonLeaf points to same memory")
	}
	if multiLevelWorksheet.subJobWorksheets["childNonLeaf"].subJobWorksheets["grandchild"] ==
		multiLevelClone.subJobWorksheets["childNonLeaf"].subJobWorksheets["grandchild"] {
		t.Errorf("Multi-level clone should create deep copy, but grandchild points to same memory")
	}
	// Verify content is identical
	originalGrandchildTasks := drain(multiLevelWorksheet.subJobWorksheets["childNonLeaf"].subJobWorksheets["grandchild"].Clone().tasks)
	clonedGrandchildTasks := drain(multiLevelClone.subJobWorksheets["childNonLeaf"].subJobWorksheets["grandchild"].tasks)
	if diff := cmp.Diff(originalGrandchildTasks, clonedGrandchildTasks); diff != "" {
		t.Errorf("Multi-level cloned grandchild tasks mismatch (-want +got): %s", diff)
	}

	// Test Clone with nil fields
	nilWorksheet := &SubJobWorksheet{
		isLeaf:           true,
		tasks:            nil,
		subJobs:          nil,
		subJobWorksheets: nil,
	}
	nilClone := nilWorksheet.Clone()
	if nilClone.tasks != nil {
		t.Errorf("Expected cloned tasks to be nil, got non-nil")
	}
	if nilClone.subJobs != nil {
		t.Errorf("Expected cloned subJobs to be nil, got non-nil")
	}
	if nilClone.subJobWorksheets != nil {
		t.Errorf("Expected cloned subJobWorksheets to be nil, got non-nil")
	}
}

func TestAllocateWithPartitionPolicyNetworkTopology(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		predicates.PluginName:           predicates.New,
		gang.PluginName:                 gang.New,
		networktopologyaware.PluginName: networktopologyaware.New,
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "podgroup hard network topology constrain and subGroup soft network topology constrain, can allocate job when resources are enough",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "soft", 0),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{0: sets.New[string]("s0", "s1"), 1: sets.New[string]("s2")},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			MinimalBindCheck: true,
			ExpectBindsNum:   3,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup soft network topology constrain, can allocate job when minavailable < replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 1,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "soft", 0),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup soft network topology constrain, two available hyperNodes, can allocate job to nodes with affinity",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 1,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "soft", 0),
					}),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "master"}),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{0: sets.New[string]("s0", "s1")},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1"),
				"s1": sets.New[string]("s1-n2"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p1": "s1-n2",
			},
			ExpectBindsNum: 1,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup soft network topology constrain and tasks in job rescheduled, can allocate job when resources are enough",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s2", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "soft", 0),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup soft network topology constrain and tasks in job rescheduled, can allocate job when resources are enough and minavailable = replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s2", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "soft", 0),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup soft network topology constrain and tasks in job rescheduled, can allocate job when cross highestTierAllowed tier and hyperNodesInfo has three tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s1", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "soft", 0),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
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
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup soft network topology constrain and subGroup hard network topology constrain, can allocate job when resources are enough",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "soft", 0,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithSubGroupSize("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 2),
						util.BuildSubGroupPolicyWithSubGroupSize("task2", []string{"volcano.sh/task-instance"}, "hard", 1, 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			MinimalBindCheck: true,
			ExpectBindsNum:   3,
			ExpectBindMap: map[string]string{
				"c1/p1": "s1-n3",
			},
		},
		{
			Name: "podgroup soft network topology constrain and subGroup hard network topology constrain, can allocate job when minavailable < replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "soft", 0,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup soft network topology constrain and subGroup hard network topology constrain, can not allocate job when cross highestTierAllowed tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "soft", 0,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup soft network topology constrain and subGroup hard network topology constrain and tasks in job rescheduled, can allocate job when resources are enough",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s2", "q1", 2, nil, schedulingv1.PodGroupInqueue, "soft", 0,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s1-n3", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum: 1,
			ExpectBindMap: map[string]string{
				"c1/p3": "s1-n4",
			},
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup soft network topology constrain and subGroup hard network topology constrain and tasks in job rescheduled, can not allocate job when cross highestTierAllowed tier and hyperNodesInfo has three tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s1", "q1", 2, nil, schedulingv1.PodGroupInqueue, "soft", 0,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 2),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "s4-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p4", "s4-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p5", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
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
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain, can allocate job when highestTierAllowed not reached",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   3,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain, can allocate job according to the network topology constrain of subGroup policy",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithSubGroupSize("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 1),
						util.BuildSubGroupPolicyWithSubGroupSize("task2", []string{"volcano.sh/task-instance"}, "hard", 1, 2),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum: 3,
			ExpectBindMap: map[string]string{
				"c1/p1": "s1-n3",
			},
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain, can allocate job when multi hyperNodes are available",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain, can allocate job when minavailable < replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 1, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithSubGroupSize("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain and tasks in job rescheduled, two available hyperNodes, can allocate job to nodes with affinity",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s1-n3", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum: 1,
			ExpectBindMap: map[string]string{
				"c1/p3": "s1-n4",
			},
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain and tasks in job rescheduled, can allocate job when highestTierAllowed not reached and minavailable = replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s0", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 2),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain and tasks in job rescheduled, can allocate job when highestTierAllowed not reached and hyperNodesInfo has three tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s3", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 3,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 2),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
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
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain and tasks in job rescheduled, can not allocate job when cross highestTierAllowed tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s0", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain and tasks in job rescheduled, can not allocate job when cross highestTierAllowed tier and hyperNodesInfo has three tier",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s3", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 3,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 2),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s3-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s3-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "s4-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p4", "s4-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p5", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
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
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain and tasks in job rescheduled, can allocate job when LCAHyperNode is empty",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s0", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 3,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicy("task1", []string{"volcano.sh/task-spec"}, "hard", 2),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain, can allocate job according to the network topology constrain of subGroup policy with minSubGroups",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 5, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithMinSubGroups("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 1, 1),
						util.BuildSubGroupPolicyWithMinSubGroups("task2", []string{"volcano.sh/task-instance"}, "hard", 1, 2, 2),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
				util.BuildPod("c1", "p5", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s2-n5", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1", "s2"), 2: sets.New[string]("s3")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 1, []api.MemberConfig{
					{
						Name:     "s2-n5",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s3": api.NewHyperNodeInfo(api.BuildHyperNode("s3", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
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
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s2-n5"),
				"s3": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4", "s2-n5"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum: 5,
			ExpectBindMap: map[string]string{
				"c1/p1": "s2-n5",
			},
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain, can allocate job according to the network topology constrain of subGroup policy with minSubGroups and minavailable = replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithMinSubGroups("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 1, 1),
						util.BuildSubGroupPolicyWithMinSubGroups("task2", []string{"volcano.sh/task-instance"}, "hard", 1, 2, 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
				util.BuildPod("c1", "p5", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum: 3,
			ExpectBindMap: map[string]string{
				"c1/p1": "s1-n3",
			},
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain, can allocate job according to the network topology constrain of subGroup policy with minSubGroups and hypernode resources meet minSubGroups",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithMinSubGroups("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 1, 2),
						util.BuildSubGroupPolicyWithMinSubGroups("task2", []string{"volcano.sh/task-instance"}, "hard", 1, 2, 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master2"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master3"}, nil),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master4"}, nil),
				util.BuildPod("c1", "p5", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p6", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p7", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
				util.BuildPod("c1", "p8", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   4,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain, can not allocate job when minMember is not satisfied",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 5, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithMinSubGroups("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 1, 1),
						util.BuildSubGroupPolicyWithMinSubGroups("task2", []string{"volcano.sh/task-instance"}, "hard", 1, 2, 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
				util.BuildPod("c1", "p5", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain, can not allocate job when minSubGroups is not satisfied",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithMinSubGroups("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 1, 1),
						util.BuildSubGroupPolicyWithMinSubGroups("task2", []string{"volcano.sh/task-instance"}, "hard", 1, 2, 2),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
				util.BuildPod("c1", "p5", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain with minSubGroups and tasks in job rescheduled, two available hyperNodes, can allocate job to nodes with affinity",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithMinSubGroups("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 2, 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s1-n3", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum: 1,
			ExpectBindMap: map[string]string{
				"c1/p3": "s1-n4",
			},
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain with minSubGroups and tasks in job rescheduled, can not allocate job when hypernode resources are insufficient.",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 2, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithMinSubGroups("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 2, 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s1-n3", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "s1-n4", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain with minSubGroups and tasks in job rescheduled, can allocate job when highestTierAllowed not reached and minAvailable = replicas",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s0", "q1", 4, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithMinSubGroups("task1", []string{"volcano.sh/task-spec"}, "hard", 2, 2, 2),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "podgroup hard network topology constrain and subGroup hard network topology constrain with minSubGroups and tasks in job rescheduled, can allocate job when subGroup is rescheduled",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithMinSubGroups("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 1, 2),
						util.BuildSubGroupPolicyWithMinSubGroups("task2", []string{"volcano.sh/task-instance"}, "hard", 1, 2, 1),
					}),
			},
			Pods: []*v1.Pod{
				// should use different role, because allocate actions default to enable the role caches when predicate
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master2"}, nil),
				util.BuildPod("c1", "p3", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master3"}, nil),
				util.BuildPod("c1", "p4", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "master4"}, nil),
				util.BuildPod("c1", "p5", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p6", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p7", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
				util.BuildPod("c1", "p8", "", v1.PodPending, api.BuildResourceList("2", "4Gi"), "pg1", map[string]string{"volcano.sh/task-instance": "worker2"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "rescheduling scenario: SubJob with multiple pending tasks, GetMinResources returns sum of all pending tasks resources",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s0", "q1", 4, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithSubGroupSize("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 4),
					}),
			},
			Pods: []*v1.Pod{
				// 2 Running tasks + 2 Pending tasks in the same SubJob
				// GetMinResources should return 4 CPU, 8G (sum of 2 pending tasks)
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "rescheduling scenario: SubJob with single pending task",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s0", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithSubGroupSize("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 3),
					}),
			},
			Pods: []*v1.Pod{
				// 2 Running tasks + 1 Pending task
				// GetMinResources should return 2 CPU, 4G (1 pending task)
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   1,
			MinimalBindCheck: true,
		},
		{
			Name: "rescheduling scenario: multiple SubJobs with different pending task counts",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s2", "q1", 6, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						// SubJob1: 2 Running, 1 Pending (GetMinResources = 2 CPU, 4G)
						util.BuildSubGroupPolicyWithSubGroupSize("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 3),
						// SubJob2: 1 Running, 2 Pending (GetMinResources = 4 CPU, 8G)
						util.BuildSubGroupPolicyWithSubGroupSize("task2", []string{"volcano.sh/task-instance"}, "hard", 1, 3),
					}),
			},
			Pods: []*v1.Pod{
				// SubJob1: task-spec=master
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				// SubJob2: task-instance=worker
				util.BuildPod("c1", "p4", "s1-n3", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p5", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
				util.BuildPod("c1", "p6", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-instance": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   3,
			MinimalBindCheck: true,
		},
		{
			Name: "rescheduling scenario: pending tasks with different resource requirements",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s0", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 1,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithSubGroupSize("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 3),
					}),
			},
			Pods: []*v1.Pod{
				// 1 Running task + 2 Pending tasks with different resource requirements
				// Pending task 1: 2 CPU, 4G
				// Pending task 2: 4 CPU, 8G
				// GetMinResources should return 6 CPU, 12G
				// Use stable pod names to ensure consistent scheduling order across different environments
				util.BuildPod("c1", "p1-running", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p2-pending-small", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3-pending-large", "", v1.PodPending, api.BuildResourceList("4", "8G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "rescheduling scenario: insufficient resources for pending tasks allocation",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s0", "q1", 3, nil, schedulingv1.PodGroupInqueue, "hard", 1,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithSubGroupSize("task1", []string{"volcano.sh/task-spec"}, "hard", 1, 3),
					}),
			},
			Pods: []*v1.Pod{
				// 1 Running task + 2 Pending tasks, each needs 4 CPU
				// HyperNode s0 only has 4 CPU available (8 total - 4 used by running task)
				// Cannot allocate both pending tasks
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("4", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("4", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("4", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				// Only 4 CPU available after running task uses 4 CPU
				util.BuildNode("s0-n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			// Gang scheduling requires all 3 tasks, but only 1 is running and resources insufficient for 2 pending
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
		{
			Name: "rescheduling scenario: SubJob rescheduling within tier-2 HyperNode constraint",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "s2", "q1", 4, nil, schedulingv1.PodGroupInqueue, "hard", 2,
					[]schedulingv1.SubGroupPolicySpec{
						// SubJob with hard topology constraint at tier 2 (same as job level)
						util.BuildSubGroupPolicyWithSubGroupSize("task1", []string{"volcano.sh/task-spec"}, "hard", 2, 4),
					}),
			},
			Pods: []*v1.Pod{
				// Running tasks in s0 HyperNode
				// AllocatedHyperNode is set to s2 (tier-2), which contains both s0 and s1
				util.BuildPod("c1", "p1", "s0-n1", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p2", "s0-n2", v1.PodRunning, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				// Pending tasks can be scheduled to any node within s2 (s0-n1, s0-n2, s1-n3, s1-n4)
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("s0-n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s0-n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n3", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("s1-n4", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New[string]("s0", "s1"), 2: sets.New[string]("s2")},
			HyperNodesMap: map[string]*api.HyperNodeInfo{
				"s0": api.NewHyperNodeInfo(api.BuildHyperNode("s0", 1, []api.MemberConfig{
					{
						Name:     "s0-n1",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s0-n2",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s1": api.NewHyperNodeInfo(api.BuildHyperNode("s1", 1, []api.MemberConfig{
					{
						Name:     "s1-n3",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
					{
						Name:     "s1-n4",
						Type:     topologyv1alpha1.MemberTypeNode,
						Selector: "exact",
					},
				})),
				"s2": api.NewHyperNodeInfo(api.BuildHyperNode("s2", 2, []api.MemberConfig{
					{
						Name:     "s0",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
					{
						Name:     "s1",
						Type:     topologyv1alpha1.MemberTypeHyperNode,
						Selector: "exact",
					},
				})),
			},
			HyperNodes: map[string]sets.Set[string]{
				"s0": sets.New[string]("s0-n1", "s0-n2"),
				"s1": sets.New[string]("s1-n3", "s1-n4"),
				"s2": sets.New[string]("s0-n1", "s0-n2", "s1-n3", "s1-n4"),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			// Both pending tasks should be allocated within s2 (which contains s0 and s1)
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
		{
			Name: "no network topology at job or subgroup level but has SubJobPolicy, can allocate job successfully",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", 2, nil, schedulingv1.PodGroupInqueue, "", 0,
					[]schedulingv1.SubGroupPolicySpec{
						util.BuildSubGroupPolicyWithMinSubGroups("worker", []string{"volcano.sh/task-spec"}, "", 0, 1, 2),
					}),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker-0"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "4G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker-1"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindsNum:   2,
			MinimalBindCheck: true,
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
					EnabledSubJobReady:  &trueValue,
					EnabledSubJobOrder:  &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:                     networktopologyaware.PluginName,
					EnabledNodeOrder:         &trueValue,
					EnabledHyperNodeOrder:    &trueValue,
					EnabledHyperNodeGradient: &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// BenchmarkHyperNodeGradientFnPerformance tests the performance optimization
// of hyperNodeGradientFn with SubGroup policy in large-scale cluster scenarios.
func BenchmarkHyperNodeGradientFnPerformance(b *testing.B) {
	plugins := map[string]framework.PluginBuilder{
		drf.PluginName:                  drf.New,
		proportion.PluginName:           proportion.New,
		predicates.PluginName:           predicates.New,
		nodeorder.PluginName:            nodeorder.New,
		gang.PluginName:                 gang.New,
		networktopologyaware.PluginName: networktopologyaware.New,
	}

	const numNodes = 1000
	const nodesPerHyperNode = 10
	const numTier1HyperNodes = numNodes / nodesPerHyperNode
	const numPods = 1000
	const podsPerSubGroup = 10
	const numSubGroups = numPods / podsPerSubGroup
	const nodeCPU, nodeMemory = "4", "8Gi"
	const podCPU, podMemory = "4", "8Gi"

	// Build 1000 nodes
	nodes := make([]*v1.Node, 0, numNodes)
	for i := 0; i < numNodes; i++ {
		nodes = append(nodes, util.BuildNode(fmt.Sprintf("n-%d", i),
			api.BuildResourceList(nodeCPU, nodeMemory, []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil))
	}

	// Build HyperNodes
	hyperNodesMap := make(map[string]*api.HyperNodeInfo)
	hyperNodes := make(map[string]sets.Set[string])

	tier1Set := sets.New[string]()
	tier2Nodes := sets.New[string]()

	for i := 0; i < numTier1HyperNodes; i++ {
		hnName := fmt.Sprintf("hn-tier1-%d", i)
		tier1Set.Insert(hnName)
		nodeSet := sets.New[string]()
		members := make([]api.MemberConfig, 0, nodesPerHyperNode)

		for j := 0; j < nodesPerHyperNode; j++ {
			nodeName := fmt.Sprintf("n-%d", i*nodesPerHyperNode+j)
			nodeSet.Insert(nodeName)

			tier2Nodes.Insert(nodeName)
			members = append(members, api.MemberConfig{Name: nodeName, Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"})
		}
		hyperNodes[hnName] = nodeSet
		hyperNodesMap[hnName] = api.NewHyperNodeInfo(
			api.BuildHyperNode(hnName, 1, members),
		)
	}

	//build tier 2 hypernodes
	tier2Members := make([]api.MemberConfig, 0, numTier1HyperNodes)
	for i := 0; i < numTier1HyperNodes; i++ {
		tier2Members = append(tier2Members, api.MemberConfig{Name: fmt.Sprintf("hn-tier1-%d", i), Type: topologyv1alpha1.MemberTypeHyperNode, Selector: "exact"})
	}
	hyperNodesMap["hn-tier2-0"] = api.NewHyperNodeInfo(api.BuildHyperNode("hn-tier2-0", 2, tier2Members))
	hyperNodes["hn-tier2-0"] = tier2Nodes

	// Build 1000 pods with 100 SubGroups
	pods := make([]*v1.Pod, 0, numPods)
	for i := 0; i < numPods; i++ {
		pods = append(pods, util.BuildPod("c1",
			fmt.Sprintf("p%d", i),
			"",
			v1.PodPending,
			api.BuildResourceList(podCPU, podMemory),
			"pg1",
			map[string]string{"volcano.sh/task-spec": fmt.Sprintf("subgroup-%d", i/10)},
			nil),
		)
	}

	// Build PodGroup with MinResources set to total job resource requirement
	pg := util.BuildPodGroupWithSubGroupPolicy("pg1", "c1", "", "q1", numPods, nil, schedulingv1.PodGroupInqueue, "hard", 2,
		[]schedulingv1.SubGroupPolicySpec{
			util.BuildSubGroupPolicyWithMinSubGroups("task1", []string{"volcano.sh/task-spec"}, "hard", 1, podsPerSubGroup, numSubGroups),
		})
	// Set MinResources = 1000 pods × (4 CPU, 8Gi) = (4000 CPU, 8000Gi)
	// This enables HyperNode pre-filtering: Tier-1 (40 CPU) < MinResources (4000 CPU) -> filtered out
	pg.Spec.MinResources = &v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("4000"),
		v1.ResourceMemory: resource.MustParse("8000Gi"),
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
					EnabledSubJobReady:  &trueValue,
					EnabledSubJobOrder:  &trueValue,
				},
				{
					Name:               drf.PluginName,
					EnabledPreemptable: &trueValue,
					EnabledJobOrder:    &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledQueueOrder:  &trueValue,
					EnabledReclaimable: &trueValue,
					EnabledAllocatable: &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:             nodeorder.PluginName,
					EnabledNodeOrder: &trueValue,
				},
				{
					Name:                     networktopologyaware.PluginName,
					EnabledNodeOrder:         &trueValue,
					EnabledHyperNodeOrder:    &trueValue,
					EnabledHyperNodeGradient: &trueValue,
				},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		testStruct := uthelper.TestCommonStruct{
			Name:                "performance test: 1000 pods with 100 SubGroups on 1000 nodes",
			PodGroups:           []*schedulingv1.PodGroup{pg},
			Pods:                pods,
			Nodes:               nodes,
			HyperNodesSetByTier: map[int]sets.Set[string]{1: tier1Set, 2: sets.New[string]("hn-tier2-0")},
			HyperNodesMap:       hyperNodesMap,
			HyperNodes:          hyperNodes,
			Queues:              []*schedulingv1.Queue{util.BuildQueue("q1", 1, nil)},
			Plugins:             plugins,
		}
		testStruct.RegisterSession(tiers, nil)
		action := New()
		testStruct.Run([]framework.Action{action})
		testStruct.Close()
	}
}
