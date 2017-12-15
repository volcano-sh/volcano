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

package schedulercache

import (
	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type QueueInfo struct {
	name  string
	queue *apiv1.Queue
	Pods  []*v1.Pod
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

func (r *QueueInfo) Name() string {
	return r.name
}

func (r *QueueInfo) Queue() *apiv1.Queue {
	return r.queue
}

func (r *QueueInfo) UsedUnderAllocated() bool {
	return (CompareResources(r.queue.Status.Used.Resources, r.queue.Status.Allocated.Resources) <= 0)
}

func (r *QueueInfo) UsedUnderDeserved() bool {
	return (CompareResources(r.queue.Status.Used.Resources, r.queue.Status.Deserved.Resources) <= 0)
}

func (r *QueueInfo) Clone() *QueueInfo {
	clone := &QueueInfo{
		name:  r.name,
		queue: r.queue.DeepCopy(),
		Pods:  r.Pods,
	}
	return clone
}
