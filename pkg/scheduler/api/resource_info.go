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

package api

import (
	"fmt"
	"math"

	"k8s.io/api/core/v1"
)

type Resource struct {
	MilliCPU float64
	Memory   float64
	GPU      int64
}

const (
	// need to follow https://github.com/NVIDIA/k8s-device-plugin/blob/66a35b71ac4b5cbfb04714678b548bd77e5ba719/server.go#L20
	GPUResourceName = "nvidia.com/gpu"
)

func EmptyResource() *Resource {
	return &Resource{
		MilliCPU: 0,
		Memory:   0,
		GPU:      0,
	}
}

func (r *Resource) Clone() *Resource {
	clone := &Resource{
		MilliCPU: r.MilliCPU,
		Memory:   r.Memory,
		GPU:      r.GPU,
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
		case GPUResourceName:
			q, _ := rQuant.AsInt64()
			r.GPU += q
		}
	}
	return r
}

func (r *Resource) IsEmpty() bool {
	return r.MilliCPU < minMilliCPU && r.Memory < minMemory && r.GPU == 0
}

func (r *Resource) IsZero(rn v1.ResourceName) bool {
	switch rn {
	case v1.ResourceCPU:
		return r.MilliCPU < minMilliCPU
	case v1.ResourceMemory:
		return r.Memory < minMemory
	case GPUResourceName:
		return r.GPU == 0
	default:
		panic("unknown resource")
	}
}

func (r *Resource) Add(rr *Resource) *Resource {
	r.MilliCPU += rr.MilliCPU
	r.Memory += rr.Memory
	r.GPU += rr.GPU
	return r
}

//Sub subtracts two Resource objects.
func (r *Resource) Sub(rr *Resource) *Resource {
	if rr.LessEqual(r) {
		r.MilliCPU -= rr.MilliCPU
		r.Memory -= rr.Memory
		r.GPU -= rr.GPU
		return r
	}

	panic(fmt.Errorf("Resource is not sufficient to do operation: <%v> sub <%v>",
		r, rr))
}

func (r *Resource) Less(rr *Resource) bool {
	return r.MilliCPU < rr.MilliCPU && r.Memory < rr.Memory && r.GPU < rr.GPU
}

func (r *Resource) LessEqual(rr *Resource) bool {
	return (r.MilliCPU < rr.MilliCPU || math.Abs(rr.MilliCPU-r.MilliCPU) < 0.01) &&
		(r.Memory < rr.Memory || math.Abs(rr.Memory-r.Memory) < 1) &&
		(r.GPU <= rr.GPU)
}

func (r *Resource) String() string {
	return fmt.Sprintf("cpu %0.2f, memory %0.2f, GPU %d",
		r.MilliCPU, r.Memory, r.GPU)
}

func (r *Resource) Get(rn v1.ResourceName) float64 {
	switch rn {
	case v1.ResourceCPU:
		return r.MilliCPU
	case v1.ResourceMemory:
		return r.Memory
	case GPUResourceName:
		return float64(r.GPU)
	default:
		panic("not support resource.")
	}
}

func ResourceNames() []v1.ResourceName {
	return []v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory, GPUResourceName}
}
