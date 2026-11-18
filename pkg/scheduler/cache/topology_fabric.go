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
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

// resolvedFabricSet holds the fabrics of one owner fact set whose members are
// resolved. Each fabric stays pinned to the member Node UIDs and to the devices
// of each member local domain found when it was resolved. A member that is
// replaced, or whose fabric domain is missing or holds other devices, makes the
// fabric unavailable instead of silently moving it to the new member facts. A
// member generation that changes only content outside the fabric, such as
// device health, keeps the fabric. The set is dropped when the owner publishes
// a new generation, which resolves its declarations again.
type resolvedFabricSet struct {
	sourceGeneration uint64
	fabrics          map[schedulingapi.FabricDomainID]schedulingapi.FabricDomain
	// domainDeviceIDs holds the devices of every member local domain of the
	// fabrics when they were resolved.
	domainDeviceIDs map[schedulingapi.DeviceDomainID][]schedulingapi.DeviceID
}

// providerNodeName finds the current facts of a fabric member, which a
// declaration names by Node name.
type providerNodeName struct {
	providerID string
	nodeName   string
}

// resolveFabricDeclarations drops the resolved fabrics of owners that changed
// or disappeared, moves resolved fabrics to newer member generations that keep
// the pinned hardware, then resolves each declaration whose members are all
// present. A declaration with a member that is not synced yet stays pending,
// so the Nodes of a fabric can sync in any order, and is tried again on the
// next publish.
func (state *deviceTopologyState) resolveFabricDeclarations() {
	for key, resolved := range state.resolvedFabrics {
		if owner, exists := state.facts[key]; !exists || owner.SourceGeneration != resolved.sourceGeneration {
			delete(state.resolvedFabrics, key)
		}
	}

	var factsByNodeName map[providerNodeName][]schedulingapi.NodeTopologyFacts
	for key, owner := range state.facts {
		if len(owner.FabricDeclarations) == 0 {
			continue
		}
		resolved, exists := state.resolvedFabrics[key]
		if !exists {
			resolved = resolvedFabricSet{
				sourceGeneration: owner.SourceGeneration,
				fabrics:          make(map[schedulingapi.FabricDomainID]schedulingapi.FabricDomain, len(owner.FabricDeclarations)),
				domainDeviceIDs:  make(map[schedulingapi.DeviceDomainID][]schedulingapi.DeviceID),
			}
		}
		for _, declaration := range owner.FabricDeclarations {
			if fabric, done := resolved.fabrics[declaration.ID]; done {
				resolved.fabrics[declaration.ID] = state.followFabricMemberGenerations(fabric, resolved.domainDeviceIDs)
				continue
			}
			if factsByNodeName == nil {
				factsByNodeName = state.syncedFactsByNodeName()
			}
			if fabric, domainDeviceIDs, ok := resolveFabricDeclaration(owner, declaration, factsByNodeName); ok {
				resolved.fabrics[declaration.ID] = fabric
				for domainID, deviceIDs := range domainDeviceIDs {
					resolved.domainDeviceIDs[domainID] = deviceIDs
				}
			}
		}
		if len(resolved.fabrics) > 0 {
			state.resolvedFabrics[key] = resolved
		}
	}
}

// syncedFactsByNodeName indexes the accepted facts of synced provider Nodes by
// provider and Node name.
func (state *deviceTopologyState) syncedFactsByNodeName() map[providerNodeName][]schedulingapi.NodeTopologyFacts {
	index := make(map[providerNodeName][]schedulingapi.NodeTopologyFacts, len(state.facts))
	for key, facts := range state.facts {
		if state.readiness[key] != schedulingapi.ProviderNodeSynced {
			continue
		}
		name := providerNodeName{providerID: key.ProviderID, nodeName: facts.NodeName}
		index[name] = append(index[name], facts)
	}
	return index
}

// followFabricMemberGenerations moves each member of a resolved fabric to the
// member's newer source generation when the member is still the same Node and
// each of its fabric local domains holds exactly the devices pinned at
// resolution, such as after a device health change. Any other member change
// keeps the old generation, so the snapshot omits the fabric until the member
// matches the pinned hardware again or the owner declares the fabric again.
// It returns a changed copy and does not modify the given members.
func (state *deviceTopologyState) followFabricMemberGenerations(fabric schedulingapi.FabricDomain, domainDeviceIDs map[schedulingapi.DeviceDomainID][]schedulingapi.DeviceID) schedulingapi.FabricDomain {
	var members []schedulingapi.FabricMember
	for index, member := range fabric.Members {
		key := schedulingapi.ProviderNodeKey{ProviderID: fabric.ID.ProviderID, NodeUID: member.NodeUID}
		memberFacts, exists := state.facts[key]
		if !exists || state.readiness[key] != schedulingapi.ProviderNodeSynced || memberFacts.SourceGeneration == member.SourceGeneration || memberFacts.NodeName != member.NodeName {
			continue
		}
		if !holdsPinnedFabricDevices(memberFacts, member.LocalDomainIDs, domainDeviceIDs) {
			continue
		}
		if members == nil {
			members = append([]schedulingapi.FabricMember(nil), fabric.Members...)
		}
		members[index].SourceGeneration = memberFacts.SourceGeneration
	}
	if members != nil {
		fabric.Members = members
	}
	return fabric
}

