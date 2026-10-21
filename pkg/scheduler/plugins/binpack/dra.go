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

package binpack

import (
	"context"
	"fmt"
	"sync"

	resourcev1 "k8s.io/api/resource/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/tools/cache"
	"k8s.io/dynamic-resource-allocation/cel"
	"k8s.io/dynamic-resource-allocation/structured"
	fwk "k8s.io/kube-scheduler/framework"
	kubefeatures "k8s.io/kubernetes/pkg/features"

	"volcano.sh/volcano/pkg/scheduler/api"
)

const draCELCacheSize = 10

// draNodeResource contains count-level DRA resources for one DeviceClass on a node.
// Capacity dimensions are intentionally left for a follow-up.
type draNodeResource struct {
	Allocatable *api.DRAResource
	Used        *api.DRAResource
}

// draNodeResources maps node name to DeviceClass name to count-level resources.
type draNodeResources map[string]map[string]*draNodeResource

type draInventoryDevice struct {
	nodeName string
	driver   string
	id       structured.DeviceID
	device   *resourcev1.Device
}

type draClassInventory struct {
	devicesByNode map[string][]structured.DeviceID
	err           error
}

// draInventory is the session-scoped, task-independent view of node-local DRA
// devices. DeviceClass matching is populated lazily and reused by all tasks in
// the session. The matching cache is rebuilt when ResourceSlices or
// DeviceClasses change, while the CEL compilation cache remains session-scoped.
type draInventory struct {
	mutex          sync.Mutex
	celCache       *cel.Cache
	deviceClasses  map[string]*resourcev1.DeviceClass
	resourceSlices map[string]*resourcev1.ResourceSlice
	devices        []draInventoryDevice
	classMatches   map[string]*draClassInventory
}

func newDRAInventory() *draInventory {
	return &draInventory{
		celCache: cel.NewCache(draCELCacheSize, cel.Features{
			EnableConsumableCapacity: utilfeature.DefaultFeatureGate.Enabled(kubefeatures.DRAConsumableCapacity),
			EnableListTypeAttributes: utilfeature.DefaultFeatureGate.Enabled(kubefeatures.DRAListTypeAttributes),
		}),
		deviceClasses:  make(map[string]*resourcev1.DeviceClass),
		resourceSlices: make(map[string]*resourcev1.ResourceSlice),
		classMatches:   make(map[string]*draClassInventory),
	}
}

func newDRAInventoryFromSnapshot(
	deviceClasses []*resourcev1.DeviceClass,
	resourceSlices []*resourcev1.ResourceSlice,
) *draInventory {
	inventory := newDRAInventory()
	inventory.refreshFromSnapshot(deviceClasses, resourceSlices)
	return inventory
}

func (i *draInventory) refresh(manager fwk.SharedDRAManager) error {
	deviceClasses, err := manager.DeviceClasses().List()
	if err != nil {
		return fmt.Errorf("list DeviceClasses: %w", err)
	}
	resourceSlices, err := manager.ResourceSlices().ListWithDeviceTaintRules()
	if err != nil {
		return fmt.Errorf("list ResourceSlices: %w", err)
	}

	i.refreshFromSnapshot(deviceClasses, resourceSlices)
	return nil
}

