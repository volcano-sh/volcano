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

package resourcefit

import (
	"fmt"
	"sort"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/unschedulable"
)

// ProviderName identifies Volcano's built-in node resource-fit predicate.
const ProviderName = "predicates-resource-fit"

// Hint key kinds separate task, node and resource values in the same index.
const (
	hintKindPodRelease   = "pod-release"
	hintKindNodeGrowth   = "node-growth"
	hintKindRejectedTask = "rejected-task"
	hintKindNodeAdd      = "node-add"
)

// hintKey joins readable slash-separated parts. Node names and task UIDs cannot
// contain "/", and resource names are always the final component.
func hintKey(parts ...string) unschedulable.HintKey {
	return unschedulable.HintKey(strings.Join(parts, "/"))
}

// PodReleaseKey identifies a resource dimension released on one node.
func PodReleaseKey(nodeName, dimension string) unschedulable.HintKey {
	return hintKey(hintKindPodRelease, nodeName, dimension)
}

// NodeGrowthKey identifies an allocatable resource dimension growing on one node.
func NodeGrowthKey(nodeName, dimension string) unschedulable.HintKey {
	return hintKey(hintKindNodeGrowth, nodeName, dimension)
}

// RejectedTaskKey identifies deletion of a task that participated in a rejection.
func RejectedTaskKey(taskID api.TaskID) unschedulable.HintKey {
	return hintKey(hintKindRejectedTask, string(taskID))
}

// NodeAddKey identifies a resource dimension supplied by a newly added node.
func NodeAddKey(dimension string) unschedulable.HintKey {
	return hintKey(hintKindNodeAdd, dimension)
}

// hasKeyKind reports whether key uses the given kind prefix.
func hasKeyKind(key unschedulable.HintKey, kind string) bool {
	return strings.HasPrefix(string(key), kind+"/")
}

// filterKeysByKind returns the subset of keys whose kind matches one of kinds.
func filterKeysByKind(keys []unschedulable.HintKey, kinds ...string) []unschedulable.HintKey {
	var filtered []unschedulable.HintKey
	for _, key := range keys {
		for _, kind := range kinds {
			if hasKeyKind(key, kind) {
				filtered = append(filtered, key)
				break
			}
		}
	}
	return filtered
}

// RejectionKeys returns the task and node/resource keys for one
// resource-fit rejection. e.g. task-1 requests 2 CPUs and is rejected on
// node-a (4 CPUs total, currently insufficient) and node-b (1 CPU total):
//   - rejected-task/task-1
//   - pod-release/node-a/cpu
//   - node-growth/node-a/cpu
//   - node-growth/node-b/cpu
//
// complete is false when the rejection does not contain enough structured data
// to build every key.
func RejectionKeys(task *api.TaskInfo, fitErrors *api.FitErrors, nodes map[string]*api.NodeInfo) ([]unschedulable.HintKey, bool) {
	if task == nil || task.InitResreq == nil || fitErrors == nil {
		return nil, false
	}

	nodeDimensions := fitErrors.NodePluginInsufficientResources(ProviderName)
	if len(nodeDimensions) == 0 {
		return nil, false
	}

	keys := sets.New[unschedulable.HintKey](RejectedTaskKey(task.UID))
	for nodeName, dimensions := range nodeDimensions {
		if len(dimensions) == 0 {
			// Do not index a rejection with unknown resource dimensions.
			return nil, false
		}
		node, ok := nodes[nodeName]
		if !ok || node == nil {
			return nil, false
		}

		for _, dimension := range dimensions {
			keys.Insert(NodeGrowthKey(nodeName, dimension))
			if task.InitResreq.Get(v1.ResourceName(dimension)) <= node.Allocatable.Get(v1.ResourceName(dimension)) {
				keys.Insert(PodReleaseKey(nodeName, dimension))
			}
		}

		if keys.Len() > unschedulable.MaxHintKeysPerPluginEvent {
			return nil, false
		}
	}

	return sortedHintKeys(keys), true
}

