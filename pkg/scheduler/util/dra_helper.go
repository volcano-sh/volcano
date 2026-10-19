package util

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	corev1nodeaffinity "k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	k8sfeatures "k8s.io/kubernetes/pkg/features"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/dynamicresources"
	"k8s.io/kubernetes/pkg/scheduler/framework/plugins/feature"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func draFeatures() feature.Features {
	return feature.Features{
		EnableDRAAdminAccess:               utilfeature.DefaultFeatureGate.Enabled(k8sfeatures.DRAAdminAccess),
		EnableDRAPrioritizedList:           utilfeature.DefaultFeatureGate.Enabled(k8sfeatures.DRAPrioritizedList),
		EnableDRAPartitionableDevices:      utilfeature.DefaultFeatureGate.Enabled(k8sfeatures.DRAPartitionableDevices),
		EnableDRADeviceTaints:              utilfeature.DefaultFeatureGate.Enabled(k8sfeatures.DRADeviceTaints),
		EnableDRADeviceBindingConditions:   utilfeature.DefaultFeatureGate.Enabled(k8sfeatures.DRADeviceBindingConditions),
		EnableDRAResourceClaimDeviceStatus: utilfeature.DefaultFeatureGate.Enabled(k8sfeatures.DRAResourceClaimDeviceStatus),
		EnableDRAConsumableCapacity:        utilfeature.DefaultFeatureGate.Enabled(k8sfeatures.DRAConsumableCapacity),
	}
}

// DraIdleCheck validates whether the task's DRA device requirements can be satisfied on the
// node via the node.Idle path.
func DraIdleCheck(ctx context.Context, task *api.TaskInfo, node *api.NodeInfo, draManager fwk.SharedDRAManager) (*api.DRADeviceInfo, error) {
	// A nil draManager means DRA is disabled; the check degrades to a pass-through.
	if draManager == nil {
		return nil, nil
	}
	return checkFit(ctx, task, node, node.DraAllocated, draManager)
}

// FutureDRAIdleCheck validates whether the task's DRA device requirements can be satisfied on
// the node via the node.FutureIdle() path. victims, when non-empty, are preemption/reclaim
// victims selected but not yet evicted: their claim devices are simulated as released (on top
// of node.DraReleasing) so the check reflects the node state after all victims are evicted.
// The released set is computed by getVictimReleasedDevices, which deduplicates claims shared
// by multiple victims and skips every claim still referenced by a non-victim active task.
func FutureDRAIdleCheck(ctx context.Context, task *api.TaskInfo, node *api.NodeInfo, draManager fwk.SharedDRAManager, victims []*api.TaskInfo) (*api.DRADeviceInfo, error) {
	// A nil draManager means DRA is disabled; the check degrades to a pass-through.
	if draManager == nil {
		return nil, nil
	}
	base := node.DraAllocated.Clone()
	for _, v := range node.DraReleasing {
		base.Subtract(v)
	}
	if victims != nil {
		base.Subtract(getVictimReleasedDevices(node, victims))
	}
	base.Merge(node.DraPipelined)
	return checkFit(ctx, task, node, base, draManager)
}

// getVictimReleasedDevices aggregates the DRA devices that are actually released
// after the victims are evicted:
// 1. Deduplicate by claimKey: multiple victims may share the same resourceClaim
//    (e.g. together with the consumable capacity feature).
// 2. Eviction actually releases a claim's allocation only when every referrer of
//    the claim on the node is in the victims set.
func getVictimReleasedDevices(node *api.NodeInfo, victims []*api.TaskInfo) *api.DRADeviceInfo {
	devices := api.NewDRADeviceState()
	victimUIDs := make(map[api.TaskID]bool)
	claimKeys := make(map[string]bool)
	for _, victim := range victims {
		victimUIDs[victim.UID] = true
		for claimKey := range victim.DRAResourceClaimKeys {
			claimKeys[claimKey] = true
		}
	}
	for claimKey := range claimKeys {
		activeClaim, ok := node.DraActiveClaims[claimKey]
		if !ok {
			continue
		}
		releasable := true
		for taskId := range activeClaim.Refs {
			if _, ok := victimUIDs[taskId]; !ok {
				releasable = false
			}
		}
		if !releasable {
			continue
		}
		devices.Merge(api.DRADeviceStateFromClaimAllocation(activeClaim.ResourceClaim))
	}
	return devices
}

// checkFit is the core logic of the DRA validation.
func checkFit(
	ctx context.Context,
	task *api.TaskInfo,
	node *api.NodeInfo,
	allocatedState *api.DRADeviceInfo,
	draManager fwk.SharedDRAManager,
) (*api.DRADeviceInfo, error) {
	claimsToAllocate := make([]*resourceapi.ResourceClaim, 0, len(task.DRAResourceClaimKeys))
	for claimKey := range task.DRAResourceClaimKeys {
		activeClaim := node.DraActiveClaims[claimKey]
		if activeClaim == nil {
			resourceClaim, err := getResourceClaim(draManager, claimKey)
			if err != nil {
				return nil, err
			}
			activeClaim = &api.ActiveClaim{
				ResourceClaim: resourceClaim,
			}
		}
		if activeClaim.ResourceClaim.Status.Allocation != nil {
			if !claimAvailableOnNode(activeClaim.ResourceClaim, node.Node) {
				return nil, fmt.Errorf("task claim %s allocated on another node, does not match node %s", claimKey, node.Name)
			}
			continue
		}
		claimsToAllocate = append(claimsToAllocate, activeClaim.ResourceClaim)
	}

	if len(claimsToAllocate) == 0 {
		return nil, nil
	}

	allocator, err := newAllocator(ctx, allocatedState, node, draManager)
	if err != nil {
		return nil, err
	}

	results, err := allocator.Allocate(ctx, node.Node, claimsToAllocate)
	if err != nil {
		return nil, fmt.Errorf("DRA device allocation check failed: %w", err)
	}
	if results == nil {
		return nil, fmt.Errorf("node %s: %s for task %s/%s",
			node.Name, api.DRAInsufficientDevicesMessage, task.Namespace, task.Name)
	}

	return api.DRADeviceStateFromAllocationResults(results, draFeatures().EnableDRAConsumableCapacity), nil
}

func getResourceClaim(draManager fwk.SharedDRAManager, claimKey string) (*resourceapi.ResourceClaim, error) {
	ns, name, ok := SplitClaimKey(claimKey)
	if !ok {
		return nil, fmt.Errorf("invalid task claim key %q", claimKey)
	}
	claim, err := draManager.ResourceClaims().Get(ns, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get task claim %s: %w", claimKey, err)
	}
	return claim, nil
}

// newAllocator creates an allocator from the node's allocated devices and the
// node's ResourceSlices.
func newAllocator(ctx context.Context, allocatedState *api.DRADeviceInfo, node *api.NodeInfo, draManager fwk.SharedDRAManager) (structured.Allocator, error) {
	allocator, err := structured.NewAllocator(ctx, dynamicresources.AllocatorFeatures(draFeatures()), structured.AllocatedState{
		AllocatedDevices:         allocatedState.Devices,
		AllocatedSharedDeviceIDs: allocatedState.SharedDevices,
		AggregatedCapacity:       allocatedState.AggregatedCapacity,
	},
		draManager.DeviceClasses(), node.DraSlices, sharedCELCache())
	if err != nil {
		return nil, fmt.Errorf("failed to create DRA allocator: %w", err)
	}
	return allocator, nil
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
