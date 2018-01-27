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

	"k8s.io/apimachinery/pkg/api/resource"
)

// TODO some operation for map[apiv1.ResourceName]resource.Quantity, it is better to enhance it in resource_info.go

func ResourcesAdd(res1 map[apiv1.ResourceName]resource.Quantity, res2 map[apiv1.ResourceName]resource.Quantity) map[apiv1.ResourceName]resource.Quantity {
	cpu1 := res1["cpu"].DeepCopy()
	cpu2 := res2["cpu"].DeepCopy()
	mem1 := res1["memory"].DeepCopy()
	mem2 := res2["memory"].DeepCopy()

	cpu1.Add(cpu2)
	mem1.Add(mem2)

	return map[apiv1.ResourceName]resource.Quantity{
		"cpu":    cpu1,
		"memory": mem1,
	}
}

func ResourcesSub(res1 map[apiv1.ResourceName]resource.Quantity, res2 map[apiv1.ResourceName]resource.Quantity) map[apiv1.ResourceName]resource.Quantity {
	cpu1 := res1["cpu"].DeepCopy()
	cpu2 := res2["cpu"].DeepCopy()
	mem1 := res1["memory"].DeepCopy()
	mem2 := res2["memory"].DeepCopy()

	if cpu1.Cmp(cpu2) <= 0 {
		cpu1 = resource.MustParse("0")
	} else {
		cpu1.Sub(cpu2)
	}
	if mem1.Cmp(mem2) <= 0 {
		mem1 = resource.MustParse("0")
	} else {
		mem1.Sub(mem2)
	}

	return map[apiv1.ResourceName]resource.Quantity{
		"cpu":    cpu1,
		"memory": mem1,
	}
}

func ResourcesIsZero(res map[apiv1.ResourceName]resource.Quantity) bool {
	cpu := res["cpu"].DeepCopy()
	mem := res["memory"].DeepCopy()

	if cpu.IsZero() && mem.IsZero() {
		return true
	}

	return false
}

func ResourcesMultiply(res map[apiv1.ResourceName]resource.Quantity, count int) map[apiv1.ResourceName]resource.Quantity {
	cpu := res["cpu"].DeepCopy()
	mem := res["memory"].DeepCopy()

	cpuInt64, _ := (&cpu).AsInt64()
	memoryInt64, _ := (&mem).AsInt64()

	return map[apiv1.ResourceName]resource.Quantity{
		"cpu":    *resource.NewQuantity(cpuInt64*int64(count), resource.DecimalSI),
		"memory": *resource.NewQuantity(memoryInt64*int64(count), resource.BinarySI),
	}
}
