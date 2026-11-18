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
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
)

func TestSchedulerCacheDeletedPodReleasesOnlyItsDevices(t *testing.T) {
	tests := []struct {
		name   string
		delete func(schedulerCache *SchedulerCache, pod *corev1.Pod)
	}{
		{
			name: "Pod deleted",
			delete: func(schedulerCache *SchedulerCache, pod *corev1.Pod) {
				schedulerCache.DeletePod(pod)
			},
		},
		{
			name: "deleted Pod in an unknown final state",
			delete: func(schedulerCache *SchedulerCache, pod *corev1.Pod) {
				schedulerCache.DeletePod(cache.DeletedFinalStateUnknown{Key: "default/pod-a", Obj: pod})
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

			revision := schedulerCache.DeviceTopologySnapshot().Revision()
			test.delete(schedulerCache, topologyReleaseTestPod("pod-a", corev1.PodRunning))

			// The handler only queues the release: the devices stay blocked,
			// and the revision a reservation in progress holds does not change.
			assertTopologyReservationAvailability(t, schedulerCache, plan, schedulingapi.DeviceBinding)
			if got := schedulerCache.DeviceTopologySnapshot().Revision(); got != revision {
				t.Fatalf("topology revision after the delete event = %d, want %d", got, revision)
			}
			if _, holds := schedulerCache.deviceTopology.podAllocations["pod-a"]; holds {
				t.Fatal("pod-a still holds devices after the delete event, want its record moved to the pending releases")
			}
			if len(allocationAdapter.releasedPods) != 0 {
				t.Fatalf("ReleasePod() calls in the event handler = %#v, want none", allocationAdapter.releasedPods)
			}

			drainTopologyTestPodReleases(schedulerCache)
			if len(allocationAdapter.releasedPods) != 1 || allocationAdapter.releasedPods[0].podUID != "pod-a" {
				t.Fatalf("ReleasePod() calls = %#v, want one call for pod-a", allocationAdapter.releasedPods)
			}
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 1), schedulingapi.DeviceBinding)

			schedulerCache.DeletePod(topologyReleaseTestPod("pod-a", corev1.PodRunning))
			if got := schedulerCache.deviceTopologyPodReleaseQueue.Len(); got != 0 {
				t.Fatalf("release queue length after a repeated delete = %d, want 0", got)
			}
		})
	}
}

// TestSchedulerCacheSlowPodReleaseDoesNotBlockReservations holds a Pod
// release inside the adapter call, and checks that a reservation of other
// devices still completes.
func TestSchedulerCacheSlowPodReleaseDoesNotBlockReservations(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := &holdingReleaseTestAdapter{topologyReservationTestAdapter: newTopologyReservationTestAdapter(resourceName)}
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	commitTopologyReservationTestPlan(t, schedulerCache, plan)
	schedulerCache.DeletePod(topologyReleaseTestPod("pod-a", corev1.PodRunning))

	reserveWhileAdapterCallIsHeld(t, schedulerCache, node, resourceName, func(hold func()) {
		allocationAdapter.hold = hold
	}, func() {
		_, _ = schedulerCache.releaseDeviceTopologyPod(context.Background(), "pod-a", false)
	})
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
}

// holdingReleaseTestAdapter runs hold inside ReleasePod, so a test can keep
// the adapter call in progress.
type holdingReleaseTestAdapter struct {
	*topologyReservationTestAdapter
	hold func()
}

func (testAdapter *holdingReleaseTestAdapter) ReleasePod(ctx context.Context, reservation adapter.AdapterReservation, podUID types.UID) error {
	if testAdapter.hold != nil {
		testAdapter.hold()
	}
	return testAdapter.topologyReservationTestAdapter.ReleasePod(ctx, reservation, podUID)
}

