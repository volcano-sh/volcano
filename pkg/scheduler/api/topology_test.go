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

package api

import (
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	topologyTestProviderID = "test-provider"
	topologyTestNamespace  = "test.example"
)

var (
	topologyTestResource   = corev1.ResourceName("nvidia.com/gpu")
	topologyTestObservedAt = time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)
)

func TestValidateNodeTopologyFacts(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*NodeTopologyFacts)
		wantError string
	}{
		{
			name: "valid facts",
		},
		{
			name: "missing provider ID",
			mutate: func(facts *NodeTopologyFacts) {
				facts.ProviderID = ""
			},
			wantError: "provider ID is required",
		},
		{
			name: "missing Node identity",
			mutate: func(facts *NodeTopologyFacts) {
				facts.NodeUID = ""
			},
			wantError: "Node name and UID are required",
		},
		{
			name: "missing source generation",
			mutate: func(facts *NodeTopologyFacts) {
				facts.SourceGeneration = 0
			},
			wantError: "source generation must be positive",
		},
		{
			name: "device belongs to another Node",
			mutate: func(facts *NodeTopologyFacts) {
				facts.Devices[0].NodeUID = types.UID("another-node")
			},
			wantError: "does not belong to Node",
		},
		{
			name: "local domain references an unknown device",
			mutate: func(facts *NodeTopologyFacts) {
				facts.LocalDomains[0].DeviceIDs = append(facts.LocalDomains[0].DeviceIDs, DeviceID{
					ProviderID: topologyTestProviderID,
					Namespace:  topologyTestNamespace,
					Value:      "missing-device",
				})
			},
			wantError: "references unknown device",
		},
		{
			name: "device is absent from its domain",
			mutate: func(facts *NodeTopologyFacts) {
				facts.LocalDomains[0].DeviceIDs = nil
			},
			wantError: "is missing from local domain",
		},
		{
			name: "identity namespaces differ",
			mutate: func(facts *NodeTopologyFacts) {
				facts.Devices[0].ID.Namespace = "other.example"
			},
			wantError: "facts use more than one identity namespace",
		},
		{
			name: "duplicate local domain",
			mutate: func(facts *NodeTopologyFacts) {
				facts.LocalDomains = append(facts.LocalDomains, facts.LocalDomains[0])
			},
			wantError: "duplicate local domain",
		},
		{
			name: "device belongs to two local domains",
			mutate: func(facts *NodeTopologyFacts) {
				secondDomain := facts.LocalDomains[0]
				secondDomain.ID.Value = "domain-b"
				facts.LocalDomains = append(facts.LocalDomains, secondDomain)
			},
			wantError: "belongs to both local domains",
		},
		{
			name: "unsupported device health",
			mutate: func(facts *NodeTopologyFacts) {
				facts.Devices[0].Health = DeviceHealth("Unknown")
			},
			wantError: "unsupported health",
		},
		{
			name: "device resource differs from local domain",
			mutate: func(facts *NodeTopologyFacts) {
				facts.Devices[0].ResourceName = "example.com/npu"
			},
			wantError: "resource does not match local domain",
		},
		{
			name: "duplicate device",
			mutate: func(facts *NodeTopologyFacts) {
				facts.Devices = append(facts.Devices, facts.Devices[0])
			},
			wantError: "duplicate device",
		},
		{
			name: "fabric omits its owner",
			mutate: func(facts *NodeTopologyFacts) {
				facts.Fabrics = []FabricDomain{{
					ID: FabricDomainID{
						ProviderID: topologyTestProviderID,
						Namespace:  topologyTestNamespace,
						Value:      "fabric-0",
					},
					OwnerProviderID:  topologyTestProviderID,
					OwnerNodeUID:     facts.NodeUID,
					SourceGeneration: facts.SourceGeneration,
					Members: []FabricMember{{
						NodeName:         "node-b",
						NodeUID:          types.UID("node-b-uid"),
						SourceGeneration: 1,
						LocalDomainIDs: []DeviceDomainID{{
							ProviderID: topologyTestProviderID,
							Namespace:  topologyTestNamespace,
							NodeUID:    types.UID("node-b-uid"),
							Value:      "domain-b",
						}},
					}},
				}}
			},
			wantError: "does not include its owner",
		},
		{
			name: "fabric has a mismatched owner generation",
			mutate: func(facts *NodeTopologyFacts) {
				fabric := testFabricForFacts(*facts, "fabric-a")
				fabric.SourceGeneration++
				facts.Fabrics = []FabricDomain{fabric}
			},
			wantError: "does not have this fact set as its owner",
		},
		{
			name: "duplicate fabric",
			mutate: func(facts *NodeTopologyFacts) {
				fabric := testFabricForFacts(*facts, "fabric-a")
				facts.Fabrics = []FabricDomain{fabric, fabric}
			},
			wantError: "duplicate fabric",
		},
		{
			name: "fabric has duplicate members",
			mutate: func(facts *NodeTopologyFacts) {
				fabric := testFabricForFacts(*facts, "fabric-a")
				fabric.Members = append(fabric.Members, fabric.Members[0])
				facts.Fabrics = []FabricDomain{fabric}
			},
			wantError: "duplicate member Node UID",
		},
		{
			name: "fabric member uses another Nodes domain",
			mutate: func(facts *NodeTopologyFacts) {
				fabric := testFabricForFacts(*facts, "fabric-a")
				fabric.Members[0].LocalDomainIDs[0].NodeUID = types.UID("another-node")
				facts.Fabrics = []FabricDomain{fabric}
			},
			wantError: "invalid local domain member",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := testNodeTopologyFacts("node-a", "node-a-uid", 1, "domain-a", "device-a")
			if test.mutate != nil {
				test.mutate(&facts)
			}

			err := ValidateNodeTopologyFacts(facts)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("ValidateNodeTopologyFacts() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("ValidateNodeTopologyFacts() error = %v, want containing %q", err, test.wantError)
			}
		})
	}
}

