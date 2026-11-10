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
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
)

// recoveredDeviceTopologyReservation is one adapter reservation reported by
// Recover, split into the devices each Pod holds.
type recoveredDeviceTopologyReservation struct {
	owner        deviceTopologyReservationOwner
	availability schedulingapi.DeviceAvailability
	deviceIDs    []schedulingapi.DeviceID
	podUIDs      []types.UID
	podDevices   map[types.UID][]schedulingapi.DeviceID
	err          error
}

// beginDeviceTopologyAllocationRecovery marks every resource whose allocation
// adapter can recover its reservations as not recovered. The scheduler keeps
// device state only in memory, so after a restart or a leader change a device
// that a running Pod uses would otherwise look free. It must run before the
// first scheduling session.
func (sc *SchedulerCache) beginDeviceTopologyAllocationRecovery() {
	sc.deviceTopologyReservationMutex.Lock()
	defer sc.deviceTopologyReservationMutex.Unlock()

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	state := sc.ensureDeviceTopologyState()
	registry := sc.deviceTopologyAdapterRegistry
	changed := false
	for _, resourceName := range registry.ResourceNames() {
		allocationAdapter, _ := registry.AdapterFor(resourceName)
		if !allocationAdapter.Capabilities().Recovery {
			// Such an adapter never serves hard placement, so it holds nothing.
			continue
		}
		state.allocationRecoveryPending[resourceName] = struct{}{}
		changed = true
	}
	if changed {
		state.publish()
	}
}

// runDeviceTopologyAllocationRecovery recovers every pending resource and
// retries the failed ones with back-off until all are recovered or stopCh
// closes.
func (sc *SchedulerCache) runDeviceTopologyAllocationRecovery(stopCh <-chan struct{}) {
	delay := deviceTopologyPodRetryBaseDelay
	for sc.recoverDeviceTopologyAllocations(context.Background()) {
		select {
		case <-stopCh:
			return
		case <-time.After(delay):
		}
		delay = min(2*delay, deviceTopologyPodRetryMaxDelay)
	}
}

// recoverDeviceTopologyAllocations asks the adapter of each pending resource
// for the reservations it holds and restores them in the cache. A resource
// whose adapter fails stays pending, so its devices stay unavailable for exact
// placement. It reports whether any resource is still pending.
func (sc *SchedulerCache) recoverDeviceTopologyAllocations(ctx context.Context) bool {
	sc.deviceTopologyReservationMutex.Lock()
	defer sc.deviceTopologyReservationMutex.Unlock()

	sc.Mutex.Lock()
	state := sc.ensureDeviceTopologyState()
	registry := sc.deviceTopologyAdapterRegistry
	resourceNames := make([]corev1.ResourceName, 0, len(state.allocationRecoveryPending))
	for resourceName := range state.allocationRecoveryPending {
		resourceNames = append(resourceNames, resourceName)
	}
	sc.Mutex.Unlock()
	sort.Slice(resourceNames, func(left, right int) bool {
		return resourceNames[left] < resourceNames[right]
	})

	stillPending := false
	for _, resourceName := range resourceNames {
		allocationAdapter, exists := registry.AdapterFor(resourceName)
		contract, _ := registry.IdentityContractFor(resourceName)
		if !exists {
			klog.ErrorS(nil, "No allocation adapter can recover device topology allocations", "resource", resourceName)
			stillPending = true
			continue
		}
		callContext, cancel := newDeviceTopologyReservationCleanupContext(ctx)
		reservations, err := allocationAdapter.Recover(callContext)
		cancel()
		if err != nil {
			klog.ErrorS(err, "Failed to recover device topology allocations; the resource stays unavailable for exact placement", "resource", resourceName)
			stillPending = true
			continue
		}

		sc.Mutex.Lock()
		restoreErrors := state.restoreDeviceTopologyAllocations(contract, reservations, sc.deviceTopologyReservationTime().Add(deviceTopologyPodConfirmTimeout))
		delete(state.allocationRecoveryPending, resourceName)
		state.publish()
		sc.Mutex.Unlock()

		for _, restoreErr := range restoreErrors {
			klog.ErrorS(restoreErr, "Recovered device topology reservation is invalid; its devices are Unreconciled", "resource", resourceName)
		}
		klog.V(2).InfoS("Recovered device topology allocations", "resource", resourceName, "reservations", len(reservations), "invalid", len(restoreErrors))
	}
	return stillPending
}

