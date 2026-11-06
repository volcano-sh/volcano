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

package rescheduling

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func buildTestPod(name, namespace string, uid types.UID) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       uid,
		},
	}
}

func buildTestTask(pod *v1.Pod, milliCPU float64, memory float64) *api.TaskInfo {
	return &api.TaskInfo{
		UID:       api.TaskID(pod.UID),
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Pod:       pod,
		Resreq: &api.Resource{
			MilliCPU: milliCPU,
			Memory:   memory,
		},
	}
}

func buildTestNodeUtilization(name string, cpuCapacity, memCapacity string, cpuUtil, memUtil float64, pods []*v1.Pod) *NodeUtilization {
	return &NodeUtilization{
		nodeInfo: &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Status: v1.NodeStatus{
				Capacity: v1.ResourceList{
					v1.ResourceCPU:    resource.MustParse(cpuCapacity),
					v1.ResourceMemory: resource.MustParse(memCapacity),
				},
			},
		},
		utilization: map[v1.ResourceName]float64{
			v1.ResourceCPU:    cpuUtil,
			v1.ResourceMemory: memUtil,
		},
		pods: pods,
	}
}

func TestEvictNamespaceCollision(t *testing.T) {
	// Source node hosts team-a/worker-0
	podTeamA := buildTestPod("worker-0", "team-a", types.UID("uid-team-a"))
	podTeamB := buildTestPod("worker-0", "team-b", types.UID("uid-team-b"))

	nodeUtil := buildTestNodeUtilization("source-node", "4000m", "8000Mi", 80.0, 80.0, []*v1.Pod{podTeamA})

	// Eligible tasks have team-b/worker-0 placed FIRST before team-a/worker-0
	taskTeamB := buildTestTask(podTeamB, 1000, 1000*1024*1024)
	taskTeamA := buildTestTask(podTeamA, 1000, 1000*1024*1024)
	tasks := []*api.TaskInfo{taskTeamB, taskTeamA}

	totalAllocatable := map[v1.ResourceName]*resource.Quantity{
		v1.ResourceCPU:    resource.NewMilliQuantity(2000, resource.DecimalSI),
		v1.ResourceMemory: resource.NewQuantity(2000*1024*1024, resource.BinarySI),
	}

	continueFn := func(usage *NodeUtilization, totalAllocatableResource map[v1.ResourceName]*resource.Quantity, config interface{}) bool {
		return true
	}

	victims := evict(nodeUtil.pods, nodeUtil, totalAllocatable, continueFn, tasks, nil)

	if len(victims) != 1 {
		t.Fatalf("expected 1 victim, got %d", len(victims))
	}
	if victims[0].Pod.UID != types.UID("uid-team-a") {
		t.Fatalf("expected victim UID %q (team-a/worker-0), got %q (%s/%s)",
			"uid-team-a", victims[0].Pod.UID, victims[0].Pod.Namespace, victims[0].Pod.Name)
	}
}

func TestEvictRecreatedPodCollision(t *testing.T) {
	// Recreated pod: same name and namespace, but different UID
	oldPod := buildTestPod("worker-0", "team-a", types.UID("old-uid"))
	newPod := buildTestPod("worker-0", "team-a", types.UID("new-uid"))

	nodeUtil := buildTestNodeUtilization("source-node", "4000m", "8000Mi", 80.0, 80.0, []*v1.Pod{newPod})

	// Eligible tasks list only contains the old task UID
	oldTask := buildTestTask(oldPod, 1000, 1000*1024*1024)
	tasks := []*api.TaskInfo{oldTask}

	totalAllocatable := map[v1.ResourceName]*resource.Quantity{
		v1.ResourceCPU:    resource.NewMilliQuantity(2000, resource.DecimalSI),
		v1.ResourceMemory: resource.NewQuantity(2000*1024*1024, resource.BinarySI),
	}

	continueFn := func(usage *NodeUtilization, totalAllocatableResource map[v1.ResourceName]*resource.Quantity, config interface{}) bool {
		return true
	}

	victims := evict(nodeUtil.pods, nodeUtil, totalAllocatable, continueFn, tasks, nil)
	if len(victims) != 0 {
		t.Fatalf("expected 0 victims for unmatched recreated pod UID, got %d", len(victims))
	}
}

