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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	kbv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/conf"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/framework"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/plugins/conformance"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/plugins/gang"
	"github.com/kubernetes-sigs/kube-batch/pkg/scheduler/util"
)

func TestReclaim(t *testing.T) {
	framework.RegisterPluginBuilder("conformance", conformance.New)
	framework.RegisterPluginBuilder("gang", gang.New)
	defer framework.CleanupPluginBuilders()

	tests := []struct {
		name      string
		podGroups []*kbv1.PodGroup
		pods      []*v1.Pod
		nodes     []*v1.Node
		queues    []*kbv1.Queue
		expected  int
	}{
		{
			name: "Two Queue with one Queue overusing resource, should reclaim",
			podGroups: []*kbv1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "c1",
					},
					Spec: kbv1.PodGroupSpec{
						Queue: "q1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg2",
						Namespace: "c1",
					},
					Spec: kbv1.PodGroupSpec{
						Queue: "q2",
					},
				},
			},
			pods: []*v1.Pod{
				util.BuildPod("c1", "preemptee1", "n1", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee2", "n1", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptee3", "n1", v1.PodRunning, util.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "preemptor1", "", v1.PodPending, util.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			nodes: []*v1.Node{
				util.BuildNode("n1", util.BuildResourceList("3", "3Gi"), make(map[string]string)),
			},
			queues: []*kbv1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q1",
					},
					Spec: kbv1.QueueSpec{
						Weight: 1,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "q2",
					},
					Spec: kbv1.QueueSpec{
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
			Evicts:  make([]string, 0),
			Channel: make(chan string),
		}
		schedulerCache := &cache.SchedulerCache{
			Nodes:         make(map[string]*api.NodeInfo),
			Jobs:          make(map[api.JobID]*api.JobInfo),
			Queues:        make(map[api.QueueID]*api.QueueInfo),
			Binder:        binder,
			Evictor:       evictor,
			StatusUpdater: &util.FakeStatusUpdater{},
			VolumeBinder:  &util.FakeVolumeBinder{},

			Recorder: record.NewFakeRecorder(100),
		}
		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}
		for _, pod := range test.pods {
			schedulerCache.AddPod(pod)
		}

		for _, ss := range test.podGroups {
			schedulerCache.AddPodGroup(ss)
		}

		for _, q := range test.queues {
			schedulerCache.AddQueue(q)
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
				},
			},
		})
		defer framework.CloseSession(ssn)

		reclaim.Execute(ssn)

		for i := 0; i < test.expected; i++ {
			select {
			case <-evictor.Channel:
			case <-time.After(3 * time.Second):
				t.Errorf("Failed to get Evictor request.")
			}
		}

		if test.expected != len(evictor.Evicts) {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, test.name, test.expected, len(evictor.Evicts))
		}
	}
}