// restoreDeviceTopologyAllocations rebuilds the ledger states, reservation
// owners, and Pod records of one resource from the reservations its adapter
// recovered. Binding devices must be confirmed again by confirmBy. A reservation
// that is malformed, or that claims a device another reservation also claims,
// cannot be trusted: its devices become Unreconciled with no owner, so they
// stay unavailable. It returns one error per invalid reservation. The caller
// holds SchedulerCache.Mutex.
func (state *deviceTopologyState) restoreDeviceTopologyAllocations(contract adapter.DeviceIdentityContract, reservations []adapter.AdapterReservationState, confirmBy time.Time) []error {
	recovered := make([]recoveredDeviceTopologyReservation, 0, len(reservations))
	claims := make(map[schedulingapi.DeviceID]int)
	for _, reservation := range reservations {
		parsed := parseRecoveredDeviceTopologyReservation(contract, reservation)
		for _, deviceID := range parsed.deviceIDs {
			claims[deviceID]++
			if state.ledger.states[deviceID].IsLive() {
				claims[deviceID]++
			}
		}
		recovered = append(recovered, parsed)
	}

	var restoreErrors []error
	for _, reservation := range recovered {
		if reservation.err == nil {
			for _, deviceID := range reservation.deviceIDs {
				if claims[deviceID] > 1 {
					reservation.err = fmt.Errorf("recovered reservation %q claims device %q that is also claimed elsewhere", reservation.owner.token, deviceID.String())
					break
				}
			}
		}
		if reservation.err != nil {
			restoreErrors = append(restoreErrors, reservation.err)
			for _, deviceID := range reservation.deviceIDs {
				_ = state.ledger.restore(deviceID, schedulingapi.DeviceUnreconciled)
			}
			continue
		}
		for _, deviceID := range reservation.deviceIDs {
			_ = state.ledger.restore(deviceID, reservation.availability)
			state.reservationOwners[deviceID] = reservation.owner
		}
		if reservation.availability == schedulingapi.DeviceHeld {
			// No Pod was bound before the restart, so no Pod record exists.
			continue
		}
		for _, podUID := range reservation.podUIDs {
			state.podAllocations[podUID] = append(state.podAllocations[podUID], deviceTopologyPodAllocation{
				owner:     reservation.owner,
				deviceIDs: append([]schedulingapi.DeviceID(nil), reservation.podDevices[podUID]...),
				confirmBy: confirmBy,
			})
		}
	}
	return restoreErrors
}

// parseRecoveredDeviceTopologyReservation checks one recovered reservation and
// splits its devices by Pod. Only devices that use the adapter's identity
// contract are collected, so an invalid reservation never marks another
// adapter's devices.
func parseRecoveredDeviceTopologyReservation(contract adapter.DeviceIdentityContract, reservation adapter.AdapterReservationState) recoveredDeviceTopologyReservation {
	parsed := recoveredDeviceTopologyReservation{
		owner:      deviceTopologyReservationOwner{resourceName: contract.ResourceName, token: reservation.Reservation.Token, planDigest: reservation.Reservation.PlanDigest},
		podDevices: make(map[types.UID][]schedulingapi.DeviceID),
	}
	fail := func(format string, args ...interface{}) {
		if parsed.err == nil {
			parsed.err = fmt.Errorf("recovered reservation %q: %s", reservation.Reservation.Token, fmt.Sprintf(format, args...))
		}
	}

	switch reservation.State {
	case adapter.AdapterReservationHeld:
		parsed.availability = schedulingapi.DeviceHeld
	case adapter.AdapterReservationBinding:
		parsed.availability = schedulingapi.DeviceBinding
	case adapter.AdapterReservationAllocated:
		parsed.availability = schedulingapi.DeviceAllocated
	default:
		fail("unknown state %q", reservation.State)
	}
	if reservation.Reservation.Token == "" || reservation.Reservation.PlanDigest == "" {
		fail("has an incomplete reservation handle")
	}

	seen := make(map[schedulingapi.DeviceID]struct{})
	for _, placement := range reservation.Plan.Placements {
		if placement.PodUID == "" {
			fail("has a placement without a Pod UID")
		}
		for _, assignment := range placement.Assignments {
			if assignment.ResourceName != contract.ResourceName {
				fail("assigns resource %q, want %q", assignment.ResourceName, contract.ResourceName)
			}
			for _, deviceID := range assignment.DeviceIDs {
				if !contract.Matches(deviceID.ProviderID, contract.ResourceName, deviceID.Namespace) {
					fail("selects device %q outside the adapter identity contract", deviceID.String())
					continue
				}
				if _, exists := seen[deviceID]; exists {
					fail("selects device %q more than once", deviceID.String())
					continue
				}
				seen[deviceID] = struct{}{}
				if _, exists := parsed.podDevices[placement.PodUID]; !exists {
					parsed.podUIDs = append(parsed.podUIDs, placement.PodUID)
				}
				parsed.deviceIDs = append(parsed.deviceIDs, deviceID)
				parsed.podDevices[placement.PodUID] = append(parsed.podDevices[placement.PodUID], deviceID)
			}
		}
	}
	if len(parsed.deviceIDs) == 0 {
		fail("selects no devices")
	}
	return parsed
}