func (i *draInventory) refreshFromSnapshot(
	deviceClasses []*resourcev1.DeviceClass,
	resourceSlices []*resourcev1.ResourceSlice,
) {
	latestDeviceClasses := make(map[string]*resourcev1.DeviceClass, len(deviceClasses))
	for _, deviceClass := range deviceClasses {
		if deviceClass != nil {
			latestDeviceClasses[deviceClass.Name] = deviceClass
		}
	}
	latestResourceSlices := make(map[string]*resourcev1.ResourceSlice, len(resourceSlices))
	for _, slice := range resourceSlices {
		if slice != nil {
			latestResourceSlices[slice.Name] = slice
		}
	}

	i.mutex.Lock()
	defer i.mutex.Unlock()
	if apiequality.Semantic.DeepEqual(i.deviceClasses, latestDeviceClasses) &&
		apiequality.Semantic.DeepEqual(i.resourceSlices, latestResourceSlices) {
		return
	}

	i.deviceClasses = make(map[string]*resourcev1.DeviceClass, len(latestDeviceClasses))
	for name, deviceClass := range latestDeviceClasses {
		i.deviceClasses[name] = deviceClass.DeepCopy()
	}
	i.resourceSlices = make(map[string]*resourcev1.ResourceSlice, len(latestResourceSlices))
	for name, slice := range latestResourceSlices {
		i.resourceSlices[name] = slice.DeepCopy()
	}
	i.devices = nil
	i.classMatches = make(map[string]*draClassInventory)

	for _, slice := range i.resourceSlices {
		perDeviceNodeSelection := slice.Spec.PerDeviceNodeSelection != nil && *slice.Spec.PerDeviceNodeSelection
		for deviceIndex := range slice.Spec.Devices {
			device := &slice.Spec.Devices[deviceIndex]
			nodeName := exactDeviceNodeName(slice, device, perDeviceNodeSelection)
			if nodeName == "" {
				continue
			}
			i.devices = append(i.devices, draInventoryDevice{
				nodeName: nodeName,
				driver:   slice.Spec.Driver,
				id:       structured.MakeDeviceID(slice.Spec.Driver, slice.Spec.Pool.Name, device.Name),
				device:   device,
			})
		}
	}
}

func calculateDRAScoringState(
	ctx context.Context,
	manager fwk.SharedDRAManager,
	inventory *draInventory,
	task *api.TaskInfo,
) (draNodeResources, map[string]*api.DRAResource, error) {
	if manager == nil {
		return nil, nil, fmt.Errorf("DRA manager is not initialized")
	}
	if inventory == nil {
		return nil, nil, fmt.Errorf("DRA inventory is not initialized")
	}

	claimTracker := manager.ResourceClaims()
	allocatedClaims, err := allocatedTaskDRAClaims(task, claimTracker)
	if err != nil {
		return nil, nil, err
	}
	draRequest := incrementalTaskDRA(task, allocatedClaims)
	requestedClasses := requestedDRADeviceClasses(draRequest)
	if requestedClasses.Len() == 0 {
		return nil, draRequest, nil
	}
	if err := inventory.refresh(manager); err != nil {
		return nil, nil, err
	}

	allocatedState, err := claimTracker.GatherAllocatedState()
	if err != nil {
		return nil, nil, fmt.Errorf("gather allocated DRA state: %w", err)
	}
	resources, err := inventory.nodeResources(ctx, requestedClasses, allocatedState)
	if err != nil {
		return nil, nil, err
	}
	return resources, draRequest, nil
}

func allocatedTaskDRAClaims(task *api.TaskInfo, tracker fwk.ResourceClaimTracker) (sets.Set[string], error) {
	allocatedClaims := sets.New[string]()
	if task == nil || len(task.ResourceClaimDRAResreq) == 0 {
		return allocatedClaims, nil
	}
	if tracker == nil {
		return nil, fmt.Errorf("DRA ResourceClaim tracker is not initialized")
	}

	for _, claimKey := range task.ResourceClaimKeys {
		namespace, name, err := cache.SplitMetaNamespaceKey(claimKey)
		if err != nil {
			return nil, fmt.Errorf("split ResourceClaim key %q: %w", claimKey, err)
		}
		claim, err := tracker.Get(namespace, name)
		if err != nil {
			return nil, fmt.Errorf("get ResourceClaim %s: %w", claimKey, err)
		}
		if claim.Status.Allocation != nil || tracker.GetPendingAllocation(claim.UID) != nil {
			allocatedClaims.Insert(claimKey)
		}
	}
	return allocatedClaims, nil
}

// incrementalTaskDRA returns only claims which still need an allocation. An already
// allocated shared claim is part of the node's used count and must not be counted again.
func incrementalTaskDRA(task *api.TaskInfo, allocatedClaims sets.Set[string]) map[string]*api.DRAResource {
	if task == nil {
		return nil
	}
	if len(task.ResourceClaimDRAResreq) == 0 {
		return task.DRAResreq
	}

	result := make(map[string]*api.DRAResource)
	for _, claimKey := range task.ResourceClaimKeys {
		if allocatedClaims.Has(claimKey) {
			continue
		}
		for deviceClass, request := range task.ResourceClaimDRAResreq[claimKey] {
			if request == nil {
				continue
			}
			if result[deviceClass] == nil {
				result[deviceClass] = &api.DRAResource{}
			}
			result[deviceClass].Add(request)
		}
	}
	return result
}

