package util

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/dynamic-resource-allocation/cel"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	// gatherAllocatedStateMaxRetries bounds the number of GatherAllocatedState retries.
	gatherAllocatedStateMaxRetries = 5
	// gatherAllocatedStateRetryInterval is the interval between two retries.
	gatherAllocatedStateRetryInterval = 20 * time.Millisecond
)

var (
	draCelCacheOnce sync.Once
	draCelCache     *cel.Cache
)

// BuildDRASnapshot performs one DRA accounting snapshot build and writes the
// results directly into the session nodes (nodeInfo):
//  1. GatherAllocatedState obtains the cluster-wide allocated DRA resources
//     (bounded retries, see gatherAllocatedStateMaxRetries);
//  2. List the cluster ResourceSlices and map the allocated DRA resources from
//     step 1 onto the nodes;
//  3. Gather all active ResourceClaims in the cluster (including in-flight
//     allocations), and build the referencing (non-Pending) tasks of each
//     ResourceClaim per node;
//  4. Compute DraReleasing for each node.
func BuildDRASnapshot(mgr fwk.SharedDRAManager, nodes map[string]*api.NodeInfo) error {
	if mgr == nil {
		return nil
	}

	// Gather the cluster-wide allocated DRA resources.
	allocatedState, err := gatherAllocatedState(mgr)
	if err != nil {
		return err
	}

	// List the ResourceSlices.
	resourceSlices, err := mgr.ResourceSlices().ListWithDeviceTaintRules()
	if err != nil {
		return fmt.Errorf("failed to get ResourceSlices: %w", err)
	}

	// Gather the active ResourceClaims (including in-flight allocations).
	activeClaims, err := gatherActiveClaims(mgr)
	if err != nil {
		return err
	}

	nSlices := nodeSlices{
		slices: resourceSlices,
	}
	nSlices.buildLocalDevices()

	for _, ni := range nodes {
		ni.DraSlices = nSlices.forNode(ni.Name)
		ni.ComputeAllocatedState(allocatedState)
		ni.ComputeActiveClaimRefs(activeClaims, nSlices.localDevicesByNode[ni.Name])
		ni.ComputeDraReleasing()
	}
	return nil
}

// sharedCELCache returns the process-wide shared DRA CEL compilation cache.
func sharedCELCache() *cel.Cache {
	draCelCacheOnce.Do(func() {
		draCelCache = cel.NewCache(10, cel.Features{EnableConsumableCapacity: draFeatures().EnableDRAConsumableCapacity})
	})
	return draCelCache
}

// gatherAllocatedState collects the cluster-wide allocated DRA resources, with
// bounded retries on conflicting concurrent modification errors.
func gatherAllocatedState(mgr fwk.SharedDRAManager) (*structured.AllocatedState, error) {
	for attempt := 0; attempt < gatherAllocatedStateMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(gatherAllocatedStateRetryInterval)
		}
		state, err := mgr.ResourceClaims().GatherAllocatedState()
		if err != nil {
			if strings.Contains(err.Error(), "conflicting concurrent modification") {
				klog.V(4).Info("Conflicting modification during GatherAllocatedState, trying again")
				continue
			}
			return nil, err
		}
		if state == nil {
			return nil, errors.New("nil allocated state")
		}
		return state, nil
	}
	return nil, fmt.Errorf("failed to gather DRA allocated state")
}

// gatherActiveClaims collects all active claims (status.Allocation != nil) in the
// cluster, including in-flight ones.
func gatherActiveClaims(mgr fwk.SharedDRAManager) (map[string]*api.ActiveClaim, error) {
	claims, err := mgr.ResourceClaims().List()
	if err != nil {
		return nil, fmt.Errorf("failed to list ResourceClaims: %w", err)
	}
	activeClaims := make(map[string]*api.ActiveClaim)
	for _, claim := range claims {
		if claim == nil {
			continue
		}
		activeClaim := claim
		claimKey := claim.Namespace + "/" + claim.Name
		if claim.Status.Allocation == nil {
			// Fill in the in-flight allocation: modify only a copy, never pollute the
			// informer-cached object.
			result := mgr.ResourceClaims().GetPendingAllocation(claim.UID)
			if result == nil {
				continue
			}
			activeClaim = claim.DeepCopy()
			activeClaim.Status.Allocation = result
		}
		activeClaims[claimKey] = &api.ActiveClaim{
			ResourceClaim: activeClaim,
			Refs:          make(map[api.TaskID]bool),
		}
	}
	return activeClaims, nil
}
