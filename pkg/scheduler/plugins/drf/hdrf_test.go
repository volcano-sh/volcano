package drf

import (
	"flag"
	"fmt"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	schedulingv1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func makePods(num int, cpu, mem, podGroupName string) []*v1.Pod {
	pods := []*v1.Pod{}
	for i := 0; i < num; i++ {
		pods = append(pods, util.BuildPod("default", fmt.Sprintf("%s-p%d", podGroupName, i), "", v1.PodPending, util.BuildResourceList(cpu, mem), podGroupName, make(map[string]string), make(map[string]string)))
	}
	return pods
}

type pgSpec struct {
	taskNum   int
	cpu       string
	mem       string
	pg        string
	hierarchy string
	weight    string
}

func TestHDRF(t *testing.T) {
	klog.InitFlags(nil)
	flag.Set("v", "4")
	flag.Set("alsologtostderr", "true")
	s := options.NewServerOption()
	s.MinNodesToFind = 100
	s.PercentageOfNodesToFind = 100
	s.RegisterOptions()

	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	tests := []struct {
		name     string
		pgSpecs  []pgSpec
		nodes    []*v1.Node
		queues   []*schedulingv1.Queue
		expected map[string]string
	}{
		{
			name: "rescaling test",
			pgSpecs: []pgSpec{
				{
					taskNum:   10,
					cpu:       "1",
					mem:       "1G",
					pg:        "pg1",
					hierarchy: "root/sci",
					weight:    "100/50",
				},
				{
					taskNum:   10,
					cpu:       "1",
					mem:       "0G",
					pg:        "pg21",
					hierarchy: "root/eng/dev",
					weight:    "100/50/50",
				},
				{
					taskNum:   10,
					cpu:       "0",
					mem:       "1G",
					pg:        "pg22",
					hierarchy: "root/eng/prod",
					weight:    "100/50/50",
				},
			},
			nodes: []*v1.Node{util.BuildNode("n",
				util.BuildResourceList("10", "10G"),
				make(map[string]string))},
			queues: []*schedulingv1.Queue{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: schedulingv1.QueueSpec{
					Weight: 1,
				},
			}},
			expected: map[string]string{
				"pg1":  "cpu 5000.00, memory 5000000000.00, nvidia.com/gpu 0.00",
				"pg21": "cpu 5000.00, memory 0.00, nvidia.com/gpu 0.00",
				"pg22": "cpu 0.00, memory 5000000000.00, nvidia.com/gpu 0.00",
			},
		},
		{
			name: "blocking nodes test",
			pgSpecs: []pgSpec{
				{
					taskNum:   30,
					cpu:       "1",
					mem:       "0G",
					pg:        "pg1",
					hierarchy: "root/pg1",
					weight:    "100/25",
				},
				{
					taskNum:   30,
					cpu:       "1",
					mem:       "0G",
					pg:        "pg2",
					hierarchy: "root/pg2",
					weight:    "100/25",
				},
				{
					taskNum:   30,
					cpu:       "1",
					mem:       "0G",
					pg:        "pg31",
					hierarchy: "root/pg3/pg31",
					weight:    "100/25/50",
				},
				{
					taskNum:   30,
					cpu:       "0",
					mem:       "1G",
					pg:        "pg32",
					hierarchy: "root/pg3/pg32",
					weight:    "100/25/50",
				},
				{
					taskNum:   30,
					cpu:       "0",
					mem:       "1G",
					pg:        "pg4",
					hierarchy: "root/pg4",
					weight:    "100/25",
				},
			},
			nodes: []*v1.Node{util.BuildNode("n",
				util.BuildResourceList("30", "30G"),
				make(map[string]string))},
			queues: []*schedulingv1.Queue{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "default",
				},
				Spec: schedulingv1.QueueSpec{
					Weight: 1,
				},
			}},
			expected: map[string]string{
				"pg1":  "cpu 10000.00, memory 0.00, nvidia.com/gpu 0.00",
				"pg2":  "cpu 10000.00, memory 0.00, nvidia.com/gpu 0.00",
				"pg31": "cpu 10000.00, memory 0.00, nvidia.com/gpu 0.00",
				"pg32": "cpu 0.00, memory 15000000000.00, nvidia.com/gpu 0.00",
				"pg4":  "cpu 0.00, memory 15000000000.00, nvidia.com/gpu 0.00",
			},
		},
	}
	for _, test := range tests {
		binder := &util.FakeBinder{
			Binds:   map[string]string{},
			Channel: make(chan string),
		}
		schedulerCache := &cache.SchedulerCache{
			Nodes:         make(map[string]*api.NodeInfo),
			Jobs:          make(map[api.JobID]*api.JobInfo),
			Queues:        make(map[api.QueueID]*api.QueueInfo),
			Binder:        binder,
			StatusUpdater: &util.FakeStatusUpdater{},
			VolumeBinder:  &util.FakeVolumeBinder{},

			Recorder: record.NewFakeRecorder(100),
		}
		for _, node := range test.nodes {
			schedulerCache.AddNode(node)
		}
		for _, q := range test.queues {
			schedulerCache.AddQueueV1beta1(q)
		}
		for _, pgSpec := range test.pgSpecs {
			pods := makePods(pgSpec.taskNum, pgSpec.cpu, pgSpec.mem, pgSpec.pg)
			for _, pod := range pods {
				schedulerCache.AddPod(pod)
			}
			schedulerCache.AddPodGroupV1beta1(&schedulingv1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pgSpec.pg,
					Namespace: "default",
					Annotations: map[string]string{
						schedulingv1.KubeGroupHierarchyAnnotationKey:       pgSpec.hierarchy,
						schedulingv1.KubeGroupHierarchyWeightAnnotationKey: pgSpec.weight,
					},
				},
				Spec: schedulingv1.PodGroupSpec{
					Queue: "default",
				},
			})
		}
		trueValue := true
		ssn := framework.OpenSession(schedulerCache, []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             PluginName,
						EnabledHierarchy: &trueValue,
						EnabledJobOrder:  &trueValue,
					},
				},
			},
		}, nil)
		defer framework.CloseSession(ssn)

		allocateAction := allocate.New()

		allocateAction.Execute(ssn)

		for _, job := range ssn.Jobs {
			if test.expected[job.Name] != job.Allocated.String() {
				t.Fatalf("job %s expected resource %s, but got %s", job.Name, test.expected[job.Name], job.Allocated)
			}
		}

	}
}
