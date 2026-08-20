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

package backfill

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	schedulingapi "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/tools/record"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	volcanofeatures "volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/resourcefit"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestPickUpPendingTasks(t *testing.T) {
	framework.RegisterPluginBuilder("priority", priority.New)
	framework.RegisterPluginBuilder("drf", drf.New)
	trueValue := true
	tilers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:               "priority",
					EnabledPreemptable: &trueValue,
					EnabledTaskOrder:   &trueValue,
					EnabledJobOrder:    &trueValue,
				},
				{
					Name:              "drf",
					EnabledQueueOrder: &trueValue,
				},
			},
		},
	}

	priority4, priority3, priority2, priority1 := int32(4), int32(3), int32(2), int32(1)

	testCases := []struct {
		name            string
		pipelinedPods   []*v1.Pod
		pendingPods     []*v1.Pod
		queues          []*schedulingv1beta1.Queue
		podGroups       []*schedulingv1beta1.PodGroup
		PriorityClasses map[string]*schedulingapi.PriorityClass
		expectedResult  []string
	}{
		{
			name: "test",
			pendingPods: []*v1.Pod{
				util.BuildPodWithPriority("default", "pg1-besteffort-task-1", "", v1.PodPending, nil, "pg1", make(map[string]string), make(map[string]string), &priority1),
				util.BuildPodWithPriority("default", "pg1-unbesteffort-task-1", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg1", make(map[string]string), make(map[string]string), &priority1),
				util.BuildPodWithPriority("default", "pg1-besteffort-task-3", "", v1.PodPending, nil, "pg1", make(map[string]string), make(map[string]string), &priority3),
				util.BuildPodWithPriority("default", "pg1-unbesteffort-task-3", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg1", make(map[string]string), make(map[string]string), &priority3),

				util.BuildPodWithPriority("default", "pg2-besteffort-task-1", "", v1.PodPending, nil, "pg2", make(map[string]string), make(map[string]string), &priority1),
				util.BuildPodWithPriority("default", "pg2-unbesteffort-task-1", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg2", make(map[string]string), make(map[string]string), &priority1),
				util.BuildPodWithPriority("default", "pg2-besteffort-task-3", "", v1.PodPending, nil, "pg2", make(map[string]string), make(map[string]string), &priority3),
				util.BuildPodWithPriority("default", "pg2-unbesteffort-task-3", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg2", make(map[string]string), make(map[string]string), &priority3),
			},
			pipelinedPods: []*v1.Pod{
				util.BuildPodWithPriority("default", "pg1-besteffort-task-2", "", v1.PodPending, nil, "pg1", make(map[string]string), make(map[string]string), &priority2),
				util.BuildPodWithPriority("default", "pg1-unbesteffort-task-2", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg1", make(map[string]string), make(map[string]string), &priority2),
				util.BuildPodWithPriority("default", "pg1-besteffort-task-4", "", v1.PodPending, nil, "pg1", make(map[string]string), make(map[string]string), &priority4),
				util.BuildPodWithPriority("default", "pg1-unbesteffort-task-4", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg1", make(map[string]string), make(map[string]string), &priority4),

				util.BuildPodWithPriority("default", "pg2-besteffort-task-2", "", v1.PodPending, nil, "pg2", make(map[string]string), make(map[string]string), &priority2),
				util.BuildPodWithPriority("default", "pg2-unbesteffort-task-2", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg2", make(map[string]string), make(map[string]string), &priority2),
				util.BuildPodWithPriority("default", "pg2-besteffort-task-4", "", v1.PodPending, nil, "pg2", make(map[string]string), make(map[string]string), &priority4),
				util.BuildPodWithPriority("default", "pg2-unbesteffort-task-4", "", v1.PodPending, v1.ResourceList{"cpu": resource.MustParse("500m")}, "pg2", make(map[string]string), make(map[string]string), &priority4),
			},
			queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
			podGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithPrio("pg1", "default", "q1", 1, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue, "job-priority-1"),
				util.BuildPodGroupWithPrio("pg2", "default", "q1", 1, map[string]int32{"": 3}, schedulingv1beta1.PodGroupInqueue, "job-priority-2"),
			},
			PriorityClasses: map[string]*schedulingapi.PriorityClass{
				"job-priority-1": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "job-priority-1",
					},
					Value: 1,
				},
				"job-priority-2": {
					ObjectMeta: metav1.ObjectMeta{
						Name: "job-priority-2",
					},
					Value: 2,
				},
			},

			expectedResult: []string{
				"pg2-besteffort-task-4",
				"pg2-besteffort-task-3",
				"pg2-besteffort-task-2",
				"pg2-besteffort-task-1",
				"pg1-besteffort-task-4",
				"pg1-besteffort-task-3",
				"pg1-besteffort-task-2",
				"pg1-besteffort-task-1",
			},
		},
	}

	for _, tc := range testCases {
		schedulerCache := &cache.SchedulerCache{
			Nodes:           make(map[string]*api.NodeInfo),
			Jobs:            make(map[api.JobID]*api.JobInfo),
			Queues:          make(map[api.QueueID]*api.QueueInfo),
			Binder:          nil,
			StatusUpdater:   &util.FakeStatusUpdater{},
			Recorder:        record.NewFakeRecorder(100),
			PriorityClasses: tc.PriorityClasses,
			HyperNodesInfo:  api.NewHyperNodesInfo(nil),
		}

		for _, q := range tc.queues {
			schedulerCache.AddQueueV1beta1(q)
		}

		for _, ss := range tc.podGroups {
			schedulerCache.AddPodGroupV1beta1(ss)
		}

		for _, pod := range tc.pendingPods {
			schedulerCache.AddPod(pod)
		}

		for _, pod := range tc.pipelinedPods {
			schedulerCache.AddPod(pod)
		}

		ssn := framework.OpenSession(schedulerCache, tilers, []conf.Configuration{})
		for _, pod := range tc.pipelinedPods {
			jobID := api.NewTaskInfo(pod).Job
			stmt := framework.NewStatement(ssn)
			task, found := ssn.Jobs[jobID].Tasks[api.PodKey(pod)]
			if found {
				stmt.Pipeline(task, "node1", false)
			}
		}

		tasks := New().pickUpPendingTasks(ssn)
		var actualResult []string
		for _, task := range tasks {
			actualResult = append(actualResult, task.Name)
		}

		if !assert.Equal(t, tc.expectedResult, actualResult) {
			t.Errorf("unexpected test; name: %s, expected result: %v, actual result: %v", tc.name, tc.expectedResult, actualResult)
		}
	}
}

type batchNodeOrderErrPlugin struct{}

func (p *batchNodeOrderErrPlugin) Name() string { return "batch-node-order-err" }

func (p *batchNodeOrderErrPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddBatchNodeOrderFn(p.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		return nil, errors.New("batch node order failed")
	})
}

func (p *batchNodeOrderErrPlugin) OnSessionClose(ssn *framework.Session) {}

// TestBackfillSkipsTaskWhenNoBestNode verifies that backfill skips a task
// instead of panicking when node scoring fails and no best node is selected.
func TestBackfillSkipsTaskWhenNoBestNode(t *testing.T) {
	test := uthelper.TestCommonStruct{
		Name: "scoring failure leaves task unscheduled without panic",
		Plugins: map[string]framework.PluginBuilder{
			"batch-node-order-err": func(arguments framework.Arguments) framework.Plugin {
				return &batchNodeOrderErrPlugin{}
			},
		},
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1beta1.PodGroupInqueue),
		},
		Pods: []*v1.Pod{
			util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("0", "0"), "pg1", make(map[string]string), make(map[string]string)),
		},
		Nodes: []*v1.Node{
			util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
		},
		Queues: []*schedulingv1beta1.Queue{
			util.BuildQueue("c1", 1, nil),
		},
		ExpectBindsNum: 0,
		ExpectBindMap:  map[string]string{},
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:             "batch-node-order-err",
					EnabledNodeOrder: &trueValue,
				},
			},
		},
	}

	test.RegisterSession(tiers, nil)
	defer test.Close()
	test.Run([]framework.Action{New()})
	if err := test.CheckBind(0); err != nil {
		t.Fatal(err)
	}
}

