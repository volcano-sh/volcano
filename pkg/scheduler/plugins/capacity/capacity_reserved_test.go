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

package capacity

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// TestHierarchicalReservedTasksCountedAtAncestor verifies that gate-reserved tasks
// recorded under a leaf queue are accounted for when the capacity check evaluates an
// ancestor queue. Without subtree aggregation, two sibling leaves can each pass their
// own check and the shared parent check (which sees reserved == 0), letting the subtree
// be admitted past the parent's realCapability (queue-isolation / quota bypass).
//
// Topology: parent P (cpu 100) with leaves L1 and L2 (each cpu 100). L1 already holds a
// gate-reserved task consuming cpu 80. A new candidate is considered for L2.
func TestHierarchicalReservedTasksCountedAtAncestor(t *testing.T) {
	const (
		parentID api.QueueID = "p"
		leaf1ID  api.QueueID = "l1"
		leaf2ID  api.QueueID = "l2"
	)

	newAttr := func(id api.QueueID, ancestors []api.QueueID) *queueAttr {
		return &queueAttr{
			queueID:         id,
			name:            string(id),
			ancestors:       ancestors,
			children:        make(map[api.QueueID]*queueAttr),
			allocated:       api.EmptyResource(),
			realCapability:  &api.Resource{MilliCPU: 100000}, // cpu 100
			reservedSubtree: api.EmptyResource(),
		}
	}

	parentAttr := newAttr(parentID, nil)
	leaf1Attr := newAttr(leaf1ID, []api.QueueID{parentID})
	leaf2Attr := newAttr(leaf2ID, []api.QueueID{parentID})
	parentAttr.children[leaf1ID] = leaf1Attr
	parentAttr.children[leaf2ID] = leaf2Attr

	// L1 holds a gate-reserved task consuming cpu 80 (recorded under the leaf only).
	reservedTask := &api.TaskInfo{
		UID:    "reserved-on-l1",
		Name:   "reserved-on-l1",
		Resreq: &api.Resource{MilliCPU: 80000},
	}

	cp := &capacityPlugin{
		queueOpts: map[api.QueueID]*queueAttr{
			parentID: parentAttr,
			leaf1ID:  leaf1Attr,
			leaf2ID:  leaf2Attr,
		},
		queueGateReservedTasks: make(map[api.QueueID]map[api.TaskID]*api.TaskInfo),
	}
	// Reserve via the real API so the cache, reverse index, and the reservedSubtree
	// aggregate (leaf + ancestors) are all populated exactly as in a live session.
	cp.addTaskToReservedCache(leaf1ID, reservedTask)

	// The aggregate must have propagated L1's reservation up to the parent's subtree.
	if parentAttr.reservedSubtree.MilliCPU != 80000 {
		t.Fatalf("parent reservedSubtree = %v, want cpu 80", parentAttr.reservedSubtree.MilliCPU)
	}
	if leaf2Attr.reservedSubtree.MilliCPU != 0 {
		t.Fatalf("sibling L2 reservedSubtree = %v, want 0", leaf2Attr.reservedSubtree.MilliCPU)
	}

	ssn := &framework.Session{
		Queues: map[api.QueueID]*api.QueueInfo{
			parentID: {UID: parentID, Name: string(parentID)},
			leaf1ID:  {UID: leaf1ID, Name: string(leaf1ID)},
			leaf2ID:  {UID: leaf2ID, Name: string(leaf2ID)},
		},
	}

	// candidate requesting cpu 20: parent sees 80 (reserved) + 20 = 100 <= 100 -> admit.
	fitting := &api.TaskInfo{UID: "cand-fit", Name: "cand-fit", Resreq: &api.Resource{MilliCPU: 20000}}
	// candidate requesting cpu 30: parent sees 80 (reserved) + 30 = 110 > 100 -> reject.
	exceeding := &api.TaskInfo{UID: "cand-exceed", Name: "cand-exceed", Resreq: &api.Resource{MilliCPU: 30000}}

	leaf2Queue := ssn.Queues[leaf2ID]
	parentQueue := ssn.Queues[parentID]

	// Leaf L2 in isolation has no reserved tasks, so both candidates fit its own quota.
	// This is the state that previously let the exceeding candidate slip through, since
	// the parent check ignored L1's reserved tasks.
	if !cp.queueAllocatable(leaf2Queue, exceeding, false, false, false) {
		t.Fatalf("leaf L2 alone should admit the candidate (its own reserved is empty)")
	}

	// The ancestor must now reject the exceeding candidate because L1's reserved task is
	// part of the parent's subtree.
	if cp.queueAllocatable(parentQueue, exceeding, false, false, false) {
		t.Fatalf("parent P should reject candidate: 80 (L1 reserved) + 30 > 100 realCapability")
	}
	if !cp.queueAllocatable(parentQueue, fitting, false, false, false) {
		t.Fatalf("parent P should admit candidate: 80 (L1 reserved) + 20 == 100 realCapability")
	}

	// Full hierarchical path (leaf + every ancestor), the actual call site in AllocatableFn.
	if cp.checkQueueAllocatableHierarchically(ssn, leaf2Queue, exceeding) {
		t.Fatalf("hierarchical check should reject the exceeding candidate due to L1's reserved tasks at parent P")
	}
	if !cp.checkQueueAllocatableHierarchically(ssn, leaf2Queue, fitting) {
		t.Fatalf("hierarchical check should admit the fitting candidate")
	}
}

