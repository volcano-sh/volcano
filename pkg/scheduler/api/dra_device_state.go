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

package api

import (
	"fmt"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
)

// DRADeviceState describes the released/occupied state of a set of DRA devices.
// It is used for resource accounting of NodeInfo.DraReleasing / NodeInfo.DraPipelined,
// aligned with the Releasing / Pipelined semantics of CPU/memory.
type DRADeviceState struct {
	// Devices is the set of exclusive device IDs
	Devices sets.Set[structured.DeviceID]
	// SharedDevices is the set of shared device IDs (when DRAConsumableCapacity is enabled)
	SharedDevices sets.Set[structured.SharedDeviceID]
	// Capacities is the list of consumed device capacities, aggregated by DeviceID (when DRAConsumableCapacity is enabled)
	Capacities []structured.DeviceConsumedCapacity
}

// NewDRADeviceState creates an empty device state.
func NewDRADeviceState() *DRADeviceState {
	return &DRADeviceState{
		Devices:       sets.New[structured.DeviceID](),
		SharedDevices: sets.New[structured.SharedDeviceID](),
	}
}

// IsEmpty returns whether the device state is empty.
func (s *DRADeviceState) IsEmpty() bool {
	return s == nil || (s.Devices.Len() == 0 && s.SharedDevices.Len() == 0 && len(s.Capacities) == 0)
}

// Merge merges the devices in other into the current state (union), with capacities aggregated by DeviceID.
func (s *DRADeviceState) Merge(other *DRADeviceState) {
	if s == nil || other == nil {
		return
	}
	s.Devices.Insert(other.Devices.UnsortedList()...)
	s.SharedDevices.Insert(other.SharedDevices.UnsortedList()...)
	for _, capacity := range other.Capacities {
		s.insertCapacity(capacity)
	}
}

// insertCapacity inserts a single capacity entry aggregated by DeviceID: capacities of
// the same device are added together to avoid duplicate DeviceIDs in the list.
func (s *DRADeviceState) insertCapacity(capacity structured.DeviceConsumedCapacity) {
	for i := range s.Capacities {
		if s.Capacities[i].DeviceID == capacity.DeviceID {
			s.Capacities[i].ConsumedCapacity.Add(capacity.ConsumedCapacity)
			return
		}
	}
	s.Capacities = append(s.Capacities, structured.DeviceConsumedCapacity{
		DeviceID:         capacity.DeviceID,
		ConsumedCapacity: capacity.ConsumedCapacity.Clone(),
	})
}

// Contains checks whether the current state contains all devices and capacities of other.
// Call it before Subtract to prevent repeated subtraction caused by double deduction.
func (s *DRADeviceState) Contains(other *DRADeviceState) bool {
	if s == nil || other == nil {
		return false
	}
	if !s.Devices.HasAll(other.Devices.UnsortedList()...) {
		return false
	}
	if !s.SharedDevices.HasAll(other.SharedDevices.UnsortedList()...) {
		return false
	}
	for _, capacity := range other.Capacities {
		found := false
		for i := range s.Capacities {
			if s.Capacities[i].DeviceID != capacity.DeviceID {
				continue
			}
			found = true
			// The current capacity must cover every item to be subtracted
			for name, quantity := range capacity.ConsumedCapacity {
				existing, ok := s.Capacities[i].ConsumedCapacity[name]
				if !ok || existing.Cmp(*quantity) < 0 {
					return false
				}
			}
			break
		}
		if !found {
			return false
		}
	}
	return true
}

// Subtract deducts the devices and capacities in other from the current state (difference).
// If other is not fully contained in the current state, an Error log is recorded and the
// deduction is skipped to prevent double subtraction.
func (s *DRADeviceState) Subtract(other *DRADeviceState) {
	if s == nil || other == nil {
		return
	}
	if !s.Contains(other) {
		klog.ErrorS(nil, "DRA device state does not contain the devices to subtract, skip",
			"devices", other.Devices.Len(), "sharedDevices", other.SharedDevices.Len(), "capacities", len(other.Capacities))
		return
	}
	s.Devices.Delete(other.Devices.UnsortedList()...)
	s.SharedDevices.Delete(other.SharedDevices.UnsortedList()...)
	for _, capacity := range other.Capacities {
		for i := range s.Capacities {
			if s.Capacities[i].DeviceID != capacity.DeviceID {
				continue
			}
			s.Capacities[i].ConsumedCapacity.Sub(capacity.ConsumedCapacity)
			if s.Capacities[i].ConsumedCapacity.Empty() {
				s.Capacities = append(s.Capacities[:i], s.Capacities[i+1:]...)
			}
			break
		}
	}
}

