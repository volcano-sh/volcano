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
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
	adaptermock "volcano.sh/volcano/pkg/scheduler/topology/adapter/mock"
)

func TestSchedulerCacheBlocksExactPlacementUntilAllocationsAreRecovered(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	allocationAdapter.recoverErr = adapter.NewError(adapter.ErrorBackendFailure, errors.New("backend unavailable"))

	schedulerCache.beginDeviceTopologyAllocationRecovery()
	if schedulerCache.DeviceTopologySnapshot().AllocationStateRecovered(resourceName) {
		t.Fatal("AllocationStateRecovered() = true at startup, want false until the adapter reports its reservations")
	}
	plan := topologyReservationTestPlan(t, schedulerCache, node, map[corev1.ResourceName]int64{resourceName: 1})
	if _, err := schedulerCache.ReserveDeviceTopologyPlan(context.Background(), plan); err == nil || !strings.Contains(err.Error(), "not recovered") {
		t.Fatalf("ReserveDeviceTopologyPlan() before recovery error = %v, want a recovery rejection", err)
	}

	if pending := schedulerCache.recoverDeviceTopologyAllocations(context.Background()); !pending {
		t.Fatal("recoverDeviceTopologyAllocations() = done, want pending after a failed Recover")
	}
	if schedulerCache.DeviceTopologySnapshot().AllocationStateRecovered(resourceName) {
		t.Fatal("AllocationStateRecovered() = true after a failed Recover, want false")
	}

	allocationAdapter.recoverErr = nil
	if pending := schedulerCache.recoverDeviceTopologyAllocations(context.Background()); pending {
		t.Fatal("recoverDeviceTopologyAllocations() = pending, want done after Recover succeeds")
	}
	if !schedulerCache.DeviceTopologySnapshot().AllocationStateRecovered(resourceName) {
		t.Fatal("AllocationStateRecovered() = false after Recover succeeded, want true")
	}
	if allocationAdapter.recoverCalls != 2 {
		t.Fatalf("Recover() calls = %d, want 2", allocationAdapter.recoverCalls)
	}
	commitTopologyReservationTestPlan(t, schedulerCache, topologyReservationTestPlan(t, schedulerCache, node, map[corev1.ResourceName]int64{resourceName: 1}))
}

func TestSchedulerCacheSkipsRecoveryForAdapterWithoutRecovery(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := &noRecoveryTopologyTestAdapter{topologyReservationTestAdapter: newTopologyReservationTestAdapter(resourceName)}
	_, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)

	schedulerCache.beginDeviceTopologyAllocationRecovery()
	if !schedulerCache.DeviceTopologySnapshot().AllocationStateRecovered(resourceName) {
		t.Fatal("AllocationStateRecovered() = false, want true for an adapter that never serves hard placement")
	}
	if pending := schedulerCache.recoverDeviceTopologyAllocations(context.Background()); pending || allocationAdapter.recoverCalls != 0 {
		t.Fatalf("recoverDeviceTopologyAllocations() = (pending %v, %d Recover calls), want (false, 0)", pending, allocationAdapter.recoverCalls)
	}
}

func TestSchedulerCacheRestoresRecoveredAllocations(t *testing.T) {
	tests := []struct {
		name  string
		state adapter.AdapterReservationLifecycleState
		want  schedulingapi.DeviceAvailability
	}{
		{name: "held reservation", state: adapter.AdapterReservationHeld, want: schedulingapi.DeviceHeld},
		{name: "binding reservation", state: adapter.AdapterReservationBinding, want: schedulingapi.DeviceBinding},
		{name: "allocated reservation", state: adapter.AdapterReservationAllocated, want: schedulingapi.DeviceAllocated},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resourceName := corev1.ResourceName("nvidia.com/gpu")
			allocationAdapter := newTopologyReservationTestAdapter(resourceName)
			node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
			plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
			handle := adapter.AdapterReservation{Token: "recovered-token", PlanDigest: "recovered-digest"}
			allocationAdapter.recoverStates = []adapter.AdapterReservationState{{Reservation: handle, Plan: plan, State: test.state}}

			recoverTopologyTestAllocations(t, schedulerCache)

			assertTopologyReservationAvailability(t, schedulerCache, plan, test.want)
			owner := deviceTopologyReservationOwner{resourceName: resourceName, token: handle.Token, planDigest: handle.PlanDigest}
			for _, placement := range plan.Placements {
				deviceID := placement.Assignments[0].DeviceIDs[0]
				if got := schedulerCache.deviceTopology.reservationOwners[deviceID]; got != owner {
					t.Fatalf("reservation owner of %q = %#v, want %#v", deviceID.String(), got, owner)
				}
			}
			// A recovered device cannot be reserved again, but a free one can.
			if _, err := schedulerCache.ReserveDeviceTopologyPlan(context.Background(), plan); err == nil {
				t.Fatal("ReserveDeviceTopologyPlan() of recovered devices succeeded, want a rejection")
			}
			free := topologyReservationTestPlan(t, schedulerCache, node, map[corev1.ResourceName]int64{resourceName: 1})
			free.Placements[0].TaskID, free.Placements[0].PodUID = "pod-c", "pod-c"
			commitTopologyReservationTestPlan(t, schedulerCache, free)

			if test.state == adapter.AdapterReservationHeld {
				if _, holds := schedulerCache.deviceTopology.podAllocations["pod-a"]; holds {
					t.Fatal("pod-a has a record from a Held reservation, want none because no Pod was bound")
				}
				return
			}
			want := []topologyTestPodCall{{reservation: handle, podUID: "pod-a"}}
			for _, podUID := range []types.UID{"pod-a", "pod-b"} {
				if allocations := schedulerCache.deviceTopology.podAllocations[podUID]; len(allocations) != 1 || allocations[0].owner != owner || len(allocations[0].deviceIDs) != 1 {
					t.Fatalf("record of %s = %#v, want one device of the recovered reservation", podUID, allocations)
				}
			}
			// The restored record lets the Pod's end release its devices.
			schedulerCache.DeletePod(topologyReleaseTestPod("pod-a", corev1.PodRunning))
			drainTopologyTestPodReleases(schedulerCache)
			if !reflect.DeepEqual(allocationAdapter.releasedPods, want) {
				t.Fatalf("ReleasePod() calls = %#v, want %#v", allocationAdapter.releasedPods, want)
			}
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 1), test.want)
		})
	}
}