// newReservedTestPlugin builds a single parent P (cpu 100) with one leaf L (cpu 100),
// returning the plugin plus the queue infos, ready for reservation-cache exercises.
func newReservedTestPlugin() (*capacityPlugin, *api.QueueInfo, *api.QueueInfo) {
	const (
		parentID api.QueueID = "p"
		leafID   api.QueueID = "l"
	)
	newAttr := func(id api.QueueID, ancestors []api.QueueID) *queueAttr {
		return &queueAttr{
			queueID:         id,
			name:            string(id),
			ancestors:       ancestors,
			children:        make(map[api.QueueID]*queueAttr),
			allocated:       api.EmptyResource(),
			realCapability:  &api.Resource{MilliCPU: 100000},
			reservedSubtree: api.EmptyResource(),
		}
	}
	parentAttr := newAttr(parentID, nil)
	leafAttr := newAttr(leafID, []api.QueueID{parentID})
	parentAttr.children[leafID] = leafAttr

	cp := &capacityPlugin{
		queueOpts: map[api.QueueID]*queueAttr{
			parentID: parentAttr,
			leafID:   leafAttr,
		},
		queueGateReservedTasks: make(map[api.QueueID]map[api.TaskID]*api.TaskInfo),
	}
	parentQueue := &api.QueueInfo{UID: parentID, Name: string(parentID)}
	leafQueue := &api.QueueInfo{UID: leafID, Name: string(leafID)}
	return cp, parentQueue, leafQueue
}

// TestReservedCandidateNotDoubleCounted verifies that a task already present in the
// reserved cache (e.g. admitted in a prior cycle and rebuilt at session open) is not
// counted twice when it is itself re-evaluated as the allocation candidate.
func TestReservedCandidateNotDoubleCounted(t *testing.T) {
	cp, parentQueue, leafQueue := newReservedTestPlugin()

	// A cpu-80 task is reserved on the leaf and is also the candidate being re-evaluated.
	task := &api.TaskInfo{UID: "t", Name: "t", Resreq: &api.Resource{MilliCPU: 80000}}
	cp.addTaskToReservedCache(leafQueue.UID, task)

	// futureUsed = allocated(0) + reserved(80 - candidate 80) + candidate(80) = 80 <= 100.
	// Without candidate exclusion this would be 160 > 100 and wrongly reject, livelocking
	// the task (it can never allocate against its own reservation).
	// Membership is resolved against the candidate's own leaf queue, exactly as
	// checkQueueAllocatableHierarchically does before walking the chain.
	isCandidateReserved := cp.isTaskReserved(leafQueue.UID, task.UID)
	if !isCandidateReserved {
		t.Fatalf("task should be reserved on its leaf after addTaskToReservedCache")
	}
	if !cp.queueAllocatable(leafQueue, task, false, false, isCandidateReserved) {
		t.Fatalf("leaf should admit the reserved candidate (its own reservation must not be double counted)")
	}
	if !cp.queueAllocatable(parentQueue, task, false, false, isCandidateReserved) {
		t.Fatalf("ancestor should admit the reserved candidate (its own reservation must not be double counted)")
	}
}