func TestSameTopologyContentIgnoresDeliveryMetadata(t *testing.T) {
	facts := testNodeTopologyFacts("node-a", "node-a-uid", 1, "domain-a", "device-a")
	metadataOnly := facts.Clone()
	metadataOnly.ProviderID = "other-provider"
	metadataOnly.NodeName = "replacement-node"
	metadataOnly.NodeUID = types.UID("replacement-node-uid")
	metadataOnly.NodeResourceVersion = "2"
	metadataOnly.SourceGeneration = 2
	metadataOnly.ObservedAt = facts.ObservedAt.Add(time.Minute)
	metadataOnly.FreshUntil = facts.FreshUntil.Add(time.Minute)

	if !SameTopologyContent(facts, metadataOnly) {
		t.Fatal("SameTopologyContent() = false for metadata-only changes, want true")
	}

	changedTopology := facts.Clone()
	changedTopology.Devices[0].Health = DeviceUnhealthy
	if SameTopologyContent(facts, changedTopology) {
		t.Fatal("SameTopologyContent() = true after a device health change, want false")
	}
}

func TestTopologySnapshotIndexesAndReturnsCopies(t *testing.T) {
	nodeUID := types.UID("node-a-uid")
	facts := testNodeTopologyFacts("node-a", nodeUID, 7, "domain-b", "device-b")
	facts.Devices[0].FabricIDs = []FabricDomainID{{
		ProviderID: topologyTestProviderID,
		Namespace:  topologyTestNamespace,
		Value:      "fabric-0",
	}}

	domainAFacts := testNodeTopologyFacts("node-a", nodeUID, 7, "domain-a", "device-a")
	facts.Devices = append(facts.Devices, domainAFacts.Devices[0])
	facts.LocalDomains = append(facts.LocalDomains, domainAFacts.LocalDomains[0])

	deviceZ := DeviceID{ProviderID: topologyTestProviderID, Namespace: topologyTestNamespace, Value: "device-z"}
	facts.Devices = append(facts.Devices, TopologyDevice{
		ID:            deviceZ,
		ResourceName:  topologyTestResource,
		NodeName:      facts.NodeName,
		NodeUID:       facts.NodeUID,
		LocalDomainID: domainAFacts.LocalDomains[0].ID,
		Health:        DeviceHealthy,
	})
	facts.LocalDomains[1].DeviceIDs = append(facts.LocalDomains[1].DeviceIDs, deviceZ)

	key := ProviderNodeKey{ProviderID: topologyTestProviderID, NodeUID: nodeUID}
	snapshot := NewTopologySnapshot(42,
		map[ProviderNodeKey]NodeTopologyFacts{key: facts},
		map[ProviderNodeKey]ProviderNodeSyncState{key: ProviderNodeSynced},
	)

	if got := snapshot.Revision(); got != 42 {
		t.Fatalf("Revision() = %d, want 42", got)
	}
	if got := snapshot.ProviderNodeReadiness(topologyTestProviderID, nodeUID); got != ProviderNodeSynced {
		t.Fatalf("ProviderNodeReadiness() = %q, want %q", got, ProviderNodeSynced)
	}
	if got := snapshot.ProviderNodeReadiness(topologyTestProviderID, "unknown-node"); got != ProviderNodePending {
		t.Fatalf("ProviderNodeReadiness() for unknown Node = %q, want %q", got, ProviderNodePending)
	}
	if !snapshot.IsProviderNodeFreshAt(topologyTestProviderID, nodeUID, facts.FreshUntil) {
		t.Fatal("IsProviderNodeFreshAt() = false at FreshUntil, want true")
	}
	if snapshot.IsProviderNodeFreshAt(topologyTestProviderID, nodeUID, facts.FreshUntil.Add(time.Nanosecond)) {
		t.Fatal("IsProviderNodeFreshAt() = true after FreshUntil, want false")
	}

	domains := snapshot.LocalDomains(nodeUID, topologyTestResource)
	if len(domains) != 2 || domains[0].ID.Value != "domain-a" || domains[1].ID.Value != "domain-b" {
		t.Fatalf("LocalDomains() = %#v, want domain-a then domain-b", domains)
	}
	healthyDeviceIDs := snapshot.HealthyDeviceIDs(domainAFacts.LocalDomains[0].ID)
	if len(healthyDeviceIDs) != 2 || healthyDeviceIDs[0].Value != "device-a" || healthyDeviceIDs[1].Value != "device-z" {
		t.Fatalf("HealthyDeviceIDs() = %#v, want device-a then device-z", healthyDeviceIDs)
	}

	facts.Devices[0].ID.Value = "mutated-input"
	facts.LocalDomains[0].DeviceIDs[0].Value = "mutated-input"
	domains[0].DeviceIDs[0].Value = "mutated-return"
	healthyDeviceIDs[0].Value = "mutated-return"

	returnedFacts, found := snapshot.Facts(topologyTestProviderID, nodeUID)
	if !found || returnedFacts.Devices[0].ID.Value != "device-b" {
		t.Fatalf("Facts() = %#v, found = %v, want an unmodified device-b fact set", returnedFacts, found)
	}
	returnedFacts.Devices[0].FabricIDs[0].Value = "mutated-return"
	device, found := snapshot.Device(DeviceID{ProviderID: topologyTestProviderID, Namespace: topologyTestNamespace, Value: "device-b"})
	if !found || len(device.FabricIDs) != 1 || device.FabricIDs[0].Value != "fabric-0" {
		t.Fatalf("Device() = %#v, found = %v, want an unmodified fabric reference", device, found)
	}

	domains = snapshot.LocalDomains(nodeUID, topologyTestResource)
	if domains[0].DeviceIDs[0].Value != "device-a" {
		t.Fatalf("LocalDomains() returned a mutable copy: %#v", domains[0].DeviceIDs)
	}
	healthyDeviceIDs = snapshot.HealthyDeviceIDs(domainAFacts.LocalDomains[0].ID)
	if healthyDeviceIDs[0].Value != "device-a" {
		t.Fatalf("HealthyDeviceIDs() returned a mutable copy: %#v", healthyDeviceIDs)
	}
}