// Clone deep-copies the device state.
func (s *DRADeviceState) Clone() *DRADeviceState {
	if s == nil {
		return nil
	}
	clone := &DRADeviceState{
		Devices:       sets.New(s.Devices.UnsortedList()...),
		SharedDevices: sets.New(s.SharedDevices.UnsortedList()...),
	}
	for _, capacity := range s.Capacities {
		clone.Capacities = append(clone.Capacities, structured.DeviceConsumedCapacity{
			DeviceID:         capacity.DeviceID,
			ConsumedCapacity: capacity.ConsumedCapacity.Clone(),
		})
	}
	return clone
}

// String returns a summary of the device state, for logging.
func (s *DRADeviceState) String() string {
	if s == nil {
		return "<nil>"
	}
	return fmt.Sprintf("devices<%d> sharedDevices<%d> capacities<%d>", s.Devices.Len(), s.SharedDevices.Len(), len(s.Capacities))
}

// DRADeviceStateFromAllocationResults builds a device state from DRA allocation results
// (the return value of allocator.Allocate or claim.Status.Allocation), used for
// DraPipelined resource accounting.
func DRADeviceStateFromAllocationResults(
	results []resourceapi.AllocationResult,
	enableConsumableCapacity bool,
) *DRADeviceState {
	state := NewDRADeviceState()
	for _, result := range results {
		for _, deviceResult := range result.Devices.Results {
			// Admin access devices can be shared with other claims; they do not count as
			// exclusive allocations, so skip them
			if ptr.Deref(deviceResult.AdminAccess, false) {
				continue
			}
			deviceID := structured.MakeDeviceID(deviceResult.Driver, deviceResult.Pool, deviceResult.Device)

			if enableConsumableCapacity && deviceResult.ShareID != nil {
				// Shared device: record the SharedDeviceID and its consumed capacity
				sharedDeviceID := structured.MakeSharedDeviceID(deviceID, deviceResult.ShareID)
				state.SharedDevices.Insert(sharedDeviceID)
				if deviceResult.ConsumedCapacity != nil {
					state.insertCapacity(structured.NewDeviceConsumedCapacity(deviceID, deviceResult.ConsumedCapacity))
				}
				continue
			}
			// Exclusive device
			state.Devices.Insert(deviceID)
		}
	}
	return state
}

// RemoveFromAllocatedState removes the currently released devices from the allocated device snapshot
func (s *DRADeviceState) RemoveFromAllocatedState(state *structured.AllocatedState) {
	if s == nil || state == nil {
		return
	}
	state.AllocatedDevices.Delete(s.Devices.UnsortedList()...)
	state.AllocatedSharedDeviceIDs.Delete(s.SharedDevices.UnsortedList()...)
	for _, capacity := range s.Capacities {
		state.AggregatedCapacity.Remove(capacity)
	}
}

// InsertToAllocatedState adds the currently occupied devices into the allocated device snapshot
func (s *DRADeviceState) InsertToAllocatedState(state *structured.AllocatedState) {
	if s == nil || state == nil {
		return
	}
	state.AllocatedDevices.Insert(s.Devices.UnsortedList()...)
	state.AllocatedSharedDeviceIDs.Insert(s.SharedDevices.UnsortedList()...)
	for _, capacity := range s.Capacities {
		state.AggregatedCapacity.Insert(capacity)
	}
}

// GetTaskDRAAllocatedDevicesFn extracts the releasable device state from the task's
// allocated claims, registered by the predicates plugin on OnSessionOpen.
var GetTaskDRAAllocatedDevicesFn func(task *TaskInfo) *DRADeviceState
