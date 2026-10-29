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
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
)

const defaultDeviceTopologyReservationCleanupTimeout = 30 * time.Second

type preparedDeviceTopologyReservation struct {
	groups []preparedDeviceTopologyAdapterReservation
}

type preparedDeviceTopologyAdapterReservation struct {
	resourceName      corev1.ResourceName
	allocationAdapter adapter.AllocationAdapter
	plan              schedulingapi.TopologyPlacementPlan
	deviceIDs         []schedulingapi.DeviceID
	facts             []schedulingapi.NodeTopologyFacts
}

type preparedDeviceTopologyReservationBuilder struct {
	allocationAdapter adapter.AllocationAdapter
	plan              schedulingapi.TopologyPlacementPlan
	deviceIDs         []schedulingapi.DeviceID
	factsByNode       map[schedulingapi.ProviderNodeKey]schedulingapi.NodeTopologyFacts
	placementIndexes  map[schedulingapi.TaskID]int
}

type completedDeviceTopologyAdapterReservation struct {
	prepared    preparedDeviceTopologyAdapterReservation
	reservation adapter.AdapterReservation
}

// deviceTopologyReservationOwner identifies the exact adapter reservation
// currently entitled to release a device from the scheduler availability
// ledger. It prevents a stale reservation handle from freeing a newer hold.
type deviceTopologyReservationOwner struct {
	resourceName corev1.ResourceName
	token        string
	planDigest   string
}

// ReserveDeviceTopologyPlan validates an exact placement against the current
// cache state, holds all selected devices, and asks each authoritative adapter
// to reserve its complete resource fragment. If any adapter fails, successful
// fragments are released and every unresolved device is failed closed.
func (sc *SchedulerCache) ReserveDeviceTopologyPlan(ctx context.Context, plan schedulingapi.TopologyPlacementPlan) (schedulingapi.TopologyReservation, error) {
	plan = plan.Clone()

	sc.deviceTopologyReservationMutex.Lock()
	defer sc.deviceTopologyReservationMutex.Unlock()

	sc.Mutex.Lock()
	state := sc.ensureDeviceTopologyState()
	registry := sc.deviceTopologyAdapterRegistry
	prepared, err := state.prepareDeviceTopologyReservation(registry, plan, sc.deviceTopologyReservationTime())
	sc.Mutex.Unlock()
	if err != nil {
		return schedulingapi.TopologyReservation{}, err
	}

	if err := validatePreparedDeviceTopologyReservation(ctx, prepared); err != nil {
		return schedulingapi.TopologyReservation{}, err
	}

	sc.Mutex.Lock()
	state = sc.ensureDeviceTopologyState()
	prepared, err = state.prepareDeviceTopologyReservation(sc.deviceTopologyAdapterRegistry, plan, sc.deviceTopologyReservationTime())
	if err == nil {
		err = state.holdDeviceTopologyReservation(prepared)
	}
	sc.Mutex.Unlock()
	if err != nil {
		return schedulingapi.TopologyReservation{}, err
	}

	completed := make([]completedDeviceTopologyAdapterReservation, 0, len(prepared.groups))
	for groupIndex := range prepared.groups {
		group := prepared.groups[groupIndex]
		reservation, reserveErr := group.allocationAdapter.Reserve(ctx, group.plan.Clone())
		if reserveErr != nil {
			if reservation.Token != "" {
				completed = append(completed, completedDeviceTopologyAdapterReservation{prepared: group, reservation: reservation})
			}
			rollbackErr := sc.rollbackDeviceTopologyReservation(ctx, prepared, completed, &group, reserveErr)
			return schedulingapi.TopologyReservation{}, joinDeviceTopologyReservationErrors("reserve device topology plan", reserveErr, rollbackErr)
		}
		if reservation.Token == "" || reservation.PlanDigest == "" {
			if reservation.Token != "" {
				completed = append(completed, completedDeviceTopologyAdapterReservation{prepared: group, reservation: reservation})
			}
			invalidReservationErr := fmt.Errorf("allocation adapter for resource %q returned an incomplete reservation handle", group.resourceName)
			rollbackErr := sc.rollbackDeviceTopologyReservation(ctx, prepared, completed, &group, invalidReservationErr)
			return schedulingapi.TopologyReservation{}, joinDeviceTopologyReservationErrors("reserve device topology plan", invalidReservationErr, rollbackErr)
		}
		completed = append(completed, completedDeviceTopologyAdapterReservation{prepared: group, reservation: reservation})
	}

	result := schedulingapi.TopologyReservation{
		Plan:                plan.Clone(),
		AdapterReservations: make([]schedulingapi.TopologyAdapterReservation, 0, len(completed)),
	}
	for _, completedReservation := range completed {
		result.AdapterReservations = append(result.AdapterReservations, schedulingapi.TopologyAdapterReservation{
			ResourceName: completedReservation.prepared.resourceName,
			Token:        completedReservation.reservation.Token,
			PlanDigest:   completedReservation.reservation.PlanDigest,
		})
	}

	sc.Mutex.Lock()
	state = sc.ensureDeviceTopologyState()
	err = state.recordDeviceTopologyReservation(prepared, completed, sc.deviceTopologyReservationTime())
	sc.Mutex.Unlock()
	if err != nil {
		rollbackErr := sc.rollbackDeviceTopologyReservation(ctx, prepared, completed, nil, err)
		return schedulingapi.TopologyReservation{}, joinDeviceTopologyReservationErrors("record device topology reservation", err, rollbackErr)
	}
	return result, nil
}

