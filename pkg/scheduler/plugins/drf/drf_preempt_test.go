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

package drf

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	vcapisv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// TestDRFPreemptable_MultipleTasksSameJob reproduces #6412: PreemptableFn must not
// mutate per-job simulated allocation for preemptees that are not selected as victims.
func TestDRFPreemptable_MultipleTasksSameJob(t *testing.T) {
	enabled := true
	plugins := map[string]framework.PluginBuilder{PluginName: New}

	test := uthelper.TestCommonStruct{
		Name:    "drf preempt multiple tasks same victim job",
		Plugins: plugins,
		Nodes: []*v1.Node{
			util.BuildNode("n1", api.BuildResourceList("100", "0"), nil),
		},
		Queues: []*vcapisv1.Queue{
			util.BuildQueue("q-victim", 1, api.BuildResourceList("100", "0")),
			util.BuildQueue("q-preemptor", 1, api.BuildResourceList("100", "0")),
		},
		PodGroups: []*vcapisv1.PodGroup{
			util.BuildPodGroup("pg-victim", "ns1", "q-victim", 2, nil, vcapisv1.PodGroupRunning),
			util.BuildPodGroup("pg-preemptor", "ns2", "q-preemptor", 2, nil, vcapisv1.PodGroupInqueue),
		},
		Pods: []*v1.Pod{
			util.BuildPod("ns1", "victim-large", "n1", v1.PodRunning, api.BuildResourceList("80", "0"), "pg-victim",
				map[string]string{vcapisv1.PodPreemptable: "true"}, nil),
			util.BuildPod("ns1", "victim-small", "n1", v1.PodRunning, api.BuildResourceList("10", "0"), "pg-victim",
				map[string]string{vcapisv1.PodPreemptable: "true"}, nil),
			util.BuildPod("ns2", "preemptor-running", "n1", v1.PodRunning, api.BuildResourceList("40", "0"), "pg-preemptor",
				nil, nil),
			util.BuildPod("ns2", "preemptor-pending", "", v1.PodPending, api.BuildResourceList("10", "0"), "pg-preemptor",
				nil, nil),
		},
	}

	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               PluginName,
					EnabledPreemptable: &enabled,
				},
			},
		},
	}
	ssn := test.RegisterSession(tiers, nil)
	defer test.Close()

	var preemptor *api.TaskInfo
	var preemptees []*api.TaskInfo
	for _, job := range ssn.Jobs {
		for _, task := range job.Tasks {
			if task.Name == "preemptor-pending" {
				preemptor = task
			}
			if task.Namespace == "ns1" && task.Status == api.Running {
				preemptees = append(preemptees, task)
			}
		}
	}
	if preemptor == nil || len(preemptees) != 2 {
		t.Fatalf("setup: preemptor=%v preemptees=%d", preemptor, len(preemptees))
	}

	// Evaluate larger task first (order-dependent bug).
	var large, small *api.TaskInfo
	for _, task := range preemptees {
		if task.Name == "victim-large" {
			large = task
		} else {
			small = task
		}
	}
	ordered := []*api.TaskInfo{large, small}

	victims := ssn.Preemptable(preemptor, ordered)
	if len(victims) != 1 {
		t.Fatalf("expected 1 victim, got %d (%v)", len(victims), victimNames(victims))
	}
	if victims[0].Name != "victim-small" {
		t.Fatalf("expected victim-small, got %s", victims[0].Name)
	}
}

func victimNames(tasks []*api.TaskInfo) []string {
	names := make([]string, len(tasks))
	for i, task := range tasks {
		names[i] = task.Name
	}
	return names
}