/*
Copyright 2025 The Volcano Authors.

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
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestDequeueStrategies(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		gang.PluginName: gang.New,
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobPipelined: &trueValue,
					EnabledJobReady:     &trueValue,
				},
			},
		},
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "FIFO strategy should not schedule second job when first job cannot allocate",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithTime("pg1", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue, metav1.Time{Time: time.Now()}),
				util.BuildPodGroupWithTime("pg2", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue, metav1.Time{Time: time.Now().Add(time.Second)}),
			},
			Pods: []*v1.Pod{
				// First job requires more resources than available, blocks FIFO queue
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg1", make(map[string]string), make(map[string]string)),
				// Second job could fit but should not be scheduled due to FIFO blocking
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueueWithStrategy("q1", 1, schedulingv1.DequeueStrategyFIFO, nil),
			},
			ExpectBindMap:  map[string]string{},
			ExpectBindsNum: 0,
		},

		{
			Name: "FIFO strategy should schedule jobs in order when resources are sufficient",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithTime("pg1", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue, metav1.Time{Time: time.Now()}),
				util.BuildPodGroupWithTime("pg2", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue, metav1.Time{Time: time.Now().Add(time.Second)}),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("4", "8G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueueWithStrategy("q1", 1, schedulingv1.DequeueStrategyFIFO, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p1": "n1",
				"c1/p2": "n1",
			},
			ExpectBindsNum: 2,
		},
		{
			Name: "Traverse strategy should skip first job and schedule second when first fails",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithTime("pg1", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue, metav1.Time{Time: time.Now()}),
				util.BuildPodGroupWithTime("pg2", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue, metav1.Time{Time: time.Now().Add(time.Second)}),
			},
			Pods: []*v1.Pod{
				// First job requires more resources than available
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg1", make(map[string]string), make(map[string]string)),
				// Second job can fit and should be scheduled
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueueWithStrategy("q1", 1, schedulingv1.DequeueStrategyTraverse, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p2": "n1",
			},
			ExpectBindsNum: 1,
		},
		{
			Name: "Default strategy should behave like traverse",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithTime("pg1", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue, metav1.Time{Time: time.Now()}),
				util.BuildPodGroupWithTime("pg2", "c1", "q1", 1, nil, schedulingv1.PodGroupInqueue, metav1.Time{Time: time.Now().Add(time.Second)}),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p2": "n1",
			},
			ExpectBindsNum: 1,
		},
		{
			Name: "Multiple queues with different strategies",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithTime("pg1", "c1", "fifo-q", 1, nil, schedulingv1.PodGroupInqueue, metav1.Time{Time: time.Now()}),
				util.BuildPodGroupWithTime("pg2", "c1", "fifo-q", 1, nil, schedulingv1.PodGroupInqueue, metav1.Time{Time: time.Now().Add(time.Second)}),
				util.BuildPodGroup("pg3", "c1", "traverse-q", 1, nil, schedulingv1.PodGroupInqueue),
				util.BuildPodGroup("pg4", "c1", "traverse-q", 1, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				// FIFO queue: first job too large, second small (but blocked by FIFO)
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
				// Traverse queue: first job too large, second small (can be scheduled)
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg3", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg4", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueueWithStrategy("fifo-q", 1, schedulingv1.DequeueStrategyFIFO, nil),
				util.BuildQueueWithStrategy("traverse-q", 1, schedulingv1.DequeueStrategyTraverse, nil),
			},
			ExpectBindMap: map[string]string{
				"c1/p4": "n1",
			},
			ExpectBindsNum: 1,
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