// reserveWhileAdapterCallIsHeld runs work in the background, holds its
// adapter call through the hook that setHook installs, and checks that a
// reservation of a free device completes while that call is in progress.
func reserveWhileAdapterCallIsHeld(t *testing.T, schedulerCache *SchedulerCache, node *corev1.Node, resourceName corev1.ResourceName, setHook func(hold func()), work func()) {
	t.Helper()
	entered := make(chan struct{})
	unblock := make(chan struct{})
	setHook(func() {
		close(entered)
		<-unblock
	})
	workDone := make(chan struct{})
	go func() {
		defer close(workDone)
		work()
	}()
	<-entered

	plan := topologyReservationTestPlan(t, schedulerCache, node, map[corev1.ResourceName]int64{resourceName: 1})
	reserved := make(chan error, 1)
	go func() {
		_, err := schedulerCache.ReserveDeviceTopologyPlan(context.Background(), plan)
		reserved <- err
	}()
	select {
	case err := <-reserved:
		if err != nil {
			close(unblock)
			t.Fatalf("ReserveDeviceTopologyPlan() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		close(unblock)
		t.Fatal("ReserveDeviceTopologyPlan() did not return while the adapter call was in progress")
	}
	close(unblock)
	<-workDone
	assertTopologyReservationAvailability(t, schedulerCache, plan, schedulingapi.DeviceHeld)
}

func TestSchedulerCacheEndedPodReleasesItsDevices(t *testing.T) {
	tests := []struct {
		name        string
		phase       corev1.PodPhase
		wantRelease bool
	}{
		{name: "Pod succeeded", phase: corev1.PodSucceeded, wantRelease: true},
		{name: "Pod failed", phase: corev1.PodFailed, wantRelease: true},
		{name: "Pod still running", phase: corev1.PodRunning},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resourceName := corev1.ResourceName("nvidia.com/gpu")
			allocationAdapter := newTopologyReservationTestAdapter(resourceName)
			node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
			plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
			commitTopologyReservationTestPlan(t, schedulerCache, plan)

			schedulerCache.UpdatePod(topologyReleaseTestPod("pod-a", corev1.PodRunning), topologyReleaseTestPod("pod-a", test.phase))
			drainTopologyTestPodReleases(schedulerCache)

			if !test.wantRelease {
				if len(allocationAdapter.releasedPods) != 0 {
					t.Fatalf("ReleasePod() calls = %#v, want none for a running Pod", allocationAdapter.releasedPods)
				}
				assertTopologyReservationAvailability(t, schedulerCache, plan, schedulingapi.DeviceBinding)
				return
			}
			if len(allocationAdapter.releasedPods) != 1 || allocationAdapter.releasedPods[0].podUID != "pod-a" {
				t.Fatalf("ReleasePod() calls = %#v, want one call for pod-a", allocationAdapter.releasedPods)
			}
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
			assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 1), schedulingapi.DeviceBinding)
		})
	}
}

func TestSchedulerCachePodReleaseRetriesUntilTheLastAttempt(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	useImmediateTopologyTestReleaseRetries(t, schedulerCache)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	commitTopologyReservationTestPlan(t, schedulerCache, plan)
	allocationAdapter.releasePodErr = adapter.NewError(adapter.ErrorBackendFailure, errors.New("backend unavailable"))

	releaseTopologyTestPod(schedulerCache, "pod-a")

	if got := len(allocationAdapter.releasedPods); got != deviceTopologyPodReleaseAttempts {
		t.Fatalf("ReleasePod() calls = %d, want %d attempts", got, deviceTopologyPodReleaseAttempts)
	}
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceUnreconciled)
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 1), schedulingapi.DeviceBinding)
	if len(schedulerCache.deviceTopology.podReleases) != 0 {
		t.Fatalf("pending releases after the last attempt = %#v, want none", schedulerCache.deviceTopology.podReleases)
	}
}

func TestSchedulerCachePodReleaseSucceedsOnRetry(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	commitTopologyReservationTestPlan(t, schedulerCache, plan)
	allocationAdapter.releasePodErr = adapter.NewError(adapter.ErrorBackendFailure, errors.New("backend unavailable"))

	releaseTopologyTestPod(schedulerCache, "pod-a")
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceReleasing)

	allocationAdapter.releasePodErr = nil
	if pending, err := schedulerCache.releaseDeviceTopologyPod(context.Background(), "pod-a", false); pending || err != nil {
		t.Fatalf("retried releaseDeviceTopologyPod(pod-a) = (%v, %v), want (false, nil)", pending, err)
	}
	assertTopologyReservationAvailability(t, schedulerCache, topologyTestPlacementOnly(plan, 0), schedulingapi.DeviceAvailable)
}

