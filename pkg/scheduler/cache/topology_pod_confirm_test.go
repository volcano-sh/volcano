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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
	adaptermock "volcano.sh/volcano/pkg/scheduler/topology/adapter/mock"
)

func TestSchedulerCacheBoundPodDevicesBecomeAllocated(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	commitTopologyReservationTestPlan(t, schedulerCache, plan)

	revision := schedulerCache.DeviceTopologySnapshot().Revision()
	bindTopologyTestPod(schedulerCache, node, "pod-a")

	// The bind only queues the confirmation: no adapter call, no state change.
	if got := schedulerCache.deviceTopologyPodConfirmQueue.Len(); got != 1 {
		t.Fatalf("confirm queue length after the bind = %d, want 1", got)
	}
	if len(allocationAdapter.confirmedPods) != 0 {
		t.Fatalf("Confirm() calls in the bind path = %#v, want none", allocationAdapter.confirmedPods)
	}
	if got := schedulerCache.DeviceTopologySnapshot().Revision(); got != revision {
		t.Fatalf("topology revision after the bind = %d, want %d", got, revision)
	}

	drainTopologyTestPodConfirms(schedulerCache)
	if len(allocationAdapter.confirmedPods) != 1 || allocationAdapter.confirmedPods[0].podUID != "pod-a" {
		t.Fatalf("Confirm() calls = %#v, want one call for pod-a", allocationAdapter.confirmedPods)
	}
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAllocated)
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 1), schedulingapi.DeviceBinding)

	// An Allocated Pod is released the same way when it ends.
	schedulerCache.DeletePod(topologyReleaseTestPod("pod-a", corev1.PodRunning))
	drainTopologyTestPodReleases(schedulerCache)
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
}

func TestSchedulerCacheBindOfPodWithoutDevicesQueuesNoConfirm(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), newTopologyReservationTestAdapter(resourceName))

	bindTopologyTestPod(schedulerCache, node, "pod-without-devices")
	if got := schedulerCache.deviceTopologyPodConfirmQueue.Len(); got != 0 {
		t.Fatalf("confirm queue length = %d, want 0 for a Pod without devices", got)
	}
}

func TestSchedulerCachePodConfirmRetriesUntilTheDeadline(t *testing.T) {
	tests := []struct {
		name        string
		failConfirm func(allocationAdapter *topologyReservationTestAdapter)
	}{
		{
			name: "adapter error",
			failConfirm: func(allocationAdapter *topologyReservationTestAdapter) {
				allocationAdapter.confirmErr = adapter.NewError(adapter.ErrorBackendFailure, errors.New("backend unavailable"))
			},
		},
		{
			name: "observation for a different Pod",
			failConfirm: func(allocationAdapter *topologyReservationTestAdapter) {
				allocationAdapter.observationFor = func(reservation adapter.AdapterReservation, _ types.UID) adapter.AllocationObservation {
					return adapter.AllocationObservation{ReservationToken: reservation.Token, PlanDigest: reservation.PlanDigest, PodUID: "pod-b", State: adapter.AllocationObservationAllocated}
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resourceName := corev1.ResourceName("nvidia.com/gpu")
			allocationAdapter := newTopologyReservationTestAdapter(resourceName)
			node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
			plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
			commitTopologyReservationTestPlan(t, schedulerCache, plan)
			test.failConfirm(allocationAdapter)

			pending, err := schedulerCache.confirmDeviceTopologyPod(context.Background(), "pod-a")
			if !pending || err == nil {
				t.Fatalf("confirmDeviceTopologyPod(pod-a) before the deadline = (%v, %v), want a pending retry with an error", pending, err)
			}
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceBinding)

			setTopologyTestClock(schedulerCache, topologyCacheTestObservedAt.Add(deviceTopologyPodConfirmTimeout))
			pending, err = schedulerCache.confirmDeviceTopologyPod(context.Background(), "pod-a")
			if pending || err == nil {
				t.Fatalf("confirmDeviceTopologyPod(pod-a) at the deadline = (%v, %v), want no retry with an error", pending, err)
			}
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceUnreconciled)
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 1), schedulingapi.DeviceBinding)

			// The Pod keeps its record, so its end still releases the devices.
			calls := len(allocationAdapter.confirmedPods)
			if pending, err := schedulerCache.confirmDeviceTopologyPod(context.Background(), "pod-a"); pending || err != nil {
				t.Fatalf("confirmDeviceTopologyPod(pod-a) after the deadline = (%v, %v), want (false, nil)", pending, err)
			}
			if len(allocationAdapter.confirmedPods) != calls {
				t.Fatal("Confirm() was called for Unreconciled devices")
			}
			schedulerCache.DeletePod(topologyReleaseTestPod("pod-a", corev1.PodRunning))
			drainTopologyTestPodReleases(schedulerCache)
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
		})
	}
}

func TestSchedulerCachePodConfirmSucceedsOnRetry(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	commitTopologyReservationTestPlan(t, schedulerCache, plan)
	allocationAdapter.confirmErr = adapter.NewError(adapter.ErrorBackendFailure, errors.New("not allocated yet"))

	if pending, _ := schedulerCache.confirmDeviceTopologyPod(context.Background(), "pod-a"); !pending {
		t.Fatal("confirmDeviceTopologyPod(pod-a) = no retry, want a retry while the adapter is not ready")
	}
	allocationAdapter.confirmErr = nil
	if pending, err := schedulerCache.confirmDeviceTopologyPod(context.Background(), "pod-a"); pending || err != nil {
		t.Fatalf("retried confirmDeviceTopologyPod(pod-a) = (%v, %v), want (false, nil)", pending, err)
	}
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAllocated)
}

