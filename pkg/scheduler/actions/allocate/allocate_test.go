/*
Copyright 2017 The Kubernetes Authors.

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

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/sets"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/binpack"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/nodeorder"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
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
		predicates.PluginName: predicates.New,
		gang.PluginName:       gang.New,
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "soft network topology constrain, can allocate job when resources are enough",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithNetWorkTopologies("pg1", "c1", "", "q1", 3, nil, schedulingv1.PodGroupInqueue, "soft", 1),
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
			Name: "hard network topology constrain, can allocate job when minavailiable < replicas",
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
			Name: "hard network topology constrain and tasks in job rescheduled, can allocate job when highestTierAllowed not reached and minavailiable = replicas",
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
			Name: "hard network topology constrain and tasks in job rescheduled, can not allocate job when LCAHyperNode is empty",
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
			ExpectBindsNum:   0,
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
		predicates.PluginName: predicates.New,
		gang.PluginName:       gang.New,
		binpack.PluginName:    binpack.New,
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
			HyperNodesSetByTier: map[int]sets.Set[string]{0: sets.New[string]("s0", "s1")},
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
			Name: "queue’s DRF share value will be updated and its priority will change before it is put back into the priority queue",
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
			Name: "queue’s one jobs has no pending tasks, should be put back to queues for next job",
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
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupRunning,
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
