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
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	pdbPolicy "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		schedulerCache := &cache.SchedulerCache{}
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
		if !reflect.DeepEqual(test.expectVictims, victims) {
			t.Errorf("Test of preemptable: test name: %s, expected: %v, got %v ", test.name, test.expectVictims, victims)
		}

		// (f. test the Reclaimable in pdb plugin
		victims = ssn.Reclaimable(&api.TaskInfo{}, test.preemptees)
		if !reflect.DeepEqual(test.expectVictims, victims) {
			t.Errorf("Test of reclaimable: test name: %s, expected: %v, got %v ", test.name, test.expectVictims, victims)
		}

		// (g. test the VictimTasks in pdb plugin
		victimsMap := ssn.VictimTasks(test.preemptees)
		if !reflect.DeepEqual(test.expectVictimsMap, victimsMap) {
			t.Errorf("Test of victimTasks: test name %s, expected: %v, got %v ", test.name, test.expectVictims, victims)
		}
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
