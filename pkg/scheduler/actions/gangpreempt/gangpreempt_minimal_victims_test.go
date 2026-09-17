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

package gangpreempt

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// TestGangPreemptMinimalVictims runs the gangpreempt action end to end with victims of one
// low-priority gang spread over several nodes. Victims are 1-CPU pods named v0, v1, ... and
// are considered in that order (equal priority, then UID).
func TestGangPreemptMinimalVictims(t *testing.T) {
	tests := []struct {
		name string
		// nodes maps node name to its CPU capacity.
		nodes map[string]string
		// placement lists, in victim order, the node each victim runs on.
		placement []string
		// victimMinAvailable is the victim gang's minMember.
		victimMinAvailable int32
		// preemptorPods and preemptorCPU describe the high-priority gang (minMember = preemptorPods).
		preemptorPods int
		preemptorCPU  string
		// queueCapability optionally caps the shared queue.
		queueCapability v1.ResourceList
		expect          []string
	}{
		{
			// Reported in the review of #5995 (TestGangPreemptEvictsOnlyVictimsOnTheChosenNode).
			// Victims are added in order until the preemptor fits on n1 (v0..v6); v1, v3, v5 on n2 are
			// then dropped. Master evicts v0..v7.
			name:               "single preemptor: evict only the victims on the node it is placed on",
			nodes:              map[string]string{"n1": "4", "n2": "3", "n3": "2"},
			placement:          []string{"n1", "n2", "n1", "n2", "n1", "n2", "n1", "n3", "n3"},
			victimMinAvailable: 1,
			preemptorPods:      1,
			preemptorCPU:       "4",
			expect:             []string{"c1/v0", "c1/v2", "c1/v4", "c1/v6"},
		},
		{
			// The trim stops at v0..v4 (placed on n1 and n2); pruning drops v2 on n3.
			name:               "gang preemptor across two nodes: drop the victim on the third node",
			nodes:              map[string]string{"n1": "2", "n2": "2", "n3": "2"},
			placement:          []string{"n1", "n2", "n3", "n1", "n2", "n3"},
			victimMinAvailable: 1,
			preemptorPods:      2,
			preemptorCPU:       "2",
			expect:             []string{"c1/v0", "c1/v1", "c1/v3", "c1/v4"},
		},
		{
			// Master evicts the whole safe bundle (v0..v9); one victim is enough.
			name:               "single node: evict one victim for a one-pod preemptor",
			nodes:              map[string]string{"n1": "11"},
			placement:          []string{"n1", "n1", "n1", "n1", "n1", "n1", "n1", "n1", "n1", "n1", "n1"},
			victimMinAvailable: 1,
			preemptorPods:      1,
			preemptorCPU:       "1",
			expect:             []string{"c1/v0"},
		},
		{
			// No surplus: the only bundle is WHOLE. It spans n1 and n2 but the preemptor only
			// uses n1; the bundle must still be evicted as a unit.
			name:               "whole bundle is kept as a unit even if partly off the placed node",
			nodes:              map[string]string{"n1": "1", "n2": "1"},
			placement:          []string{"n1", "n2"},
			victimMinAvailable: 2,
			preemptorPods:      1,
			preemptorCPU:       "1",
			expect:             []string{"c1/v0", "c1/v1"},
		},
		{
			// The queue is at its 5-CPU capability. The preemptor fits on n0 once v0 is evicted, but
			// the queue needs 2 CPU freed, so a victim on another node is also needed. After v0, v1
			// places the job on n0; dropping v1 (not on n0) fails the re-simulation on quota, so v0
			// and v1 are kept. Master evicts the whole safe bundle v0..v3.
			name:               "queue at capability: keep the off-node victim that frees quota",
			nodes:              map[string]string{"n0": "2", "n1": "2", "n2": "2"},
			placement:          []string{"n0", "n1", "n2", "n1", "n2"},
			victimMinAvailable: 1,
			preemptorPods:      1,
			preemptorCPU:       "2",
			queueCapability:    api.BuildResourceList("5", "5G"),
			expect:             []string{"c1/v0", "c1/v1"},
		},
		{
			// The queue is at its 2-CPU capability and the preemptor fits on the empty n0 without
			// evicting anything there; it only needs 2 CPU of queue quota freed, so both victims
			// (on n1 and n2) must be evicted even though the preemptor runs on neither node.
			name:               "queue at capability: evict victims on other nodes only to free quota",
			nodes:              map[string]string{"n0": "2", "n1": "1", "n2": "1"},
			placement:          []string{"n1", "n2"},
			victimMinAvailable: 1,
			preemptorPods:      1,
			preemptorCPU:       "2",
			queueCapability:    api.BuildResourceList("2", "2G"),
			expect:             []string{"c1/v0", "c1/v1"},
		},
	}

	plugins := map[string]framework.PluginBuilder{
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		priority.PluginName:    priority.New,
		proportion.PluginName:  proportion.New,
	}
	trueValue := true
	tiers := []conf.Tier{{Plugins: []conf.PluginOption{
		{Name: conformance.PluginName, EnabledPreemptable: &trueValue},
		{Name: gang.PluginName, EnabledPreemptable: &trueValue, EnabledJobPipelined: &trueValue, EnabledJobStarving: &trueValue},
		{Name: priority.PluginName, EnabledTaskOrder: &trueValue, EnabledJobOrder: &trueValue, EnabledPreemptable: &trueValue, EnabledJobPipelined: &trueValue, EnabledJobStarving: &trueValue},
		{Name: proportion.PluginName, EnabledOverused: &trueValue, EnabledAllocatable: &trueValue, EnabledQueueOrder: &trueValue},
	}}}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			preemptable := map[string]string{schedulingv1beta1.PodPreemptable: "true"}
			pods := []*v1.Pod{}
			for j, node := range tc.placement {
				pods = append(pods, util.BuildPod("c1", fmt.Sprintf("v%d", j), node, v1.PodRunning,
					api.BuildResourceList("1", "1G"), "pg1", preemptable, make(map[string]string)))
			}
			for j := 0; j < tc.preemptorPods; j++ {
				pods = append(pods, util.BuildPod("c1", fmt.Sprintf("preemptor-%d", j), "", v1.PodPending,
					api.BuildResourceList(tc.preemptorCPU, "1G"), "pg2", make(map[string]string), make(map[string]string)))
			}
			nodes := []*v1.Node{}
			for name, cpu := range tc.nodes {
				nodes = append(nodes, util.BuildNode(name, api.BuildResourceList(cpu, cpu+"G", []api.ScalarResource{{Name: "pods", Value: "110"}}...), make(map[string]string)))
			}

			test := uthelper.TestCommonStruct{
				Name:    tc.name,
				Plugins: plugins,
				PodGroups: []*schedulingv1beta1.PodGroup{
					util.BuildPodGroupWithPrio("pg1", "c1", "q1", tc.victimMinAvailable, map[string]int32{"": tc.victimMinAvailable}, schedulingv1beta1.PodGroupRunning, "low-priority"),
					util.BuildPodGroupWithPrio("pg2", "c1", "q1", int32(tc.preemptorPods), map[string]int32{"": int32(tc.preemptorPods)}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
				},
				Pods:           pods,
				Nodes:          nodes,
				Queues:         []*schedulingv1beta1.Queue{util.BuildQueue("q1", 1, tc.queueCapability)},
				PriClass:       []*schedulingv1.PriorityClass{util.BuildPriorityClass("high-priority", 100000), util.BuildPriorityClass("low-priority", 10)},
				ExpectEvicted:  tc.expect,
				ExpectEvictNum: len(tc.expect),
			}
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckEvict(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
