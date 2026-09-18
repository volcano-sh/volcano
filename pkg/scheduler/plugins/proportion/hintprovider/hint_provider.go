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

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/apis/pkg/apis/scheduling"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/hintutil"
	"volcano.sh/volcano/pkg/scheduler/unschedulable"
)

// Provider supplies QueueingHints for Jobs rejected by the proportion plugin.
//
// proportion recomputes every Queue's deserved as a global weight-proportional
// split of cluster resources, so a rejection in one Queue can be relieved by a
// change in ANY other Queue. Unlike capacity, these hints therefore never scope
// events to the rejected Job's own Queue. They filter only on what cannot shift
// any per-dimension deserved share: no-op Queue updates, and releases confined
// to resource dimensions the Job does not request. The Node hint is identical
// to capacity's and is shared through hintutil.
type Provider struct{}

var _ unschedulable.HintProvider = (*Provider)(nil)

// EventsToRegister implements unschedulable.HintProvider. Queue Add is intentionally not
// registered: adding a weighted Queue only dilutes the split and can never
// raise an already-rejected Job's deserved.
func (p *Provider) EventsToRegister(_ context.Context) ([]unschedulable.EventWithHint, error) {
	return []unschedulable.EventWithHint{
		// Global share inputs change when a Queue is updated or removed.
		{Event: fwk.ClusterEvent{Resource: unschedulable.QueueEvent, ActionType: fwk.Update | fwk.Delete}, HintFn: queueHint},
		// A PodGroup leaving quota-consuming state returns demand to the pool.
		{Event: fwk.ClusterEvent{Resource: unschedulable.PodGroupEvent, ActionType: fwk.Update | fwk.Delete}, HintFn: podGroupHint},
		// New allocatable can increase every Queue's effective capacity.
		{Event: fwk.ClusterEvent{Resource: fwk.Node, ActionType: fwk.Add | fwk.UpdateNodeAllocatable}, HintFn: hintutil.NodeHint},
		// Pod deletion or scale-down lowers globally accounted usage.
		{Event: fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete | fwk.UpdatePodScaleDown}, HintFn: podHint},
	}, nil
}

// queueHint wakes a rejected Job when a Queue change can reshape the global
// weight-proportional split. It deliberately does not restrict itself to the
// Job's own Queue: deserved is recomputed across all Queues, so a change to any
// Queue can raise this Job's share. Only metadata-only updates, which leave
// every redistribution input unchanged, are skipped.
func queueHint(_ *api.JobInfo, _ unschedulable.Rejection, oldObj, newObj any) (unschedulable.HintResult, error) {
	// Delete: the Queue's deserved share returns to the pool for the rest.
	if newObj == nil {
		return unschedulable.HintWakeup, nil
	}
	// Update must provide both snapshots. Unknown payloads wake conservatively.
	oldQueue, oldOK := oldObj.(*scheduling.Queue)
	newQueue, newOK := newObj.(*scheduling.Queue)
	if !oldOK || oldQueue == nil || !newOK || newQueue == nil {
		return unschedulable.HintWakeup, nil
	}
	// Any scheduling input change can reshape every Queue's global share.
	if queueRedistributionInputChanged(oldQueue, newQueue) {
		return unschedulable.HintWakeup, nil
	}
	return unschedulable.HintSkip, nil
}

// queueRedistributionInputChanged reports whether a Queue update touched a field
// that feeds proportion's split: open/closed state, weight, capability limit or
// guarantee floor. Direction is irrelevant here - a tighter bound on one Queue
// frees resources that raise another Queue's deserved - so any change wakes.
func queueRedistributionInputChanged(oldQueue, newQueue *scheduling.Queue) bool {
	// Closed Queues do not participate in allocation like open Queues.
	if oldQueue.Status.State != newQueue.Status.State {
		return true
	}
	// Weight directly changes the Queue's fraction of remaining resources.
	if oldQueue.Spec.Weight != newQueue.Spec.Weight {
		return true
	}
	// Capability caps the share and guarantee supplies its floor. Direction is
	// intentionally ignored because tightening one Queue can help another.
	return !equality.Semantic.DeepEqual(oldQueue.Spec.Capability, newQueue.Spec.Capability) ||
		!equality.Semantic.DeepEqual(oldQueue.Spec.Guarantee.Resource, newQueue.Spec.Guarantee.Resource)
}

