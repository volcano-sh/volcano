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
	"fmt"
	"reflect"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// DeviceID is the stable identity of one physical accelerator device. Provider
// and Namespace are scheduler configuration, not workload-controlled values.
type DeviceID struct {
	ProviderID string
	Namespace  string
	Value      string
}

// String returns a stable, human-readable representation of the device ID.
func (id DeviceID) String() string {
	return fmt.Sprintf("%s/%s/%s", id.ProviderID, id.Namespace, id.Value)
}

// DeviceDomainID identifies a Node-local accelerator interconnect domain.
// NodeUID prevents a replacement Node with the same name from colliding with
// the previous Node's topology.
type DeviceDomainID struct {
	ProviderID string
	Namespace  string
	NodeUID    types.UID
	Value      string
}

// FabricDomainID identifies one provider-owned cross-Node accelerator fabric.
type FabricDomainID struct {
	ProviderID string
	Namespace  string
	Value      string
}

// DeviceHealth is the provider-reported health state of a device.
type DeviceHealth string

const (
	// DeviceHealthy means the provider reports the device as usable.
	DeviceHealthy DeviceHealth = "Healthy"
	// DeviceUnhealthy means the provider reports the device as unavailable.
	DeviceUnhealthy DeviceHealth = "Unhealthy"
)

// DeviceAvailability is the scheduler-owned lifecycle state for one device.
// It is separate from provider-reported DeviceHealth because a healthy device
// can still be reserved, allocated, releasing, or awaiting reconciliation.
type DeviceAvailability string

const (
	// DeviceAvailable means a healthy device is not held by the scheduler or an
	// allocation backend.
	DeviceAvailable DeviceAvailability = "Available"
	// DeviceHeld means the scheduler has tentatively reserved the device before
	// bind handoff.
	DeviceHeld DeviceAvailability = "Held"
	// DeviceBinding means a reservation has been handed to the bind path and is
	// awaiting authoritative allocation confirmation.
	DeviceBinding DeviceAvailability = "Binding"
	// DeviceAllocated means the allocation backend confirmed the device is in use.
	DeviceAllocated DeviceAvailability = "Allocated"
	// DeviceReleasing means an allocation is being released but the backend has
	// not yet confirmed that the device is free.
	DeviceReleasing DeviceAvailability = "Releasing"
	// DeviceUnavailable means provider health or scheduler safety checks make
	// the device ineligible for a new placement.
	DeviceUnavailable DeviceAvailability = "Unavailable"
	// DeviceUnreconciled means the scheduler cannot safely determine whether the
	// backend still holds the device.
	DeviceUnreconciled DeviceAvailability = "Unreconciled"
)

// IsValid reports whether availability is a supported ledger lifecycle state.
func (availability DeviceAvailability) IsValid() bool {
	switch availability {
	case DeviceAvailable, DeviceHeld, DeviceBinding, DeviceAllocated, DeviceReleasing, DeviceUnavailable, DeviceUnreconciled:
		return true
	default:
		return false
	}
}

// IsLive reports whether the availability state must survive a provider fact
// removal until allocation reconciliation confirms it is safe to discard.
func (availability DeviceAvailability) IsLive() bool {
	switch availability {
	case DeviceHeld, DeviceBinding, DeviceAllocated, DeviceReleasing, DeviceUnreconciled:
		return true
	default:
		return false
	}
}

// TopologyRevision identifies one immutable topology snapshot.
type TopologyRevision uint64

// ProviderNodeSyncState describes whether a provider has completed an initial
// valid sync for a specific Node UID.
type ProviderNodeSyncState string

const (
	// ProviderNodePending is the state before a provider returns a valid update.
	ProviderNodePending ProviderNodeSyncState = "Pending"
	// ProviderNodeSynced is the state after a provider returns a valid update.
	ProviderNodeSynced ProviderNodeSyncState = "Synced"
)

// ProviderNodeKey identifies facts and readiness for one provider and Node.
type ProviderNodeKey struct {
	ProviderID string
	NodeUID    types.UID
}

// NodeResourceKey identifies Node-local topology domains for one resource.
type NodeResourceKey struct {
	NodeUID      types.UID
	ResourceName corev1.ResourceName
}

