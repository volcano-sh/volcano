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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
	adaptermock "volcano.sh/volcano/pkg/scheduler/topology/adapter/mock"
)

func TestSchedulerCacheReleasesOneBoundPodAndKeepsTheRest(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, cache, node, resourceName)
	reservation := commitTopologyReservationTestPlan(t, cache, plan)

	releaseTopologyTestPod(cache, "pod-a")
	handle := adapter.AdapterReservation{Token: reservation.AdapterReservations[0].Token, PlanDigest: reservation.AdapterReservations[0].PlanDigest}
	if want := []topologyTestPodCall{{reservation: handle, podUID: "pod-a"}}; !reflect.DeepEqual(allocationAdapter.releasedPods, want) {
		t.Fatalf("ReleasePod() calls = %#v, want %#v", allocationAdapter.releasedPods, want)
	}
	assertTopologyReservationAvailability(t, cache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
	assertTopologyReservationAvailability(t, cache, topologyTestPlacementOnly(plan, 1), schedulingapi.DeviceBinding)
	if len(allocationAdapter.releasedReservations) != 0 {
		t.Fatalf("Release() calls = %#v, want none for a one-Pod release", allocationAdapter.releasedReservations)
	}

	releaseTopologyTestPod(cache, "pod-a")
	releaseTopologyTestPod(cache, "pod-without-devices")
	if len(allocationAdapter.releasedPods) != 1 {
		t.Fatalf("ReleasePod() calls = %d, want no call for released or unknown Pods", len(allocationAdapter.releasedPods))
	}

	releaseTopologyTestPod(cache, "pod-b")
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceAvailable)
	if len(cache.deviceTopology.podAllocations) != 0 || len(cache.deviceTopology.podReleases) != 0 || len(cache.deviceTopology.reservationOwners) != 0 {
		t.Fatalf("records after releasing every Pod = (%#v, %#v, %#v), want none", cache.deviceTopology.podAllocations, cache.deviceTopology.podReleases, cache.deviceTopology.reservationOwners)
	}
}

func TestSchedulerCachePodReleaseFailureLeavesOnlyThatPodUnreconciled(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, cache, node, resourceName)
	commitTopologyReservationTestPlan(t, cache, plan)
	allocationAdapter.releasePodErr = adapter.NewError(adapter.ErrorBackendFailure, errors.New("backend unavailable"))

	releaseTopologyTestPod(cache, "pod-a")
	assertTopologyReservationAvailability(t, cache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceReleasing)
	assertTopologyReservationAvailability(t, cache, topologyTestPlacementOnly(plan, 1), schedulingapi.DeviceBinding)
	if _, pending := cache.deviceTopology.podReleases["pod-a"]; !pending {
		t.Fatal("pod-a has no pending release after a failed attempt, want one for a retry")
	}

	retry := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})
	if got := retry.Placements[0].Assignments[0].DeviceIDs[0].Value; got != "gpu-2" {
		t.Fatalf("retry plan selects %q, want the spare gpu-2 while gpu-0 is Releasing", got)
	}
	commitTopologyReservationTestPlan(t, cache, retry)
	assertTopologyReservationAvailability(t, cache, retry, schedulingapi.DeviceBinding)

	pending, err := cache.releaseDeviceTopologyPod(context.Background(), "pod-a", true)
	if pending || err == nil || !strings.Contains(err.Error(), "backend unavailable") {
		t.Fatalf("last releaseDeviceTopologyPod(pod-a) = (%v, %v), want no pending release and a backend failure", pending, err)
	}
	assertTopologyReservationAvailability(t, cache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceUnreconciled)
	assertTopologyReservationAvailability(t, cache, retry, schedulingapi.DeviceBinding)
	if allocations := cache.deviceTopology.podAllocations["pod-a"]; len(allocations) != 1 {
		t.Fatalf("pod-a allocations after the old release gave up = %#v, want the new bind's record", allocations)
	}
}