// podHint wakes a rejected Job when a deleted or scaled-down Pod frees capacity
// in a dimension the Job requests. Because proportion accounts usage globally,
// the released Pod's Queue is irrelevant; only the released dimensions matter.
func podHint(job *api.JobInfo, rejection unschedulable.Rejection, oldObj, newObj any) (unschedulable.HintResult, error) {
	// Delete and scale-down events need the old Pod to measure released usage.
	oldPod, ok := oldObj.(*v1.Pod)
	if !ok || oldPod == nil {
		return unschedulable.HintWakeup, nil
	}
	request, complete := hintutil.RejectedRequest(job, rejection)
	if !complete {
		return unschedulable.HintWakeup, nil
	}
	// A zero request has no resource dimension that this release can improve.
	if request.IsEmpty() {
		return unschedulable.HintSkip, nil
	}
	oldUsage := api.GetPodResourceRequest(oldPod)
	newUsage := api.EmptyResource()
	// Delete has no new Pod and therefore releases all old usage. Scale-down
	// compares the two requests and may release only selected dimensions.
	if newPod, ok := newObj.(*v1.Pod); ok && newPod != nil {
		newUsage = api.GetPodResourceRequest(newPod)
	}
	if hintutil.UsageReleasedForRequest(request, oldUsage, newUsage) {
		return unschedulable.HintWakeup, nil
	}
	return unschedulable.HintSkip, nil
}

// podGroupHint wakes a rejected Job when a consuming PodGroup releases demand in
// a dimension the Job requests. As with podHint the releasing Queue is
// irrelevant under global fair share; only the released dimensions matter.
func podGroupHint(job *api.JobInfo, rejection unschedulable.Rejection, oldObj, newObj any) (unschedulable.HintResult, error) {
	// The old snapshot identifies whether the PodGroup previously consumed quota.
	oldPg, ok := oldObj.(*api.PodGroup)
	if !ok || oldPg == nil {
		return unschedulable.HintWakeup, nil
	}
	if !hintutil.PodGroupConsumesQuota(oldPg.Status.Phase) {
		return unschedulable.HintSkip, nil
	}
	// Delete releases the whole old demand; update releases it only when the
	// PodGroup leaves consuming state or moves to another Queue.
	released := newObj == nil
	if !released {
		newPg, ok := newObj.(*api.PodGroup)
		if !ok || newPg == nil {
			return unschedulable.HintWakeup, nil
		}
		released = hintutil.PodGroupReleasedQuota(oldPg, newPg)
	}
	if !released {
		return unschedulable.HintSkip, nil
	}

	// MinResources is the demand proportion charged while the gang was queued.
	demand := podGroupDemand(oldPg)
	if demand.IsEmpty() {
		// Released dimensions unknown; wake conservatively.
		return unschedulable.HintWakeup, nil
	}
	request, complete := hintutil.RejectedRequest(job, rejection)
	if !complete {
		return unschedulable.HintWakeup, nil
	}
	// No requested dimension can benefit from the released demand.
	if request.IsEmpty() {
		return unschedulable.HintSkip, nil
	}
	if hintutil.UsageReleasedForRequest(request, demand, api.EmptyResource()) {
		return unschedulable.HintWakeup, nil
	}
	return unschedulable.HintSkip, nil
}

// podGroupDemand returns the resource dimensions a PodGroup withdraws from the
// fair-share pool when it stops consuming, taken from its minimum resources.
func podGroupDemand(pg *api.PodGroup) *api.Resource {
	// Missing MinResources gives no safe dimension information; the caller
	// recognizes the empty result and wakes conservatively.
	if pg.Spec.MinResources == nil {
		return api.EmptyResource()
	}
	return api.NewResource(*pg.Spec.MinResources)
}
