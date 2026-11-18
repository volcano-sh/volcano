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
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
	adaptermock "volcano.sh/volcano/pkg/scheduler/topology/adapter/mock"
	"volcano.sh/volcano/pkg/scheduler/topology/provider"
)

func TestSchedulerCacheReservesAndReleasesExactDeviceTopologyPlan(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}, {resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-1"}}, allocationAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 2})

	reservation, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v", err)
	}
	if len(allocationAdapter.validatedFacts) != 1 {
		t.Fatalf("ValidateTopology() calls = %d, want 1", len(allocationAdapter.validatedFacts))
	}
	if len(allocationAdapter.reservedPlans) != 1 {
		t.Fatalf("Reserve() calls = %d, want 1", len(allocationAdapter.reservedPlans))
	}
	if got := allocationAdapter.reservedPlans[0]; len(got.Placements) != 1 || len(got.Placements[0].Assignments) != 1 || len(got.Placements[0].Assignments[0].DeviceIDs) != 2 {
		t.Fatalf("Reserve() plan = %#v, want one exact two-device assignment", got)
	}
	if len(reservation.AdapterReservations) != 1 || reservation.AdapterReservations[0].ResourceName != resourceName || reservation.AdapterReservations[0].Token != "nvidia.com/gpu-token-1" {
		t.Fatalf("ReserveDeviceTopologyPlan() reservation = %#v, want adapter handle", reservation)
	}

	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceHeld)
	if available := cache.DeviceTopologySnapshot().AvailableDeviceIDs(plan.Placements[0].Assignments[0].LocalDomainID); len(available) != 0 {
		t.Fatalf("AvailableDeviceIDs() = %v, want no held devices", available)
	}

	if err := cache.ReleaseDeviceTopologyReservation(context.Background(), reservation); err != nil {
		t.Fatalf("ReleaseDeviceTopologyReservation() error = %v", err)
	}
	if len(allocationAdapter.releasedReservations) != 1 || allocationAdapter.releasedReservations[0].Token != reservation.AdapterReservations[0].Token {
		t.Fatalf("Release() calls = %#v, want returned adapter handle", allocationAdapter.releasedReservations)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceAvailable)
}

func TestSchedulerCachePreparesAndCommitsDeviceTopologyBindHandoffs(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, allocationAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})

	reservation, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v", err)
	}
	handoffs, err := cache.PrepareDeviceTopologyReservationBind(context.Background(), reservation)
	if err != nil {
		t.Fatalf("PrepareDeviceTopologyReservationBind() error = %v", err)
	}
	if len(allocationAdapter.preparedBindPlacements) != 1 {
		t.Fatalf("PrepareBind() calls = %d, want 1", len(allocationAdapter.preparedBindPlacements))
	}
	taskHandoffs := handoffs[plan.Placements[0].TaskID]
	if len(taskHandoffs) != 1 || taskHandoffs[0].ResourceName != resourceName {
		t.Fatalf("prepared handoffs = %#v, want one %q handoff", handoffs, resourceName)
	}
	if taskHandoffs[0].Handoff.ReservationToken != reservation.AdapterReservations[0].Token || taskHandoffs[0].Handoff.PodUID != plan.Placements[0].PodUID || taskHandoffs[0].Handoff.NodeName != node.Name {
		t.Fatalf("prepared handoff = %#v, want reservation, Pod, and Node identity", taskHandoffs[0].Handoff)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceHeld)

	if err := cache.CommitDeviceTopologyReservationBind(context.Background(), reservation); err != nil {
		t.Fatalf("CommitDeviceTopologyReservationBind() error = %v", err)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceBinding)
	if allocations := cache.deviceTopology.podAllocations[plan.Placements[0].PodUID]; len(allocations) != 1 || len(allocations[0].deviceIDs) != 1 {
		t.Fatalf("Pod allocations after commit = %#v, want one record with one device", allocations)
	}

	if err := cache.RollbackDeviceTopologyReservationBind(context.Background(), reservation); err != nil {
		t.Fatalf("RollbackDeviceTopologyReservationBind() error = %v", err)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceAvailable)
	if len(cache.deviceTopology.podAllocations) != 0 {
		t.Fatalf("Pod allocations after rollback = %#v, want none", cache.deviceTopology.podAllocations)
	}
}

func TestSchedulerCacheReservesThroughMockAllocationAdapter(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	mockAdapter, err := adaptermock.New(adapter.DeviceIdentityContract{
		ProviderID:   topologyCacheTestProviderID,
		ResourceName: resourceName,
		Namespace:    topologyCacheTestNamespace,
	}, nil)
	if err != nil {
		t.Fatalf("adaptermock.New() error = %v", err)
	}
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, mockAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})

	reservation, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v", err)
	}
	handoffs, err := cache.PrepareDeviceTopologyReservationBind(context.Background(), reservation)
	if err != nil {
		t.Fatalf("PrepareDeviceTopologyReservationBind() error = %v", err)
	}
	if taskHandoffs := handoffs[plan.Placements[0].TaskID]; len(taskHandoffs) != 1 || taskHandoffs[0].Handoff.ReservationToken != reservation.AdapterReservations[0].Token {
		t.Fatalf("prepared handoffs = %#v, want one mock adapter handoff", handoffs)
	}
	if err := cache.CommitDeviceTopologyReservationBind(context.Background(), reservation); err != nil {
		t.Fatalf("CommitDeviceTopologyReservationBind() error = %v", err)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceBinding)
	if states, err := mockAdapter.Recover(context.Background()); err != nil || len(states) != 1 || states[0].State != adapter.AdapterReservationBinding {
		t.Fatalf("mock Recover() = (%#v, %v), want one Binding reservation", states, err)
	}

	if err := cache.RollbackDeviceTopologyReservationBind(context.Background(), reservation); err != nil {
		t.Fatalf("RollbackDeviceTopologyReservationBind() error = %v", err)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceAvailable)
	if states, err := mockAdapter.Recover(context.Background()); err != nil || len(states) != 0 {
		t.Fatalf("mock Recover() after rollback = (%#v, %v), want no reservations", states, err)
	}
}