func TestSchedulerCacheFailedBindRollbackDoesNotBlockPods(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})
	reservation := commitTopologyReservationTestPlan(t, cache, plan)
	allocationAdapter.releaseErr = adapter.NewError(adapter.ErrorBackendFailure, errors.New("backend unavailable"))

	if err := cache.RollbackDeviceTopologyReservationBind(context.Background(), reservation); err == nil {
		t.Fatal("RollbackDeviceTopologyReservationBind() error = nil, want backend failure")
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceUnreconciled)
	if len(cache.deviceTopology.podAllocations) != 0 {
		t.Fatalf("Pod allocations after failed rollback = %#v, want none", cache.deviceTopology.podAllocations)
	}
	commitTopologyReservationTestPlan(t, cache, topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1}))
}

func TestSchedulerCacheBindCommitRejectsPodThatStillHoldsDevices(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, cache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	commitTopologyReservationTestPlan(t, cache, topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1}))

	second := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 1})
	reservation, err := cache.ReserveDeviceTopologyPlan(context.Background(), second)
	if err != nil {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v", err)
	}
	if _, err := cache.PrepareDeviceTopologyReservationBind(context.Background(), reservation); err != nil {
		t.Fatalf("PrepareDeviceTopologyReservationBind() error = %v", err)
	}
	if err := cache.CommitDeviceTopologyReservationBind(context.Background(), reservation); err == nil || !strings.Contains(err.Error(), "still holds") {
		t.Fatalf("CommitDeviceTopologyReservationBind() error = %v, want conflict with the Pod's earlier devices", err)
	}
	assertTopologyReservationAvailability(t, cache, second, schedulingapi.DeviceHeld)
}

func TestSchedulerCacheFailedBindReleasesOnlyThatPodsDevices(t *testing.T) {
	tests := []struct {
		name string
		bind func(cache *SchedulerCache, bindContexts []*BindContext)
	}{
		{
			name: "bind fails",
			bind: func(cache *SchedulerCache, bindContexts []*BindContext) {
				cache.Binder = failingTopologyTestBinder{}
				cache.Bind(context.Background(), bindContexts, nil)
			},
		},
		{
			name: "pre-bind fails",
			bind: func(cache *SchedulerCache, bindContexts []*BindContext) {
				if bound := cache.executePreBinds(context.Background(), bindContexts, map[string]PreBinder{"failing": failingTopologyTestPreBinder{}}); len(bound) != 0 {
					t.Fatalf("executePreBinds() = %d contexts, want none after pre-bind failure", len(bound))
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resourceName := corev1.ResourceName("nvidia.com/gpu")
			allocationAdapter := newTopologyReservationTestAdapter(resourceName)
			node, cache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
			plan := twoPodTopologyReservationTestPlan(t, cache, node, resourceName)
			commitTopologyReservationTestPlan(t, cache, plan)
			task := schedulingapi.NewTaskInfo(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "default", UID: "pod-a"}})
			task.NodeName = node.Name

			test.bind(cache, []*BindContext{{TaskInfo: task}})
			if _, pending := cache.deviceTopology.podReleases["pod-a"]; !pending {
				t.Fatal("pod-a has no pending release after its bind failed")
			}
			drainTopologyTestPodReleases(cache)

			if len(allocationAdapter.releasedPods) != 1 || allocationAdapter.releasedPods[0].podUID != "pod-a" {
				t.Fatalf("ReleasePod() calls = %#v, want one call for pod-a", allocationAdapter.releasedPods)
			}
			assertTopologyReservationAvailability(t, cache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
			assertTopologyReservationAvailability(t, cache, topologyTestPlacementOnly(plan, 1), schedulingapi.DeviceBinding)
		})
	}
}

