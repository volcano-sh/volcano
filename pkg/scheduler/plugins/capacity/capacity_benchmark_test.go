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

package capacity

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	benchQueues      = 50
	benchJobs        = 1000
	benchTasksPerJob = 20
	benchNodes       = 100
)

// openBenchmarkSession opens a Session over a mock cache holding benchJobs jobs
// of benchTasksPerJob 1-CPU tasks, half running and half pending, spread over
// benchQueues child queues of root. The recorder drops events: CloseSession
// records one per job, which would block the mock's default 100-event recorder.
func openBenchmarkSession(b *testing.B, tiers []conf.Tier) *framework.Session {
	b.Helper()
	sc := cache.NewCustomMockSchedulerCache("volcano", nil, nil, &util.FakeStatusUpdater{}, nil, &record.FakeRecorder{})
	for n := 0; n < benchNodes; n++ {
		if err := sc.AddOrUpdateNode(util.BuildNode(fmt.Sprintf("n%d", n),
			api.BuildResourceList("1000", "1000Gi", []api.ScalarResource{{Name: "pods", Value: "1000"}}...), nil)); err != nil {
			b.Fatal(err)
		}
	}
	sc.AddQueueV1beta1(util.BuildQueue("root", 1, nil))
	for q := 0; q < benchQueues; q++ {
		queue := util.BuildQueue(fmt.Sprintf("q%d", q), 1, nil)
		queue.Spec.Parent = "root"
		sc.AddQueueV1beta1(queue)
	}
	for j := 0; j < benchJobs; j++ {
		pg := fmt.Sprintf("pg%d", j)
		sc.AddPodGroupV1beta1(util.BuildPodGroup(pg, "ns1", fmt.Sprintf("q%d", j%benchQueues),
			benchTasksPerJob, nil, schedulingv1beta1.PodGroupRunning))
		for i := 0; i < benchTasksPerJob; i++ {
			node, phase := fmt.Sprintf("n%d", (j*benchTasksPerJob+i)%benchNodes), corev1.PodRunning
			if i%2 == 1 {
				node, phase = "", corev1.PodPending
			}
			sc.AddPod(util.BuildPod("ns1", fmt.Sprintf("%s-%d", pg, i), node, phase,
				api.BuildResourceList("1", "1Gi"), pg, nil, nil))
		}
	}

	framework.RegisterPluginBuilder(PluginName, New)
	ssn := framework.OpenSession(sc, tiers, nil)
	b.Cleanup(func() {
		framework.CloseSession(ssn)
		framework.CleanupPluginBuilders()
		cache.ShutdownMockSchedulerCache(sc)
	})
	return ssn
}

// BenchmarkCapacityOnSessionOpen measures building the per-queue attributes,
// with and without hierarchy. Run it with:
//
//	go test ./pkg/scheduler/plugins/capacity/ -run '^$' -bench OnSessionOpen -benchmem
func BenchmarkCapacityOnSessionOpen(b *testing.B) {
	for _, hierarchy := range []bool{false, true} {
		b.Run(fmt.Sprintf("hierarchy=%t", hierarchy), func(b *testing.B) {
			ssn := openBenchmarkSession(b, []conf.Tier{{Plugins: []conf.PluginOption{
				{Name: PluginName, EnabledHierarchy: &hierarchy},
			}}})

			// q0 holds benchJobs/benchQueues jobs, each with half its 1-CPU tasks running.
			wantAllocated := float64(benchJobs/benchQueues*benchTasksPerJob/2) * 1000
			plugin := New(nil).(*capacityPlugin)
			plugin.OnSessionOpen(ssn)
			if got := plugin.queueOpts["q0"].allocated.MilliCPU; got != wantAllocated {
				b.Fatalf("q0 allocated = %v, want %v", got, wantAllocated)
			}

			b.ReportAllocs()
			for b.Loop() {
				New(nil).OnSessionOpen(ssn)
			}
		})
	}
}