func TestSchedulerCacheRejectsMismatchedDeviceTopologyBindHandoff(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	allocationAdapter.handoffForPlacement = func(_ adapter.AdapterReservation, placement schedulingapi.TopologyTaskPlacement) adapter.AllocationHandoff {
		return adapter.AllocationHandoff{
			ReservationToken: "wrong-token",
			PlanDigest:       "wrong-digest",
			PodUID:           placement.PodUID,
			NodeName:         placement.NodeName,
		}
	}
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, allocationAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})

	reservation, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v", err)
	}
	if _, err := cache.PrepareDeviceTopologyReservationBind(context.Background(), reservation); err == nil || !strings.Contains(err.Error(), "different reservation") {
		t.Fatalf("PrepareDeviceTopologyReservationBind() error = %v, want mismatched handoff rejection", err)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceHeld)
	if err := cache.RollbackDeviceTopologyReservationBind(context.Background(), reservation); err != nil {
		t.Fatalf("RollbackDeviceTopologyReservationBind() error = %v", err)
	}
}

func TestSchedulerCacheReservationRollsBackEarlierAdapterFragments(t *testing.T) {
	nvidiaResource := corev1.ResourceName("nvidia.com/gpu")
	amdResource := corev1.ResourceName("amd.com/gpu")
	nvidiaAdapter := newTopologyReservationTestAdapter(nvidiaResource)
	amdAdapter := newTopologyReservationTestAdapter(amdResource)
	nvidiaAdapter.reserveErr = adapter.NewError(adapter.ErrorReservationConflict, errors.New("already reserved"))
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: nvidiaResource, domainValue: "nvidia-domain", deviceValue: "nvidia-0"}, {resourceName: amdResource, domainValue: "amd-domain", deviceValue: "amd-0"}}, nvidiaAdapter, amdAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{nvidiaResource: 1, amdResource: 1})

	_, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "ReservationConflict") {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want reservation conflict", err)
	}
	if len(amdAdapter.reservedPlans) != 1 || len(amdAdapter.releasedReservations) != 1 {
		t.Fatalf("amd adapter calls reserve=%d release=%d, want one each", len(amdAdapter.reservedPlans), len(amdAdapter.releasedReservations))
	}
	if len(nvidiaAdapter.reservedPlans) != 1 {
		t.Fatalf("nvidia adapter Reserve() calls = %d, want 1", len(nvidiaAdapter.reservedPlans))
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceAvailable)
}

func TestSchedulerCacheReservationFailsClosedWhenRollbackCannotRelease(t *testing.T) {
	nvidiaResource := corev1.ResourceName("nvidia.com/gpu")
	amdResource := corev1.ResourceName("amd.com/gpu")
	nvidiaAdapter := newTopologyReservationTestAdapter(nvidiaResource)
	nvidiaAdapter.reserveErr = errors.New("backend reserve result unknown")
	amdAdapter := newTopologyReservationTestAdapter(amdResource)
	amdAdapter.releaseErr = errors.New("backend release failed")
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: nvidiaResource, domainValue: "nvidia-domain", deviceValue: "nvidia-0"}, {resourceName: amdResource, domainValue: "amd-domain", deviceValue: "amd-0"}}, nvidiaAdapter, amdAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{nvidiaResource: 1, amdResource: 1})

	_, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan)
	if err == nil || !strings.Contains(err.Error(), "backend reserve result unknown") {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want backend failure", err)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceUnreconciled)
}

func TestSchedulerCacheReservationRejectsStaleAndUnavailablePlansWithoutAdapterCalls(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, allocationAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})

	futurePlan := plan.Clone()
	futurePlan.Revision++
	if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), futurePlan); err == nil || !strings.Contains(err.Error(), "is not a published revision") {
		t.Fatalf("ReserveDeviceTopologyPlan() future revision error = %v, want revision rejection", err)
	}

	cache.Mutex.Lock()
	state := cache.ensureDeviceTopologyState()
	deviceID := plan.Placements[0].Assignments[0].DeviceIDs[0]
	if err := state.ledger.transition(deviceID, schedulingapi.DeviceHeld); err != nil {
		cache.Mutex.Unlock()
		t.Fatalf("ledger transition error = %v", err)
	}
	state.publish()
	cache.Mutex.Unlock()
	plan.Revision = cache.DeviceTopologySnapshot().Revision()
	if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "non-available") {
		t.Fatalf("ReserveDeviceTopologyPlan() unavailable error = %v, want availability rejection", err)
	}
	if len(allocationAdapter.reservedPlans) != 0 || len(allocationAdapter.validatedFacts) != 0 {
		t.Fatalf("adapter calls validate=%d reserve=%d, want none after local validation failures", len(allocationAdapter.validatedFacts), len(allocationAdapter.reservedPlans))
	}
}

func TestSchedulerCacheReservationRejectsExpiredFactsWithoutAdapterCalls(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, allocationAdapter)
	cache.deviceTopologyNow = func() time.Time {
		return topologyCacheTestObservedAt.Add(2 * time.Hour)
	}
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})

	if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "stale provider facts") {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want stale provider facts", err)
	}
	if len(allocationAdapter.validatedFacts) != 0 || len(allocationAdapter.reservedPlans) != 0 {
		t.Fatalf("adapter calls validate=%d reserve=%d, want none for expired facts", len(allocationAdapter.validatedFacts), len(allocationAdapter.reservedPlans))
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceAvailable)
}