// ReleaseDeviceTopologyReservation releases all adapter fragments for a prior
// reservation. A release error leaves only that fragment's devices
// Unreconciled, preventing unsafe reuse until backend recovery resolves it.
func (sc *SchedulerCache) ReleaseDeviceTopologyReservation(ctx context.Context, reservation schedulingapi.TopologyReservation) error {
	if sc == nil {
		return fmt.Errorf("device topology release requires a SchedulerCache")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reservation = reservation.Clone()

	sc.deviceTopologyReservationMutex.Lock()
	defer sc.deviceTopologyReservationMutex.Unlock()

	sc.Mutex.Lock()
	state := sc.ensureDeviceTopologyState()
	prepared, err := prepareDeviceTopologyReservationRelease(state, sc.deviceTopologyAdapterRegistry, reservation)
	sc.Mutex.Unlock()
	if err != nil {
		return err
	}

	targets := availabilityTargetsForPreparedRelease(prepared)
	var releaseErrors []error
	for groupIndex := len(prepared.groups) - 1; groupIndex >= 0; groupIndex-- {
		group := prepared.groups[groupIndex]
		if err := group.allocationAdapter.Release(ctx, adapter.AdapterReservation{Token: group.token, PlanDigest: group.planDigest}); err != nil {
			releaseErrors = append(releaseErrors, fmt.Errorf("release device topology reservation for resource %q: %w", group.resourceName, err))
			setDeviceAvailabilityTarget(targets, group.deviceIDs, schedulingapi.DeviceUnreconciled)
		}
	}

	if err := sc.applyDeviceTopologyAvailabilityTargets(state, targets); err != nil {
		releaseErrors = append(releaseErrors, err)
	}
	if len(releaseErrors) > 0 {
		return errors.Join(releaseErrors...)
	}
	return nil
}

type preparedDeviceTopologyRelease struct {
	groups []preparedDeviceTopologyReleaseGroup
}

type preparedDeviceTopologyReleaseGroup struct {
	resourceName      corev1.ResourceName
	allocationAdapter adapter.AllocationAdapter
	token             string
	planDigest        string
	deviceIDs         []schedulingapi.DeviceID
}

// prepareDeviceTopologyReservation validates an exact plan against the
// current immutable snapshot and ledger, then creates one complete child plan
// per configured resource adapter. The caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) prepareDeviceTopologyReservation(registry *adapter.Registry, plan schedulingapi.TopologyPlacementPlan, at time.Time) (preparedDeviceTopologyReservation, error) {
	if registry == nil {
		return preparedDeviceTopologyReservation{}, fmt.Errorf("no device topology allocation adapter registry is configured")
	}
	currentRevision := schedulingapi.TopologyRevision(0)
	if state.snapshot != nil {
		currentRevision = state.snapshot.Revision()
	}
	// A plan from an older revision is accepted when every selected device
	// still passes the live checks below. The revision changes whenever any
	// device or Node changes, so requiring the same revision would reject plans
	// for unrelated changes, such as another Pod releasing its devices.
	if plan.Revision == 0 || plan.Revision > currentRevision {
		return preparedDeviceTopologyReservation{}, fmt.Errorf("device topology reservation plan revision %d is not a published revision, the current revision is %d", plan.Revision, currentRevision)
	}
	if plan.JobID == "" || plan.SchedulingUnitID == "" || len(plan.Placements) == 0 {
		return preparedDeviceTopologyReservation{}, fmt.Errorf("device topology reservation plan has no placements")
	}
	fabricMemberDomainIDs, err := state.validateDeviceTopologyReservationFabric(plan.FabricID, at)
	if err != nil {
		return preparedDeviceTopologyReservation{}, err
	}

	builders := make(map[corev1.ResourceName]*preparedDeviceTopologyReservationBuilder)
	seenTasks := make(map[schedulingapi.TaskID]struct{}, len(plan.Placements))
	seenDevices := make(map[schedulingapi.DeviceID]struct{})
	for _, placement := range plan.Placements {
		if err := validateTopologyTaskPlacement(placement); err != nil {
			return preparedDeviceTopologyReservation{}, err
		}
		if _, exists := seenTasks[placement.TaskID]; exists {
			return preparedDeviceTopologyReservation{}, fmt.Errorf("device topology reservation plan repeats Task %q", placement.TaskID)
		}
		seenTasks[placement.TaskID] = struct{}{}

		for _, assignment := range placement.Assignments {
			if err := validateTopologyContainerAssignment(assignment, placement); err != nil {
				return preparedDeviceTopologyReservation{}, err
			}
			builder, err := state.validateTopologyAssignment(registry, plan, placement, assignment, at, fabricMemberDomainIDs, seenDevices, builders)
			if err != nil {
				return preparedDeviceTopologyReservation{}, err
			}
			builder.addPlacementAssignment(placement, assignment)
		}
	}

	resourceNames := make([]corev1.ResourceName, 0, len(builders))
	for resourceName := range builders {
		resourceNames = append(resourceNames, resourceName)
	}
	sort.Slice(resourceNames, func(left, right int) bool {
		return resourceNames[left] < resourceNames[right]
	})

	result := preparedDeviceTopologyReservation{groups: make([]preparedDeviceTopologyAdapterReservation, 0, len(resourceNames))}
	for _, resourceName := range resourceNames {
		builder := builders[resourceName]
		factKeys := make([]schedulingapi.ProviderNodeKey, 0, len(builder.factsByNode))
		for key := range builder.factsByNode {
			factKeys = append(factKeys, key)
		}
		sort.Slice(factKeys, func(left, right int) bool {
			if factKeys[left].ProviderID != factKeys[right].ProviderID {
				return factKeys[left].ProviderID < factKeys[right].ProviderID
			}
			return string(factKeys[left].NodeUID) < string(factKeys[right].NodeUID)
		})
		facts := make([]schedulingapi.NodeTopologyFacts, 0, len(factKeys))
		for _, key := range factKeys {
			facts = append(facts, builder.factsByNode[key].Clone())
		}
		result.groups = append(result.groups, preparedDeviceTopologyAdapterReservation{
			resourceName:      resourceName,
			allocationAdapter: builder.allocationAdapter,
			plan:              builder.plan,
			deviceIDs:         append([]schedulingapi.DeviceID(nil), builder.deviceIDs...),
			facts:             facts,
		})
	}
	return result, nil
}

