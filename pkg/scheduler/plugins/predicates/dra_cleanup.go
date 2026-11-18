/*
Copyright 2024 The Volcano Authors.

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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/dynamic-resource-allocation/resourceclaim"
	"k8s.io/klog/v2"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// cleanupStaleDRAPendingAllocations removes stale inFlightAllocations from the DRA ResourceClaimTracker.
//
// In upstream k8s scheduler, inFlightAllocations are added during Reserve (when a claim allocation is computed)
// and removed either in Unreserve (on failure) or after PreBind succeeds (via AssumeClaimAfterAPICall + informer update).
//
// In Volcano's batch scheduling model, if a scheduling session ends (e.g., due to a job not being fully schedulable)
// without going through the Unreserve path for every reserved task, the inFlightAllocations entries remain in the
// shared DRAManager's claimTracker. Since the DRAManager is shared across sessions, these stale entries persist
// and cause subsequent scheduling cycles to fail with:
//
//	"resource claim <ns>/<name> is in the process of being allocated"
//
// or cause the DRA Filter to report "cannot allocate all claims" because ListAllAllocatedDevices() includes
// devices from stale inFlightAllocations, making the allocator think devices are occupied when they are not.
//
// This function iterates over all pods' resource claims and removes any stale pending allocations
// at the beginning of each new scheduling session, ensuring a clean state.
func cleanupStaleDRAPendingAllocations(draManager fwk.SharedDRAManager, jobs map[api.JobID]*api.JobInfo) {
	if draManager == nil {
		return
	}

	claimTracker := draManager.ResourceClaims()
	if claimTracker == nil {
		return
	}

	// At the beginning of each new scheduling session, ALL inFlightAllocations from the previous session
	// should be considered stale. This is because:
	// 1. Successfully bound tasks: their claims have been written to the API server and the informer cache
	//    has been updated, so the inFlightAllocation is no longer needed.
	// 2. Failed/discarded tasks: their claims were never actually allocated, so the inFlightAllocation
	//    is stale and must be removed.
	//
	// We iterate over ALL jobs (not just pending ones) to catch claims from tasks in any state,
	// including those that were allocated in a previous session but whose inFlightAllocation was
	// not properly cleaned up.
	cleanedCount := 0
	cleanedAllocatedCount := 0
	cleanedUnallocatedCount := 0
	for _, job := range jobs {
		for _, task := range job.Tasks {
			pod := task.Pod
			if pod == nil || len(pod.Spec.ResourceClaims) == 0 {
				continue
			}

			for i := range pod.Spec.ResourceClaims {
				claimName, _, err := resourceclaim.Name(pod, &pod.Spec.ResourceClaims[i])
				if err != nil || claimName == nil {
					continue
				}

				claim, err := claimTracker.Get(pod.Namespace, *claimName)
				if err != nil {
					continue
				}

				// Remove ANY pending allocation for this claim, regardless of whether the claim
				// is actually allocated or not. At the start of a new session:
				// - If the claim IS allocated (Status.Allocation != nil): the allocation is already
				//   persisted in etcd and reflected in the informer cache, so the inFlightAllocation
				//   is redundant and should be removed to prevent double-counting in
				//   ListAllAllocatedDevices().
				// - If the claim is NOT allocated (Status.Allocation == nil): the inFlightAllocation
				//   is stale from a failed previous session and must be removed.
				if claimTracker.ClaimHasPendingAllocation(types.UID(claim.UID)) {
					if deleted := claimTracker.RemoveClaimPendingAllocation(types.UID(claim.UID)); deleted {
						claimTracker.AssumedClaimRestore(claim.Namespace, claim.Name)
						cleanedCount++
						if claim.Status.Allocation != nil {
							cleanedAllocatedCount++
						} else {
							cleanedUnallocatedCount++
						}
						klog.V(4).Infof("Cleaned up stale DRA inFlightAllocation for claim %s/%s (UID: %s, actuallyAllocated: %v) - "+
							"this inFlightAllocation was leaked from a previous scheduling session and would cause "+
							"'cannot allocate all claims' errors by making DRA think devices are occupied when they are not",
							claim.Namespace, claim.Name, claim.UID, claim.Status.Allocation != nil)
					}
				}
			}
		}
	}
	if cleanedCount > 0 {
		klog.Infof("DRA inFlightAllocation cleanup: removed %d stale inFlightAllocations at session start "+
			"(%d from already-allocated claims causing double-counting, %d from unallocated claims). "+
			"These leaked inFlightAllocations would have caused DRA Filter to report 'cannot allocate all claims' "+
			"on nodes where devices are actually free.",
			cleanedCount, cleanedAllocatedCount, cleanedUnallocatedCount)
	}
}