func TestSchedulerCacheReservationValidatesFabricBeforeAdapterCalls(t *testing.T) {
	nvidiaResource := corev1.ResourceName("nvidia.com/gpu")
	amdResource := corev1.ResourceName("amd.com/gpu")

	t.Run("unknown fabric", func(t *testing.T) {
		allocationAdapter := newTopologyReservationTestAdapter(nvidiaResource)
		node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: nvidiaResource, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, allocationAdapter)
		plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{nvidiaResource: 1})
		plan.FabricID = schedulingapi.FabricDomainID{ProviderID: topologyCacheTestProviderID, Namespace: topologyCacheTestNamespace, Value: "missing-fabric"}

		if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "is not current") {
			t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want unknown fabric rejection", err)
		}
		assertTopologyReservationAdapterNotCalled(t, allocationAdapter)
	})

	t.Run("domain outside fabric", func(t *testing.T) {
		nvidiaAdapter := newTopologyReservationTestAdapter(nvidiaResource)
		amdAdapter := newTopologyReservationTestAdapter(amdResource)
		devices := []topologyReservationDeviceSpec{{resourceName: nvidiaResource, domainValue: "nvidia-domain", deviceValue: "nvidia-0"}, {resourceName: amdResource, domainValue: "amd-domain", deviceValue: "amd-0"}}
		node, cache := newTopologyReservationTestCache(t, devices, nvidiaAdapter, amdAdapter)
		fabricID := schedulingapi.FabricDomainID{ProviderID: topologyCacheTestProviderID, Namespace: topologyCacheTestNamespace, Value: "fabric-a"}
		if err := cache.ApplyProviderNodeUpdate(topologyReservationTestFabricUpdate(node, devices, nvidiaResource, fabricID)); err != nil {
			t.Fatalf("ApplyProviderNodeUpdate() error = %v", err)
		}
		plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{nvidiaResource: 1, amdResource: 1})
		plan.FabricID = fabricID

		if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "does not include local domain") {
			t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want fabric member rejection", err)
		}
		assertTopologyReservationAdapterNotCalled(t, nvidiaAdapter)
		assertTopologyReservationAdapterNotCalled(t, amdAdapter)
	})

	t.Run("stale fabric", func(t *testing.T) {
		allocationAdapter := newTopologyReservationTestAdapter(nvidiaResource)
		devices := []topologyReservationDeviceSpec{{resourceName: nvidiaResource, domainValue: "gpu-domain", deviceValue: "gpu-0"}}
		node, cache := newTopologyReservationTestCache(t, devices, allocationAdapter)
		fabricID := schedulingapi.FabricDomainID{ProviderID: topologyCacheTestProviderID, Namespace: topologyCacheTestNamespace, Value: "fabric-a"}
		update := topologyReservationTestFabricUpdate(node, devices, nvidiaResource, fabricID)
		if err := cache.ApplyProviderNodeUpdate(update); err != nil {
			t.Fatalf("ApplyProviderNodeUpdate() error = %v", err)
		}
		cache.deviceTopologyNow = func() time.Time {
			return update.FreshUntil.Add(time.Nanosecond)
		}
		plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{nvidiaResource: 1})
		plan.FabricID = fabricID

		if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "stale owner facts") {
			t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want stale fabric rejection", err)
		}
		assertTopologyReservationAdapterNotCalled(t, allocationAdapter)
	})

	t.Run("stale fabric member", func(t *testing.T) {
		allocationAdapter := newTopologyReservationTestAdapter(nvidiaResource)
		ownerDevices := []topologyReservationDeviceSpec{{resourceName: nvidiaResource, domainValue: "owner-domain", deviceValue: "owner-gpu-0"}}
		ownerNode, cache := newTopologyReservationTestCache(t, ownerDevices, allocationAdapter)
		memberNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "member-node", UID: types.UID("member-node-uid"), ResourceVersion: "1"}}
		if err := cache.AddOrUpdateNode(memberNode); err != nil {
			t.Fatalf("AddOrUpdateNode(member) error = %v", err)
		}
		memberDevices := []topologyReservationDeviceSpec{{resourceName: nvidiaResource, domainValue: "member-domain", deviceValue: "member-gpu-0"}}
		memberUpdate := topologyReservationTestUpdate(memberNode, memberDevices)
		if err := cache.ApplyProviderNodeUpdate(memberUpdate); err != nil {
			t.Fatalf("ApplyProviderNodeUpdate(member) error = %v", err)
		}

		fabricID := schedulingapi.FabricDomainID{ProviderID: topologyCacheTestProviderID, Namespace: topologyCacheTestNamespace, Value: "fabric-a"}
		ownerUpdate := topologyReservationTestUpdate(ownerNode, ownerDevices)
		ownerUpdate.SourceGeneration = 2
		ownerUpdate.ObservedAt = topologyCacheTestObservedAt.Add(time.Minute)
		ownerUpdate.FreshUntil = ownerUpdate.ObservedAt.Add(time.Hour)
		ownerFacts := ownerUpdate.Facts.Clone()
		ownerFacts.SourceGeneration = ownerUpdate.SourceGeneration
		ownerFacts.ObservedAt = ownerUpdate.ObservedAt
		ownerFacts.FreshUntil = ownerUpdate.FreshUntil
		ownerFacts.Fabrics = []schedulingapi.FabricDomain{{
			ID:               fabricID,
			OwnerProviderID:  topologyCacheTestProviderID,
			OwnerNodeUID:     ownerNode.UID,
			SourceGeneration: ownerUpdate.SourceGeneration,
			Members: []schedulingapi.FabricMember{
				{
					NodeName:         ownerNode.Name,
					NodeUID:          ownerNode.UID,
					SourceGeneration: ownerUpdate.SourceGeneration,
					LocalDomainIDs:   []schedulingapi.DeviceDomainID{ownerFacts.LocalDomains[0].ID},
				},
				{
					NodeName:         memberNode.Name,
					NodeUID:          memberNode.UID,
					SourceGeneration: memberUpdate.SourceGeneration,
					LocalDomainIDs:   []schedulingapi.DeviceDomainID{memberUpdate.Facts.LocalDomains[0].ID},
				},
			},
		}}
		ownerUpdate.Facts = &ownerFacts
		if err := cache.ApplyProviderNodeUpdate(ownerUpdate); err != nil {
			t.Fatalf("ApplyProviderNodeUpdate(owner) error = %v", err)
		}
		cache.deviceTopologyNow = func() time.Time {
			return memberUpdate.FreshUntil.Add(time.Nanosecond)
		}
		plan := topologyReservationTestPlan(t, cache, ownerNode, map[corev1.ResourceName]int64{nvidiaResource: 1})
		plan.FabricID = fabricID

		if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "stale member facts") {
			t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want stale fabric member rejection", err)
		}
		assertTopologyReservationAdapterNotCalled(t, allocationAdapter)
	})
}