func TestTopologySnapshotIndexesLedgerAvailableDevices(t *testing.T) {
	facts := testNodeTopologyFacts("node-a", "node-a-uid", 1, "domain-a", "device-a")
	secondDeviceID := DeviceID{ProviderID: topologyTestProviderID, Namespace: topologyTestNamespace, Value: "device-b"}
	facts.Devices = append(facts.Devices, TopologyDevice{
		ID:            secondDeviceID,
		ResourceName:  topologyTestResource,
		NodeName:      facts.NodeName,
		NodeUID:       facts.NodeUID,
		LocalDomainID: facts.LocalDomains[0].ID,
		Health:        DeviceHealthy,
	})
	facts.LocalDomains[0].DeviceIDs = append(facts.LocalDomains[0].DeviceIDs, secondDeviceID)
	key := ProviderNodeKey{ProviderID: topologyTestProviderID, NodeUID: facts.NodeUID}

	defaultSnapshot := NewTopologySnapshot(1, map[ProviderNodeKey]NodeTopologyFacts{key: facts}, nil)
	if state, found := defaultSnapshot.DeviceAvailability(facts.Devices[0].ID); !found || state != DeviceAvailable {
		t.Fatalf("default DeviceAvailability() = (%q, %v), want (%q, true)", state, found, DeviceAvailable)
	}

	tombstoneID := DeviceID{ProviderID: topologyTestProviderID, Namespace: topologyTestNamespace, Value: "removed-device"}
	snapshot := NewTopologySnapshotWithAvailability(2,
		map[ProviderNodeKey]NodeTopologyFacts{key: facts},
		nil,
		map[DeviceID]DeviceAvailability{
			facts.Devices[0].ID: DeviceHeld,
			secondDeviceID:      DeviceAvailable,
			tombstoneID:         DeviceUnreconciled,
		},
	)

	healthyDeviceIDs := snapshot.HealthyDeviceIDs(facts.LocalDomains[0].ID)
	if len(healthyDeviceIDs) != 2 {
		t.Fatalf("HealthyDeviceIDs() = %#v, want both healthy devices", healthyDeviceIDs)
	}
	availableDeviceIDs := snapshot.AvailableDeviceIDs(facts.LocalDomains[0].ID)
	if len(availableDeviceIDs) != 1 || availableDeviceIDs[0] != secondDeviceID {
		t.Fatalf("AvailableDeviceIDs() = %#v, want only device-b", availableDeviceIDs)
	}
	if state, found := snapshot.DeviceAvailability(facts.Devices[0].ID); !found || state != DeviceHeld {
		t.Fatalf("DeviceAvailability(device-a) = (%q, %v), want (%q, true)", state, found, DeviceHeld)
	}
	if state, found := snapshot.DeviceAvailability(tombstoneID); !found || state != DeviceUnreconciled {
		t.Fatalf("DeviceAvailability(tombstone) = (%q, %v), want (%q, true)", state, found, DeviceUnreconciled)
	}

	availableDeviceIDs[0].Value = "mutated-device"
	if got := snapshot.AvailableDeviceIDs(facts.LocalDomains[0].ID); len(got) != 1 || got[0] != secondDeviceID {
		t.Fatalf("AvailableDeviceIDs() returned a mutable copy: %#v", got)
	}
}

