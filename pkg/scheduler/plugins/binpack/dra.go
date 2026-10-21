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

	resourcev1 "k8s.io/api/resource/v1"
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

// calculateDRANodeResources builds the node-local DRA resource view used by binpack scoring.
// Only ResourceSlices and devices with an exact NodeName are included. Resources available
// through AllNodes or NodeSelector are excluded because attributing them to every matching
// node would double-count shared devices.
func calculateDRAScoringState(
	ctx context.Context,
	manager fwk.SharedDRAManager,
	task *api.TaskInfo,
) (draNodeResources, map[string]*api.DRAResource, error) {
	if manager == nil {
		return nil, nil, fmt.Errorf("DRA manager is not initialized")
	}

	resources, err := calculateDRANodeResources(ctx, manager)
	if err != nil {
		return nil, nil, err
	}
	allocatedClaims, err := allocatedTaskDRAClaims(task, manager.ResourceClaims())
	if err != nil {
		return nil, nil, err
	}
	return resources, incrementalTaskDRA(task, allocatedClaims), nil
}

func calculateDRANodeResources(ctx context.Context, manager fwk.SharedDRAManager) (draNodeResources, error) {
	if manager == nil {
		return nil, fmt.Errorf("DRA manager is not initialized")
	}

	deviceClasses, err := manager.DeviceClasses().List()
	if err != nil {
		return nil, fmt.Errorf("list DeviceClasses: %w", err)
	}
	resourceSlices, err := manager.ResourceSlices().ListWithDeviceTaintRules()
	if err != nil {
		return nil, fmt.Errorf("list ResourceSlices: %w", err)
	}
	claimTracker := manager.ResourceClaims()
	allocatedState, err := claimTracker.GatherAllocatedState()
	if err != nil {
		return nil, fmt.Errorf("gather allocated DRA state: %w", err)
	}

	resources, err := aggregateDRANodeResources(ctx, deviceClasses, resourceSlices, allocatedState)
	if err != nil {
		return nil, err
	}

	return resources, nil
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

func aggregateDRANodeResources(
	ctx context.Context,
	deviceClasses []*resourcev1.DeviceClass,
	resourceSlices []*resourcev1.ResourceSlice,
	allocatedState *structured.AllocatedState,
) (draNodeResources, error) {
	if allocatedState == nil {
		allocatedState = &structured.AllocatedState{
			AllocatedDevices:         sets.New[structured.DeviceID](),
			AllocatedSharedDeviceIDs: sets.New[structured.SharedDeviceID](),
		}
	}

	celCache := cel.NewCache(draCELCacheSize, cel.Features{
		EnableConsumableCapacity: utilfeature.DefaultFeatureGate.Enabled(kubefeatures.DRAConsumableCapacity),
		EnableListTypeAttributes: utilfeature.DefaultFeatureGate.Enabled(kubefeatures.DRAListTypeAttributes),
	})
	result := make(draNodeResources)

	for _, slice := range resourceSlices {
		if slice == nil {
			continue
		}

		perDeviceNodeSelection := slice.Spec.PerDeviceNodeSelection != nil && *slice.Spec.PerDeviceNodeSelection
		for i := range slice.Spec.Devices {
			device := &slice.Spec.Devices[i]
			nodeName := exactDeviceNodeName(slice, device, perDeviceNodeSelection)
			if nodeName == "" {
				continue
			}

			deviceID := structured.MakeDeviceID(slice.Spec.Driver, slice.Spec.Pool.Name, device.Name)
			allocated := structured.IsDeviceAllocated(deviceID, allocatedState)
			for _, deviceClass := range deviceClasses {
				if deviceClass == nil {
					continue
				}
				matches, err := deviceMatchesClass(ctx, celCache, slice.Spec.Driver, device, deviceClass)
				if err != nil {
					return nil, fmt.Errorf("match device %s from driver %s against DeviceClass %s: %w",
						device.Name, slice.Spec.Driver, deviceClass.Name, err)
				}
				if !matches {
					continue
				}

				resource := ensureDRANodeResource(result, nodeName, deviceClass.Name)
				resource.Allocatable.Count++
				if allocated {
					resource.Used.Count++
				}
			}
		}
	}

	return result, nil
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