func TestSchedulerCacheReservationDoesNotHoldDevicesWhenAdapterTopologyValidationFails(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	allocationAdapter.validateErr = errors.New("adapter does not recognize topology")
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, allocationAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})

	if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "adapter does not recognize topology") {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want adapter validation failure", err)
	}
	if len(allocationAdapter.reservedPlans) != 0 || len(allocationAdapter.releasedReservations) != 0 {
		t.Fatalf("adapter calls reserve=%d release=%d, want no backend reservation", len(allocationAdapter.reservedPlans), len(allocationAdapter.releasedReservations))
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceAvailable)
}

func TestSchedulerCacheReservationRollbackUsesUsableCleanupContext(t *testing.T) {
	nvidiaResource := corev1.ResourceName("nvidia.com/gpu")
	amdResource := corev1.ResourceName("amd.com/gpu")
	nvidiaAdapter := newTopologyReservationTestAdapter(nvidiaResource)
	nvidiaAdapter.reserveErr = adapter.NewError(adapter.ErrorReservationConflict, errors.New("already reserved"))
	amdAdapter := newTopologyReservationTestAdapter(amdResource)
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: nvidiaResource, domainValue: "nvidia-domain", deviceValue: "nvidia-0"}, {resourceName: amdResource, domainValue: "amd-domain", deviceValue: "amd-0"}}, nvidiaAdapter, amdAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{nvidiaResource: 1, amdResource: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := cache.ReserveDeviceTopologyPlan(ctx, plan); err == nil || !strings.Contains(err.Error(), "ReservationConflict") {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want reservation conflict", err)
	}
	if len(amdAdapter.releaseContextErrors) != 1 || amdAdapter.releaseContextErrors[0] != nil {
		t.Fatalf("rollback Release() context errors = %v, want one usable context", amdAdapter.releaseContextErrors)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceAvailable)
}

func TestSchedulerCacheReservationTimesOutHungAdapterCalls(t *testing.T) {
	tests := []struct {
		name  string
		block func(*hungTopologyReservationTestAdapter)
		want  schedulingapi.DeviceAvailability
	}{
		{
			name:  "ValidateTopology holds no devices",
			block: func(hungAdapter *hungTopologyReservationTestAdapter) { hungAdapter.hangValidate = true },
			want:  schedulingapi.DeviceAvailable,
		},
		{
			name:  "Reserve leaves an unknown backend result unreconciled",
			block: func(hungAdapter *hungTopologyReservationTestAdapter) { hungAdapter.hangReserve = true },
			want:  schedulingapi.DeviceUnreconciled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resourceName := corev1.ResourceName("nvidia.com/gpu")
			hungAdapter := &hungTopologyReservationTestAdapter{topologyReservationTestAdapter: newTopologyReservationTestAdapter(resourceName)}
			tt.block(hungAdapter)
			node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, hungAdapter)
			cache.deviceTopologyAdapterCallTimeout = 20 * time.Millisecond
			plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})

			if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want deadline exceeded", err)
			}
			assertTopologyReservationAvailability(t, cache, plan, tt.want)
		})
	}
}

func TestDeviceTopologyAdapterCallContextDefaultsTimeout(t *testing.T) {
	sc := &SchedulerCache{}
	callContext, cancel := sc.newDeviceTopologyAdapterCallContext(context.Background())
	defer cancel()

	deadline, hasDeadline := callContext.Deadline()
	if !hasDeadline || time.Until(deadline) > defaultDeviceTopologyAdapterCallTimeout {
		t.Fatalf("adapter call deadline = (%v, %v), want at most %v from now", deadline, hasDeadline, defaultDeviceTopologyAdapterCallTimeout)
	}
}

func TestSchedulerCacheReservationHonorsCallerCancellationDuringAdapterCall(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	hungAdapter := &hungTopologyReservationTestAdapter{topologyReservationTestAdapter: newTopologyReservationTestAdapter(resourceName), hangReserve: true}
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, hungAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := cache.ReserveDeviceTopologyPlan(ctx, plan); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want caller cancellation", err)
	}
}

func TestSchedulerCachePrepareBindTimesOutHungAdapterCall(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	hungAdapter := &hungTopologyReservationTestAdapter{topologyReservationTestAdapter: newTopologyReservationTestAdapter(resourceName)}
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, hungAdapter)
	cache.deviceTopologyAdapterCallTimeout = 20 * time.Millisecond
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})
	reservation, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v", err)
	}
	hungAdapter.hangPrepareBind = true

	if _, err := cache.PrepareDeviceTopologyReservationBind(context.Background(), reservation); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("PrepareDeviceTopologyReservationBind() error = %v, want deadline exceeded", err)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceHeld)

	if err := cache.RollbackDeviceTopologyReservationBind(context.Background(), reservation); err != nil {
		t.Fatalf("RollbackDeviceTopologyReservationBind() error = %v", err)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceAvailable)
}

