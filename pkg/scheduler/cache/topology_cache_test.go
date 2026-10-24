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
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
)

const (
	topologyCacheTestProviderID       = "test-provider"
	topologyCacheTestNamespace        = "test.example"
	topologyCacheTestSecondProviderID = "second-provider"
	topologyCacheTestSecondNamespace  = "second.example"
)

var topologyCacheTestObservedAt = time.Date(2026, time.October, 19, 10, 0, 0, 0, time.UTC)

func TestSchedulerCacheApplyProviderNodeUpdatePublishesTopologySnapshot(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologyTestSchedulerCache(t, node)
	update := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)

	if err := cache.ApplyProviderNodeUpdate(update); err != nil {
		t.Fatalf("ApplyProviderNodeUpdate() error = %v", err)
	}

	snapshot := cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 1 {
		t.Fatalf("Revision() = %d, want 1", snapshot.Revision())
	}
	if got := cache.DeviceTopologySnapshot().ProviderNodeReadiness(topologyCacheTestProviderID, node.UID); got != schedulingapi.ProviderNodeSynced {
		t.Fatalf("ProviderNodeReadiness() = %q, want %q", got, schedulingapi.ProviderNodeSynced)
	}
	facts, exists := snapshot.Facts(topologyCacheTestProviderID, node.UID)
	if !exists || len(facts.Devices) != 1 || facts.Devices[0].ID.Value != "device-a" {
		t.Fatalf("Facts() = (%#v, %t), want the accepted device facts", facts, exists)
	}
	domains := snapshot.LocalDomains(node.UID, corev1.ResourceName("nvidia.com/gpu"))
	if len(domains) != 1 || domains[0].ID.Value != "domain-a" {
		t.Fatalf("LocalDomains() = %#v, want the accepted local domain", domains)
	}
	healthyDeviceIDs := snapshot.HealthyDeviceIDs(domains[0].ID)
	if len(healthyDeviceIDs) != 1 || healthyDeviceIDs[0].Value != "device-a" {
		t.Fatalf("HealthyDeviceIDs() = %#v, want device-a", healthyDeviceIDs)
	}
	availableDeviceIDs := snapshot.AvailableDeviceIDs(domains[0].ID)
	if len(availableDeviceIDs) != 1 || availableDeviceIDs[0].Value != "device-a" {
		t.Fatalf("AvailableDeviceIDs() = %#v, want device-a", availableDeviceIDs)
	}
	if state, found := snapshot.DeviceAvailability(availableDeviceIDs[0]); !found || state != schedulingapi.DeviceAvailable {
		t.Fatalf("DeviceAvailability() = (%q, %v), want (%q, true)", state, found, schedulingapi.DeviceAvailable)
	}
}

func TestSchedulerCacheTopologyAvailabilityTracksProviderHealth(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologyTestSchedulerCache(t, node)
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("initial ApplyProviderNodeUpdate() error = %v", err)
	}

	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(node, 2, topologyCacheTestObservedAt.Add(time.Minute), "device-a", schedulingapi.DeviceUnhealthy)); err != nil {
		t.Fatalf("unhealthy ApplyProviderNodeUpdate() error = %v", err)
	}

	snapshot := cache.DeviceTopologySnapshot()
	domains := snapshot.LocalDomains(node.UID, corev1.ResourceName("nvidia.com/gpu"))
	if len(domains) != 1 {
		t.Fatalf("LocalDomains() = %#v, want one domain", domains)
	}
	if availableDeviceIDs := snapshot.AvailableDeviceIDs(domains[0].ID); len(availableDeviceIDs) != 0 {
		t.Fatalf("AvailableDeviceIDs() = %#v, want no unhealthy devices", availableDeviceIDs)
	}
	facts, found := snapshot.Facts(topologyCacheTestProviderID, node.UID)
	if !found || len(facts.Devices) != 1 {
		t.Fatalf("Facts() = (%#v, %v), want one unhealthy device", facts, found)
	}
	if state, found := snapshot.DeviceAvailability(facts.Devices[0].ID); !found || state != schedulingapi.DeviceUnavailable {
		t.Fatalf("DeviceAvailability() = (%q, %v), want (%q, true)", state, found, schedulingapi.DeviceUnavailable)
	}
}

func TestSchedulerCacheRejectsProviderUpdateForStaleNodeVersion(t *testing.T) {
	currentNode := topologyCacheTestNode("node-a", "node-a-uid", "2")
	cache := newTopologyTestSchedulerCache(t, currentNode)
	staleNode := currentNode.DeepCopy()
	staleNode.ResourceVersion = "1"

	err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(staleNode, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy))
	if err == nil || !strings.Contains(err.Error(), "does not match the current Node UID and resource version") {
		t.Fatalf("ApplyProviderNodeUpdate() error = %v, want stale Node delivery error", err)
	}
	if snapshot := cache.DeviceTopologySnapshot(); snapshot.Revision() != 0 {
		t.Fatalf("Revision() = %d, want 0 after rejected update", snapshot.Revision())
	}
}

