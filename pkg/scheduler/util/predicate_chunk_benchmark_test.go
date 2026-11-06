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
	"testing"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func BenchmarkPredicate400Nodes(b *testing.B) {
	previous := options.ServerOpts
	options.ServerOpts = &options.ServerOption{MinNodesToFind: 100, MinPercentageOfNodesToFind: 5}
	b.Cleanup(func() { options.ServerOpts = previous })
	nodes := make([]*api.NodeInfo, 400)
	for i := range nodes {
		nodes[i] = &api.NodeInfo{Name: fmt.Sprintf("node-%d", i)}
	}
	task := &api.TaskInfo{Namespace: "ns", Name: "pod", Job: "ns/job", TaskRole: "worker"}
	helper := NewPredicateHelper()
	target := int(CalculateNumOfFeasibleNodesToFind(int32(len(nodes))))
	check := func(*api.TaskInfo, *api.NodeInfo) error { return nil }
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fit, _ := helper.PredicateNodes(task, nodes, check, false, nil)
		if len(fit) != target {
			b.Fatalf("got %d feasible nodes, expected %d", len(fit), target)
		}
	}
}