func TestSchedulerCacheReleaseUsesBoundedCleanupContext(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, allocationAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})
	reservation, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := cache.ReleaseDeviceTopologyReservation(ctx, reservation); err != nil {
		t.Fatalf("ReleaseDeviceTopologyReservation() error = %v", err)
	}
	if len(allocationAdapter.releaseContextErrors) != 1 || allocationAdapter.releaseContextErrors[0] != nil {
		t.Fatalf("Release() context errors = %v, want one usable context", allocationAdapter.releaseContextErrors)
	}
	if len(allocationAdapter.releaseContextDeadlines) != 1 || !allocationAdapter.releaseContextDeadlines[0] {
		t.Fatalf("Release() context deadlines = %v, want one bounded context", allocationAdapter.releaseContextDeadlines)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceAvailable)
}

func TestSchedulerCacheReleaseMarksOnlyFailedFragmentUnreconciled(t *testing.T) {
	nvidiaResource := corev1.ResourceName("nvidia.com/gpu")
	amdResource := corev1.ResourceName("amd.com/gpu")
	nvidiaAdapter := newTopologyReservationTestAdapter(nvidiaResource)
	amdAdapter := newTopologyReservationTestAdapter(amdResource)
	amdAdapter.releaseErr = errors.New("amd release failed")
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: nvidiaResource, domainValue: "nvidia-domain", deviceValue: "nvidia-0"}, {resourceName: amdResource, domainValue: "amd-domain", deviceValue: "amd-0"}}, nvidiaAdapter, amdAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{nvidiaResource: 1, amdResource: 1})

	reservation, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v", err)
	}
	if err := cache.ReleaseDeviceTopologyReservation(context.Background(), reservation); err == nil || !strings.Contains(err.Error(), "amd release failed") {
		t.Fatalf("ReleaseDeviceTopologyReservation() error = %v, want amd release failure", err)
	}

	snapshot := cache.DeviceTopologySnapshot()
	for _, assignment := range plan.Placements[0].Assignments {
		availability, exists := snapshot.DeviceAvailability(assignment.DeviceIDs[0])
		if !exists {
			t.Fatalf("DeviceAvailability(%q) not found", assignment.DeviceIDs[0].String())
		}
		want := schedulingapi.DeviceAvailable
		if assignment.ResourceName == amdResource {
			want = schedulingapi.DeviceUnreconciled
		}
		if availability != want {
			t.Fatalf("DeviceAvailability(%q) = %q, want %q", assignment.DeviceIDs[0].String(), availability, want)
		}
	}
}

func TestSchedulerCacheReleaseRejectsStaleReservationHandle(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, allocationAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})

	firstReservation, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("first ReserveDeviceTopologyPlan() error = %v", err)
	}
	if err := cache.ReleaseDeviceTopologyReservation(context.Background(), firstReservation); err != nil {
		t.Fatalf("first ReleaseDeviceTopologyReservation() error = %v", err)
	}

	secondPlan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})
	secondReservation, err := cache.ReserveDeviceTopologyPlan(context.Background(), secondPlan)
	if err != nil {
		t.Fatalf("second ReserveDeviceTopologyPlan() error = %v", err)
	}
	if err := cache.ReleaseDeviceTopologyReservation(context.Background(), firstReservation); err == nil || !strings.Contains(err.Error(), "no longer owns") {
		t.Fatalf("stale ReleaseDeviceTopologyReservation() error = %v, want ownership rejection", err)
	}
	if len(allocationAdapter.releasedReservations) != 1 {
		t.Fatalf("Release() calls = %d, want stale handle rejected before backend release", len(allocationAdapter.releasedReservations))
	}
	assertTopologyReservationAvailability(t, cache, secondPlan, schedulingapi.DeviceHeld)

	if err := cache.ReleaseDeviceTopologyReservation(context.Background(), secondReservation); err != nil {
		t.Fatalf("second ReleaseDeviceTopologyReservation() error = %v", err)
	}
}

func TestSchedulerCacheReservationAcceptsPlanFromOlderRevision(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, allocationAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})

	// An unrelated change, such as another Pod releasing its devices,
	// publishes a newer revision. The planned device is still valid.
	cache.Mutex.Lock()
	cache.ensureDeviceTopologyState().publish()
	cache.Mutex.Unlock()
	if current := cache.DeviceTopologySnapshot().Revision(); current <= plan.Revision {
		t.Fatalf("current revision = %d, want newer than plan revision %d", current, plan.Revision)
	}

	if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan); err != nil {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want a plan from an older revision accepted", err)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceHeld)
}

func TestSchedulerCacheReservationIgnoresUnrelatedChangeDuringBackendReservation(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, allocationAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})
	allocationAdapter.reserveHook = func() {
		cache.Mutex.Lock()
		cache.ensureDeviceTopologyState().publish()
		cache.Mutex.Unlock()
	}

	if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan); err != nil {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want an unrelated change ignored", err)
	}
	if len(allocationAdapter.releasedReservations) != 0 {
		t.Fatalf("Release() calls = %d, want no rollback", len(allocationAdapter.releasedReservations))
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceHeld)
}