// validateTopologyTaskPlacement verifies that one plan entry identifies a Pod,
// its selected Node, and at least one exact device assignment.
func validateTopologyTaskPlacement(placement schedulingapi.TopologyTaskPlacement) error {
	if placement.TaskID == "" || placement.PodUID == "" || placement.NodeName == "" || placement.NodeUID == "" {
		return fmt.Errorf("device topology reservation plan has an incomplete Task placement")
	}
	if len(placement.Assignments) == 0 {
		return fmt.Errorf("device topology reservation plan has no assignments for Task %q", placement.TaskID)
	}
	return nil
}

// validateTopologyContainerAssignment verifies one resource selection belongs
// to its Task placement and contains one or more exact device IDs.
func validateTopologyContainerAssignment(assignment schedulingapi.TopologyContainerAssignment, placement schedulingapi.TopologyTaskPlacement) error {
	if assignment.ResourceName == "" || assignment.ContainerName == "" || assignment.LocalDomainID.ProviderID == "" || assignment.LocalDomainID.Namespace == "" || assignment.LocalDomainID.NodeUID == "" || assignment.LocalDomainID.Value == "" {
		return fmt.Errorf("device topology reservation plan has an incomplete assignment for Task %q", placement.TaskID)
	}
	if assignment.LocalDomainID.NodeUID != placement.NodeUID {
		return fmt.Errorf("device topology reservation assignment for Task %q does not belong to Node UID %q", placement.TaskID, placement.NodeUID)
	}
	if len(assignment.DeviceIDs) == 0 {
		return fmt.Errorf("device topology reservation assignment for Task %q and resource %q has no device IDs", placement.TaskID, assignment.ResourceName)
	}
	return nil
}

