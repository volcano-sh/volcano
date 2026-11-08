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

package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
)

const (
	// deviceTopologyPodReleaseAttempts is how many times the scheduler asks an
	// adapter to release one Pod's devices before it marks them Unreconciled.
	deviceTopologyPodReleaseAttempts = 5
	// deviceTopologyPodReleaseBaseDelay and deviceTopologyPodReleaseMaxDelay
	// bound the back-off between release attempts.
	deviceTopologyPodReleaseBaseDelay = time.Second
	deviceTopologyPodReleaseMaxDelay  = 30 * time.Second
)

// newDeviceTopologyPodReleaseQueue creates the queue of Pods whose devices
// wait for an adapter release.
func newDeviceTopologyPodReleaseQueue() workqueue.TypedRateLimitingInterface[types.UID] {
	return workqueue.NewTypedRateLimitingQueue(workqueue.NewTypedItemExponentialFailureRateLimiter[types.UID](deviceTopologyPodReleaseBaseDelay, deviceTopologyPodReleaseMaxDelay))
}

// releaseEndedPodDeviceTopologyLocked starts the release of a Pod's devices
// when the Pod succeeded, failed, or is gone. The caller holds
// SchedulerCache.Mutex.
func (sc *SchedulerCache) releaseEndedPodDeviceTopologyLocked(pod *v1.Pod) {
	if pod == nil {
		return
	}
	sc.startDeviceTopologyPodReleaseLocked(pod.UID)
}

// releaseFailedBindDeviceTopology starts the release of the exact devices of a
// Pod whose pre-bind or bind failed, so they do not stay Binding while the Pod
// is scheduled again.
func (sc *SchedulerCache) releaseFailedBindDeviceTopology(task *schedulingapi.TaskInfo) {
	// Without an allocation adapter no Pod holds devices. The registry is set
	// before Run, so this check needs no lock on the bind path.
	if task == nil || task.Pod == nil || sc.deviceTopologyAdapterRegistry == nil {
		return
	}
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	sc.startDeviceTopologyPodReleaseLocked(task.Pod.UID)
}

// startDeviceTopologyPodReleaseLocked queues the release of the devices a Pod
// holds. The Pod's record moves to the pending releases at once, so the Pod
// can be scheduled again while its old devices are released. The devices keep
// their state until the release worker marks them Releasing, just before it
// calls the adapter. A Pod that holds no devices is ignored. It does
// no adapter I/O. The caller holds SchedulerCache.Mutex.
func (sc *SchedulerCache) startDeviceTopologyPodReleaseLocked(podUID types.UID) {
	state := sc.deviceTopology
	if state == nil {
		return
	}
	allocations, holdsDevices := state.podAllocations[podUID]
	if !holdsDevices {
		return
	}
	delete(state.podAllocations, podUID)
	state.podReleases[podUID] = append(state.podReleases[podUID], allocations...)
	sc.deviceTopologyPodReleaseQueue.Add(podUID)
}

// processDeviceTopologyPodReleaseQueue releases the devices of one queued Pod.
// A failed release is retried with back-off; after the last attempt the
// devices become Unreconciled.
func (sc *SchedulerCache) processDeviceTopologyPodReleaseQueue() {
	queue := sc.deviceTopologyPodReleaseQueue
	podUID, shutdown := queue.Get()
	if shutdown {
		return
	}
	defer queue.Done(podUID)

	lastAttempt := queue.NumRequeues(podUID)+1 >= deviceTopologyPodReleaseAttempts
	pending, err := sc.releaseDeviceTopologyPod(context.Background(), podUID, lastAttempt)
	if err != nil {
		klog.ErrorS(err, "Failed to release device topology devices of a Pod", "podUID", podUID, "retry", pending)
	}
	if pending {
		queue.AddRateLimited(podUID)
		return
	}
	queue.Forget(podUID)
}