func TestSchedulerCacheReservationRollsBackWhenReservedDeviceChangesDuringBackendReservation(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, []topologyReservationDeviceSpec{{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"}}, allocationAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})
	deviceID := plan.Placements[0].Assignments[0].DeviceIDs[0]
	allocationAdapter.reserveHook = func() {
		cache.Mutex.Lock()
		defer cache.Mutex.Unlock()
		state := cache.ensureDeviceTopologyState()
		key := schedulingapi.ProviderNodeKey{ProviderID: deviceID.ProviderID, NodeUID: node.UID}
		facts := state.facts[key].Clone()
		for index := range facts.Devices {
			if facts.Devices[index].ID == deviceID {
				facts.Devices[index].Health = schedulingapi.DeviceUnhealthy
			}
		}
		state.facts[key] = facts
		state.publish()
	}

	if _, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "changed while reserving exact devices") {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v, want rejection of the changed device", err)
	}
	if len(allocationAdapter.releasedReservations) != 1 {
		t.Fatalf("Release() calls = %d, want rollback after the reserved device changed", len(allocationAdapter.releasedReservations))
	}
}

type topologyReservationDeviceSpec struct {
	resourceName corev1.ResourceName
	domainValue  string
	deviceValue  string
}

type topologyReservationTestAdapter struct {
	contract               adapter.DeviceIdentityContract
	validatedFacts         []schedulingapi.NodeTopologyFacts
	reservedPlans          []schedulingapi.TopologyPlacementPlan
	preparedBindPlacements []schedulingapi.TopologyTaskPlacement
	releasedReservations   []adapter.AdapterReservation
	releaseContextErrors   []error
	// releaseContextDeadlines records whether each Release context had a deadline.
	releaseContextDeadlines []bool
	reserveHook             func()
	handoffForPlacement     func(adapter.AdapterReservation, schedulingapi.TopologyTaskPlacement) adapter.AllocationHandoff
	validateErr             error
	reserveErr              error
	prepareBindErr          error
	releaseErr              error
	releasedPods            []topologyTestPodCall
	releasePodErr           error
	confirmedPods           []topologyTestPodCall
	confirmHook             func()
	confirmErr              error
	recoverCalls            int
	recoverStates           []adapter.AdapterReservationState
	recoverErr              error
	// observationFor, when set, replaces the Allocated observation Confirm
	// returns by default.
	observationFor func(adapter.AdapterReservation, types.UID) adapter.AllocationObservation
}

// topologyTestPodCall records one ReleasePod or Confirm call.
type topologyTestPodCall struct {
	reservation adapter.AdapterReservation
	podUID      types.UID
}

func newTopologyReservationTestAdapter(resourceName corev1.ResourceName) *topologyReservationTestAdapter {
	return &topologyReservationTestAdapter{contract: adapter.DeviceIdentityContract{
		ProviderID:   topologyCacheTestProviderID,
		ResourceName: resourceName,
		Namespace:    topologyCacheTestNamespace,
	}}
}

func (testAdapter *topologyReservationTestAdapter) Capabilities() adapter.AdapterCapabilities {
	return adapter.AdapterCapabilities{
		ExactDeviceIDs:        true,
		AtomicReservation:     true,
		ImmutableBindHandoff:  true,
		AllocationObservation: true,
		ReleaseObservation:    true,
		Recovery:              true,
	}
}

func (testAdapter *topologyReservationTestAdapter) IdentityContract() adapter.DeviceIdentityContract {
	return testAdapter.contract
}

func (testAdapter *topologyReservationTestAdapter) ValidateTopology(_ context.Context, facts schedulingapi.NodeTopologyFacts) error {
	testAdapter.validatedFacts = append(testAdapter.validatedFacts, facts.Clone())
	return testAdapter.validateErr
}

func (testAdapter *topologyReservationTestAdapter) Reserve(_ context.Context, plan schedulingapi.TopologyPlacementPlan) (adapter.AdapterReservation, error) {
	testAdapter.reservedPlans = append(testAdapter.reservedPlans, plan.Clone())
	if testAdapter.reserveHook != nil {
		testAdapter.reserveHook()
	}
	if testAdapter.reserveErr != nil {
		return adapter.AdapterReservation{}, testAdapter.reserveErr
	}
	return adapter.AdapterReservation{Token: fmt.Sprintf("%s-token-%d", testAdapter.contract.ResourceName, len(testAdapter.reservedPlans)), PlanDigest: fmt.Sprintf("%s-digest-%d", testAdapter.contract.ResourceName, len(testAdapter.reservedPlans))}, nil
}

func (testAdapter *topologyReservationTestAdapter) PrepareBind(_ context.Context, reservation adapter.AdapterReservation, placement schedulingapi.TopologyTaskPlacement) (adapter.AllocationHandoff, error) {
	testAdapter.preparedBindPlacements = append(testAdapter.preparedBindPlacements, placement.Clone())
	if testAdapter.prepareBindErr != nil {
		return adapter.AllocationHandoff{}, testAdapter.prepareBindErr
	}
	if testAdapter.handoffForPlacement != nil {
		return testAdapter.handoffForPlacement(reservation, placement), nil
	}
	return adapter.AllocationHandoff{
		ReservationToken: reservation.Token,
		PlanDigest:       reservation.PlanDigest,
		PodUID:           placement.PodUID,
		NodeName:         placement.NodeName,
	}, nil
}

func (testAdapter *topologyReservationTestAdapter) Confirm(_ context.Context, reservation adapter.AdapterReservation, podUID types.UID) (adapter.AllocationObservation, error) {
	testAdapter.confirmedPods = append(testAdapter.confirmedPods, topologyTestPodCall{reservation: reservation, podUID: podUID})
	if testAdapter.confirmHook != nil {
		testAdapter.confirmHook()
	}
	if testAdapter.confirmErr != nil {
		return adapter.AllocationObservation{}, testAdapter.confirmErr
	}
	if testAdapter.observationFor != nil {
		return testAdapter.observationFor(reservation, podUID), nil
	}
	return adapter.AllocationObservation{
		ReservationToken: reservation.Token,
		PlanDigest:       reservation.PlanDigest,
		PodUID:           podUID,
		State:            adapter.AllocationObservationAllocated,
	}, nil
}

func (testAdapter *topologyReservationTestAdapter) ReleasePod(_ context.Context, reservation adapter.AdapterReservation, podUID types.UID) error {
	testAdapter.releasedPods = append(testAdapter.releasedPods, topologyTestPodCall{reservation: reservation, podUID: podUID})
	return testAdapter.releasePodErr
}

func (testAdapter *topologyReservationTestAdapter) Release(ctx context.Context, reservation adapter.AdapterReservation) error {
	testAdapter.releasedReservations = append(testAdapter.releasedReservations, reservation)
	testAdapter.releaseContextErrors = append(testAdapter.releaseContextErrors, ctx.Err())
	_, hasDeadline := ctx.Deadline()
	testAdapter.releaseContextDeadlines = append(testAdapter.releaseContextDeadlines, hasDeadline)
	return testAdapter.releaseErr
}

func (testAdapter *topologyReservationTestAdapter) Recover(context.Context) ([]adapter.AdapterReservationState, error) {
	testAdapter.recoverCalls++
	if testAdapter.recoverErr != nil {
		return nil, testAdapter.recoverErr
	}
	return testAdapter.recoverStates, nil
}

// hungTopologyReservationTestAdapter behaves like a hung backend that honors
// its call context in the selected methods.
type hungTopologyReservationTestAdapter struct {
	*topologyReservationTestAdapter
	hangValidate    bool
	hangReserve     bool
	hangPrepareBind bool
}

func (testAdapter *hungTopologyReservationTestAdapter) ValidateTopology(ctx context.Context, facts schedulingapi.NodeTopologyFacts) error {
	if testAdapter.hangValidate {
		return waitForAdapterCallContext(ctx)
	}
	return testAdapter.topologyReservationTestAdapter.ValidateTopology(ctx, facts)
}

func (testAdapter *hungTopologyReservationTestAdapter) Reserve(ctx context.Context, plan schedulingapi.TopologyPlacementPlan) (adapter.AdapterReservation, error) {
	if testAdapter.hangReserve {
		return adapter.AdapterReservation{}, waitForAdapterCallContext(ctx)
	}
	return testAdapter.topologyReservationTestAdapter.Reserve(ctx, plan)
}

func (testAdapter *hungTopologyReservationTestAdapter) PrepareBind(ctx context.Context, reservation adapter.AdapterReservation, placement schedulingapi.TopologyTaskPlacement) (adapter.AllocationHandoff, error) {
	if testAdapter.hangPrepareBind {
		return adapter.AllocationHandoff{}, waitForAdapterCallContext(ctx)
	}
	return testAdapter.topologyReservationTestAdapter.PrepareBind(ctx, reservation, placement)
}

// waitForAdapterCallContext returns the context error, or a plain error if the
// context has no effective deadline, so a missing timeout fails instead of
// hanging the test binary.
func waitForAdapterCallContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
		return errors.New("adapter call context did not end")
	}
}

