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
	"strings"
	"testing"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

func TestDeviceTopologyAvailabilityLedgerReconcilesProviderHealth(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	update := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
	deviceID := update.Facts.Devices[0].ID
	facts := map[schedulingapi.ProviderNodeKey]schedulingapi.NodeTopologyFacts{{
		ProviderID: update.ProviderID,
		NodeUID:    update.NodeUID,
	}: *update.Facts}
	ledger := newDeviceTopologyAvailabilityLedger()

	ledger.reconcile(facts)
	if state := ledger.snapshot()[deviceID]; state != schedulingapi.DeviceAvailable {
		t.Fatalf("healthy device state = %q, want %q", state, schedulingapi.DeviceAvailable)
	}

	factSet := facts[schedulingapi.ProviderNodeKey{ProviderID: update.ProviderID, NodeUID: update.NodeUID}]
	factSet.Devices[0].Health = schedulingapi.DeviceUnhealthy
	facts[schedulingapi.ProviderNodeKey{ProviderID: update.ProviderID, NodeUID: update.NodeUID}] = factSet
	ledger.reconcile(facts)
	if state := ledger.snapshot()[deviceID]; state != schedulingapi.DeviceUnavailable {
		t.Fatalf("unhealthy device state = %q, want %q", state, schedulingapi.DeviceUnavailable)
	}

	factSet.Devices[0].Health = schedulingapi.DeviceHealthy
	facts[schedulingapi.ProviderNodeKey{ProviderID: update.ProviderID, NodeUID: update.NodeUID}] = factSet
	ledger.reconcile(facts)
	if state := ledger.snapshot()[deviceID]; state != schedulingapi.DeviceAvailable {
		t.Fatalf("recovered device state = %q, want %q", state, schedulingapi.DeviceAvailable)
	}
}

func TestDeviceTopologyAvailabilityLedgerRetainsLiveTombstones(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	update := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
	deviceID := update.Facts.Devices[0].ID
	facts := map[schedulingapi.ProviderNodeKey]schedulingapi.NodeTopologyFacts{{
		ProviderID: update.ProviderID,
		NodeUID:    update.NodeUID,
	}: *update.Facts}
	ledger := newDeviceTopologyAvailabilityLedger()
	ledger.reconcile(facts)
	if err := ledger.transition(deviceID, schedulingapi.DeviceHeld); err != nil {
		t.Fatalf("transition(DeviceHeld) error = %v", err)
	}

	ledger.reconcile(nil)
	states := ledger.snapshot()
	if state := states[deviceID]; state != schedulingapi.DeviceHeld {
		t.Fatalf("removed live device state = %q, want %q tombstone", state, schedulingapi.DeviceHeld)
	}
	states[deviceID] = schedulingapi.DeviceAvailable
	if state := ledger.snapshot()[deviceID]; state != schedulingapi.DeviceHeld {
		t.Fatalf("snapshot() returned mutable state: %q", state)
	}

	if err := ledger.transition(deviceID, schedulingapi.DeviceAvailable); err != nil {
		t.Fatalf("transition(DeviceAvailable) error = %v", err)
	}
	ledger.reconcile(nil)
	if _, found := ledger.snapshot()[deviceID]; found {
		t.Fatal("released removed device remains in ledger, want no free tombstone")
	}
}

func TestDeviceTopologyAvailabilityLedgerPreservesLiveStateOnHealthChange(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	update := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
	deviceID := update.Facts.Devices[0].ID
	key := schedulingapi.ProviderNodeKey{ProviderID: update.ProviderID, NodeUID: update.NodeUID}
	facts := map[schedulingapi.ProviderNodeKey]schedulingapi.NodeTopologyFacts{key: *update.Facts}
	ledger := newDeviceTopologyAvailabilityLedger()
	ledger.reconcile(facts)
	if err := ledger.transition(deviceID, schedulingapi.DeviceAllocated); err != nil {
		t.Fatalf("transition(DeviceAllocated) error = %v", err)
	}

	factSet := facts[key]
	factSet.Devices[0].Health = schedulingapi.DeviceUnhealthy
	facts[key] = factSet
	ledger.reconcile(facts)
	if state := ledger.snapshot()[deviceID]; state != schedulingapi.DeviceAllocated {
		t.Fatalf("health update changed live state to %q, want %q", state, schedulingapi.DeviceAllocated)
	}
}

func TestDeviceTopologyAvailabilityLedgerValidatesTransitions(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	update := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
	deviceID := update.Facts.Devices[0].ID
	ledger := newDeviceTopologyAvailabilityLedger()
	ledger.reconcile(map[schedulingapi.ProviderNodeKey]schedulingapi.NodeTopologyFacts{{
		ProviderID: update.ProviderID,
		NodeUID:    update.NodeUID,
	}: *update.Facts})

	for _, state := range []schedulingapi.DeviceAvailability{schedulingapi.DeviceHeld, schedulingapi.DeviceBinding, schedulingapi.DeviceAllocated} {
		if err := ledger.transition(deviceID, state); err != nil {
			t.Fatalf("transition(%q) error = %v", state, err)
		}
	}
	if err := ledger.transition(deviceID, schedulingapi.DeviceHeld); err == nil || !strings.Contains(err.Error(), "cannot transition") {
		t.Fatalf("transition(Allocated -> Held) error = %v, want lifecycle rejection", err)
	}
	unknownDeviceID := deviceID
	unknownDeviceID.Value = "unknown-device"
	if err := ledger.transition(unknownDeviceID, schedulingapi.DeviceHeld); err == nil || !strings.Contains(err.Error(), "no availability ledger state") {
		t.Fatalf("transition(unknown) error = %v, want unknown-device rejection", err)
	}
}
