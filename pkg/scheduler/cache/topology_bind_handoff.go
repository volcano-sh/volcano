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
	"fmt"

	corev1 "k8s.io/api/core/v1"

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
// handoffs. The backend reservation remains owned until a later allocation
// observation or rollback releases it.
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
	targets := make(map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability)
	for _, group := range prepared.groups {
		setDeviceAvailabilityTarget(targets, group.deviceIDs, schedulingapi.DeviceBinding)
	}
	return sc.applyDeviceTopologyAvailabilityTargetsLocked(state, targets)
}

// RollbackDeviceTopologyReservationBind releases an unbound or failed bind
// reservation. The release uses its own cleanup context.
func (sc *SchedulerCache) RollbackDeviceTopologyReservationBind(ctx context.Context, reservation schedulingapi.TopologyReservation) error {
	return sc.ReleaseDeviceTopologyReservation(ctx, reservation)
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