// TopologyDevice is a source-normalized device record.
type TopologyDevice struct {
	ID            DeviceID
	ResourceName  corev1.ResourceName
	NodeName      string
	NodeUID       types.UID
	LocalDomainID DeviceDomainID
	FabricIDs     []FabricDomainID
	Health        DeviceHealth
}

// DeviceDomain is a Node-local group of devices with a physical interconnect.
type DeviceDomain struct {
	ID           DeviceDomainID
	NodeName     string
	NodeUID      types.UID
	ResourceName corev1.ResourceName
	DeviceIDs    []DeviceID
}

// FabricMember is one Node's contribution to a cross-Node fabric.
type FabricMember struct {
	NodeName         string
	NodeUID          types.UID
	SourceGeneration uint64
	LocalDomainIDs   []DeviceDomainID
}

// FabricDomain is a provider-owned cross-Node accelerator fabric.
type FabricDomain struct {
	ID                 FabricDomainID
	OwnerProviderID    string
	OwnerNodeUID       types.UID
	SourceGeneration   uint64
	Members            []FabricMember
	HyperNodeReference string
	Interconnect       string
}

// NodeTopologyFacts is the complete source-owned topology fact set for one
// provider and Node. Scheduler-owned availability and reservation state is
// deliberately not stored here.
type NodeTopologyFacts struct {
	ProviderID          string
	NodeName            string
	NodeUID             types.UID
	NodeResourceVersion string
	SourceGeneration    uint64
	ObservedAt          time.Time
	FreshUntil          time.Time
	Devices             []TopologyDevice
	LocalDomains        []DeviceDomain
	Fabrics             []FabricDomain
}

// Clone returns an independent copy of topology facts.
func (facts NodeTopologyFacts) Clone() NodeTopologyFacts {
	clone := facts
	clone.Devices = make([]TopologyDevice, len(facts.Devices))
	for index, device := range facts.Devices {
		clone.Devices[index] = device
		clone.Devices[index].FabricIDs = append([]FabricDomainID(nil), device.FabricIDs...)
	}
	clone.LocalDomains = make([]DeviceDomain, len(facts.LocalDomains))
	for index, domain := range facts.LocalDomains {
		clone.LocalDomains[index] = domain
		clone.LocalDomains[index].DeviceIDs = append([]DeviceID(nil), domain.DeviceIDs...)
	}
	clone.Fabrics = make([]FabricDomain, len(facts.Fabrics))
	for index, fabric := range facts.Fabrics {
		clone.Fabrics[index] = fabric
		clone.Fabrics[index].Members = make([]FabricMember, len(fabric.Members))
		for memberIndex, member := range fabric.Members {
			clone.Fabrics[index].Members[memberIndex] = member
			clone.Fabrics[index].Members[memberIndex].LocalDomainIDs = append([]DeviceDomainID(nil), member.LocalDomainIDs...)
		}
	}
	return clone
}

// SameTopologyContent reports whether two fact sets describe the same hardware
// topology. Delivery and liveness metadata intentionally do not participate.
func SameTopologyContent(left, right NodeTopologyFacts) bool {
	left = left.Clone()
	right = right.Clone()
	left.ProviderID = ""
	left.NodeName = ""
	left.NodeUID = ""
	left.NodeResourceVersion = ""
	left.SourceGeneration = 0
	left.ObservedAt = time.Time{}
	left.FreshUntil = time.Time{}
	right.ProviderID = ""
	right.NodeName = ""
	right.NodeUID = ""
	right.NodeResourceVersion = ""
	right.SourceGeneration = 0
	right.ObservedAt = time.Time{}
	right.FreshUntil = time.Time{}
	return reflect.DeepEqual(left, right)
}

