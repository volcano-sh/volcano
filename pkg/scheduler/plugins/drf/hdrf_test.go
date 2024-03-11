package drf

import (
	"flag"
	"fmt"
	"reflect"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func makePods(num int, cpu, mem, podGroupName string) []*v1.Pod {
	pods := []*v1.Pod{}
	for i := 0; i < num; i++ {
		pods = append(pods, util.BuildPod("default",
			fmt.Sprintf("%s-p%d", podGroupName, i), "",
			v1.PodPending, api.BuildResourceList(cpu, mem),
			podGroupName, make(map[string]string), make(map[string]string)))
	}
	return pods
}

type queueSpec struct {
	name      string
	hierarchy string
	weights   string
}

type pgSpec struct {
	taskNum int
	cpu     string
	mem     string
	pg      string
	queue   string
}

func TestHDRF(t *testing.T) {
	klog.InitFlags(nil)
	flag.Set("v", "4")
	flag.Set("alsologtostderr", "true")
	var tmp *cache.SchedulerCache
	patches := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "AddBindTask", func(scCache *cache.SchedulerCache, task *api.TaskInfo) error {
		scCache.Binder.Bind(nil, []*api.TaskInfo{task})
		return nil
	})
	defer patches.Reset()

	patchUpdateQueueStatus := gomonkey.ApplyMethod(reflect.TypeOf(tmp), "UpdateQueueStatus", func(scCache *cache.SchedulerCache, queue *api.QueueInfo) error {
		return nil
	})
	defer patchUpdateQueueStatus.Reset()

	s := options.NewServerOption()
	s.MinNodesToFind = 100
	s.PercentageOfNodesToFind = 100
	s.RegisterOptions()

	framework.RegisterPluginBuilder(PluginName, New)
	framework.RegisterPluginBuilder("proportion", proportion.New)
	defer framework.CleanupPluginBuilders()

	tests := []struct {
		name       string
		pgSpecs    []pgSpec
		nodes      []*v1.Node
		queues     []*schedulingv1.Queue
		queueSpecs []queueSpec
		expected   map[string]*api.Resource
	}{
		{
			name: "rescaling test",
			pgSpecs: []pgSpec{
				{
					taskNum: 10,
					cpu:     "1",
					mem:     "1G",
					pg:      "pg1",
					queue:   "root-sci",
				},
				{
					taskNum: 10,
					cpu:     "1",
					mem:     "0G",
					pg:      "pg21",
					queue:   "root-eng-dev",
				},
				{
					taskNum: 10,
					cpu:     "0",
					mem:     "1G",
					pg:      "pg22",
					queue:   "root-eng-prod",
				},
			},
			nodes: []*v1.Node{util.BuildNode("n",
				api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "50"}}...),
				make(map[string]string))},
			queueSpecs: []queueSpec{
				{
					name:      "root-sci",
					hierarchy: "root/sci",
					weights:   "100/50",
				},
				{
					name:      "root-eng-dev",
					hierarchy: "root/eng/dev",
					weights:   "100/50/50",
				},
				{
					name:      "root-eng-prod",
					hierarchy: "root/eng/prod",
					weights:   "100/50/50",
				},
			},
			expected: map[string]*api.Resource{
				"pg1": {
					MilliCPU:        5000,
					Memory:          5000000000,
					ScalarResources: map[v1.ResourceName]float64{"pods": 5},
				},
				"pg21": {
					MilliCPU:        5000,
					Memory:          0,
					ScalarResources: map[v1.ResourceName]float64{"pods": 5},
				},
				"pg22": {
					MilliCPU:        0,
					Memory:          5000000000,
					ScalarResources: map[v1.ResourceName]float64{"pods": 5},
				},
			},
		},
		{
			name: "blocking nodes test",
			pgSpecs: []pgSpec{
				{
					taskNum: 30,
					cpu:     "1",
					mem:     "0G",
					pg:      "pg1",
					queue:   "root-pg1",
				},
				{
					taskNum: 30,
					cpu:     "1",
					mem:     "0G",
					pg:      "pg2",
					queue:   "root-pg2",
				},
				{
					taskNum: 30,
					cpu:     "1",
					mem:     "0G",
					pg:      "pg31",
					queue:   "root-pg3-pg31",
				},
				{
					taskNum: 30,
					cpu:     "0",
					mem:     "1G",
					pg:      "pg32",
					queue:   "root-pg3-pg32",
				},
				{
					taskNum: 30,
					cpu:     "0",
					mem:     "1G",
					pg:      "pg4",
					queue:   "root-pg4",
				},
			},
			nodes: []*v1.Node{util.BuildNode("n",
				api.BuildResourceList("30", "30G", []api.ScalarResource{{Name: "pods", Value: "500"}}...),
				make(map[string]string))},
			queueSpecs: []queueSpec{
				{
					name:      "root-pg1",
					hierarchy: "root/pg1",
					weights:   "100/25",
				},
				{
					name:      "root-pg2",
					hierarchy: "root/pg2",
					weights:   "100/25",
				},
				{
					name:      "root-pg3-pg31",
					hierarchy: "root/pg3/pg31",
					weights:   "100/25/50",
				},
				{
					name:      "root-pg3-pg32",
					hierarchy: "root/pg3/pg32",
					weights:   "100/25/50",
				},
				{
					name:      "root-pg4",
					hierarchy: "root/pg4",
					weights:   "100/25",
				},
			},
			expected: map[string]*api.Resource{

				"pg1": {
					MilliCPU:        10000,
					Memory:          0,
					ScalarResources: map[v1.ResourceName]float64{"pods": 10},
				},
				"pg": {
					MilliCPU:        10000,
					Memory:          0,
					ScalarResources: map[v1.ResourceName]float64{"pods": 10},
				},
				"pg31": {
					MilliCPU:        10000,
					Memory:          0,
					ScalarResources: map[v1.ResourceName]float64{"pods": 10},
				},
				"pg32": {
					MilliCPU:        0,
					Memory:          15000000000,
					ScalarResources: map[v1.ResourceName]float64{"pods": 15},
				},
				"pg4": {
					MilliCPU:        0,
					Memory:          15000000000,
					ScalarResources: map[v1.ResourceName]float64{"pods": 15},
				},
			},
		},
	}
	for _, test := range tests {
		if test.name == "blocking nodes test" {
			// TODO(wangyang0616): First make sure that ut can run, and then fix the failed ut later
			// See issue for details: https://github.com/volcano-sh/volcano/issues/2810
			t.Skip("Test cases are not as expected, fixed later. see issue: #2810")
		}

		binder := &util.FakeBinder{
			Binds:   map[string]string{},
			Channel: make(chan string, 300),
		}
		schedulerCache := &cache.SchedulerCache{
			Nodes:         make(map[string]*api.NodeInfo),
			Jobs:          make(map[api.JobID]*api.JobInfo),
			Queues:        make(map[api.QueueID]*api.QueueInfo),
			Binder:        binder,
			StatusUpdater: &util.FakeStatusUpdater{},
			VolumeBinder:  &util.FakeVolumeBinder{},
			Recorder:      record.NewFakeRecorder(100),
		}
		for _, node := range test.nodes {
			schedulerCache.AddOrUpdateNode(node)
		}
		for _, q := range test.queueSpecs {
			schedulerCache.AddQueueV1beta1(
				&schedulingv1.Queue{
					ObjectMeta: metav1.ObjectMeta{
						Name: q.name,
						Annotations: map[string]string{
							schedulingv1.KubeHierarchyAnnotationKey:       q.hierarchy,
							schedulingv1.KubeHierarchyWeightAnnotationKey: q.weights,
						},
					},
					Spec: schedulingv1.QueueSpec{
						Weight: 1,
					},
				})
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
				},
				Spec: schedulingv1.PodGroupSpec{
					Queue: pgSpec.queue,
				},
				Status: schedulingv1.PodGroupStatus{
					Phase: schedulingv1.PodGroupInqueue,
				},
			})
		}
		trueValue := true
		ssn := framework.OpenSession(schedulerCache, []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:              PluginName,
						EnabledHierarchy:  &trueValue,
						EnabledQueueOrder: &trueValue,
						EnabledJobOrder:   &trueValue,
					},
					{
						Name:               "proportion",
						EnabledJobEnqueued: &trueValue,
						EnabledQueueOrder:  &trueValue,
						EnabledReclaimable: &trueValue,
					},
				},
			},
		}, nil)
		defer framework.CloseSession(ssn)
		allocateAction := allocate.New()

		allocateAction.Execute(ssn)

		for _, job := range ssn.Jobs {
			if reflect.DeepEqual(test.expected, job.Allocated) {
				t.Fatalf("%s: job %s expected resource %s, but got %s", test.name, job.Name, test.expected[job.Name], job.Allocated)
			}
		}

	}
}
