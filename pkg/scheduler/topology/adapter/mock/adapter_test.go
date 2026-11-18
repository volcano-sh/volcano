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

package mock

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
)

const (
	testProviderID   = "mock-inventory"
	testNamespace    = "mock-gpu-v1"
	testResourceName = corev1.ResourceName("nvidia.com/gpu")
)

var testObservedAt = time.Date(2026, time.November, 7, 0, 0, 0, 0, time.UTC)

func TestNewRequiresValidContractAndSupportsHardTopology(t *testing.T) {
	if _, err := New(adapter.DeviceIdentityContract{ProviderID: testProviderID, ResourceName: testResourceName}, nil); err == nil {
		t.Fatal("New() error = nil, want missing identity namespace rejection")
	}

	mockAdapter := newTestAdapter(t)
	if !mockAdapter.Capabilities().SupportsHardTopology() {
		t.Fatalf("Capabilities() = %#v, want hard topology support", mockAdapter.Capabilities())
	}
	if got := mockAdapter.IdentityContract(); got != testContract() {
		t.Fatalf("IdentityContract() = %#v, want %#v", got, testContract())
	}
}

func TestAdapterReservesDevicesAllOrNothing(t *testing.T) {
	mockAdapter := newTestAdapter(t)
	first, err := mockAdapter.Reserve(context.Background(), testPlan("pod-a", "gpu-0", "gpu-1"))
	if err != nil {
		t.Fatalf("first Reserve() error = %v", err)
	}
	if first.Token == "" || first.PlanDigest == "" {
		t.Fatalf("first Reserve() = %#v, want token and digest", first)
	}

	_, err = mockAdapter.Reserve(context.Background(), testPlan("pod-b", "gpu-1", "gpu-2"))
	assertAdapterErrorReason(t, err, adapter.ErrorReservationConflict)
	if _, err := mockAdapter.Reserve(context.Background(), testPlan("pod-c", "gpu-2")); err != nil {
		t.Fatalf("Reserve() of the device left free by the conflict error = %v, want no partial hold", err)
	}

	if err := mockAdapter.Release(context.Background(), first); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if _, err := mockAdapter.Reserve(context.Background(), testPlan("pod-d", "gpu-0", "gpu-1")); err != nil {
		t.Fatalf("Reserve() after Release() error = %v", err)
	}
	if err := mockAdapter.Release(context.Background(), first); err != nil {
		t.Fatalf("repeated Release() error = %v, want idempotent cleanup", err)
	}
}

func TestAdapterRejectsPlansOutsideItsContract(t *testing.T) {
	foreignDevice := api.DeviceID{ProviderID: testProviderID, Namespace: "other-namespace", Value: "gpu-0"}
	tests := []struct {
		name string
		plan api.TopologyPlacementPlan
	}{
		{
			name: "foreign identity namespace",
			plan: api.TopologyPlacementPlan{Placements: []api.TopologyTaskPlacement{{
				TaskID: "task-a", PodUID: "pod-a", NodeName: "node-a",
				Assignments: []api.TopologyContainerAssignment{{ResourceName: testResourceName, DeviceIDs: []api.DeviceID{foreignDevice}}},
			}}},
		},
		{
			name: "different resource",
			plan: api.TopologyPlacementPlan{Placements: []api.TopologyTaskPlacement{{
				TaskID: "task-a", PodUID: "pod-a", NodeName: "node-a",
				Assignments: []api.TopologyContainerAssignment{{ResourceName: "amd.com/gpu", DeviceIDs: []api.DeviceID{testDeviceID("gpu-0")}}},
			}}},
		},
		{
			name: "repeated device",
			plan: testPlan("pod-a", "gpu-0", "gpu-0"),
		},
		{
			name: "repeated Pod",
			plan: api.TopologyPlacementPlan{Placements: []api.TopologyTaskPlacement{
				{TaskID: "task-a", PodUID: "pod-a", NodeName: "node-a", Assignments: []api.TopologyContainerAssignment{{ResourceName: testResourceName, DeviceIDs: []api.DeviceID{testDeviceID("gpu-0")}}}},
				{TaskID: "task-b", PodUID: "pod-a", NodeName: "node-b", Assignments: []api.TopologyContainerAssignment{{ResourceName: testResourceName, DeviceIDs: []api.DeviceID{testDeviceID("gpu-1")}}}},
			}},
		},
		{
			name: "no devices",
			plan: testPlan("pod-a"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTestAdapter(t).Reserve(context.Background(), tt.plan)
			assertAdapterErrorReason(t, err, adapter.ErrorUnsupportedExactSelection)
		})
	}
}

