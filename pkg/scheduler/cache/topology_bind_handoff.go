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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
)

type preparedDeviceTopologyBind struct {
	groups []preparedDeviceTopologyBindGroup
}

type preparedDeviceTopologyBindGroup struct {
	resourceName      corev1.ResourceName
	allocationAdapter adapter.AllocationAdapter
	reservation       adapter.AdapterReservation
	deviceIDs         []schedulingapi.DeviceID
	placements        []schedulingapi.TopologyTaskPlacement
}

// deviceTopologyPodAllocation is one Pod's share of one adapter reservation
// after bind handoff.
type deviceTopologyPodAllocation struct {
	owner     deviceTopologyReservationOwner
	deviceIDs []schedulingapi.DeviceID
}

// PrepareDeviceTopologyReservationBind asks each reservation-owning adapter
// for immutable Pod-specific handoffs while every selected device remains
// Held. It performs adapter I/O outside SchedulerCache.Mutex and does not
// advance availability; CommitDeviceTopologyReservationBind does that only
// after every batch participant has prepared successfully.
func (sc *SchedulerCache) PrepareDeviceTopologyReservationBind(ctx context.Context, reservation schedulingapi.TopologyReservation) (map[schedulingapi.TaskID][]DeviceTopologyBindHandoff, error) {
	reservation = reservation.Clone()

	sc.deviceTopologyReservationMutex.Lock()
	defer sc.deviceTopologyReservationMutex.Unlock()

	sc.Mutex.Lock()
	prepared, err := prepareDeviceTopologyReservationBind(sc.ensureDeviceTopologyState(), sc.deviceTopologyAdapterRegistry, reservation)
	sc.Mutex.Unlock()
	if err != nil {
		return nil, err
	}

	handoffs := make(map[schedulingapi.TaskID][]DeviceTopologyBindHandoff)
	for _, group := range prepared.groups {
		for _, placement := range group.placements {
			callContext, cancel := sc.newDeviceTopologyAdapterCallContext(ctx)
			handoff, err := group.allocationAdapter.PrepareBind(callContext, group.reservation, placement.Clone())
			cancel()
			if err != nil {
				return nil, fmt.Errorf("prepare device topology bind handoff for resource %q and Task %q: %w", group.resourceName, placement.TaskID, err)
			}
			if err := validateDeviceTopologyBindHandoff(group.resourceName, group.reservation, placement, handoff); err != nil {
				return nil, err
			}
			handoffs[placement.TaskID] = append(handoffs[placement.TaskID], DeviceTopologyBindHandoff{
				ResourceName: group.resourceName,
				Handoff:      handoff,
			})
		}
	}
	return handoffs, nil
}

// CommitDeviceTopologyReservationBind advances every still-owned reservation
// from Held to Binding after its complete batch has produced valid immutable
// handoffs, and records the devices each Pod holds. The backend reservation
// remains owned until a later allocation observation or release frees it.
func (sc *SchedulerCache) CommitDeviceTopologyReservationBind(_ context.Context, reservation schedulingapi.TopologyReservation) error {
	reservation = reservation.Clone()

	sc.deviceTopologyReservationMutex.Lock()
	defer sc.deviceTopologyReservationMutex.Unlock()

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	state := sc.ensureDeviceTopologyState()
	prepared, err := prepareDeviceTopologyReservationBind(state, sc.deviceTopologyAdapterRegistry, reservation)
	if err != nil {
		return err
	}
	podAllocations, err := state.podAllocationsForBind(prepared)
	if err != nil {
		return err
	}
	targets := make(map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability)
	for _, group := range prepared.groups {
		setDeviceAvailabilityTarget(targets, group.deviceIDs, schedulingapi.DeviceBinding)
	}
	if err := sc.applyDeviceTopologyAvailabilityTargetsLocked(state, targets); err != nil {
		return err
	}
	for podUID, allocations := range podAllocations {
		state.podAllocations[podUID] = allocations
	}
	return nil
}

// RollbackDeviceTopologyReservationBind releases an unbound or failed bind
// reservation. The release uses its own cleanup context.
func (sc *SchedulerCache) RollbackDeviceTopologyReservationBind(ctx context.Context, reservation schedulingapi.TopologyReservation) error {
	return sc.ReleaseDeviceTopologyReservation(ctx, reservation)
}

