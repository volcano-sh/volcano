/*
Copyright 2018 The Kubernetes Authors.

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

package preempt

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

func TestPreempt(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		capacity.PluginName:    capacity.New,
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		priority.PluginName:    priority.New,
		proportion.PluginName:  proportion.New,
	}
	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)
	options.Default()

	tests := []uthelper.TestCommonStruct{
		{
			Name: "do not preempt if there are enough idle resources",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 3, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
			},
			// If there are enough idle resources on the node, then there is no need to preempt anything.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "do not preempt if job is pipelined",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue),
				util.BuildPodGroup("pg2", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue),
			},
			// Both pg1 and pg2 jobs are pipelined, because enough pods are already running.
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			// All resources on the node will be in use.
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("3", "3G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
		},
		{
			Name: "preempt one task of different job to fit both jobs on one node",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			Name: "preempt enough tasks to fit large task of different job",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			// There are 3 cpus and 3G of memory idle and 3 tasks running each consuming 1 cpu and 1G of memory.
			// Big task requiring 5 cpus and 5G of memory should preempt 2 of 3 running tasks to fit into the node.
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("5", "5G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("6", "6G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvicted:  []string{"c1/preemptee2", "c1/preemptee1"},
			ExpectEvictNum: 2,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("4", "4G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3161
			Name: "preempt low priority job in same queue: allocatable and has enough resource, don't preempt",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("3", "3G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvictNum: 0,
		},
		{
			// case about issue #2232
			Name: "preempt low priority job in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 1, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "false"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPriority("c1", "preemptee3", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &highPrio.Value),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("3", "3G")),
			},
			ExpectEvicted:  []string{"c1/preemptee2"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "unBestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, v1.ResourceList{}, "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("3", "3G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3335
			Name: "BestEffort high-priority pod preempt BestEffort low-priority pod in same queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, v1.ResourceList{}, "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, v1.ResourceList{}, "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("6", "6G")),
			},
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			// case about #3642
			Name: "can not preempt resources when task preemption policy is never",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPreemptionPolicy("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string), v1.PreemptNever),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "1Gi", []api.ScalarResource{{Name: "pods", Value: "1"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 0,
			ExpectEvicted:  []string{}, // no victims should be reclaimed
		},
		{
			// Regression test for the preemptorTasks overwrite issue in multi-queue preemption.
			//
			// Instead of:
			//    intraJobPreemptors := util.NewPriorityQueue(ssn.TaskOrderFn)
			// We have used:
			//    preemptorTasks[job.UID] = util.NewPriorityQueue(ssn.TaskOrderFn)
			// in the "Preemption between Task within Job" loop, which caused preemptorTasks to be overwritten/drained across queues.
			// This test verifies that the preemptorTasks for pg3 (high-priority preemptor in q2) is not overwritten/drained when processing q1, so that pg3 can successfully preempt pg2.
			//
			// Scenario:
			// - q1 has a running non-starving job (pg1) and no preemptor.
			// - q2 has a low-priority running victim (pg2) and a high-priority starving
			//   preemptor job (pg3).
			// - underRequest is shared across queues.
			//
			// Buggy behavior:
			// - While processing q1, the intra-job pass overwrites/drains
			//   preemptorTasks[pg3], so q2 later sees no preemptor and skips eviction.
			//
			// Why this was flaky:
			// - Queue iteration order came from a Go map, so the run usually passed when
			//   q2 was visited first, but failed when q1 was visited first.
			Name: "multi-queue: preemptorTasks must not be overwritten by intra-job preemption of another queue",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue),
				util.BuildPodGroupWithPrio("pg2", "c1", "q2", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q2", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "q1-runner1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "q2-preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "q2-preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg3", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
				util.BuildQueue("q2", 1, api.BuildResourceList("4", "4G")),
			},
			ExpectEvicted:  []string{"c1/q2-preemptee1"},
			ExpectEvictNum: 1,
		},
		{
			Name: "preemption with priority queues",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg3", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupRunning, "high-priority"),
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupRunning, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg3", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPodWithPreemptionPolicy("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string), v1.PreemptLowerPriority),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueueWithPriorityAndResourcesQuantity("q1", 1, api.BuildResourceList("1", "1G"), api.BuildResourceList("1", "1G")),
				util.BuildQueueWithPriorityAndResourcesQuantity("q2", 10, api.BuildResourceList("1", "1G"), api.BuildResourceList("1", "1G")),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee1"},
		},
		{
			// Equivalent behavior to the fix in #5214 (SelectVictimsOnNode in master).
			// When multiple victims of differing pod priorities are eligible for eviction
			// and only one eviction is needed, the lowest-priority victim must be chosen,
			// sparing the higher-priority victim.
			Name: "evict lowest priority victim first to spare higher priority pods",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				// Two preemptable victims in the same low-priority job with different pod priorities.
				// pg1's two tasks consume all 2 CPUs of the queue capacity, so the queue is full.
				util.BuildPodWithPriority("c1", "victim-low-prio", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1",
					map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &lowPrio.Value),
				util.BuildPodWithPriority("c1", "victim-high-prio", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1",
					map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &highPrio.Value),
				// The preemptor only needs 1 CPU — a single eviction is sufficient.
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2",
					make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("12", "12G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				// Queue capacity matches the total usage of pg1 (2 CPUs), so the preemptor
				// cannot be accommodated without evicting at least one victim.
				util.BuildQueue("q1", 1, api.BuildResourceList("2", "2G")),
			},
			// Only the lowest-priority victim should be evicted; the higher-priority one is spared.
			ExpectEvicted:  []string{"c1/victim-low-prio"},
			ExpectEvictNum: 1,
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               conformance.PluginName,
					EnabledPreemptable: &trueValue,
				},
				{
					Name:                gang.PluginName,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:                priority.PluginName,
					EnabledTaskOrder:    &trueValue,
					EnabledJobOrder:     &trueValue,
					EnabledPreemptable:  &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledOverused:    &trueValue,
					EnabledAllocatable: &trueValue,
					EnabledQueueOrder:  &trueValue,
				},
				{
					Name:              capacity.PluginName,
					EnabledQueueOrder: &trueValue,
				},
			},
		}}

	actions := []framework.Action{New()}
	for i, test := range tests {
		test.Plugins = plugins
		test.PriClass = []*schedulingv1.PriorityClass{highPrio, lowPrio}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