func TestAdapterValidateTopologyChecksIdentityContract(t *testing.T) {
	mockAdapter := newTestAdapter(t)
	facts := api.NodeTopologyFacts{ProviderID: testProviderID, Devices: []api.TopologyDevice{
		{ID: testDeviceID("gpu-0"), ResourceName: testResourceName},
		{ID: api.DeviceID{ProviderID: testProviderID, Namespace: "amd-v1", Value: "amd-0"}, ResourceName: "amd.com/gpu"},
	}}
	if err := mockAdapter.ValidateTopology(context.Background(), facts); err != nil {
		t.Fatalf("ValidateTopology() error = %v", err)
	}

	foreignProvider := facts
	foreignProvider.ProviderID = "other-inventory"
	assertAdapterErrorReason(t, mockAdapter.ValidateTopology(context.Background(), foreignProvider), adapter.ErrorStaleInventory)

	foreignDevice := facts
	foreignDevice.Devices = []api.TopologyDevice{{ID: api.DeviceID{ProviderID: testProviderID, Namespace: "other-namespace", Value: "gpu-0"}, ResourceName: testResourceName}}
	assertAdapterErrorReason(t, mockAdapter.ValidateTopology(context.Background(), foreignDevice), adapter.ErrorStaleInventory)
}

func TestAdapterPrepareBindReturnsStableHandoff(t *testing.T) {
	mockAdapter := newTestAdapter(t)
	plan := testPlan("pod-a", "gpu-0")
	reservation, err := mockAdapter.Reserve(context.Background(), plan)
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}

	handoff, err := mockAdapter.PrepareBind(context.Background(), reservation, plan.Placements[0])
	if err != nil {
		t.Fatalf("PrepareBind() error = %v", err)
	}
	want := adapter.AllocationHandoff{ReservationToken: reservation.Token, PlanDigest: reservation.PlanDigest, PodUID: "pod-a", NodeName: "node-a"}
	if handoff != want {
		t.Fatalf("PrepareBind() = %#v, want %#v", handoff, want)
	}
	repeated, err := mockAdapter.PrepareBind(context.Background(), reservation, plan.Placements[0])
	if err != nil || repeated != handoff {
		t.Fatalf("repeated PrepareBind() = (%#v, %v), want the same handoff", repeated, err)
	}

	otherNode := plan.Placements[0]
	otherNode.NodeName = "node-b"
	if _, err := mockAdapter.PrepareBind(context.Background(), reservation, otherNode); err == nil {
		t.Fatal("PrepareBind() for another Node error = nil, want rejection")
	}
	otherPod := plan.Placements[0]
	otherPod.PodUID = "pod-b"
	if _, err := mockAdapter.PrepareBind(context.Background(), reservation, otherPod); err == nil {
		t.Fatal("PrepareBind() for another Pod error = nil, want rejection")
	}
	forged := reservation
	forged.PlanDigest = "sha256:forged"
	if _, err := mockAdapter.PrepareBind(context.Background(), forged, plan.Placements[0]); err == nil {
		t.Fatal("PrepareBind() with a forged digest error = nil, want rejection")
	}
	if err := mockAdapter.Release(context.Background(), forged); err == nil {
		t.Fatal("Release() with a forged digest error = nil, want rejection")
	}
}

