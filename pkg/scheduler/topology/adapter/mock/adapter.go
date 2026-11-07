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

// Package mock provides an in-memory allocation adapter for simulation and tests.
package mock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
)

// Adapter is an in-memory exact allocation adapter. It reserves the selected
// device IDs in its own memory and returns immutable handoffs, but nothing on a
// Node enforces them, so it is only suitable for KWOK simulation and tests.
type Adapter struct {
	contract adapter.DeviceIdentityContract
	now      func() time.Time

	mutex        sync.Mutex
	nextToken    uint64
	reservations map[string]*reservation
	deviceOwners map[api.DeviceID]string
}

// reservation is one accepted plan and the devices it owns.
type reservation struct {
	planDigest string
	plan       api.TopologyPlacementPlan
	deviceIDs  []api.DeviceID
	placements map[types.UID]api.TopologyTaskPlacement
	state      adapter.AdapterReservationLifecycleState
}

var _ adapter.AllocationAdapter = (*Adapter)(nil)

// New creates a mock adapter for one device-ID contract. A nil clock uses
// time.Now.
func New(contract adapter.DeviceIdentityContract, now func() time.Time) (*Adapter, error) {
	if err := contract.Validate(); err != nil {
		return nil, err
	}
	if now == nil {
		now = time.Now
	}
	return &Adapter{
		contract:     contract,
		now:          now,
		reservations: make(map[string]*reservation),
		deviceOwners: make(map[api.DeviceID]string),
	}, nil
}

// Capabilities reports every hard-mode guarantee. The guarantees hold only
// inside this adapter's memory.
func (mockAdapter *Adapter) Capabilities() adapter.AdapterCapabilities {
	return adapter.AdapterCapabilities{
		ExactDeviceIDs:        true,
		AtomicReservation:     true,
		ImmutableBindHandoff:  true,
		AllocationObservation: true,
		ReleaseObservation:    true,
		Recovery:              true,
	}
}

// IdentityContract returns the only device-ID scheme this adapter accepts.
func (mockAdapter *Adapter) IdentityContract() adapter.DeviceIdentityContract {
	return mockAdapter.contract
}

// ValidateTopology accepts facts from the contract's provider whose devices
// for the contract's resource use the contract's identity namespace.
func (mockAdapter *Adapter) ValidateTopology(ctx context.Context, facts api.NodeTopologyFacts) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if facts.ProviderID != mockAdapter.contract.ProviderID {
		return adapter.NewError(adapter.ErrorStaleInventory, fmt.Errorf("facts from provider %q do not match adapter provider %q", facts.ProviderID, mockAdapter.contract.ProviderID))
	}
	for _, device := range facts.Devices {
		if device.ResourceName != mockAdapter.contract.ResourceName {
			continue
		}
		if !mockAdapter.ownsDeviceID(device.ID) {
			return adapter.NewError(adapter.ErrorStaleInventory, fmt.Errorf("device %q does not use the adapter identity contract", device.ID.String()))
		}
	}
	return nil
}

// Reserve holds every device selected by plan, or returns an error without
// holding any of them.
func (mockAdapter *Adapter) Reserve(ctx context.Context, plan api.TopologyPlacementPlan) (adapter.AdapterReservation, error) {
	if err := ctx.Err(); err != nil {
		return adapter.AdapterReservation{}, err
	}
	deviceIDs, placements, err := mockAdapter.planDevices(plan)
	if err != nil {
		return adapter.AdapterReservation{}, err
	}
	planDigest, err := digestPlan(plan)
	if err != nil {
		return adapter.AdapterReservation{}, adapter.NewError(adapter.ErrorBackendFailure, err)
	}

	mockAdapter.mutex.Lock()
	defer mockAdapter.mutex.Unlock()
	for _, deviceID := range deviceIDs {
		if owner, exists := mockAdapter.deviceOwners[deviceID]; exists {
			return adapter.AdapterReservation{}, adapter.NewError(adapter.ErrorReservationConflict, fmt.Errorf("device %q is already held by reservation %q", deviceID.String(), owner))
		}
	}
	mockAdapter.nextToken++
	token := fmt.Sprintf("mock-%s-%d", mockAdapter.contract.ResourceName, mockAdapter.nextToken)
	mockAdapter.reservations[token] = &reservation{
		planDigest: planDigest,
		plan:       plan.Clone(),
		deviceIDs:  deviceIDs,
		placements: placements,
		state:      adapter.AdapterReservationHeld,
	}
	for _, deviceID := range deviceIDs {
		mockAdapter.deviceOwners[deviceID] = token
	}
	return adapter.AdapterReservation{Token: token, PlanDigest: planDigest}, nil
}