// validateTopologyAssignment verifies selected devices are currently available
// and builds the exact child plan for their resource's allocation adapter.
// The caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) validateTopologyAssignment(registry *adapter.Registry, plan schedulingapi.TopologyPlacementPlan, placement schedulingapi.TopologyTaskPlacement, assignment schedulingapi.TopologyContainerAssignment, at time.Time, fabricMemberDomainIDs map[schedulingapi.DeviceDomainID]struct{}, seenDevices map[schedulingapi.DeviceID]struct{}, builders map[corev1.ResourceName]*preparedDeviceTopologyReservationBuilder) (*preparedDeviceTopologyReservationBuilder, error) {
	if fabricMemberDomainIDs != nil {
		if _, exists := fabricMemberDomainIDs[assignment.LocalDomainID]; !exists {
			return nil, fmt.Errorf("device topology reservation fabric %q does not include local domain %q", plan.FabricID.Value, assignment.LocalDomainID.Value)
		}
	}
	builder := builders[assignment.ResourceName]
	for _, deviceID := range assignment.DeviceIDs {
		if _, exists := seenDevices[deviceID]; exists {
			return nil, fmt.Errorf("device topology reservation plan selects device %q more than once", deviceID.String())
		}
		seenDevices[deviceID] = struct{}{}

		device, exists := state.snapshot.Device(deviceID)
		if !exists || device.Health != schedulingapi.DeviceHealthy || device.ResourceName != assignment.ResourceName || device.NodeName != placement.NodeName || device.NodeUID != placement.NodeUID || device.LocalDomainID != assignment.LocalDomainID {
			return nil, fmt.Errorf("device topology reservation plan selects unavailable device %q", deviceID.String())
		}
		if state.ledger.states[deviceID] != schedulingapi.DeviceAvailable {
			return nil, fmt.Errorf("device topology reservation plan selects non-available device %q", deviceID.String())
		}
		if _, exists := state.reservationOwners[deviceID]; exists {
			return nil, fmt.Errorf("device topology reservation plan selects device %q with an unresolved reservation owner", deviceID.String())
		}

		allocationAdapter, found := registry.HardTopologyAdapter(deviceID.ProviderID, assignment.ResourceName, deviceID.Namespace)
		if !found {
			return nil, fmt.Errorf("resource %q device %q has no hard-topology allocation adapter", assignment.ResourceName, deviceID.String())
		}
		if state.snapshot == nil || !state.snapshot.IsProviderNodeFreshAt(deviceID.ProviderID, device.NodeUID, at) {
			return nil, fmt.Errorf("device topology reservation plan selects device %q with stale provider facts", deviceID.String())
		}

		if builder == nil {
			builder = &preparedDeviceTopologyReservationBuilder{
				allocationAdapter: allocationAdapter,
				plan: schedulingapi.TopologyPlacementPlan{
					JobID:            plan.JobID,
					SchedulingUnitID: plan.SchedulingUnitID,
					Revision:         plan.Revision,
					FabricID:         plan.FabricID,
				},
				factsByNode:      make(map[schedulingapi.ProviderNodeKey]schedulingapi.NodeTopologyFacts),
				placementIndexes: make(map[schedulingapi.TaskID]int),
			}
			builders[assignment.ResourceName] = builder
		} else if contract := builder.allocationAdapter.IdentityContract(); !contract.Matches(deviceID.ProviderID, assignment.ResourceName, deviceID.Namespace) {
			return nil, fmt.Errorf("resource %q device assignments use different allocation adapters", assignment.ResourceName)
		}

		factsKey := schedulingapi.ProviderNodeKey{ProviderID: deviceID.ProviderID, NodeUID: device.NodeUID}
		facts, exists := state.facts[factsKey]
		if !exists {
			return nil, fmt.Errorf("device topology reservation plan has no accepted facts for device %q", deviceID.String())
		}
		builder.deviceIDs = append(builder.deviceIDs, deviceID)
		builder.factsByNode[factsKey] = facts.Clone()
	}
	return builder, nil
}