func TestTopologySnapshotOmitsFabricWithStaleMember(t *testing.T) {
	ownerFacts := testNodeTopologyFacts("node-a", "node-a-uid", 3, "domain-a", "device-a")
	memberFacts := testNodeTopologyFacts("node-b", "node-b-uid", 4, "domain-b", "device-b")
	fabricID := FabricDomainID{ProviderID: topologyTestProviderID, Namespace: topologyTestNamespace, Value: "fabric-0"}
	ownerFacts.Fabrics = []FabricDomain{{
		ID:                 fabricID,
		OwnerProviderID:    topologyTestProviderID,
		OwnerNodeUID:       ownerFacts.NodeUID,
		SourceGeneration:   ownerFacts.SourceGeneration,
		HyperNodeReference: "hypernode-a",
		Members: []FabricMember{
			{
				NodeName:         ownerFacts.NodeName,
				NodeUID:          ownerFacts.NodeUID,
				SourceGeneration: ownerFacts.SourceGeneration,
				LocalDomainIDs:   []DeviceDomainID{ownerFacts.LocalDomains[0].ID},
			},
			{
				NodeName:         memberFacts.NodeName,
				NodeUID:          memberFacts.NodeUID,
				SourceGeneration: memberFacts.SourceGeneration,
				LocalDomainIDs:   []DeviceDomainID{memberFacts.LocalDomains[0].ID},
			},
		},
	}}

	facts := map[ProviderNodeKey]NodeTopologyFacts{
		{ProviderID: topologyTestProviderID, NodeUID: ownerFacts.NodeUID}:  ownerFacts,
		{ProviderID: topologyTestProviderID, NodeUID: memberFacts.NodeUID}: memberFacts,
	}
	snapshot := NewTopologySnapshot(1, facts, nil)
	fabric, found := snapshot.Fabric(fabricID)
	if !found || fabric.HyperNodeReference != "hypernode-a" {
		t.Fatalf("Fabric() = %#v, found = %v, want the current fabric", fabric, found)
	}

	memberFacts.SourceGeneration++
	facts[ProviderNodeKey{ProviderID: topologyTestProviderID, NodeUID: memberFacts.NodeUID}] = memberFacts
	staleSnapshot := NewTopologySnapshot(2, facts, nil)
	if _, found := staleSnapshot.Fabric(fabricID); found {
		t.Fatal("Fabric() found a fabric with a stale member generation, want no fabric")
	}
}

