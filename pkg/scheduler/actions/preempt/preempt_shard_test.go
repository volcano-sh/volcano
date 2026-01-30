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

package preempt

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

// TestPreemptWithShard tests preempt action behavior with shard configuration
func TestPreemptWithShard(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		priority.PluginName:    priority.New,
		proportion.PluginName:  proportion.New,
		predicates.PluginName:  predicates.New,
	}

	// Save original options
	originalShardingMode := options.ServerOpts.ShardingMode
	originalShardName := options.ServerOpts.ShardName
	defer func() {
		options.ServerOpts.ShardingMode = originalShardingMode
		options.ServerOpts.ShardName = originalShardName
	}()

	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledPreemptable: &trueValue,
				},
				{
					Name:               gang.PluginName,
					EnabledPreemptable: &trueValue,
					EnabledJobStarving: &trueValue,
				},
				{
					Name:               priority.PluginName,
					EnabledPreemptable: &trueValue,
					EnabledJobStarving: &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
			},
		},
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "hard sharding mode - preempt from node in shard",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, lowPrio.Name),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, highPrio.Name),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "1G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("1", "1G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			PriClass:       []*schedulingv1.PriorityClass{highPrio, lowPrio},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee1"},
			ShardingMode:   commonutil.HardShardingMode,
			ShardName:      "test-shard",
			NodesInShard:   []string{"n1"},
		},
		{
			Name: "soft sharding mode - prefer preempting from node in shard",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, lowPrio.Name),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, highPrio.Name),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "1G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("1", "1G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			PriClass:       []*schedulingv1.PriorityClass{highPrio, lowPrio},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee1"},
			ShardingMode:   commonutil.SoftShardingMode,
			ShardName:      "test-shard",
			NodesInShard:   []string{"n1"},
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			ssn := test.RegisterSession(tiers, nil)
			if len(test.NodesInShard) > 0 {
				ssn.NodesInShard = sets.New(test.NodesInShard...)
				options.ServerOpts.ShardingMode = test.ShardingMode
				options.ServerOpts.ShardName = test.ShardName
			}
			defer test.Close()

			action := New()
			test.Run([]framework.Action{action})

			if err := test.CheckAll(i); err != nil {
				t.Fatalf("Test %s failed: %v", test.Name, err)
			}
		})
	}
}
