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

package reclaim

import (
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestReclaim(t *testing.T) {
	framework.RegisterPluginBuilder("conformance", conformance.New)
	framework.RegisterPluginBuilder("gang", gang.New)
	framework.RegisterPluginBuilder("proportion", proportion.New)
	defer framework.CleanupPluginBuilders()

	tests := []struct {
		name      string
		podGroups []*schedulingv1beta1.PodGroup
		pods      []*v1.Pod
		nodes     []*v1.Node
		queues    []*schedulingv1beta1.Queue
		expected  int
	}{
		{
			name: "Two Queue with one Queue overusing resource, should reclaim",
			podGroups: []*schedulingv1beta1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "c1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
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
						Queue:             "q2",
						PriorityClassName: "high-priority",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupInqueue,
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("3", "3Gi"), make(map[string]string)),
			},
			queues: []*schedulingv1beta1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q2",
					},
					Spec: schedulingv1beta1.QueueSpec{
						Weight: 1,
					},
				},
			},
			expected: 1,
		},
	}

	reclaim := New()

	for i, test := range tests {
		binder := &util.FakeBinder{
			Binds:   map[string]string{},
			Channel: make(chan string),
		}
		evictor := &util.FakeEvictor{
			Channel: make(chan string),
		}
		schedulerCache := &cache.SchedulerCache{
			Nodes:           make(map[string]*api.NodeInfo),
			Jobs:            make(map[api.JobID]*api.JobInfo),
			Queues:          make(map[api.QueueID]*api.QueueInfo),
			Binder:          binder,
			Evictor:         evictor,
			StatusUpdater:   &util.FakeStatusUpdater{},
			VolumeBinder:    &util.FakeVolumeBinder{},
			PriorityClasses: make(map[string]*schedulingv1.PriorityClass),

			Recorder: record.NewFakeRecorder(100),
		}
		schedulerCache.PriorityClasses["high-priority"] = &schedulingv1.PriorityClass{
			Value: 100000,
		}
		schedulerCache.PriorityClasses["low-priority"] = &schedulingv1.PriorityClass{
			Value: 10,
		}
		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}
		for _, pod := range test.pods {
			schedulerCache.AddPod(pod)
		}

		for _, ss := range test.podGroups {
			schedulerCache.AddPodGroupV1beta1(ss)
		}

		for _, q := range test.queues {
			schedulerCache.AddQueueV1beta1(q)
		}

		trueValue := true
		ssn := framework.OpenSession(schedulerCache, []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:               "conformance",
						EnabledReclaimable: &trueValue,
					},
					{
						Name:               "gang",
						EnabledReclaimable: &trueValue,
					},
					{
						Name:               "proportion",
						EnabledReclaimable: &trueValue,
					},
				},
			},
		}, nil)
		defer framework.CloseSession(ssn)

		reclaim.Execute(ssn)

		for i := 0; i < test.expected; i++ {
			select {
			case <-evictor.Channel:
			case <-time.After(3 * time.Second):
				t.Errorf("Failed to get Evictor request.")
			}
		}

		if test.expected != len(evictor.Evicts()) {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, test.name, test.expected, len(evictor.Evicts()))
		}
	}
}
