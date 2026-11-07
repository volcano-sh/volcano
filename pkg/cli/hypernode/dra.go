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

package hypernode

import (
	"fmt"
	"strconv"

	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/structured"
)

type draInventory struct {
	drivers []string
	devices map[string]map[string]sets.Set[structured.DeviceID]
}

func buildDRAInventory(slices []resourcev1.ResourceSlice, members sets.Set[string]) (draInventory, error) {
	inventory := draInventory{devices: make(map[string]map[string]sets.Set[structured.DeviceID])}
	pools := make(map[string][]*resourcev1.ResourceSlice)
	for i := range slices {
		slice := &slices[i]
		key := slice.Spec.Driver + "/" + slice.Spec.Pool.Name
		current := pools[key]
		if len(current) == 0 || slice.Spec.Pool.Generation > current[0].Spec.Pool.Generation {
			pools[key] = []*resourcev1.ResourceSlice{slice}
		} else if slice.Spec.Pool.Generation == current[0].Spec.Pool.Generation {
			pools[key] = append(current, slice)
		}
	}
	keys := sets.New[string]()
	for key := range pools {
		keys.Insert(key)
	}
	for _, key := range sets.List(keys) {
		pool := pools[key]
		relevant := false
		for _, slice := range pool {
			spec := &slice.Spec
			if spec.NodeName == nil || *spec.NodeName == "" || spec.NodeSelector != nil ||
				spec.AllNodes != nil || spec.PerDeviceNodeSelection != nil && *spec.PerDeviceNodeSelection {
				return inventory, fmt.Errorf("ResourceSlice %q: only node-local inventory with spec.nodeName is supported; use --dra=false to omit DRA inventory", slice.Name)
			}
			relevant = relevant || members.Has(*spec.NodeName)
		}
		if !relevant {
			continue
		}
		seen := sets.New[structured.DeviceID]()
		for _, slice := range pool {
			if slice.Spec.Pool.ResourceSliceCount != int64(len(pool)) {
				return inventory, fmt.Errorf("DRA pool %q generation %d is incomplete or inconsistent: read %d slices, ResourceSlice %q reports %d; retry after the pool update",
					key, slice.Spec.Pool.Generation, len(pool), slice.Name, slice.Spec.Pool.ResourceSliceCount)
			}
			driver, nodeName := slice.Spec.Driver, *slice.Spec.NodeName
			if members.Has(nodeName) {
				if inventory.devices[driver] == nil {
					inventory.devices[driver] = make(map[string]sets.Set[structured.DeviceID])
				}
				if inventory.devices[driver][nodeName] == nil {
					inventory.devices[driver][nodeName] = sets.New[structured.DeviceID]()
				}
			}
			for _, device := range slice.Spec.Devices {
				id := structured.MakeDeviceID(driver, slice.Spec.Pool.Name, device.Name)
				if seen.Has(id) {
					return inventory, fmt.Errorf("DRA pool %q generation %d has duplicate device %q", key, slice.Spec.Pool.Generation, device.Name)
				}
				seen.Insert(id)
				if members.Has(nodeName) {
					inventory.devices[driver][nodeName].Insert(id)
				}
			}
		}
	}
	drivers := sets.New[string]()
	for driver := range inventory.devices {
		drivers.Insert(driver)
	}
	inventory.drivers = sets.List(drivers)
	return inventory, nil
}

func (d *draInventory) values(members sets.Set[string]) []string {
	values := make([]string, 0, len(d.drivers))
	for _, driver := range d.drivers {
		total := sets.New[structured.DeviceID]()
		reported := false
		for member := range members {
			if devices, exists := d.devices[driver][member]; exists {
				total.Insert(devices.UnsortedList()...)
				reported = true
			}
		}
		value := "-"
		if reported {
			value = strconv.Itoa(total.Len())
		}
		values = append(values, value)
	}
	return values
}
