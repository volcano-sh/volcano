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
	"math"
	"math/rand"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apitypes "k8s.io/apimachinery/pkg/types"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/util/workqueue"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

const epsilon = 1e-6

// newAccountingTestCache builds a minimal SchedulerCache for exercising the
// queue accounting handlers without informers or clients.
func newAccountingTestCache() *SchedulerCache {
	return &SchedulerCache{
		Jobs:   map[schedulingapi.JobID]*schedulingapi.JobInfo{},
		Queues: map[schedulingapi.QueueID]*schedulingapi.QueueInfo{},
		Nodes:  map[string]*schedulingapi.NodeInfo{},
		DeletedJobs: workqueue.NewTypedRateLimitingQueue[string](
			workqueue.DefaultTypedControllerRateLimiter[string]()),
		defaultQueue: "default",
	}
}

func enableQueueAccounting(t *testing.T) {
	t.Helper()
	if err := utilfeature.DefaultMutableFeatureGate.Set("CacheQueueAccounting=true"); err != nil {
		t.Fatalf("failed to enable CacheQueueAccounting: %v", err)
	}
	t.Cleanup(func() {
		if err := utilfeature.DefaultMutableFeatureGate.Set("CacheQueueAccounting=false"); err != nil {
			t.Fatalf("failed to disable CacheQueueAccounting: %v", err)
		}
	})
}

func addQueueToCache(sc *SchedulerCache, name string) {
	sc.addQueue(&scheduling.Queue{ObjectMeta: metav1.ObjectMeta{Name: name}})
}

func setJobPodGroup(sc *SchedulerCache, ns, pgName, queue string) {
	pg := &schedulingapi.PodGroup{
		PodGroup: scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{Name: pgName, Namespace: ns},
			Spec:       scheduling.PodGroupSpec{Queue: queue, MinMember: 1},
		},
		Version: schedulingapi.PodGroupVersionV1Beta1,
	}
	if err := sc.setPodGroup(pg); err != nil {
		panic(err)
	}
}

// newTask builds a TaskInfo tied to podgroup pgName with the given status/resources.
func newTask(ns, name, pgName string, status schedulingapi.TaskStatus, cpu, mem string) *schedulingapi.TaskInfo {
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:         apitypes.UID(fmt.Sprintf("%s-%s", ns, name)),
			Name:        name,
			Namespace:   ns,
			Annotations: map[string]string{v1beta1.KubeGroupNameAnnotationKey: pgName},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Resources: v1.ResourceRequirements{Requests: schedulingapi.BuildResourceList(cpu, mem)}},
			},
		},
	}
	ti := schedulingapi.NewTaskInfo(pod)
	ti.Status = status
	return ti
}

// assertIncrementalMatchesReconcile snapshots the incremental queue totals, runs
// a full reconcile as ground truth, and checks they match.
func assertIncrementalMatchesReconcile(t *testing.T, sc *SchedulerCache, step string) {
	t.Helper()

	type agg struct{ alloc, req *schedulingapi.Resource }
	incremental := map[schedulingapi.QueueID]agg{}
	for qid, q := range sc.Queues {
		incremental[qid] = agg{alloc: q.Allocated.Clone(), req: q.Request.Clone()}
	}

	// Rebuild from scratch as the ground truth.
	sc.reconcileQueueAllocationsLocked()

	for qid, q := range sc.Queues {
		inc := incremental[qid]
		if !resourcesEqual(inc.alloc, q.Allocated) {
			t.Fatalf("[%s] queue %s allocated mismatch: incremental=%v reconcile=%v", step, qid, inc.alloc, q.Allocated)
		}
		if !resourcesEqual(inc.req, q.Request) {
			t.Fatalf("[%s] queue %s request mismatch: incremental=%v reconcile=%v", step, qid, inc.req, q.Request)
		}
	}
}

func resourcesEqual(a, b *schedulingapi.Resource) bool {
	if a == nil {
		a = schedulingapi.EmptyResource()
	}
	if b == nil {
		b = schedulingapi.EmptyResource()
	}
	if math.Abs(a.MilliCPU-b.MilliCPU) > epsilon || math.Abs(a.Memory-b.Memory) > epsilon {
		return false
	}
	names := map[v1.ResourceName]struct{}{}
	for n := range a.ScalarResources {
		names[n] = struct{}{}
	}
	for n := range b.ScalarResources {
		names[n] = struct{}{}
	}
	for n := range names {
		if math.Abs(a.ScalarResources[n]-b.ScalarResources[n]) > epsilon {
			return false
		}
	}
	return true
}