// ValidateNodeTopologyFacts verifies internal device and domain consistency.
// It does not validate freshness or source-generation ordering; those are
// provider and cache lifecycle concerns.
func ValidateNodeTopologyFacts(facts NodeTopologyFacts) error {
	if facts.ProviderID == "" {
		return fmt.Errorf("provider ID is required")
	}
	if facts.NodeName == "" || facts.NodeUID == "" {
		return fmt.Errorf("Node name and UID are required")
	}
	if facts.SourceGeneration == 0 {
		return fmt.Errorf("source generation must be positive")
	}

	identityNamespace := ""
	observeNamespace := func(namespace string) error {
		if namespace == "" {
			return fmt.Errorf("identity namespace is required")
		}
		if identityNamespace == "" {
			identityNamespace = namespace
			return nil
		}
		if namespace != identityNamespace {
			return fmt.Errorf("facts use more than one identity namespace")
		}
		return nil
	}

	domains := make(map[DeviceDomainID]DeviceDomain, len(facts.LocalDomains))
	devicesInDomain := make(map[DeviceID]DeviceDomainID, len(facts.Devices))
	for _, domain := range facts.LocalDomains {
		if domain.ID.ProviderID == "" || domain.ID.Namespace == "" || domain.ID.NodeUID == "" || domain.ID.Value == "" {
			return fmt.Errorf("local domain has an incomplete identity")
		}
		if domain.ID.ProviderID != facts.ProviderID {
			return fmt.Errorf("local domain %q has a different provider ID", domain.ID.Value)
		}
		if err := observeNamespace(domain.ID.Namespace); err != nil {
			return fmt.Errorf("local domain %q: %w", domain.ID.Value, err)
		}
		if domain.ID.NodeUID != facts.NodeUID || domain.NodeUID != facts.NodeUID || domain.NodeName != facts.NodeName {
			return fmt.Errorf("local domain %q does not belong to Node %q", domain.ID.Value, facts.NodeName)
		}
		if domain.ResourceName == "" {
			return fmt.Errorf("local domain %q has no resource name", domain.ID.Value)
		}
		if _, exists := domains[domain.ID]; exists {
			return fmt.Errorf("duplicate local domain %q", domain.ID.Value)
		}
		domains[domain.ID] = domain
		for _, deviceID := range domain.DeviceIDs {
			if previous, exists := devicesInDomain[deviceID]; exists {
				return fmt.Errorf("device %q belongs to both local domains %q and %q", deviceID.Value, previous.Value, domain.ID.Value)
			}
			devicesInDomain[deviceID] = domain.ID
		}
	}

	devices := make(map[DeviceID]TopologyDevice, len(facts.Devices))
	for _, device := range facts.Devices {
		if device.ID.ProviderID == "" || device.ID.Namespace == "" || device.ID.Value == "" {
			return fmt.Errorf("device has an incomplete identity")
		}
		if device.ID.ProviderID != facts.ProviderID {
			return fmt.Errorf("device %q has a different provider ID", device.ID.Value)
		}
		if err := observeNamespace(device.ID.Namespace); err != nil {
			return fmt.Errorf("device %q: %w", device.ID.Value, err)
		}
		if device.NodeName != facts.NodeName || device.NodeUID != facts.NodeUID {
			return fmt.Errorf("device %q does not belong to Node %q", device.ID.Value, facts.NodeName)
		}
		if device.ResourceName == "" {
			return fmt.Errorf("device %q has no resource name", device.ID.Value)
		}
		if device.Health != DeviceHealthy && device.Health != DeviceUnhealthy {
			return fmt.Errorf("device %q has unsupported health %q", device.ID.Value, device.Health)
		}
		if _, exists := devices[device.ID]; exists {
			return fmt.Errorf("duplicate device %q", device.ID.Value)
		}
		domain, exists := domains[device.LocalDomainID]
		if !exists {
			return fmt.Errorf("device %q references unknown local domain %q", device.ID.Value, device.LocalDomainID.Value)
		}
		if domain.ResourceName != device.ResourceName {
			return fmt.Errorf("device %q resource does not match local domain %q", device.ID.Value, domain.ID.Value)
		}
		if devicesInDomain[device.ID] != device.LocalDomainID {
			return fmt.Errorf("device %q is missing from local domain %q", device.ID.Value, device.LocalDomainID.Value)
		}
		devices[device.ID] = device
	}

	for deviceID, domainID := range devicesInDomain {
		if _, exists := devices[deviceID]; !exists {
			return fmt.Errorf("local domain %q references unknown device %q", domainID.Value, deviceID.Value)
		}
	}

	fabrics := make(map[FabricDomainID]struct{}, len(facts.Fabrics))
	for _, fabric := range facts.Fabrics {
		if fabric.ID.ProviderID == "" || fabric.ID.Namespace == "" || fabric.ID.Value == "" {
			return fmt.Errorf("fabric has an incomplete identity")
		}
		if fabric.ID.ProviderID != facts.ProviderID || fabric.OwnerProviderID != facts.ProviderID {
			return fmt.Errorf("fabric %q has a different provider ID", fabric.ID.Value)
		}
		if err := observeNamespace(fabric.ID.Namespace); err != nil {
			return fmt.Errorf("fabric %q: %w", fabric.ID.Value, err)
		}
		if fabric.OwnerNodeUID != facts.NodeUID || fabric.SourceGeneration != facts.SourceGeneration {
			return fmt.Errorf("fabric %q does not have this fact set as its owner", fabric.ID.Value)
		}
		if _, exists := fabrics[fabric.ID]; exists {
			return fmt.Errorf("duplicate fabric %q", fabric.ID.Value)
		}
		fabrics[fabric.ID] = struct{}{}

		members := make(map[types.UID]struct{}, len(fabric.Members))
		ownerIsMember := false
		for _, member := range fabric.Members {
			if member.NodeName == "" || member.NodeUID == "" || member.SourceGeneration == 0 {
				return fmt.Errorf("fabric %q has an incomplete member", fabric.ID.Value)
			}
			if _, exists := members[member.NodeUID]; exists {
				return fmt.Errorf("fabric %q has duplicate member Node UID %q", fabric.ID.Value, member.NodeUID)
			}
			members[member.NodeUID] = struct{}{}
			if member.NodeUID == fabric.OwnerNodeUID {
				ownerIsMember = true
			}
			for _, domainID := range member.LocalDomainIDs {
				if domainID.ProviderID != facts.ProviderID || domainID.Namespace != fabric.ID.Namespace || domainID.NodeUID != member.NodeUID || domainID.Value == "" {
					return fmt.Errorf("fabric %q has an invalid local domain member", fabric.ID.Value)
				}
			}
		}
		if !ownerIsMember {
			return fmt.Errorf("fabric %q does not include its owner", fabric.ID.Value)
		}
	}
	return nil
}

