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
				util.MakePodGroup("pg1", "c1").Queue("c1").MinMember(2).Phase(schedulingv1.PodGroupInqueue).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "p1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg1").Phase(v1.PodPending).Obj(),
				util.MakePod("c1", "p2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg1").Phase(v1.PodPending).Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue("c1").Weight(1).Capability(api.BuildResourceList("4", "4Gi")).Obj(),
				util.MakeQueue("c2").Weight(1).Capability(api.BuildResourceList("4", "4Gi")).Obj(),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupInqueue,
			},
		},
		{
			Name: "when podgroup status is pending",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("c1").MinMember(1).Phase(schedulingv1.PodGroupPending).Obj(),
				util.MakePodGroup("pg2", "c1").Queue("c2").MinMember(1).Phase(schedulingv1.PodGroupPending).Obj(),
			},
			Pods: []*v1.Pod{
				// pending pod with owner1, under ns:c1/q:c1
				util.MakePod("c1", "p1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("3", "1G")).Obj()},
				).GroupName("pg1").Phase(v1.PodPending).Obj(),
				// pending pod with owner2, under ns:c1/q:c2
				util.MakePod("c1", "p2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg2").Phase(v1.PodPending).Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue("c1").Weight(1).Capability(api.BuildResourceList("4", "4Gi")).Obj(),
				util.MakeQueue("c2").Weight(1).Capability(api.BuildResourceList("4", "4Gi")).Obj(),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupInqueue,
				"c1/pg2": scheduling.PodGroupInqueue,
			},
		},
		{
			Name: "when podgroup status is running",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("c1").MinMember(2).Phase(schedulingv1.PodGroupRunning).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("c1", "p1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg1").Phase(v1.PodRunning).Obj(),
				util.MakePod("c1", "p2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "1G")).Obj()},
				).GroupName("pg1").Phase(v1.PodRunning).Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue("c1").Weight(1).Capability(api.BuildResourceList("4", "4Gi")).Obj(),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupRunning,
			},
		},
		{
			Name: "pggroup cannot enqueue because the specified queue is c1, but there is only c2",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("c1").MinMember(0).Phase(schedulingv1.PodGroupPending).Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue("c2").Weight(1).Capability(api.BuildResourceList("4", "4Gi")).Obj(),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{},
		},
		{
			Name: "pggroup cannot enqueue because queue resources are less than podgroup MinResources",
			PodGroups: []*schedulingv1.PodGroup{
				util.MakePodGroup("pg1", "c1").Queue("c1").MinMember(1).
					MinResources(api.BuildResourceList("8", "8G")).Phase(schedulingv1.PodGroupPending).Obj(),
			},
			Queues: []*schedulingv1.Queue{
				util.MakeQueue("c1").Weight(1).Capability(api.BuildResourceList("1", "1Gi")).Obj(),
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