func requestedDRADeviceClasses(request map[string]*api.DRAResource) sets.Set[string] {
	result := sets.New[string]()
	for deviceClass, resource := range request {
		if resource != nil && resource.Count > 0 {
			result.Insert(deviceClass)
		}
	}
	return result
}

func (i *draInventory) nodeResources(
	ctx context.Context,
	deviceClasses sets.Set[string],
	allocatedState *structured.AllocatedState,
) (draNodeResources, error) {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	if allocatedState == nil {
		allocatedState = &structured.AllocatedState{
			AllocatedDevices:         sets.New[structured.DeviceID](),
			AllocatedSharedDeviceIDs: sets.New[structured.SharedDeviceID](),
		}
	}

	result := make(draNodeResources)
	for deviceClass := range deviceClasses {
		devicesByNode, err := i.matchingDevicesLocked(ctx, deviceClass)
		if err != nil {
			return nil, err
		}
		for nodeName, deviceIDs := range devicesByNode {
			resource := ensureDRANodeResource(result, nodeName, deviceClass)
			resource.Allocatable.Count = int64(len(deviceIDs))
			for _, deviceID := range deviceIDs {
				if structured.IsDeviceAllocated(deviceID, allocatedState) {
					resource.Used.Count++
				}
			}
		}
	}

	return result, nil
}

func (i *draInventory) matchingDevicesLocked(ctx context.Context, deviceClassName string) (map[string][]structured.DeviceID, error) {
	if cached, ok := i.classMatches[deviceClassName]; ok {
		return cached.devicesByNode, cached.err
	}

	devicesByNode := make(map[string][]structured.DeviceID)
	deviceClass := i.deviceClasses[deviceClassName]
	if deviceClass == nil {
		i.classMatches[deviceClassName] = &draClassInventory{devicesByNode: devicesByNode}
		return devicesByNode, nil
	}

	for _, device := range i.devices {
		matches, err := deviceMatchesClass(ctx, i.celCache, device.driver, device.device, deviceClass)
		if err != nil {
			err = fmt.Errorf("match device %s from driver %s against DeviceClass %s: %w",
				device.device.Name, device.driver, deviceClassName, err)
			i.classMatches[deviceClassName] = &draClassInventory{err: err}
			return nil, err
		}
		if matches {
			devicesByNode[device.nodeName] = append(devicesByNode[device.nodeName], device.id)
		}
	}

	i.classMatches[deviceClassName] = &draClassInventory{devicesByNode: devicesByNode}
	return devicesByNode, nil
}

func exactDeviceNodeName(slice *resourcev1.ResourceSlice, device *resourcev1.Device, perDeviceNodeSelection bool) string {
	if perDeviceNodeSelection {
		if device.NodeName == nil {
			return ""
		}
		return *device.NodeName
	}
	if slice.Spec.NodeName == nil {
		return ""
	}
	return *slice.Spec.NodeName
}

func deviceMatchesClass(
	ctx context.Context,
	celCache *cel.Cache,
	driver string,
	device *resourcev1.Device,
	deviceClass *resourcev1.DeviceClass,
) (bool, error) {
	for _, selector := range deviceClass.Spec.Selectors {
		if selector.CEL == nil {
			continue
		}
		compiled := celCache.GetOrCompile(selector.CEL.Expression)
		if compiled.Error != nil {
			return false, compiled.Error
		}
		matches, _, err := compiled.DeviceMatches(ctx, cel.Device{
			Driver:                   driver,
			AllowMultipleAllocations: device.AllowMultipleAllocations,
			Attributes:               device.Attributes,
			Capacity:                 device.Capacity,
		})
		if err != nil || !matches {
			return false, err
		}
	}
	return true, nil
}

func ensureDRANodeResource(resources draNodeResources, nodeName, deviceClassName string) *draNodeResource {
	if resources[nodeName] == nil {
		resources[nodeName] = make(map[string]*draNodeResource)
	}
	if resources[nodeName][deviceClassName] == nil {
		resources[nodeName][deviceClassName] = &draNodeResource{
			Allocatable: &api.DRAResource{},
			Used:        &api.DRAResource{},
		}
	}
	return resources[nodeName][deviceClassName]
}