// releaseDeviceTopologyPod frees the devices one Pod holds after bind handoff.
// Other Pods of the same reservations keep theirs. A Pod that holds no devices
// is ignored. The Pod's record is dropped even if a release fails, so the Pod
// can be scheduled again; devices an adapter cannot release become
// Unreconciled instead, which keeps them unavailable until recovery resolves
// them. The release uses its own cleanup context.
func (sc *SchedulerCache) releaseDeviceTopologyPod(ctx context.Context, podUID types.UID) error {
	if sc == nil {
		return fmt.Errorf("device topology Pod release requires a SchedulerCache")
	}
	sc.Mutex.Lock()
	_, holdsDevices := sc.ensureDeviceTopologyState().podAllocations[podUID]
	sc.Mutex.Unlock()
	if !holdsDevices {
		return nil
	}
	cleanupContext, cancel := newDeviceTopologyReservationCleanupContext(ctx)
	defer cancel()

	sc.deviceTopologyReservationMutex.Lock()
	defer sc.deviceTopologyReservationMutex.Unlock()

	sc.Mutex.Lock()
	state := sc.ensureDeviceTopologyState()
	groups, err := state.prepareDeviceTopologyPodRelease(sc.deviceTopologyAdapterRegistry, podUID)
	delete(state.podAllocations, podUID)
	sc.Mutex.Unlock()
	if err != nil {
		return err
	}

	targets := make(map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability)
	var releaseErrors []error
	for _, group := range groups {
		handle := adapter.AdapterReservation{Token: group.token, PlanDigest: group.planDigest}
		if err := group.allocationAdapter.ReleasePod(cleanupContext, handle, podUID); err != nil {
			releaseErrors = append(releaseErrors, fmt.Errorf("release device topology devices of Pod %q for resource %q: %w", podUID, group.resourceName, err))
			setDeviceAvailabilityTarget(targets, group.deviceIDs, schedulingapi.DeviceUnreconciled)
			continue
		}
		setDeviceAvailabilityTarget(targets, group.deviceIDs, schedulingapi.DeviceAvailable)
	}

	sc.Mutex.Lock()
	if err := sc.applyDeviceTopologyAvailabilityTargetsLocked(state, targets); err != nil {
		releaseErrors = append(releaseErrors, err)
	}
	sc.Mutex.Unlock()
	return errors.Join(releaseErrors...)
}

// releaseFailedBindDeviceTopology frees the exact devices of a Pod whose
// pre-bind or bind failed, so they do not stay Binding while the Pod is
// scheduled again.
func (sc *SchedulerCache) releaseFailedBindDeviceTopology(ctx context.Context, task *schedulingapi.TaskInfo) {
	if task == nil || task.Pod == nil {
		return
	}
	if err := sc.releaseDeviceTopologyPod(ctx, task.Pod.UID); err != nil {
		klog.ErrorS(err, "Failed to release device topology devices of a Pod whose bind failed", "pod", klog.KObj(task.Pod))
	}
}

// podAllocationsForBind splits a prepared bind into the devices each Pod will
// hold. It rejects a Pod that still holds devices from an earlier bind. The
// caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) podAllocationsForBind(prepared preparedDeviceTopologyBind) (map[types.UID][]deviceTopologyPodAllocation, error) {
	result := make(map[types.UID][]deviceTopologyPodAllocation)
	for _, group := range prepared.groups {
		owner := deviceTopologyReservationOwner{resourceName: group.resourceName, token: group.reservation.Token, planDigest: group.reservation.PlanDigest}
		for _, placement := range group.placements {
			if _, exists := state.podAllocations[placement.PodUID]; exists {
				return nil, fmt.Errorf("device topology bind for Pod %q conflicts with devices the Pod still holds", placement.PodUID)
			}
			allocation := deviceTopologyPodAllocation{owner: owner}
			for _, assignment := range placement.Assignments {
				allocation.deviceIDs = append(allocation.deviceIDs, assignment.DeviceIDs...)
			}
			result[placement.PodUID] = append(result[placement.PodUID], allocation)
		}
	}
	return result, nil
}

// prepareDeviceTopologyPodRelease verifies that each recorded share of a Pod
// still owns its devices and finds the adapter that releases it. The caller
// holds SchedulerCache.Mutex.
func (state *deviceTopologyState) prepareDeviceTopologyPodRelease(registry *adapter.Registry, podUID types.UID) ([]preparedDeviceTopologyReleaseGroup, error) {
	if registry == nil {
		return nil, fmt.Errorf("no device topology allocation adapter registry is configured")
	}
	allocations := state.podAllocations[podUID]
	groups := make([]preparedDeviceTopologyReleaseGroup, 0, len(allocations))
	for _, allocation := range allocations {
		for _, deviceID := range allocation.deviceIDs {
			availability, exists := state.ledger.states[deviceID]
			if !exists || !availability.IsLive() {
				return nil, fmt.Errorf("device topology device %q of Pod %q is not actively held", deviceID.String(), podUID)
			}
			if state.reservationOwners[deviceID] != allocation.owner {
				return nil, fmt.Errorf("device topology reservation of Pod %q no longer owns device %q", podUID, deviceID.String())
			}
		}
		allocationAdapter, exists := registry.AdapterFor(allocation.owner.resourceName)
		if !exists {
			return nil, fmt.Errorf("resource %q has no allocation adapter to release Pod %q", allocation.owner.resourceName, podUID)
		}
		groups = append(groups, preparedDeviceTopologyReleaseGroup{
			resourceName:      allocation.owner.resourceName,
			allocationAdapter: allocationAdapter,
			token:             allocation.owner.token,
			planDigest:        allocation.owner.planDigest,
			deviceIDs:         append([]schedulingapi.DeviceID(nil), allocation.deviceIDs...),
		})
	}
	return groups, nil
}