func TestSchedulerCacheRecoveryMarksUntrustedReservationsUnreconciled(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	other := topologyTestPlacementOnly(plan, 0)
	unknown := topologyReservationTestPlan(t, schedulerCache, node, map[corev1.ResourceName]int64{resourceName: 3})
	unknown.Placements[0].Assignments[0].DeviceIDs = unknown.Placements[0].Assignments[0].DeviceIDs[2:]
	allocationAdapter.recoverStates = []adapter.AdapterReservationState{
		// Both reservations claim pod-a's device, so neither can be trusted.
		{Reservation: adapter.AdapterReservation{Token: "token-1", PlanDigest: "digest-1"}, Plan: plan, State: adapter.AdapterReservationAllocated},
		{Reservation: adapter.AdapterReservation{Token: "token-2", PlanDigest: "digest-2"}, Plan: other, State: adapter.AdapterReservationAllocated},
		{Reservation: adapter.AdapterReservation{Token: "token-3", PlanDigest: "digest-3"}, Plan: unknown, State: "Lost"},
	}

	recoverTopologyTestAllocations(t, schedulerCache)

	assertTopologyReservationAvailability(t, schedulerCache, plan, schedulingapi.DeviceUnreconciled)
	assertTopologyReservationAvailability(t, schedulerCache, unknown, schedulingapi.DeviceUnreconciled)
	if len(schedulerCache.deviceTopology.reservationOwners) != 0 || len(schedulerCache.deviceTopology.podAllocations) != 0 {
		t.Fatalf("records after recovering untrusted reservations = (%#v, %#v), want none", schedulerCache.deviceTopology.reservationOwners, schedulerCache.deviceTopology.podAllocations)
	}
}

func TestSchedulerCacheRecoveryKeepsDeviceWithoutFacts(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := topologyReservationTestPlan(t, schedulerCache, node, map[corev1.ResourceName]int64{resourceName: 1})
	// The device's Node has not reported its facts yet.
	plan.Placements[0].Assignments[0].DeviceIDs[0].Value = "gpu-without-facts"
	allocationAdapter.recoverStates = []adapter.AdapterReservationState{{Reservation: adapter.AdapterReservation{Token: "token", PlanDigest: "digest"}, Plan: plan, State: adapter.AdapterReservationAllocated}}

	recoverTopologyTestAllocations(t, schedulerCache)
	schedulerCache.Mutex.Lock()
	schedulerCache.ensureDeviceTopologyState().publish()
	schedulerCache.Mutex.Unlock()

	assertTopologyReservationAvailability(t, schedulerCache, plan, schedulingapi.DeviceAllocated)
}

func TestSchedulerCacheRecoversAfterRestartThroughMockAllocationAdapter(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	mockAdapter, err := adaptermock.New(adapter.DeviceIdentityContract{
		ProviderID:   topologyCacheTestProviderID,
		ResourceName: resourceName,
		Namespace:    topologyCacheTestNamespace,
	}, nil)
	if err != nil {
		t.Fatalf("adaptermock.New() error = %v", err)
	}
	node, before := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), mockAdapter)
	plan := twoPodTopologyReservationTestPlan(t, before, node, resourceName)
	commitTopologyReservationTestPlan(t, before, plan)

	// A new scheduler starts with an empty ledger and the same backend.
	_, after := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), mockAdapter)
	recoverTopologyTestAllocations(t, after)
	assertTopologyReservationAvailability(t, after, plan, schedulingapi.DeviceBinding)

	after.DeletePod(topologyReleaseTestPod("pod-a", corev1.PodRunning))
	drainTopologyTestPodReleases(after)
	assertTopologyReservationAvailability(t, after, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
	assertTopologyReservationAvailability(t, after, topologyTestPlacementOnly(plan, 1), schedulingapi.DeviceBinding)
	states, err := mockAdapter.Recover(context.Background())
	if err != nil || len(states) != 1 || len(states[0].Plan.Placements) != 1 || states[0].Plan.Placements[0].PodUID != "pod-b" {
		t.Fatalf("mock Recover() after releasing pod-a = (%#v, %v), want only pod-b", states, err)
	}
}

// recoverTopologyTestAllocations runs startup recovery once and requires it to
// finish.
func recoverTopologyTestAllocations(t *testing.T, schedulerCache *SchedulerCache) {
	t.Helper()
	schedulerCache.beginDeviceTopologyAllocationRecovery()
	if pending := schedulerCache.recoverDeviceTopologyAllocations(context.Background()); pending {
		t.Fatal("recoverDeviceTopologyAllocations() = pending, want done")
	}
}

// noRecoveryTopologyTestAdapter is an adapter that cannot recover its
// reservations, so it never serves hard placement.
type noRecoveryTopologyTestAdapter struct {
	*topologyReservationTestAdapter
}

func (testAdapter *noRecoveryTopologyTestAdapter) Capabilities() adapter.AdapterCapabilities {
	capabilities := testAdapter.topologyReservationTestAdapter.Capabilities()
	capabilities.Recovery = false
	return capabilities
}
