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
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	var tmp *cache.SchedulerCache
	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	framework.RegisterPluginBuilder("conformance", conformance.New)
	framework.RegisterPluginBuilder("gang", gang.New)
	framework.RegisterPluginBuilder("proportion", proportion.New)
	defer framework.CleanupPluginBuilders()

	tests := []struct {
		name string
		cache.TestArg
		expected int
	}{
		{
			name: "Two Queue with one Queue overusing resource, should reclaim",
			TestArg: cache.TestArg{
				PodGroups: []*schedulingv1beta1.PodGroup{
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
				Pods: []*v1.Pod{
					util.BuildPreemptablePod("c1", "preemptee1", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, "pg1", util.PodResourceOption("1", "1G")),
					util.BuildPod("c1", "preemptor1", "", v1.PodPending, "pg2", util.PodResourceOption("1", "1G")),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", util.BuildResourceList("3", "3Gi"), make(map[string]string)),
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
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "q2",
						},
						Spec: schedulingv1beta1.QueueSpec{
							Weight: 1,
						},
					},
				},
			},
			expected: 1,
		},
	}

	reclaim := New()

	for i, test := range tests {
		schedulerCache, _, evictor, _ := cache.CreateCacheForTest(&test.TestArg, 100000, 10)

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
