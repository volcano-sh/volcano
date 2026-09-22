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

type queueDRAAllocation struct {
	// unattributed contains DRA resources that cannot be associated with an
	// individual ResourceClaim and therefore cannot be deduplicated.
	unattributed map[string]*api.DRAResource
	// claims contains DRA resources keyed by ResourceClaim. Keeping the claim
	// identity allows ancestors to deduplicate claims shared by descendants.
	claims map[string]map[string]*api.DRAResource
}

func newQueueDRAAllocation() *queueDRAAllocation {
	return &queueDRAAllocation{}
}

func (allocated *queueDRAAllocation) addTask(task *api.TaskInfo) {
	if task == nil {
		return
	}

	if len(task.ResourceClaimDRAResreq) == 0 {
		if len(task.DRAResreq) == 0 {
			return
		}
		if allocated.unattributed == nil {
			allocated.unattributed = make(map[string]*api.DRAResource)
		}
		mergeDRAResourceMap(allocated.unattributed, task.DRAResreq)
		return
	}

	for _, claimKey := range task.ResourceClaimKeys {
		if _, found := allocated.claims[claimKey]; found {
			continue
		}
		claimReq := task.ResourceClaimDRAResreq[claimKey]
		if len(claimReq) == 0 {
			continue
		}
		if allocated.claims == nil {
			allocated.claims = make(map[string]map[string]*api.DRAResource)
		}
		allocated.claims[claimKey] = claimReq
	}
}

func (allocated *queueDRAAllocation) addChild(child *queueDRAAllocation) {
	if len(child.unattributed) > 0 {
		if allocated.unattributed == nil {
			allocated.unattributed = make(map[string]*api.DRAResource)
		}
		mergeDRAResourceMap(allocated.unattributed, child.unattributed)
	}
	if len(child.claims) > 0 && allocated.claims == nil {
		allocated.claims = make(map[string]map[string]*api.DRAResource)
	}
	for claimKey, claimReq := range child.claims {
		if _, found := allocated.claims[claimKey]; !found {
			allocated.claims[claimKey] = claimReq
		}
	}
}

func (allocated *queueDRAAllocation) empty() bool {
	return allocated == nil || len(allocated.unattributed) == 0 && len(allocated.claims) == 0
}

func (allocated *queueDRAAllocation) total() map[string]*api.DRAResource {
	if allocated.empty() {
		return nil
	}
	total := make(map[string]*api.DRAResource)
	mergeDRAResourceMap(total, allocated.unattributed)
	for _, claimReq := range allocated.claims {
		mergeDRAResourceMap(total, claimReq)
	}
	return total
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
