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
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// releasingPod builds a running pod that is being deleted, whose resources are releasing.
func releasingPod(namespace, name, nodeName string, req v1.ResourceList) *v1.Pod {
	pod := util.BuildPod(namespace, name, nodeName, v1.PodRunning, req, "", nil, nil)
	pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	return pod
}

func TestAllocateE2eJobSchedulingMetrics(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		gang.PluginName:       gang.New,
		predicates.PluginName: predicates.New,
	}
	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
			},
		},
	}
	nodes := []*v1.Node{
		util.BuildNode("n1", api.BuildResourceList("16", "32Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
		util.BuildNode("n2", api.BuildResourceList("16", "32Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
	}
	queues := []*schedulingv1.Queue{util.BuildQueue("c1", 1, nil)}
	buildPods := func(pg string, replicas int, cpu string) []*v1.Pod {
		pods := make([]*v1.Pod, 0, replicas)
		for i := 0; i < replicas; i++ {
			pods = append(pods, util.BuildPod("c1", fmt.Sprintf("%s-p%d", pg, i), "", v1.PodPending,
				api.BuildResourceList(cpu, "1Gi"), pg, nil, nil))
		}
		return pods
	}

	tests := []struct {
		name string
		pg   *schedulingv1.PodGroup
		pods []*v1.Pod
		// cycles is the number of scheduling cycles to run.
		cycles             int
		expectBinds        int
		expectLatencyDelta uint64
		expectDuration     bool
	}{
		{
			// The scenario of https://github.com/volcano-sh/volcano/issues/6328: only 2 of the
			// 3 gang members fit, so the statement is discarded in every cycle.
			name:               "gang job that cannot be fully placed is not recorded",
			pg:                 util.BuildPodGroup("pg-stuck", "c1", "c1", 3, nil, schedulingv1.PodGroupInqueue),
			pods:               buildPods("pg-stuck", 3, "10"),
			cycles:             3,
			expectBinds:        0,
			expectLatencyDelta: 0,
			expectDuration:     false,
		},
		{
			name:               "gang job is recorded once, not once per task",
			pg:                 util.BuildPodGroup("pg-gang", "c1", "c1", 3, nil, schedulingv1.PodGroupInqueue),
			pods:               buildPods("pg-gang", 3, "1"),
			cycles:             1,
			expectBinds:        3,
			expectLatencyDelta: 1,
			expectDuration:     true,
		},
		{
			name:               "job committed in several rounds is recorded once",
			pg:                 util.BuildPodGroup("pg-elastic", "c1", "c1", 1, nil, schedulingv1.PodGroupInqueue),
			pods:               buildPods("pg-elastic", 3, "1"),
			cycles:             1,
			expectBinds:        3,
			expectLatencyDelta: 1,
			expectDuration:     true,
		},
		{
			// minAvailable is met by the running pod, so the job is ready and its statement is
			// committed although the pending pod does not fit anywhere.
			name: "ready job whose statement places no task is not recorded",
			pg:   util.BuildPodGroup("pg-ready", "c1", "c1", 1, nil, schedulingv1.PodGroupRunning),
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg-ready-p0", "n1", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg-ready", nil, nil),
				util.BuildPod("c1", "pg-ready-p1", "", v1.PodPending, api.BuildResourceList("20", "1Gi"), "pg-ready", nil, nil),
			},
			cycles:             2,
			expectBinds:        0,
			expectLatencyDelta: 0,
			expectDuration:     false,
		},
		{
			// The pending pod only fits on n1 once the releasing pod is gone, so it is pipelined,
			// and committing the ready job's statement does not dispatch it for binding.
			name: "ready job whose statement only pipelines a task is not recorded",
			pg:   util.BuildPodGroup("pg-pipelined", "c1", "c1", 1, nil, schedulingv1.PodGroupRunning),
			pods: []*v1.Pod{
				util.BuildPod("c1", "pg-pipelined-p0", "n2", v1.PodRunning, api.BuildResourceList("16", "1Gi"), "pg-pipelined", nil, nil),
				util.BuildPod("c1", "pg-pipelined-p1", "", v1.PodPending, api.BuildResourceList("16", "1Gi"), "pg-pipelined", nil, nil),
				releasingPod("c1", "other-p0", "n1", api.BuildResourceList("16", "1Gi")),
			},
			cycles:             1,
			expectBinds:        0,
			expectLatencyDelta: 0,
			expectDuration:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { metrics.DeleteJobMetrics(tc.pg.Name, "c1", tc.pg.Namespace) })
			before, _, err := uthelper.E2eJobSchedulingMetrics(tc.pg.Namespace, tc.pg.Name)
			if err != nil {
				t.Fatal(err)
			}

			for cycle := 0; cycle < tc.cycles; cycle++ {
				test := uthelper.TestCommonStruct{
					Name:             tc.name,
					Plugins:          plugins,
					PodGroups:        []*schedulingv1.PodGroup{tc.pg},
					Pods:             tc.pods,
					Nodes:            nodes,
					Queues:           queues,
					ExpectBindsNum:   tc.expectBinds,
					MinimalBindCheck: true,
				}
				test.RegisterSession(tiers, nil)
				test.Run([]framework.Action{New()})
				if err := test.CheckBind(cycle); err != nil {
					t.Fatal(err)
				}
				test.Close()
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
