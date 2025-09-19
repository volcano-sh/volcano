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

package backfill

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	schedulingapi "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestPickUpPendingTasks(t *testing.T) {
	framework.RegisterPluginBuilder("priority", priority.New)
	framework.RegisterPluginBuilder("drf", drf.New)
	trueValue := true
	tilers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               "priority",
					EnabledPreemptable: &trueValue,
					EnabledTaskOrder:   &trueValue,
					EnabledJobOrder:    &trueValue,
				},
				{
					Name:              "drf",
					EnabledQueueOrder: &trueValue,
				},
			},
		},
	}

	priority4, priority3, priority2, priority1 := int32(4), int32(3), int32(2), int32(1)

	testCases := []struct {
		name            string
		pipelinedPods   []*v1.Pod
		pendingPods     []*v1.Pod
		queues          []*schedulingv1beta1.Queue
		podGroups       []*schedulingv1beta1.PodGroup
		PriorityClasses map[string]*schedulingapi.PriorityClass
		expectedResult  []string
	}{
		{
			name: "test",
			pendingPods: []*v1.Pod{
				util.MakePod().
					Namespace("default").
					Name("pg1-besteffort-task-1").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(nil).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority1).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg1-unbesteffort-task-1").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(v1.ResourceList{"cpu": resource.MustParse("500m")}).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority1).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg1-besteffort-task-3").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(nil).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority3).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg1-unbesteffort-task-3").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(v1.ResourceList{"cpu": resource.MustParse("500m")}).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority3).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg2-besteffort-task-1").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(nil).
					GroupName("pg2").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority1).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg2-unbesteffort-task-1").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(v1.ResourceList{"cpu": resource.MustParse("500m")}).
					GroupName("pg2").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority1).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg2-besteffort-task-3").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(nil).
					GroupName("pg2").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority3).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg2-unbesteffort-task-3").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(v1.ResourceList{"cpu": resource.MustParse("500m")}).
					GroupName("pg2").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority3).
					Obj(),
			},
			pipelinedPods: []*v1.Pod{
				util.MakePod().
					Namespace("default").
					Name("pg1-besteffort-task-2").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(nil).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority2).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg1-unbesteffort-task-2").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(v1.ResourceList{"cpu": resource.MustParse("500m")}).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority2).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg1-besteffort-task-4").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(nil).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority4).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg1-unbesteffort-task-4").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(v1.ResourceList{"cpu": resource.MustParse("500m")}).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority4).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg2-besteffort-task-2").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(nil).
					GroupName("pg2").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority2).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg2-unbesteffort-task-2").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(v1.ResourceList{"cpu": resource.MustParse("500m")}).
					GroupName("pg2").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority2).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg2-besteffort-task-4").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(nil).
					GroupName("pg2").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority4).
					Obj(),
				util.MakePod().
					Namespace("default").
					Name("pg2-unbesteffort-task-4").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(v1.ResourceList{"cpu": resource.MustParse("500m")}).
					GroupName("pg2").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Priority(&priority4).
					Obj(),
			},
			queues: []*schedulingv1beta1.Queue{
				util.MakeQueue().Name("q1").Weight(1).Capability(nil).State(schedulingv1beta1.QueueStateOpen).Obj(),
			},
			podGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup().
					Name("pg1").
					Namespace("default").
					Queue("q1").
					MinMember(1).
					MinTaskMember(map[string]int32{"": 3}).
					Phase(schedulingv1beta1.PodGroupInqueue).
					PriorityClassName("job-priority-1").
					Obj(),
				util.MakePodGroup().
					Name("pg2").
					Namespace("default").
					Queue("q1").
					MinMember(1).
					MinTaskMember(map[string]int32{"": 3}).
					Phase(schedulingv1beta1.PodGroupInqueue).
					PriorityClassName("job-priority-2").
					Obj(),
			},
			PriorityClasses: map[string]*schedulingapi.PriorityClass{
				"job-priority-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "job-priority-1",
					},
					Value: 1,
				},
				"job-priority-2": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "job-priority-2",
					},
					Value: 2,
				},
			},

			expectedResult: []string{
				"pg2-besteffort-task-4",
				"pg2-besteffort-task-3",
				"pg2-besteffort-task-2",
				"pg2-besteffort-task-1",
				"pg1-besteffort-task-4",
				"pg1-besteffort-task-3",
				"pg1-besteffort-task-2",
				"pg1-besteffort-task-1",
			},
		},
	}

	for _, tc := range testCases {
		schedulerCache := &cache.SchedulerCache{
			Nodes:           make(map[string]*api.NodeInfo),
			Jobs:            make(map[api.JobID]*api.JobInfo),
			Queues:          make(map[api.QueueID]*api.QueueInfo),
			Binder:          nil,
			StatusUpdater:   &util.FakeStatusUpdater{},
			Recorder:        record.NewFakeRecorder(100),
			PriorityClasses: tc.PriorityClasses,
			HyperNodesInfo:  api.NewHyperNodesInfo(nil),
		}

		for _, q := range tc.queues {
			schedulerCache.AddQueueV1beta1(q)
		}

		for _, ss := range tc.podGroups {
			schedulerCache.AddPodGroupV1beta1(ss)
		}

		for _, pod := range tc.pendingPods {
			schedulerCache.AddPod(pod)
		}

		for _, pod := range tc.pipelinedPods {
			schedulerCache.AddPod(pod)
		}

		ssn := framework.OpenSession(schedulerCache, tilers, []conf.Configuration{})
		for _, pod := range tc.pipelinedPods {
			jobID := api.NewTaskInfo(pod).Job
			stmt := framework.NewStatement(ssn)
			task, found := ssn.Jobs[jobID].Tasks[api.PodKey(pod)]
			if found {
				stmt.Pipeline(task, "node1", false)
			}
		}

		tasks := New().pickUpPendingTasks(ssn)
		var actualResult []string
		for _, task := range tasks {
			actualResult = append(actualResult, task.Name)
		}

		if !assert.Equal(t, tc.expectedResult, actualResult) {
			t.Errorf("unexpected test; name: %s, expected result: %v, actual result: %v", tc.name, tc.expectedResult, actualResult)
		}
	}
}
