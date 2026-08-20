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

package hintprovider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/resourcefit"
)

func TestResourceFitRejectionKeysUsesReadableSlashSeparatedKeys(t *testing.T) {
	pod := podWithCPU("target", "2")
	pod.UID = types.UID("task-1")
	task := api.NewTaskInfo(pod)

	fitErrors := api.NewFitErrors()
	fitErrors.SetNodeError("node-a", api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: "node-a"},
		&api.Status{Code: api.Unschedulable, Plugin: resourcefit.ProviderName, InsufficientResources: []string{"cpu"}}))
	fitErrors.SetNodeError("node-b", api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: "node-b"},
		&api.Status{Code: api.Unschedulable, Plugin: resourcefit.ProviderName, InsufficientResources: []string{"cpu"}}))

	nodes := map[string]*api.NodeInfo{
		"node-a": nodeInfoWithAllocatable("node-a", "4", "8Gi", "100"),
		"node-b": nodeInfoWithAllocatable("node-b", "1", "8Gi", "100"),
	}

	keys, complete := resourcefit.RejectionKeys(task, fitErrors, nodes)
	assert.True(t, complete)

	want := []api.HintKey{
		"rejected-task/task-1",
		"pod-release/node-a/cpu",
		"node-growth/node-a/cpu",
		"node-growth/node-b/cpu",
	}
	assert.ElementsMatch(t, want, keys)
	assert.NotContains(t, keys, api.HintKey("pod-release/node-b/cpu"))
}

// nodeInfoWithAllocatable builds a NodeInfo with the given total allocatable
// CPU/memory/pods. RejectionKeys reads insufficient dimensions
// from the rejecting Status's structured InsufficientResources rather than
// recomputing them from FutureIdle, so only Allocatable (used for the
// above-total-allocatable Pod-release gate) needs to be controlled here.
func nodeInfoWithAllocatable(name, allocatableCPU, allocatableMemory, allocatablePods string) *api.NodeInfo {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: v1.NodeStatus{Allocatable: v1.ResourceList{
			v1.ResourceCPU:    resource.MustParse(allocatableCPU),
			v1.ResourceMemory: resource.MustParse(allocatableMemory),
			v1.ResourcePods:   resource.MustParse(allocatablePods),
		}},
	}
	return api.NewNodeInfo(node)
}