// TopologySnapshot is an immutable indexed view of accepted provider facts.
// All accessors return copies so a scheduling session cannot mutate the view.
type TopologySnapshot struct {
	revision                   TopologyRevision
	factsByProviderNode        map[ProviderNodeKey]NodeTopologyFacts
	providerNodeReadiness      map[ProviderNodeKey]ProviderNodeSyncState
	devices                    map[DeviceID]TopologyDevice
	deviceAvailabilityByID     map[DeviceID]DeviceAvailability
	domains                    map[DeviceDomainID]DeviceDomain
	fabrics                    map[FabricDomainID]FabricDomain
	domainIDsByNodeAndResource map[NodeResourceKey][]DeviceDomainID
	healthyDeviceIDsByDomain   map[DeviceDomainID][]DeviceID
	availableDeviceIDsByDomain map[DeviceDomainID][]DeviceID
	// allocationRecoveryPending lists resources whose allocation adapter has
	// not yet reported the reservations that existed before this scheduler
	// started, so their availability is not known.
	allocationRecoveryPending map[corev1.ResourceName]struct{}
}

// NewTopologySnapshot creates an indexed immutable view from accepted facts.
func NewTopologySnapshot(revision TopologyRevision, facts map[ProviderNodeKey]NodeTopologyFacts, readiness map[ProviderNodeKey]ProviderNodeSyncState) *TopologySnapshot {
	return NewTopologySnapshotWithAvailability(revision, facts, readiness, nil)
}

// NewTopologySnapshotWithAvailability creates an indexed immutable view from
// accepted facts and scheduler-owned device availability. Missing availability
// states default to Available for healthy devices and Unavailable for unhealthy
// devices. Valid states for removed live devices are retained as ledger
// tombstones but do not participate in candidate indexes.
func NewTopologySnapshotWithAvailability(revision TopologyRevision, facts map[ProviderNodeKey]NodeTopologyFacts, readiness map[ProviderNodeKey]ProviderNodeSyncState, availability map[DeviceID]DeviceAvailability) *TopologySnapshot {
	return NewTopologySnapshotWithAllocationState(revision, facts, readiness, availability, nil)
}