func TestSchedulerCacheRejectsInvalidAndUnknownProviderUpdates(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologyTestSchedulerCache(t, node)

	invalid := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
	invalid.Facts = nil
	err := cache.ApplyProviderNodeUpdate(invalid)
	assertTopologySourceErrorReason(t, err, provider.SourceErrorInvalidTopology)

	unknownNode := topologyCacheTestNode("node-b", "node-b-uid", "1")
	err = cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(unknownNode, 1, topologyCacheTestObservedAt, "device-b", schedulingapi.DeviceHealthy))
	if err == nil || !strings.Contains(err.Error(), "is not in SchedulerCache") {
		t.Fatalf("ApplyProviderNodeUpdate() error = %v, want unknown Node error", err)
	}
	if snapshot := cache.DeviceTopologySnapshot(); snapshot.Revision() != 0 {
		t.Fatalf("Revision() = %d, want 0 after rejected updates", snapshot.Revision())
	}
}

func TestSchedulerCacheRejectsProviderUpdatesOutsideConfiguredOwnership(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	tests := []struct {
		name   string
		cache  *SchedulerCache
		update provider.ProviderNodeUpdate
	}{
		{
			name:   "no provider registry",
			cache:  newTopologyTestSchedulerCacheWithRegistry(t, nil, node),
			update: topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy),
		},
		{
			name:  "unconfigured provider clear facts",
			cache: newTopologyTestSchedulerCache(t, node),
			update: provider.ProviderNodeUpdate{
				ProviderID:          "other-provider",
				NodeName:            node.Name,
				NodeUID:             node.UID,
				NodeResourceVersion: node.ResourceVersion,
				ObservedAt:          topologyCacheTestObservedAt,
				Operation:           provider.ClearFacts,
			},
		},
		{
			name:  "unowned resource",
			cache: newTopologyTestSchedulerCache(t, node),
			update: func() provider.ProviderNodeUpdate {
				update := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
				setTopologyCacheTestResource(&update, "amd.com/gpu")
				return update
			}(),
		},
		{
			name:  "unexpected identity namespace",
			cache: newTopologyTestSchedulerCache(t, node),
			update: func() provider.ProviderNodeUpdate {
				update := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
				setTopologyCacheTestIdentityNamespace(&update, "unexpected.example")
				return update
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.cache.ApplyProviderNodeUpdate(test.update)
			assertTopologySourceErrorReason(t, err, provider.SourceErrorUnauthorizedProvider)
			if snapshot := test.cache.DeviceTopologySnapshot(); snapshot.Revision() != 0 {
				t.Fatalf("Revision() = %d, want 0 after rejected provider ownership update", snapshot.Revision())
			}
		})
	}
}

func TestSchedulerCacheInitialClearFactsCompletesProviderSync(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologyTestSchedulerCache(t, node)
	clearFacts := provider.ProviderNodeUpdate{
		ProviderID:          topologyCacheTestProviderID,
		NodeName:            node.Name,
		NodeUID:             node.UID,
		NodeResourceVersion: node.ResourceVersion,
		ObservedAt:          topologyCacheTestObservedAt,
		Operation:           provider.ClearFacts,
	}

	if err := cache.ApplyProviderNodeUpdate(clearFacts); err != nil {
		t.Fatalf("ApplyProviderNodeUpdate() error = %v", err)
	}
	snapshot := cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 1 {
		t.Fatalf("Revision() = %d, want 1 after initial ClearFacts", snapshot.Revision())
	}
	if _, exists := snapshot.Facts(topologyCacheTestProviderID, node.UID); exists {
		t.Fatal("Facts() returned topology after initial ClearFacts")
	}
	if got := snapshot.ProviderNodeReadiness(topologyCacheTestProviderID, node.UID); got != schedulingapi.ProviderNodeSynced {
		t.Fatalf("ProviderNodeReadiness() = %q, want %q", got, schedulingapi.ProviderNodeSynced)
	}
}

func TestSchedulerCacheEnforcesProviderGenerationRules(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologyTestSchedulerCache(t, node)
	accepted := topologyCacheTestUpdate(node, 2, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
	if err := cache.ApplyProviderNodeUpdate(accepted); err != nil {
		t.Fatalf("initial ApplyProviderNodeUpdate() error = %v", err)
	}

	stale := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt.Add(time.Minute), "device-a", schedulingapi.DeviceHealthy)
	err := cache.ApplyProviderNodeUpdate(stale)
	assertTopologySourceErrorReason(t, err, provider.SourceErrorStaleGeneration)

	conflicting := topologyCacheTestUpdate(node, 2, topologyCacheTestObservedAt.Add(2*time.Minute), "device-a", schedulingapi.DeviceUnhealthy)
	err = cache.ApplyProviderNodeUpdate(conflicting)
	assertTopologySourceErrorReason(t, err, provider.SourceErrorGenerationReuseWithDifferentFacts)

	snapshot := cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 1 {
		t.Fatalf("Revision() = %d, want 1 after rejected updates", snapshot.Revision())
	}
	facts, exists := snapshot.Facts(topologyCacheTestProviderID, node.UID)
	if !exists || facts.SourceGeneration != 2 || facts.Devices[0].Health != schedulingapi.DeviceHealthy {
		t.Fatalf("Facts() = (%#v, %t), want original generation-two healthy device", facts, exists)
	}
}

