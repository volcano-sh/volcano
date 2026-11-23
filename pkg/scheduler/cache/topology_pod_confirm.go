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
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/topology/adapter"
)

// deviceTopologyPodConfirmTimeout is how long after the bind commit, or after
// recovery, an adapter has to confirm a Pod's devices before they become
// Unreconciled.
const deviceTopologyPodConfirmTimeout = 5 * time.Minute

// preparedDeviceTopologyConfirmGroup is one Pod's Binding share of one
// adapter reservation, waiting for confirmation.
type preparedDeviceTopologyConfirmGroup struct {
	owner             deviceTopologyReservationOwner
	allocationAdapter adapter.AllocationAdapter
	deviceIDs         []schedulingapi.DeviceID
	confirmBy         time.Time
}

// confirmBoundDeviceTopology queues the confirmation of the devices of Pods
// whose bind succeeded. Pods that hold no devices are ignored. It takes
// SchedulerCache.Mutex once for the whole batch, does no adapter I/O, and
// does not change the topology revision.
func (sc *SchedulerCache) confirmBoundDeviceTopology(tasks []*schedulingapi.TaskInfo) {
	// Without an allocation adapter no Pod holds devices. The registry is set
	// before Run, so this check needs no lock on the bind path.
	if len(tasks) == 0 || sc.deviceTopologyAdapterRegistry == nil {
		return
	}
	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	state := sc.deviceTopology
	if state == nil || len(state.podAllocations) == 0 {
		return
	}
	for _, task := range tasks {
		if task == nil || task.Pod == nil {
			continue
		}
		if _, holdsDevices := state.podAllocations[task.Pod.UID]; !holdsDevices {
			continue
		}
		sc.deviceTopologyPodConfirmQueue.Add(task.Pod.UID)
	}
}

// processDeviceTopologyPodConfirmQueue confirms the devices of one queued Pod.
// An unconfirmed Pod is retried with back-off until its confirm deadline.
func (sc *SchedulerCache) processDeviceTopologyPodConfirmQueue() {
	queue := sc.deviceTopologyPodConfirmQueue
	podUID, shutdown := queue.Get()
	if shutdown {
		return
	}
	defer queue.Done(podUID)

	pending, err := sc.confirmDeviceTopologyPod(context.Background(), podUID)
	if pending {
		// An adapter may not observe the allocation until the Node admits the
		// Pod, so a retry is expected and is not reported as an error.
		klog.V(3).InfoS("Device topology devices of a Pod are not confirmed yet", "podUID", podUID, "err", err)
		queue.AddRateLimited(podUID)
		return
	}
	if err != nil {
		klog.ErrorS(err, "Failed to confirm device topology devices of a Pod", "podUID", podUID)
	}
	queue.Forget(podUID)
}

// confirmDeviceTopologyPod asks the adapters whether they assigned a Pod's
// Binding devices. Confirmed devices become Allocated. A failed confirmation
// keeps the devices Binding for a retry until the confirm deadline, and then
// makes them Unreconciled. A report that the devices were released while the
// Pod is still bound makes them Unreconciled at once. Unreconciled devices
// keep their Pod record, so the Pod's end still releases them. A Pod that
// ended during the adapter call is left to the release worker. It reports
// whether the Pod still has devices to confirm.
//
// It does not hold the reservation mutex, so a slow adapter call never blocks
// a reservation of the scheduling path. That is safe: Binding devices cannot
// be reserved, and the answer is applied only while the Pod still holds them.
func (sc *SchedulerCache) confirmDeviceTopologyPod(ctx context.Context, podUID types.UID) (bool, error) {
	sc.Mutex.Lock()
	state := sc.ensureDeviceTopologyState()
	targets := make(map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability)
	groups, confirmErrors := state.prepareDeviceTopologyPodConfirm(sc.deviceTopologyAdapterRegistry, podUID, targets)
	sc.Mutex.Unlock()

	pending := false
	results := make(map[deviceTopologyReservationOwner]schedulingapi.DeviceAvailability, len(groups))
	for _, group := range groups {
		reservation := adapter.AdapterReservation{Token: group.owner.token, PlanDigest: group.owner.planDigest}
		callContext, cancel := sc.newDeviceTopologyAdapterCallContext(ctx)
		observation, err := group.allocationAdapter.Confirm(callContext, reservation, podUID)
		cancel()
		if err == nil {
			err = checkAllocationObservation(reservation, podUID, observation)
		}
		switch {
		case err == nil && observation.State == adapter.AllocationObservationAllocated:
			results[group.owner] = schedulingapi.DeviceAllocated
		case err == nil:
			confirmErrors = append(confirmErrors, fmt.Errorf("allocation adapter for resource %q reports the devices of bound Pod %q as released", group.owner.resourceName, podUID))
			results[group.owner] = schedulingapi.DeviceUnreconciled
		case sc.deviceTopologyReservationTime().Before(group.confirmBy):
			confirmErrors = append(confirmErrors, fmt.Errorf("confirm device topology devices of Pod %q for resource %q: %w", podUID, group.owner.resourceName, err))
			pending = true
		default:
			confirmErrors = append(confirmErrors, fmt.Errorf("device topology devices of Pod %q for resource %q were not confirmed by the deadline: %w", podUID, group.owner.resourceName, err))
			results[group.owner] = schedulingapi.DeviceUnreconciled
		}
	}

	sc.Mutex.Lock()
	defer sc.Mutex.Unlock()
	for _, group := range groups {
		availability, decided := results[group.owner]
		if !decided || !state.holdsPodShare(podUID, group.owner) {
			continue
		}
		setDeviceAvailabilityTarget(targets, group.deviceIDs, availability)
	}
	if err := sc.applyDeviceTopologyAvailabilityTargetsLocked(state, targets); err != nil {
		confirmErrors = append(confirmErrors, err)
	}
	return pending && len(state.podAllocations[podUID]) > 0, errors.Join(confirmErrors...)
}