func TestEvictCandidateEligibility(t *testing.T) {
	podEligible := buildTestPod("pod-1", "team-a", types.UID("uid-1"))
	podIneligible := buildTestPod("pod-2", "team-a", types.UID("uid-2"))

	nodeUtil := buildTestNodeUtilization("source-node", "4000m", "8000Mi", 80.0, 80.0, []*v1.Pod{podIneligible, podEligible})

	taskEligible := buildTestTask(podEligible, 1000, 1000*1024*1024)
	tasks := []*api.TaskInfo{taskEligible}

	totalAllocatable := map[v1.ResourceName]*resource.Quantity{
		v1.ResourceCPU:    resource.NewMilliQuantity(2000, resource.DecimalSI),
		v1.ResourceMemory: resource.NewQuantity(2000*1024*1024, resource.BinarySI),
	}

	continueFn := func(usage *NodeUtilization, totalAllocatableResource map[v1.ResourceName]*resource.Quantity, config interface{}) bool {
		return true
	}

	victims := evict(nodeUtil.pods, nodeUtil, totalAllocatable, continueFn, tasks, nil)
	if len(victims) != 1 {
		t.Fatalf("expected 1 victim, got %d", len(victims))
	}
	if victims[0].Pod.UID != types.UID("uid-1") {
		t.Fatalf("expected victim UID %q, got %q", "uid-1", victims[0].Pod.UID)
	}
}

func TestEvictStoppingConditions(t *testing.T) {
	pod1 := buildTestPod("pod-1", "team-a", types.UID("uid-1"))
	pod2 := buildTestPod("pod-2", "team-a", types.UID("uid-2"))

	nodeUtil := buildTestNodeUtilization("source-node", "4000m", "8000Mi", 80.0, 80.0, []*v1.Pod{pod1, pod2})

	task1 := buildTestTask(pod1, 1000, 1000*1024*1024)
	task2 := buildTestTask(pod2, 1000, 1000*1024*1024)
	tasks := []*api.TaskInfo{task1, task2}

	totalAllocatable := map[v1.ResourceName]*resource.Quantity{
		v1.ResourceCPU:    resource.NewMilliQuantity(2000, resource.DecimalSI),
		v1.ResourceMemory: resource.NewQuantity(2000*1024*1024, resource.BinarySI),
	}

	// Stop eviction after the first pod is evicted
	evictionCount := 0
	continueFn := func(usage *NodeUtilization, totalAllocatableResource map[v1.ResourceName]*resource.Quantity, config interface{}) bool {
		if evictionCount >= 1 {
			return false
		}
		evictionCount++
		return true
	}

	victims := evict(nodeUtil.pods, nodeUtil, totalAllocatable, continueFn, tasks, nil)
	if len(victims) != 1 {
		t.Fatalf("expected 1 victim due to stopping condition, got %d", len(victims))
	}
	if victims[0].Pod.UID != types.UID("uid-1") {
		t.Fatalf("expected first victim to be uid-1, got %q", victims[0].Pod.UID)
	}
}

func TestEvictResourceAccounting(t *testing.T) {
	pod := buildTestPod("pod-1", "team-a", types.UID("uid-1"))
	nodeUtil := buildTestNodeUtilization("source-node", "4000m", "8000Mi", 80.0, 80.0, []*v1.Pod{pod})

	// Task requests 1000m CPU (25% of 4000m)
	task := buildTestTask(pod, 1000, 0)
	tasks := []*api.TaskInfo{task}

	totalAllocatable := map[v1.ResourceName]*resource.Quantity{
		v1.ResourceCPU:    resource.NewMilliQuantity(2000, resource.DecimalSI),
		v1.ResourceMemory: resource.NewQuantity(2000*1024*1024, resource.BinarySI),
	}

	continueFn := func(usage *NodeUtilization, totalAllocatableResource map[v1.ResourceName]*resource.Quantity, config interface{}) bool {
		return true
	}

	victims := evict(nodeUtil.pods, nodeUtil, totalAllocatable, continueFn, tasks, nil)
	if len(victims) != 1 {
		t.Fatalf("expected 1 victim, got %d", len(victims))
	}

	// 80% - 25% = 55%
	expectedUtil := 55.0
	actualUtil := nodeUtil.utilization[v1.ResourceCPU]
	if actualUtil < expectedUtil-0.01 || actualUtil > expectedUtil+0.01 {
		t.Fatalf("expected node CPU utilization ~%.2f%%, got %.2f%%", expectedUtil, actualUtil)
	}

	// 2000m - 1000m = 1000m
	if totalAllocatable[v1.ResourceCPU].MilliValue() != 1000 {
		t.Fatalf("expected remaining allocatable CPU 1000m, got %d", totalAllocatable[v1.ResourceCPU].MilliValue())
	}
}