// forgetPodAllocation drops a Pod's record for one released reservation. The
// caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) forgetPodAllocation(podUID types.UID, owner deviceTopologyReservationOwner) {
	allocations := state.podAllocations[podUID]
	kept := make([]deviceTopologyPodAllocation, 0, len(allocations))
	for _, allocation := range allocations {
		if allocation.owner != owner {
			kept = append(kept, allocation)
		}
	}
	if len(kept) == 0 {
		delete(state.podAllocations, podUID)
		return
	}
	state.podAllocations[podUID] = kept
}

// forgetReservationPodAllocations drops every Pod's record for one released
// reservation. The caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) forgetReservationPodAllocations(owner deviceTopologyReservationOwner) {
	for podUID := range state.podAllocations {
		state.forgetPodAllocation(podUID, owner)
	}
}

// prepareDeviceTopologyReservationBind verifies current reservation ownership,
// requires every selected device to remain Held, and groups resource-specific
// Task placements for the adapters that created the reservation handles. The
// caller holds SchedulerCache.Mutex.
func prepareDeviceTopologyReservationBind(state *deviceTopologyState, registry *adapter.Registry, reservation schedulingapi.TopologyReservation) (preparedDeviceTopologyBind, error) {
	preparedRelease, err := prepareDeviceTopologyReservationRelease(state, registry, reservation)
	if err != nil {
		return preparedDeviceTopologyBind{}, err
	}

	placementsByResource := make(map[corev1.ResourceName][]schedulingapi.TopologyTaskPlacement, len(preparedRelease.groups))
	placementIndexes := make(map[corev1.ResourceName]map[schedulingapi.TaskID]int, len(preparedRelease.groups))
	for _, placement := range reservation.Plan.Placements {
		for _, assignment := range placement.Assignments {
			indexes, exists := placementIndexes[assignment.ResourceName]
			if !exists {
				indexes = make(map[schedulingapi.TaskID]int)
				placementIndexes[assignment.ResourceName] = indexes
			}
			placementIndex, exists := indexes[placement.TaskID]
			if !exists {
				placementIndex = len(placementsByResource[assignment.ResourceName])
				indexes[placement.TaskID] = placementIndex
				placementsByResource[assignment.ResourceName] = append(placementsByResource[assignment.ResourceName], schedulingapi.TopologyTaskPlacement{
					TaskID:   placement.TaskID,
					PodUID:   placement.PodUID,
					NodeName: placement.NodeName,
					NodeUID:  placement.NodeUID,
				})
			}
			placementsByResource[assignment.ResourceName][placementIndex].Assignments = append(placementsByResource[assignment.ResourceName][placementIndex].Assignments, assignment.Clone())
		}
	}

	result := preparedDeviceTopologyBind{groups: make([]preparedDeviceTopologyBindGroup, 0, len(preparedRelease.groups))}
	for _, releaseGroup := range preparedRelease.groups {
		for _, deviceID := range releaseGroup.deviceIDs {
			if state.ledger.states[deviceID] != schedulingapi.DeviceHeld {
				return preparedDeviceTopologyBind{}, fmt.Errorf("device topology reservation device %q is not held for bind preparation", deviceID.String())
			}
		}
		placements := placementsByResource[releaseGroup.resourceName]
		if len(placements) == 0 {
			return preparedDeviceTopologyBind{}, fmt.Errorf("device topology reservation has no Task placement for resource %q", releaseGroup.resourceName)
		}
		result.groups = append(result.groups, preparedDeviceTopologyBindGroup{
			resourceName:      releaseGroup.resourceName,
			allocationAdapter: releaseGroup.allocationAdapter,
			reservation:       adapter.AdapterReservation{Token: releaseGroup.token, PlanDigest: releaseGroup.planDigest},
			deviceIDs:         append([]schedulingapi.DeviceID(nil), releaseGroup.deviceIDs...),
			placements:        append([]schedulingapi.TopologyTaskPlacement(nil), placements...),
		})
	}
	return result, nil
}

// validateDeviceTopologyBindHandoff ensures an adapter returned a handoff for
// the same immutable reservation, Pod, and Node that the cache validated.
func validateDeviceTopologyBindHandoff(resourceName corev1.ResourceName, reservation adapter.AdapterReservation, placement schedulingapi.TopologyTaskPlacement, handoff adapter.AllocationHandoff) error {
	if handoff.ReservationToken != reservation.Token || handoff.PlanDigest != reservation.PlanDigest {
		return fmt.Errorf("device topology adapter for resource %q returned a handoff for a different reservation", resourceName)
	}
	if handoff.PodUID != placement.PodUID || handoff.NodeName != placement.NodeName {
		return fmt.Errorf("device topology adapter for resource %q returned a handoff for a different Pod or Node", resourceName)
	}
	return nil
}