// validateDeviceTopologyReservationFabric verifies that an optional fabric in
// a final plan is current and fresh, and returns its member local domains so
// each selected domain can be checked against them. The caller holds
// SchedulerCache.Mutex.
func (state *deviceTopologyState) validateDeviceTopologyReservationFabric(fabricID schedulingapi.FabricDomainID, at time.Time) (map[schedulingapi.DeviceDomainID]struct{}, error) {
	if fabricID == (schedulingapi.FabricDomainID{}) {
		return nil, nil
	}
	if fabricID.ProviderID == "" || fabricID.Namespace == "" || fabricID.Value == "" {
		return nil, fmt.Errorf("device topology reservation plan has an incomplete fabric identity")
	}
	if state.snapshot == nil {
		return nil, fmt.Errorf("device topology reservation requires a topology snapshot for fabric %q", fabricID.Value)
	}
	fabric, exists := state.snapshot.Fabric(fabricID)
	if !exists {
		return nil, fmt.Errorf("device topology reservation fabric %q is not current", fabricID.Value)
	}
	if !state.snapshot.IsProviderNodeFreshAt(fabric.OwnerProviderID, fabric.OwnerNodeUID, at) {
		return nil, fmt.Errorf("device topology reservation fabric %q has stale owner facts", fabricID.Value)
	}

	memberDomainIDs := make(map[schedulingapi.DeviceDomainID]struct{})
	for _, member := range fabric.Members {
		if !state.snapshot.IsProviderNodeFreshAt(fabric.ID.ProviderID, member.NodeUID, at) {
			return nil, fmt.Errorf("device topology reservation fabric %q has stale member facts for Node UID %q", fabricID.Value, member.NodeUID)
		}
		for _, domainID := range member.LocalDomainIDs {
			memberDomainIDs[domainID] = struct{}{}
		}
	}
	return memberDomainIDs, nil
}

// recordDeviceTopologyReservation associates every locally held device with
// the backend handle that reserved it. The caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) recordDeviceTopologyReservation(prepared preparedDeviceTopologyReservation, completed []completedDeviceTopologyAdapterReservation, at time.Time) error {
	if err := state.validateHeldDevicesUnchanged(prepared, at); err != nil {
		return err
	}
	if len(prepared.groups) != len(completed) {
		return fmt.Errorf("device topology reservation completed %d adapter fragments, want %d", len(completed), len(prepared.groups))
	}
	for groupIndex, group := range prepared.groups {
		completedReservation := completed[groupIndex]
		if completedReservation.prepared.resourceName != group.resourceName || completedReservation.reservation.Token == "" || completedReservation.reservation.PlanDigest == "" {
			return fmt.Errorf("device topology reservation has an incomplete adapter result for resource %q", group.resourceName)
		}
		for _, deviceID := range group.deviceIDs {
			if state.ledger.states[deviceID] != schedulingapi.DeviceHeld {
				return fmt.Errorf("device topology reservation device %q is no longer held", deviceID.String())
			}
			if _, exists := state.reservationOwners[deviceID]; exists {
				return fmt.Errorf("device topology reservation device %q already has an owner", deviceID.String())
			}
		}
	}
	for groupIndex, group := range prepared.groups {
		completedReservation := completed[groupIndex]
		owner := deviceTopologyReservationOwner{
			resourceName: group.resourceName,
			token:        completedReservation.reservation.Token,
			planDigest:   completedReservation.reservation.PlanDigest,
		}
		for _, deviceID := range group.deviceIDs {
			state.reservationOwners[deviceID] = owner
		}
	}
	return nil
}

// addPlacementAssignment appends one assignment to this resource adapter's
// child plan, creating its Task placement the first time it is encountered.
func (builder *preparedDeviceTopologyReservationBuilder) addPlacementAssignment(placement schedulingapi.TopologyTaskPlacement, assignment schedulingapi.TopologyContainerAssignment) {
	placementIndex, exists := builder.placementIndexes[placement.TaskID]
	if !exists {
		placementIndex = len(builder.plan.Placements)
		builder.placementIndexes[placement.TaskID] = placementIndex
		builder.plan.Placements = append(builder.plan.Placements, schedulingapi.TopologyTaskPlacement{
			TaskID:   placement.TaskID,
			PodUID:   placement.PodUID,
			NodeName: placement.NodeName,
			NodeUID:  placement.NodeUID,
		})
	}
	builder.plan.Placements[placementIndex].Assignments = append(builder.plan.Placements[placementIndex].Assignments, assignment.Clone())
}

// holdDeviceTopologyReservation makes every validated device unavailable to
// new scheduler plans before any adapter Reserve call starts. The caller holds
// SchedulerCache.Mutex.
func (state *deviceTopologyState) holdDeviceTopologyReservation(prepared preparedDeviceTopologyReservation) error {
	for _, group := range prepared.groups {
		for _, deviceID := range group.deviceIDs {
			if state.ledger.states[deviceID] != schedulingapi.DeviceAvailable {
				return fmt.Errorf("device topology reservation device %q is no longer available", deviceID.String())
			}
			if _, exists := state.reservationOwners[deviceID]; exists {
				return fmt.Errorf("device topology reservation device %q already has an owner", deviceID.String())
			}
		}
	}
	for _, group := range prepared.groups {
		for _, deviceID := range group.deviceIDs {
			if err := state.ledger.transition(deviceID, schedulingapi.DeviceHeld); err != nil {
				return err
			}
		}
	}
	state.publish()
	return nil
}