func TestNodeTopologyFactsCloneDeepCopiesNestedSlices(t *testing.T) {
	facts := testNodeTopologyFacts("node-a", "node-a-uid", 1, "domain-a", "device-a")
	facts.Devices[0].FabricIDs = []FabricDomainID{{
		ProviderID: topologyTestProviderID,
		Namespace:  topologyTestNamespace,
		Value:      "fabric-a",
	}}
	facts.Fabrics = []FabricDomain{testFabricForFacts(facts, "fabric-a")}

	clone := facts.Clone()
	clone.Devices[0].FabricIDs[0].Value = "changed-fabric"
	clone.LocalDomains[0].DeviceIDs[0].Value = "changed-device"
	clone.Fabrics[0].Members[0].LocalDomainIDs[0].Value = "changed-domain"

	if got := facts.Devices[0].FabricIDs[0].Value; got != "fabric-a" {
		t.Fatalf("Clone() mutated original device fabric IDs: got %q", got)
	}
	if got := facts.LocalDomains[0].DeviceIDs[0].Value; got != "device-a" {
		t.Fatalf("Clone() mutated original local domain device IDs: got %q", got)
	}
	if got := facts.Fabrics[0].Members[0].LocalDomainIDs[0].Value; got != "domain-a" {
		t.Fatalf("Clone() mutated original fabric member local domains: got %q", got)
	}
}

