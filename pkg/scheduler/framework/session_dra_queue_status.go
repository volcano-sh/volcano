/*
Copyright 2025 The Volcano Authors.

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

package framework

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"volcano.sh/volcano/pkg/scheduler/api"
)

const (
	queueDRADeviceClassCountPrefix = "deviceclass/"
	queueDRADeviceClassCapacitySep = ".deviceclass/"
)

func addTaskDRAAllocatedByQueue(
	allocated map[api.QueueID]map[string]*api.DRAResource,
	resourceClaimRefs map[api.QueueID]map[string]int,
	queueID api.QueueID,
	task *api.TaskInfo,
) {
	if task == nil {
		return
	}

	if allocated[queueID] == nil {
		allocated[queueID] = make(map[string]*api.DRAResource)
	}
	if resourceClaimRefs[queueID] == nil {
		resourceClaimRefs[queueID] = make(map[string]int)
	}

	if len(task.ResourceClaimDRAResreq) == 0 {
		mergeDRAResourceMap(allocated[queueID], task.DRAResreq)
		return
	}

	for _, claimKey := range task.ResourceClaimKeys {
		claimReq := task.ResourceClaimDRAResreq[claimKey]
		if len(claimReq) == 0 {
			continue
		}
		if resourceClaimRefs[queueID][claimKey] == 0 {
			mergeDRAResourceMap(allocated[queueID], claimReq)
		}
		resourceClaimRefs[queueID][claimKey]++
	}
}

func mergeDRAResourceMap(dst map[string]*api.DRAResource, src map[string]*api.DRAResource) {
	if len(src) == 0 {
		return
	}

	for deviceClass, res := range src {
		if res == nil {
			continue
		}
		if dst[deviceClass] == nil {
			dst[deviceClass] = &api.DRAResource{
				Capacity: make(map[string]resource.Quantity),
			}
		}
		dst[deviceClass].Add(res)
	}
}

func mergeDRAAllocatedIntoResourceList(
	allocated v1.ResourceList,
	draAllocated map[string]*api.DRAResource,
) v1.ResourceList {
	if allocated == nil {
		allocated = make(v1.ResourceList)
	}

	for deviceClass, res := range draAllocated {
		if res == nil {
			continue
		}

		if res.Count > 0 {
			allocated[v1.ResourceName(queueDRADeviceClassCountPrefix+deviceClass)] = *resource.NewQuantity(res.Count, resource.DecimalSI)
		}

		for dim, qty := range res.Capacity {
			if qty.IsZero() {
				continue
			}
			allocated[v1.ResourceName(dim+queueDRADeviceClassCapacitySep+deviceClass)] = qty.DeepCopy()
		}
	}

	return allocated
}
