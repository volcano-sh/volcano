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
	"fmt"
	"log"
	"math"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type Resource struct {
	MilliCPU float64
}

func Decorator(fn func(r *Resource)) func(r *Resource) {
	return func(r *Resource) {
		log.Println("starting")
		fn(r)
		log.Println("completed")
	}
}

func EmptyResource() *Resource {
	return &Resource{
		MilliCPU: 0,
	}
}

func (r *Resource) Clone() *Resource {
	clone := &Resource{
		MilliCPU: r.MilliCPU,
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
		}
	}
	return r
}

func (r *Resource) ResourceList() v1.ResourceList {
	rl := make(v1.ResourceList)

	rl[v1.ResourceCPU] = *resource.NewQuantity(int64(r.MilliCPU), resource.DecimalSI)

	return rl
}

func (r *Resource) IsEmpty() bool {
	return r.MilliCPU < minMilliCPU
}

func (r *Resource) Add(rr *Resource) *Resource {
	r.MilliCPU += rr.MilliCPU
	return r
}

//A function to Subtract two Resource objects.
func (r *Resource) Sub(rr *Resource) *Resource {
	if r.Less(rr) == false {
		r.MilliCPU -= rr.MilliCPU
		return r
	}
	panic("Resource is not sufficient to do operation: Sub()")
}

func (r *Resource) Less(rr *Resource) bool {
	return r.MilliCPU < rr.MilliCPU
}

func (r *Resource) Equal(rr *Resource) bool {
	return math.Abs(r.MilliCPU-rr.MilliCPU) < 0.01
}

func (r *Resource) LessEqual(rr *Resource) bool {
	return (r.MilliCPU < rr.MilliCPU || math.Abs(rr.MilliCPU-r.MilliCPU) < 0.01)
}

func (r *Resource) String() string {
	return fmt.Sprintf("cpu %f", r.MilliCPU)
}

func (r *Resource) Multiply(fra float64) *Resource {
	r.MilliCPU *= fra

	return r
}
