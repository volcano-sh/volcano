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

package util

import (
	"fmt"
	"sync/atomic"
	"testing"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestChunkedPredicatePreservesFeasibleNodesAndErrorCache(t *testing.T) {
	previous, previousIndex := options.ServerOpts, lastProcessedNodeIndex.Load()
	t.Cleanup(func() { options.ServerOpts = previous; lastProcessedNodeIndex.Store(previousIndex) })
	options.ServerOpts = &options.ServerOption{MinNodesToFind: 100, MinPercentageOfNodesToFind: 5}
	for _, fits := range []int{50, 400} {
		t.Run(fmt.Sprintf("%d-fit", fits), func(t *testing.T) {
			nodes := make([]*api.NodeInfo, 400)
			expected := make(map[string]bool)
			for i := range nodes {
				nodes[i] = &api.NodeInfo{Name: fmt.Sprintf("node-%d", i)}
				if i < fits {
					expected[nodes[i].Name] = true
				}
			}
			var calls atomic.Int32
			predicate := func(_ *api.TaskInfo, node *api.NodeInfo) error {
				calls.Add(1)
				if expected[node.Name] {
					return nil
				}
				return fmt.Errorf("resource constraint")
			}
			helper := NewPredicateHelper().(*predicateHelper)
			task := &api.TaskInfo{Namespace: "ns", Name: "pod", Job: "ns/job", TaskRole: "worker"}
			target := min(fits, int(CalculateNumOfFeasibleNodesToFind(400)))
			for pass := 0; pass < 2; pass++ {
				calls.Store(0)
				result, _ := helper.PredicateNodes(task, nodes, predicate, true, nil)
				if len(result) != target {
					t.Fatalf("got %d feasible nodes, want %d", len(result), target)
				}
				seen := make(map[string]bool)
				for _, node := range result {
					if !expected[node.Name] || seen[node.Name] {
						t.Fatalf("infeasible or duplicate node %s", node.Name)
					}
					seen[node.Name] = true
				}
				if fits <= target {
					wantCalls := int32(400)
					if pass == 1 {
						wantCalls = int32(fits)
					}
					if calls.Load() != wantCalls {
						t.Fatalf("pass %d: predicate calls %d, want %d", pass, calls.Load(), wantCalls)
					}
					if got := len(helper.taskPredicateErrorCache[taskGroupID(task)]); got != 400-fits {
						t.Fatalf("cached failures %d, want %d", got, 400-fits)
					}
				}
			}
		})
	}
}
