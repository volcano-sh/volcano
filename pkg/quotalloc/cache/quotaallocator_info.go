/*
Copyright 2017 The Kubernetes Authors.

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
	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/apis/v1"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type QuotaAllocatorInfo struct {
	name           string
	quotaAllocator *apiv1.QuotaAllocator
	Pods           []*v1.Pod
}

// -1  - if res1 < res2
// 0   - if res1 = res2
// 1   - if not belong above cases
func CompareResources(res1 map[apiv1.ResourceName]resource.Quantity, res2 map[apiv1.ResourceName]resource.Quantity) int {
	cpu1 := res1["cpu"].DeepCopy()
	cpu2 := res2["cpu"].DeepCopy()
	memory1 := res1["memory"].DeepCopy()
	memory2 := res2["memory"].DeepCopy()

	if cpu1.Cmp(cpu2) <= 0 && memory1.Cmp(memory2) <= 0 {
		if cpu1.Cmp(cpu2) == 0 && memory1.Cmp(memory2) == 0 {
			return 0
		} else {
			return -1
		}
	} else {
		return 1
	}
}

func (r *QuotaAllocatorInfo) Name() string {
	return r.name
}

func (r *QuotaAllocatorInfo) QuotaAllocator() *apiv1.QuotaAllocator {
	return r.quotaAllocator
}

func (r *QuotaAllocatorInfo) UsedUnderAllocated() bool {
	return (CompareResources(r.quotaAllocator.Status.Used.Resources, r.quotaAllocator.Status.Allocated.Resources) <= 0)
}

func (r *QuotaAllocatorInfo) UsedUnderDeserved() bool {
	return (CompareResources(r.quotaAllocator.Status.Used.Resources, r.quotaAllocator.Status.Deserved.Resources) <= 0)
}

func (r *QuotaAllocatorInfo) Clone() *QuotaAllocatorInfo {
	clone := &QuotaAllocatorInfo{
		name:           r.name,
		quotaAllocator: r.quotaAllocator.DeepCopy(),
		Pods:           r.Pods,
	}
	return clone
}