func TestSchedulerCacheAcceptsNewerProviderGenerationAndReplacesIndexes(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologyTestSchedulerCache(t, node)
	initial := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
	if err := cache.ApplyProviderNodeUpdate(initial); err != nil {
		t.Fatalf("initial ApplyProviderNodeUpdate() error = %v", err)
	}

	replacement := topologyCacheTestUpdate(node, 2, topologyCacheTestObservedAt.Add(time.Minute), "device-b", schedulingapi.DeviceHealthy)
	setTopologyCacheTestLocalDomainValue(&replacement, "domain-b")
	if err := cache.ApplyProviderNodeUpdate(replacement); err != nil {
		t.Fatalf("replacement ApplyProviderNodeUpdate() error = %v", err)
	}

	snapshot := cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 2 {
		t.Fatalf("Revision() = %d, want 2 after newer generation", snapshot.Revision())
	}
	facts, exists := snapshot.Facts(topologyCacheTestProviderID, node.UID)
	if !exists || facts.SourceGeneration != 2 || len(facts.Devices) != 1 || facts.Devices[0].ID.Value != "device-b" {
		t.Fatalf("Facts() = (%#v, %t), want generation-two device-b facts", facts, exists)
	}
	if oldDeviceIDs := snapshot.HealthyDeviceIDs(initial.Facts.LocalDomains[0].ID); len(oldDeviceIDs) != 0 {
		t.Fatalf("HealthyDeviceIDs(old domain) = %#v, want no replaced devices", oldDeviceIDs)
	}
	domains := snapshot.LocalDomains(node.UID, corev1.ResourceName("nvidia.com/gpu"))
	if len(domains) != 1 || domains[0].ID.Value != "domain-b" {
		t.Fatalf("LocalDomains() = %#v, want replacement domain-b", domains)
	}
	deviceIDs := snapshot.HealthyDeviceIDs(domains[0].ID)
	if len(deviceIDs) != 1 || deviceIDs[0].Value != "device-b" {
		t.Fatalf("HealthyDeviceIDs(replacement domain) = %#v, want device-b", deviceIDs)
	}
}

func TestSchedulerCacheRejectsStaleClearFactsAfterNodeRefresh(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologyTestSchedulerCache(t, node)
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("initial ApplyProviderNodeUpdate() error = %v", err)
	}

	refreshedNode := node.DeepCopy()
	refreshedNode.ResourceVersion = "2"
	if err := cache.AddOrUpdateNode(refreshedNode); err != nil {
		t.Fatalf("AddOrUpdateNode() error = %v", err)
	}
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(refreshedNode, 2, topologyCacheTestObservedAt.Add(time.Minute), "device-b", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("refreshed ApplyProviderNodeUpdate() error = %v", err)
	}

	staleClearFacts := provider.ProviderNodeUpdate{
		ProviderID:          topologyCacheTestProviderID,
		NodeName:            node.Name,
		NodeUID:             node.UID,
		NodeResourceVersion: node.ResourceVersion,
		ObservedAt:          topologyCacheTestObservedAt.Add(2 * time.Minute),
		Operation:           provider.ClearFacts,
	}
	err := cache.ApplyProviderNodeUpdate(staleClearFacts)
	if err == nil || !strings.Contains(err.Error(), "does not match the current Node UID and resource version") {
		t.Fatalf("ApplyProviderNodeUpdate() error = %v, want stale ClearFacts delivery error", err)
	}

	snapshot := cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 2 {
		t.Fatalf("Revision() = %d, want 2 after rejected stale ClearFacts", snapshot.Revision())
	}
	facts, exists := snapshot.Facts(topologyCacheTestProviderID, node.UID)
	if !exists || facts.SourceGeneration != 2 || len(facts.Devices) != 1 || facts.Devices[0].ID.Value != "device-b" {
		t.Fatalf("Facts() = (%#v, %t), want the refreshed generation-two facts", facts, exists)
	}
}

func TestSchedulerCacheAcceptsMetadataHeartbeat(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologyTestSchedulerCache(t, node)
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(node, 3, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("initial ApplyProviderNodeUpdate() error = %v", err)
	}

	refreshedNode := node.DeepCopy()
	refreshedNode.ResourceVersion = "2"
	if err := cache.AddOrUpdateNode(refreshedNode); err != nil {
		t.Fatalf("AddOrUpdateNode() error = %v", err)
	}
	refreshedAt := topologyCacheTestObservedAt.Add(time.Minute)
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(refreshedNode, 3, refreshedAt, "device-a", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("heartbeat ApplyProviderNodeUpdate() error = %v", err)
	}

	snapshot := cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 2 {
		t.Fatalf("Revision() = %d, want 2 after metadata heartbeat", snapshot.Revision())
	}
	facts, exists := snapshot.Facts(topologyCacheTestProviderID, refreshedNode.UID)
	if !exists || facts.NodeResourceVersion != refreshedNode.ResourceVersion || !facts.ObservedAt.Equal(refreshedAt) {
		t.Fatalf("Facts() = (%#v, %t), want refreshed delivery metadata", facts, exists)
	}
}