func TestTopologySnapshotNilAndMissingAccessors(t *testing.T) {
	var snapshot *TopologySnapshot
	deviceID := DeviceID{ProviderID: topologyTestProviderID, Namespace: topologyTestNamespace, Value: "unknown-device"}
	domainID := DeviceDomainID{ProviderID: topologyTestProviderID, Namespace: topologyTestNamespace, NodeUID: "unknown-node", Value: "unknown-domain"}
	fabricID := FabricDomainID{ProviderID: topologyTestProviderID, Namespace: topologyTestNamespace, Value: "unknown-fabric"}

	if got := snapshot.Revision(); got != 0 {
		t.Fatalf("nil Revision() = %d, want 0", got)
	}
	if got := snapshot.ProviderNodeReadiness(topologyTestProviderID, "unknown-node"); got != ProviderNodePending {
		t.Fatalf("nil ProviderNodeReadiness() = %q, want %q", got, ProviderNodePending)
	}
	if _, found := snapshot.Facts(topologyTestProviderID, "unknown-node"); found {
		t.Fatal("nil Facts() found facts, want false")
	}
	if snapshot.IsProviderNodeFreshAt(topologyTestProviderID, "unknown-node", topologyTestObservedAt) {
		t.Fatal("nil IsProviderNodeFreshAt() = true, want false")
	}
	if got := snapshot.LocalDomains("unknown-node", topologyTestResource); got != nil {
		t.Fatalf("nil LocalDomains() = %#v, want nil", got)
	}
	if got := snapshot.HealthyDeviceIDs(domainID); got != nil {
		t.Fatalf("nil HealthyDeviceIDs() = %#v, want nil", got)
	}
	if got := snapshot.AvailableDeviceIDs(domainID); got != nil {
		t.Fatalf("nil AvailableDeviceIDs() = %#v, want nil", got)
	}
	if _, found := snapshot.DeviceAvailability(deviceID); found {
		t.Fatal("nil DeviceAvailability() found a device, want false")
	}
	if _, found := snapshot.Device(deviceID); found {
		t.Fatal("nil Device() found a device, want false")
	}
	if _, found := snapshot.Fabric(fabricID); found {
		t.Fatal("nil Fabric() found a fabric, want false")
	}
	if got := snapshot.Fabrics(); got != nil {
		t.Fatalf("nil Fabrics() = %#v, want nil", got)
	}

	empty := NewTopologySnapshot(1, nil, nil)
	if _, found := empty.Facts(topologyTestProviderID, "unknown-node"); found {
		t.Fatal("empty Facts() found facts, want false")
	}
	if empty.IsProviderNodeFreshAt(topologyTestProviderID, "unknown-node", topologyTestObservedAt) {
		t.Fatal("empty IsProviderNodeFreshAt() = true, want false")
	}
	if got := empty.LocalDomains("unknown-node", topologyTestResource); len(got) != 0 {
		t.Fatalf("empty LocalDomains() = %#v, want no domains", got)
	}
	if _, found := empty.Device(deviceID); found {
		t.Fatal("empty Device() found a device, want false")
	}
	if _, found := empty.Fabric(fabricID); found {
		t.Fatal("empty Fabric() found a fabric, want false")
	}
	if got := empty.Fabrics(); len(got) != 0 {
		t.Fatalf("empty Fabrics() = %#v, want no fabrics", got)
	}
}

func TestTopologySnapshotFreshnessRequiresSyncedFactsAndDeadline(t *testing.T) {
	key := ProviderNodeKey{ProviderID: topologyTestProviderID, NodeUID: "node-a-uid"}
	tests := []struct {
		name          string
		readiness     map[ProviderNodeKey]ProviderNodeSyncState
		freshUntil    time.Time
		wantFreshness bool
	}{
		{
			name: "pending provider",
			readiness: map[ProviderNodeKey]ProviderNodeSyncState{
				key: ProviderNodePending,
			},
			freshUntil: topologyTestObservedAt.Add(time.Minute),
		},
		{
			name:       "missing provider readiness",
			freshUntil: topologyTestObservedAt.Add(time.Minute),
		},
		{
			name: "missing freshness deadline",
			readiness: map[ProviderNodeKey]ProviderNodeSyncState{
				key: ProviderNodeSynced,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := testNodeTopologyFacts("node-a", key.NodeUID, 1, "domain-a", "device-a")
			facts.FreshUntil = test.freshUntil
			snapshot := NewTopologySnapshot(1, map[ProviderNodeKey]NodeTopologyFacts{key: facts}, test.readiness)

			if got := snapshot.IsProviderNodeFreshAt(topologyTestProviderID, key.NodeUID, topologyTestObservedAt); got != test.wantFreshness {
				t.Fatalf("IsProviderNodeFreshAt() = %v, want %v", got, test.wantFreshness)
			}
		})
	}
}