// prepareDeviceTopologyPodConfirm finds the adapter for each Binding share of
// a Pod. A share that is already Allocated or Unreconciled needs nothing. A
// share without an adapter can never be confirmed, so its devices are set to
// become Unreconciled in targets. The caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) prepareDeviceTopologyPodConfirm(registry *adapter.Registry, podUID types.UID, targets map[schedulingapi.DeviceID]schedulingapi.DeviceAvailability) ([]preparedDeviceTopologyConfirmGroup, []error) {
	allocations := state.podAllocations[podUID]
	groups := make([]preparedDeviceTopologyConfirmGroup, 0, len(allocations))
	var confirmErrors []error
	for _, allocation := range allocations {
		if !state.awaitsConfirmation(allocation) {
			continue
		}
		if err := state.checkPodAllocation(podUID, allocation); err != nil {
			confirmErrors = append(confirmErrors, err)
			continue
		}
		var allocationAdapter adapter.AllocationAdapter
		if registry != nil {
			allocationAdapter, _ = registry.AdapterFor(allocation.owner.resourceName)
		}
		if allocationAdapter == nil {
			confirmErrors = append(confirmErrors, fmt.Errorf("resource %q has no allocation adapter to confirm Pod %q", allocation.owner.resourceName, podUID))
			setDeviceAvailabilityTarget(targets, allocation.deviceIDs, schedulingapi.DeviceUnreconciled)
			continue
		}
		groups = append(groups, preparedDeviceTopologyConfirmGroup{
			owner:             allocation.owner,
			allocationAdapter: allocationAdapter,
			deviceIDs:         append([]schedulingapi.DeviceID(nil), allocation.deviceIDs...),
			confirmBy:         allocation.confirmBy,
		})
	}
	return groups, confirmErrors
}

// holdsPodShare reports whether a bound Pod still holds its share of one
// reservation. The caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) holdsPodShare(podUID types.UID, owner deviceTopologyReservationOwner) bool {
	for _, allocation := range state.podAllocations[podUID] {
		if allocation.owner == owner {
			return true
		}
	}
	return false
}

// awaitsConfirmation reports whether every device of one Pod share is still
// Binding. The caller holds SchedulerCache.Mutex.
func (state *deviceTopologyState) awaitsConfirmation(allocation deviceTopologyPodAllocation) bool {
	for _, deviceID := range allocation.deviceIDs {
		if state.ledger.states[deviceID] != schedulingapi.DeviceBinding {
			return false
		}
	}
	return len(allocation.deviceIDs) > 0
}

// checkAllocationObservation verifies that an adapter answered for the same
// reservation and Pod that the cache asked about.
func checkAllocationObservation(reservation adapter.AdapterReservation, podUID types.UID, observation adapter.AllocationObservation) error {
	if observation.ReservationToken != reservation.Token || observation.PlanDigest != reservation.PlanDigest || observation.PodUID != podUID {
		return fmt.Errorf("allocation adapter returned an observation for a different reservation or Pod")
	}
	if observation.State != adapter.AllocationObservationAllocated && observation.State != adapter.AllocationObservationReleased {
		return fmt.Errorf("allocation adapter returned an unknown observation state %q", observation.State)
	}
	return nil
}
