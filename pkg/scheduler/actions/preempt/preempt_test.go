/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Rewritten tests using TestCommonStruct framework with comprehensive preemption scenarios
- Added TestTopologyAwarePreempt for topology-aware preemption testing

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
	"flag"
	"math"
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/binpack"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func init() {
	options.Default()
	klog.InitFlags(nil)
	flag.Set("v", "4")
}

func TestPreempt(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		priority.PluginName:    priority.New,
		proportion.PluginName:  proportion.New,
	}
	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)

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
			Name: "preempt low priority job in same queue but not pod with preemptable=false or higher priority",
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
			Name: nominatedNodeCase,
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 2, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			// preemptor1 carries a nominatedNodeName from an earlier cycle. With no
			// predicate plugins every node passes predicates, so upstream's bail-out
			// would drop preemptor1 and leave the gang at 1/2, discarding the
			// transaction every session.
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				buildPodWithNominatedNode(util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg2", make(map[string]string), make(map[string]string)), "n1"),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 2,
			ExpectEvicted:  []string{"c1/preemptee1", "c1/preemptee2"},
			ExpectPipeLined: map[string][]string{
				"c1/pg2": {"n1", "n2"},
			},
			ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
				"c1/pg2": {api.Pipelined: 2},
			},
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
			},
		}}

	actions := []framework.Action{New()}
	for i, test := range tests {
		test.Plugins = plugins
		test.PriClass = []*schedulingv1.PriorityClass{highPrio, lowPrio}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, []conf.Configuration{{Name: actions[0].Name(),
				Arguments: map[string]interface{}{EnableTopologyAwarePreemptionKey: false}}})
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

const (
	nominatedNodeCase = "task nominated to a node that passes predicates still preempts for its gang"
	// staleNominationCase is a gang task whose nominated node is not in the session.
	staleNominationCase = "task nominated to a node not in the session still preempts for its gang"
	retryCase           = "retry gang placement when the first task lands in a domain its siblings cannot join"
	drainingNodeCase    = "retry gang placement when the first task fits a draining node its siblings cannot join"
)

// allocateFirstCases run allocate before preempt, as in production, so that
// allocate's side effects (predicate cycle state, per-node fit errors, a
// discarded pipelining) are in place when preempt looks at the job.
var allocateFirstCases = sets.New(nominatedNodeCase, staleNominationCase, drainingNodeCase)

// exhaustiveCandidateCases predicate every node instead of a random sample,
// as production does, so that a retry's outcome depends only on the exclusion
// list and not on which nodes the sample happened to skip.
var exhaustiveCandidateCases = sets.New(retryCase, drainingNodeCase)

