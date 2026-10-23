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

package predicates

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1nodeaffinity "k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
	"k8s.io/dynamic-resource-allocation/cel"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// draPreemptHelper provides DRA device availability checks during preemption/reclaim.
//
// The core mechanism is an in-memory snapshot: it fetches the global allocated-device
// snapshot via claimTracker.GatherAllocatedState(), adjusts the snapshot in memory based
// on node.DraReleasing (devices being released) and node.DraPipelined (devices reserved by
// Pipelined tasks), then creates an isolated allocator via structured.NewAllocator() to
// verify whether the task's DRA requirements can be satisfied.
// It never mutates the global sharedDRAManager, which naturally fits the Statement's
// Commit/Discard transaction semantics - no extra DRA state cleanup is needed when a
// preemption is rolled back.
//
// Two paths are provided (strictly aligned with the CPU/memory Idle/FutureIdle):
//   - Idle path: allocate directly on the current snapshot;
//   - FutureIdle path: snapshot - node.DraReleasing + node.DraPipelined.
type draPreemptHelper struct {
	draManager fwk.SharedDRAManager
	features   feature.Features
	celCache   *cel.Cache
	slices     []*resourceapi.ResourceSlice
}

func newDRAPreemptHelper(draManager fwk.SharedDRAManager, features feature.Features) *draPreemptHelper {
	return &draPreemptHelper{
		draManager: draManager,
		features:   features,
		celCache:   cel.NewCache(10, cel.Features{EnableConsumableCapacity: features.EnableDRAConsumableCapacity}),
	}
}

// IdleCheck validates whether the task's DRA device requirements can be satisfied on the
// node via the node.Idle path.
func (h *draPreemptHelper) IdleCheck(ctx context.Context, task *api.TaskInfo, node *api.NodeInfo) ([]resourceapi.AllocationResult, error) {
	state, err := h.gatherAllocatedState()
	if err != nil {
		return nil, err
	}
	return h.checkFit(ctx, task, node, state)
}

// FutureIdleCheck validates whether the task's DRA device requirements can be satisfied on
// the node via the node.FutureIdle() path.
func (h *draPreemptHelper) FutureIdleCheck(ctx context.Context, task *api.TaskInfo, node *api.NodeInfo) ([]resourceapi.AllocationResult, error) {
	state, err := h.gatherAllocatedState()
	if err != nil {
		return nil, err
	}
	node.DraReleasing.RemoveFromAllocatedState(state)
	node.DraPipelined.InsertToAllocatedState(state)
	return h.checkFit(ctx, task, node, state)
}

// gatherAllocatedState fetches the global allocated-device snapshot.
func (h *draPreemptHelper) gatherAllocatedState() (*structured.AllocatedState, error) {
	allocatedState, err := h.draManager.ResourceClaims().GatherAllocatedState()
	if err != nil {
		return nil, fmt.Errorf("failed to get DRA global allocated state snapshot: %w", err)
	}
	if allocatedState == nil {
		return nil, fmt.Errorf("DRA global allocated state snapshot is nil")
	}
	return allocatedState, nil
}

// checkFit is the core logic of the DRA validation;
// on success it writes the pre-allocation results into task.DRAPreAlloc, so that the
// NodeInfo can account for DraPipelined after the task is Pipelined.
func (h *draPreemptHelper) checkFit(
	ctx context.Context,
	task *api.TaskInfo,
	node *api.NodeInfo,
	state *structured.AllocatedState,
) ([]resourceapi.AllocationResult, error) {
	claimsToAllocate := make([]*resourceapi.ResourceClaim, 0, len(task.ResourceClaimKeys))
	for _, claimKey := range task.ResourceClaimKeys {
		ns, name, ok := splitClaimKey(claimKey)
		if !ok {
			klog.V(4).Infof("DRA check: skip unparseable claim key %q", claimKey)
			continue
		}
		claim, err := h.draManager.ResourceClaims().Get(ns, name)
		if err != nil {
			return nil, fmt.Errorf("failed to get task claim %s: %w", claimKey, err)
		}
		if claim == nil {
			return nil, fmt.Errorf("task claim %s does not exist", claimKey)
		}
		if claim.Status.Allocation != nil {
			if !claimAvailableOnNode(claim, node.Node) {
				return nil, fmt.Errorf("task claim %s allocated on another node, does not match node %s", claimKey, node.Name)
			}
			continue
		}
		claimsToAllocate = append(claimsToAllocate, claim)
	}

	if len(claimsToAllocate) == 0 {
		return nil, nil
	}

	allocator, err := h.newAllocator(ctx, state)
	if err != nil {
		return nil, err
	}

	results, err := allocator.Allocate(ctx, node.Node, claimsToAllocate)
	if err != nil {
		return nil, fmt.Errorf("DRA device allocation check failed: %w", err)
	}
	if len(results) != len(claimsToAllocate) {
		return nil, fmt.Errorf("node %s: %s for task %s/%s: allocated %d of %d claims",
			node.Name, api.DRAInsufficientDevicesMessage, task.Namespace, task.Name, len(results), len(claimsToAllocate))
	}

	task.DRAPreAlloc = api.DRADeviceStateFromAllocationResults(results, h.features.EnableDRAConsumableCapacity)
	return results, nil
}

