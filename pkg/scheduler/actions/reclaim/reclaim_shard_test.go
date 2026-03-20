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

package reclaim

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

// TestReclaimWithShard tests reclaim action behavior with shard configuration
func TestReclaimWithShard(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		proportion.PluginName:  proportion.New,
		predicates.PluginName:  predicates.New,
		priority.PluginName:    priority.New,
	}

	// Save original options
	originalShardingMode := options.ServerOpts.ShardingMode
	originalShardName := options.ServerOpts.ShardName
	defer func() {
		options.ServerOpts.ShardingMode = originalShardingMode
		options.ServerOpts.ShardName = originalShardName
	}()

	reclaim := New()
	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledReclaimable: &trueValue,
				},
				{
					Name:               gang.PluginName,
					EnabledReclaimable: &trueValue,
					EnabledJobStarving: &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledReclaimable: &trueValue,
					EnabledQueueOrder:  &trueValue,
					EnablePreemptive:   &trueValue,
				},
				{
					Name:               priority.PluginName,
					EnabledReclaimable: &trueValue,
					EnabledJobOrder:    &trueValue,
					EnabledTaskOrder:   &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
			},
		},
	}

	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)

	tests := []uthelper.TestCommonStruct{
		{
			Name: "hard sharding mode - only reclaim from nodes in shard",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, lowPrio.Name),
				util.BuildPodGroupWithPrio("pg2", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupInqueue, highPrio.Name),
			},
			Pods: []*v1.Pod{
				// Preemptable pod on n1 (in shard) - node is full
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("2", "2Gi"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				// Preemptable pod on n2 (out of shard) to highlight hard mode only touches shard nodes
				util.BuildPod("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("2", "2Gi"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				// High-priority pending pod.
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
				util.BuildQueue("q2", 1, nil),
			},
			PriClass:       []*schedulingv1.PriorityClass{highPrio, lowPrio},
			ExpectEvictNum: 1,
			// In hard mode, reclaim should only evict from nodes in shard (n1),
			// even though there is another preemptable pod on n2 (out of shard).
			ExpectEvicted: []string{"c1/preemptee1"},
			ShardingMode:  commonutil.HardShardingMode,
			ShardName:     "test-shard",
			NodesInShard:  []string{"n1"},
		},
		{
			Name: "soft sharding mode - prefer reclaiming from node in shard",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, lowPrio.Name),
				util.BuildPodGroupWithPrio("pg2", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupInqueue, highPrio.Name),
			},
			Pods: []*v1.Pod{
				// Preemptable pod on n1 (in shard) - node is full
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("2", "2Gi"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				// Preemptable pod on n2 (out of shard) to highlight soft mode prefers shard nodes
				util.BuildPod("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("2", "2Gi"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				// High-priority pending pod.
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				// n1 and n2 are identical in capacity and labels.
				util.BuildNode("n1", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
				util.BuildQueue("q2", 1, nil),
			},
			PriClass:       []*schedulingv1.PriorityClass{highPrio, lowPrio},
			ExpectEvictNum: 1,
			// In soft mode, reclaim should prefer evicting from nodes in shard (n1),
			// even though n1 and n2 are identical and both have preemptable pods.
			ExpectEvicted: []string{"c1/preemptee1"},
			ShardingMode:  commonutil.SoftShardingMode,
			ShardName:     "test-shard",
			NodesInShard:  []string{"n1"},
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

			test.Run([]framework.Action{reclaim})

			if err := test.CheckAll(i); err != nil {
				t.Fatalf("Test %s failed: %v", test.Name, err)
			}
		})
	}
}