func TestSchedulerCacheSnapshotCapturesImmutableDeviceTopology(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologySnapshotTestSchedulerCache(t, node)
	initial := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
	if err := cache.ApplyProviderNodeUpdate(initial); err != nil {
		t.Fatalf("initial ApplyProviderNodeUpdate() error = %v", err)
	}

	firstClusterSnapshot := cache.Snapshot()
	if firstClusterSnapshot.DeviceTopology == nil {
		t.Fatal("Snapshot().DeviceTopology = nil, want immutable topology snapshot")
	}
	if firstClusterSnapshot.DeviceTopology != cache.DeviceTopologySnapshot() {
		t.Fatal("Snapshot().DeviceTopology does not reference the cache's current immutable topology snapshot")
	}
	if firstClusterSnapshot.DeviceTopology.Revision() != 1 {
		t.Fatalf("first Snapshot().DeviceTopology.Revision() = %d, want 1", firstClusterSnapshot.DeviceTopology.Revision())
	}

	replacement := topologyCacheTestUpdate(node, 2, topologyCacheTestObservedAt.Add(time.Minute), "device-b", schedulingapi.DeviceHealthy)
	if err := cache.ApplyProviderNodeUpdate(replacement); err != nil {
		t.Fatalf("replacement ApplyProviderNodeUpdate() error = %v", err)
	}

	secondClusterSnapshot := cache.Snapshot()
	if firstClusterSnapshot.DeviceTopology.Revision() != 1 {
		t.Fatalf("first Snapshot().DeviceTopology.Revision() = %d, want immutable revision 1", firstClusterSnapshot.DeviceTopology.Revision())
	}
	if secondClusterSnapshot.DeviceTopology == firstClusterSnapshot.DeviceTopology {
		t.Fatal("replacement topology update reused the previous immutable snapshot")
	}
	if secondClusterSnapshot.DeviceTopology.Revision() != 2 {
		t.Fatalf("second Snapshot().DeviceTopology.Revision() = %d, want 2", secondClusterSnapshot.DeviceTopology.Revision())
	}
	facts, exists := secondClusterSnapshot.DeviceTopology.Facts(topologyCacheTestProviderID, node.UID)
	if !exists || facts.SourceGeneration != 2 || len(facts.Devices) != 1 || facts.Devices[0].ID.Value != "device-b" {
		t.Fatalf("second Snapshot().DeviceTopology.Facts() = (%#v, %t), want generation-two device-b facts", facts, exists)
	}
}

func TestSchedulerCacheDoesNotPublishDuplicateProviderUpdate(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologyTestSchedulerCache(t, node)
	update := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
	if err := cache.ApplyProviderNodeUpdate(update); err != nil {
		t.Fatalf("first ApplyProviderNodeUpdate() error = %v", err)
	}
	if err := cache.ApplyProviderNodeUpdate(update); err != nil {
		t.Fatalf("duplicate ApplyProviderNodeUpdate() error = %v", err)
	}
	if snapshot := cache.DeviceTopologySnapshot(); snapshot.Revision() != 1 {
		t.Fatalf("Revision() = %d, want 1 after duplicate update", snapshot.Revision())
	}
}

func TestSchedulerCacheCapsProviderFreshnessAtMaxAge(t *testing.T) {
	tests := []struct {
		name string
		// sourceFreshUntil is the source deadline after ObservedAt; zero
		// means the source sets no deadline.
		sourceFreshUntil time.Duration
		wantFreshUntil   time.Duration
	}{
		{
			name:             "a source deadline later than the max age is capped",
			sourceFreshUntil: 24 * time.Hour,
			wantFreshUntil:   provider.DefaultMaxAge,
		},
		{
			name:             "an earlier source deadline is kept",
			sourceFreshUntil: 30 * time.Second,
			wantFreshUntil:   30 * time.Second,
		},
		{
			name:           "without a source deadline the max age applies",
			wantFreshUntil: provider.DefaultMaxAge,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := topologyCacheTestNode("node-a", "node-a-uid", "1")
			cache := newTopologyTestSchedulerCache(t, node)
			update := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
			update.FreshUntil = time.Time{}
			if test.sourceFreshUntil != 0 {
				update.FreshUntil = topologyCacheTestObservedAt.Add(test.sourceFreshUntil)
			}
			update.Facts.FreshUntil = update.FreshUntil

			if err := cache.ApplyProviderNodeUpdate(update); err != nil {
				t.Fatalf("ApplyProviderNodeUpdate() error = %v", err)
			}

			snapshot := cache.DeviceTopologySnapshot()
			wantFreshUntil := topologyCacheTestObservedAt.Add(test.wantFreshUntil)
			facts, exists := snapshot.Facts(topologyCacheTestProviderID, node.UID)
			if !exists || !facts.FreshUntil.Equal(wantFreshUntil) {
				t.Fatalf("Facts().FreshUntil = %s (exists %t), want %s", facts.FreshUntil, exists, wantFreshUntil)
			}
			if !snapshot.IsProviderNodeFreshAt(topologyCacheTestProviderID, node.UID, wantFreshUntil) {
				t.Fatal("IsProviderNodeFreshAt(deadline) = false, want true")
			}
			if snapshot.IsProviderNodeFreshAt(topologyCacheTestProviderID, node.UID, wantFreshUntil.Add(time.Nanosecond)) {
				t.Fatal("IsProviderNodeFreshAt(after deadline) = true, want false")
			}
		})
	}
}