// releaseDeviceTopologyPod marks the pending devices of one Pod Releasing and
// asks the adapters to release them. Other Pods of the same reservations keep
// theirs. Released devices become Available. A failed release keeps the
// devices Releasing for a retry,
// or makes them Unreconciled on the last attempt, which keeps them unavailable
// until recovery resolves them. It reports whether the Pod still has devices
// to release. The release uses its own cleanup context.
//
// It does not hold the reservation mutex, so a slow adapter call never blocks
// a reservation of the scheduling path. That is safe: the devices are
// Releasing during the call, so no reservation can select them, and no other
// operation changes them.
func (sc *SchedulerCache) releaseDeviceTopologyPod(ctx context.Context, podUID types.UID, lastAttempt bool) (bool, error) {
	cleanupContext, cancel := newDeviceTopologyReservationCleanupContext(ctx)
	defer cancel()

	sc.Mutex.Lock()
	state := sc.ensureDeviceTopologyState()
	groups, releaseErrors := state.prepareDeviceTopologyPodRelease(sc.deviceTopologyAdapterRegistry, podUID)
	targets := make(map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability)
	for _, group := range groups {
		setDeviceAvailabilityTarget(targets, group.deviceIDs, schedulingapi.DeviceReleasing)
	}
	if err := sc.applyDeviceTopologyAvailabilityTargetsLocked(state, targets); err != nil {
		releaseErrors = append(releaseErrors, err)
	}
	sc.Mutex.Unlock()

	targets = make(map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability)
	done := make([]deviceTopologyReservationOwner, 0, len(groups))
	for _, group := range groups {
		owner := deviceTopologyReservationOwner{resourceName: group.resourceName, token: group.token, planDigest: group.planDigest}
		handle := adapter.AdapterReservation{Token: group.token, PlanDigest: group.planDigest}
		if err := group.allocationAdapter.ReleasePod(cleanupContext, handle, podUID); err != nil {
			releaseErrors = append(releaseErrors, fmt.Errorf("release device topology devices of Pod %q for resource %q: %w", podUID, group.resourceName, err))
			if !lastAttempt {
				continue
			}
			setDeviceAvailabilityTarget(targets, group.deviceIDs, schedulingapi.DeviceUnreconciled)
		} else {
			setDeviceAvailabilityTarget(targets, group.deviceIDs, schedulingapi.DeviceAvailable)
		}
		done = append(done, owner)
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	if err := sc.applyDeviceTopologyAvailabilityTargetsLocked(state, targets); err != nil {
		releaseErrors = append(releaseErrors, err)
	}
	for _, owner := range done {
		state.forgetPodRelease(podUID, owner)
	}
	_, pending := state.podReleases[podUID]
	return pending, errors.Join(releaseErrors...)
}

// prepareDeviceTopologyPodRelease finds the adapter for each pending share of
// a Pod. A share that no longer owns its devices, or has no adapter, cannot be
// released; it is dropped and reported. The caller holds
// SchedulerCache.Mutex.
func (state *deviceTopologyState) prepareDeviceTopologyPodRelease(registry *adapter.Registry, podUID types.UID) ([]preparedDeviceTopologyReleaseGroup, []error) {
	allocations := state.podReleases[podUID]
	groups := make([]preparedDeviceTopologyReleaseGroup, 0, len(allocations))
	var invalidErrors []error
	for _, allocation := range allocations {
		err := state.checkPodAllocation(podUID, allocation)
		var allocationAdapter adapter.AllocationAdapter
		if err == nil {
			if registry == nil {
				err = fmt.Errorf("no device topology allocation adapter registry is configured")
			} else if found, exists := registry.AdapterFor(allocation.owner.resourceName); !exists {
				err = fmt.Errorf("resource %q has no allocation adapter to release Pod %q", allocation.owner.resourceName, podUID)
			} else {
				allocationAdapter = found
			}
		}
		if err != nil {
			invalidErrors = append(invalidErrors, err)
			state.forgetPodRelease(podUID, allocation.owner)
			continue
		}
		groups = append(groups, preparedDeviceTopologyReleaseGroup{
			resourceName:      allocation.owner.resourceName,
			allocationAdapter: allocationAdapter,
			token:             allocation.owner.token,
			planDigest:        allocation.owner.planDigest,
			deviceIDs:         append([]schedulingapi.DeviceID(nil), allocation.deviceIDs...),
		})
	}
	return groups, invalidErrors
}

// checkPodAllocation verifies that one share of a Pod still owns live devices.
// The caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) checkPodAllocation(podUID types.UID, allocation deviceTopologyPodAllocation) error {
	for _, deviceID := range allocation.deviceIDs {
		availability, exists := state.ledger.states[deviceID]
		if !exists || !availability.IsLive() {
			return fmt.Errorf("device topology device %q of Pod %q is not actively held", deviceID.String(), podUID)
		}
		if state.reservationOwners[deviceID] != allocation.owner {
			return fmt.Errorf("device topology reservation of Pod %q no longer owns device %q", podUID, deviceID.String())
		}
	}
	return nil
}

