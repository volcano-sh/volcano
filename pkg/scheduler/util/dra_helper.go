package util

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	corev1nodeaffinity "k8s.io/component-helpers/scheduling/corev1/nodeaffinity"
	"k8s.io/dynamic-resource-allocation/cel"
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

// IdleCheck validates whether the task's DRA device requirements can be satisfied on the
// node via the node.Idle path.
func IdleCheck(ctx context.Context, task *api.TaskInfo, node *api.NodeInfo, draManager fwk.SharedDRAManager) ([]resourceapi.AllocationResult, error) {
	// A nil draManager means DRA is disabled; the check degrades to a pass-through.
	if draManager == nil {
		return nil, nil
	}
	state, err := gatherAllocatedState(draManager)
	if err != nil {
		return nil, err
	}
	return checkFit(ctx, task, node, state, draManager)
}

// FutureIdleCheck validates whether the task's DRA device requirements can be satisfied on
// the node via the node.FutureIdle() path. victims, when non-empty, are preemption/reclaim
// victims selected but not yet evicted: their devices are simulated as released (on top of
// node.DraReleasing) so the check reflects the node state after all victims are evicted.
// Victims holding shared claims never reach this point — SharedClaimEvictableFn vetoes them
// during victim selection.
func FutureIdleCheck(ctx context.Context, task *api.TaskInfo, node *api.NodeInfo, draManager fwk.SharedDRAManager, victims []*api.TaskInfo) ([]resourceapi.AllocationResult, error) {
	// A nil draManager means DRA is disabled; the check degrades to a pass-through.
	if draManager == nil {
		return nil, nil
	}
	state, err := gatherAllocatedState(draManager)
	if err != nil {
		return nil, err
	}
	node.DraReleasing.RemoveFromAllocatedState(state)
	if victims != nil {
		devices := api.NewDRADeviceState()
		for _, victim := range victims {
			devices.Merge(victim.DRAAllocatedDevices)
		}
		devices.RemoveFromAllocatedState(state)
	}
	node.DraPipelined.InsertToAllocatedState(state)
	return checkFit(ctx, task, node, state, draManager)
}

func CheckSharedClaim(evictee *api.TaskInfo, draManager fwk.SharedDRAManager) bool {
	taskUIDs := sets.New[types.UID](evictee.Pod.UID)
	for _, claimKey := range evictee.ResourceClaimKeys {
		ns, name, ok := SplitClaimKey(claimKey)
		if !ok {
			continue
		}
		claim, err := draManager.ResourceClaims().Get(ns, name)
		if err != nil || claim == nil {
			klog.V(4).Infof("DRA shared-claim check: failed to get claim %s: %v", claimKey, err)
			continue
		}
		if claim.Status.Allocation == nil {
			continue
		}
		if !ClaimFullyHeldByTask(claim, taskUIDs) {
			return true
		}
	}
	return false
}

// gatherAllocatedState fetches the global allocated-device snapshot.
func gatherAllocatedState(draManager fwk.SharedDRAManager) (*structured.AllocatedState, error) {
	allocatedState, err := draManager.ResourceClaims().GatherAllocatedState()
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
func checkFit(
	ctx context.Context,
	task *api.TaskInfo,
	node *api.NodeInfo,
	state *structured.AllocatedState,
	draManager fwk.SharedDRAManager,
) ([]resourceapi.AllocationResult, error) {
	claimsToAllocate := make([]*resourceapi.ResourceClaim, 0, len(task.ResourceClaimKeys))
	for _, claimKey := range task.ResourceClaimKeys {
		ns, name, ok := SplitClaimKey(claimKey)
		if !ok {
			klog.V(4).Infof("DRA check: skip unparseable claim key %q", claimKey)
			continue
		}
		claim, err := draManager.ResourceClaims().Get(ns, name)
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

	allocator, err := newAllocator(ctx, state, draManager)
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

	task.DRAPreAlloc = api.DRADeviceStateFromAllocationResults(results, draFeatures().EnableDRAConsumableCapacity)
	return results, nil
}

// newAllocator creates an isolated DRA allocator based on the (adjusted) global snapshot.
func newAllocator(ctx context.Context, state *structured.AllocatedState, draManager fwk.SharedDRAManager) (structured.Allocator, error) {
	slices, err := draManager.ResourceSlices().ListWithDeviceTaintRules()
	if err != nil {
		return nil, fmt.Errorf("failed to get ResourceSlices: %w", err)
	}

	allocator, err := structured.NewAllocator(ctx, dynamicresources.AllocatorFeatures(draFeatures()), *state,
		draManager.DeviceClasses(), slices, cel.NewCache(10, cel.Features{EnableConsumableCapacity: draFeatures().EnableDRAConsumableCapacity}))
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