func TestSchedulerCacheUsesConfiguredProviderMaxAge(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	registry, err := provider.NewRegistry([]provider.Registration{{
		ProviderID:        topologyCacheTestProviderID,
		IdentityNamespace: topologyCacheTestNamespace,
		ResourceNames:     []corev1.ResourceName{"nvidia.com/gpu"},
		MaxAge:            10 * time.Minute,
	}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	cache := newTopologyTestSchedulerCacheWithRegistry(t, registry, node)
	update := topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
	update.FreshUntil = topologyCacheTestObservedAt.Add(time.Hour)
	update.Facts.FreshUntil = update.FreshUntil

	if err := cache.ApplyProviderNodeUpdate(update); err != nil {
		t.Fatalf("ApplyProviderNodeUpdate() error = %v", err)
	}

	facts, _ := cache.DeviceTopologySnapshot().Facts(topologyCacheTestProviderID, node.UID)
	if want := topologyCacheTestObservedAt.Add(10 * time.Minute); !facts.FreshUntil.Equal(want) {
		t.Fatalf("Facts().FreshUntil = %s, want %s", facts.FreshUntil, want)
	}
}

func TestSchedulerCacheRejectsProviderObservationTimeInTheFuture(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologyTestSchedulerCache(t, node)

	err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(node, 1, time.Now().Add(time.Hour), "device-a", schedulingapi.DeviceHealthy))
	assertTopologySourceErrorReason(t, err, provider.SourceErrorInvalidTopology)
	if _, exists := cache.DeviceTopologySnapshot().Facts(topologyCacheTestProviderID, node.UID); exists {
		t.Fatal("Facts() exists after a rejected future observation time, want none")
	}

	now := topologyCacheTestObservedAt
	withinSkew := topologyCacheTestUpdate(node, 1, now.Add(deviceTopologyMaxClockSkew), "device-a", schedulingapi.DeviceHealthy)
	if _, err := cache.effectiveFreshUntil(withinSkew, now); err != nil {
		t.Fatalf("effectiveFreshUntil(observed at the skew limit) error = %v, want nil", err)
	}
	beyondSkew := topologyCacheTestUpdate(node, 1, now.Add(deviceTopologyMaxClockSkew+time.Nanosecond), "device-a", schedulingapi.DeviceHealthy)
	if _, err := cache.effectiveFreshUntil(beyondSkew, now); err == nil {
		t.Fatal("effectiveFreshUntil(observed beyond the skew limit) error = nil, want rejection")
	}
}

func TestSchedulerCacheDoesNotPublishNodeResourceVersionOnlyChange(t *testing.T) {
	node := topologyCacheTestNode("node-a", "node-a-uid", "1")
	cache := newTopologyTestSchedulerCache(t, node)
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("initial ApplyProviderNodeUpdate() error = %v", err)
	}

	// An unrelated Node change, such as a label or status update, gives the
	// Node a new resourceVersion while the published topology stays the same.
	relabeledNode := node.DeepCopy()
	relabeledNode.ResourceVersion = "2"
	if err := cache.AddOrUpdateNode(relabeledNode); err != nil {
		t.Fatalf("AddOrUpdateNode() error = %v", err)
	}
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(relabeledNode, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("unchanged ApplyProviderNodeUpdate() error = %v", err)
	}
	if snapshot := cache.DeviceTopologySnapshot(); snapshot.Revision() != 1 {
		t.Fatalf("Revision() = %d, want 1 after a Node resourceVersion-only change", snapshot.Revision())
	}

	// The next real change publishes the latest Node resourceVersion.
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(relabeledNode, 2, topologyCacheTestObservedAt.Add(time.Minute), "device-b", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("changed ApplyProviderNodeUpdate() error = %v", err)
	}
	snapshot := cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 2 {
		t.Fatalf("Revision() = %d, want 2 after a topology change", snapshot.Revision())
	}
	facts, exists := snapshot.Facts(topologyCacheTestProviderID, node.UID)
	if !exists || facts.NodeResourceVersion != relabeledNode.ResourceVersion {
		t.Fatalf("Facts() = (%#v, %t), want Node resourceVersion %q", facts, exists, relabeledNode.ResourceVersion)
	}
}

func TestSchedulerCacheClearsTopologyForNodeReplacementAndDeletion(t *testing.T) {
	oldNode := topologyCacheTestNode("node-a", "node-a-old", "1")
	cache := newTopologyTestSchedulerCache(t, oldNode)
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(oldNode, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("initial ApplyProviderNodeUpdate() error = %v", err)
	}

	replacementNode := topologyCacheTestNode("node-a", "node-a-new", "1")
	if err := cache.AddOrUpdateNode(replacementNode); err != nil {
		t.Fatalf("AddOrUpdateNode() replacement error = %v", err)
	}
	snapshot := cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 2 {
		t.Fatalf("Revision() = %d, want 2 after Node replacement", snapshot.Revision())
	}
	if _, exists := snapshot.Facts(topologyCacheTestProviderID, oldNode.UID); exists {
		t.Fatal("Facts() returned replaced Node topology")
	}
	if got := snapshot.ProviderNodeReadiness(topologyCacheTestProviderID, oldNode.UID); got != schedulingapi.ProviderNodePending {
		t.Fatalf("old ProviderNodeReadiness() = %q, want %q", got, schedulingapi.ProviderNodePending)
	}

	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(replacementNode, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("replacement ApplyProviderNodeUpdate() error = %v", err)
	}
	clearFacts := provider.ProviderNodeUpdate{
		ProviderID:          topologyCacheTestProviderID,
		NodeName:            replacementNode.Name,
		NodeUID:             replacementNode.UID,
		NodeResourceVersion: replacementNode.ResourceVersion,
		ObservedAt:          topologyCacheTestObservedAt.Add(time.Minute),
		Operation:           provider.ClearFacts,
	}
	if err := cache.ApplyProviderNodeUpdate(clearFacts); err != nil {
		t.Fatalf("ClearFacts ApplyProviderNodeUpdate() error = %v", err)
	}
	snapshot = cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 4 {
		t.Fatalf("Revision() = %d, want 4 after ClearFacts", snapshot.Revision())
	}
	if _, exists := snapshot.Facts(topologyCacheTestProviderID, replacementNode.UID); exists {
		t.Fatal("Facts() returned cleared topology")
	}
	if got := snapshot.ProviderNodeReadiness(topologyCacheTestProviderID, replacementNode.UID); got != schedulingapi.ProviderNodeSynced {
		t.Fatalf("ProviderNodeReadiness() = %q, want %q after ClearFacts", got, schedulingapi.ProviderNodeSynced)
	}
	if err := cache.ApplyProviderNodeUpdate(clearFacts); err != nil {
		t.Fatalf("duplicate ClearFacts ApplyProviderNodeUpdate() error = %v", err)
	}
	if snapshot := cache.DeviceTopologySnapshot(); snapshot.Revision() != 4 {
		t.Fatalf("Revision() = %d, want 4 after duplicate ClearFacts", snapshot.Revision())
	}

	if err := cache.RemoveNode(replacementNode.Name); err != nil {
		t.Fatalf("RemoveNode() error = %v", err)
	}
	snapshot = cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 5 {
		t.Fatalf("Revision() = %d, want 5 after Node deletion", snapshot.Revision())
	}
	if got := snapshot.ProviderNodeReadiness(topologyCacheTestProviderID, replacementNode.UID); got != schedulingapi.ProviderNodePending {
		t.Fatalf("ProviderNodeReadiness() = %q, want %q after Node deletion", got, schedulingapi.ProviderNodePending)
	}
}

