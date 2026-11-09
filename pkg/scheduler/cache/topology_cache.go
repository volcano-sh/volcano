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
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/types"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
)

// deviceTopologyState is the mutable, cache-owned source of immutable device
// topology snapshots. SchedulerCache.Mutex protects every field.
type deviceTopologyState struct {
	revision          schedulingapi.TopologyRevision
	facts             map[schedulingapi.ProviderNodeKey]schedulingapi.NodeTopologyFacts
	readiness         map[schedulingapi.ProviderNodeKey]schedulingapi.ProviderNodeSyncState
	ledger            *deviceTopologyAvailabilityLedger
	reservationOwners map[schedulingapi.DeviceID]deviceTopologyReservationOwner
	// podAllocations records the devices each Pod holds after bind handoff,
	// one entry per adapter reservation, so one Pod can be released alone.
	podAllocations map[types.UID][]deviceTopologyPodAllocation
	// podReleases records the devices of Pods that ended or failed to bind,
	// until an adapter releases them.
	podReleases map[types.UID][]deviceTopologyPodAllocation
	snapshot    *schedulingapi.TopologySnapshot
}

// WithDeviceTopologyProviderRegistry configures the immutable authoritative
// provider ownership map used to accept device topology updates. A nil
// registry disables provider updates while preserving an empty snapshot.
func WithDeviceTopologyProviderRegistry(registry *provider.Registry) Option {
	return func(cache *SchedulerCache) {
		cache.deviceTopologyProviderRegistry = registry
	}
}

// ConfigureDeviceTopologyProviders installs the immutable provider ownership
// registry and source instances before SchedulerCache begins processing Nodes.
// A nil registry with no sources explicitly disables topology source sync.
func (sc *SchedulerCache) ConfigureDeviceTopologyProviders(registry *provider.Registry, sources []provider.NodeTopologyProvider) error {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	if sc.deviceTopologyProvidersFrozen {
		return fmt.Errorf("device topology providers cannot be configured after SchedulerCache starts")
	}
	if sc.deviceTopologyProvidersConfigured {
		return fmt.Errorf("device topology providers are already configured")
	}

	sc.deviceTopologyProviderRegistry = registry
	sc.deviceTopologyProviders = append([]provider.NodeTopologyProvider(nil), sources...)
	sc.deviceTopologyProvidersConfigured = true
	return nil
}

// freezeDeviceTopologyProviders prevents changes to source ownership once the
// cache begins informer processing. The caller does not need to hold the lock.
func (sc *SchedulerCache) freezeDeviceTopologyProviders() {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	sc.deviceTopologyProvidersFrozen = true
}

// WithDeviceTopologyAdapterRegistry configures the immutable allocation
// adapter ownership map used to reserve exact device IDs for hard topology.
// A nil registry leaves topology facts observable but prevents reservations.
func WithDeviceTopologyAdapterRegistry(registry *adapter.Registry) Option {
	return func(cache *SchedulerCache) {
		cache.deviceTopologyAdapterRegistry = registry
	}
}

// ConfigureDeviceTopologyAdapters installs the immutable allocation adapter
// registry before SchedulerCache begins processing Nodes. A nil registry
// leaves topology facts observable but prevents reservations.
func (sc *SchedulerCache) ConfigureDeviceTopologyAdapters(registry *adapter.Registry) error {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	if sc.deviceTopologyProvidersFrozen {
		return fmt.Errorf("device topology adapters cannot be configured after SchedulerCache starts")
	}
	if sc.deviceTopologyAdaptersConfigured {
		return fmt.Errorf("device topology adapters are already configured")
	}

	sc.deviceTopologyAdapterRegistry = registry
	sc.deviceTopologyAdaptersConfigured = true
	return nil
}

// newDeviceTopologyState creates an empty topology view. Unknown provider and
// Node pairs are pending by the TopologySnapshot contract.
func newDeviceTopologyState() *deviceTopologyState {
	state := &deviceTopologyState{
		facts:             make(map[schedulingapi.ProviderNodeKey]schedulingapi.NodeTopologyFacts),
		readiness:         make(map[schedulingapi.ProviderNodeKey]schedulingapi.ProviderNodeSyncState),
		ledger:            newDeviceTopologyAvailabilityLedger(),
		reservationOwners: make(map[schedulingapi.DeviceID]deviceTopologyReservationOwner),
		podAllocations:    make(map[types.UID][]deviceTopologyPodAllocation),
		podReleases:       make(map[types.UID][]deviceTopologyPodAllocation),
	}
	state.snapshot = schedulingapi.NewTopologySnapshotWithAvailability(state.revision, state.facts, state.readiness, state.ledger.snapshot())
	return state
}

