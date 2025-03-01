package enqueue

import (
	"os"
	"testing"

	v1 "k8s.io/api/core/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/capacity"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/overcommit"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/plugins/resourcequota"
	"volcano.sh/volcano/pkg/scheduler/plugins/sla"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestMain(m *testing.M) {
	options.Default()
	os.Exit(m.Run())
}

func TestEnqueue(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		drf.PluginName:        drf.New,
		gang.PluginName:       gang.New,
		sla.PluginName:        sla.New,
		proportion.PluginName: proportion.New,
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "when podgroup status is inqueue",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, api.BuildResourceList("4", "4G")),
				util.BuildQueue("c2", 1, api.BuildResourceList("4", "4G")),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupInqueue,
			},
		},
		{
			Name: "when podgroup status is pending",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupPending),
				util.BuildPodGroup("pg2", "c1", "c2", 0, nil, schedulingv1.PodGroupPending),
			},
			Pods: []*v1.Pod{
				// pending pod with owner1, under ns:c1/q:c1
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("3", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				// pending pod with owner2, under ns:c1/q:c2
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg2", make(map[string]string), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, api.BuildResourceList("4", "4G")),
				util.BuildQueue("c2", 1, api.BuildResourceList("4", "4G")),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupInqueue,
				"c1/pg2": scheduling.PodGroupInqueue,
			},
		},
		{
			Name: "when podgroup status is running",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupRunning),
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				util.BuildPod("c1", "p2", "", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, api.BuildResourceList("4", "4G")),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
				"c1/pg1": scheduling.PodGroupRunning,
			},
		},
		{
			Name: "pggroup cannot enqueue because the specified queue is c1, but there is only c2",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupPending),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c2", 1, api.BuildResourceList("4", "4G")),
			},
			ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{},
		},
		{
			Name: "pggroup cannot enqueue because queue resources are less than podgroup MinResources",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroupWithMinResources("pg1", "c1", "c1", 0,
					nil, api.BuildResourceList("8", "8G"), schedulingv1.PodGroupPending),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, api.BuildResourceList("1", "1G")),
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

func TestEnqueueReason(t *testing.T) {

	minRes := api.BuildResourceList("4", "4G")
	midRes := api.BuildResourceList("6", "6G")
	maxRes := api.BuildResourceList("8", "8G")

	pg1 := util.BuildPodGroupWithMinResources("pg1", "ns1", "q1", 2, nil, maxRes, schedulingv1.PodGroupPending)

	pod11 := util.BuildPod("ns1", "p1", "", v1.PodPending, minRes, "pg1", make(map[string]string), make(map[string]string))
	pod12 := util.BuildPod("ns1", "p2", "", v1.PodPending, minRes, "pg1", make(map[string]string), make(map[string]string))
	queue1 := util.BuildQueueWithResourcesQuantity("q1", minRes, midRes)
	queue2 := util.BuildQueueWithResourcesQuantity("q2", minRes, maxRes)

	n1 := util.BuildNode("node1", minRes, nil)
	n2 := util.BuildNode("node2", midRes, nil)

	tests := []struct {
		fitErrors map[string]string
		uthelper.TestCommonStruct
	}{
		{
			fitErrors: map[string]string{"ns1/pg1": "queue resource quota insufficient"},
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "capacity plugin: capability is not enough to enqueue job",
				Plugins: map[string]framework.PluginBuilder{
					gang.PluginName:     gang.New,
					capacity.PluginName: capacity.New,
				},
				PodGroups:    []*schedulingv1.PodGroup{pg1},
				Pods:         []*v1.Pod{pod11, pod12},
				Queues:       []*schedulingv1.Queue{queue1, queue2},
				Nodes:        []*v1.Node{n1, n2},
				ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{"ns1/pg1": scheduling.PodGroupPending},
			},
		},
		{
			fitErrors: map[string]string{"ns1/pg1": "resource in cluster is overused"},
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "overcommit plugin: resource in cluster is overused",
				Plugins: map[string]framework.PluginBuilder{
					gang.PluginName:       gang.New,
					overcommit.PluginName: overcommit.New,
				},
				PodGroups:    []*schedulingv1.PodGroup{pg1},
				Pods:         []*v1.Pod{pod11, pod12},
				Queues:       []*schedulingv1.Queue{queue1, queue2},
				Nodes:        []*v1.Node{n2},
				ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{"ns1/pg1": scheduling.PodGroupPending},
			},
		},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:            gang.PluginName,
					EnabledJobOrder: &trueValue,
				},
				{
					Name:               capacity.PluginName,
					EnabledJobEnqueued: &trueValue,
				},
				{
					Name:               overcommit.PluginName,
					EnabledJobEnqueued: &trueValue,
				},
				{
					Name:               resourcequota.PluginName,
					EnabledJobEnqueued: &trueValue,
				},
			},
		},
	}
	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			ssn := test.RegisterSession(tiers, []conf.Configuration{})
			defer test.Close()
			test.Run([]framework.Action{New(), allocate.New()})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}

			for jobID, str := range test.fitErrors {
				job := ssn.Jobs[api.JobID(jobID)]
				if job == nil {
					t.Fatalf("%s doesn't exist in session", jobID)
				}
				if str != job.JobFitErrors {
					t.Fatalf("JobFitErrors of job %s:\nwant %s\n got %s", jobID, str, job.JobFitErrors)
				}
			}
		})
	}
}
