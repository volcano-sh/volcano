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
	"time"

	corev1 "k8s.io/api/core/v1"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
)

var fabricTestID = schedulingapi.FabricDomainID{ProviderID: topologyCacheTestProviderID, Namespace: topologyCacheTestNamespace, Value: "rack-1/fabric-0"}

func TestSchedulerCacheResolvesFabricInAnySyncOrder(t *testing.T) {
	for _, ownerFirst := range []bool{true, false} {
		name := "member syncs first"
		if ownerFirst {
			name = "owner syncs first"
		}
		t.Run(name, func(t *testing.T) {
			owner := topologyCacheTestNode("node-a", "node-a-uid", "1")
			member := topologyCacheTestNode("node-b", "node-b-uid", "1")
			cache := newTopologyTestSchedulerCache(t, owner, member)
			updates := []provider.ProviderNodeUpdate{
				fabricOwnerTestUpdate(owner, 1, "device-a", "node-a", "node-b"),
				topologyCacheTestUpdate(member, 1, topologyCacheTestObservedAt, "device-b", schedulingapi.DeviceHealthy),
			}
			if !ownerFirst {
				updates[0], updates[1] = updates[1], updates[0]
			}

			applyTopologyCacheTestUpdate(t, cache, updates[0])
			if _, found := cache.DeviceTopologySnapshot().Fabric(fabricTestID); found {
				t.Fatal("Fabric() found the fabric before every member Node synced")
			}
			applyTopologyCacheTestUpdate(t, cache, updates[1])

			fabric, found := cache.DeviceTopologySnapshot().Fabric(fabricTestID)
			if !found {
				t.Fatal("Fabric() = not found, want the fabric after every member Node synced")
			}
			if fabric.OwnerNodeUID != owner.UID || fabric.SourceGeneration != 1 || fabric.Interconnect != "NVLink" || len(fabric.Members) != 2 {
				t.Fatalf("Fabric() = %#v, want owner node-a, generation 1, NVLink, two members", fabric)
			}
			memberB := fabric.Members[1]
			wantDomain := schedulingapi.DeviceDomainID{ProviderID: topologyCacheTestProviderID, Namespace: topologyCacheTestNamespace, NodeUID: member.UID, Value: "domain-a"}
			if memberB.NodeName != "node-b" || memberB.NodeUID != member.UID || memberB.SourceGeneration != 1 || len(memberB.LocalDomainIDs) != 1 || memberB.LocalDomainIDs[0] != wantDomain {
				t.Fatalf("member node-b = %#v, want UID %q, generation 1 and domain %#v", memberB, member.UID, wantDomain)
			}
		})
	}
}

func TestSchedulerCacheKeepsFabricPinnedToResolvedMembers(t *testing.T) {
	owner := topologyCacheTestNode("node-a", "node-a-uid", "1")
	member := topologyCacheTestNode("node-b", "node-b-uid", "1")
	cache := newTopologyTestSchedulerCache(t, owner, member)
	applyTopologyCacheTestUpdate(t, cache, topologyCacheTestUpdate(member, 1, topologyCacheTestObservedAt, "device-b", schedulingapi.DeviceHealthy))
	applyTopologyCacheTestUpdate(t, cache, fabricOwnerTestUpdate(owner, 1, "device-a", "node-a", "node-b"))
	if _, found := cache.DeviceTopologySnapshot().Fabric(fabricTestID); !found {
		t.Fatal("Fabric() = not found, want the resolved fabric")
	}

	// A new member generation still has domain-a, but with another device, so
	// the fabric is not moved to it until the owner declares the fabric again.
	applyTopologyCacheTestUpdate(t, cache, topologyCacheTestUpdate(member, 2, topologyCacheTestObservedAt.Add(time.Minute), "device-c", schedulingapi.DeviceHealthy))
	if _, found := cache.DeviceTopologySnapshot().Fabric(fabricTestID); found {
		t.Fatal("Fabric() found the fabric after its member domain changed devices, want it unavailable")
	}

	applyTopologyCacheTestUpdate(t, cache, fabricOwnerTestUpdate(owner, 2, "device-a", "node-a", "node-b"))
	fabric, found := cache.DeviceTopologySnapshot().Fabric(fabricTestID)
	if !found || fabric.SourceGeneration != 2 || fabric.Members[1].SourceGeneration != 2 {
		t.Fatalf("Fabric() = (%#v, %t), want it resolved again against member generation 2", fabric, found)
	}
}

func TestSchedulerCacheKeepsFabricWhenMemberChangesOnlyHealth(t *testing.T) {
	owner := topologyCacheTestNode("node-a", "node-a-uid", "1")
	member := topologyCacheTestNode("node-b", "node-b-uid", "1")
	cache := newTopologyTestSchedulerCache(t, owner, member)
	applyTopologyCacheTestUpdate(t, cache, topologyCacheTestUpdate(member, 1, topologyCacheTestObservedAt, "device-b", schedulingapi.DeviceHealthy))
	applyTopologyCacheTestUpdate(t, cache, fabricOwnerTestUpdate(owner, 1, "device-a", "node-a", "node-b"))
	resolvedSnapshot := cache.DeviceTopologySnapshot()

	// A health change needs a new member generation. The member is the same
	// Node with the same fabric domain devices, so the fabric follows it.
	applyTopologyCacheTestUpdate(t, cache, topologyCacheTestUpdate(member, 2, topologyCacheTestObservedAt.Add(time.Minute), "device-b", schedulingapi.DeviceUnhealthy))
	fabric, found := cache.DeviceTopologySnapshot().Fabric(fabricTestID)
	if !found || fabric.SourceGeneration != 1 || fabric.Members[1].SourceGeneration != 2 {
		t.Fatalf("Fabric() = (%#v, %t), want owner generation 1 and member generation 2 after a health-only change", fabric, found)
	}
	if earlier, found := resolvedSnapshot.Fabric(fabricTestID); !found || earlier.Members[1].SourceGeneration != 1 {
		t.Fatalf("earlier snapshot Fabric() = (%#v, %t), want it unchanged at member generation 1", earlier, found)
	}

	applyTopologyCacheTestUpdate(t, cache, topologyCacheTestUpdate(member, 3, topologyCacheTestObservedAt.Add(2*time.Minute), "device-b", schedulingapi.DeviceHealthy))
	fabric, found = cache.DeviceTopologySnapshot().Fabric(fabricTestID)
	if !found || fabric.Members[1].SourceGeneration != 3 {
		t.Fatalf("Fabric() = (%#v, %t), want member generation 3 after the device recovered", fabric, found)
	}
}