// validateHeldDevicesUnchanged checks, after the adapters reserved a plan,
// that every held device is still healthy, still in its planned local domain
// on its planned Node, and has fresh provider facts. Changes to other devices
// or Nodes do not matter. The caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) validateHeldDevicesUnchanged(prepared preparedDeviceTopologyReservation, at time.Time) error {
	for _, group := range prepared.groups {
		for _, placement := range group.plan.Placements {
			for _, assignment := range placement.Assignments {
				for _, deviceID := range assignment.DeviceIDs {
					device, exists := state.snapshot.Device(deviceID)
					if !exists || device.Health != schedulingapi.DeviceHealthy || device.NodeUID != placement.NodeUID || device.LocalDomainID != assignment.LocalDomainID {
						return fmt.Errorf("device topology reservation device %q changed while reserving exact devices", deviceID.String())
					}
					if !state.snapshot.IsProviderNodeFreshAt(deviceID.ProviderID, device.NodeUID, at) {
						return fmt.Errorf("device topology reservation device %q has stale provider facts after reserving exact devices", deviceID.String())
					}
				}
			}
		}
	}
	return nil
}

// validatePreparedDeviceTopologyReservation asks every selected adapter to
// verify its source topology before the cache holds any device IDs.
func validatePreparedDeviceTopologyReservation(ctx context.Context, prepared preparedDeviceTopologyReservation) error {
	for _, group := range prepared.groups {
		for _, facts := range group.facts {
			if err := group.allocationAdapter.ValidateTopology(ctx, facts.Clone()); err != nil {
				return fmt.Errorf("validate device topology for resource %q on Node %q: %w", group.resourceName, facts.NodeName, err)
			}
		}
	}
	return nil
}

// rollbackDeviceTopologyReservation compensates completed adapter reservations
// after a later fragment fails and leaves uncertain devices unreconciled.
func (sc *SchedulerCache) rollbackDeviceTopologyReservation(ctx context.Context, prepared preparedDeviceTopologyReservation, completed []completedDeviceTopologyAdapterReservation, failedGroup *preparedDeviceTopologyAdapterReservation, reserveErr error) error {
	cleanupContext, cancel := newDeviceTopologyReservationCleanupContext(ctx)
	defer cancel()

	targets := availabilityTargetsForPreparedReservation(prepared)
	if failedGroup != nil && !reservationFailureIsKnownSafe(reserveErr) {
		setDeviceAvailabilityTarget(targets, failedGroup.deviceIDs, schedulingapi.DeviceUnreconciled)
	}

	var rollbackErrors []error
	for completedIndex := len(completed) - 1; completedIndex >= 0; completedIndex-- {
		completedReservation := completed[completedIndex]
		if err := completedReservation.prepared.allocationAdapter.Release(cleanupContext, completedReservation.reservation); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("release device topology reservation for resource %q: %w", completedReservation.prepared.resourceName, err))
			setDeviceAvailabilityTarget(targets, completedReservation.prepared.deviceIDs, schedulingapi.DeviceUnreconciled)
		} else {
			setDeviceAvailabilityTarget(targets, completedReservation.prepared.deviceIDs, schedulingapi.DeviceAvailable)
		}
	}

	sc.Mutex.Lock()
	state := sc.ensureDeviceTopologyState()
	if err := sc.applyDeviceTopologyAvailabilityTargetsLocked(state, targets); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	sc.Mutex.Unlock()
	if len(rollbackErrors) > 0 {
		return errors.Join(rollbackErrors...)
	}
	return nil
}

// deviceTopologyReservationTime returns the current time used to enforce
// dynamic source freshness at the reservation correctness boundary.
func (sc *SchedulerCache) deviceTopologyReservationTime() time.Time {
	if sc.deviceTopologyNow != nil {
		return sc.deviceTopologyNow()
	}
	return time.Now()
}

// newDeviceTopologyReservationCleanupContext keeps rollback eligible to reach
// the backend after the scheduling attempt's context has been cancelled.
func newDeviceTopologyReservationCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), defaultDeviceTopologyReservationCleanupTimeout)
}

// reservationFailureIsKnownSafe reports whether an adapter confirmed that it
// did not reserve any device IDs for the failed fragment.
func reservationFailureIsKnownSafe(err error) bool {
	var adapterErr *adapter.Error
	return errors.As(err, &adapterErr) && (adapterErr.Reason == adapter.ErrorReservationConflict || adapterErr.Reason == adapter.ErrorUnsupportedExactSelection)
}