func TestAdapterConfirmAndRecoverFollowReservationLifecycle(t *testing.T) {
	mockAdapter := newTestAdapter(t)
	plan := testPlan("pod-a", "gpu-0")
	reservation, err := mockAdapter.Reserve(context.Background(), plan)
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	assertRecoveredState(t, mockAdapter, reservation, adapter.AdapterReservationHeld)
	if _, err := mockAdapter.Confirm(context.Background(), reservation, "pod-a"); err == nil {
		t.Fatal("Confirm() before PrepareBind() error = nil, want missing handoff")
	}

	if _, err := mockAdapter.PrepareBind(context.Background(), reservation, plan.Placements[0]); err != nil {
		t.Fatalf("PrepareBind() error = %v", err)
	}
	assertRecoveredState(t, mockAdapter, reservation, adapter.AdapterReservationBinding)
	if _, err := mockAdapter.Confirm(context.Background(), reservation, "pod-b"); err == nil {
		t.Fatal("Confirm() for a Pod outside the reservation error = nil, want rejection")
	}

	observation, err := mockAdapter.Confirm(context.Background(), reservation, "pod-a")
	if err != nil || observation.State != adapter.AllocationObservationAllocated || observation.PodUID != "pod-a" || !observation.ObservedAt.Equal(testObservedAt) {
		t.Fatalf("Confirm() = (%#v, %v), want pod-a Allocated at %v", observation, err, testObservedAt)
	}
	assertRecoveredState(t, mockAdapter, reservation, adapter.AdapterReservationAllocated)

	if err := mockAdapter.Release(context.Background(), reservation); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	observation, err = mockAdapter.Confirm(context.Background(), reservation, "pod-a")
	if err != nil || observation.State != adapter.AllocationObservationReleased {
		t.Fatalf("Confirm() after Release() = (%#v, %v), want Released", observation, err)
	}
	states, err := mockAdapter.Recover(context.Background())
	if err != nil || len(states) != 0 {
		t.Fatalf("Recover() after Release() = (%#v, %v), want no reservations", states, err)
	}
}

func TestAdapterReleasesOnePodAndKeepsTheRest(t *testing.T) {
	mockAdapter := newTestAdapter(t)
	plan := testPlan("pod-a", "gpu-0")
	plan.Placements = append(plan.Placements, testPlan("pod-b", "gpu-1").Placements...)
	reservation, err := mockAdapter.Reserve(context.Background(), plan)
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	for _, placement := range plan.Placements {
		if _, err := mockAdapter.PrepareBind(context.Background(), reservation, placement); err != nil {
			t.Fatalf("PrepareBind(%q) error = %v", placement.PodUID, err)
		}
	}

	if err := mockAdapter.ReleasePod(context.Background(), reservation, "pod-a"); err != nil {
		t.Fatalf("ReleasePod(pod-a) error = %v", err)
	}
	if _, err := mockAdapter.Reserve(context.Background(), testPlan("pod-c", "gpu-0")); err != nil {
		t.Fatalf("Reserve() of the released Pod's device error = %v", err)
	}
	_, err = mockAdapter.Reserve(context.Background(), testPlan("pod-d", "gpu-1"))
	assertAdapterErrorReason(t, err, adapter.ErrorReservationConflict)

	observation, err := mockAdapter.Confirm(context.Background(), reservation, "pod-a")
	if err != nil || observation.State != adapter.AllocationObservationReleased {
		t.Fatalf("Confirm(pod-a) after ReleasePod() = (%#v, %v), want Released", observation, err)
	}
	observation, err = mockAdapter.Confirm(context.Background(), reservation, "pod-b")
	if err != nil || observation.State != adapter.AllocationObservationAllocated {
		t.Fatalf("Confirm(pod-b) = (%#v, %v), want Allocated", observation, err)
	}
	states, err := mockAdapter.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if len(states) != 2 || states[0].Reservation != reservation || len(states[0].Plan.Placements) != 1 || states[0].Plan.Placements[0].PodUID != "pod-b" {
		t.Fatalf("Recover() = %#v, want the first reservation to list only pod-b", states)
	}

	if err := mockAdapter.ReleasePod(context.Background(), reservation, "pod-a"); err != nil {
		t.Fatalf("repeated ReleasePod(pod-a) error = %v, want idempotent cleanup", err)
	}
	if err := mockAdapter.ReleasePod(context.Background(), reservation, "pod-x"); err == nil {
		t.Fatal("ReleasePod() for a Pod outside the reservation error = nil, want rejection")
	}
	forged := reservation
	forged.PlanDigest = "sha256:forged"
	if err := mockAdapter.ReleasePod(context.Background(), forged, "pod-b"); err == nil {
		t.Fatal("ReleasePod() with a forged digest error = nil, want rejection")
	}

	if err := mockAdapter.ReleasePod(context.Background(), reservation, "pod-b"); err != nil {
		t.Fatalf("ReleasePod(pod-b) error = %v", err)
	}
	states, err = mockAdapter.Recover(context.Background())
	if err != nil || len(states) != 1 || states[0].Reservation == reservation {
		t.Fatalf("Recover() after the last ReleasePod() = (%#v, %v), want only the pod-c reservation", states, err)
	}
	if err := mockAdapter.ReleasePod(context.Background(), reservation, "pod-b"); err != nil {
		t.Fatalf("ReleasePod() after the reservation ended error = %v, want idempotent cleanup", err)
	}
}