func TestSchedulerCacheRemovesFactsForAllProvidersOnNodeLifecycle(t *testing.T) {
	registry, err := provider.NewRegistry([]provider.Registration{
		{
			ProviderID:        topologyCacheTestProviderID,
			IdentityNamespace: topologyCacheTestNamespace,
			ResourceNames:     []corev1.ResourceName{"nvidia.com/gpu"},
		},
		{
			ProviderID:        topologyCacheTestSecondProviderID,
			IdentityNamespace: topologyCacheTestSecondNamespace,
			ResourceNames:     []corev1.ResourceName{"amd.com/gpu"},
		},
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	tests := []struct {
		name      string
		lifecycle func(*testing.T, *SchedulerCache, *corev1.Node)
	}{
		{
			name: "replacement",
			lifecycle: func(t *testing.T, cache *SchedulerCache, node *corev1.Node) {
				t.Helper()
				replacement := topologyCacheTestNode(node.Name, "node-a-replacement-uid", "1")
				if err := cache.AddOrUpdateNode(replacement); err != nil {
					t.Fatalf("AddOrUpdateNode() replacement error = %v", err)
				}
			},
		},
		{
			name: "deletion",
			lifecycle: func(t *testing.T, cache *SchedulerCache, node *corev1.Node) {
				t.Helper()
				if err := cache.RemoveNode(node.Name); err != nil {
					t.Fatalf("RemoveNode() error = %v", err)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			node := topologyCacheTestNode("node-a", "node-a-uid", "1")
			cache := newTopologyTestSchedulerCacheWithRegistry(t, registry, node)
			updates := []provider.ProviderNodeUpdate{
				topologyCacheTestUpdate(node, 1, topologyCacheTestObservedAt, "nvidia-device-a", schedulingapi.DeviceHealthy),
				topologyCacheTestUpdateForProvider(node, topologyCacheTestSecondProviderID, topologyCacheTestSecondNamespace, "amd.com/gpu", 1, topologyCacheTestObservedAt, "amd-device-a", schedulingapi.DeviceHealthy),
			}
			for _, update := range updates {
				if err := cache.ApplyProviderNodeUpdate(update); err != nil {
					t.Fatalf("ApplyProviderNodeUpdate(%q) error = %v", update.ProviderID, err)
				}
			}

			test.lifecycle(t, cache, node)

			snapshot := cache.DeviceTopologySnapshot()
			if snapshot.Revision() != 3 {
				t.Fatalf("Revision() = %d, want 3 after %s", snapshot.Revision(), test.name)
			}
			for _, providerID := range []string{topologyCacheTestProviderID, topologyCacheTestSecondProviderID} {
				if _, exists := snapshot.Facts(providerID, node.UID); exists {
					t.Fatalf("Facts(%q, %q) returned removed topology", providerID, node.UID)
				}
				if got := snapshot.ProviderNodeReadiness(providerID, node.UID); got != schedulingapi.ProviderNodePending {
					t.Fatalf("ProviderNodeReadiness(%q, %q) = %q, want %q", providerID, node.UID, got, schedulingapi.ProviderNodePending)
				}
			}
		})
	}
}

func TestSchedulerCacheRejectsConflictingDeviceOwnership(t *testing.T) {
	nodeA := topologyCacheTestNode("node-a", "node-a-uid", "1")
	nodeB := topologyCacheTestNode("node-b", "node-b-uid", "1")
	cache := newTopologyTestSchedulerCache(t, nodeA, nodeB)
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(nodeA, 1, topologyCacheTestObservedAt, "shared-device", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("first ApplyProviderNodeUpdate() error = %v", err)
	}

	err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(nodeB, 1, topologyCacheTestObservedAt, "shared-device", schedulingapi.DeviceHealthy))
	assertTopologySourceErrorReason(t, err, provider.SourceErrorInvalidTopology)

	snapshot := cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 1 {
		t.Fatalf("Revision() = %d, want 1 after rejected device conflict", snapshot.Revision())
	}
	if _, exists := snapshot.Facts(topologyCacheTestProviderID, nodeB.UID); exists {
		t.Fatal("Facts() accepted conflicting device ownership for node-b")
	}
	facts, exists := snapshot.Facts(topologyCacheTestProviderID, nodeA.UID)
	if !exists || len(facts.Devices) != 1 || facts.Devices[0].ID.Value != "shared-device" {
		t.Fatalf("Facts() = (%#v, %t), want the original node-a device facts", facts, exists)
	}
}

func TestSchedulerCacheKeepsExistingFactsAfterConflictingReplacement(t *testing.T) {
	nodeA := topologyCacheTestNode("node-a", "node-a-uid", "1")
	nodeB := topologyCacheTestNode("node-b", "node-b-uid", "1")
	cache := newTopologyTestSchedulerCache(t, nodeA, nodeB)
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(nodeA, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("node-a ApplyProviderNodeUpdate() error = %v", err)
	}
	if err := cache.ApplyProviderNodeUpdate(topologyCacheTestUpdate(nodeB, 1, topologyCacheTestObservedAt, "device-b", schedulingapi.DeviceHealthy)); err != nil {
		t.Fatalf("node-b ApplyProviderNodeUpdate() error = %v", err)
	}

	conflictingReplacement := topologyCacheTestUpdate(nodeA, 2, topologyCacheTestObservedAt.Add(time.Minute), "device-b", schedulingapi.DeviceHealthy)
	err := cache.ApplyProviderNodeUpdate(conflictingReplacement)
	assertTopologySourceErrorReason(t, err, provider.SourceErrorInvalidTopology)

	snapshot := cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 2 {
		t.Fatalf("Revision() = %d, want 2 after rejected replacement", snapshot.Revision())
	}
	facts, exists := snapshot.Facts(topologyCacheTestProviderID, nodeA.UID)
	if !exists || facts.SourceGeneration != 1 || len(facts.Devices) != 1 || facts.Devices[0].ID.Value != "device-a" {
		t.Fatalf("Facts() = (%#v, %t), want the original node-a facts", facts, exists)
	}
}

func TestSchedulerCacheRejectsConflictingFabricOwnership(t *testing.T) {
	nodeA := topologyCacheTestNode("node-a", "node-a-uid", "1")
	nodeB := topologyCacheTestNode("node-b", "node-b-uid", "1")
	cache := newTopologyTestSchedulerCache(t, nodeA, nodeB)
	updateA := topologyCacheTestUpdate(nodeA, 1, topologyCacheTestObservedAt, "device-a", schedulingapi.DeviceHealthy)
	addTopologyCacheTestFabric(&updateA, "fabric-a")
	if err := cache.ApplyProviderNodeUpdate(updateA); err != nil {
		t.Fatalf("first ApplyProviderNodeUpdate() error = %v", err)
	}

	updateB := topologyCacheTestUpdate(nodeB, 1, topologyCacheTestObservedAt, "device-b", schedulingapi.DeviceHealthy)
	addTopologyCacheTestFabric(&updateB, "fabric-a")
	err := cache.ApplyProviderNodeUpdate(updateB)
	assertTopologySourceErrorReason(t, err, provider.SourceErrorInvalidTopology)

	snapshot := cache.DeviceTopologySnapshot()
	if snapshot.Revision() != 1 {
		t.Fatalf("Revision() = %d, want 1 after rejected fabric conflict", snapshot.Revision())
	}
	if _, exists := snapshot.Facts(topologyCacheTestProviderID, nodeB.UID); exists {
		t.Fatal("Facts() accepted conflicting fabric ownership for node-b")
	}
	facts, exists := snapshot.Facts(topologyCacheTestProviderID, nodeA.UID)
	if !exists || len(facts.Fabrics) != 1 || facts.Fabrics[0].ID.Value != "fabric-a" {
		t.Fatalf("Facts() = (%#v, %t), want the original node-a fabric facts", facts, exists)
	}
}

func newTopologyTestSchedulerCache(t *testing.T, nodes ...*corev1.Node) *SchedulerCache {
	t.Helper()
	return newTopologyTestSchedulerCacheWithRegistry(t, newTopologyTestProviderRegistry(t), nodes...)
}

func newTopologySnapshotTestSchedulerCache(t *testing.T, nodes ...*corev1.Node) *SchedulerCache {
	t.Helper()
	cache := NewDefaultMockSchedulerCache("device-topology-snapshot-test")
	t.Cleanup(func() {
		ShutdownMockSchedulerCache(cache)
	})
	WithDeviceTopologyProviderRegistry(newTopologyTestProviderRegistry(t))(cache)
	for _, node := range nodes {
		if err := cache.AddOrUpdateNode(node); err != nil {
			t.Fatalf("AddOrUpdateNode(%q) error = %v", node.Name, err)
		}
	}
	return cache
}

func newTopologyTestProviderRegistry(t *testing.T) *provider.Registry {
	t.Helper()
	registry, err := provider.NewRegistry([]provider.Registration{{
		ProviderID:        topologyCacheTestProviderID,
		IdentityNamespace: topologyCacheTestNamespace,
		ResourceNames:     []corev1.ResourceName{"nvidia.com/gpu"},
	}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	return registry
}

func newTopologyTestSchedulerCacheWithRegistry(t *testing.T, registry *provider.Registry, nodes ...*corev1.Node) *SchedulerCache {
	t.Helper()
	cache := &SchedulerCache{
		Nodes:       make(map[string]*schedulingapi.NodeInfo),
		NodeList:    []string{},
		imageStates: make(map[string]*imageState),
	}
	WithDeviceTopologyProviderRegistry(registry)(cache)
	for _, node := range nodes {
		if err := cache.AddOrUpdateNode(node); err != nil {
			t.Fatalf("AddOrUpdateNode(%q) error = %v", node.Name, err)
		}
	}
	return cache
}

func setTopologyCacheTestResource(update *provider.ProviderNodeUpdate, resourceName corev1.ResourceName) {
	update.Facts.Devices[0].ResourceName = resourceName
	update.Facts.LocalDomains[0].ResourceName = resourceName
}

func setTopologyCacheTestIdentityNamespace(update *provider.ProviderNodeUpdate, identityNamespace string) {
	update.Facts.Devices[0].ID.Namespace = identityNamespace
	update.Facts.Devices[0].LocalDomainID.Namespace = identityNamespace
	update.Facts.LocalDomains[0].ID.Namespace = identityNamespace
	update.Facts.LocalDomains[0].DeviceIDs[0].Namespace = identityNamespace
}

func setTopologyCacheTestLocalDomainValue(update *provider.ProviderNodeUpdate, domainValue string) {
	domainID := update.Facts.LocalDomains[0].ID
	domainID.Value = domainValue
	update.Facts.Devices[0].LocalDomainID = domainID
	update.Facts.LocalDomains[0].ID = domainID
}

func topologyCacheTestNode(name string, uid types.UID, resourceVersion string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:            name,
		UID:             uid,
		ResourceVersion: resourceVersion,
	}}
}

func topologyCacheTestUpdate(node *corev1.Node, generation uint64, observedAt time.Time, deviceValue string, health schedulingapi.DeviceHealth) provider.ProviderNodeUpdate {
	return topologyCacheTestUpdateForProvider(node, topologyCacheTestProviderID, topologyCacheTestNamespace, "nvidia.com/gpu", generation, observedAt, deviceValue, health)
}

func topologyCacheTestUpdateForProvider(node *corev1.Node, providerID, identityNamespace string, resourceName corev1.ResourceName, generation uint64, observedAt time.Time, deviceValue string, health schedulingapi.DeviceHealth) provider.ProviderNodeUpdate {
	freshUntil := observedAt.Add(5 * time.Minute)
	domainID := schedulingapi.DeviceDomainID{
		ProviderID: providerID,
		Namespace:  identityNamespace,
		NodeUID:    node.UID,
		Value:      "domain-a",
	}
	deviceID := schedulingapi.DeviceID{
		ProviderID: providerID,
		Namespace:  identityNamespace,
		Value:      deviceValue,
	}
	facts := schedulingapi.NodeTopologyFacts{
		ProviderID:          providerID,
		NodeName:            node.Name,
		NodeUID:             node.UID,
		NodeResourceVersion: node.ResourceVersion,
		SourceGeneration:    generation,
		ObservedAt:          observedAt,
		FreshUntil:          freshUntil,
		Devices: []schedulingapi.TopologyDevice{{
			ID:            deviceID,
			ResourceName:  resourceName,
			NodeName:      node.Name,
			NodeUID:       node.UID,
			LocalDomainID: domainID,
			Health:        health,
		}},
		LocalDomains: []schedulingapi.DeviceDomain{{
			ID:           domainID,
			NodeName:     node.Name,
			NodeUID:      node.UID,
			ResourceName: resourceName,
			DeviceIDs:    []schedulingapi.DeviceID{deviceID},
		}},
	}
	return provider.ProviderNodeUpdate{
		ProviderID:          providerID,
		NodeName:            node.Name,
		NodeUID:             node.UID,
		NodeResourceVersion: node.ResourceVersion,
		SourceGeneration:    generation,
		ObservedAt:          observedAt,
		FreshUntil:          freshUntil,
		Operation:           provider.ReplaceFacts,
		Facts:               &facts,
	}
}

func addTopologyCacheTestFabric(update *provider.ProviderNodeUpdate, fabricValue string) {
	domainID := update.Facts.LocalDomains[0].ID
	update.Facts.Fabrics = []schedulingapi.FabricDomain{{
		ID:               schedulingapi.FabricDomainID{ProviderID: update.ProviderID, Namespace: domainID.Namespace, Value: fabricValue},
		OwnerProviderID:  update.ProviderID,
		OwnerNodeUID:     update.NodeUID,
		SourceGeneration: update.SourceGeneration,
		Members: []schedulingapi.FabricMember{{
			NodeName:         update.NodeName,
			NodeUID:          update.NodeUID,
			SourceGeneration: update.SourceGeneration,
			LocalDomainIDs:   []schedulingapi.DeviceDomainID{domainID},
		}},
	}}
}

func assertTopologySourceErrorReason(t *testing.T, err error, want provider.SourceErrorReason) {
	t.Helper()
	var sourceError *provider.SourceError
	if !errors.As(err, &sourceError) {
		t.Fatalf("error %T = %v, want *provider.SourceError", err, err)
	}
	if sourceError.Reason != want {
		t.Fatalf("SourceError reason = %q, want %q", sourceError.Reason, want)
	}
}