func TestTopologySnapshotExcludesUnhealthyDevicesFromHealthyIndex(t *testing.T) {
	facts := testNodeTopologyFacts("node-a", "node-a-uid", 1, "domain-a", "device-a")
	facts.Devices[0].Health = DeviceUnhealthy
	key := ProviderNodeKey{ProviderID: topologyTestProviderID, NodeUID: facts.NodeUID}
	snapshot := NewTopologySnapshot(1, map[ProviderNodeKey]NodeTopologyFacts{key: facts}, nil)

	if got := snapshot.HealthyDeviceIDs(facts.LocalDomains[0].ID); len(got) != 0 {
		t.Fatalf("HealthyDeviceIDs() = %#v, want no unhealthy devices", got)
	}
	device, found := snapshot.Device(facts.Devices[0].ID)
	if !found || device.Health != DeviceUnhealthy {
		t.Fatalf("Device() = %#v, found = %v, want the unhealthy device record", device, found)
	}
}

func TestTopologySnapshotFabricsReturnsSortedCopies(t *testing.T) {
	facts := testNodeTopologyFacts("node-a", "node-a-uid", 1, "domain-a", "device-a")
	fabricB := testFabricForFacts(facts, "fabric-b")
	fabricA := testFabricForFacts(facts, "fabric-a")
	facts.Fabrics = []FabricDomain{fabricB, fabricA}
	key := ProviderNodeKey{ProviderID: topologyTestProviderID, NodeUID: facts.NodeUID}
	snapshot := NewTopologySnapshot(1, map[ProviderNodeKey]NodeTopologyFacts{key: facts}, nil)

	fabrics := snapshot.Fabrics()
	if len(fabrics) != 2 || fabrics[0].ID.Value != "fabric-a" || fabrics[1].ID.Value != "fabric-b" {
		t.Fatalf("Fabrics() = %#v, want fabric-a then fabric-b", fabrics)
	}
	fabrics[0].Members[0].LocalDomainIDs[0].Value = "mutated-domain"

	fabric, found := snapshot.Fabric(fabricA.ID)
	if !found || fabric.Members[0].LocalDomainIDs[0].Value != "domain-a" {
		t.Fatalf("Fabrics() returned a mutable copy: %#v, found = %v", fabric, found)
	}
	fabric.Members[0].LocalDomainIDs[0].Value = "mutated-domain"
	fabric, found = snapshot.Fabric(fabricA.ID)
	if !found || fabric.Members[0].LocalDomainIDs[0].Value != "domain-a" {
		t.Fatalf("Fabric() returned a mutable copy: %#v, found = %v", fabric, found)
	}
}