func TestAdapterHonorsCancelledContext(t *testing.T) {
	mockAdapter := newTestAdapter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := mockAdapter.ValidateTopology(ctx, api.NodeTopologyFacts{ProviderID: testProviderID}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ValidateTopology() error = %v, want context canceled", err)
	}
	if _, err := mockAdapter.Reserve(ctx, testPlan("pod-a", "gpu-0")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reserve() error = %v, want context canceled", err)
	}
	if err := mockAdapter.ReleasePod(ctx, adapter.AdapterReservation{Token: "mock-1", PlanDigest: "sha256:1"}, "pod-a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("ReleasePod() error = %v, want context canceled", err)
	}
	if _, err := mockAdapter.Recover(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Recover() error = %v, want context canceled", err)
	}
}

func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	mockAdapter, err := New(testContract(), func() time.Time { return testObservedAt })
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return mockAdapter
}

func testContract() adapter.DeviceIdentityContract {
	return adapter.DeviceIdentityContract{ProviderID: testProviderID, ResourceName: testResourceName, Namespace: testNamespace}
}

func testDeviceID(value string) api.DeviceID {
	return api.DeviceID{ProviderID: testProviderID, Namespace: testNamespace, Value: value}
}

// testPlan places one Pod on node-a with the given device values.
func testPlan(podUID string, deviceValues ...string) api.TopologyPlacementPlan {
	deviceIDs := make([]api.DeviceID, 0, len(deviceValues))
	for _, value := range deviceValues {
		deviceIDs = append(deviceIDs, testDeviceID(value))
	}
	return api.TopologyPlacementPlan{
		JobID:            "job-a",
		SchedulingUnitID: "job-a",
		Revision:         1,
		Placements: []api.TopologyTaskPlacement{{
			TaskID:      api.TaskID("task-" + podUID),
			PodUID:      types.UID(podUID),
			NodeName:    "node-a",
			NodeUID:     "node-a-uid",
			Assignments: []api.TopologyContainerAssignment{{ContainerName: "trainer", ResourceName: testResourceName, DeviceIDs: deviceIDs}},
		}},
	}
}

func assertAdapterErrorReason(t *testing.T, err error, want adapter.ErrorReason) {
	t.Helper()
	var adapterErr *adapter.Error
	if !errors.As(err, &adapterErr) || adapterErr.Reason != want {
		t.Fatalf("error = %v, want adapter reason %q", err, want)
	}
}

func assertRecoveredState(t *testing.T, mockAdapter *Adapter, reservation adapter.AdapterReservation, want adapter.AdapterReservationLifecycleState) {
	t.Helper()
	states, err := mockAdapter.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if len(states) != 1 || states[0].Reservation != reservation || states[0].State != want || len(states[0].Plan.Placements) != 1 {
		t.Fatalf("Recover() = %#v, want one %q reservation %#v", states, want, reservation)
	}
}
