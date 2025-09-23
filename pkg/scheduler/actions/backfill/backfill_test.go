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
				util.BuildPodWithPriority("default", "pg1-besteffort-task-1", "", v1.PodPending, nil, "pg1", make(map[string]string), make(map[string]string), &priority1),
				util.BuildPodWithPriority("default", "pg1-unbesteffort-task-1", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg1", make(map[string]string), make(map[string]string), &priority1),
				util.BuildPodWithPriority("default", "pg1-besteffort-task-3", "", v1.PodPending, nil, "pg1", make(map[string]string), make(map[string]string), &priority3),
				util.BuildPodWithPriority("default", "pg1-unbesteffort-task-3", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg1", make(map[string]string), make(map[string]string), &priority3),

				util.BuildPodWithPriority("default", "pg2-besteffort-task-1", "", v1.PodPending, nil, "pg2", make(map[string]string), make(map[string]string), &priority1),
				util.BuildPodWithPriority("default", "pg2-unbesteffort-task-1", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg2", make(map[string]string), make(map[string]string), &priority1),
				util.BuildPodWithPriority("default", "pg2-besteffort-task-3", "", v1.PodPending, nil, "pg2", make(map[string]string), make(map[string]string), &priority3),
				util.BuildPodWithPriority("default", "pg2-unbesteffort-task-3", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg2", make(map[string]string), make(map[string]string), &priority3),
			},
			pipelinedPods: []*v1.Pod{
				util.BuildPodWithPriority("default", "pg1-besteffort-task-2", "", v1.PodPending, nil, "pg1", make(map[string]string), make(map[string]string), &priority2),
				util.BuildPodWithPriority("default", "pg1-unbesteffort-task-2", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg1", make(map[string]string), make(map[string]string), &priority2),
				util.BuildPodWithPriority("default", "pg1-besteffort-task-4", "", v1.PodPending, nil, "pg1", make(map[string]string), make(map[string]string), &priority4),
				util.BuildPodWithPriority("default", "pg1-unbesteffort-task-4", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg1", make(map[string]string), make(map[string]string), &priority4),

				util.BuildPodWithPriority("default", "pg2-besteffort-task-2", "", v1.PodPending, nil, "pg2", make(map[string]string), make(map[string]string), &priority2),
				util.BuildPodWithPriority("default", "pg2-unbesteffort-task-2", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg2", make(map[string]string), make(map[string]string), &priority2),
				util.BuildPodWithPriority("default", "pg2-besteffort-task-4", "", v1.PodPending, nil, "pg2", make(map[string]string), make(map[string]string), &priority4),
				util.BuildPodWithPriority("default", "pg2-unbesteffort-task-4", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg2", make(map[string]string), make(map[string]string), &priority4),
			},
			queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			podGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "default", "q1", 1, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue, "job-priority-1"),
				util.BuildPodGroupWithPrio("pg2", "default", "q1", 1, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue, "job-priority-2"),
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