// fakeUnschedulableCacheForBackfill is a minimal cache.UnschedulableCache used
// to observe the rejections recorded at CloseSession without exercising the
// real cache's event-index bookkeeping.
type fakeUnschedulableCacheForBackfill struct {
	recorded map[api.JobID][]api.Rejection
}

func (f *fakeUnschedulableCacheForBackfill) BeginSession() {}

func (f *fakeUnschedulableCacheForBackfill) AddHintProvider(string, api.HintProvider) {}

func (f *fakeUnschedulableCacheForBackfill) RecordUnschedulable(job *api.JobInfo, rejections []api.Rejection) {
	if f.recorded == nil {
		f.recorded = map[api.JobID][]api.Rejection{}
	}
	f.recorded[job.UID] = rejections
}

func (f *fakeUnschedulableCacheForBackfill) GetCachedRejections(*api.JobInfo) []api.Rejection {
	return nil
}

func (f *fakeUnschedulableCacheForBackfill) ForgetUnschedulable(api.JobID) {}

// TestBackfillResourceFitRejectionCarriesHintKeys proves that backfill's
// fitErrors.UnschedulablePlugins() loop calls AddRejectionWithKeys for the
// resource-fit plugin, while a different rejecting plugin on the same task
// still falls back to the plain, key-less AddRejection. The resource-fit rejection is synthesized directly (rather
// than routed through the real "predicates" plugin's NodePodNumberExceeded
// check) because backfill only schedules BestEffort tasks, whose zero CPU/Mem
// request never fails a resource check; only "pods" fits that description,
// and driving it end-to-end would additionally require a live k8s snapshot
// lister. The synthesized Status carries InsufficientResources: []string{"pods"},
// matching how the real predicates Pod-count check populates it, since
// ResourceFitRejectionKeys now reads dimensions from that structured field
// rather than recomputing them from node.FutureIdle().
func TestBackfillResourceFitRejectionCarriesHintKeys(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, volcanofeatures.UnschedulableJobCache, true)

	binder := util.NewFakeBinder(0)
	evictor := util.NewFakeEvictor(0)
	statusUpdater := &util.FakeStatusUpdater{}
	schedulerCache := cache.NewCustomMockSchedulerCache("ut-backfill-resource-fit-hint-keys", binder, evictor, statusUpdater, nil, nil)
	stop := make(chan struct{})
	defer close(stop)
	schedulerCache.Run(stop)
	schedulerCache.WaitForCacheSync(stop)

	// node-small has plenty of total allocatable "pods" capacity (100), but
	// the fake resource-fit predicate rejects it anyway with a structured
	// "pods" dimension — mirroring the real predicates Pod-count check, whose
	// verdict is driven by the live k8s snapshot's Pod count rather than
	// Volcano's own Idle/Releasing bookkeeping. Because the node's *total*
	// allocatable capacity (100) comfortably exceeds the task's 1-pod
	// request, the Pod-release key must still be produced.
	if err := schedulerCache.AddOrUpdateNode(util.BuildNode("node-small",
		api.BuildResourceList("1", "4Gi", []api.ScalarResource{{Name: "pods", Value: "100"}}...), nil)); err != nil {
		t.Fatalf("AddOrUpdateNode(node-small) error = %v", err)
	}
	// node-other has an idle pod slot, so it passes the fake resource-fit
	// predicate and reaches the fake "fake-other" predicate, which rejects it.
	if err := schedulerCache.AddOrUpdateNode(util.BuildNode("node-other",
		api.BuildResourceList("1", "4Gi", []api.ScalarResource{{Name: "pods", Value: "100"}}...), nil)); err != nil {
		t.Fatalf("AddOrUpdateNode(node-other) error = %v", err)
	}

	schedulerCache.AddQueueV1beta1(util.BuildQueue("q1", 1, nil))
	schedulerCache.AddPodGroupV1beta1(util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue))
	// nil resource requests makes the task BestEffort, which is required for
	// backfill.pickUpPendingTasks to consider it at all.
	schedulerCache.AddPod(util.BuildPod("ns1", "task1", "", v1.PodPending, nil, "pg1", nil, nil))

	trueValue := true
	tilers := []conf.Tier{{Plugins: []conf.PluginOption{
		{Name: resourcefit.ProviderName, EnabledPredicate: &trueValue},
		{Name: "fake-other", EnabledPredicate: &trueValue},
	}}}

	fakeCache := &fakeUnschedulableCacheForBackfill{}
	ssn := framework.OpenSession(schedulerCache, tilers, []conf.Configuration{}, framework.WithUnschedulableCache(fakeCache))

	ssn.AddPredicateFn(resourcefit.ProviderName, func(task *api.TaskInfo, node *api.NodeInfo) error {
		if node.Name == "node-small" {
			return api.NewFitErrWithStatus(task, node, &api.Status{
				Code:                  api.Unschedulable,
				Plugin:                resourcefit.ProviderName,
				InsufficientResources: []string{"pods"},
			})
		}
		return nil
	})
	ssn.AddPredicateFn("fake-other", func(task *api.TaskInfo, node *api.NodeInfo) error {
		if node.Name == "node-other" {
			return api.NewFitErrWithStatus(task, node, &api.Status{Code: api.UnschedulableAndUnresolvable, Plugin: "fake-other"})
		}
		return nil
	})

	New().Execute(ssn)
	framework.CloseSession(ssn)

	rejections := fakeCache.recorded["ns1/pg1"]
	if !assert.NotEmpty(t, rejections, "expected rejections to be recorded for job ns1/pg1") {
		return
	}

	var resourceFit, other *api.Rejection
	for i := range rejections {
		switch rejections[i].Plugin {
		case resourcefit.ProviderName:
			resourceFit = &rejections[i]
		case "fake-other":
			other = &rejections[i]
		}
	}

	if assert.NotNil(t, resourceFit, "expected a resource-fit rejection") {
		assert.NotEmpty(t, resourceFit.HintKeys, "resource-fit rejection must carry hint keys")
		assert.True(t, hasPodReleaseKeyForDimension(resourceFit.HintKeys, "node-small", "pods"),
			"resource-fit rejection must carry a pod-release key for node-small's pods dimension; "+
				"got %v", resourceFit.HintKeys)
	}
	if assert.NotNil(t, other, "expected a fake-other rejection") {
		assert.Empty(t, other.HintKeys, "non-resource-fit rejection must not carry hint keys")
	}
}

// hasPodReleaseKeyForDimension reports whether keys contains a Pod-release
// hint key for the given node and resource dimension. It mirrors the
// slash-separated encoding resourcefit key produces
// ("pod-release/<node>/<dimension>") without importing the unexported
// constructor, so this test observes the same externally-visible key the
// unschedulable cache would use to narrow dispatch.
func hasPodReleaseKeyForDimension(keys []api.HintKey, node, dimension string) bool {
	want := fmt.Sprintf("pod-release/%s/%s", node, dimension)
	for _, k := range keys {
		if string(k) == want {
			return true
		}
	}
	return false
}