func TestSchedulerCacheReservationReleaseDropsPendingPodRelease(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	reservation := commitTopologyReservationTestPlan(t, schedulerCache, plan)
	allocationAdapter.releasePodErr = adapter.NewError(adapter.ErrorBackendFailure, errors.New("backend unavailable"))
	releaseTopologyTestPod(schedulerCache, "pod-a")

	if err := schedulerCache.ReleaseDeviceTopologyReservation(context.Background(), reservation); err != nil {
		t.Fatalf("ReleaseDeviceTopologyReservation() error = %v", err)
	}
	assertTopologyReservationAvailability(t, schedulerCache, plan, schedulingapi.DeviceAvailable)
	if len(schedulerCache.deviceTopology.podAllocations) != 0 || len(schedulerCache.deviceTopology.podReleases) != 0 {
		t.Fatalf("records after releasing the reservation = (%#v, %#v), want none", schedulerCache.deviceTopology.podAllocations, schedulerCache.deviceTopology.podReleases)
	}

	calls := len(allocationAdapter.releasedPods)
	if pending, err := schedulerCache.releaseDeviceTopologyPod(context.Background(), "pod-a", false); pending || err != nil {
		t.Fatalf("releaseDeviceTopologyPod(pod-a) after the reservation release = (%v, %v), want (false, nil)", pending, err)
	}
	if len(allocationAdapter.releasedPods) != calls {
		t.Fatal("ReleasePod() was called for a reservation that is already released")
	}
}

func TestSchedulerCacheReservationReleaseAfterOnePodWasReleased(t *testing.T) {
	resourceName := corev1.ResourceName("nvidia.com/gpu")
	allocationAdapter := newTopologyReservationTestAdapter(resourceName)
	node, schedulerCache := newTopologyReservationTestCache(t, topologyBindTestDevices(resourceName), allocationAdapter)
	plan := twoPodTopologyReservationTestPlan(t, schedulerCache, node, resourceName)
	reservation := commitTopologyReservationTestPlan(t, schedulerCache, plan)

	// pod-a is deleted after the bind commit and its devices are released
	// before the statement rolls the whole batch back.
	schedulerCache.DeletePod(topologyReleaseTestPod("pod-a", corev1.PodPending))
	drainTopologyTestPodReleases(schedulerCache)

	if err := schedulerCache.RollbackDeviceTopologyReservationBind(context.Background(), reservation); err != nil {
		t.Fatalf("RollbackDeviceTopologyReservationBind() error = %v", err)
	}
	assertTopologyReservationAvailability(t, schedulerCache, plan, schedulingapi.DeviceAvailable)
	if len(schedulerCache.deviceTopology.podAllocations) != 0 || len(schedulerCache.deviceTopology.reservationOwners) != 0 {
		t.Fatalf("records after the rollback = (%#v, %#v), want none", schedulerCache.deviceTopology.podAllocations, schedulerCache.deviceTopology.reservationOwners)
	}
	commitTopologyReservationTestPlan(t, schedulerCache, topologyReservationTestPlan(t, schedulerCache, node, map[corev1.ResourceName]int64{resourceName: 1}))
}

// topologyReleaseTestPod returns a Pod with the UID used by the topology
// release test plans.
func topologyReleaseTestPod(uid types.UID, phase corev1.PodPhase) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: string(uid), Namespace: "default", UID: uid},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

// releaseTopologyTestPod starts the release of a Pod's devices and runs the
// release worker until no attempt is due.
func releaseTopologyTestPod(schedulerCache *SchedulerCache, podUID types.UID) {
	schedulerCache.Mutex.Lock()
	schedulerCache.startDeviceTopologyPodReleaseLocked(podUID)
	schedulerCache.Mutex.Unlock()
	drainTopologyTestPodReleases(schedulerCache)
}

// drainTopologyTestPodReleases runs the release worker until no attempt is
// due. A retry waiting for its back-off is left in the queue.
func drainTopologyTestPodReleases(schedulerCache *SchedulerCache) {
	for schedulerCache.deviceTopologyPodReleaseQueue.Len() > 0 {
		schedulerCache.processDeviceTopologyPodReleaseQueue()
	}
}

// useImmediateTopologyTestReleaseRetries replaces the release queue with one
// that retries without back-off.
func useImmediateTopologyTestReleaseRetries(t *testing.T, schedulerCache *SchedulerCache) {
	t.Helper()
	schedulerCache.deviceTopologyPodReleaseQueue.ShutDown()
	schedulerCache.deviceTopologyPodReleaseQueue = workqueue.NewTypedRateLimitingQueue(workqueue.NewTypedItemExponentialFailureRateLimiter[types.UID](0, 0))
}
