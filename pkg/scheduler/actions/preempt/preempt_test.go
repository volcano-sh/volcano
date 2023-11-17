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
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestPreempt(t *testing.T) {
	var tmp *cache.SchedulerCache
	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	framework.RegisterPluginBuilder("conformance", conformance.New)
	framework.RegisterPluginBuilder("gang", gang.New)
	options.ServerOpts = &options.ServerOption{
		MinNodesToFind:             100,
		MinPercentageOfNodesToFind: 5,
		PercentageOfNodesToFind:    100,
	}
	defer framework.CleanupPluginBuilders()

	tests := []struct {
		name string
		cache.TestArg
		expected int
	}{
		{
			name: "do not preempt if there are enough idle resources",
			TestArg: cache.TestArg{
				PodGroups: []*schedulingv1beta1.PodGroup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg1",
							Namespace: "c1",
						},
						Spec: schedulingv1beta1.PodGroupSpec{
							MinMember: 3,
							MinTaskMember: map[string]int32{
								"": 3,
							},
							Queue: "q1",
						},
						Status: schedulingv1beta1.PodGroupStatus{
							Phase: schedulingv1beta1.PodGroupInqueue,
						},
					},
				},
				Pods: []*v1.Pod{
					util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptor1", "", v1.PodPending, "pg1", util.PodResourceOption("1", "1G")),
				},
				// If there are enough idle resources on the node, then there is no need to preempt anything.
				Nodes: []*v1.Node{
					util.BuildNode("n1", util.BuildResourceList("10", "10G"), make(map[string]string)),
				},
				Queues: []*schedulingv1beta1.Queue{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "q1",
						},
						Spec: schedulingv1beta1.QueueSpec{
							Weight: 1,
						},
					},
				},
			},
			expected: 0,
		},
		{
			name: "do not preempt if job is pipelined",
			TestArg: cache.TestArg{
				PodGroups: []*schedulingv1beta1.PodGroup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg1",
							Namespace: "c1",
						},
						Spec: schedulingv1beta1.PodGroupSpec{
							MinMember: 1,
							MinTaskMember: map[string]int32{
								"": 2,
							},
							Queue: "q1",
						},
						Status: schedulingv1beta1.PodGroupStatus{
							Phase: schedulingv1beta1.PodGroupInqueue,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg2",
							Namespace: "c1",
						},
						Spec: schedulingv1beta1.PodGroupSpec{
							MinMember: 1,
							MinTaskMember: map[string]int32{
								"": 2,
							},
							Queue: "q1",
						},
						Status: schedulingv1beta1.PodGroupStatus{
							Phase: schedulingv1beta1.PodGroupInqueue,
						},
					},
				},
				// Both pg1 and pg2 jobs are pipelined, because enough pods are already running.
				Pods: []*v1.Pod{
					util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, "pg2", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptor2", "", v1.PodPending, "pg2", util.PodResourceOption("1", "1G")),
				},
				// All resources on the node will be in use.
				Nodes: []*v1.Node{
					util.BuildNode("n1", util.BuildResourceList("3", "3G"), make(map[string]string)),
				},
				Queues: []*schedulingv1beta1.Queue{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "q1",
						},
						Spec: schedulingv1beta1.QueueSpec{
							Weight: 1,
						},
					},
				},
			},
			expected: 0,
		},
		{
			name: "preempt one task of different job to fit both jobs on one node",
			TestArg: cache.TestArg{
				PodGroups: []*schedulingv1beta1.PodGroup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg1",
							Namespace: "c1",
						},
						Spec: schedulingv1beta1.PodGroupSpec{
							MinMember: 1,
							MinTaskMember: map[string]int32{
								"": 2,
							},
							Queue:             "q1",
							PriorityClassName: "low-priority",
						},
						Status: schedulingv1beta1.PodGroupStatus{
							Phase: schedulingv1beta1.PodGroupInqueue,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg2",
							Namespace: "c1",
						},
						Spec: schedulingv1beta1.PodGroupSpec{
							MinMember: 1,
							MinTaskMember: map[string]int32{
								"": 2,
							},
							Queue:             "q1",
							PriorityClassName: "high-priority",
						},
						Status: schedulingv1beta1.PodGroupStatus{
							Phase: schedulingv1beta1.PodGroupInqueue,
						},
					},
				},
				Pods: []*v1.Pod{
					util.BuildPreemptablePod("c1", "preemptee1", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptor1", "", v1.PodPending, "pg2", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptor2", "", v1.PodPending, "pg2", util.PodResourceOption("1", "1G")),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", util.BuildResourceList("2", "2G"), make(map[string]string)),
				},
				Queues: []*schedulingv1beta1.Queue{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "q1",
						},
						Spec: schedulingv1beta1.QueueSpec{
							Weight: 1,
						},
					},
				},
			},
			expected: 1,
		},
		{
			name: "preempt enough tasks to fit large task of different job",
			TestArg: cache.TestArg{
				PodGroups: []*schedulingv1beta1.PodGroup{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg1",
							Namespace: "c1",
						},
						Spec: schedulingv1beta1.PodGroupSpec{
							MinMember: 1,
							MinTaskMember: map[string]int32{
								"": 3,
							},
							Queue:             "q1",
							PriorityClassName: "low-priority",
						},
						Status: schedulingv1beta1.PodGroupStatus{
							Phase: schedulingv1beta1.PodGroupInqueue,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "pg2",
							Namespace: "c1",
						},
						Spec: schedulingv1beta1.PodGroupSpec{
							MinMember: 1,
							MinTaskMember: map[string]int32{
								"": 1,
							},
							Queue:             "q1",
							PriorityClassName: "high-priority",
						},
						Status: schedulingv1beta1.PodGroupStatus{
							Phase: schedulingv1beta1.PodGroupInqueue,
						},
					},
				},
				// There are 3 cpus and 3G of memory idle and 3 tasks running each consuming 1 cpu and 1G of memory.
				// Big task requiring 5 cpus and 5G of memory should preempt 2 of 3 running tasks to fit into the node.
				Pods: []*v1.Pod{
					util.BuildPreemptablePod("c1", "preemptee1", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPreemptablePod("c1", "preemptee2", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPreemptablePod("c1", "preemptee3", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptor1", "", v1.PodPending, "pg2", util.PodResourceOption("5", "5G")),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", util.BuildResourceList("6", "6G"), make(map[string]string)),
				},
				Queues: []*schedulingv1beta1.Queue{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "q1",
						},
						Spec: schedulingv1beta1.QueueSpec{
							Weight: 1,
						},
					},
				},
			},
			expected: 2,
		},
	}

	preempt := New()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			schedulerCache, _, evictor, _ := cache.CreateCacheForTest(&test.TestArg, 100000, 10)

			trueValue := true
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:               "conformance",
							EnabledPreemptable: &trueValue,
						},
						{
							Name:                "gang",
							EnabledPreemptable:  &trueValue,
							EnabledJobPipelined: &trueValue,
							EnabledJobStarving:  &trueValue,
						},
					},
				},
			}, nil)
			defer framework.CloseSession(ssn)

			preempt.Execute(ssn)

			for i := 0; i < test.expected; i++ {
				select {
				case <-evictor.Channel:
				case <-time.After(time.Second):
					t.Errorf("not enough evictions")
				}
			}
			select {
			case key, opened := <-evictor.Channel:
				if opened {
					t.Errorf("unexpected eviction: %s", key)
				}
			case <-time.After(50 * time.Millisecond):
				// TODO: Active waiting here is not optimal, but there is no better way currently.
				//	 Ideally we would like to wait for evict and bind request goroutines to finish first.
			}
		})
	}
}