// NewTopologySnapshotWithAllocationState creates an indexed immutable view like
// NewTopologySnapshotWithAvailability, and also records the resources whose
// allocation state is not recovered yet after scheduler startup.
func NewTopologySnapshotWithAllocationState(revision TopologyRevision, facts map[ProviderNodeKey]NodeTopologyFacts, readiness map[ProviderNodeKey]ProviderNodeSyncState, availability map[DeviceID]DeviceAvailability, recoveryPending map[corev1.ResourceName]struct{}) *TopologySnapshot {
	snapshot := &TopologySnapshot{
		allocationRecoveryPending:  make(map[corev1.ResourceName]struct{}, len(recoveryPending)),
		revision:                   revision,
		factsByProviderNode:        make(map[ProviderNodeKey]NodeTopologyFacts, len(facts)),
		providerNodeReadiness:      make(map[ProviderNodeKey]ProviderNodeSyncState, len(readiness)),
		devices:                    make(map[DeviceID]TopologyDevice),
		deviceAvailabilityByID:     make(map[DeviceID]DeviceAvailability, len(availability)),
		domains:                    make(map[DeviceDomainID]DeviceDomain),
		fabrics:                    make(map[FabricDomainID]FabricDomain),
		domainIDsByNodeAndResource: make(map[NodeResourceKey][]DeviceDomainID),
		healthyDeviceIDsByDomain:   make(map[DeviceDomainID][]DeviceID),
		availableDeviceIDsByDomain: make(map[DeviceDomainID][]DeviceID),
	}
	for resourceName := range recoveryPending {
		snapshot.allocationRecoveryPending[resourceName] = struct{}{}
	}
	for deviceID, state := range availability {
		if state.IsValid() {
			snapshot.deviceAvailabilityByID[deviceID] = state
		}
	}
	for key, state := range readiness {
		snapshot.providerNodeReadiness[key] = state
	}
	for key, factSet := range facts {
		factSet = factSet.Clone()
		snapshot.factsByProviderNode[key] = factSet
		for _, domain := range factSet.LocalDomains {
			snapshot.domains[domain.ID] = cloneDeviceDomain(domain)
			resourceKey := NodeResourceKey{NodeUID: domain.NodeUID, ResourceName: domain.ResourceName}
			snapshot.domainIDsByNodeAndResource[resourceKey] = append(snapshot.domainIDsByNodeAndResource[resourceKey], domain.ID)
		}
		for _, device := range factSet.Devices {
			snapshot.devices[device.ID] = cloneTopologyDevice(device)
			availability, exists := snapshot.deviceAvailabilityByID[device.ID]
			if !exists {
				availability = DeviceAvailable
				if device.Health != DeviceHealthy {
					availability = DeviceUnavailable
				}
			} else if device.Health != DeviceHealthy && availability == DeviceAvailable {
				availability = DeviceUnavailable
			}
			snapshot.deviceAvailabilityByID[device.ID] = availability
			if device.Health == DeviceHealthy {
				snapshot.healthyDeviceIDsByDomain[device.LocalDomainID] = append(snapshot.healthyDeviceIDsByDomain[device.LocalDomainID], device.ID)
				if availability == DeviceAvailable {
					snapshot.availableDeviceIDsByDomain[device.LocalDomainID] = append(snapshot.availableDeviceIDsByDomain[device.LocalDomainID], device.ID)
				}
			}
		}
	}
	for _, factSet := range snapshot.factsByProviderNode {
		for _, fabric := range factSet.Fabrics {
			if snapshot.hasCurrentFabricMembers(fabric) {
				snapshot.fabrics[fabric.ID] = cloneFabricDomain(fabric)
			}
		}
	}
	for _, domainIDs := range snapshot.domainIDsByNodeAndResource {
		sort.Slice(domainIDs, func(left, right int) bool {
			if domainIDs[left].ProviderID != domainIDs[right].ProviderID {
				return domainIDs[left].ProviderID < domainIDs[right].ProviderID
			}
			if domainIDs[left].Namespace != domainIDs[right].Namespace {
				return domainIDs[left].Namespace < domainIDs[right].Namespace
			}
			return domainIDs[left].Value < domainIDs[right].Value
		})
	}
	for _, deviceIDs := range snapshot.healthyDeviceIDsByDomain {
		sort.Slice(deviceIDs, func(left, right int) bool {
			return deviceIDs[left].String() < deviceIDs[right].String()
		})
	}
	for _, deviceIDs := range snapshot.availableDeviceIDsByDomain {
		sort.Slice(deviceIDs, func(left, right int) bool {
			return deviceIDs[left].String() < deviceIDs[right].String()
		})
	}
	return snapshot
}