// TestNonHierarchicalReservedAccounting verifies that reserved resources are accounted
// correctly when hierarchy is disabled. A queue then has no ancestors, so the shared
// reservedSubtree aggregate holds exactly that queue's own reservations and the capacity
// decisions match the hierarchical path without a separate code path.
func TestNonHierarchicalReservedAccounting(t *testing.T) {
	const queueID api.QueueID = "q"
	attr := &queueAttr{
		queueID:         queueID,
		name:            string(queueID),
		allocated:       api.EmptyResource(),
		realCapability:  &api.Resource{MilliCPU: 100000}, // cpu 100
		reservedSubtree: api.EmptyResource(),
	}
	cp := &capacityPlugin{
		queueOpts:              map[api.QueueID]*queueAttr{queueID: attr},
		queueGateReservedTasks: make(map[api.QueueID]map[api.TaskID]*api.TaskInfo),
	}
	queue := &api.QueueInfo{UID: queueID, Name: string(queueID)}

	reservedTask := &api.TaskInfo{UID: "r", Name: "r", Resreq: &api.Resource{MilliCPU: 80000}}
	cp.addTaskToReservedCache(queueID, reservedTask)

	// With no ancestors the aggregate holds only this queue's own reservation.
	if got := attr.reservedSubtree.MilliCPU; got != 80000 {
		t.Fatalf("reservedSubtree = %v without hierarchy, want cpu 80", got)
	}

	// The reservation is accounted in the capacity check: 80 + 30 > 100 -> reject.
	exceeding := &api.TaskInfo{UID: "c1", Name: "c1", Resreq: &api.Resource{MilliCPU: 30000}}
	if cp.queueAllocatable(queue, exceeding, false, false, false) {
		t.Fatalf("queue should reject candidate: 80 (reserved) + 30 > 100")
	}
	// 80 + 20 == 100 -> admit.
	fitting := &api.TaskInfo{UID: "c2", Name: "c2", Resreq: &api.Resource{MilliCPU: 20000}}
	if !cp.queueAllocatable(queue, fitting, false, false, false) {
		t.Fatalf("queue should admit candidate: 80 (reserved) + 20 == 100")
	}
}

// TestAddTaskToReservedCacheIdempotent verifies that admitting the same task multiple
// times within a cycle (multiple node attempts / rollback re-add) increments the
// reservedSubtree aggregate exactly once.
func TestAddTaskToReservedCacheIdempotent(t *testing.T) {
	cp, parentQueue, leafQueue := newReservedTestPlugin()
	task := &api.TaskInfo{UID: "t", Name: "t", Resreq: &api.Resource{MilliCPU: 30000}}

	cp.addTaskToReservedCache(leafQueue.UID, task)
	cp.addTaskToReservedCache(leafQueue.UID, task)
	cp.addTaskToReservedCache(leafQueue.UID, task)

	if got := cp.queueOpts[leafQueue.UID].reservedSubtree.MilliCPU; got != 30000 {
		t.Fatalf("leaf reservedSubtree = %v after repeated adds, want cpu 30", got)
	}
	if got := cp.queueOpts[parentQueue.UID].reservedSubtree.MilliCPU; got != 30000 {
		t.Fatalf("parent reservedSubtree = %v after repeated adds, want cpu 30", got)
	}
	if got := len(cp.queueGateReservedTasks[leafQueue.UID]); got != 1 {
		t.Fatalf("task map has %d entries, want 1", got)
	}
}

