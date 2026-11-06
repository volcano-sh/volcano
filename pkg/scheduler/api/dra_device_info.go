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

// DRADeviceInfo describes the released/occupied state of a set of DRA devices.
type DRADeviceInfo struct {
	Devices            sets.Set[structured.DeviceID]
	SharedDevices      sets.Set[structured.SharedDeviceID]
	AggregatedCapacity structured.ConsumedCapacityCollection
}

func NewDRADeviceState() *DRADeviceInfo {
	return &DRADeviceInfo{
		Devices:            sets.New[structured.DeviceID](),
		SharedDevices:      sets.New[structured.SharedDeviceID](),
		AggregatedCapacity: structured.NewConsumedCapacityCollection(),
	}
}

// IsEmpty returns whether the device state is empty.
func (s *DRADeviceInfo) IsEmpty() bool {
	return s == nil || (s.Devices.Len() == 0 &&
		s.SharedDevices.Len() == 0 &&
		len(s.AggregatedCapacity) == 0)
}

// Merge merges the devices in other into the current state (union), with capacities
// aggregated per DeviceID by ConsumedCapacityCollection.Insert.
func (s *DRADeviceInfo) Merge(other *DRADeviceInfo) {
	if s == nil || other == nil {
		return
	}
	s.Devices.Insert(other.Devices.UnsortedList()...)
	s.SharedDevices.Insert(other.SharedDevices.UnsortedList()...)
	for deviceID, capacity := range other.AggregatedCapacity {
		s.AggregatedCapacity.Insert(structured.DeviceConsumedCapacity{
			DeviceID:         deviceID,
			ConsumedCapacity: capacity,
		})
	}
}

// Contains checks whether the current state contains all devices and capacities of other.
// Call it before Subtract to prevent repeated subtraction caused by double deduction.
func (s *DRADeviceInfo) Contains(other *DRADeviceInfo) bool {
	if s == nil || other == nil {
		return false
	}
	if !s.Devices.HasAll(other.Devices.UnsortedList()...) {
		return false
	}
	if !s.SharedDevices.HasAll(other.SharedDevices.UnsortedList()...) {
		return false
	}
	for deviceID, capacity := range other.AggregatedCapacity {
		existing, found := s.AggregatedCapacity[deviceID]
		if !found {
			return false
		}
		// The current capacity must cover every item to be subtracted
		for name, quantity := range capacity {
			existingQty, ok := existing[name]
			if !ok || existingQty.Cmp(*quantity) < 0 {
				return false
			}
		}
	}
	return true
}

// Subtract deducts the devices and capacities in other from the current state (difference).
// If other is not fully contained in the current state, an Error log is recorded and the
// deduction is skipped to prevent double subtraction.
func (s *DRADeviceInfo) Subtract(other *DRADeviceInfo) {
	if s == nil || other == nil {
		return
	}
	if !s.Contains(other) {
		klog.ErrorS(nil, "DRA device state does not contain the devices to subtract, skip",
			"devices", other.Devices.Len(), "sharedDevices", other.SharedDevices.Len(),
			"capacities", len(other.AggregatedCapacity))
		return
	}
	s.Devices.Delete(other.Devices.UnsortedList()...)
	s.SharedDevices.Delete(other.SharedDevices.UnsortedList()...)
	for deviceID, capacity := range other.AggregatedCapacity {
		s.AggregatedCapacity.Remove(structured.DeviceConsumedCapacity{
			DeviceID:         deviceID,
			ConsumedCapacity: capacity,
		})
	}
}

// Clone deep-copies the device state.
func (s *DRADeviceInfo) Clone() *DRADeviceInfo {
	if s == nil {
		return nil
	}
	clone := NewDRADeviceState()
	for deviceID := range s.Devices {
		clone.Devices.Insert(deviceID)
	}
	for sharedDeviceID := range s.SharedDevices {
		clone.SharedDevices.Insert(sharedDeviceID)
	}
	for deviceID, capacity := range s.AggregatedCapacity {
		clone.AggregatedCapacity[deviceID] = capacity.Clone()
	}
	return clone
}

// String returns a summary of the device state, for logging.
func (s *DRADeviceInfo) String() string {
	if s == nil {
		return "<nil>"
	}
	return fmt.Sprintf("devices<%d> sharedDevices<%d> capacities<%d>",
		s.Devices.Len(), s.SharedDevices.Len(), len(s.AggregatedCapacity))
}

// DRADeviceStateFromAllocationResults builds a device state from DRA allocation results
// (the return value of allocator.Allocate or claim.Status.Allocation), used for
// DraPipelined resource accounting and TaskInfo.DRAAllocatedDevices extraction.
func DRADeviceStateFromAllocationResults(
	results []resourceapi.AllocationResult,
	enableConsumableCapacity bool,
) *DRADeviceInfo {
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
					state.AggregatedCapacity.Insert(structured.NewDeviceConsumedCapacity(deviceID, deviceResult.ConsumedCapacity))
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
func (s *DRADeviceInfo) RemoveFromAllocatedState(state *structured.AllocatedState) {
	if s == nil || state == nil {
		return
	}
	state.AllocatedDevices.Delete(s.Devices.UnsortedList()...)
	state.AllocatedSharedDeviceIDs.Delete(s.SharedDevices.UnsortedList()...)
	for deviceID, capacity := range s.AggregatedCapacity {
		state.AggregatedCapacity.Remove(structured.DeviceConsumedCapacity{
			DeviceID:         deviceID,
			ConsumedCapacity: capacity,
		})
	}
}

// InsertToAllocatedState adds the currently occupied devices into the allocated device snapshot
func (s *DRADeviceInfo) InsertToAllocatedState(state *structured.AllocatedState) {
	if s == nil || state == nil {
		return
	}
	state.AllocatedDevices.Insert(s.Devices.UnsortedList()...)
	state.AllocatedSharedDeviceIDs.Insert(s.SharedDevices.UnsortedList()...)
	for deviceID, capacity := range s.AggregatedCapacity {
		state.AggregatedCapacity.Insert(structured.DeviceConsumedCapacity{
			DeviceID:         deviceID,
			ConsumedCapacity: capacity,
		})
	}
}