// withoutReleasedPodDevices removes from a reservation the devices that no
// longer belong to anyone, because a Pod release already freed them. A batch
// can be rolled back after one of its Pods was deleted and released, and
// those devices must not stop the release of the rest. Devices owned by
// another reservation stay, so the release still rejects them. The caller
// holds SchedulerCache.Mutex.
func (state *deviceTopologyState) withoutReleasedPodDevices(reservation schedulingapi.TopologyReservation) schedulingapi.TopologyReservation {
	released := func(deviceID schedulingapi.DeviceID) bool {
		_, owned := state.reservationOwners[deviceID]
		return !owned && !state.ledger.states[deviceID].IsLive()
	}
	keptResources := make(map[v1.ResourceName]struct{})
	placements := make([]schedulingapi.TopologyTaskPlacement, 0, len(reservation.Plan.Placements))
	for _, placement := range reservation.Plan.Placements {
		assignments := make([]schedulingapi.TopologyContainerAssignment, 0, len(placement.Assignments))
		for _, assignment := range placement.Assignments {
			deviceIDs := make([]schedulingapi.DeviceID, 0, len(assignment.DeviceIDs))
			for _, deviceID := range assignment.DeviceIDs {
				if !released(deviceID) {
					deviceIDs = append(deviceIDs, deviceID)
				}
			}
			if len(deviceIDs) == 0 {
				continue
			}
			assignment.DeviceIDs = deviceIDs
			assignments = append(assignments, assignment)
			keptResources[assignment.ResourceName] = struct{}{}
		}
		if len(assignments) == 0 {
			continue
		}
		placement.Assignments = assignments
		placements = append(placements, placement)
	}
	handles := make([]schedulingapi.TopologyAdapterReservation, 0, len(reservation.AdapterReservations))
	for _, handle := range reservation.AdapterReservations {
		if _, kept := keptResources[handle.ResourceName]; kept {
			handles = append(handles, handle)
		}
	}
	reservation.Plan.Placements = placements
	reservation.AdapterReservations = handles
	return reservation
}

// forgetPodRelease drops a Pod's pending release for one reservation. The
// caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) forgetPodRelease(podUID types.UID, owner deviceTopologyReservationOwner) {
	forgetPodShare(state.podReleases, podUID, owner)
}

// forgetReservationPodAllocations drops every Pod's record and pending release
// for one released reservation. The caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) forgetReservationPodAllocations(owner deviceTopologyReservationOwner) {
	for podUID := range state.podAllocations {
		forgetPodShare(state.podAllocations, podUID, owner)
	}
	for podUID := range state.podReleases {
		forgetPodShare(state.podReleases, podUID, owner)
	}
}

// forgetPodShare removes a Pod's share of one reservation from a record map.
func forgetPodShare(records map[types.UID][]deviceTopologyPodAllocation, podUID types.UID, owner deviceTopologyReservationOwner) {
	allocations := records[podUID]
	kept := make([]deviceTopologyPodAllocation, 0, len(allocations))
	for _, allocation := range allocations {
		if allocation.owner != owner {
			kept = append(kept, allocation)
		}
	}
	if len(kept) == 0 {
		delete(records, podUID)
		return
	}
	records[podUID] = kept
}
