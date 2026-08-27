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

// Package hintutil holds QueueingHint building blocks shared by quota plugins
// (capacity, proportion). It only contains logic that is independent of a
// plugin's queue accounting model: resource-dimension comparisons, PodGroup
// quota-phase checks, and the Node hint. Queue-scoping decisions (whether an
// event's Queue is relevant to a rejection) are plugin specific and stay in the
// plugins, because capacity uses per-Queue hierarchical quota while proportion
// recomputes a global weight-proportional share.
package hintutil

import (
	v1 "k8s.io/api/core/v1"

	"volcano.sh/apis/pkg/apis/scheduling"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/unschedulable"
)

// RejectedRequest returns the aggregate resource request whose positive
// dimensions are relevant to the rejection, and whether it could be resolved.
// The quantities are meant only to select the dimensions a rejected Job cares
// about; callers must not treat the sum as a schedulability check.
func RejectedRequest(job *api.JobInfo, rejection unschedulable.Rejection) (*api.Resource, bool) {
	if job == nil {
		return nil, false
	}
	// Enqueue rejections apply to the gang as a whole, so MinResources is the
	// request whose quota must become available.
	if len(rejection.Tasks) == 0 {
		return job.GetMinResources(), true
	}

	// Allocate rejections identify the tasks that failed. Restricting the
	// request to them avoids waking for dimensions used only by accepted tasks.
	request := api.EmptyResource()
	for _, taskID := range rejection.Tasks {
		task := job.Tasks[taskID]
		if task == nil || task.InitResreq == nil {
			// The cache snapshot is incomplete; callers must fail open.
			return nil, false
		}
		request.Add(task.InitResreq)
	}
	return request, true
}

// AllocatableGrewForRequest reports whether allocatable grew from old to new in
// any dimension the request occupies. Growth in an unrequested dimension cannot
// help the rejected Job.
func AllocatableGrewForRequest(request, oldAllocatable, newAllocatable *api.Resource) bool {
	// CPU and memory are stored outside ScalarResources.
	if request.MilliCPU > 0 && newAllocatable.MilliCPU > oldAllocatable.MilliCPU {
		return true
	}
	if request.Memory > 0 && newAllocatable.Memory > oldAllocatable.Memory {
		return true
	}
	// Extended resources only matter when the rejected request names them.
	for name, quantity := range request.ScalarResources {
		if quantity > 0 && newAllocatable.ScalarResources[name] > oldAllocatable.ScalarResources[name] {
			return true
		}
	}
	return false
}

// UsageReleasedForRequest reports whether usage dropped from old to new in any
// dimension the request occupies. A release confined to unrequested dimensions
// cannot raise the rejected Job's per-dimension share. Pass an empty new usage
// to model a full release (deletion).
func UsageReleasedForRequest(request, oldUsage, newUsage *api.Resource) bool {
	// A lower usage can help only on dimensions that constrained this request.
	if request.MilliCPU > 0 && oldUsage.MilliCPU > newUsage.MilliCPU {
		return true
	}
	if request.Memory > 0 && oldUsage.Memory > newUsage.Memory {
		return true
	}
	// Check requested extended resources with the same rule.
	for name, quantity := range request.ScalarResources {
		if quantity > 0 && oldUsage.ScalarResources[name] > newUsage.ScalarResources[name] {
			return true
		}
	}
	return false
}

// ResourceListIncreased reports whether any dimension in newList exceeds its
// value in oldList.
func ResourceListIncreased(oldList, newList v1.ResourceList) bool {
	// A missing old value is zero, which also covers newly added dimensions.
	for name, newQuantity := range newList {
		if newQuantity.Cmp(oldList[name]) > 0 {
			return true
		}
	}
	return false
}

// ResourceLimitRelaxed reports whether a limit ResourceList became looser,
// either because a previously limited dimension became unlimited (dropped) or
// because a remaining dimension's limit increased.
func ResourceLimitRelaxed(oldLimit, newLimit v1.ResourceList) bool {
	// Removing a dimension removes its hard limit entirely.
	for name := range oldLimit {
		if _, limited := newLimit[name]; !limited {
			return true
		}
	}
	// Limits that remain are looser only when their quantity increases.
	return ResourceListIncreased(oldLimit, newLimit)
}

// PodGroupConsumesQuota reports whether the phase makes the PodGroup count as
// inqueue or allocated demand for its Queue.
func PodGroupConsumesQuota(phase scheduling.PodGroupPhase) bool {
	return phase == scheduling.PodGroupInqueue || phase == scheduling.PodGroupRunning
}

// PodGroupReleasedQuota reports whether a previously consuming PodGroup stopped
// consuming quota in its old Queue, either by leaving the consuming phase or by
// moving to another Queue.
func PodGroupReleasedQuota(oldPg, newPg *api.PodGroup) bool {
	// A PodGroup that did not consume quota has nothing to release.
	if !PodGroupConsumesQuota(oldPg.Status.Phase) {
		return false
	}
	// Moving the PodGroup stops charging its old Queue even if it keeps running.
	if oldPg.Spec.Queue != newPg.Spec.Queue {
		return true
	}
	// Otherwise only leaving Inqueue/Running releases quota.
	return !PodGroupConsumesQuota(newPg.Status.Phase)
}

// NodeHint wakes a quota-rejected Job when cluster capacity grows in a resource
// dimension the Job requests. Node allocatable feeds every quota plugin's
// effective capacity regardless of queue topology, so this hint carries no
// queue scoping and is shared verbatim by capacity and proportion. It does not
// decide whether the growth is sufficient; the next scheduling session
// recomputes that. Missing request data wakes conservatively.
func NodeHint(job *api.JobInfo, rejection unschedulable.Rejection, oldObj, newObj any) (unschedulable.HintResult, error) {
	// Node events should carry the post-change Node. Unknown payloads cannot be
	// proved irrelevant, so wake rather than risk a stale cached Job.
	newNode, ok := newObj.(*v1.Node)
	if !ok || newNode == nil {
		return unschedulable.HintWakeup, nil
	}
	request, complete := RejectedRequest(job, rejection)
	if !complete {
		return unschedulable.HintWakeup, nil
	}
	// A zero request cannot be helped by allocatable growth.
	if request.IsEmpty() {
		return unschedulable.HintSkip, nil
	}

	oldAllocatable := api.EmptyResource()
	if oldObj != nil {
		// Update compares against the previous allocatable. Add intentionally
		// compares against zero, treating all capacity on the new Node as growth.
		oldNode, ok := oldObj.(*v1.Node)
		if !ok || oldNode == nil {
			return unschedulable.HintWakeup, nil
		}
		oldAllocatable = api.NewResource(oldNode.Status.Allocatable)
	}
	newAllocatable := api.NewResource(newNode.Status.Allocatable)
	// Sufficiency is deliberately left to the next scheduling session; hints
	// only establish that the event could improve a relevant dimension.
	if AllocatableGrewForRequest(request, oldAllocatable, newAllocatable) {
		return unschedulable.HintWakeup, nil
	}
	return unschedulable.HintSkip, nil
}
