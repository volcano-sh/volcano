/*
Copyright 2023 The Volcano Authors.

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

package pdb

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	pdbPolicy "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const LabelName = "volcano.sh/job-name"

func TestPreemptableAndReclaimableFn(t *testing.T) {
	// 1. init task1 and task2
	task1 := api.NewTaskInfo(makePod("test-pod1", map[string]string{LabelName: "job-1"}))
	task2 := api.NewTaskInfo(makePod("test-pod2", map[string]string{LabelName: "job-1"}))
	// 2. init tests
	// (a. test without pdb
	// (b. test with pdb, but no tasks can be evicted
	// (c. test with pdb, but only test1 can be evicted
	tests := []struct {
		name             string
		pdbs             []*pdbPolicy.PodDisruptionBudget
		preemptees       []*api.TaskInfo
		expectVictims    []*api.TaskInfo
		expectVictimsMap map[*api.TaskInfo]bool
	}{{
		name:             "test without pdbs",
		pdbs:             nil,
		preemptees:       []*api.TaskInfo{task1, task2},
		expectVictims:    []*api.TaskInfo{task1, task2},
		expectVictimsMap: map[*api.TaskInfo]bool{task1: true, task2: true},
	}, {
		name: "test with pdbs(can evict 0 pod)",
		pdbs: []*pdbPolicy.PodDisruptionBudget{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pdb",
					Namespace: "default",
				},
				Spec:   pdbPolicy.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{LabelName: "job-1"}}},
				Status: pdbPolicy.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
			},
		},
		preemptees:       []*api.TaskInfo{task1, task2},
		expectVictims:    nil,
		expectVictimsMap: make(map[*api.TaskInfo]bool),
	}, {
		name: "test with pdbs(can evict 1 pod)",
		pdbs: []*pdbPolicy.PodDisruptionBudget{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pdb",
					Namespace: "default",
				},
				Spec:   pdbPolicy.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{LabelName: "job-1"}}},
				Status: pdbPolicy.PodDisruptionBudgetStatus{DisruptionsAllowed: 1},
			},
		},
		preemptees:       []*api.TaskInfo{task1, task2},
		expectVictims:    []*api.TaskInfo{task1},
		expectVictimsMap: map[*api.TaskInfo]bool{task1: true},
	},
	}

	// 3. set the test plugin name and fns
	enabled := true
	pluginOption := conf.PluginOption{
		Name:               PluginName,
		EnabledPreemptable: &enabled,
		EnabledReclaimable: &enabled,
		EnabledVictim:      &enabled,
	}

	// 4. range every test
	for _, test := range tests {

		// (a. set the fake informerFactory and add pdbs to pdb informer
		client := fake.NewSimpleClientset()
		informerFactory := informers.NewSharedInformerFactory(client, 0)
		informer := informerFactory.Policy().V1().PodDisruptionBudgets().Informer()
		for _, pdb := range test.pdbs {
			informer.GetStore().Add(pdb)
		}

		// (b. set the SchedulerCache
		schedulerCache := cache.NewDefaultMockSchedulerCache("volcano")
		schedulerCache.SetSharedInformerFactory(informerFactory)

		// (c. set the Session with preempt, reclaim and shuffle action
		ssn := framework.OpenSession(schedulerCache, []conf.Tier{
			{
				Plugins: []conf.PluginOption{pluginOption},
			},
		},
			[]conf.Configuration{
				{
					Name: "preempt",
				},
				{
					Name: "reclaim",
				},
				{
					Name: "shuffle",
				},
			},
		)

		// (d. register actions
		plugin := &pdbPlugin{}
		plugin.OnSessionOpen(ssn) // register preempt fn

		// (e. test the Preemptable in pdb plugin
		victims := ssn.Preemptable(&api.TaskInfo{}, test.preemptees)
		if !equality.Semantic.DeepEqual(test.expectVictims, victims) {
			t.Errorf("Test of preemptable: test name: %s, expected: %v, got %v ", test.name, test.expectVictims, victims)
		}

		// (f. test the Reclaimable in pdb plugin
		victims = ssn.Reclaimable(&api.TaskInfo{}, test.preemptees)
		if !equality.Semantic.DeepEqual(test.expectVictims, victims) {
			t.Errorf("Test of reclaimable: test name: %s, expected: %v, got %v ", test.name, test.expectVictims, victims)
		}

		// (g. test the VictimTasks in pdb plugin
		victimsMap := ssn.VictimTasks(test.preemptees)
		if !equality.Semantic.DeepEqual(test.expectVictimsMap, victimsMap) {
			t.Errorf("Test of victimTasks: test name %s, expected: %v, got %v ", test.name, test.expectVictims, victims)
		}

		// (h. test the UnifiedEvictable used by gangpreempt and gangreclaim
		victims = ssn.UnifiedEvictable(&api.EvictionContext{Kind: api.EvictionKindGangPreempt}, test.preemptees)
		if !equality.Semantic.DeepEqual(test.expectVictims, victims) {
			t.Errorf("Test of unifiedEvictable: test name: %s, expected: %v, got %v ", test.name, test.expectVictims, victims)
		}
	}
}

// The scheduler deletes evicted pods directly, so the PDB status keeps the
// old DisruptionsAllowed for the rest of the session. Evictions already made
// in the session must spend the budget, or each later Preemptable call can
// evict another pod under the same PDB.
func TestBudgetSpentByEvictionsInSession(t *testing.T) {
	jobID := api.JobID("default/job-1")
	var tasks []*api.TaskInfo
	for _, name := range []string{"test-pod1", "test-pod2", "test-pod3"} {
		pod := makePod(name, map[string]string{LabelName: "job-1"})
		pod.UID = types.UID(name)
		pod.Status.Phase = v1.PodRunning
		pod.Spec.NodeName = "n1"
		task := api.NewTaskInfo(pod)
		task.Job = jobID
		tasks = append(tasks, task)
	}

	client := fake.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(client, 0)
	err := informerFactory.Policy().V1().PodDisruptionBudgets().Informer().GetStore().Add(&pdbPolicy.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{Name: "test-pdb", Namespace: "default"},
		Spec:       pdbPolicy.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{LabelName: "job-1"}}},
		Status:     pdbPolicy.PodDisruptionBudgetStatus{DisruptionsAllowed: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	schedulerCache := cache.NewDefaultMockSchedulerCache("volcano")
	schedulerCache.SetSharedInformerFactory(informerFactory)

	enabled := true
	ssn := framework.OpenSession(schedulerCache, []conf.Tier{{Plugins: []conf.PluginOption{{
		Name:               PluginName,
		EnabledPreemptable: &enabled,
		EnabledReclaimable: &enabled,
		EnabledVictim:      &enabled,
	}}}}, nil)
	origJobs := ssn.Jobs
	defer func() {
		ssn.Jobs = origJobs
		framework.CloseSession(ssn)
	}()
	ssn.Jobs = map[api.JobID]*api.JobInfo{jobID: api.NewJobInfo(jobID, tasks...)}
	(&pdbPlugin{}).OnSessionOpen(ssn)

	victimNames := func(victims []*api.TaskInfo) []string {
		names := []string{}
		for _, v := range victims {
			names = append(names, v.Name)
		}
		return names
	}
	gangCtx := &api.EvictionContext{Kind: api.EvictionKindGangPreempt}

	// One disruption is allowed, so only one of the candidates is a victim.
	if got := victimNames(ssn.Preemptable(&api.TaskInfo{}, tasks[:2])); !equality.Semantic.DeepEqual(got, []string{"test-pod1"}) {
		t.Fatalf("before eviction: expected [test-pod1], got %v", got)
	}

	stmt := framework.NewStatement(ssn)
	stmt.Evict(tasks[0], "preempt")

	// test-pod1 used the budget, so a later call must not offer another pod.
	rest := tasks[1:]
	if got := victimNames(ssn.Preemptable(&api.TaskInfo{}, rest)); len(got) != 0 {
		t.Errorf("Preemptable after eviction: expected no victims, got %v", got)
	}
	if got := victimNames(ssn.Reclaimable(&api.TaskInfo{}, rest)); len(got) != 0 {
		t.Errorf("Reclaimable after eviction: expected no victims, got %v", got)
	}
	if got := victimNames(ssn.UnifiedEvictable(gangCtx, rest)); len(got) != 0 {
		t.Errorf("UnifiedEvictable after eviction: expected no victims, got %v", got)
	}
	if got := ssn.VictimTasks(rest); len(got) != 0 {
		t.Errorf("VictimTasks after eviction: expected no victims, got %v", got)
	}
	// The pod being evicted is not charged twice.
	if got := victimNames(ssn.Preemptable(&api.TaskInfo{}, tasks[:1])); !equality.Semantic.DeepEqual(got, []string{"test-pod1"}) {
		t.Errorf("evicted pod: expected [test-pod1], got %v", got)
	}

	// Discarding the eviction gives the budget back.
	stmt.Discard()
	if got := victimNames(ssn.Preemptable(&api.TaskInfo{}, rest)); !equality.Semantic.DeepEqual(got, []string{"test-pod2"}) {
		t.Errorf("after discard: expected [test-pod2], got %v", got)
	}
}

func makePod(name string, labels map[string]string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			Labels:    labels,
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{},
			},
		},
	}
}