// PrepareBind returns the handoff for one Pod of a held reservation. Repeated
// calls for the same Pod return the same handoff.
func (mockAdapter *Adapter) PrepareBind(ctx context.Context, handle adapter.AdapterReservation, placement api.TopologyTaskPlacement) (adapter.AllocationHandoff, error) {
	if err := ctx.Err(); err != nil {
		return adapter.AllocationHandoff{}, err
	}

	mockAdapter.mutex.Lock()
	defer mockAdapter.mutex.Unlock()
	held, err := mockAdapter.reservationFor(handle)
	if err != nil {
		return adapter.AllocationHandoff{}, err
	}
	planned, exists := held.placements[placement.PodUID]
	if !exists || planned.NodeName != placement.NodeName {
		return adapter.AllocationHandoff{}, adapter.NewError(adapter.ErrorBackendFailure, fmt.Errorf("reservation %q has no placement for Pod %q on Node %q", handle.Token, placement.PodUID, placement.NodeName))
	}
	held.state = adapter.AdapterReservationBinding
	return adapter.AllocationHandoff{
		ReservationToken: handle.Token,
		PlanDigest:       held.planDigest,
		PodUID:           planned.PodUID,
		NodeName:         planned.NodeName,
	}, nil
}

// Confirm treats every prepared handoff as enforced, because nothing outside
// this adapter can disagree with it. An unknown reservation is reported as
// released.
func (mockAdapter *Adapter) Confirm(ctx context.Context, handle adapter.AdapterReservation) (adapter.AllocationObservation, error) {
	if err := ctx.Err(); err != nil {
		return adapter.AllocationObservation{}, err
	}

	mockAdapter.mutex.Lock()
	defer mockAdapter.mutex.Unlock()
	observation := adapter.AllocationObservation{
		ReservationToken: handle.Token,
		PlanDigest:       handle.PlanDigest,
		ObservedAt:       mockAdapter.now(),
	}
	held, exists := mockAdapter.reservations[handle.Token]
	if !exists {
		observation.State = adapter.AllocationObservationReleased
		return observation, nil
	}
	if held.planDigest != handle.PlanDigest {
		return adapter.AllocationObservation{}, adapter.NewError(adapter.ErrorBackendFailure, fmt.Errorf("reservation %q has a different plan digest", handle.Token))
	}
	if held.state == adapter.AdapterReservationHeld {
		return adapter.AllocationObservation{}, adapter.NewError(adapter.ErrorBackendFailure, fmt.Errorf("reservation %q has no prepared bind handoff", handle.Token))
	}
	held.state = adapter.AdapterReservationAllocated
	observation.State = adapter.AllocationObservationAllocated
	return observation, nil
}

// Release frees every device of a reservation. Releasing an unknown
// reservation succeeds, so a repeated cleanup is safe.
func (mockAdapter *Adapter) Release(ctx context.Context, handle adapter.AdapterReservation) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	mockAdapter.mutex.Lock()
	defer mockAdapter.mutex.Unlock()
	held, exists := mockAdapter.reservations[handle.Token]
	if !exists {
		return nil
	}
	if held.planDigest != handle.PlanDigest {
		return adapter.NewError(adapter.ErrorBackendFailure, fmt.Errorf("reservation %q has a different plan digest", handle.Token))
	}
	for _, deviceID := range held.deviceIDs {
		delete(mockAdapter.deviceOwners, deviceID)
	}
	delete(mockAdapter.reservations, handle.Token)
	return nil
}