// availabilityTargetsForPreparedReservation starts a rollback target map with
// every held device returned to Available.
func availabilityTargetsForPreparedReservation(prepared preparedDeviceTopologyReservation) map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability {
	targets := make(map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability)
	for _, group := range prepared.groups {
		setDeviceAvailabilityTarget(targets, group.deviceIDs, schedulingapi.DeviceAvailable)
	}
	return targets
}

// availabilityTargetsForPreparedRelease starts a release target map with every
// reserved device returned to Available.
func availabilityTargetsForPreparedRelease(prepared preparedDeviceTopologyRelease) map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability {
	targets := make(map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability)
	for _, group := range prepared.groups {
		setDeviceAvailabilityTarget(targets, group.deviceIDs, schedulingapi.DeviceAvailable)
	}
	return targets
}

// setDeviceAvailabilityTarget applies one desired lifecycle state to each
// supplied device ID in a target map.
func setDeviceAvailabilityTarget(targets map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability, deviceIDs []schedulingapi.DeviceID, availability schedulingapi.DeviceAvailability) {
	for _, deviceID := range deviceIDs {
		targets[deviceID] = availability
	}
}

// applyDeviceTopologyAvailabilityTargets locks SchedulerCache before applying
// one validated set of device availability transitions.
func (sc *SchedulerCache) applyDeviceTopologyAvailabilityTargets(state *deviceTopologyState, targets map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability) error {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	return sc.applyDeviceTopologyAvailabilityTargetsLocked(state, targets)
}

// applyDeviceTopologyAvailabilityTargetsLocked validates and applies all
// availability transitions as one cache update. The caller holds
// SchedulerCache.Mutex.
func (sc *SchedulerCache) applyDeviceTopologyAvailabilityTargetsLocked(state *deviceTopologyState, targets map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability) error {
	if state != sc.ensureDeviceTopologyState() {
		return fmt.Errorf("device topology reservation state changed while applying availability targets")
	}
	deviceIDs := make([]schedulingapi.DeviceID, 0, len(targets))
	for deviceID := range targets {
		deviceIDs = append(deviceIDs, deviceID)
	}
	sort.Slice(deviceIDs, func(left, right int) bool {
		return deviceIDs[left].String() < deviceIDs[right].String()
	})
	for _, deviceID := range deviceIDs {
		current, exists := state.ledger.states[deviceID]
		if !exists {
			return fmt.Errorf("device topology reservation device %q has no availability ledger state", deviceID.String())
		}
		if !canTransitionDeviceAvailability(current, targets[deviceID]) {
			return fmt.Errorf("device topology reservation device %q cannot transition from %q to %q", deviceID.String(), current, targets[deviceID])
		}
	}

	changed := false
	for _, deviceID := range deviceIDs {
		if state.ledger.states[deviceID] == targets[deviceID] {
			continue
		}
		if err := state.ledger.transition(deviceID, targets[deviceID]); err != nil {
			return err
		}
		changed = true
	}
	for _, deviceID := range deviceIDs {
		if targets[deviceID] == schedulingapi.DeviceAvailable {
			delete(state.reservationOwners, deviceID)
		}
	}
	if changed {
		state.publish()
	}
	return nil
}