func TestTopologyAwarePreempt(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		conformance.PluginName: conformance.New,
		gang.PluginName:        gang.New,
		binpack.PluginName:     binpack.New,
		priority.PluginName:    priority.New,
		proportion.PluginName:  proportion.New,
		predicates.PluginName:  predicates.New,
	}
	highPrio := util.BuildPriorityClass("high-priority", 100000)
	lowPrio := util.BuildPriorityClass("low-priority", 10)
	mediumPrio := util.BuildPriorityClass("medium-priority", 20)

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
			Name: "preempt low priority job in same queue but not pod with preemptable=false or higher priority",
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
			Name: "preempt low-priority pod when high-priority pod has PodAntiAffinity conflict",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 0, map[string]int32{}, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				buildPodWithPodAntiAffinity("c1", "preemptee2", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true", "test": "test"}, make(map[string]string), "kubernetes.io/hostname"),

				buildPodWithPodAntiAffinity("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg3", map[string]string{"test": "test"}, make(map[string]string), "kubernetes.io/hostname"),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "2"}}...), map[string]string{"kubernetes.io/hostname": "n1"}),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, api.BuildResourceList("2", "2G")),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee2"},
		},
		{
			Name: "prefer binpack node when highest-priority victims tie",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G", api.ScalarResource{Name: "nvidia.com/gpu", Value: "1"}), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("1", "1G", api.ScalarResource{Name: "nvidia.com/gpu", Value: "1"}), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G", api.ScalarResource{Name: "nvidia.com/gpu", Value: "1"}), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "1G", api.ScalarResource{Name: "pods", Value: "10"}, api.ScalarResource{Name: "nvidia.com/gpu", Value: "10"}), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("1", "1G", api.ScalarResource{Name: "pods", Value: "10"}, api.ScalarResource{Name: "nvidia.com/gpu", Value: "2"}), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee2"},
		},
		{
			Name: "victim priority beats node-order score",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "medium-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPodWithPriority("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G", api.ScalarResource{Name: "nvidia.com/gpu", Value: "1"}), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &lowPrio.Value),
				util.BuildPodWithPriority("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("1", "1G", api.ScalarResource{Name: "nvidia.com/gpu", Value: "1"}), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &mediumPrio.Value),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G", api.ScalarResource{Name: "nvidia.com/gpu", Value: "1"}), "pg3", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "1G", api.ScalarResource{Name: "pods", Value: "10"}, api.ScalarResource{Name: "nvidia.com/gpu", Value: "10"}), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("1", "1G", api.ScalarResource{Name: "pods", Value: "10"}, api.ScalarResource{Name: "nvidia.com/gpu", Value: "2"}), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee1"},
		},
		{
			Name: "disable node-order score preserves default selection",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q1", 1, map[string]int32{"": 1}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G", api.ScalarResource{Name: "nvidia.com/gpu", Value: "2"}), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("500m", "500M", api.ScalarResource{Name: "nvidia.com/gpu", Value: "1"}), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n2", v1.PodRunning, api.BuildResourceList("500m", "500M", api.ScalarResource{Name: "nvidia.com/gpu", Value: "1"}), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G", api.ScalarResource{Name: "nvidia.com/gpu", Value: "2"}), "pg3", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "1G", api.ScalarResource{Name: "pods", Value: "10"}, api.ScalarResource{Name: "nvidia.com/gpu", Value: "10"}), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("1", "1G", api.ScalarResource{Name: "pods", Value: "10"}, api.ScalarResource{Name: "nvidia.com/gpu", Value: "2"}), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee1"},
		},
		{
			Name: "reuse capacity freed by a sibling's preemption instead of evicting on another node",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "medium-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q1", 2, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			// Evicting the low-priority victim on n1 frees 2 cpus; the second
			// preemptor task must pipeline into that hole instead of evicting
			// the medium-priority victim on n2.
			Pods: []*v1.Pod{
				util.BuildPodWithPriority("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &lowPrio.Value),
				util.BuildPodWithPriority("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &mediumPrio.Value),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg3", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg3", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 1,
			ExpectEvicted:  []string{"c1/preemptee1"},
			ExpectPipeLined: map[string][]string{
				"c1/pg3": {"n1"},
			},
		},
		{
			Name: nominatedNodeCase,
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q1", 2, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			// preemptor1 carries a nominatedNodeName from an earlier cycle. The
			// node passes predicates (they ignore resources) but is full, so the
			// task must stay eligible or the gang never reaches minAvailable.
			// n3 is tainted so neither task can use it; its idle capacity only
			// keeps the queue from being overused so allocate considers the tasks
			// and, as in production, leaves the predicate cycle state behind for
			// preempt to consult.
			Pods: []*v1.Pod{
				util.BuildPodWithPriority("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &lowPrio.Value),
				util.BuildPodWithPriority("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &lowPrio.Value),
				buildPodWithNominatedNode(util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg3", make(map[string]string), make(map[string]string)), "n1"),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg3", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				buildTaintedNode(util.BuildNode("n3", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 2,
			ExpectEvicted:  []string{"c1/preemptee1", "c1/preemptee2"},
			ExpectPipeLined: map[string][]string{
				"c1/pg3": {"n1", "n2"},
			},
		},
		{
			Name: staleNominationCase,
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q1", 2, map[string]int32{"": 2}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			// preemptor1 is still nominated to a node that has since left the
			// cluster (a reclaimed spot node). The nomination must not take the
			// task out of preemption, or the gang stays at 1/2 and every
			// session's transaction is discarded.
			Pods: []*v1.Pod{
				util.BuildPodWithPriority("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &lowPrio.Value),
				util.BuildPodWithPriority("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &lowPrio.Value),
				buildPodWithNominatedNode(util.BuildPod("c1", "preemptor1", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg3", make(map[string]string), make(map[string]string)), "reclaimed-node"),
				util.BuildPod("c1", "preemptor2", "", v1.PodPending, api.BuildResourceList("2", "2G"), "pg3", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				buildTaintedNode(util.BuildNode("n3", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 2,
			ExpectEvicted:  []string{"c1/preemptee1", "c1/preemptee2"},
			ExpectPipeLined: map[string][]string{
				"c1/pg3": {"n1", "n2"},
			},
		},
		{
			Name: retryCase,
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "medium-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q1", 2, map[string]int32{"worker": 2}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			// The gang's tasks must share a zone. The cheapest victim is in zone a,
			// which has a single node, so placing preemptor1 there strands
			// preemptor2. The retry must move the whole gang to zone b and evict
			// both medium-priority victims. The tasks share a task role so the
			// predicate error cache is live and must not leak across attempts.
			Pods: []*v1.Pod{
				util.BuildPodWithPriority("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &lowPrio.Value),
				util.BuildPodWithPriority("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &mediumPrio.Value),
				util.BuildPodWithPriority("c1", "preemptee3", "n3", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &mediumPrio.Value),
				buildGangWorker("c1", "preemptor1", api.BuildResourceList("2", "2G"), "pg3", "zone"),
				buildGangWorker("c1", "preemptor2", api.BuildResourceList("2", "2G"), "pg3", "zone"),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"zone": "a"}),
				util.BuildNode("n2", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"zone": "b"}),
				util.BuildNode("n3", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"zone": "b"}),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 2,
			ExpectEvicted:  []string{"c1/preemptee2", "c1/preemptee3"},
			ExpectPipeLined: map[string][]string{
				"c1/pg3": {"n2", "n3"},
			},
			ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
				"c1/pg3": {api.Pipelined: 2},
			},
		},
		{
			Name: drainingNodeCase,
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "low-priority"),
				util.BuildPodGroupWithPrio("pg2", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue, "medium-priority"),
				util.BuildPodGroupWithPrio("pg3", "c1", "q1", 2, map[string]int32{"worker": 2}, schedulingv1beta1.PodGroupInqueue, "high-priority"),
			},
			// The production shape. n1 (zone a) is draining a pod, so allocate
			// pipelines preemptor1 there, strands preemptor2 (pod affinity to its
			// sibling) and discards the transaction. preempt's first attempt
			// repeats that placement for free and fails the same way; the retry
			// must move the whole gang to zone b. n4 is tainted so neither task
			// can use it; its idle capacity only keeps the queue from being
			// overused so allocate considers the tasks at all.
			Pods: []*v1.Pod{
				buildReleasingPod(util.BuildPodWithPriority("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &lowPrio.Value)),
				util.BuildPodWithPriority("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &mediumPrio.Value),
				util.BuildPodWithPriority("c1", "preemptee3", "n3", v1.PodRunning, api.BuildResourceList("2", "2G"), "pg2", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string), &mediumPrio.Value),
				buildGangWorker("c1", "preemptor1", api.BuildResourceList("2", "2G"), "pg3", "zone"),
				buildGangWorker("c1", "preemptor2", api.BuildResourceList("2", "2G"), "pg3", "zone"),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"zone": "a"}),
				util.BuildNode("n2", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"zone": "b"}),
				util.BuildNode("n3", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"zone": "b"}),
				buildTaintedNode(util.BuildNode("n4", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectEvictNum: 2,
			ExpectEvicted:  []string{"c1/preemptee2", "c1/preemptee3"},
			ExpectPipeLined: map[string][]string{
				"c1/pg3": {"n2", "n3"},
			},
			ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
				"c1/pg3": {api.Pipelined: 2},
			},
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
					EnabledJobReady:     &trueValue,
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
					Name:             binpack.PluginName,
					EnabledNodeOrder: &trueValue,
					Arguments: map[string]interface{}{
						binpack.BinpackResources:           "nvidia.com/gpu",
						"binpack.resources.nvidia.com/gpu": 10,
					},
				},
				{
					Name:               proportion.PluginName,
					EnabledOverused:    &trueValue,
					EnabledAllocatable: &trueValue,
					EnabledQueueOrder:  &trueValue,
					EnabledPredicate:   &trueValue,
				},
				{
					Name:               predicates.PluginName,
					EnabledPreemptable: &trueValue,
					EnabledPredicate:   &trueValue,
				},
			},
		}}

	actions := []framework.Action{New()}
	for i, test := range tests {
		test.Plugins = plugins
		test.PriClass = []*schedulingv1.PriorityClass{highPrio, lowPrio, mediumPrio}
		t.Run(test.Name, func(t *testing.T) {
			enableNodeOrderScore := test.Name != "disable node-order score preserves default selection"
			candidateNodes := 2
			if exhaustiveCandidateCases.Has(test.Name) {
				candidateNodes = len(test.Nodes)
			}
			test.RegisterSession(tiers, []conf.Configuration{{Name: actions[0].Name(),
				Arguments: map[string]interface{}{
					EnableTopologyAwarePreemptionKey:    true,
					EnableNodeOrderScoreInPreemptionKey: enableNodeOrderScore,
					MinCandidateNodesAbsoluteKey:        candidateNodes,
					MaxCandidateNodesAbsoluteKey:        candidateNodes,
				}}})
			defer test.Close()
			if allocateFirstCases.Has(test.Name) {
				test.Run([]framework.Action{allocate.New(), New()})
			} else {
				test.Run(actions)
			}
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type fractionalNodeOrderPlugin struct{}

func (p *fractionalNodeOrderPlugin) Name() string {
	return "fractional-node-order"
}

func (p *fractionalNodeOrderPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddNodeOrderFn(p.Name(), func(_ *api.TaskInfo, node *api.NodeInfo) (float64, error) {
		if node.Name == "n1" {
			return 10.1, nil
		}
		return 10.9, nil
	})
}

func (p *fractionalNodeOrderPlugin) OnSessionClose(_ *framework.Session) {}

func TestNodeOrderScorePreservesFractionalScores(t *testing.T) {
	trueValue := true
	test := &uthelper.TestCommonStruct{
		Plugins: map[string]framework.PluginBuilder{
			"fractional-node-order": func(framework.Arguments) framework.Plugin {
				return &fractionalNodeOrderPlugin{}
			},
		},
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroup("pg1", "c1", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue),
		},
		Pods: []*v1.Pod{
			util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", nil, nil),
			util.BuildPod("c1", "preemptee2", "n2", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", nil, nil),
		},
		Nodes: []*v1.Node{
			util.BuildNode("n1", api.BuildResourceList("2", "2G"), nil),
			util.BuildNode("n2", api.BuildResourceList("2", "2G"), nil),
		},
		Queues: []*schedulingv1beta1.Queue{
			util.BuildQueue("q1", 1, nil),
		},
	}
	ssn := test.RegisterSession([]conf.Tier{{
		Plugins: []conf.PluginOption{{
			Name:             "fractional-node-order",
			EnabledNodeOrder: &trueValue,
		}},
	}}, nil)
	defer test.Close()

	nodesToVictims := map[string][]*api.TaskInfo{
		"n1": {ssn.Nodes["n1"].Tasks["c1/preemptee1"]},
		"n2": {ssn.Nodes["n2"].Tasks["c1/preemptee2"]},
	}
	score := nodeOrderScoreFunc(ssn, &api.TaskInfo{}, nodesToVictims)

	if score("n2") <= score("n1") {
		t.Fatalf("expected fractional node-order scores to distinguish n2 from n1, got n1=%d n2=%d", score("n1"), score("n2"))
	}
	if got := score("missing"); got != math.MinInt64 {
		t.Fatalf("expected missing node score %d, got %d", math.MinInt64, got)
	}

	scoreFuncs := []func(node string) int64{
		func(string) int64 { return 0 },
		score,
	}
	if got := pickOneNodeForPreemption(nodesToVictims, scoreFuncs); got != "n2" {
		t.Fatalf("expected n2 to win fractional node-order tie-breaker, got %s", got)
	}
}

func TestSortNodesByGradientPrefersIdleFit(t *testing.T) {
	trueValue := true
	test := &uthelper.TestCommonStruct{
		Plugins: map[string]framework.PluginBuilder{
			binpack.PluginName: binpack.New,
		},
	}
	ssn := test.RegisterSession([]conf.Tier{{
		Plugins: []conf.PluginOption{{
			Name:             binpack.PluginName,
			EnabledNodeOrder: &trueValue,
			Arguments: map[string]interface{}{
				binpack.BinpackResources:           "nvidia.com/gpu",
				"binpack.resources.nvidia.com/gpu": 10,
			},
		}},
	}}, nil)
	defer test.Close()

	gpu := func(value string) *api.Resource {
		return api.NewResource(api.BuildResourceList("0", "0", api.ScalarResource{Name: "nvidia.com/gpu", Value: value}))
	}
	req := gpu("1")
	task := &api.TaskInfo{
		Name:       "task",
		Namespace:  "c1",
		Resreq:     req,
		InitResreq: req,
	}

	// The draining node's committed future occupancy (Allocatable - FutureIdle)
	// gives it a higher binpack score than the idle-fit node, but the idle-fit
	// node must still be ordered first.
	idleFit := &api.NodeInfo{
		Name:        "idle-fit",
		Idle:        gpu("7"),
		Used:        gpu("1"),
		Releasing:   gpu("0"),
		Pipelined:   gpu("0"),
		Allocatable: gpu("8"),
	}
	draining := &api.NodeInfo{
		Name:        "draining",
		Idle:        gpu("0"),
		Used:        gpu("8"),
		Releasing:   gpu("8"),
		Pipelined:   gpu("4"),
		Allocatable: gpu("8"),
	}

	sorted := sortNodesByGradient(ssn, task, []*api.NodeInfo{draining, idleFit})
	if len(sorted) != 2 {
		t.Fatalf("expected 2 sorted nodes, got %d", len(sorted))
	}
	if sorted[0].Name != "idle-fit" {
		t.Fatalf("expected idle-fit node ordered before draining node, got %s first", sorted[0].Name)
	}
}

// buildTaintedNode adds a NoSchedule taint no test pod tolerates.
func buildTaintedNode(node *v1.Node) *v1.Node {
	node.Spec.Taints = []v1.Taint{{Key: "reserved", Effect: v1.TaintEffectNoSchedule}}
	return node
}

// buildReleasingPod marks a running pod as terminating, so the scheduler sees
// its resources as releasing: not idle yet, but free to pipeline onto.
func buildReleasingPod(pod *v1.Pod) *v1.Pod {
	now := metav1.Now()
	pod.DeletionTimestamp = &now
	return pod
}

// buildPodWithNominatedNode stamps a nominatedNodeName as a prior scheduling
// cycle's preemption would have.
func buildPodWithNominatedNode(pod *v1.Pod, nodeName string) *v1.Pod {
	pod.Status.NominatedNodeName = nodeName
	return pod
}

// buildGangWorker builds a pending pod of a gang whose members must share the
// topologyKey domain, expressed as a required pod affinity to the gang's own
// label. All workers share the "worker" task role.
func buildGangWorker(namespace, name string, req v1.ResourceList, groupName, topologyKey string) *v1.Pod {
	gangLabels := map[string]string{"gang": groupName}
	pod := util.BuildPod(namespace, name, "", v1.PodPending, req, groupName, gangLabels, make(map[string]string))
	pod.Annotations[batchv1alpha1.TaskSpecKey] = "worker"
	pod.Spec.Affinity = &v1.Affinity{
		PodAffinity: &v1.PodAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: gangLabels,
					},
					TopologyKey: topologyKey,
				},
			},
		},
	}
	return pod
}

func buildPodWithPodAntiAffinity(name, namespace, node string, phase v1.PodPhase, req v1.ResourceList, groupName string, labels map[string]string, selector map[string]string, topologyKey string) *v1.Pod {
	pod := util.BuildPod(name, namespace, node, phase, req, groupName, labels, selector)

	pod.Spec.Affinity = &v1.Affinity{
		PodAntiAffinity: &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: labels,
					},
					TopologyKey: topologyKey,
				},
			},
		},
	}

	return pod
}
