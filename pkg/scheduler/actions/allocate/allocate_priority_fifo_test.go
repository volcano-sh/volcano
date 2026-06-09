/*
Copyright 2024 The Volcano Authors.

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
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1k8s "k8s.io/api/scheduling/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	vcschedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestPriorityFIFO(t *testing.T) {
	trueValue := true

	plugins := map[string]framework.PluginBuilder{
		proportion.PluginName: proportion.New,
		predicates.PluginName: predicates.New,
		gang.PluginName:       gang.New,
		priority.PluginName:   priority.New,
	}

	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledQueueOrder:  &trueValue,
					EnabledAllocatable: &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:            priority.PluginName,
					EnabledJobOrder: &trueValue,
				},
			},
		},
	}

	highPri := int32(1000)
	lowPri := int32(500)

	tests := []uthelper.TestCommonStruct{
		{
			Name: "PriorityFIFO: same priority job can proceed after failure, lower priority blocked",
			PodGroups: []*vcschedulingv1.PodGroup{
				util.BuildPodGroupWithPrio("pg-a", "ns1", "q1", 1, nil, vcschedulingv1.PodGroupInqueue, "high"),
				util.BuildPodGroupWithPrio("pg-b", "ns1", "q1", 1, nil, vcschedulingv1.PodGroupInqueue, "high"),
				util.BuildPodGroupWithPrio("pg-c", "ns1", "q1", 1, nil, vcschedulingv1.PodGroupInqueue, "low"),
			},
			Pods: []*v1.Pod{
				util.BuildPodWithPriority("ns1", "pod-a", "", v1.PodPending, api.BuildResourceList("4", "1G"), "pg-a", make(map[string]string), make(map[string]string), &highPri),
				util.BuildPodWithPriority("ns1", "pod-b", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg-b", make(map[string]string), make(map[string]string), &highPri),
				util.BuildPodWithPriority("ns1", "pod-c", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg-c", make(map[string]string), make(map[string]string), &lowPri),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			Queues: []*vcschedulingv1.Queue{
				util.BuildQueueWithDequeueStrategy("q1", 1, nil, vcschedulingv1.DequeueStrategyPriorityFIFO),
			},
			PriClass: []*schedulingv1k8s.PriorityClass{
				util.BuildPriorityClass("high", 1000),
				util.BuildPriorityClass("low", 500),
			},
			ExpectBindMap: map[string]string{
				"ns1/pod-b": "n1",
			},
			ExpectBindsNum: 1,
		},
		{
			Name: "PriorityFIFO: all same priority, all can proceed",
			PodGroups: []*vcschedulingv1.PodGroup{
				util.BuildPodGroupWithPrio("pg-a", "ns1", "q1", 1, nil, vcschedulingv1.PodGroupInqueue, "high"),
				util.BuildPodGroupWithPrio("pg-b", "ns1", "q1", 1, nil, vcschedulingv1.PodGroupInqueue, "high"),
				util.BuildPodGroupWithPrio("pg-c", "ns1", "q1", 1, nil, vcschedulingv1.PodGroupInqueue, "high"),
			},
			Pods: []*v1.Pod{
				util.BuildPodWithPriority("ns1", "pod-a", "", v1.PodPending, api.BuildResourceList("4", "1G"), "pg-a", make(map[string]string), make(map[string]string), &highPri),
				util.BuildPodWithPriority("ns1", "pod-b", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg-b", make(map[string]string), make(map[string]string), &highPri),
				util.BuildPodWithPriority("ns1", "pod-c", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg-c", make(map[string]string), make(map[string]string), &highPri),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			Queues: []*vcschedulingv1.Queue{
				util.BuildQueueWithDequeueStrategy("q1", 1, nil, vcschedulingv1.DequeueStrategyPriorityFIFO),
			},
			PriClass: []*schedulingv1k8s.PriorityClass{
				util.BuildPriorityClass("high", 1000),
			},
			ExpectBindMap: map[string]string{
				"ns1/pod-b": "n1",
				"ns1/pod-c": "n1",
			},
			ExpectBindsNum: 2,
		},
		{
			Name: "FIFO backward compatibility: all blocked after first failure",
			PodGroups: []*vcschedulingv1.PodGroup{
				util.BuildPodGroupWithPrio("pg-a", "ns1", "q1", 1, nil, vcschedulingv1.PodGroupInqueue, "high"),
				util.BuildPodGroupWithPrio("pg-b", "ns1", "q1", 1, nil, vcschedulingv1.PodGroupInqueue, "high"),
			},
			Pods: []*v1.Pod{
				util.BuildPodWithPriority("ns1", "pod-a", "", v1.PodPending, api.BuildResourceList("4", "1G"), "pg-a", make(map[string]string), make(map[string]string), &highPri),
				util.BuildPodWithPriority("ns1", "pod-b", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg-b", make(map[string]string), make(map[string]string), &highPri),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			Queues: []*vcschedulingv1.Queue{
				util.BuildQueueWithDequeueStrategy("q1", 1, nil, vcschedulingv1.DequeueStrategyFIFO),
			},
			PriClass: []*schedulingv1k8s.PriorityClass{
				util.BuildPriorityClass("high", 1000),
			},
			ExpectBindMap:  map[string]string{},
			ExpectBindsNum: 0,
		},
		{
			Name: "FIFO: same priority and lower priority blocked after failure",
			PodGroups: []*vcschedulingv1.PodGroup{
				util.BuildPodGroupWithPrio("pg-a", "ns1", "q1", 1, nil, vcschedulingv1.PodGroupInqueue, "high"),
				util.BuildPodGroupWithPrio("pg-b", "ns1", "q1", 1, nil, vcschedulingv1.PodGroupInqueue, "high"),
				util.BuildPodGroupWithPrio("pg-c", "ns1", "q1", 1, nil, vcschedulingv1.PodGroupInqueue, "low"),
			},
			Pods: []*v1.Pod{
				util.BuildPodWithPriority("ns1", "pod-a", "", v1.PodPending, api.BuildResourceList("4", "1G"), "pg-a", make(map[string]string), make(map[string]string), &highPri),
				util.BuildPodWithPriority("ns1", "pod-b", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg-b", make(map[string]string), make(map[string]string), &highPri),
				util.BuildPodWithPriority("ns1", "pod-c", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg-c", make(map[string]string), make(map[string]string), &lowPri),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil),
			},
			Queues: []*vcschedulingv1.Queue{
				util.BuildQueueWithDequeueStrategy("q1", 1, nil, vcschedulingv1.DequeueStrategyFIFO),
			},
			PriClass: []*schedulingv1k8s.PriorityClass{
				util.BuildPriorityClass("high", 1000),
				util.BuildPriorityClass("low", 500),
			},
			ExpectBindMap:  map[string]string{},
			ExpectBindsNum: 0,
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
			checkDequeueBlockedJobStatus(t, ssn, i, test.Name)
		})
	}
}

func checkDequeueBlockedJobStatus(t *testing.T, ssn *framework.Session, caseIndex int, caseName string) {
	t.Helper()

	gangPlugin := gang.New(nil)
	gangPlugin.OnSessionClose(ssn)

	checkBlocked := func(jobID api.JobID, wantReason string) {
		t.Helper()
		job := ssn.Jobs[jobID]
		if job == nil {
			t.Fatalf("case %d(%s): job <%v> not found in session", caseIndex, caseName, jobID)
		}
		if job.UnschedulableReason != wantReason {
			t.Fatalf("case %d(%s): job <%v> UnschedulableReason want %q, got %q", caseIndex, caseName, jobID, wantReason, job.UnschedulableReason)
		}
		if job.JobFitErrors == "" {
			t.Fatalf("case %d(%s): job <%v> JobFitErrors should not be empty", caseIndex, caseName, jobID)
		}
		var found bool
		for _, cond := range job.PodGroup.Status.Conditions {
			if cond.Type == scheduling.PodGroupUnschedulableType && cond.Reason == wantReason {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("case %d(%s): job <%v> PodGroup condition Reason want %q", caseIndex, caseName, jobID, wantReason)
		}
	}

	switch caseIndex {
	case 0:
		checkBlocked(api.JobID("ns1/pg-c"), scheduling.PriorityFIFOBlockedReason)
	case 2:
		checkBlocked(api.JobID("ns1/pg-b"), scheduling.FIFOBlockedReason)
	case 3:
		checkBlocked(api.JobID("ns1/pg-b"), scheduling.FIFOBlockedReason)
		checkBlocked(api.JobID("ns1/pg-c"), scheduling.FIFOBlockedReason)
	}
}