func TestResourceFitRejectionKeys(t *testing.T) {
	task := api.NewTaskInfo(podWithCPU("target", "2"))

	tests := []struct {
		name         string
		task         *api.TaskInfo
		fitErrors    func() *api.FitErrors
		nodes        map[string]*api.NodeInfo
		wantKeys     []api.HintKey
		wantComplete bool
	}{
		{
			name: "CPU insufficient produces pod-release and node-growth keys",
			task: task,
			fitErrors: func() *api.FitErrors {
				fe := api.NewFitErrors()
				fe.SetNodeError("node-a", api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: "node-a"},
					&api.Status{Code: api.Unschedulable, Plugin: resourcefit.ProviderName, InsufficientResources: []string{"cpu"}}))
				return fe
			},
			nodes: map[string]*api.NodeInfo{
				"node-a": nodeInfoWithAllocatable("node-a", "8", "8Gi", "100"),
			},
			wantKeys: []api.HintKey{
				resourcefit.RejectedTaskKey(task.UID),
				resourcefit.NodeGrowthKey("node-a", "cpu"),
				resourcefit.PodReleaseKey("node-a", "cpu"),
			},
			wantComplete: true,
		},
		{
			name: "CPU request above total allocatable omits only pod-release key",
			task: task,
			fitErrors: func() *api.FitErrors {
				fe := api.NewFitErrors()
				fe.SetNodeError("node-a", api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: "node-a"},
					&api.Status{Code: api.Unschedulable, Plugin: resourcefit.ProviderName, InsufficientResources: []string{"cpu"}}))
				return fe
			},
			nodes: map[string]*api.NodeInfo{
				// Node's total allocatable CPU (1) is below the task's 2 CPU request.
				"node-a": nodeInfoWithAllocatable("node-a", "1", "8Gi", "100"),
			},
			wantKeys: []api.HintKey{
				resourcefit.RejectedTaskKey(task.UID),
				resourcefit.NodeGrowthKey("node-a", "cpu"),
			},
			wantComplete: true,
		},
		{
			name: "pod-count shortage produces a pods key",
			task: task,
			fitErrors: func() *api.FitErrors {
				fe := api.NewFitErrors()
				fe.SetNodeError("node-a", api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: "node-a"},
					&api.Status{Code: api.Unschedulable, Plugin: resourcefit.ProviderName, InsufficientResources: []string{"pods"}}))
				return fe
			},
			nodes: map[string]*api.NodeInfo{
				"node-a": nodeInfoWithAllocatable("node-a", "8", "8Gi", "100"),
			},
			wantKeys: []api.HintKey{
				resourcefit.RejectedTaskKey(task.UID),
				resourcefit.NodeGrowthKey("node-a", "pods"),
				resourcefit.PodReleaseKey("node-a", "pods"),
			},
			wantComplete: true,
		},
		{
			// Regression for the finding that recomputing dimensions from
			// FutureIdle systematically misses real Pod-count blockers: the
			// k8s snapshot's live Pod count (what the predicates Pod-count
			// check actually compares against) is not reflected in
			// node.FutureIdle(), which only tracks CPU/memory/scalar
			// Releasing. Here the node's total allocatable resources make it
			// look CPU/memory-sufficient in every way FutureIdle can express,
			// yet the real rejecting Status still identifies "pods" as the
			// insufficient dimension; the structured status must be the sole
			// source of truth, so the pods keys must still be produced.
			name: "pod-count status produces a pods key even though the node looks otherwise unconstrained",
			task: task,
			fitErrors: func() *api.FitErrors {
				fe := api.NewFitErrors()
				fe.SetNodeError("node-a", api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: "node-a"},
					&api.Status{Code: api.Unschedulable, Plugin: resourcefit.ProviderName, InsufficientResources: []string{"pods"}}))
				return fe
			},
			nodes: map[string]*api.NodeInfo{
				"node-a": nodeInfoWithAllocatable("node-a", "64", "256Gi", "110"),
			},
			wantKeys: []api.HintKey{
				resourcefit.RejectedTaskKey(task.UID),
				resourcefit.NodeGrowthKey("node-a", "pods"),
				resourcefit.PodReleaseKey("node-a", "pods"),
			},
			wantComplete: true,
		},
		{
			// A resource-fit-rejected node whose Status carries no structured
			// InsufficientResources (e.g. an older/unstructured caller) must
			// fail open rather than silently contribute only the generic
			// rejected-task key.
			name: "resource-fit status without structured dimensions fails open",
			task: task,
			fitErrors: func() *api.FitErrors {
				fe := api.NewFitErrors()
				fe.SetNodeError("node-a", api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: "node-a"},
					&api.Status{Code: api.Unschedulable, Plugin: resourcefit.ProviderName}))
				return fe
			},
			nodes: map[string]*api.NodeInfo{
				"node-a": nodeInfoWithAllocatable("node-a", "8", "8Gi", "100"),
			},
			wantKeys:     nil,
			wantComplete: false,
		},
		{
			name: "node rejected only by another plugin produces no resource-fit key",
			task: task,
			fitErrors: func() *api.FitErrors {
				fe := api.NewFitErrors()
				fe.SetNodeError("node-a", api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: "node-a"},
					&api.Status{Code: api.Unschedulable, Plugin: "node-affinity"}))
				return fe
			},
			nodes: map[string]*api.NodeInfo{
				"node-a": nodeInfoWithAllocatable("node-a", "8", "8Gi", "100"),
			},
			wantKeys:     nil,
			wantComplete: false,
		},
		{
			name: "missing task request returns complete false",
			task: api.NewTaskInfo(&v1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "no-request", UID: types.UID("no-request")}}),
			fitErrors: func() *api.FitErrors {
				fe := api.NewFitErrors()
				fe.SetNodeError("node-a", api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: "node-a"},
					&api.Status{Code: api.Unschedulable, Plugin: resourcefit.ProviderName, InsufficientResources: []string{"cpu"}}))
				return fe
			},
			nodes: map[string]*api.NodeInfo{
				"node-a": nodeInfoWithAllocatable("node-a", "8", "8Gi", "100"),
			},
			wantComplete: false,
		},
		{
			name: "missing node returns complete false",
			task: task,
			fitErrors: func() *api.FitErrors {
				fe := api.NewFitErrors()
				fe.SetNodeError("node-missing", api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: "node-missing"},
					&api.Status{Code: api.Unschedulable, Plugin: resourcefit.ProviderName, InsufficientResources: []string{"cpu"}}))
				return fe
			},
			nodes:        map[string]*api.NodeInfo{},
			wantComplete: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// api.NewTaskInfo never leaves InitResreq nil for a Pod with no
			// requests; force the nil case explicitly for that scenario.
			taskArg := test.task
			if test.name == "missing task request returns complete false" {
				taskArg = &api.TaskInfo{UID: taskArg.UID}
			}
			keys, complete := resourcefit.RejectionKeys(taskArg, test.fitErrors(), test.nodes)
			assert.Equal(t, test.wantComplete, complete)
			if test.wantComplete {
				assert.ElementsMatch(t, test.wantKeys, keys)
			} else {
				assert.Nil(t, keys)
			}
		})
	}
}

