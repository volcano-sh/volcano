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

package allocate

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	scheduleroptions "volcano.sh/volcano/cmd/scheduler/app/options"
	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	"volcano.sh/volcano/pkg/agentscheduler/plugins/nodeorder"
	"volcano.sh/volcano/pkg/agentscheduler/plugins/predicates"
	agentuthelper "volcano.sh/volcano/pkg/agentscheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

// TestAllocateWithShard tests agent scheduler allocate action behavior with shard configuration
func TestAllocateWithShard(t *testing.T) {
	// Register plugins
	framework.RegisterPluginBuilder(predicates.PluginName, predicates.New)
	framework.RegisterPluginBuilder(nodeorder.PluginName, nodeorder.New)

	// Initialize ServerOpts if nil (for agent scheduler)
	if options.ServerOpts == nil {
		options.ServerOpts = options.NewServerOption()
	}
	// Initialize scheduler ServerOpts if nil (for volume binding plugin)
	// predicate_helper.go uses scheduler's options, so we need to set it
	if scheduleroptions.ServerOpts == nil {
		scheduleroptions.ServerOpts = scheduleroptions.NewServerOption()
	}
	// Save original options to restore after test
	originalShardingMode := options.ServerOpts.ShardingMode
	originalShardName := options.ServerOpts.ShardName
	originalSchedulerShardingMode := scheduleroptions.ServerOpts.ShardingMode
	originalSchedulerShardName := scheduleroptions.ServerOpts.ShardName
	defer func() {
		if options.ServerOpts != nil {
			options.ServerOpts.ShardingMode = originalShardingMode
			options.ServerOpts.ShardName = originalShardName
		}
		if scheduleroptions.ServerOpts != nil {
			scheduleroptions.ServerOpts.ShardingMode = originalSchedulerShardingMode
			scheduleroptions.ServerOpts.ShardName = originalSchedulerShardName
		}
	}()

	scheduleroptions.ServerOpts.PercentageOfNodesToFind = 100

	// Common setup shared across all test cases
	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:             nodeorder.PluginName,
					EnabledNodeOrder: &trueValue,
					Arguments: map[string]interface{}{
						"leastrequested.weight": 1,
						"mostrequested.weight":  0,
					},
				},
			},
		},
	}

	// Create test framework (shared setup)
	testFwk, err := agentuthelper.NewTestFramework("test-scheduler", []framework.Action{New()}, tiers, []conf.Configuration{})
	if err != nil {
		t.Fatalf("Failed to create test framework: %v", err)
	}
	defer testFwk.Close()

	// Add nodes to cache (shared setup)
	// n1: in shard, medium resources.
	// n2: out of shard, fewer resources.
	// n3: out of shard, more resources than n1 (to highlight soft shard preference).
	// n4: in shard, fewer resources than n1.
	n1 := util.BuildNode("n1", api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n2 := util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n3 := util.BuildNode("n3", api.BuildResourceList("20", "40Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n4 := util.BuildNode("n4", api.BuildResourceList("5", "10Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	testFwk.MockCache.AddOrUpdateNode(n1)
	testFwk.MockCache.AddOrUpdateNode(n2)
	testFwk.MockCache.AddOrUpdateNode(n3)
	testFwk.MockCache.AddOrUpdateNode(n4)

	// Update snapshot after adding nodes.
	snapshot := testFwk.Framework.GetSnapshot()
	if err := testFwk.MockCache.UpdateSnapshot(snapshot); err != nil {
		t.Fatalf("Failed to update snapshot: %v", err)
	}

	// Create common pod and task (shared setup)
	pod := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "", make(map[string]string), make(map[string]string))
	task := api.NewTaskInfo(pod)

	shardTests := []struct {
		shardingMode string
		shardName    string
		nodesInShard []string
		expectedNode string
		testName     string
	}{
		{
			shardingMode: commonutil.HardShardingMode,
			shardName:    "test-shard",
			nodesInShard: []string{"n1", "n4"},
			expectedNode: "n1", // within shard, n1 has more resources than n4.
			testName:     "hard sharding mode - allocate to node in shard",
		},
		{
			shardingMode: commonutil.SoftShardingMode,
			shardName:    "test-shard",
			nodesInShard: []string{"n1", "n4"},
			expectedNode: "n1", // prefer shard node n1 over n3 even though n3 has more resources.
			testName:     "soft sharding mode - prefer node in shard",
		},
	}

	for _, tt := range shardTests {
		t.Run(tt.testName, func(t *testing.T) {
			// Set sharding options for both agent scheduler and scheduler.
			options.ServerOpts.ShardingMode = tt.shardingMode
			options.ServerOpts.ShardName = tt.shardName
			scheduleroptions.ServerOpts.ShardingMode = tt.shardingMode
			scheduleroptions.ServerOpts.ShardName = tt.shardName

			// Create scheduling context (per-test setup)
			nodesInShardSet := sets.New(tt.nodesInShard...)
			queuedPodInfo, err := agentuthelper.CreateSchedulingContext(pod, nodesInShardSet)
			if err != nil {
				t.Fatalf("Failed to create QueuedPodInfo: %v", err)
			}
			schedCtx := &agentapi.SchedulingContext{
				Task:          task,
				QueuedPodInfo: queuedPodInfo,
				NodesInShard:  nodesInShardSet,
			}

			// Execute scheduling
			testFwk.Framework.ClearCycleState()
			testFwk.Framework.OnCycleStart()
			testFwk.Action.Execute(testFwk.Framework, schedCtx)
			testFwk.Framework.OnCycleEnd()

			// Verify result
			agentuthelper.VerifySchedulingResult(t, testFwk.MockCache, tt.expectedNode)
		})
	}
}