func newTopologyReservationTestCache(t *testing.T, devices []topologyReservationDeviceSpec, adapters ...adapter.AllocationAdapter) (*corev1.Node, *SchedulerCache) {
	t.Helper()
	resourceNames := make([]corev1.ResourceName, 0, len(devices))
	seenResources := make(map[corev1.ResourceName]struct{}, len(devices))
	for _, device := range devices {
		if _, exists := seenResources[device.resourceName]; !exists {
			seenResources[device.resourceName] = struct{}{}
			resourceNames = append(resourceNames, device.resourceName)
		}
	}
	// These tests control freshness only through the source FreshUntil, so
	// the provider max age must not end it first.
	providerRegistry, err := provider.NewRegistry([]provider.Registration{{
		ProviderID:        topologyCacheTestProviderID,
		IdentityNamespace: topologyCacheTestNamespace,
		ResourceNames:     resourceNames,
		MaxAge:            24 * time.Hour,
	}})
	if err != nil {
		t.Fatalf("provider.NewRegistry() error = %v", err)
	}
	adapterRegistry, err := adapter.NewRegistry(adapters)
	if err != nil {
		t.Fatalf("adapter.NewRegistry() error = %v", err)
	}

	cache := NewDefaultMockSchedulerCache("device-topology-reservation-test")
	t.Cleanup(func() {
		ShutdownMockSchedulerCache(cache)
	})
	cache.deviceTopologyNow = func() time.Time {
		return topologyCacheTestObservedAt
	}
	WithDeviceTopologyProviderRegistry(providerRegistry)(cache)
	WithDeviceTopologyAdapterRegistry(adapterRegistry)(cache)
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: types.UID("node-a-uid"), ResourceVersion: "1"}}
	if err := cache.AddOrUpdateNode(node); err != nil {
		t.Fatalf("AddOrUpdateNode() error = %v", err)
	}
	if err := cache.ApplyProviderNodeUpdate(topologyReservationTestUpdate(node, devices)); err != nil {
		t.Fatalf("ApplyProviderNodeUpdate() error = %v", err)
	}
	return node, cache
}

func topologyReservationTestFabricUpdate(node *corev1.Node, devices []topologyReservationDeviceSpec, memberResource corev1.ResourceName, fabricID schedulingapi.FabricDomainID) provider.ProviderNodeUpdate {
	update := topologyReservationTestUpdate(node, devices)
	update.SourceGeneration = 2
	update.ObservedAt = topologyCacheTestObservedAt.Add(time.Minute)
	update.FreshUntil = update.ObservedAt.Add(time.Hour)
	facts := update.Facts.Clone()
	facts.SourceGeneration = update.SourceGeneration
	facts.ObservedAt = update.ObservedAt
	facts.FreshUntil = update.FreshUntil

	memberDomainIDs := make([]schedulingapi.DeviceDomainID, 0)
	for _, domain := range facts.LocalDomains {
		if domain.ResourceName == memberResource {
			memberDomainIDs = append(memberDomainIDs, domain.ID)
		}
	}
	facts.Fabrics = []schedulingapi.FabricDomain{{
		ID:               fabricID,
		OwnerProviderID:  topologyCacheTestProviderID,
		OwnerNodeUID:     node.UID,
		SourceGeneration: update.SourceGeneration,
		Members: []schedulingapi.FabricMember{{
			NodeName:         node.Name,
			NodeUID:          node.UID,
			SourceGeneration: update.SourceGeneration,
			LocalDomainIDs:   memberDomainIDs,
		}},
	}}
	update.Facts = &facts
	return update
}

func assertTopologyReservationAdapterNotCalled(t *testing.T, allocationAdapter *topologyReservationTestAdapter) {
	t.Helper()
	if len(allocationAdapter.validatedFacts) != 0 || len(allocationAdapter.reservedPlans) != 0 || len(allocationAdapter.releasedReservations) != 0 {
		t.Fatalf("adapter calls validate=%d reserve=%d release=%d, want none", len(allocationAdapter.validatedFacts), len(allocationAdapter.reservedPlans), len(allocationAdapter.releasedReservations))
	}
}