// TestReservedCacheAddRemoveRoundTrip verifies that add -> remove -> add (the
// admit / allocate / rollback lifecycle) leaves the leaf and every ancestor aggregate
// holding exactly one reservation, and that a full removal zeroes the aggregate.
func TestReservedCacheAddRemoveRoundTrip(t *testing.T) {
	cp, parentQueue, leafQueue := newReservedTestPlugin()
	task := &api.TaskInfo{UID: "t", Name: "t", Resreq: &api.Resource{MilliCPU: 40000}}

	cp.addTaskToReservedCache(leafQueue.UID, task)          // admit
	cp.removeTaskFromReservedCache(leafQueue.UID, task.UID) // allocate
	cp.addTaskToReservedCache(leafQueue.UID, task)          // rollback re-add

	if got := cp.queueOpts[leafQueue.UID].reservedSubtree.MilliCPU; got != 40000 {
		t.Fatalf("leaf reservedSubtree = %v after round trip, want cpu 40", got)
	}
	if got := cp.queueOpts[parentQueue.UID].reservedSubtree.MilliCPU; got != 40000 {
		t.Fatalf("parent reservedSubtree = %v after round trip, want cpu 40", got)
	}

	cp.removeTaskFromReservedCache(leafQueue.UID, task.UID)
	if got := cp.queueOpts[leafQueue.UID].reservedSubtree.MilliCPU; got != 0 {
		t.Fatalf("leaf reservedSubtree = %v after final remove, want 0", got)
	}
	if got := cp.queueOpts[parentQueue.UID].reservedSubtree.MilliCPU; got != 0 {
		t.Fatalf("parent reservedSubtree = %v after final remove, want 0", got)
	}
	if _, ok := cp.queueGateReservedTasks[leafQueue.UID][task.UID]; ok {
		t.Fatalf("reserved cache still references removed task")
	}
	if _, ok := cp.queueGateReservedTasks[leafQueue.UID]; ok {
		t.Fatalf("empty leaf bucket should have been pruned from the cache")
	}
}

// TestReservedExclusionDoesNotPanicOnUnderflow guards Blocker 1: if membership says the
// candidate is reserved but the reserved-subtree amount does not cover it (an invariant
// violation that a consistent snapshot prevents, but which must never crash the scheduler),
// queueAllocatableWithReserved must skip the exclusion instead of panicking in Resource.Sub.
func TestReservedExclusionDoesNotPanicOnUnderflow(t *testing.T) {
	attr := &queueAttr{
		queueID:         "l",
		name:            "l",
		allocated:       api.EmptyResource(),
		realCapability:  &api.Resource{MilliCPU: 100000}, // cpu 100
		reservedSubtree: &api.Resource{MilliCPU: 10000},  // cpu 10 (does not cover candidate)
	}
	cp := &capacityPlugin{
		queueOpts: map[api.QueueID]*queueAttr{"l": attr},
	}
	queue := &api.QueueInfo{UID: "l", Name: "l"}
	candidate := &api.TaskInfo{UID: "c", Name: "c", Resreq: &api.Resource{MilliCPU: 50000}} // cpu 50

	// Membership claims the candidate is reserved, but reservedSubtree (cpu 10) < candidate
	// (cpu 50). Exclusion must be skipped (no panic). futureUsed = 0 + 10 + 50 = 60 <= 100.
	if !cp.queueAllocatableWithReserved(attr, candidate, queue, false, false, true) {
		t.Fatalf("expected allocatable with conservative over-count (60 <= 100) and no panic")
	}
}

// TestCapacityStateCloneCarriesFrozenMembership verifies that Clone propagates the frozen
// membership answer and the task it belongs to. A clone that dropped either would make
// preempt simulation treat a reserved candidate as unreserved and double count it.
func TestCapacityStateCloneCarriesFrozenMembership(t *testing.T) {
	state := &capacityState{
		queueAttrs:          map[api.QueueID]*queueAttr{},
		candidateTaskUID:    "t1",
		isCandidateReserved: true,
	}

	cloned, ok := state.Clone().(*capacityState)
	if !ok {
		t.Fatalf("Clone did not return a *capacityState")
	}
	if cloned.candidateTaskUID != "t1" {
		t.Fatalf("candidateTaskUID = %q after Clone, want t1", cloned.candidateTaskUID)
	}
	if !cloned.isCandidateReserved {
		t.Fatalf("isCandidateReserved lost in Clone")
	}

	// The queue attributes must still be deep copied.
	state.queueAttrs["q"] = &queueAttr{queueID: "q"}
	if _, ok := cloned.queueAttrs["q"]; ok {
		t.Fatalf("clone observed a queue added to the original after cloning")
	}
}