// holdsPinnedFabricDevices reports whether every given local domain is in the
// member facts and holds exactly the devices pinned for it.
func holdsPinnedFabricDevices(memberFacts schedulingapi.NodeTopologyFacts, domainIDs []schedulingapi.DeviceDomainID, domainDeviceIDs map[schedulingapi.DeviceDomainID][]schedulingapi.DeviceID) bool {
	current := make(map[schedulingapi.DeviceDomainID][]schedulingapi.DeviceID, len(memberFacts.LocalDomains))
	for _, domain := range memberFacts.LocalDomains {
		current[domain.ID] = domain.DeviceIDs
	}
	for _, domainID := range domainIDs {
		deviceIDs, exists := current[domainID]
		pinned, pinnedExists := domainDeviceIDs[domainID]
		if !exists || !pinnedExists || !sameDeviceIDSet(deviceIDs, pinned) {
			return false
		}
	}
	return true
}

// sameDeviceIDSet reports whether two lists hold the same device IDs in any
// order. Device IDs in one local domain are unique.
func sameDeviceIDSet(left, right []schedulingapi.DeviceID) bool {
	if len(left) != len(right) {
		return false
	}
	set := make(map[schedulingapi.DeviceID]struct{}, len(left))
	for _, deviceID := range left {
		set[deviceID] = struct{}{}
	}
	for _, deviceID := range right {
		if _, exists := set[deviceID]; !exists {
			return false
		}
	}
	return true
}

// resolveFabricDeclaration resolves every member of one declaration to the
// member's current Node UID, source generation, and local domain IDs, and
// returns the devices of each of those local domains. It fails when a member
// Node has no synced facts, or when a named local domain is not in them.
func resolveFabricDeclaration(owner schedulingapi.NodeTopologyFacts, declaration schedulingapi.FabricDeclaration, factsByNodeName map[providerNodeName][]schedulingapi.NodeTopologyFacts) (schedulingapi.FabricDomain, map[schedulingapi.DeviceDomainID][]schedulingapi.DeviceID, bool) {
	fabric := schedulingapi.FabricDomain{
		ID:               declaration.ID,
		OwnerProviderID:  owner.ProviderID,
		OwnerNodeUID:     owner.NodeUID,
		SourceGeneration: owner.SourceGeneration,
		Members:          make([]schedulingapi.FabricMember, 0, len(declaration.Members)),
		Interconnect:     declaration.Interconnect,
	}
	domainDeviceIDs := make(map[schedulingapi.DeviceDomainID][]schedulingapi.DeviceID)
	for _, member := range declaration.Members {
		candidates := factsByNodeName[providerNodeName{providerID: owner.ProviderID, nodeName: member.NodeName}]
		if len(candidates) != 1 {
			return schedulingapi.FabricDomain{}, nil, false
		}
		memberFacts := candidates[0]
		memberDomains := make(map[schedulingapi.DeviceDomainID][]schedulingapi.DeviceID, len(memberFacts.LocalDomains))
		for _, domain := range memberFacts.LocalDomains {
			memberDomains[domain.ID] = domain.DeviceIDs
		}
		domainIDs := make([]schedulingapi.DeviceDomainID, 0, len(member.LocalDomainValues))
		for _, value := range member.LocalDomainValues {
			domainID := schedulingapi.DeviceDomainID{
				ProviderID: owner.ProviderID,
				Namespace:  declaration.ID.Namespace,
				NodeUID:    memberFacts.NodeUID,
				Value:      value,
			}
			deviceIDs, exists := memberDomains[domainID]
			if !exists {
				return schedulingapi.FabricDomain{}, nil, false
			}
			domainIDs = append(domainIDs, domainID)
			domainDeviceIDs[domainID] = append([]schedulingapi.DeviceID(nil), deviceIDs...)
		}
		fabric.Members = append(fabric.Members, schedulingapi.FabricMember{
			NodeName:         member.NodeName,
			NodeUID:          memberFacts.NodeUID,
			SourceGeneration: memberFacts.SourceGeneration,
			LocalDomainIDs:   domainIDs,
		})
	}
	return fabric, domainDeviceIDs, true
}

// factsWithResolvedFabrics returns the accepted facts with each owner's
// resolved fabrics added, for building a snapshot. The stored facts keep only
// the source declarations.
func (state *deviceTopologyState) factsWithResolvedFabrics() map[schedulingapi.ProviderNodeKey]schedulingapi.NodeTopologyFacts {
	if len(state.resolvedFabrics) == 0 {
		return state.facts
	}
	view := make(map[schedulingapi.ProviderNodeKey]schedulingapi.NodeTopologyFacts, len(state.facts))
	for key, facts := range state.facts {
		if resolved, exists := state.resolvedFabrics[key]; exists {
			fabrics := append([]schedulingapi.FabricDomain(nil), facts.Fabrics...)
			for _, declaration := range facts.FabricDeclarations {
				if fabric, resolved := resolved.fabrics[declaration.ID]; resolved {
					fabrics = append(fabrics, fabric)
				}
			}
			facts.Fabrics = fabrics
		}
		view[key] = facts
	}
	return view
}
