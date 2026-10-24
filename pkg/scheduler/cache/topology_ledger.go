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
	"fmt"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

// deviceTopologyAvailabilityLedger owns mutable scheduler-side device
// availability. Its caller holds SchedulerCache.Mutex, so it has no separate
// lock. Reservation and adapter reconciliation operations are added later.
type deviceTopologyAvailabilityLedger struct {
	states map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability
}

// newDeviceTopologyAvailabilityLedger creates an empty ledger. Provider facts
// populate its initial Available or Unavailable states during reconciliation.
func newDeviceTopologyAvailabilityLedger() *deviceTopologyAvailabilityLedger {
	return &deviceTopologyAvailabilityLedger{
		states: make(map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability),
	}
}

// reconcile updates source-derived availability without overwriting active
// reservations or allocations. Removed free devices disappear, while live
// states remain as unavailable tombstones until future reconciliation resolves
// them.
func (ledger *deviceTopologyAvailabilityLedger) reconcile(facts map[schedulingapi.ProviderNodeKey]schedulingapi.NodeTopologyFacts) {
	currentHealth := make(map[schedulingapi.DeviceID]schedulingapi.DeviceHealth)
	for _, factSet := range facts {
		for _, device := range factSet.Devices {
			currentHealth[device.ID] = device.Health
			state, exists := ledger.states[device.ID]
			if !exists || !state.IsValid() {
				ledger.states[device.ID] = deviceAvailabilityForHealth(device.Health)
				continue
			}
			if device.Health != schedulingapi.DeviceHealthy && !state.IsLive() {
				ledger.states[device.ID] = schedulingapi.DeviceUnavailable
				continue
			}
			if device.Health == schedulingapi.DeviceHealthy && state == schedulingapi.DeviceUnavailable {
				ledger.states[device.ID] = schedulingapi.DeviceAvailable
			}
		}
	}

	for deviceID, state := range ledger.states {
		if _, exists := currentHealth[deviceID]; !exists && !state.IsLive() {
			delete(ledger.states, deviceID)
		}
	}
}

// transition moves one known device through a valid scheduler-owned lifecycle
// transition. It does not reserve or release a backend device by itself.
func (ledger *deviceTopologyAvailabilityLedger) transition(deviceID schedulingapi.DeviceID, next schedulingapi.DeviceAvailability) error {
	current, exists := ledger.states[deviceID]
	if !exists {
		return fmt.Errorf("device %q has no availability ledger state", deviceID.String())
	}
	if !next.IsValid() {
		return fmt.Errorf("device %q has unsupported availability state %q", deviceID.String(), next)
	}
	if !canTransitionDeviceAvailability(current, next) {
		return fmt.Errorf("device %q cannot transition from %q to %q", deviceID.String(), current, next)
	}
	ledger.states[deviceID] = next
	return nil
}

// snapshot returns an independent availability map for one immutable topology
// snapshot publication.
func (ledger *deviceTopologyAvailabilityLedger) snapshot() map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability {
	states := make(map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability, len(ledger.states))
	for deviceID, state := range ledger.states {
		states[deviceID] = state
	}
	return states
}

// deviceAvailabilityForHealth maps provider health to the initial ledger
// state for a device with no reservation or allocation lifecycle yet.
func deviceAvailabilityForHealth(health schedulingapi.DeviceHealth) schedulingapi.DeviceAvailability {
	if health == schedulingapi.DeviceHealthy {
		return schedulingapi.DeviceAvailable
	}
	return schedulingapi.DeviceUnavailable
}

// canTransitionDeviceAvailability defines the safe lifecycle changes used by
// future reservation and allocation-reconciliation paths.
func canTransitionDeviceAvailability(current, next schedulingapi.DeviceAvailability) bool {
	if current == next {
		return true
	}
	switch current {
	case schedulingapi.DeviceAvailable:
		return next == schedulingapi.DeviceHeld ||
			next == schedulingapi.DeviceBinding ||
			next == schedulingapi.DeviceAllocated ||
			next == schedulingapi.DeviceUnavailable ||
			next == schedulingapi.DeviceUnreconciled
	case schedulingapi.DeviceHeld:
		return next == schedulingapi.DeviceAvailable ||
			next == schedulingapi.DeviceBinding ||
			next == schedulingapi.DeviceAllocated ||
			next == schedulingapi.DeviceUnavailable ||
			next == schedulingapi.DeviceUnreconciled
	case schedulingapi.DeviceBinding:
		return next == schedulingapi.DeviceAvailable ||
			next == schedulingapi.DeviceAllocated ||
			next == schedulingapi.DeviceReleasing ||
			next == schedulingapi.DeviceUnavailable ||
			next == schedulingapi.DeviceUnreconciled
	case schedulingapi.DeviceAllocated:
		return next == schedulingapi.DeviceAvailable ||
			next == schedulingapi.DeviceReleasing ||
			next == schedulingapi.DeviceUnavailable ||
			next == schedulingapi.DeviceUnreconciled
	case schedulingapi.DeviceReleasing:
		return next == schedulingapi.DeviceAvailable ||
			next == schedulingapi.DeviceAllocated ||
			next == schedulingapi.DeviceUnavailable ||
			next == schedulingapi.DeviceUnreconciled
	case schedulingapi.DeviceUnavailable:
		return next == schedulingapi.DeviceAvailable ||
			next == schedulingapi.DeviceHeld ||
			next == schedulingapi.DeviceBinding ||
			next == schedulingapi.DeviceAllocated ||
			next == schedulingapi.DeviceUnreconciled
	case schedulingapi.DeviceUnreconciled:
		return next == schedulingapi.DeviceAvailable ||
			next == schedulingapi.DeviceHeld ||
			next == schedulingapi.DeviceBinding ||
			next == schedulingapi.DeviceAllocated ||
			next == schedulingapi.DeviceReleasing ||
			next == schedulingapi.DeviceUnavailable
	default:
		return false
	}
}
