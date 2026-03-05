/*
Copyright 2018 The Kubernetes Authors.
Copyright 2019-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Rewritten tests using TestCommonStruct framework with comprehensive reclaim scenarios

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

package reclaim

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/capacity"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func init() {
	options.Default()
}

func TestReclaim(t *testing.T) {
	tests := []uthelper.TestCommonStruct{
		{
			Name: "Two Queue with one Queue overusing resource, should reclaim",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				proportion.PluginName:  proportion.New,
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
				util.BuildQueue("q2", 1, nil),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee2"}, // let pod2 in the middle when sort tasks be preemptable and will not disturb
		},
		{
			Name: "sort reclaimees when reclaiming from overusing queue",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				priority.PluginName:    priority.New,
				proportion.PluginName:  proportion.New,
			},
			PriClass: []*schedulingv1.PriorityClass{
				util.BuildPriorityClass("low-priority", 100),
				util.BuildPriorityClass("mid-priority", 500),
				util.BuildPriorityClass("high-priority", 1000),
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, "mid-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"), // reclaimed first
				util.BuildPodGroupWithPrio("pg3", "c1", "q3", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1-1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee1-2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2-1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2-2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg3", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
				util.BuildQueue("q2", 1, nil),
				util.BuildQueue("q3", 1, nil),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee2-1"}, // low priority job's preemptable pod is evicted
		},
		{
			Name: "sort reclaimees when reclaiming from overusing queues with different queue priority",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				proportion.PluginName:  proportion.New,
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, "mid-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupInqueue, "mid-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q3", 1, nil, schedulingv1beta1.PodGroupInqueue, "mid-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1-1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee1-2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2-1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2-2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg3", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueueWithPriorityAndResourcesQuantity("q1", 5, nil, nil),
				util.BuildQueueWithPriorityAndResourcesQuantity("q2", 10, nil, nil), // highest queue priority
				util.BuildQueueWithPriorityAndResourcesQuantity("q3", 1, nil, nil),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee1-1"}, // low queue priority job's preemptable pod is evicted
		},
		{
			// case about #3642
			Name: "can not reclaim resources when task preemption policy is never",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				proportion.PluginName:  proportion.New,
			},
			PriClass: []*schedulingv1.PriorityClass{
				util.BuildPriorityClass("low-priority", 100),
				util.BuildPriorityClassWithPreemptionPolicy("high-priority", 1000, v1.PreemptNever),
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q2", 0, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPreemptionPolicy("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string), v1.PreemptNever),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 5, nil),
				util.BuildQueue("q2", 10, nil),
			},
			ExpectEvictNum: 0,
			ExpectEvicted:  []string{}, // no victims should be reclaimed
		},
		{
			Name: "can not reclaim resources when queue is not open(proportion plugin)",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				proportion.PluginName:  proportion.New,
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
				util.BuildQueueWithState("q2", 1, nil, schedulingv1beta1.QueueStateClosed),
			},
			ExpectEvictNum: 0,
			ExpectEvicted:  []string{},
		},
		{
			Name: "can not reclaim resources when queue is not open(capacity plugin)",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				capacity.PluginName:    capacity.New,
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("3", "3Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
				util.BuildQueueWithState("q2", 1, nil, schedulingv1beta1.QueueStateClosed),
			},
			ExpectEvictNum: 0,
			ExpectEvicted:  []string{},
		},
		{
			Name: "Node available resources should be included in reclaimed resources",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				priority.PluginName:    priority.New,
				proportion.PluginName:  proportion.New,
			},
			PriClass: []*schedulingv1.PriorityClass{
				util.BuildPriorityClass("low-priority", 100),
				util.BuildPriorityClass("mid-priority", 500),
				util.BuildPriorityClass("high-priority", 1000),
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupRunning, "mid-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q2", 0, nil, schedulingv1beta1.PodGroupRunning, "mid-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q3", 1, nil, schedulingv1beta1.PodGroupInqueue, "mid-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1-1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2-1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("2", "1G"), "pg3", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("10", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueueWithPriorityAndResourcesQuantity("q1", 1, nil, nil),
				util.BuildQueueWithPriorityAndResourcesQuantity("q2", 2, nil, nil),
				util.BuildQueue("q3", 3, nil),
			},
			ExpectEvictNum: 1,
			// cpu resource is enough in node, memory resource is not enough, need 1G memory to schedule preemptor1
			ExpectEvicted: []string{"c1/preemptee1-1"},
		},
		{
			// Regression test for the per-node nodeStmt isolation introduced to fix
			// ghost evictions in reclaimForTask.
			//
			// Setup: two nodes; preemptor (q2) needs 2 CPU.
			//   Cluster total = 4 CPU → each queue deserves 2 CPU (equal weight).
			//   q1 allocated = 3 CPU (overusing by 1 CPU); q2 allocated = 0 CPU.
			//   Proportion Preemptive check: 0 + 2 = 2 ≤ 2 (q2.deserved) → passes.
			//
			//   n1: 1 CPU total, fully used by victim-n1 (1 CPU, q1, preemptable).
			//       FutureIdle=0; 0+1=1 < 2 → ValidateVictims fails → n1 skipped entirely.
			//   n2: 3 CPU total, 1 CPU idle + victim-n2 (2 CPU, q1, preemptable).
			//       FutureIdle=1; 1+2=3 ≥ 2 → ValidateVictims passes → reclaim succeeds.
			//
			// Expected: only victim-n2 is evicted. victim-n1 must NOT be evicted.
			//
			// Before the nodeStmt fix, evictions went directly into the outer Statement
			// without per-node isolation, so evictions from nodes that are later skipped
			// or fail could leak into stmt.Commit(). The nodeStmt pattern prevents this.
			Name: "only evict victims from the node where reclaim actually succeeds",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				proportion.PluginName:  proportion.New,
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg-victim", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
				util.BuildPodGroupWithPrio("pg-preemptor", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				// n1 victim: only 1 CPU freed — insufficient for 2 CPU preemptor;
				// ValidateVictims (0 idle + 1 CPU = 1 < 2) rejects n1 before any eviction.
				util.BuildPod("c1", "victim-n1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg-victim", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				// n2 victim: 2 CPU freed; n2 has 1 CPU idle, so 1+2=3 ≥ 2; reclaim succeeds.
				util.BuildPod("c1", "victim-n2", "n2", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg-victim", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg-preemptor", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				// n1: 1 CPU total, fully used by victim-n1.
				util.BuildNode("n1", api.BuildResourceList("1", "1G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				// n2: 3 CPU total; victim-n2 uses 2 CPU leaving 1 CPU idle.
				util.BuildNode("n2", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
				util.BuildQueue("q2", 1, nil),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/victim-n2"},
		},
		{
			Name: "Reclaim succeeds for second task when first task has PreemptionPolicy=Never",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
				gang.PluginName:        gang.New,
				proportion.PluginName:  proportion.New,
				priority.PluginName:    priority.New,
			},
			PriClass: []*schedulingv1.PriorityClass{
				util.BuildPriorityClass("low-priority", 100),
				util.BuildPriorityClassWithPreemptionPolicy("high-priority-no-preempt", 1000, v1.PreemptNever),
				util.BuildPriorityClass("high-priority-can-preempt", 900),
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg-victim", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
				util.BuildPodGroupWithPrio("pg-preemptor", "c1", "q2", 2, nil, schedulingv1beta1.PodGroupInqueue, "high-priority-no-preempt"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "victim-pod-no", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg-victim", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "victim-pod", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg-victim", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPreemptionPolicy("c1", "preemptor-task1-non-preemptable", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg-preemptor", make(map[string]string), make(map[string]string), v1.PreemptNever),
				util.BuildPod("c1", "preemptor-task2-preemptable", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg-preemptor", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
				util.BuildQueue("q2", 1, nil),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/victim-pod"},
		},
	}

	reclaim := New()
	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledReclaimable: &trueValue,
				},
				{
					Name:               gang.PluginName,
					EnabledReclaimable: &trueValue,
					EnabledJobStarving: &trueValue,
				},
				{ // proportion plugin will cause deserved resource larger than preemptable pods' usage, and return less victims
					Name:               proportion.PluginName,
					EnabledReclaimable: &trueValue,
					EnabledQueueOrder:  &trueValue,
					EnablePreemptive:   &trueValue,
				},
				{
					Name:               capacity.PluginName,
					EnabledReclaimable: &trueValue,
					EnabledQueueOrder:  &trueValue,
					EnablePreemptive:   &trueValue,
				},
				{
					Name:               priority.PluginName,
					EnabledReclaimable: &trueValue,
					EnabledJobOrder:    &trueValue,
					EnabledTaskOrder:   &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{reclaim})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