// deviceTopologyMaxClockSkew is how far a source observation time may be ahead
// of the scheduler clock, to allow small clock differences between the
// publisher and the scheduler.
const deviceTopologyMaxClockSkew = time.Minute

// ApplyProviderNodeUpdate validates and atomically applies a provider's
// current facts for one cached Node. The update must match the cache's current
// Node UID and opaque resource version.
func (sc *SchedulerCache) ApplyProviderNodeUpdate(update provider.ProviderNodeUpdate) error {
	if err := update.Validate(); err != nil {
		return provider.NewSourceError(provider.SourceErrorInvalidTopology, err)
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()

	if err := sc.validateTopologyProviderOwnership(update); err != nil {
		return provider.NewSourceError(provider.SourceErrorUnauthorizedProvider, err)
	}
	if err := sc.validateCurrentTopologyNode(update); err != nil {
		return err
	}
	freshUntil, err := sc.effectiveFreshUntil(update, time.Now())
	if err != nil {
		return provider.NewSourceError(provider.SourceErrorInvalidTopology, err)
	}
	return sc.ensureDeviceTopologyState().apply(update, freshUntil)
}

// effectiveFreshUntil returns the freshness deadline the cache stores for the
// facts of a ReplaceFacts update: ObservedAt plus the provider's MaxAge, or the
// source's own FreshUntil when that is earlier. So a source cannot keep its
// facts fresh for longer than MaxAge without refreshing them. An observation
// time too far ahead of now is rejected, because it would do the same. The
// caller holds SchedulerCache.Mutex and has validated the update's provider.
func (sc *SchedulerCache) effectiveFreshUntil(update provider.ProviderNodeUpdate, now time.Time) (time.Time, error) {
	if update.Operation != provider.ReplaceFacts {
		return time.Time{}, nil
	}
	if update.ObservedAt.After(now.Add(deviceTopologyMaxClockSkew)) {
		return time.Time{}, fmt.Errorf("observation time %s is more than %s ahead of the scheduler clock", update.ObservedAt.UTC().Format(time.RFC3339), deviceTopologyMaxClockSkew)
	}
	maxAge, exists := sc.deviceTopologyProviderRegistry.MaxAge(update.ProviderID)
	if !exists {
		return time.Time{}, fmt.Errorf("provider %q has no configured max age", update.ProviderID)
	}
	freshUntil := update.ObservedAt.Add(maxAge)
	if !update.FreshUntil.IsZero() && update.FreshUntil.Before(freshUntil) {
		freshUntil = update.FreshUntil
	}
	return freshUntil, nil
}

// DeviceTopologySnapshot returns the current immutable topology view. The
// caller must treat the returned snapshot as read-only.
func (sc *SchedulerCache) DeviceTopologySnapshot() *schedulingapi.TopologySnapshot {
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	return sc.ensureDeviceTopologyState().snapshot
}

// ensureDeviceTopologyState creates the lazily allocated topology state. The
// caller must hold SchedulerCache.Mutex.
func (sc *SchedulerCache) ensureDeviceTopologyState() *deviceTopologyState {
	if sc.deviceTopology == nil {
		sc.deviceTopology = newDeviceTopologyState()
	}
	return sc.deviceTopology
}

// validateCurrentTopologyNode verifies that a provider did not race a Node
// replacement or newer informer update.
func (sc *SchedulerCache) validateCurrentTopologyNode(update provider.ProviderNodeUpdate) error {
	nodeInfo, exists := sc.Nodes[update.NodeName]
	if !exists || nodeInfo == nil || nodeInfo.Node == nil {
		return fmt.Errorf("provider update references Node %q that is not in SchedulerCache", update.NodeName)
	}
	node := nodeInfo.Node
	if node.Name != update.NodeName || node.UID != update.NodeUID || node.ResourceVersion != update.NodeResourceVersion {
		return fmt.Errorf("provider update for Node %q does not match the current Node UID and resource version", update.NodeName)
	}
	return nil
}

// validateTopologyProviderOwnership verifies that configuration selected the
// update provider for every resource it reports. The caller must hold
// SchedulerCache.Mutex.
func (sc *SchedulerCache) validateTopologyProviderOwnership(update provider.ProviderNodeUpdate) error {
	if sc.deviceTopologyProviderRegistry == nil {
		return fmt.Errorf("no device topology provider registry is configured")
	}
	return sc.deviceTopologyProviderRegistry.ValidateOwnership(update)
}

// apply updates complete source facts and publishes a new immutable view only
// when the externally visible topology or readiness state changes. The stored
// facts carry freshUntil, the effective freshness deadline, instead of the
// source's own deadline.
func (state *deviceTopologyState) apply(update provider.ProviderNodeUpdate, freshUntil time.Time) error {
	key := schedulingapi.ProviderNodeKey{ProviderID: update.ProviderID, NodeUID: update.NodeUID}
	if update.Operation == provider.ClearFacts {
		changed := false
		if _, exists := state.facts[key]; exists {
			delete(state.facts, key)
			changed = true
		}
		if state.readiness[key] != schedulingapi.ProviderNodeSynced {
			state.readiness[key] = schedulingapi.ProviderNodeSynced
			changed = true
		}
		if changed {
			state.publish()
		}
		return nil
	}

	facts := update.Facts.Clone()
	facts.FreshUntil = freshUntil
	previous, exists := state.facts[key]
	if exists {
		if update.SourceGeneration < previous.SourceGeneration {
			return provider.NewSourceError(provider.SourceErrorStaleGeneration, fmt.Errorf("source generation %d is older than accepted generation %d", update.SourceGeneration, previous.SourceGeneration))
		}
		if update.SourceGeneration == previous.SourceGeneration && !schedulingapi.SameTopologyContent(previous, facts) {
			return provider.NewSourceError(provider.SourceErrorGenerationReuseWithDifferentFacts, fmt.Errorf("source generation %d describes different topology content", update.SourceGeneration))
		}
	}
	if err := state.validateNoTopologyConflicts(key, facts); err != nil {
		return provider.NewSourceError(provider.SourceErrorInvalidTopology, err)
	}

	// Every Node update runs the providers again, so the facts carry the Node's
	// new resourceVersion even when nothing else changed. That field alone must
	// not publish a new revision, or any unrelated Node update would make
	// in-flight placement plans stale.
	previous.NodeResourceVersion = facts.NodeResourceVersion
	changed := !exists || !reflect.DeepEqual(previous, facts) || state.readiness[key] != schedulingapi.ProviderNodeSynced
	state.facts[key] = facts
	if !changed {
		return nil
	}
	state.readiness[key] = schedulingapi.ProviderNodeSynced
	state.publish()
	return nil
}

// removeNode discards every provider fact and readiness result tied to a Node
// UID, invalidating any fabric that depended on those facts in the next view.
func (state *deviceTopologyState) removeNode(nodeUID types.UID) {
	changed := false
	for key := range state.facts {
		if key.NodeUID == nodeUID {
			delete(state.facts, key)
			changed = true
		}
	}
	for key := range state.readiness {
		if key.NodeUID == nodeUID {
			delete(state.readiness, key)
			changed = true
		}
	}
	if changed {
		state.publish()
	}
}

// validateNoTopologyConflicts rejects provider-owned IDs that another accepted
// fact set already claims. The candidate replaces only its own fact set.
func (state *deviceTopologyState) validateNoTopologyConflicts(candidateKey schedulingapi.ProviderNodeKey, candidate schedulingapi.NodeTopologyFacts) error {
	candidateDevices := make(map[schedulingapi.DeviceID]struct{}, len(candidate.Devices))
	for _, device := range candidate.Devices {
		candidateDevices[device.ID] = struct{}{}
	}
	candidateFabrics := make(map[schedulingapi.FabricDomainID]struct{}, len(candidate.Fabrics))
	for _, fabric := range candidate.Fabrics {
		candidateFabrics[fabric.ID] = struct{}{}
	}

	for key, facts := range state.facts {
		if key == candidateKey {
			continue
		}
		for _, device := range facts.Devices {
			if _, exists := candidateDevices[device.ID]; exists {
				return fmt.Errorf("device %q is already claimed by provider %q on Node UID %q", device.ID.Value, key.ProviderID, key.NodeUID)
			}
		}
		for _, fabric := range facts.Fabrics {
			if _, exists := candidateFabrics[fabric.ID]; exists {
				return fmt.Errorf("fabric %q is already owned by provider %q on Node UID %q", fabric.ID.Value, key.ProviderID, key.NodeUID)
			}
		}
	}
	return nil
}

// publish increments the topology revision and rebuilds its immutable indexed
// view from the accepted fact sets.
func (state *deviceTopologyState) publish() {
	state.ledger.reconcile(state.facts)
	state.revision++
	state.snapshot = schedulingapi.NewTopologySnapshotWithAvailability(state.revision, state.facts, state.readiness, state.ledger.snapshot())
}