// prepareDeviceTopologyReservationRelease verifies a prior reservation still
// owns every selected device and groups its handles by allocation adapter. The
// caller holds SchedulerCache.Mutex.
func prepareDeviceTopologyReservationRelease(state *deviceTopologyState, registry *adapter.Registry, reservation schedulingapi.TopologyReservation) (preparedDeviceTopologyRelease, error) {
	if state == nil {
		return preparedDeviceTopologyRelease{}, fmt.Errorf("device topology reservation state is unavailable")
	}
	if registry == nil {
		return preparedDeviceTopologyRelease{}, fmt.Errorf("no device topology allocation adapter registry is configured")
	}
	if len(reservation.AdapterReservations) == 0 {
		return preparedDeviceTopologyRelease{}, fmt.Errorf("device topology reservation has no adapter handles")
	}

	deviceIDsByResource := make(map[corev1.ResourceName][]schedulingapi.DeviceID)
	seenDevices := make(map[schedulingapi.DeviceID]struct{})
	for _, placement := range reservation.Plan.Placements {
		if err := validateTopologyTaskPlacement(placement); err != nil {
			return preparedDeviceTopologyRelease{}, err
		}
		for _, assignment := range placement.Assignments {
			if err := validateTopologyContainerAssignment(assignment, placement); err != nil {
				return preparedDeviceTopologyRelease{}, err
			}
			for _, deviceID := range assignment.DeviceIDs {
				if _, exists := seenDevices[deviceID]; exists {
					return preparedDeviceTopologyRelease{}, fmt.Errorf("device topology reservation selects device %q more than once", deviceID.String())
				}
				seenDevices[deviceID] = struct{}{}
				deviceIDsByResource[assignment.ResourceName] = append(deviceIDsByResource[assignment.ResourceName], deviceID)
			}
		}
	}

	handlesByResource := make(map[corev1.ResourceName]schedulingapi.TopologyAdapterReservation, len(reservation.AdapterReservations))
	for _, handle := range reservation.AdapterReservations {
		if handle.ResourceName == "" || handle.Token == "" || handle.PlanDigest == "" {
			return preparedDeviceTopologyRelease{}, fmt.Errorf("device topology reservation has an incomplete adapter handle")
		}
		if _, exists := handlesByResource[handle.ResourceName]; exists {
			return preparedDeviceTopologyRelease{}, fmt.Errorf("device topology reservation repeats adapter handle for resource %q", handle.ResourceName)
		}
		handlesByResource[handle.ResourceName] = handle
	}

	resourceNames := make([]corev1.ResourceName, 0, len(deviceIDsByResource))
	for resourceName := range deviceIDsByResource {
		resourceNames = append(resourceNames, resourceName)
	}
	sort.Slice(resourceNames, func(left, right int) bool {
		return resourceNames[left] < resourceNames[right]
	})
	result := preparedDeviceTopologyRelease{groups: make([]preparedDeviceTopologyReleaseGroup, 0, len(resourceNames))}
	for _, resourceName := range resourceNames {
		handle, exists := handlesByResource[resourceName]
		if !exists {
			return preparedDeviceTopologyRelease{}, fmt.Errorf("device topology reservation has no adapter handle for resource %q", resourceName)
		}
		allocationAdapter, exists := registry.AdapterFor(resourceName)
		if !exists {
			return preparedDeviceTopologyRelease{}, fmt.Errorf("resource %q has no allocation adapter to release its reservation", resourceName)
		}
		result.groups = append(result.groups, preparedDeviceTopologyReleaseGroup{
			resourceName:      resourceName,
			allocationAdapter: allocationAdapter,
			token:             handle.Token,
			planDigest:        handle.PlanDigest,
			deviceIDs:         append([]schedulingapi.DeviceID(nil), deviceIDsByResource[resourceName]...),
		})
	}
	if len(handlesByResource) != len(result.groups) {
		return preparedDeviceTopologyRelease{}, fmt.Errorf("device topology reservation has an adapter handle without matching plan assignments")
	}
	if err := state.validateDeviceTopologyReservationOwners(result); err != nil {
		return preparedDeviceTopologyRelease{}, err
	}
	return result, nil
}

// validateDeviceTopologyReservationOwners ensures a release request still
// names every device held by each backend handle. The caller holds
// SchedulerCache.Mutex.
func (state *deviceTopologyState) validateDeviceTopologyReservationOwners(prepared preparedDeviceTopologyRelease) error {
	for _, group := range prepared.groups {
		expectedOwner := deviceTopologyReservationOwner{resourceName: group.resourceName, token: group.token, planDigest: group.planDigest}
		expectedDeviceIDs := make(map[schedulingapi.DeviceID]struct{}, len(group.deviceIDs))
		for _, deviceID := range group.deviceIDs {
			expectedDeviceIDs[deviceID] = struct{}{}
			availability, exists := state.ledger.states[deviceID]
			if !exists || !availability.IsLive() {
				return fmt.Errorf("device topology reservation device %q is not actively held", deviceID.String())
			}
			owner, exists := state.reservationOwners[deviceID]
			if !exists || owner != expectedOwner {
				return fmt.Errorf("device topology reservation handle no longer owns device %q", deviceID.String())
			}
		}
		for deviceID, owner := range state.reservationOwners {
			if owner != expectedOwner {
				continue
			}
			if _, exists := expectedDeviceIDs[deviceID]; !exists {
				return fmt.Errorf("device topology reservation handle for resource %q does not include all of its held devices", group.resourceName)
			}
		}
	}
	return nil
}

// joinDeviceTopologyReservationErrors preserves both the original failure and
// any unsuccessful rollback while adding operation context.
func joinDeviceTopologyReservationErrors(operation string, primary, rollback error) error {
	if rollback == nil {
		return fmt.Errorf("%s: %w", operation, primary)
	}
	return fmt.Errorf("%s: %w", operation, errors.Join(primary, rollback))
}
