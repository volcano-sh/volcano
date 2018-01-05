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
	"fmt"
	"math"

	"k8s.io/api/core/v1"
)

type Resource struct {
	MilliCPU float64
	Memory   float64
}

func EmptyResource() *Resource {
	return &Resource{
		MilliCPU: 0,
		Memory:   0,
	}
}

func (r *Resource) Clone() *Resource {
	clone := &Resource{
		MilliCPU: r.MilliCPU,
		Memory:   r.Memory,
	}
	return clone
}

var minMilliCPU float64 = 10
var minMemory float64 = 10 * 1024 * 1024

func NewResource(rl v1.ResourceList) *Resource {
	r := EmptyResource()
	for rName, rQuant := range rl {
		switch rName {
		case v1.ResourceCPU:
			r.MilliCPU += float64(rQuant.MilliValue())
		case v1.ResourceMemory:
			r.Memory += float64(rQuant.Value())
		}
	}
	return r
}

func (r *Resource) IsEmpty() bool {
	return r.MilliCPU < minMilliCPU && r.Memory < minMemory
}

func (r *Resource) IsZero(rn v1.ResourceName) bool {
	switch rn {
	case v1.ResourceCPU:
		return r.MilliCPU < minMilliCPU
	case v1.ResourceMemory:
		return r.Memory < minMemory
	default:
		panic("unknown resource")
	}
}

func (r *Resource) Add(rr *Resource) *Resource {
	r.MilliCPU += rr.MilliCPU
	r.Memory += rr.Memory
	return r
}

//A function to Subtract two Resource objects.
func (r *Resource) Sub(rr *Resource) *Resource {
	if r.Less(rr) == false {
		r.MilliCPU -= rr.MilliCPU
		r.Memory -= rr.Memory
		return r
	}
	panic("Resource is not sufficient to do operation: Sub()")
}

func (r *Resource) Less(rr *Resource) bool {
	return r.MilliCPU < rr.MilliCPU && r.Memory < rr.Memory
}

func (r *Resource) LessEqual(rr *Resource) bool {
	return (r.MilliCPU < rr.MilliCPU || math.Abs(rr.MilliCPU-r.MilliCPU) < 0.01) &&
		(r.Memory < rr.Memory || math.Abs(rr.Memory-r.Memory) < 1)
}

func (r *Resource) String() string {
	return fmt.Sprintf("cpu %0.2f, memory %0.2f", r.MilliCPU, r.Memory)
}

func (r *Resource) Get(rn v1.ResourceName) float64 {
	switch rn {
	case v1.ResourceCPU:
		return r.MilliCPU
	case v1.ResourceMemory:
		return r.Memory
	default:
		panic("not support resource.")
	}
}

func ResourceNames() []v1.ResourceName {
	return []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory}
}
