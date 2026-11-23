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

package backfill

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestBackfillE2eJobSchedulingMetrics(t *testing.T) {
	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:            gang.PluginName,
					EnabledJobReady: &trueValue,
				},
			},
		},
	}
	bestEffort := api.BuildResourceList("0", "0")

	tests := []struct {
		name               string
		pg                 *schedulingv1beta1.PodGroup
		pods               []*v1.Pod
		expectBinds        int
		expectLatencyDelta uint64
		expectDuration     bool
	}{
		{
			// Backfill allocates both best-effort pods, but the job only becomes ready with the
			// third pod, which does not fit, so no task is dispatched for binding.
			name: "job that does not become ready is not recorded",
			pg:   util.BuildPodGroup("pg-not-ready", "c1", "c1", 3, nil, schedulingv1beta1.PodGroupInqueue),
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg-not-ready-p0", "", v1.PodPending, bestEffort, "pg-not-ready", nil, nil),
				util.BuildPod("c1", "pg-not-ready-p1", "", v1.PodPending, bestEffort, "pg-not-ready", nil, nil),
				util.BuildPod("c1", "pg-not-ready-p2", "", v1.PodPending, api.BuildResourceList("20", "1Gi"), "pg-not-ready", nil, nil),
			},
			expectBinds:        0,
			expectLatencyDelta: 0,
			expectDuration:     false,
		},
		{
			name: "ready job is recorded once, not once per task",
			pg:   util.BuildPodGroup("pg-ready", "c1", "c1", 1, nil, schedulingv1beta1.PodGroupInqueue),
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg-ready-p0", "", v1.PodPending, bestEffort, "pg-ready", nil, nil),
				util.BuildPod("c1", "pg-ready-p1", "", v1.PodPending, bestEffort, "pg-ready", nil, nil),
			},
			expectBinds:        2,
			expectLatencyDelta: 1,
			expectDuration:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { metrics.DeleteJobMetrics(tc.pg.Name, "c1", tc.pg.Namespace) })
			before, _, err := uthelper.E2eJobSchedulingMetrics(tc.pg.Namespace, tc.pg.Name)
			if err != nil {
				t.Fatal(err)
			}

			test := uthelper.TestCommonStruct{
				Name:      tc.name,
				Plugins:   map[string]framework.PluginBuilder{gang.PluginName: gang.New},
				PodGroups: []*schedulingv1beta1.PodGroup{tc.pg},
				Pods:      tc.pods,
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
				},
				Queues:           []*schedulingv1beta1.Queue{util.BuildQueue("c1", 1, nil)},
				ExpectBindsNum:   tc.expectBinds,
				MinimalBindCheck: true,
			}
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckBind(0); err != nil {
				t.Fatal(err)
			}

			after, hasDuration, err := uthelper.E2eJobSchedulingMetrics(tc.pg.Namespace, tc.pg.Name)
			if err != nil {
				t.Fatal(err)
			}
			if got := after - before; got != tc.expectLatencyDelta {
				t.Errorf("e2e job scheduling latency samples = %d, want %d", got, tc.expectLatencyDelta)
			}
			if hasDuration != tc.expectDuration {
				t.Errorf("e2e job scheduling duration series present = %v, want %v", hasDuration, tc.expectDuration)
			}
		})
	}
}