func topologyReservationTestUpdate(node *corev1.Node, devices []topologyReservationDeviceSpec) provider.ProviderNodeUpdate {
	observedAt := topologyCacheTestObservedAt
	facts := schedulingapi.NodeTopologyFacts{
		ProviderID:          topologyCacheTestProviderID,
		NodeName:            node.Name,
		NodeUID:             node.UID,
		NodeResourceVersion: node.ResourceVersion,
		SourceGeneration:    1,
		ObservedAt:          observedAt,
		FreshUntil:          observedAt.Add(time.Hour),
	}
	domains := make(map[corev1.ResourceName]*schedulingapi.DeviceDomain)
	for _, specification := range devices {
		domain, exists := domains[specification.resourceName]
		if !exists {
			domain = &schedulingapi.DeviceDomain{
				ID: schedulingapi.DeviceDomainID{
					ProviderID: topologyCacheTestProviderID,
					Namespace:  topologyCacheTestNamespace,
					NodeUID:    node.UID,
					Value:      specification.domainValue,
				},
				NodeName:     node.Name,
				NodeUID:      node.UID,
				ResourceName: specification.resourceName,
			}
			domains[specification.resourceName] = domain
		}
		deviceID := schedulingapi.DeviceID{ProviderID: topologyCacheTestProviderID, Namespace: topologyCacheTestNamespace, Value: specification.deviceValue}
		domain.DeviceIDs = append(domain.DeviceIDs, deviceID)
		facts.Devices = append(facts.Devices, schedulingapi.TopologyDevice{
			ID:            deviceID,
			ResourceName:  specification.resourceName,
			NodeName:      node.Name,
			NodeUID:       node.UID,
			LocalDomainID: domain.ID,
			Health:        schedulingapi.DeviceHealthy,
		})
	}
	for _, resourceName := range resourceNamesForTopologyReservation(devices) {
		facts.LocalDomains = append(facts.LocalDomains, *domains[resourceName])
	}
	return provider.ProviderNodeUpdate{
		ProviderID:          topologyCacheTestProviderID,
		NodeName:            node.Name,
		NodeUID:             node.UID,
		NodeResourceVersion: node.ResourceVersion,
		SourceGeneration:    facts.SourceGeneration,
		ObservedAt:          facts.ObservedAt,
		FreshUntil:          facts.FreshUntil,
		Operation:           provider.ReplaceFacts,
		Facts:               &facts,
	}
}

func resourceNamesForTopologyReservation(devices []topologyReservationDeviceSpec) []corev1.ResourceName {
	seen := make(map[corev1.ResourceName]struct{}, len(devices))
	result := make([]corev1.ResourceName, 0, len(devices))
	for _, device := range devices {
		if _, exists := seen[device.resourceName]; exists {
			continue
		}
		seen[device.resourceName] = struct{}{}
		result = append(result, device.resourceName)
	}
	return result
}

func topologyReservationTestPlan(t *testing.T, cache *SchedulerCache, node *corev1.Node, quantities map[corev1.ResourceName]int64) schedulingapi.TopologyPlacementPlan {
	t.Helper()
	snapshot := cache.DeviceTopologySnapshot()
	assignments := make([]schedulingapi.TopologyContainerAssignment, 0, len(quantities))
	for _, resourceName := range resourceNamesForTopologyReservationFromQuantities(quantities) {
		domains := snapshot.LocalDomains(node.UID, resourceName)
		if len(domains) != 1 {
			t.Fatalf("LocalDomains(%q) = %#v, want one domain", resourceName, domains)
		}
		availableDeviceIDs := snapshot.AvailableDeviceIDs(domains[0].ID)
		quantity := quantities[resourceName]
		if int64(len(availableDeviceIDs)) < quantity {
			t.Fatalf("AvailableDeviceIDs(%q) = %v, want at least %d", resourceName, availableDeviceIDs, quantity)
		}
		assignments = append(assignments, schedulingapi.TopologyContainerAssignment{
			ContainerName: "trainer",
			ResourceName:  resourceName,
			LocalDomainID: domains[0].ID,
			DeviceIDs:     append([]schedulingapi.DeviceID(nil), availableDeviceIDs[:quantity]...),
		})
	}
	return schedulingapi.TopologyPlacementPlan{
		JobID:            "job-a",
		SchedulingUnitID: "job-a",
		Revision:         snapshot.Revision(),
		Placements: []schedulingapi.TopologyTaskPlacement{{
			TaskID:      "pod-a",
			PodUID:      "pod-a",
			NodeName:    node.Name,
			NodeUID:     node.UID,
			Assignments: assignments,
		}},
	}
}

func resourceNamesForTopologyReservationFromQuantities(quantities map[corev1.ResourceName]int64) []corev1.ResourceName {
	result := make([]corev1.ResourceName, 0, len(quantities))
	for resourceName := range quantities {
		result = append(result, resourceName)
	}
	for left := 0; left < len(result); left++ {
		for right := left + 1; right < len(result); right++ {
			if result[right] < result[left] {
				result[left], result[right] = result[right], result[left]
			}
		}
	}
	return result
}

func assertTopologyReservationAvailability(t *testing.T, cache *SchedulerCache, plan schedulingapi.TopologyPlacementPlan, want schedulingapi.DeviceAvailability) {
	t.Helper()
	snapshot := cache.DeviceTopologySnapshot()
	for _, placement := range plan.Placements {
		for _, assignment := range placement.Assignments {
			for _, deviceID := range assignment.DeviceIDs {
				got, exists := snapshot.DeviceAvailability(deviceID)
				if !exists || got != want {
					t.Fatalf("DeviceAvailability(%q) = (%q, %v), want (%q, true)", deviceID.String(), got, exists, want)
				}
			}
		}
	}
}