// AllocationStateRecovered reports whether the scheduler knows which devices of
// a resource were already reserved or allocated before it started. Until then
// the resource's devices must not be selected for exact placement.
func (snapshot *TopologySnapshot) AllocationStateRecovered(resourceName corev1.ResourceName) bool {
	if snapshot == nil {
		return false
	}
	_, pending := snapshot.allocationRecoveryPending[resourceName]
	return !pending
}

// Revision returns the immutable snapshot revision.
func (snapshot *TopologySnapshot) Revision() TopologyRevision {
	if snapshot == nil {
		return 0
	}
	return snapshot.revision
}

// ProviderNodeReadiness returns the latest readiness for a provider and Node.
// Unknown pairs are pending by definition.
func (snapshot *TopologySnapshot) ProviderNodeReadiness(providerID string, nodeUID types.UID) ProviderNodeSyncState {
	if snapshot == nil {
		return ProviderNodePending
	}
	state, exists := snapshot.providerNodeReadiness[ProviderNodeKey{ProviderID: providerID, NodeUID: nodeUID}]
	if !exists {
		return ProviderNodePending
	}
	return state
}

// Facts returns a copy of a provider's accepted facts for a Node.
func (snapshot *TopologySnapshot) Facts(providerID string, nodeUID types.UID) (NodeTopologyFacts, bool) {
	if snapshot == nil {
		return NodeTopologyFacts{}, false
	}
	facts, exists := snapshot.factsByProviderNode[ProviderNodeKey{ProviderID: providerID, NodeUID: nodeUID}]
	if !exists {
		return NodeTopologyFacts{}, false
	}
	return facts.Clone(), true
}

// IsProviderNodeFreshAt reports whether an accepted provider fact set is still
// within its immutable effective freshness deadline at the supplied time.
func (snapshot *TopologySnapshot) IsProviderNodeFreshAt(providerID string, nodeUID types.UID, at time.Time) bool {
	if snapshot == nil || snapshot.ProviderNodeReadiness(providerID, nodeUID) != ProviderNodeSynced {
		return false
	}
	facts, exists := snapshot.factsByProviderNode[ProviderNodeKey{ProviderID: providerID, NodeUID: nodeUID}]
	if !exists || facts.FreshUntil.IsZero() {
		return false
	}
	return !at.After(facts.FreshUntil)
}

// LocalDomains returns copies of local domains matching a Node and resource.
func (snapshot *TopologySnapshot) LocalDomains(nodeUID types.UID, resourceName corev1.ResourceName) []DeviceDomain {
	if snapshot == nil {
		return nil
	}
	domainIDs := snapshot.domainIDsByNodeAndResource[NodeResourceKey{NodeUID: nodeUID, ResourceName: resourceName}]
	domains := make([]DeviceDomain, 0, len(domainIDs))
	for _, domainID := range domainIDs {
		domains = append(domains, cloneDeviceDomain(snapshot.domains[domainID]))
	}
	return domains
}

// HealthyDeviceIDs returns the currently healthy devices in one local domain.
func (snapshot *TopologySnapshot) HealthyDeviceIDs(domainID DeviceDomainID) []DeviceID {
	if snapshot == nil {
		return nil
	}
	return append([]DeviceID(nil), snapshot.healthyDeviceIDsByDomain[domainID]...)
}

// AvailableDeviceIDs returns healthy devices that the availability ledger marks
// free for a new topology placement in one local domain.
func (snapshot *TopologySnapshot) AvailableDeviceIDs(domainID DeviceDomainID) []DeviceID {
	if snapshot == nil {
		return nil
	}
	return append([]DeviceID(nil), snapshot.availableDeviceIDsByDomain[domainID]...)
}

// DeviceAvailability returns the scheduler-owned availability state for one
// current device or retained allocation tombstone.
func (snapshot *TopologySnapshot) DeviceAvailability(deviceID DeviceID) (DeviceAvailability, bool) {
	if snapshot == nil {
		return "", false
	}
	availability, exists := snapshot.deviceAvailabilityByID[deviceID]
	return availability, exists
}

