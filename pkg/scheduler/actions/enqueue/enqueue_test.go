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

package enqueue

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/plugins/sla"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestEnqueue(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		drf.PluginName:        drf.New,
		gang.PluginName:       gang.New,
		sla.PluginName:        sla.New,
		proportion.PluginName: proportion.New,
	}
	options.Default()
	tests := []uthelper.TestCommonStruct{
		{
			Name: "when podgroup status is inqueue",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup().
					Name("pg1").
					Namespace("c1").
					Queue("c1").
					MinMember(2).
					MinTaskMember(nil).
					Phase(schedulingv1.PodGroupInqueue).
					Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod().
					Namespace("c1").
					Name("p1").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("1", "1G")).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Obj(),
				util.MakePod().
					Namespace("c1").
					Name("p2").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("1", "1G")).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue().Name("c1").Weight(1).Capability(api.BuildResourceList("4", "4G")).Obj(),
				util.MakeQueue().Name("c2").Weight(1).Capability(api.BuildResourceList("4", "4G")).Obj(),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupInqueue,
			},
		},
		{
			Name: "when podgroup status is pending",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup().
					Name("pg1").
					Namespace("c1").
					Queue("c1").
					MinMember(1).
					MinTaskMember(nil).
					Phase(schedulingv1.PodGroupPending).
					Obj(),
				util.MakePodGroup().
					Name("pg2").
					Namespace("c1").
					Queue("c2").
					MinMember(1).
					MinTaskMember(nil).
					Phase(schedulingv1.PodGroupPending).
					Obj(),
			},
			Pods: []*v1.Pod{
				// pending pod with owner1, under ns:c1/q:c1
				util.MakePod().
					Namespace("c1").
					Name("p1").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("3", "1G")).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Obj(),
				// pending pod with owner2, under ns:c1/q:c2
				util.MakePod().
					Namespace("c1").
					Name("p2").
					NodeName("").
					PodPhase(v1.PodPending).
					ResourceList(api.BuildResourceList("1", "1G")).
					GroupName("pg2").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue().Name("c1").Weight(1).Capability(api.BuildResourceList("4", "4G")).Obj(),
				util.MakeQueue().Name("c2").Weight(1).Capability(api.BuildResourceList("4", "4G")).Obj(),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupInqueue,
				"c1/pg2": scheduling.PodGroupInqueue,
			},
		},
		{
			Name: "when podgroup status is running",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup().
					Name("pg1").
					Namespace("c1").
					Queue("c1").
					MinMember(2).
					MinTaskMember(nil).
					Phase(schedulingv1.PodGroupRunning).
					Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod().
					Namespace("c1").
					Name("p1").
					NodeName("").
					PodPhase(v1.PodRunning).
					ResourceList(api.BuildResourceList("1", "1G")).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Obj(),
				util.MakePod().
					Namespace("c1").
					Name("p2").
					NodeName("").
					PodPhase(v1.PodRunning).
					ResourceList(api.BuildResourceList("1", "1G")).
					GroupName("pg1").
					Labels(make(map[string]string)).
					NodeSelector(make(map[string]string)).
					Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue().Name("c1").Weight(1).Capability(api.BuildResourceList("4", "4G")).Obj(),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupRunning,
			},
		},
		{
			Name: "pggroup cannot enqueue because the specified queue is c1, but there is only c2",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup().
					Name("pg1").
					Namespace("c1").
					Queue("c1").
					MinMember(0).
					MinTaskMember(nil).
					Phase(schedulingv1.PodGroupPending).
					Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue().Name("c2").Weight(1).Capability(api.BuildResourceList("4", "4G")).Obj(),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{},
		},
		{
			Name: "pggroup cannot enqueue because queue resources are less than podgroup MinResources",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup().
					Name("pg1").
					Namespace("c1").
					Queue("c1").
					MinMember(1).
					MinTaskMember(nil).
					MinResources(api.BuildResourceList("8", "8G")).
					Phase(schedulingv1.PodGroupPending).
					Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue().Name("c1").Weight(1).Capability(api.BuildResourceList("1", "1G")).Obj(),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupPending,
			},
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               drf.PluginName,
					EnabledJobOrder:    &trueValue,
					EnabledJobEnqueued: &trueValue,
				},
				{
					Name:               proportion.PluginName,
					EnabledQueueOrder:  &trueValue,
					EnabledJobEnqueued: &trueValue,
				},
				{
					Name: sla.PluginName,
					Arguments: map[string]interface{}{
						"sla-waiting-time": "3m",
					},
					EnabledJobOrder:    &trueValue,
					EnabledJobEnqueued: &trueValue,
				},
				{
					Name:            gang.PluginName,
					EnabledJobOrder: &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()
			action := New()
			test.Run([]framework.Action{action})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