// TestQueueAllocation_Deterministic walks the interesting orderings: podgroup
// before tasks, tasks before podgroup, queue reassignment, queue added after
// jobs, task deletion and podgroup deletion.
func TestQueueAllocation_Deterministic(t *testing.T) {
	enableQueueAccounting(t)
	sc := newAccountingTestCache()
	ns := "default"

	addQueueToCache(sc, "q1")
	addQueueToCache(sc, "q2")

	// Case 1: PodGroup assigned before tasks are added.
	setJobPodGroup(sc, ns, "pg1", "q1")
	sc.addTask(newTask(ns, "pg1-a", "pg1", schedulingapi.Running, "1000m", "1Gi"))
	sc.addTask(newTask(ns, "pg1-b", "pg1", schedulingapi.Pending, "2000m", "2Gi"))
	assertIncrementalMatchesReconcile(t, sc, "pg1 running+pending")

	// Case 2: tasks added before the PodGroup resolves the queue.
	sc.addTask(newTask(ns, "pg2-a", "pg2", schedulingapi.Running, "500m", "512Mi"))
	sc.addTask(newTask(ns, "pg2-b", "pg2", schedulingapi.Bound, "500m", "512Mi"))
	// Not counted anywhere yet since there is no PodGroup, so reconcile is empty too.
	assertIncrementalMatchesReconcile(t, sc, "pg2 tasks before podgroup")
	setJobPodGroup(sc, ns, "pg2", "q1")
	assertIncrementalMatchesReconcile(t, sc, "pg2 podgroup resolves queue")

	// Case 3: queue reassignment moves the whole job aggregate.
	setJobPodGroup(sc, ns, "pg2", "q2")
	assertIncrementalMatchesReconcile(t, sc, "pg2 moved q1->q2")

	// Case 4: task deletion.
	del := newTask(ns, "pg1-a", "pg1", schedulingapi.Running, "1000m", "1Gi")
	sc.deleteTask(del)
	assertIncrementalMatchesReconcile(t, sc, "pg1-a deleted")

	// Case 5: queue created after jobs already reference it.
	setJobPodGroup(sc, ns, "pg3", "q3") // q3 not in cache yet
	sc.addTask(newTask(ns, "pg3-a", "pg3", schedulingapi.Running, "4000m", "4Gi"))
	addQueueToCache(sc, "q3") // addQueue rebuilds from existing jobs
	assertIncrementalMatchesReconcile(t, sc, "q3 added after jobs")

	// Case 6: podgroup deletion removes the job's contribution.
	if err := sc.deletePodGroup(schedulingapi.JobID(ns + "/pg2")); err != nil {
		t.Fatalf("deletePodGroup failed: %v", err)
	}
	assertIncrementalMatchesReconcile(t, sc, "pg2 podgroup deleted")
}

// TestQueueAllocation_Churn applies a random sequence of cache mutations and
// checks after every step that the incremental per queue totals equal a full
// recompute. This is the main correctness proof for the delta logic.
func TestQueueAllocation_Churn(t *testing.T) {
	enableQueueAccounting(t)
	sc := newAccountingTestCache()
	ns := "default"

	queues := []string{"q1", "q2", "q3"}
	for _, q := range queues {
		addQueueToCache(sc, q)
	}

	statuses := []schedulingapi.TaskStatus{
		schedulingapi.Running, schedulingapi.Bound, schedulingapi.Allocated,
		schedulingapi.Pending, schedulingapi.Pipelined,
	}

	rng := rand.New(rand.NewSource(42))
	// live tracks the tasks currently added so we can delete real ones.
	type liveTask struct {
		pg   string
		task *schedulingapi.TaskInfo
	}
	var live []liveTask
	pgQueue := map[string]string{} // pg to assigned queue

	const steps = 2000
	uid := 0
	for i := 0; i < steps; i++ {
		switch rng.Intn(5) {
		case 0, 1: // add a task
			uid++
			pg := fmt.Sprintf("pg%d", rng.Intn(6))
			st := statuses[rng.Intn(len(statuses))]
			cpu := fmt.Sprintf("%dm", 100*(1+rng.Intn(20)))
			mem := fmt.Sprintf("%dMi", 128*(1+rng.Intn(8)))
			ti := newTask(ns, fmt.Sprintf("t%d", uid), pg, st, cpu, mem)
			sc.addTask(ti)
			live = append(live, liveTask{pg: pg, task: ti})
		case 2: // delete a random task
			if len(live) > 0 {
				idx := rng.Intn(len(live))
				sc.deleteTask(live[idx].task)
				live = append(live[:idx], live[idx+1:]...)
			}
		case 3: // reassign a podgroup to a random queue
			pg := fmt.Sprintf("pg%d", rng.Intn(6))
			q := queues[rng.Intn(len(queues))]
			setJobPodGroup(sc, ns, pg, q)
			pgQueue[pg] = q
		case 4: // delete a podgroup that exists
			pg := fmt.Sprintf("pg%d", rng.Intn(6))
			jobID := schedulingapi.JobID(ns + "/" + pg)
			if _, ok := sc.Jobs[jobID]; ok {
				_ = sc.deletePodGroup(jobID)
				delete(pgQueue, pg)
				// Drop live tasks of this pg since its job is gone.
				filtered := live[:0]
				for _, lt := range live {
					if lt.pg != pg {
						filtered = append(filtered, lt)
					}
				}
				live = filtered
			}
		}

		assertIncrementalMatchesReconcile(t, sc, fmt.Sprintf("churn step %d", i))
	}
}
