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

package backfill

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

// TestBackfillWithShard tests backfill action behavior with shard configuration
func TestBackfillWithShard(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		gang.PluginName:       gang.New,
		predicates.PluginName: predicates.New,
	}

	// Initialize ServerOpts if nil
	if options.ServerOpts == nil {
		options.ServerOpts = options.NewServerOption()
	}
	// Save original options
	originalShardingMode := options.ServerOpts.ShardingMode
	originalShardName := options.ServerOpts.ShardName
	defer func() {
		if options.ServerOpts != nil {
			options.ServerOpts.ShardingMode = originalShardingMode
			options.ServerOpts.ShardName = originalShardName
		}
	}()

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:            gang.PluginName,
					EnabledJobReady: &trueValue,
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
			Name: "hard sharding mode - backfill to node in shard",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				// BestEffort pod for backfill
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("0", "0"), "pg1", make(map[string]string), make(map[string]string)),
			},
			// n1 in shard, n2/n3/n4 are out of shard. n3 has more resources but is out of shard.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n3", api.BuildResourceList("20", "40Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n4", api.BuildResourceList("5", "10Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			ExpectBindsNum: 1,
			ExpectBindMap: map[string]string{
				"c1/p1": "n1",
			},
			ShardingMode: commonutil.HardShardingMode,
			ShardName:    "test-shard",
			NodesInShard: []string{"n1"},
		},
		{
			Name: "soft sharding mode - prefer node in shard",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				// BestEffort pod for backfill
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("0", "0"), "pg1", make(map[string]string), make(map[string]string)),
			},
			// n1 in shard, n2/n3/n4 are out of shard. Soft mode still prefers shard nodes,
			// even though n3 (out of shard) has the most resources.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n3", api.BuildResourceList("20", "40Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n4", api.BuildResourceList("5", "10Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			ExpectBindsNum: 1,
			ExpectBindMap: map[string]string{
				"c1/p1": "n1",
			},
			ShardingMode: commonutil.SoftShardingMode,
			ShardName:    "test-shard",
			NodesInShard: []string{"n1"},
		},
		{
			// Identical nodes, only one is in shard: soft sharding should still prefer the shard node.
			Name: "soft sharding mode - identical nodes prefer shard node",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				// BestEffort pod for backfill
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("0", "0"), "pg1", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				// n1 and n2 are identical in capacity and labels.
				util.BuildNode("n1", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			ExpectBindsNum: 1,
			ExpectBindMap: map[string]string{
				"c1/p1": "n1",
			},
			ShardingMode: commonutil.SoftShardingMode,
			ShardName:    "test-shard",
			NodesInShard: []string{"n1"},
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