func TestSchedulerCacheReleasesOnePodThroughMockAllocationAdapter(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	mockAdapter, err := adaptermock.New(adapter.DeviceIdentityContract{
		ProviderID:   topologyCacheTestProviderID,
		ResourceName: resourceName,
		Namespace:    topologyCacheTestNamespace,
	}, nil)
	if err != nil {
		t.Fatalf("adaptermock.New() error = %v", err)
	}
	node, cache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), mockAdapter)
	plan := twoPodTopologyReservationTestPlan(t, cache, node, resourceName)
	commitTopologyReservationTestPlan(t, cache, plan)

	releaseTopologyTestPod(cache, "pod-a")
	states, err := mockAdapter.Recover(context.Background())
	if err != nil || len(states) != 1 || len(states[0].Plan.Placements) != 1 || states[0].Plan.Placements[0].PodUID != "pod-b" {
		t.Fatalf("mock Recover() = (%#v, %v), want one reservation that keeps only pod-b", states, err)
	}
	assertTopologyReservationAvailability(t, cache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)

	releaseTopologyTestPod(cache, "pod-b")
	if states, err := mockAdapter.Recover(context.Background()); err != nil || len(states) != 0 {
		t.Fatalf("mock Recover() after the last Pod = (%#v, %v), want no reservations", states, err)
	}
	assertTopologyReservationAvailability(t, cache, plan, schedulingapi.DeviceAvailable)
}

type failingTopologyTestBinder struct{}

func (failingTopologyTestBinder) Bind(_ kubernetes.Interface, tasks []*schedulingapi.TaskInfo) map[schedulingapi.TaskID]string {
	errMsg := make(map[schedulingapi.TaskID]string, len(tasks))
	for _, task := range tasks {
		errMsg[task.UID] = "bind rejected"
	}
	return errMsg
}

type failingTopologyTestPreBinder struct{}

func (failingTopologyTestPreBinder) PreBind(context.Context, *BindContext) error {
	return errors.New("pre-bind rejected")
}

func (failingTopologyTestPreBinder) PreBindRollBack(context.Context, *BindContext) {}

// topologyBindTestDevices returns three GPUs in one domain: two for a two-Pod
// plan and one spare for scheduling a Pod again.
func topologyBindTestDevices(resourceName corev1.ResourceName) []topologyReservationDeviceSpec {
	return []topologyReservationDeviceSpec{
		{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-0"},
		{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-1"},
		{resourceName: resourceName, domainValue: "gpu-domain", deviceValue: "gpu-2"},
	}
}

// twoPodTopologyReservationTestPlan places pod-a and pod-b on one Node with
// one device each.
func twoPodTopologyReservationTestPlan(t *testing.T, cache *SchedulerCache, node *corev1.Node, resourceName corev1.ResourceName) schedulingapi.TopologyPlacementPlan {
	t.Helper()
	plan := topologyReservationTestPlan(t, cache, node, map[corev1.ResourceName]int64{resourceName: 2})
	first := plan.Placements[0]
	second := first.Clone()
	second.TaskID, second.PodUID = "pod-b", "pod-b"
	first.Assignments[0].DeviceIDs = first.Assignments[0].DeviceIDs[:1]
	second.Assignments[0].DeviceIDs = second.Assignments[0].DeviceIDs[1:]
	plan.Placements = []schedulingapi.TopologyTaskPlacement{first, second}
	return plan
}

// commitTopologyReservationTestPlan reserves a plan and moves it to Binding.
func commitTopologyReservationTestPlan(t *testing.T, cache *SchedulerCache, plan schedulingapi.TopologyPlacementPlan) schedulingapi.TopologyReservation {
	t.Helper()
	reservation, err := cache.ReserveDeviceTopologyPlan(context.Background(), plan)
	if err != nil {
		t.Fatalf("ReserveDeviceTopologyPlan() error = %v", err)
	}
	if _, err := cache.PrepareDeviceTopologyReservationBind(context.Background(), reservation); err != nil {
		t.Fatalf("PrepareDeviceTopologyReservationBind() error = %v", err)
	}
	if err := cache.CommitDeviceTopologyReservationBind(context.Background(), reservation); err != nil {
		t.Fatalf("CommitDeviceTopologyReservationBind() error = %v", err)
	}
	return reservation
}

// topologyTestPlacementOnly returns a plan with only one of its placements.
func topologyTestPlacementOnly(plan schedulingapi.TopologyPlacementPlan, index int) schedulingapi.TopologyPlacementPlan {
	return schedulingapi.TopologyPlacementPlan{Placements: plan.Placements[index : index+1]}
}