func TestSchedulerCacheReleasedObservationForBoundPodMakesDevicesUnreconciled(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	commitTopologyReservationTestPlan(t, schedulerCache, plan)
	allocationAdapter.observationFor = func(reservation adapter.AdapterReservation, podUID types.UID) adapter.AllocationObservation {
		return adapter.AllocationObservation{ReservationToken: reservation.Token, PlanDigest: reservation.PlanDigest, PodUID: podUID, State: adapter.AllocationObservationReleased}
	}

	pending, err := schedulerCache.confirmDeviceTopologyPod(context.Background(), "pod-a")
	if pending || err == nil {
		t.Fatalf("confirmDeviceTopologyPod(pod-a) = (%v, %v), want no retry with an error", pending, err)
	}
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceUnreconciled)
	if _, holds := schedulerCache.deviceTopology.podAllocations["pod-a"]; !holds {
		t.Fatal("pod-a lost its record, want it kept so the Pod's end releases the devices")
	}
}

func TestSchedulerCacheConfirmSkipsPodThatEnded(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	commitTopologyReservationTestPlan(t, schedulerCache, plan)

	bindTopologyTestPod(schedulerCache, node, "pod-a")
	schedulerCache.DeletePod(topologyReleaseTestPod("pod-a", corev1.PodRunning))
	drainTopologyTestPodConfirms(schedulerCache)

	if len(allocationAdapter.confirmedPods) != 0 {
		t.Fatalf("Confirm() calls = %#v, want none for a Pod that ended", allocationAdapter.confirmedPods)
	}
	drainTopologyTestPodReleases(schedulerCache)
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
}

func TestSchedulerCacheConfirmLeavesPodDeletedDuringTheCallToRelease(t *testing.T) {
	tests := []struct {
		name       string
		confirmErr error
	}{
		{name: "adapter confirms"},
		{name: "adapter fails after the deadline", confirmErr: adapter.NewError(adapter.ErrorBackendFailure, errors.New("backend unavailable"))},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resourceName := corev1.ResourceName("nvidia.com/gpu")
			allocationAdapter := newTopologyReservationTestAdapter(resourceName)
			node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
			plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
			commitTopologyReservationTestPlan(t, schedulerCache, plan)
			if test.confirmErr != nil {
				setTopologyTestClock(schedulerCache, topologyCacheTestObservedAt.Add(deviceTopologyPodConfirmTimeout))
			}
			allocationAdapter.confirmErr = test.confirmErr
			allocationAdapter.confirmHook = func() {
				schedulerCache.DeletePod(topologyReleaseTestPod("pod-a", corev1.PodRunning))
			}

			if pending, _ := schedulerCache.confirmDeviceTopologyPod(context.Background(), "pod-a"); pending {
				t.Fatal("confirmDeviceTopologyPod(pod-a) = retry, want none for a Pod that ended")
			}
			// The release worker owns the devices now, so the answer is ignored.
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceBinding)
			drainTopologyTestPodReleases(schedulerCache)
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
		})
	}
}

func TestSchedulerCacheConfirmsOnePodThroughMockAllocationAdapter(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	mockAdapter, err := adaptermock.New(adapter.DeviceIdentityContract{
		ProviderID:   topologyCacheTestProviderID,
		ResourceName: resourceName,
		Namespace:    topologyCacheTestNamespace,
	}, nil)
	if err != nil {
		t.Fatalf("adaptermock.New() error = %v", err)
	}
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), mockAdapter)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	commitTopologyReservationTestPlan(t, schedulerCache, plan)

	bindTopologyTestPod(schedulerCache, node, "pod-a")
	drainTopologyTestPodConfirms(schedulerCache)
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAllocated)
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 1), schedulingapi.DeviceBinding)
}

// TestSchedulerCacheSlowPodConfirmDoesNotBlockReservations holds a Pod
// confirmation inside the adapter call, and checks that a reservation of
// other devices still completes.
func TestSchedulerCacheSlowPodConfirmDoesNotBlockReservations(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	commitTopologyReservationTestPlan(t, schedulerCache, plan)

	reserveWhileAdapterCallIsHeld(t, schedulerCache, node, resourceName, func(hold func()) {
		allocationAdapter.confirmHook = hold
	}, func() {
		_, _ = schedulerCache.confirmDeviceTopologyPod(context.Background(), "pod-a")
	})
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAllocated)
}

// bindTopologyTestPod runs a successful Kubernetes bind for one Pod.
func bindTopologyTestPod(schedulerCache *SchedulerCache, node *corev1.Node, podUID types.UID) {
	task := schedulingapi.NewTaskInfo(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: string(podUID), Namespace: "default", UID: podUID}})
	task.NodeName = node.Name
	schedulerCache.Binder = succeedingTopologyTestBinder{}
	schedulerCache.Bind(context.Background(), []*BindContext{{TaskInfo: task}}, nil)
}

// drainTopologyTestPodConfirms runs the confirm worker until no attempt is
// due. A retry waiting for its back-off is left in the queue.
func drainTopologyTestPodConfirms(schedulerCache *SchedulerCache) {
	for schedulerCache.deviceTopologyPodConfirmQueue.Len() > 0 {
		schedulerCache.processDeviceTopologyPodConfirmQueue()
	}
}

// setTopologyTestClock fixes the time the cache reads.
func setTopologyTestClock(schedulerCache *SchedulerCache, now time.Time) {
	schedulerCache.deviceTopologyNow = func() time.Time {
		return now
	}
}

type succeedingTopologyTestBinder struct{}

func (succeedingTopologyTestBinder) Bind(kubernetes.Interface, []*schedulingapi.TaskInfo) map[schedulingapi.TaskID]string {
	return nil
}