func TestResourceFitPodReleaseKeysIntersectRejectedTask(t *testing.T) {
	targetPod := podWithCPU("target", "2")
	task := api.NewTaskInfo(targetPod)
	job := api.NewJobInfo("job", task)

	entries, err := (&ResourceFitHintProvider{}).EventsToRegister(context.Background())
	if err != nil {
		t.Fatalf("EventsToRegister() error = %v", err)
	}
	entry := findEvent(t, entries, fwk.Pod, fwk.Delete)

	rejection := api.Rejection{
		Tasks:    []api.TaskID{task.UID},
		HintKeys: []api.HintKey{resourcefit.RejectedTaskKey(task.UID), resourcefit.NodeGrowthKey("node-a", "cpu")},
	}

	jobKeys, err := entry.JobKeysFn(job, rejection)
	if err != nil {
		t.Fatalf("JobKeysFn() error = %v", err)
	}

	deletedPod := scheduledPodWithCPU("target", "2")
	deletedPod.UID = targetPod.UID
	eventKeys, err := entry.EventKeysFn(deletedPod, nil)
	if err != nil {
		t.Fatalf("EventKeysFn() error = %v", err)
	}

	assert.Contains(t, jobKeys, resourcefit.RejectedTaskKey(task.UID))
	assert.Contains(t, eventKeys, resourcefit.RejectedTaskKey(task.UID))
}

// TestResourceFitPodReleaseOnAnotherRejectedNodeIntersects verifies that the
// index records every candidate node rejected by Resource Fit. Releasing CPU
// on node-b must wake a Job even when node-a was also insufficient.
func TestResourceFitPodReleaseOnAnotherRejectedNodeIntersects(t *testing.T) {
	task := api.NewTaskInfo(podWithCPU("target", "2"))
	job := api.NewJobInfo("job", task)

	fitErrors := api.NewFitErrors()
	for _, nodeName := range []string{"node-a", "node-b"} {
		fitErrors.SetNodeError(nodeName, api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: nodeName},
			&api.Status{Code: api.Unschedulable, Plugin: resourcefit.ProviderName, InsufficientResources: []string{"cpu"}}))
	}
	nodes := map[string]*api.NodeInfo{
		"node-a": nodeInfoWithAllocatable("node-a", "4", "8Gi", "100"),
		"node-b": nodeInfoWithAllocatable("node-b", "4", "8Gi", "100"),
	}
	rejectionKeys, complete := resourcefit.RejectionKeys(task, fitErrors, nodes)
	if !complete {
		t.Fatalf("resourcefit.RejectionKeys() complete = false, want true")
	}

	entries, err := (&ResourceFitHintProvider{}).EventsToRegister(context.Background())
	if err != nil {
		t.Fatalf("EventsToRegister() error = %v", err)
	}
	entry := findEvent(t, entries, fwk.Pod, fwk.Delete)
	jobKeys, err := entry.JobKeysFn(job, api.Rejection{Tasks: []api.TaskID{task.UID}, HintKeys: rejectionKeys})
	if err != nil {
		t.Fatalf("JobKeysFn() error = %v", err)
	}

	deletedPod := scheduledPodWithCPU("other", "2")
	deletedPod.Spec.NodeName = "node-b"
	eventKeys, err := entry.EventKeysFn(deletedPod, nil)
	if err != nil {
		t.Fatalf("EventKeysFn() error = %v", err)
	}

	wantKey := resourcefit.PodReleaseKey("node-b", "cpu")
	assert.Contains(t, jobKeys, wantKey)
	assert.Contains(t, eventKeys, wantKey)
}