func sortedHintKeys(keys sets.Set[unschedulable.HintKey]) []unschedulable.HintKey {
	result := keys.UnsortedList()
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// requestedDimensions returns resources with a positive request.
func requestedDimensions(r *api.Resource) []string {
	if r == nil {
		return nil
	}
	var dims []string
	if r.MilliCPU > 0 {
		dims = append(dims, string(v1.ResourceCPU))
	}
	if r.Memory > 0 {
		dims = append(dims, string(v1.ResourceMemory))
	}
	for name, quantity := range r.ScalarResources {
		if quantity > 0 {
			dims = append(dims, string(name))
		}
	}
	return dims
}

// changedDimensions returns resources that increased from before to after.
func changedDimensions(before, after *api.Resource) []string {
	if before == nil || after == nil {
		return nil
	}
	var dims []string
	if after.MilliCPU > before.MilliCPU {
		dims = append(dims, string(v1.ResourceCPU))
	}
	if after.Memory > before.Memory {
		dims = append(dims, string(v1.ResourceMemory))
	}
	names := sets.New[v1.ResourceName]()
	for name := range before.ScalarResources {
		names.Insert(name)
	}
	for name := range after.ScalarResources {
		names.Insert(name)
	}
	for name := range names {
		if after.ScalarResources[name] > before.ScalarResources[name] {
			dims = append(dims, string(name))
		}
	}
	return dims
}

// PodReleaseJobKeys returns the Pod-release and task keys recorded
// for a resource-fit rejection.
func PodReleaseJobKeys(job *api.JobInfo, rejection unschedulable.Rejection) ([]unschedulable.HintKey, error) {
	if len(rejection.HintKeys) == 0 {
		return nil, fmt.Errorf("resource-fit pod-release keys are incomplete")
	}
	return filterKeysByKind(rejection.HintKeys, hintKindPodRelease, hintKindRejectedTask), nil
}

// PodReleaseEventKeys returns the task key and resources released
// by a Pod deletion, termination or scale-down.
func PodReleaseEventKeys(oldObj, newObj any) ([]unschedulable.HintKey, error) {
	oldPod, ok := oldObj.(*v1.Pod)
	if !ok || oldPod == nil {
		return nil, fmt.Errorf("expected old object to be *v1.Pod, got %T", oldObj)
	}

	keys := []unschedulable.HintKey{RejectedTaskKey(api.TaskID(oldPod.UID))}
	if oldPod.Spec.NodeName == "" {
		// A pending Pod holds no node resources to release.
		return keys, nil
	}

	oldRequest := api.GetPodResourceRequest(oldPod)
	newRequest := api.EmptyResource()
	if newObj != nil {
		newPod, ok := newObj.(*v1.Pod)
		if !ok || newPod == nil {
			return nil, fmt.Errorf("expected new object to be *v1.Pod, got %T", newObj)
		}
		newRequest = api.GetPodResourceRequest(newPod)
	}

	for _, dimension := range changedDimensions(newRequest, oldRequest) {
		keys = append(keys, PodReleaseKey(oldPod.Spec.NodeName, dimension))
	}
	return keys, nil
}

// NodeGrowthJobKeys returns the Node-growth keys recorded for a
// resource-fit rejection.
func NodeGrowthJobKeys(job *api.JobInfo, rejection unschedulable.Rejection) ([]unschedulable.HintKey, error) {
	if len(rejection.HintKeys) == 0 {
		return nil, fmt.Errorf("resource-fit node-growth keys are incomplete")
	}
	return filterKeysByKind(rejection.HintKeys, hintKindNodeGrowth), nil
}

// NodeGrowthEventKeys returns resources whose allocatable value
// increased on a Node.
func NodeGrowthEventKeys(oldObj, newObj any) ([]unschedulable.HintKey, error) {
	oldNode, ok := oldObj.(*v1.Node)
	if !ok || oldNode == nil {
		return nil, fmt.Errorf("expected old object to be *v1.Node, got %T", oldObj)
	}
	newNode, ok := newObj.(*v1.Node)
	if !ok || newNode == nil {
		return nil, fmt.Errorf("expected new object to be *v1.Node, got %T", newObj)
	}

	dimensions := changedDimensions(api.NewResource(oldNode.Status.Allocatable), api.NewResource(newNode.Status.Allocatable))
	if len(dimensions) == 0 {
		// No increased resource can match a Node-growth key.
		return nil, nil
	}
	keys := make([]unschedulable.HintKey, 0, len(dimensions))
	for _, dimension := range dimensions {
		keys = append(keys, NodeGrowthKey(newNode.Name, dimension))
	}
	return keys, nil
}

// NodeAddJobKeys returns the resources requested by rejected tasks.
func NodeAddJobKeys(job *api.JobInfo, rejection unschedulable.Rejection) ([]unschedulable.HintKey, error) {
	if job == nil {
		return nil, fmt.Errorf("resource-fit node-add keys: job is nil")
	}

	dims := sets.New[string]()
	for _, taskID := range rejection.Tasks {
		task := job.Tasks[taskID]
		if task == nil {
			continue
		}
		if task.InitResreq == nil {
			return nil, fmt.Errorf("resource-fit node-add keys: task %s has no InitResreq", taskID)
		}
		dims.Insert(requestedDimensions(task.InitResreq)...)
	}
	if dims.Len() == 0 {
		return nil, fmt.Errorf("resource-fit node-add keys: no requested dimensions found")
	}

	keys := make([]unschedulable.HintKey, 0, dims.Len())
	for _, dim := range sortedStrings(dims) {
		keys = append(keys, NodeAddKey(dim))
	}
	return keys, nil
}

// NodeAddEventKeys returns resources supplied by a new Node.
func NodeAddEventKeys(_, newObj any) ([]unschedulable.HintKey, error) {
	newNode, ok := newObj.(*v1.Node)
	if !ok || newNode == nil {
		return nil, fmt.Errorf("expected new object to be *v1.Node, got %T", newObj)
	}

	dims := requestedDimensions(api.NewResource(newNode.Status.Allocatable))
	if len(dims) == 0 {
		return nil, fmt.Errorf("resource-fit node-add event: no allocatable dimensions found")
	}
	keys := make([]unschedulable.HintKey, 0, len(dims))
	for _, dim := range dims {
		keys = append(keys, NodeAddKey(dim))
	}
	return keys, nil
}

func sortedStrings(s sets.Set[string]) []string {
	result := s.UnsortedList()
	sort.Strings(result)
	return result
}