// Recover returns every active reservation in token order. Mock reservations
// live only in memory, so they never expire.
func (mockAdapter *Adapter) Recover(ctx context.Context) ([]adapter.AdapterReservationState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	mockAdapter.mutex.Lock()
	defer mockAdapter.mutex.Unlock()
	tokens := make([]string, 0, len(mockAdapter.reservations))
	for token := range mockAdapter.reservations {
		tokens = append(tokens, token)
	}
	sort.Strings(tokens)
	states := make([]adapter.AdapterReservationState, 0, len(tokens))
	for _, token := range tokens {
		held := mockAdapter.reservations[token]
		states = append(states, adapter.AdapterReservationState{
			Reservation: adapter.AdapterReservation{Token: token, PlanDigest: held.planDigest},
			Plan:        held.plan.Clone(),
			State:       held.state,
		})
	}
	return states, nil
}

// planDevices checks that a plan selects only this adapter's resource and
// identity contract, and indexes its devices and Pod placements.
func (mockAdapter *Adapter) planDevices(plan api.TopologyPlacementPlan) ([]api.DeviceID, map[types.UID]api.TopologyTaskPlacement, error) {
	var deviceIDs []api.DeviceID
	seen := make(map[api.DeviceID]struct{})
	placements := make(map[types.UID]api.TopologyTaskPlacement, len(plan.Placements))
	for _, placement := range plan.Placements {
		if placement.PodUID == "" || placement.NodeName == "" {
			return nil, nil, adapter.NewError(adapter.ErrorUnsupportedExactSelection, fmt.Errorf("placement for Task %q has no Pod UID or Node", placement.TaskID))
		}
		if _, exists := placements[placement.PodUID]; exists {
			return nil, nil, adapter.NewError(adapter.ErrorUnsupportedExactSelection, fmt.Errorf("plan places Pod %q more than once", placement.PodUID))
		}
		placements[placement.PodUID] = placement.Clone()
		for _, assignment := range placement.Assignments {
			if assignment.ResourceName != mockAdapter.contract.ResourceName {
				return nil, nil, adapter.NewError(adapter.ErrorUnsupportedExactSelection, fmt.Errorf("assignment for resource %q does not match adapter resource %q", assignment.ResourceName, mockAdapter.contract.ResourceName))
			}
			for _, deviceID := range assignment.DeviceIDs {
				if !mockAdapter.ownsDeviceID(deviceID) {
					return nil, nil, adapter.NewError(adapter.ErrorUnsupportedExactSelection, fmt.Errorf("device %q does not use the adapter identity contract", deviceID.String()))
				}
				if _, exists := seen[deviceID]; exists {
					return nil, nil, adapter.NewError(adapter.ErrorUnsupportedExactSelection, fmt.Errorf("plan selects device %q more than once", deviceID.String()))
				}
				seen[deviceID] = struct{}{}
				deviceIDs = append(deviceIDs, deviceID)
			}
		}
	}
	if len(deviceIDs) == 0 {
		return nil, nil, adapter.NewError(adapter.ErrorUnsupportedExactSelection, fmt.Errorf("plan selects no %q devices", mockAdapter.contract.ResourceName))
	}
	return deviceIDs, placements, nil
}

// reservationFor returns the reservation named by a handle whose digest
// matches. The caller holds the adapter mutex.
func (mockAdapter *Adapter) reservationFor(handle adapter.AdapterReservation) (*reservation, error) {
	held, exists := mockAdapter.reservations[handle.Token]
	if !exists {
		return nil, adapter.NewError(adapter.ErrorBackendFailure, fmt.Errorf("reservation %q does not exist", handle.Token))
	}
	if held.planDigest != handle.PlanDigest {
		return nil, adapter.NewError(adapter.ErrorBackendFailure, fmt.Errorf("reservation %q has a different plan digest", handle.Token))
	}
	return held, nil
}

// ownsDeviceID reports whether a device ID uses this adapter's provider and
// identity namespace.
func (mockAdapter *Adapter) ownsDeviceID(deviceID api.DeviceID) bool {
	return deviceID.ProviderID == mockAdapter.contract.ProviderID && deviceID.Namespace == mockAdapter.contract.Namespace
}

// digestPlan returns a stable digest of the complete immutable plan.
func digestPlan(plan api.TopologyPlacementPlan) (string, error) {
	encoded, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("encode placement plan: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