func TestResourceFitPodReleaseJobKeysErrorsWhenIncomplete(t *testing.T) {
	task := api.NewTaskInfo(podWithCPU("target", "2"))
	job := api.NewJobInfo("job", task)

	entries, err := (&ResourceFitHintProvider{}).EventsToRegister(context.Background())
	if err != nil {
		t.Fatalf("EventsToRegister() error = %v", err)
	}
	entry := findEvent(t, entries, fwk.Pod, fwk.Delete)

	rejection := api.Rejection{Tasks: []api.TaskID{task.UID}}
	_, err = entry.JobKeysFn(job, rejection)
	assert.Error(t, err)
}

func TestResourceFitNodeGrowthJobKeysErrorsWhenIncomplete(t *testing.T) {
	task := api.NewTaskInfo(podWithCPU("target", "2"))
	job := api.NewJobInfo("job", task)

	entries, err := (&ResourceFitHintProvider{}).EventsToRegister(context.Background())
	if err != nil {
		t.Fatalf("EventsToRegister() error = %v", err)
	}
	entry := findEvent(t, entries, fwk.Node, fwk.UpdateNodeAllocatable)

	rejection := api.Rejection{Tasks: []api.TaskID{task.UID}}
	_, err = entry.JobKeysFn(job, rejection)
	assert.Error(t, err)
}

func TestResourceFitNodeAddKeysIntersectRequestedDimensions(t *testing.T) {
	task := api.NewTaskInfo(podWithCPU("target", "2"))
	job := api.NewJobInfo("job", task)
	rejection := api.Rejection{Tasks: []api.TaskID{task.UID}}

	entries, err := (&ResourceFitHintProvider{}).EventsToRegister(context.Background())
	if err != nil {
		t.Fatalf("EventsToRegister() error = %v", err)
	}
	entry := findEvent(t, entries, fwk.Node, fwk.Add)

	// Node-Add keys must derive from job.Tasks directly, independent of any
	// stored rejection hint keys.
	jobKeys, err := entry.JobKeysFn(job, rejection)
	if err != nil {
		t.Fatalf("JobKeysFn() error = %v", err)
	}

	newNode := nodeWithResources("4", "8Gi")
	eventKeys, err := entry.EventKeysFn(nil, newNode)
	if err != nil {
		t.Fatalf("EventKeysFn() error = %v", err)
	}

	assert.Contains(t, jobKeys, resourcefit.NodeAddKey("cpu"))
	assert.Contains(t, eventKeys, resourcefit.NodeAddKey("cpu"))
}

// TestResourceFitNodeGrowthEventKeysNoIncreaseReturnsEmpty proves that a valid
// Node/UpdateNodeAllocatable event with no increased dimension (e.g. a label
// or condition update carried on the same allocatable values) returns an
// empty key list, not an error. Returning an error here would send every such
// event to the coarse full-bucket fallback (§4), even though the event
// necessarily cannot satisfy any Node-growth key's necessary condition.
func TestResourceFitNodeGrowthEventKeysNoIncreaseReturnsEmpty(t *testing.T) {
	entries, err := (&ResourceFitHintProvider{}).EventsToRegister(context.Background())
	if err != nil {
		t.Fatalf("EventsToRegister() error = %v", err)
	}
	entry := findEvent(t, entries, fwk.Node, fwk.UpdateNodeAllocatable)

	node := nodeWithResources("4", "8Gi")
	keys, err := entry.EventKeysFn(node, node)
	if err != nil {
		t.Fatalf("EventKeysFn() error = %v, want nil error for a valid event with no increased dimension", err)
	}
	assert.Empty(t, keys)
}