func (h *draPreemptHelper) newAllocator(ctx context.Context, state *structured.AllocatedState) (structured.Allocator, error) {
	if h.slices == nil {
		slices, err := h.draManager.ResourceSlices().ListWithDeviceTaintRules()
		if err != nil {
			return nil, fmt.Errorf("failed to get ResourceSlices: %w", err)
		}
		h.slices = slices
	}

	allocator, err := structured.NewAllocator(ctx, dynamicresources.AllocatorFeatures(h.features), *state,
		h.draManager.DeviceClasses(), h.slices, h.celCache)
	if err != nil {
		return nil, fmt.Errorf("failed to create DRA allocator: %w", err)
	}
	return allocator, nil
}

// GetTaskAllocatedDevices extracts the releasable devices from the task's allocated claims.
func (h *draPreemptHelper) GetTaskAllocatedDevices(task *api.TaskInfo) *api.DRADeviceState {
	if h == nil || task == nil || task.Pod == nil {
		return nil
	}
	state := api.NewDRADeviceState()
	taskUIDs := sets.New[types.UID](task.Pod.UID)
	for _, claimKey := range task.ResourceClaimKeys {
		ns, name, ok := splitClaimKey(claimKey)
		if !ok {
			continue
		}
		claim, err := h.draManager.ResourceClaims().Get(ns, name)
		if err != nil || claim == nil {
			klog.V(4).Infof("DRA device extraction: failed to get claim %s: %v", claimKey, err)
			continue
		}
		if claim.Status.Allocation == nil {
			continue
		}
		if !claimFullyHeldByTask(claim, taskUIDs) {
			klog.V(4).Infof("DRA device extraction: shared claim %s still referenced by other pods, skip", claimKey)
			continue
		}
		state.Merge(api.DRADeviceStateFromAllocationResults(
			[]resourceapi.AllocationResult{*claim.Status.Allocation}, h.features.EnableDRAConsumableCapacity))
	}
	return state
}

// TaskHoldsSharedClaim reports whether the task holds an allocated DRA claim that is
// also referenced by other pods.
func (h *draPreemptHelper) TaskHoldsSharedClaim(task *api.TaskInfo) bool {
	if h == nil || task == nil || task.Pod == nil {
		return false
	}
	taskUIDs := sets.New[types.UID](task.Pod.UID)
	for _, claimKey := range task.ResourceClaimKeys {
		ns, name, ok := splitClaimKey(claimKey)
		if !ok {
			continue
		}
		claim, err := h.draManager.ResourceClaims().Get(ns, name)
		if err != nil || claim == nil {
			klog.V(4).Infof("DRA shared-claim check: failed to get claim %s: %v", claimKey, err)
			continue
		}
		if claim.Status.Allocation == nil {
			continue
		}
		if !claimFullyHeldByTask(claim, taskUIDs) {
			return true
		}
	}
	return false
}

// SharedClaimEvictableFn is the DRA device sharing check for preemption/reclaim
func (h *draPreemptHelper) SharedClaimEvictableFn(initiator *api.TaskInfo, evictees []*api.TaskInfo) ([]*api.TaskInfo, int) {
	if h == nil {
		return nil, util.Abstain
	}
	for _, evictee := range evictees {
		if initiator != nil && len(initiator.ResourceClaimKeys) > 0 && h.TaskHoldsSharedClaim(evictee) {
			klog.V(3).Infof("DRA preempt/reclaim: task %s/%s holds shared DRA claim, veto evicting it for task %s/%s",
				evictee.Namespace, evictee.Name, initiator.Namespace, initiator.Name)
			return nil, util.Reject
		}
	}
	return nil, util.Abstain
}

// claimFullyHeldByTask checks whether an allocated claim is referenced only by the given task.
func claimFullyHeldByTask(claim *resourceapi.ResourceClaim, taskUIDs sets.Set[types.UID]) bool {
	if len(claim.Status.ReservedFor) == 0 {
		return true
	}
	for _, ref := range claim.Status.ReservedFor {
		if !taskUIDs.Has(ref.UID) {
			return false
		}
	}
	return true
}

// claimAvailableOnNode validates whether the allocated claim's node selector matches the given node.
func claimAvailableOnNode(claim *resourceapi.ResourceClaim, node *v1.Node) bool {
	if claim.Status.Allocation == nil {
		return true
	}
	if claim.Status.Allocation.NodeSelector == nil {
		return true
	}
	selector, err := corev1nodeaffinity.NewNodeSelector(claim.Status.Allocation.NodeSelector)
	if err != nil {
		klog.V(4).Infof("DRA check: failed to parse node selector for claim %s/%s: %v",
			claim.Namespace, claim.Name, err)
		return false
	}
	return selector.Match(node)
}

// splitClaimKey splits a claim key in "namespace/claimName" format into namespace and name.
func splitClaimKey(key string) (namespace, name string, ok bool) {
	idx := strings.Index(key, "/")
	if idx <= 0 || idx == len(key)-1 {
		return "", "", false
	}
	return key[:idx], key[idx+1:], true
}
