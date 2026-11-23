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

package cache

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// BenchmarkUpdatePod measures UpdatePod for a running pod whose new version
// differs only in resourceVersion, in a cache holding 1000 jobs of 20 tasks over
// 50 queues.
// Run it with:
//
//	go test ./pkg/scheduler/cache/ -run '^$' -bench UpdatePod -benchmem
func BenchmarkUpdatePod(b *testing.B) {
	const queues, jobs, tasksPerJob = 50, 1000, 20
	// The recorder drops events instead of blocking once the mock's default
	// 100-event buffer is full.
	sc := NewCustomMockSchedulerCache("volcano", nil, nil, &util.FakeStatusUpdater{}, nil, &record.FakeRecorder{})
	defer ShutdownMockSchedulerCache(sc)
	if err := sc.AddOrUpdateNode(util.BuildNode("n1",
		schedulingapi.BuildResourceList("100000", "100000Gi", []schedulingapi.ScalarResource{{Name: "pods", Value: "100000"}}...), nil)); err != nil {
		b.Fatal(err)
	}
	for q := 0; q < queues; q++ {
		sc.AddQueueV1beta1(util.BuildQueue(fmt.Sprintf("q%d", q), 1, nil))
	}
	for j := 0; j < jobs; j++ {
		pg := fmt.Sprintf("pg%d", j)
		sc.AddPodGroupV1beta1(util.BuildPodGroup(pg, "ns1", fmt.Sprintf("q%d", j%queues),
			tasksPerJob, nil, schedulingv1beta1.PodGroupRunning))
		for i := 0; i < tasksPerJob; i++ {
			sc.AddPod(util.BuildPod("ns1", fmt.Sprintf("%s-%d", pg, i), "n1", v1.PodRunning,
				schedulingapi.BuildResourceList("1", "1Gi"), pg, nil, nil))
		}
	}

	// Alternate between two versions of one running pod so the cache state is
	// the same after every pair of updates.
	current := util.BuildPod("ns1", "pg0-0", "n1", v1.PodRunning, schedulingapi.BuildResourceList("1", "1Gi"), "pg0", nil, nil)
	current.ResourceVersion = "1"
	next := current.DeepCopy()
	next.ResourceVersion = "2"

	b.ReportAllocs()
	for b.Loop() {
		sc.UpdatePod(current, next)
		current, next = next, current
	}
}