// TestSimulateRemoveFreesAllocatedVictim guards preemption: the queue allocation gate annotation
// persists on running pods, so a victim still carries it. SimulateRemoveTaskFn must free the
// victim's capacity for the preemptor and must not re-count it as reserved.
func TestSimulateRemoveFreesAllocatedVictim(t *testing.T) {
	options.Default()
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.SchedulingGatesQueueAdmission, true)

	plugins := map[string]framework.PluginBuilder{PluginName: New}
	uthelper.RegisterPlugins(plugins)
	defer framework.CleanupPluginBuilders()

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{Name: PluginName, EnabledAllocatable: &trueValue, EnabledHierarchy: &trueValue, EnabledPredicate: &trueValue},
			},
		},
	}

	n1 := util.BuildNode("n1", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil)
	cpu4 := api.BuildResourceList("4", "4Gi")

	// Running victim in leaf q11, carrying the persistent gate annotation.
	victim := util.BuildPod("ns1", "victim", "n1", corev1.PodRunning, cpu4, "pg-victim", map[string]string{}, map[string]string{})
	victim.Annotations[schedulingv1beta1.QueueAllocationGateKey] = "true"
	pgVictim := util.BuildPodGroup("pg-victim", "ns1", "q11", 1, nil, schedulingv1beta1.PodGroupRunning)

	// Pending preemptor in the same leaf, no annotation (not gate-reserved).
	preemptorPod := util.BuildPod("ns1", "preemptor", "", corev1.PodPending, cpu4, "pg-preemptor", map[string]string{}, map[string]string{})
	pgPreemptor := util.BuildPodGroup("pg-preemptor", "ns1", "q11", 1, nil, schedulingv1beta1.PodGroupInqueue)

	root := buildQueueWithParents("root", "", nil, cpu4)
	q1 := buildQueueWithParents("q1", "root", nil, cpu4)
	q11 := buildQueueWithParents("q11", "q1", nil, cpu4)

	binder := util.NewFakeBinder(1)
	evictor := util.NewFakeEvictor(0)
	statusUpdater := &util.FakeStatusUpdater{}
	stop := make(chan struct{})
	defer close(stop)

	sc := cache.NewCustomMockSchedulerCache("test-capacity-preempt", binder, evictor, statusUpdater, nil, nil)
	sc.Run(stop)
	sc.AddOrUpdateNode(n1)
	sc.AddPod(victim)
	sc.AddPod(preemptorPod)
	sc.AddPodGroupV1beta1(pgVictim)
	sc.AddPodGroupV1beta1(pgPreemptor)
	sc.AddQueueV1beta1(root)
	sc.AddQueueV1beta1(q1)
	sc.AddQueueV1beta1(q11)

	ssn := framework.OpenSession(sc, tiers, nil)
	defer framework.CloseSession(ssn)

	findTask := func(name string) *api.TaskInfo {
		for _, job := range ssn.Jobs {
			for _, task := range job.Tasks {
				if task.Name == name {
					return task
				}
			}
		}
		return nil
	}
	victimTask := findTask("victim")
	preemptorTask := findTask("preemptor")
	if victimTask == nil || preemptorTask == nil {
		t.Fatalf("victim or preemptor task not found in session")
	}

	if err := ssn.PrePredicateFn(preemptorTask); err != nil {
		t.Fatalf("PrePredicateFn: %v", err)
	}
	cycleState := ssn.GetCycleState(preemptorTask.UID)
	ctx := context.TODO()

	if err := ssn.SimulateRemoveTaskFn(ctx, cycleState, preemptorTask, victimTask, ssn.Nodes["n1"]); err != nil {
		t.Fatalf("SimulateRemoveTaskFn: %v", err)
	}

	// Removing the cpu-4 victim empties the leaf and its ancestors, so the cpu-4 preemptor fits.
	// If the victim were re-counted as reserved, the ancestor check would see 0+4+4 > 4 and reject.
	if !ssn.SimulateAllocatableFn(ctx, cycleState, ssn.Queues["q11"], preemptorTask) {
		t.Fatalf("preemptor must be allocatable after removing the annotated victim (reserved double-counting regression)")
	}
}