// Device returns a copy of a device record.
func (snapshot *TopologySnapshot) Device(deviceID DeviceID) (TopologyDevice, bool) {
	if snapshot == nil {
		return TopologyDevice{}, false
	}
	device, exists := snapshot.devices[deviceID]
	if !exists {
		return TopologyDevice{}, false
	}
	return cloneTopologyDevice(device), true
}

// Fabric returns a copy of a currently valid cross-Node fabric. A fabric is
// unavailable when its owner or a member disappears, or when a member no
// longer publishes the generation and local domains the fabric references.
func (snapshot *TopologySnapshot) Fabric(fabricID FabricDomainID) (FabricDomain, bool) {
	if snapshot == nil {
		return FabricDomain{}, false
	}
	fabric, exists := snapshot.fabrics[fabricID]
	if !exists {
		return FabricDomain{}, false
	}
	return cloneFabricDomain(fabric), true
}

// Fabrics returns copies of all currently valid cross-Node fabrics in stable
// identity order. Invalidated owner or member facts are omitted.
func (snapshot *TopologySnapshot) Fabrics() []FabricDomain {
	if snapshot == nil {
		return nil
	}
	fabricIDs := make([]FabricDomainID, 0, len(snapshot.fabrics))
	for fabricID := range snapshot.fabrics {
		fabricIDs = append(fabricIDs, fabricID)
	}
	sort.Slice(fabricIDs, func(left, right int) bool {
		if fabricIDs[left].ProviderID != fabricIDs[right].ProviderID {
			return fabricIDs[left].ProviderID < fabricIDs[right].ProviderID
		}
		if fabricIDs[left].Namespace != fabricIDs[right].Namespace {
			return fabricIDs[left].Namespace < fabricIDs[right].Namespace
		}
		return fabricIDs[left].Value < fabricIDs[right].Value
	})
	fabrics := make([]FabricDomain, 0, len(fabricIDs))
	for _, fabricID := range fabricIDs {
		fabrics = append(fabrics, cloneFabricDomain(snapshot.fabrics[fabricID]))
	}
	return fabrics
}

// hasCurrentFabricMembers reports whether the fabric owner and every member
// still publish the generation and local domains referenced by the fabric.
func (snapshot *TopologySnapshot) hasCurrentFabricMembers(fabric FabricDomain) bool {
	owner, exists := snapshot.factsByProviderNode[ProviderNodeKey{ProviderID: fabric.OwnerProviderID, NodeUID: fabric.OwnerNodeUID}]
	if !exists || owner.SourceGeneration != fabric.SourceGeneration {
		return false
	}
	for _, member := range fabric.Members {
		memberFacts, exists := snapshot.factsByProviderNode[ProviderNodeKey{ProviderID: fabric.ID.ProviderID, NodeUID: member.NodeUID}]
		if !exists || memberFacts.NodeName != member.NodeName || memberFacts.SourceGeneration != member.SourceGeneration {
			return false
		}
		memberDomains := make(map[DeviceDomainID]struct{}, len(memberFacts.LocalDomains))
		for _, domain := range memberFacts.LocalDomains {
			memberDomains[domain.ID] = struct{}{}
		}
		for _, domainID := range member.LocalDomainIDs {
			if _, exists := memberDomains[domainID]; !exists {
				return false
			}
		}
	}
	return true
}

// cloneTopologyDevice returns an independent copy of a device record.
func cloneTopologyDevice(device TopologyDevice) TopologyDevice {
	clone := device
	clone.FabricIDs = append([]FabricDomainID(nil), device.FabricIDs...)
	return clone
}

// cloneDeviceDomain returns an independent copy of a local domain record.
func cloneDeviceDomain(domain DeviceDomain) DeviceDomain {
	clone := domain
	clone.DeviceIDs = append([]DeviceID(nil), domain.DeviceIDs...)
	return clone
}

// cloneFabricDomain returns an independent copy of a fabric and its members.
func cloneFabricDomain(fabric FabricDomain) FabricDomain {
	clone := fabric
	clone.Members = make([]FabricMember, len(fabric.Members))
	for index, member := range fabric.Members {
		clone.Members[index] = member
		clone.Members[index].LocalDomainIDs = append([]DeviceDomainID(nil), member.LocalDomainIDs...)
	}
	return clone
}
