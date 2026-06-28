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

package framework

import (
	"fmt"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
)

func makeBenchTasks(n int) []*api.TaskInfo {
	tasks := make([]*api.TaskInfo, n)
	for i := range tasks {
		tasks[i] = &api.TaskInfo{UID: api.TaskID(fmt.Sprintf("task-%d", i))}
	}
	return tasks
}

// BenchmarkReclaimableIntersection measures the two-plugin intersection path in
// Reclaimable with increasing task counts. The first plugin admits all n tasks;
// the second admits only the first half, producing an n/2 intersection.
func BenchmarkReclaimableIntersection(b *testing.B) {
	for _, n := range []int{100, 500, 1000} {
		n := n
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			enabled := true
			tasks := makeBenchTasks(n)
			half := tasks[:n/2]
			ssn := &Session{
				Tiers: []conf.Tier{{Plugins: []conf.PluginOption{
					{Name: "p1", EnabledReclaimable: &enabled},
					{Name: "p2", EnabledReclaimable: &enabled},
				}}},
				reclaimableFns: map[string]api.EvictableFn{},
			}
			ssn.AddReclaimableFn("p1", func(_ *api.TaskInfo, _ []*api.TaskInfo) ([]*api.TaskInfo, int) {
				return tasks, 1
			})
			ssn.AddReclaimableFn("p2", func(_ *api.TaskInfo, _ []*api.TaskInfo) ([]*api.TaskInfo, int) {
				return half, 1
			})
			reclaimer := &api.TaskInfo{}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ssn.Reclaimable(reclaimer, tasks)
			}
		})
	}
}

// BenchmarkPreemptableIntersection mirrors BenchmarkReclaimableIntersection for
// the Preemptable path.
func BenchmarkPreemptableIntersection(b *testing.B) {
	for _, n := range []int{100, 500, 1000} {
		n := n
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			enabled := true
			tasks := makeBenchTasks(n)
			half := tasks[:n/2]
			ssn := &Session{
				Tiers: []conf.Tier{{Plugins: []conf.PluginOption{
					{Name: "p1", EnabledPreemptable: &enabled},
					{Name: "p2", EnabledPreemptable: &enabled},
				}}},
				preemptableFns: map[string]api.EvictableFn{},
			}
			ssn.AddPreemptableFn("p1", func(_ *api.TaskInfo, _ []*api.TaskInfo) ([]*api.TaskInfo, int) {
				return tasks, 1
			})
			ssn.AddPreemptableFn("p2", func(_ *api.TaskInfo, _ []*api.TaskInfo) ([]*api.TaskInfo, int) {
				return half, 1
			})
			preemptor := &api.TaskInfo{}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ssn.Preemptable(preemptor, tasks)
			}
		})
	}
}

// BenchmarkUnifiedEvictableIntersection mirrors the above for the
// UnifiedEvictable path.
func BenchmarkUnifiedEvictableIntersection(b *testing.B) {
	for _, n := range []int{100, 500, 1000} {
		n := n
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			tasks := makeBenchTasks(n)
			half := tasks[:n/2]
			ctx := &api.EvictionContext{Kind: api.EvictionKindGangPreempt}
			ssn := &Session{
				Tiers: []conf.Tier{{Plugins: []conf.PluginOption{
					{Name: "p1"},
					{Name: "p2"},
				}}},
				unifiedEvictableFns: map[string]api.UnifiedEvictableFn{},
			}
			ssn.AddUnifiedEvictableFn("p1", func(_ *api.EvictionContext, _ []*api.TaskInfo) ([]*api.TaskInfo, int) {
				return tasks, 1
			})
			ssn.AddUnifiedEvictableFn("p2", func(_ *api.EvictionContext, _ []*api.TaskInfo) ([]*api.TaskInfo, int) {
				return half, 1
			})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ssn.UnifiedEvictable(ctx, tasks)
			}
		})
	}
}