func TestTopologySnapshotOmitsFabricWithInvalidCurrentFacts(t *testing.T) {
	newFacts := func() (map[ProviderNodeKey]NodeTopologyFacts, FabricDomainID) {
		ownerFacts := testNodeTopologyFacts("node-a", "node-a-uid", 3, "domain-a", "device-a")
		memberFacts := testNodeTopologyFacts("node-b", "node-b-uid", 4, "domain-b", "device-b")
		fabric := testFabricForFacts(ownerFacts, "fabric-0")
		fabric.Members = append(fabric.Members, FabricMember{
			NodeName:         memberFacts.NodeName,
			NodeUID:          memberFacts.NodeUID,
			SourceGeneration: memberFacts.SourceGeneration,
			LocalDomainIDs:   []DeviceDomainID{memberFacts.LocalDomains[0].ID},
		})
		ownerFacts.Fabrics = []FabricDomain{fabric}
		return map[ProviderNodeKey]NodeTopologyFacts{
			{ProviderID: topologyTestProviderID, NodeUID: ownerFacts.NodeUID}:  ownerFacts,
			{ProviderID: topologyTestProviderID, NodeUID: memberFacts.NodeUID}: memberFacts,
		}, fabric.ID
	}

	tests := []struct {
		name   string
		mutate func(map[ProviderNodeKey]NodeTopologyFacts)
	}{
		{
			name: "owner generation changes",
			mutate: func(facts map[ProviderNodeKey]NodeTopologyFacts) {
				key := ProviderNodeKey{ProviderID: topologyTestProviderID, NodeUID: "node-a-uid"}
				ownerFacts := facts[key]
				ownerFacts.SourceGeneration++
				facts[key] = ownerFacts
			},
		},
		{
			name: "member facts disappear",
			mutate: func(facts map[ProviderNodeKey]NodeTopologyFacts) {
				delete(facts, ProviderNodeKey{ProviderID: topologyTestProviderID, NodeUID: "node-b-uid"})
			},
		},
		{
			name: "member Node name changes",
			mutate: func(facts map[ProviderNodeKey]NodeTopologyFacts) {
				key := ProviderNodeKey{ProviderID: topologyTestProviderID, NodeUID: "node-b-uid"}
				memberFacts := facts[key]
				memberFacts.NodeName = "renamed-node-b"
				facts[key] = memberFacts
			},
		},
		{
			name: "member local domain disappears",
			mutate: func(facts map[ProviderNodeKey]NodeTopologyFacts) {
				key := ProviderNodeKey{ProviderID: topologyTestProviderID, NodeUID: "node-b-uid"}
				memberFacts := facts[key]
				memberFacts.LocalDomains = nil
				facts[key] = memberFacts
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts, fabricID := newFacts()
			test.mutate(facts)
			snapshot := NewTopologySnapshot(1, facts, nil)
			if _, found := snapshot.Fabric(fabricID); found {
				t.Fatal("Fabric() found a fabric with invalid current facts, want no fabric")
			}
		})
	}
}

func testNodeTopologyFacts(nodeName string, nodeUID types.UID, sourceGeneration uint64, domainValue, deviceValue string) NodeTopologyFacts {
	domainID := DeviceDomainID{
		ProviderID: topologyTestProviderID,
		Namespace:  topologyTestNamespace,
		NodeUID:    nodeUID,
		Value:      domainValue,
	}
	deviceID := DeviceID{
		ProviderID: topologyTestProviderID,
		Namespace:  topologyTestNamespace,
		Value:      deviceValue,
	}
	return NodeTopologyFacts{
		ProviderID:          topologyTestProviderID,
		NodeName:            nodeName,
		NodeUID:             nodeUID,
		NodeResourceVersion: "1",
		SourceGeneration:    sourceGeneration,
		ObservedAt:          topologyTestObservedAt,
		FreshUntil:          topologyTestObservedAt.Add(2 * time.Minute),
		Devices: []TopologyDevice{{
			ID:            deviceID,
			ResourceName:  topologyTestResource,
			NodeName:      nodeName,
			NodeUID:       nodeUID,
			LocalDomainID: domainID,
			Health:        DeviceHealthy,
		}},
		LocalDomains: []DeviceDomain{{
			ID:           domainID,
			NodeName:     nodeName,
			NodeUID:      nodeUID,
			ResourceName: topologyTestResource,
			DeviceIDs:    []DeviceID{deviceID},
		}},
	}
}

func testFabricForFacts(facts NodeTopologyFacts, value string) FabricDomain {
	return FabricDomain{
		ID: FabricDomainID{
			ProviderID: facts.ProviderID,
			Namespace:  facts.LocalDomains[0].ID.Namespace,
			Value:      value,
		},
		OwnerProviderID:  facts.ProviderID,
		OwnerNodeUID:     facts.NodeUID,
		SourceGeneration: facts.SourceGeneration,
		Members: []FabricMember{{
			NodeName:         facts.NodeName,
			NodeUID:          facts.NodeUID,
			SourceGeneration: facts.SourceGeneration,
			LocalDomainIDs:   []DeviceDomainID{facts.LocalDomains[0].ID},
		}},
	}
}
