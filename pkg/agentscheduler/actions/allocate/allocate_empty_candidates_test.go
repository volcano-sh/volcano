/*
Copyright 2026 The Volcano Authors.

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
	agentuthelper "volcano.sh/volcano/pkg/agentscheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

func TestParseArgumentsCandidateNodeCount(t *testing.T) {
	tests := []struct {
		name     string
		value    int
		expected int
	}{
		{name: "positive value is kept", value: 5, expected: 5},
		{name: "zero falls back to default", value: 0, expected: DefaultCandidateNodeCount},
		{name: "negative falls back to default", value: -1, expected: DefaultCandidateNodeCount},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alloc := New()
			alloc.OnActionInit([]conf.Configuration{{
				Name:      alloc.Name(),
				Arguments: map[string]interface{}{CandidateNodeCountKey: tt.value},
			}})
			if alloc.candidateNodeCount != tt.expected {
				t.Errorf("expected candidateNodeCount %d, got %d", tt.expected, alloc.candidateNodeCount)
			}
		})
	}
}

func TestAllocateTaskWithNoCandidateNodes(t *testing.T) {
	agentuthelper.InitTestEnv(t)

	tests := []struct {
		name               string
		shardingMode       string
		candidateNodeCount int
	}{
		{
			name:               "non-positive candidate count",
			shardingMode:       commonutil.NoneShardingMode,
			candidateNodeCount: 0,
		},
		{
			name:               "unrecognized sharding mode",
			shardingMode:       "Soft",
			candidateNodeCount: DefaultCandidateNodeCount,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options.ServerOpts.ShardingMode = tt.shardingMode
			scheduleroptions.ServerOpts.ShardingMode = tt.shardingMode
			defer func() {
				options.ServerOpts.ShardingMode = commonutil.NoneShardingMode
				scheduleroptions.ServerOpts.ShardingMode = commonutil.NoneShardingMode
			}()

			alloc := New()
			testFwk, err := agentuthelper.NewTestFramework("test-scheduler", 1, []framework.Action{alloc}, agentuthelper.DefaultTiers(), []conf.Configuration{})
			if err != nil {
				t.Fatalf("Failed to create test framework: %v", err)
			}
			defer testFwk.Close()
			alloc.candidateNodeCount = tt.candidateNodeCount

			for _, name := range []string{"n1", "n2"} {
				node := util.BuildNode(name, api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
				testFwk.MockCache.AddOrUpdateNode(node)
			}
			fwk := testFwk.Frameworks[0]
			if err := testFwk.MockCache.UpdateSnapshot(fwk.GetSnapshot()); err != nil {
				t.Fatalf("Failed to update snapshot: %v", err)
			}

			pod := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "", make(map[string]string), make(map[string]string))
			queuedPodInfo, err := agentuthelper.CreateSchedulingContext(pod, sets.New[string]())
			if err != nil {
				t.Fatalf("Failed to create QueuedPodInfo: %v", err)
			}
			schedCtx := &agentapi.SchedulingContext{
				Task:          api.NewTaskInfo(pod),
				QueuedPodInfo: queuedPodInfo,
				NodesInShard:  sets.New[string](),
			}

			fwk.ClearCycleState()
			fwk.OnCycleStart()
			err = alloc.allocateTask(fwk, schedCtx)
			fwk.OnCycleEnd()

			if err == nil {
				t.Errorf("expected an error when no candidate nodes are selected, got nil")
			}
			if n := len(testFwk.MockCache.ConflictAwareBinder.BindCheckChannel); n != 0 {
				t.Errorf("expected no result to be sent to the binder, got %d", n)
			}
		})
	}
}