func TestSchedulerCacheRemovesFabricWhenOwnerClearsFacts(t *testing.T) {
	owner := topologyCacheTestNode("node-a", "node-a-uid", "1")
	member := topologyCacheTestNode("node-b", "node-b-uid", "1")
	cache := newTopologyTestSchedulerCache(t, owner, member)
	applyTopologyCacheTestUpdate(t, cache, topologyCacheTestUpdate(member, 1, topologyCacheTestObservedAt, "device-b", schedulingapi.DeviceHealthy))
	applyTopologyCacheTestUpdate(t, cache, fabricOwnerTestUpdate(owner, 1, "device-a", "node-a", "node-b"))

	applyTopologyCacheTestUpdate(t, cache, provider.ProviderNodeUpdate{
		ProviderID:          topologyCacheTestProviderID,
		NodeName:            owner.Name,
		NodeUID:             owner.UID,
		NodeResourceVersion: owner.ResourceVersion,
		ObservedAt:          topologyCacheTestObservedAt.Add(time.Minute),
		Operation:           provider.ClearFacts,
	})
	if _, found := cache.DeviceTopologySnapshot().Fabric(fabricTestID); found {
		t.Fatal("Fabric() found the fabric after its owner cleared its facts")
	}
	if len(cache.ensureDeviceTopologyState().resolvedFabrics) != 0 {
		t.Fatalf("resolvedFabrics = %#v, want none after the owner cleared its facts", cache.ensureDeviceTopologyState().resolvedFabrics)
	}
}

func TestSchedulerCacheLeavesFabricPendingForMissingMemberDomain(t *testing.T) {
	owner := topologyCacheTestNode("node-a", "node-a-uid", "1")
	member := topologyCacheTestNode("node-b", "node-b-uid", "1")
	cache := newTopologyTestSchedulerCache(t, owner, member)
	applyTopologyCacheTestUpdate(t, cache, topologyCacheTestUpdate(member, 1, topologyCacheTestObservedAt, "device-b", schedulingapi.DeviceHealthy))

	update := fabricOwnerTestUpdate(owner, 1, "device-a", "node-a", "node-b")
	update.Facts.FabricDeclarations[0].Members[1].LocalDomainValues = []string{"domain-x"}
	applyTopologyCacheTestUpdate(t, cache, update)

	if _, found := cache.DeviceTopologySnapshot().Fabric(fabricTestID); found {
		t.Fatal("Fabric() found a fabric whose member does not publish the named local domain")
	}
}

func TestSchedulerCacheRejectsSecondDeclarationOfFabric(t *testing.T) {
	owner := topologyCacheTestNode("node-a", "node-a-uid", "1")
	member := topologyCacheTestNode("node-b", "node-b-uid", "1")
	cache := newTopologyTestSchedulerCache(t, owner, member)
	applyTopologyCacheTestUpdate(t, cache, fabricOwnerTestUpdate(owner, 1, "device-a", "node-a", "node-b"))

	err := cache.ApplyProviderNodeUpdate(fabricOwnerTestUpdate(member, 1, "device-b", "node-b", "node-a"))
	if err == nil || !strings.Contains(err.Error(), "already owned") {
		t.Fatalf("ApplyProviderNodeUpdate() error = %v, want a second declaration rejected", err)
	}
}

// fabricOwnerTestUpdate returns facts for one Node that declare fabricTestID
// over the given member Nodes, each with its local domain domain-a.
func fabricOwnerTestUpdate(node *corev1.Node, generation uint64, deviceValue string, memberNodes ...string) provider.ProviderNodeUpdate {
	update := topologyCacheTestUpdate(node, generation, topologyCacheTestObservedAt.Add(time.Duration(generation)*time.Minute), deviceValue, schedulingapi.DeviceHealthy)
	declaration := schedulingapi.FabricDeclaration{ID: fabricTestID, Interconnect: "NVLink"}
	for _, memberNode := range memberNodes {
		declaration.Members = append(declaration.Members, schedulingapi.FabricMemberDeclaration{NodeName: memberNode, LocalDomainValues: []string{"domain-a"}})
	}
	update.Facts.FabricDeclarations = []schedulingapi.FabricDeclaration{declaration}
	return update
}

func applyTopologyCacheTestUpdate(t *testing.T, cache *SchedulerCache, update provider.ProviderNodeUpdate) {
	t.Helper()
	if err := cache.ApplyProviderNodeUpdate(update); err != nil {
		t.Fatalf("ApplyProviderNodeUpdate(%s generation %d) error = %v", update.NodeName, update.SourceGeneration, err)
	}
}
