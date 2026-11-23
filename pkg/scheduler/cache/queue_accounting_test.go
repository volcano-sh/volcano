/*
Copyright 2026 The Volcano Authors.

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

package cache

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/features"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	accountingNamespace = "ns1"
	accountingGPU       = v1.ResourceName("nvidia.com/gpu")
)

// queueTotals is the expected direct totals of a queue: allocated and requested
// CPU in millicores, and allocated GPUs in milli-units.
type queueTotals struct {
	allocated    float64
	request      float64
	allocatedGPU float64
}

// accountingStep mutates the cache through one of its entry points.
type accountingStep func(t *testing.T, sc *SchedulerCache)

func newAccountingCache(t *testing.T) *SchedulerCache {
	t.Helper()
	sc := NewCustomMockSchedulerCache("volcano", nil, util.NewFakeEvictor(10), nil, nil, nil)
	sc.BindFlowChannel = make(chan *BindContext, 10)
	t.Cleanup(func() { ShutdownMockSchedulerCache(sc) })
	require.NoError(t, sc.AddOrUpdateNode(util.BuildNode("n1", schedulingapi.BuildResourceList("10", "10Gi",
		[]schedulingapi.ScalarResource{{Name: "pods", Value: "10"}, {Name: string(accountingGPU), Value: "8"}}...), nil)))
	return sc
}

func accountingPod(name, nodeName string, phase v1.PodPhase, cpu, podGroup string) *v1.Pod {
	return util.BuildPod(accountingNamespace, name, nodeName, phase,
		schedulingapi.BuildResourceList(cpu, "1Gi"), podGroup, nil, nil)
}

func accountingPodGroup(name, queue, resourceVersion string) *schedulingv1beta1.PodGroup {
	pg := util.BuildPodGroup(name, accountingNamespace, queue, 1, nil, schedulingv1beta1.PodGroupRunning)
	pg.ResourceVersion = resourceVersion
	return pg
}

func accountingQueue(name, resourceVersion string) *schedulingv1beta1.Queue {
	q := util.BuildQueue(name, 1, nil)
	q.ResourceVersion = resourceVersion
	return q
}

func addQueue(name string) accountingStep {
	return func(_ *testing.T, sc *SchedulerCache) { sc.AddQueueV1beta1(accountingQueue(name, "1")) }
}

// enableNamespaceQueues turns on the NamespaceQueue feature gate for the rest of the test.
func enableNamespaceQueues(t *testing.T, _ *SchedulerCache) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)
}

// addNamespaceQueue adds NamespaceQueue accountingNamespace/name under cluster queue q1.
func addNamespaceQueue(name string) accountingStep {
	return func(_ *testing.T, sc *SchedulerCache) {
		sc.AddNamespaceQueueV1beta1(&schedulingv1beta1.NamespaceQueue{
			ObjectMeta: metav1.ObjectMeta{Namespace: accountingNamespace, Name: name, ResourceVersion: "1"},
			Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "cluster/q1"},
		})
	}
}

func addPodGroup(name, queue string) accountingStep {
	return func(_ *testing.T, sc *SchedulerCache) { sc.AddPodGroupV1beta1(accountingPodGroup(name, queue, "1")) }
}

func addPod(pod *v1.Pod) accountingStep {
	return func(_ *testing.T, sc *SchedulerCache) { sc.AddPod(pod) }
}

// bindPod binds a cached task to n1 through AddBindTask.
func bindPod(pod *v1.Pod, wantErr bool) accountingStep {
	return func(t *testing.T, sc *SchedulerCache) {
		task := cachedTask(t, sc, pod).Clone()
		task.NodeName = "n1"
		err := sc.AddBindTask(&BindContext{TaskInfo: task})
		if wantErr {
			require.Error(t, err)
			return
		}
		require.NoError(t, err)
	}
}

func evictPod(pod *v1.Pod) accountingStep {
	return func(t *testing.T, sc *SchedulerCache) {
		require.NoError(t, sc.Evict(cachedTask(t, sc, pod), "test"))
	}
}

func cachedTask(t *testing.T, sc *SchedulerCache, pod *v1.Pod) *schedulingapi.TaskInfo {
	t.Helper()
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	pi := schedulingapi.NewTaskInfo(pod)
	job, found := sc.Jobs[pi.Job]
	require.True(t, found, "job %s not in cache", pi.Job)
	task, found := job.Tasks[pi.UID]
	require.True(t, found, "task %s not in cache", pi.UID)
	return task
}

// recomputeQueueTotals computes direct queue totals from each job's tasks,
// independently of the incremental accounting: allocated-status tasks count
// toward allocated and request, Pending tasks toward request only, over jobs a
// Snapshot exposes.
func recomputeQueueTotals(sc *SchedulerCache) map[schedulingapi.QueueID][2]*schedulingapi.Resource {
	totals := map[schedulingapi.QueueID][2]*schedulingapi.Resource{}
	for qid := range sc.Queues {
		totals[qid] = [2]*schedulingapi.Resource{schedulingapi.EmptyResource(), schedulingapi.EmptyResource()}
	}
	for _, job := range sc.Jobs {
		q, found := totals[job.Queue]
		if job.PodGroup == nil || !found {
			continue
		}
		for status, tasks := range job.TaskStatusIndex {
			for _, task := range tasks {
				if schedulingapi.AllocatedStatus(status) {
					q[0].Add(task.Resreq)
					q[1].Add(task.Resreq)
				} else if status == schedulingapi.Pending {
					q[1].Add(task.Resreq)
				}
			}
		}
	}
	return totals
}

func assertQueueTotalsMatchRecompute(t *testing.T, sc *SchedulerCache, step int) {
	t.Helper()
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	for qid, want := range recomputeQueueTotals(sc) {
		q := sc.Queues[qid]
		assert.Truef(t, want[0].Equal(q.DirectAllocated, schedulingapi.Zero),
			"step %d: queue %s allocated = %v, recompute = %v", step, qid, q.DirectAllocated, want[0])
		assert.Truef(t, want[1].Equal(q.DirectRequest, schedulingapi.Zero),
			"step %d: queue %s request = %v, recompute = %v", step, qid, q.DirectRequest, want[1])
	}
}

func runAccountingSteps(t *testing.T, steps []accountingStep, want map[string]queueTotals) {
	t.Helper()
	sc := newAccountingCache(t)
	for i, step := range steps {
		step(t, sc)
		assertQueueTotalsMatchRecompute(t, sc, i)
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	for name, totals := range want {
		q, found := sc.Queues[schedulingapi.QueueID(name)]
		require.True(t, found, "queue %s not in cache", name)
		assert.InDelta(t, totals.allocated, q.DirectAllocated.MilliCPU, 0.01, fmt.Sprintf("queue %s allocated CPU", name))
		assert.InDelta(t, totals.request, q.DirectRequest.MilliCPU, 0.01, fmt.Sprintf("queue %s request CPU", name))
		assert.InDelta(t, totals.allocatedGPU, q.DirectAllocated.ScalarResources[accountingGPU], 0.01, fmt.Sprintf("queue %s allocated GPU", name))
	}
}

func TestUpdateQueueForJob(t *testing.T) {
	pending := accountingPod("pending", "", v1.PodPending, "1", "j1")
	running := accountingPod("running", "n1", v1.PodRunning, "2", "j1")
	succeeded := accountingPod("succeeded", "n1", v1.PodSucceeded, "4", "j1")
	oversized := accountingPod("oversized", "", v1.PodPending, "20", "j1")
	// The same pod as pending, after it starts running on n1.
	pendingStarted := accountingPod("pending", "n1", v1.PodRunning, "1", "j1")
	runningGPU := util.BuildPod(accountingNamespace, "running-gpu", "n1", v1.PodRunning,
		schedulingapi.BuildResourceList("1", "1Gi", schedulingapi.ScalarResource{Name: string(accountingGPU), Value: "2"}), "j1", nil, nil)

	tests := []struct {
		name  string
		steps []accountingStep
		want  map[string]queueTotals
	}{
		{
			name:  "pending tasks count toward request and running tasks toward both",
			steps: []accountingStep{addQueue("q1"), addPodGroup("j1", "q1"), addPod(pending), addPod(running)},
			want:  map[string]queueTotals{"q1": {allocated: 2000, request: 3000}},
		},
		{
			name:  "completed tasks are excluded from request",
			steps: []accountingStep{addQueue("q1"), addPodGroup("j1", "q1"), addPod(running), addPod(succeeded)},
			want:  map[string]queueTotals{"q1": {allocated: 2000, request: 2000}},
		},
		{
			name:  "GPU requests count toward the queue's scalar resources",
			steps: []accountingStep{addQueue("q1"), addPodGroup("j1", "q1"), addPod(runningGPU)},
			want:  map[string]queueTotals{"q1": {allocated: 1000, request: 1000, allocatedGPU: 2000}},
		},
		{
			name:  "a PodGroup without a queue counts toward the default queue",
			steps: []accountingStep{addQueue("default"), addPodGroup("j1", ""), addPod(pending), addPod(running)},
			want:  map[string]queueTotals{"default": {allocated: 2000, request: 3000}},
		},
		{
			name: "a job in a NamespaceQueue counts toward its namespaced queue ID",
			steps: []accountingStep{enableNamespaceQueues, addQueue("q1"), addNamespaceQueue("nq"),
				addPodGroup("j1", "namespace/nq"), addPod(pending), addPod(running)},
			want: map[string]queueTotals{"q1": {}, accountingNamespace + "/nq": {allocated: 2000, request: 3000}},
		},
		{
			name:  "tasks of a job without a PodGroup are not counted",
			steps: []accountingStep{addQueue("q1"), addPod(running)},
			want:  map[string]queueTotals{"q1": {}},
		},
		{
			name: "pod update moves resources from pending to allocated",
			steps: []accountingStep{addQueue("q1"), addPodGroup("j1", "q1"), addPod(pending),
				func(_ *testing.T, sc *SchedulerCache) { sc.UpdatePod(pending, pendingStarted) }},
			want: map[string]queueTotals{"q1": {allocated: 1000, request: 1000}},
		},
		{
			name: "pod deletion removes its resources",
			steps: []accountingStep{addQueue("q1"), addPodGroup("j1", "q1"), addPod(pending), addPod(running),
				func(_ *testing.T, sc *SchedulerCache) { sc.DeletePod(running) }},
			want: map[string]queueTotals{"q1": {allocated: 0, request: 1000}},
		},
		{
			name:  "binding a pending task counts it as allocated",
			steps: []accountingStep{addQueue("q1"), addPodGroup("j1", "q1"), addPod(pending), bindPod(pending, false)},
			want:  map[string]queueTotals{"q1": {allocated: 1000, request: 1000}},
		},
		{
			name:  "failed bind rolls back to the pending totals",
			steps: []accountingStep{addQueue("q1"), addPodGroup("j1", "q1"), addPod(oversized), bindPod(oversized, true)},
			want:  map[string]queueTotals{"q1": {allocated: 0, request: 20000}},
		},
		{
			name:  "evicting a running task releases its resources",
			steps: []accountingStep{addQueue("q1"), addPodGroup("j1", "q1"), addPod(running), evictPod(running)},
			want:  map[string]queueTotals{"q1": {allocated: 0, request: 0}},
		},
		{
			name: "moving a job to another queue moves its totals",
			steps: []accountingStep{addQueue("q1"), addQueue("q2"), addPodGroup("j1", "q1"), addPod(pending), addPod(running),
				func(_ *testing.T, sc *SchedulerCache) {
					sc.UpdatePodGroupV1beta1(accountingPodGroup("j1", "q1", "1"), accountingPodGroup("j1", "q2", "2"))
				}},
			want: map[string]queueTotals{"q1": {}, "q2": {allocated: 2000, request: 3000}},
		},
		{
			name: "deleting the PodGroup removes the job from its queue",
			steps: []accountingStep{addQueue("q1"), addPodGroup("j1", "q1"), addPod(pending), addPod(running),
				func(_ *testing.T, sc *SchedulerCache) { sc.DeletePodGroupV1beta1(accountingPodGroup("j1", "q1", "1")) }},
			want: map[string]queueTotals{"q1": {}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runAccountingSteps(t, tt.steps, tt.want)
		})
	}
}

func TestSetQueueInfo(t *testing.T) {
	pending := accountingPod("pending", "", v1.PodPending, "1", "j1")
	running := accountingPod("running", "n1", v1.PodRunning, "2", "j1")
	jobSteps := []accountingStep{addPodGroup("j1", "q1"), addPod(pending), addPod(running)}

	tests := []struct {
		name  string
		steps []accountingStep
		want  map[string]queueTotals
	}{
		{
			name:  "queue added after its jobs picks up their totals",
			steps: append(append([]accountingStep{}, jobSteps...), addQueue("q1")),
			want:  map[string]queueTotals{"q1": {allocated: 2000, request: 3000}},
		},
		{
			name: "NamespaceQueue added after its jobs picks up their totals",
			steps: []accountingStep{enableNamespaceQueues, addPodGroup("j1", "namespace/nq"), addPod(pending), addPod(running),
				addNamespaceQueue("nq")},
			want: map[string]queueTotals{accountingNamespace + "/nq": {allocated: 2000, request: 3000}},
		},
		{
			name: "job moved to a queue that does not exist yet counts there once it is added",
			steps: append(append([]accountingStep{addQueue("q1")}, jobSteps...),
				func(_ *testing.T, sc *SchedulerCache) {
					sc.UpdatePodGroupV1beta1(accountingPodGroup("j1", "q1", "1"), accountingPodGroup("j1", "q2", "2"))
				},
				addQueue("q2")),
			want: map[string]queueTotals{"q1": {}, "q2": {allocated: 2000, request: 3000}},
		},
		{
			name: "tasks added while their queue is deleted count once it returns",
			steps: []accountingStep{addQueue("q1"), addPodGroup("j1", "q1"), addPod(pending),
				func(_ *testing.T, sc *SchedulerCache) { sc.DeleteQueueV1beta1(accountingQueue("q1", "1")) },
				addPod(running), addQueue("q1")},
			want: map[string]queueTotals{"q1": {allocated: 2000, request: 3000}},
		},
		{
			name: "queue status update keeps the totals",
			steps: append(append([]accountingStep{addQueue("q1")}, jobSteps...),
				func(_ *testing.T, sc *SchedulerCache) {
					updated := accountingQueue("q1", "2")
					updated.Status.State = schedulingv1beta1.QueueStateOpen
					sc.UpdateQueueV1beta1(accountingQueue("q1", "1"), updated)
				}),
			want: map[string]queueTotals{"q1": {allocated: 2000, request: 3000}},
		},
		{
			name: "deleted and recreated queue rebuilds its totals",
			steps: append(append([]accountingStep{addQueue("q1")}, jobSteps...),
				func(_ *testing.T, sc *SchedulerCache) { sc.DeleteQueueV1beta1(accountingQueue("q1", "1")) },
				addQueue("q1")),
			want: map[string]queueTotals{"q1": {allocated: 2000, request: 3000}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runAccountingSteps(t, tt.steps, tt.want)
		})
	}
}
