/*
Copyright 2022 The Kubernetes Authors.
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

package proportion

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	apiv1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/actions/enqueue"
	"volcano.sh/volcano/pkg/scheduler/actions/reclaim"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestMain(m *testing.M) {
	options.Default()
	os.Exit(m.Run())
}

func getWorkerAffinity() *apiv1.Affinity {
	return &apiv1.Affinity{
		PodAntiAffinity: &apiv1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []apiv1.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "role",
								Operator: "In",
								Values:   []string{"worker"},
							},
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}
}

func getLocalMetrics() int {
	var data int

	url := "http://127.0.0.1:8081/metrics"
	method := "GET"

	client := &http.Client{}
	req, err := http.NewRequest(method, url, nil)

	if err != nil {
		return data
	}
	req.Header.Add("Authorization", "8cbdb37a-b880-4f2e-844c-e420858ea7eb")

	res, err := client.Do(req)
	if err != nil {
		return data
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return data
	}

	split := strings.Split(string(body), "\n")
	for _, v := range split {
		if !strings.Contains(v, "#") && (strings.Contains(v, "volcano_queue_allocated_memory_bytes") || strings.Contains(v, "volcano_queue_allocated_milli_cpu")) {
			data, _ = strconv.Atoi(strings.Split(v, " ")[1])
		}
	}

	return data
}

func TestProportion(t *testing.T) {
	c := make(chan bool, 1)

	uthelper.RegisterPlugins(map[string]framework.PluginBuilder{PluginName: New, gang.PluginName: gang.New, priority.PluginName: priority.New})
	defer framework.CleanupPluginBuilders()

	// Running pods
	w1 := util.BuildPod("ns1", "worker-1", "", apiv1.PodRunning, api.BuildResourceList("3", "3k"), "pg1", map[string]string{"role": "worker"}, map[string]string{"selector": "worker"})
	w2 := util.BuildPod("ns1", "worker-2", "", apiv1.PodRunning, api.BuildResourceList("5", "5k"), "pg1", map[string]string{"role": "worker"}, map[string]string{})
	w3 := util.BuildPod("ns1", "worker-3", "", apiv1.PodRunning, api.BuildResourceList("4", "4k"), "pg2", map[string]string{"role": "worker"}, map[string]string{})
	w4 := util.BuildPod("ns1", "rdma-demo", "", apiv1.PodRunning, api.BuildResourceList("1", "1k", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "1"}, {Name: "rdma/hca", Value: "1"}}...), "pg3", map[string]string{}, map[string]string{})
	w1.Spec.Affinity = getWorkerAffinity()
	w2.Spec.Affinity = getWorkerAffinity()
	w3.Spec.Affinity = getWorkerAffinity()

	// nodes
	n1 := util.BuildNode("node1", api.BuildResourceList("4", "4k", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"selector": "worker"})
	n2 := util.BuildNode("node2", api.BuildResourceList("3", "3k", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{})
	n3 := util.BuildNode("node3", api.BuildResourceList("4", "4k", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "nvidia.com/gpu", Value: "8"}, {Name: "rdma/hca", Value: "1k"}}...), map[string]string{})
	n1.Status.Allocatable["pods"] = resource.MustParse("15")
	n2.Status.Allocatable["pods"] = resource.MustParse("15")
	n3.Status.Allocatable["pods"] = resource.MustParse("15")
	n1.Labels["kubernetes.io/hostname"] = "node1"
	n2.Labels["kubernetes.io/hostname"] = "node2"
	n3.Labels["kubernetes.io/hostname"] = "node3"

	// priority
	p1 := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "p1"}, Value: 1}
	p2 := &schedulingv1.PriorityClass{ObjectMeta: metav1.ObjectMeta{Name: "p2"}, Value: 2}
	// podgroup
	pg1 := util.BuildPodGroupWithPrio("pg1", "ns1", "q1", 2, nil, "", p2.Name)
	pg2 := util.BuildPodGroupWithPrio("pg2", "ns1", "q1", 1, nil, "", p1.Name)
	pg3 := util.BuildPodGroupWithPrio("pg3", "ns1", "q2", 1, nil, "", p1.Name)

	pgRes3 := api.BuildResourceList("1", "1k", []api.ScalarResource{{Name: "nvidia.com/gpu", Value: "1"}, {Name: "rdma/hca", Value: "1"}}...)
	pg3.Spec.MinResources = &pgRes3

	// queue
	queue1 := util.BuildQueue("q1", 0, nil)
	queue2 := util.BuildQueue("q2", 0, api.BuildResourceList("2", "2k", []api.ScalarResource{{Name: "pods", Value: "10"}, {Name: "nvidia.com/gpu", Value: "4"}}...))

	// tests
	tests := []struct {
		name     string
		pods     []*apiv1.Pod
		nodes    []*apiv1.Node
		pcs      []*schedulingv1.PriorityClass
		pgs      []*schedulingv1beta1.PodGroup
		expected map[string]string
	}{
		{
			name:  "pod-deallocate",
			pods:  []*apiv1.Pod{w1, w2, w3},
			nodes: []*apiv1.Node{n1, n2},
			pcs:   []*schedulingv1.PriorityClass{p1, p2},
			pgs:   []*schedulingv1beta1.PodGroup{pg1, pg2},
			expected: map[string]string{ // podKey -> node
				"ns1/worker-3": "node1",
			},
		},
		{
			name:  "realcapability-test",
			pods:  []*apiv1.Pod{w1, w2, w3, w4},
			nodes: []*apiv1.Node{n1, n2, n3},
			pcs:   []*schedulingv1.PriorityClass{p1, p2},
			pgs:   []*schedulingv1beta1.PodGroup{pg1, pg2, pg3},
			expected: map[string]string{ // podKey -> node
				"ns1/rdma-demo": "node3",
			},
		},
	}

	for _, test := range tests {
		// initialize schedulerCache
		binder := util.NewFakeBinder(0)
		recorder := record.NewFakeRecorder(100)
		go func() {
			for {
				event := <-recorder.Events
				t.Logf("%s: [Event] %s", test.name, event)
			}
		}()
		schedulerCache := cache.NewCustomMockSchedulerCache("mock-test", binder, nil, &util.FakeStatusUpdater{}, nil, &util.FakeVolumeBinder{}, recorder)

		for _, node := range test.nodes {
			schedulerCache.AddOrUpdateNode(node)
		}
		for _, pod := range test.pods {
			schedulerCache.AddPod(pod)
		}
		for _, pc := range test.pcs {
			schedulerCache.PriorityClasses[pc.Name] = pc
		}
		for _, pg := range test.pgs {
			pg.Status = schedulingv1beta1.PodGroupStatus{
				Phase: schedulingv1beta1.PodGroupInqueue,
			}
			schedulerCache.AddPodGroupV1beta1(pg)
		}
		schedulerCache.AddQueueV1beta1(queue1)
		schedulerCache.AddQueueV1beta1(queue2)
		// session
		trueValue := true

		num := 1
		// proportion
		go func() {
			for {
				ssn := framework.OpenSession(schedulerCache, []conf.Tier{
					{
						Plugins: []conf.PluginOption{
							{
								Name:             PluginName,
								EnabledPredicate: &trueValue,
							},
							{
								Name:                gang.PluginName,
								EnabledJobReady:     &trueValue,
								EnabledJobPipelined: &trueValue,
							},
							{
								Name:            priority.PluginName,
								EnabledJobOrder: &trueValue,
							},
						},
					},
				}, nil)

				allocator := allocate.New()
				allocator.Execute(ssn)
				framework.CloseSession(ssn)
				time.Sleep(time.Second * 3)
				if num == 1 {
					metrics := getLocalMetrics()
					if metrics == 12000 {
						t.Logf("init queue_allocated metrics is ok,%v", metrics)
					}
					schedulerCache.DeletePodGroupV1beta1(pg1)
				} else if num == 2 {
					metrics := getLocalMetrics()
					if metrics == 4000 {
						t.Logf("after delete vcjob pg1, queue_allocated metrics is ok,%v", metrics)
					}
					schedulerCache.DeletePodGroupV1beta1(pg2)
				} else {
					metrics := getLocalMetrics()
					if metrics != 0 {
						t.Errorf("after delete vcjob pg2, queue_allocated metrics is fail,%v", metrics)
						c <- false
						return
					}
					t.Logf("after delete vcjob pg2, queue_allocated metrics is ok,%v", metrics)
					c <- true
				}
				num++
			}
		}()

		go func() {
			http.Handle("/metrics", promhttp.Handler())
			err := http.ListenAndServe(":8081", nil)
			if err != nil {
				t.Errorf("ListenAndServe() err = %v", err.Error())
			}
		}()

		for res := range c {
			if !res {
				t.Error("TestProportion failed")
			} else {
				t.Log("TestProportion successful")
			}
			return
		}
	}
}

func TestEnqueueAndAllocable(t *testing.T) {
	// nodes
	n1 := util.BuildNode("n1", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil)
	n2 := util.BuildNode("n2", api.BuildResourceList("2", "2G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil)

	// resources
	res1c2g := api.BuildResourceList("1", "2G")
	res2c1g := api.BuildResourceList("2", "1G")
	res1c0g := api.BuildResourceList("1", "0G")
	res0c1g := api.BuildResourceList("0", "1G")
	res1c1g := api.BuildResourceList("1", "1G")
	// pod
	p1 := util.BuildPod("ns1", "pod1", "n1", apiv1.PodRunning, res1c2g, "pg1", nil, nil)
	p2 := util.BuildPod("ns1", "pod2", "n2", apiv1.PodRunning, res2c1g, "pg2", nil, nil)
	p3 := util.BuildPod("ns1", "pod3", "", apiv1.PodPending, res1c0g, "pg3", nil, nil)
	p4 := util.BuildPod("ns1", "pod4", "", apiv1.PodPending, res0c1g, "pg4", nil, nil)
	p5 := util.BuildPod("ns1", "pod5", "", apiv1.PodPending, res1c1g, "pg5", nil, nil)
	p6 := util.BuildPod("ns1", "pod6", "", apiv1.PodPending, res1c1g, "pg6", nil, nil)

	// podgroup
	pg1 := util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg2 := util.BuildPodGroup("pg2", "ns1", "q2", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg3 := util.BuildPodGroup("pg3", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupPending)
	pg4 := util.BuildPodGroup("pg4", "ns1", "q2", 1, nil, schedulingv1beta1.PodGroupPending)
	pg5 := util.BuildPodGroup("pg5", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupPending)
	pg6WithClosedQueue := util.BuildPodGroup("pg6", "ns1", "q3", 1, nil, schedulingv1beta1.PodGroupPending)

	pg1.Spec.MinResources = &res1c2g
	pg2.Spec.MinResources = &res2c1g
	pg3.Spec.MinResources = &res1c0g
	pg4.Spec.MinResources = &res0c1g
	pg5.Spec.MinResources = &res1c1g
	pg6WithClosedQueue.Spec.MinResources = &res1c1g

	queue1 := util.BuildQueue("q1", 1, api.BuildResourceList("2", "2G"))
	queue2 := util.BuildQueue("q2", 1, api.BuildResourceList("3", "3G"))
	closedQueue3 := util.BuildQueueWithState("q3", 1, api.BuildResourceList("3", "3G"), schedulingv1beta1.QueueStateClosed)

	plugins := map[string]framework.PluginBuilder{PluginName: New}
	trueValue, falseValue := true, false
	allEnable := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               PluginName,
					EnabledAllocatable: &trueValue,
					EnabledOverused:    &trueValue,
					EnabledJobEnqueued: &trueValue,
				},
			},
		},
	}
	enqueueable := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               PluginName,
					EnabledAllocatable: &falseValue,
					EnabledOverused:    &falseValue,
					EnabledJobEnqueued: &trueValue,
				},
			},
		},
	}
	allocatable := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               PluginName,
					EnabledAllocatable: &trueValue,
					EnabledOverused:    &falseValue,
					EnabledJobEnqueued: &falseValue,
				},
			},
		},
	}
	tests := []struct {
		uthelper.TestCommonStruct
		tiers []conf.Tier
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:           "case0: memory exceed derserved, job only request cpu can be enqueued and allocated",
				Plugins:        plugins,
				Pods:           []*apiv1.Pod{p1, p2, p3},
				Nodes:          []*apiv1.Node{n1, n2},
				PodGroups:      []*schedulingv1beta1.PodGroup{pg1, pg2, pg3},
				Queues:         []*schedulingv1beta1.Queue{queue1, queue2},
				ExpectBindsNum: 1,
				ExpectBindMap:  map[string]string{"ns1/pod3": "n1"},
			},
			tiers: allEnable,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:           "case1: cpu exceed derserved, job only request memory can be enqueued and allocated",
				Plugins:        plugins,
				Pods:           []*apiv1.Pod{p1, p2, p4},
				Nodes:          []*apiv1.Node{n1, n2},
				PodGroups:      []*schedulingv1beta1.PodGroup{pg1, pg2, pg4},
				Queues:         []*schedulingv1beta1.Queue{queue1, queue2},
				ExpectBindsNum: 1,
				ExpectBindMap:  map[string]string{"ns1/pod4": "n2"},
			},
			tiers: allEnable,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:           "case2: exceed capacity, can not enqueue",
				Plugins:        plugins,
				Pods:           []*apiv1.Pod{p1, p2, p5},
				Nodes:          []*apiv1.Node{n1, n2},
				PodGroups:      []*schedulingv1beta1.PodGroup{pg1, pg2, pg5},
				Queues:         []*schedulingv1beta1.Queue{queue1, queue2},
				ExpectBindsNum: 0,
				ExpectBindMap:  map[string]string{},
			},
			tiers: enqueueable,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:           "case3: exceed deserved, can not allocate",
				Plugins:        plugins,
				Pods:           []*apiv1.Pod{p1, p2, p5},
				Nodes:          []*apiv1.Node{n1, n2},
				PodGroups:      []*schedulingv1beta1.PodGroup{pg1, pg2, pg5},
				Queues:         []*schedulingv1beta1.Queue{queue1, queue2},
				ExpectBindsNum: 0,
				ExpectBindMap:  map[string]string{},
			},
			tiers: allocatable,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:           "case4: queue with  non-open state, can not enqueue",
				Plugins:        plugins,
				Pods:           []*apiv1.Pod{p6},
				Nodes:          []*apiv1.Node{n1, n2},
				PodGroups:      []*schedulingv1beta1.PodGroup{pg6WithClosedQueue},
				Queues:         []*schedulingv1beta1.Queue{closedQueue3},
				ExpectBindsNum: 0,
				ExpectBindMap:  map[string]string{},
			},
			tiers: enqueueable,
		},
	}
	actions := []framework.Action{enqueue.New(), allocate.New()}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(test.tiers, nil)
			defer test.Close()
			test.Run(actions)

			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAllocate(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{PluginName: New}
	trueValue := true
	actions := []framework.Action{allocate.New(), reclaim.New()}

	// nodes
	n1 := util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	n2 := util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))

	// pod
	p1 := util.BuildPod("ns1", "p1", "n1", apiv1.PodRunning, api.BuildResourceList("2", "4Gi"), "pg1", make(map[string]string), make(map[string]string))
	p2 := util.BuildPod("ns1", "p2", "", apiv1.PodPending, api.BuildResourceList("2", "4Gi"), "pg2", make(map[string]string), make(map[string]string))
	p3 := util.BuildPod("ns1", "p3", "", apiv1.PodPending, api.BuildResourceList("2", "4Gi"), "pg3", make(map[string]string), make(map[string]string))

	// podgroup
	pg1 := util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning)
	pg2 := util.BuildPodGroup("pg2", "ns1", "q2", 1, nil, schedulingv1beta1.PodGroupInqueue)
	pg3 := util.BuildPodGroup("pg3", "ns1", "q3", 1, nil, schedulingv1beta1.PodGroupInqueue)

	// queue
	queue1 := util.BuildQueueWithPriorityAndResourcesQuantity("q1", 5, nil, api.BuildResourceList("2", "4Gi"))
	queue2 := util.BuildQueueWithPriorityAndResourcesQuantity("q2", 1, nil, api.BuildResourceList("2", "4Gi"))
	queue3 := util.BuildQueueWithPriorityAndResourcesQuantity("q3", 10, nil, api.BuildResourceList("2", "4Gi"))

	tests := []uthelper.TestCommonStruct{
		{
			Name:      "case0: Pods are assigned according to the order of Queue Priority in which PGs are placed",
			Plugins:   plugins,
			Pods:      []*apiv1.Pod{p1, p2, p3},
			Nodes:     []*apiv1.Node{n1, n2},
			PodGroups: []*schedulingv1beta1.PodGroup{pg1, pg2, pg3},
			Queues:    []*schedulingv1beta1.Queue{queue1, queue2, queue3},
			ExpectBindMap: map[string]string{
				"ns1/p3": "n2",
			},
			ExpectBindsNum: 1,
		},
	}

	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:              PluginName,
					EnabledQueueOrder: &trueValue,
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run(actions)
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