// TestResourceFitNodeAddJobKeysErrorsOnNilInitResreq proves that a Job/Add key
// extraction fails open when any rejected task lacks InitResreq, instead of
// silently skipping that task and returning a known-incomplete key list built
// only from the other tasks' dimensions.
func TestResourceFitNodeAddJobKeysErrorsOnNilInitResreq(t *testing.T) {
	completeTask := api.NewTaskInfo(podWithCPU("complete", "2"))
	incompleteTask := api.NewTaskInfo(podWithCPU("incomplete", "1"))
	incompleteTask.InitResreq = nil
	job := api.NewJobInfo("job", completeTask, incompleteTask)
	rejection := api.Rejection{Tasks: []api.TaskID{completeTask.UID, incompleteTask.UID}}

	entries, err := (&ResourceFitHintProvider{}).EventsToRegister(context.Background())
	if err != nil {
		t.Fatalf("EventsToRegister() error = %v", err)
	}
	entry := findEvent(t, entries, fwk.Node, fwk.Add)

	_, err = entry.JobKeysFn(job, rejection)
	assert.Error(t, err, "a rejected task with nil InitResreq must fall back instead of returning a partial dimension list from the other tasks")
}

// TestResourceFitPodReleasePodCountKeyIntersectsRealPodDelete proves that a
// Pod-count Job key (pod-release/<node>/pods) computed from a resource-fit
// rejection intersects the Event key produced by deleting a real scheduled
// Pod on the blocked node. The deleted Pod's own spec requests no "pods"
// resource explicitly; GetPodResourceRequest adds an implicit "pods":1 scalar
// to every Pod (including the rejected task's own request), so the pods
// dimension is always present on both sides of the necessary-condition
// contract for a Pod-count blocker.
func TestResourceFitPodReleasePodCountKeyIntersectsRealPodDelete(t *testing.T) {
	task := api.NewTaskInfo(podWithCPU("target", "2"))
	job := api.NewJobInfo("job", task)

	fitErrors := api.NewFitErrors()
	fitErrors.SetNodeError("node-a", api.NewFitErrWithStatus(&api.TaskInfo{}, &api.NodeInfo{Name: "node-a"},
		&api.Status{Code: api.Unschedulable, Plugin: resourcefit.ProviderName, InsufficientResources: []string{"pods"}}))
	nodes := map[string]*api.NodeInfo{
		"node-a": nodeInfoWithAllocatable("node-a", "8", "8Gi", "100"),
	}
	keys, complete := resourcefit.RejectionKeys(task, fitErrors, nodes)
	if !complete {
		t.Fatalf("resourcefit.RejectionKeys() complete = false, want true")
	}
	wantKey := resourcefit.PodReleaseKey("node-a", "pods")
	assert.Contains(t, keys, wantKey)

	rejection := api.Rejection{Tasks: []api.TaskID{task.UID}, HintKeys: keys}

	entries, err := (&ResourceFitHintProvider{}).EventsToRegister(context.Background())
	if err != nil {
		t.Fatalf("EventsToRegister() error = %v", err)
	}
	entry := findEvent(t, entries, fwk.Pod, fwk.Delete)

	jobKeys, err := entry.JobKeysFn(job, rejection)
	if err != nil {
		t.Fatalf("JobKeysFn() error = %v", err)
	}
	assert.Contains(t, jobKeys, wantKey)

	// An unrelated scheduled Pod on the same node is deleted; it requests no
	// "pods" resource in its own spec.
	deletedPod := scheduledPodWithCPU("other", "1")
	deletedPod.Spec.NodeName = "node-a"
	eventKeys, err := entry.EventKeysFn(deletedPod, nil)
	if err != nil {
		t.Fatalf("EventKeysFn() error = %v", err)
	}
	assert.Contains(t, eventKeys, wantKey, "Pod/Delete event keys must derive an implicit pods=1 release so a Pod-count Job key intersects it")
}

// findEvent locates the ClusterEventWithHint entry for resource whose
// ActionType includes action. The Pod subscription's ActionType may also
// carry UpdatePodScaleDown depending on the InPlacePodVerticalScaling feature
// gate, so this matches on the bit being set rather than exact equality.
func findEvent(t *testing.T, entries []api.ClusterEventWithHint, resource fwk.EventResource, action fwk.ActionType) api.ClusterEventWithHint {
	t.Helper()
	for _, entry := range entries {
		if entry.Event.Resource == resource && entry.Event.ActionType&action == action {
			return entry
		}
	}
	t.Fatalf("no ClusterEventWithHint found for resource=%v action=%v", resource, action)
	return api.ClusterEventWithHint{}
}
