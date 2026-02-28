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
	"k8s.io/klog/v2"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestBuildBackfillContext(t *testing.T) {
	framework.RegisterPluginBuilder("priority", priority.New)
	framework.RegisterPluginBuilder("drf", drf.New)
	defer framework.CleanupPluginBuilders()
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

		schedulerCache.AddOrUpdateNode(
			util.BuildNode("node1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
		)

		ssn := framework.OpenSession(schedulerCache, tilers, []conf.Configuration{})
		for _, pod := range tc.pipelinedPods {
			jobID := api.NewTaskInfo(pod).Job
			stmt := framework.NewStatement(ssn)
			taskUID := api.TaskID(pod.UID)
			task, found := ssn.Jobs[jobID].Tasks[taskUID]
			if found {
				err := stmt.Pipeline(task, "node1", false)
				if err != nil {
					klog.Errorf("Failed to pipeline task: %s", err.Error())
					stmt.Discard()
				} else {
					stmt.Commit()
				}
			}
		}

		action := New()
		action.session = ssn

		actx := action.buildBackfillContext()
		assert.NotNil(t, actx)

		var actualResult []string
		for !actx.queues.Empty() {
			queue := actx.queues.Pop().(*api.QueueInfo)

			jobs, found := actx.jobsByQueue[queue.UID]
			if !found || jobs.Empty() {
				continue
			}
			job := jobs.Pop().(*api.JobInfo)

			tasks, found := actx.tasksByJob[job.UID]
			if !found || tasks.Empty() {
				actx.queues.Push(queue)
				continue
			}
			for !tasks.Empty() {
				task := tasks.Pop().(*api.TaskInfo)
				actualResult = append(actualResult, task.Name)
			}
			actx.queues.Push(queue)
		}
		framework.CloseSession(ssn)

		if !assert.Equal(t, tc.expectedResult, actualResult) {
			t.Errorf("unexpected test; name: %s, expected result: %v, actual result: %v", tc.name, tc.expectedResult, actualResult)
		}
	}
}

func TestBackfillReallocateBestEffortTask(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		gang.PluginName: gang.New,
	}

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

	type testCase struct {
		uthelper.TestCommonStruct
		podsToPipeline []string
	}

	tests := []testCase{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "backfill allocates pending best-effort tasks and skips non-best-effort tasks",
				PodGroups: []*schedulingv1beta1.PodGroup{
					util.BuildPodGroup("pg1", "default", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					util.BuildPod("default", "besteffort-task-1", "", v1.PodPending, api.BuildResourceList("0", "0"), "pg1", make(map[string]string), make(map[string]string)),
					util.BuildPod("default", "normal-task-1", "", v1.PodPending, api.BuildResourceList("500m", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("node1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*schedulingv1beta1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
				ExpectBindsNum: 1,
				ExpectBindMap: map[string]string{
					"default/besteffort-task-1": "node1",
				},
			},
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "backfill reallocates pipelined best-effort tasks",
				PodGroups: []*schedulingv1beta1.PodGroup{
					util.BuildPodGroup("pg1", "default", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					util.BuildPod("default", "besteffort-task-1", "", v1.PodPending, api.BuildResourceList("0", "0"), "pg1", make(map[string]string), make(map[string]string)),
					util.BuildPod("default", "besteffort-task-2", "", v1.PodPending, api.BuildResourceList("0", "0"), "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("node1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*schedulingv1beta1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
				ExpectBindsNum: 2,
				ExpectBindMap: map[string]string{
					"default/besteffort-task-1": "node1",
					"default/besteffort-task-2": "node1",
				},
			},
			podsToPipeline: []string{"besteffort-task-1"},
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "backfill handles multiple jobs across same queue",
				PodGroups: []*schedulingv1beta1.PodGroup{
					util.BuildPodGroup("pg1", "default", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue),
					util.BuildPodGroup("pg2", "default", "q1", 0, nil, schedulingv1beta1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					util.BuildPod("default", "p1-besteffort", "", v1.PodPending, api.BuildResourceList("0", "0"), "pg1", make(map[string]string), make(map[string]string)),
					util.BuildPod("default", "p2-besteffort", "", v1.PodPending, api.BuildResourceList("0", "0"), "pg2", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("node1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*schedulingv1beta1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
				ExpectBindsNum: 2,
				ExpectBindMap: map[string]string{
					"default/p1-besteffort": "node1",
					"default/p2-besteffort": "node1",
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()

			for _, podName := range test.podsToPipeline {
				for _, job := range ssn.Jobs {
					for _, task := range job.Tasks {
						if task.Name == podName {
							stmt := framework.NewStatement(ssn)
							if err := stmt.Pipeline(task, "node1", false); err != nil {
								t.Fatalf("Failed to pipeline task %s: %v", podName, err)
							}
							stmt.Commit()
						}
					}
				}
			}

			action := New()
			test.Run([]framework.Action{action})

			if err := test.CheckAll(i); err != nil {
				t.Fatalf("Test %s failed: %v", test.Name, err)
			}
		})
	}
}
