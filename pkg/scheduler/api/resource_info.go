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

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	v1helper "k8s.io/kubernetes/pkg/apis/core/v1/helper"

	"volcano.sh/volcano/pkg/scheduler/util/assert"
)

const (
	// GPUResourceName need to follow https://github.com/NVIDIA/k8s-device-plugin/blob/66a35b71ac4b5cbfb04714678b548bd77e5ba719/server.go#L20
	GPUResourceName = "nvidia.com/gpu"
)

const (
	minResource float64 = 0.1
)

// DimensionDefaultValue means default value for black resource dimension
type DimensionDefaultValue int

const (
	// Zero means resource dimension not defined will be treated as zero
	Zero DimensionDefaultValue = 0
	// Infinity means resource dimension not defined will be treated as infinity
	Infinity DimensionDefaultValue = -1
)

// Resource struct defines all the resource type
type Resource struct {
	MilliCPU float64
	Memory   float64

	// ScalarResources
	ScalarResources map[v1.ResourceName]float64

	// MaxTaskNum is only used by predicates; it should NOT
	// be accounted in other operators, e.g. Add.
	MaxTaskNum int
}

// EmptyResource creates a empty resource object and returns
func EmptyResource() *Resource {
	return &Resource{}
}

// NewResource creates a new resource object from resource list
func NewResource(rl v1.ResourceList) *Resource {
	r := EmptyResource()
	for rName, rQuant := range rl {
		switch rName {
		case v1.ResourceCPU:
			r.MilliCPU += float64(rQuant.MilliValue())
		case v1.ResourceMemory:
			r.Memory += float64(rQuant.Value())
		case v1.ResourcePods:
			r.MaxTaskNum += int(rQuant.Value())
		default:
			//NOTE: When converting this back to k8s resource, we need record the format as well as / 1000
			if v1helper.IsScalarResourceName(rName) {
				r.AddScalar(rName, float64(rQuant.MilliValue()))
			}
		}
	}
	return r
}

// ResFloat642Quantity transform resource quantity
func ResFloat642Quantity(resName v1.ResourceName, quantity float64) resource.Quantity {
	var resQuantity *resource.Quantity
	switch resName {
	case v1.ResourceCPU:
		resQuantity = resource.NewMilliQuantity(int64(quantity), resource.DecimalSI)
	default:
		resQuantity = resource.NewQuantity(int64(quantity), resource.BinarySI)
	}

	return *resQuantity
}

// ResQuantity2Float64 transform resource quantity
func ResQuantity2Float64(resName v1.ResourceName, quantity resource.Quantity) float64 {
	var resQuantity float64
	switch resName {
	case v1.ResourceCPU:
		resQuantity = float64(quantity.MilliValue())
	default:
		resQuantity = float64(quantity.Value())
	}

	return resQuantity
}

// Clone is used to clone a resource type, which is a deep copy function.
func (r *Resource) Clone() *Resource {
	clone := &Resource{
		MilliCPU:   r.MilliCPU,
		Memory:     r.Memory,
		MaxTaskNum: r.MaxTaskNum,
	}

	if r.ScalarResources != nil {
		clone.ScalarResources = make(map[v1.ResourceName]float64)
		for k, v := range r.ScalarResources {
			clone.ScalarResources[k] = v
		}
	}

	return clone
}

// String returns resource details in string format
func (r *Resource) String() string {
	str := fmt.Sprintf("cpu %0.2f, memory %0.2f", r.MilliCPU, r.Memory)
	for rName, rQuant := range r.ScalarResources {
		str = fmt.Sprintf("%s, %s %0.2f", str, rName, rQuant)
	}
	return str
}

// ResourceNames returns all resource types
func (r *Resource) ResourceNames() ResourceNameList {
	resNames := ResourceNameList{}

	if r.MilliCPU >= minResource {
		resNames = append(resNames, v1.ResourceCPU)
	}

	if r.Memory >= minResource {
		resNames = append(resNames, v1.ResourceMemory)
	}

	for rName, rMount := range r.ScalarResources {
		if rMount >= minResource {
			resNames = append(resNames, rName)
		}
	}

	return resNames
}

// Get returns the resource value for that particular resource type
func (r *Resource) Get(rn v1.ResourceName) float64 {
	switch rn {
	case v1.ResourceCPU:
		return r.MilliCPU
	case v1.ResourceMemory:
		return r.Memory
	default:
		if r.ScalarResources == nil {
			return 0
		}
		return r.ScalarResources[rn]
	}
}

// IsEmpty returns false if any kind of resource is not less than min value, otherwise returns true
func (r *Resource) IsEmpty() bool {
	if !(r.MilliCPU < minResource && r.Memory < minResource) {
		return false
	}

	for _, rQuant := range r.ScalarResources {
		if rQuant >= minResource {
			return false
		}
	}

	return true
}

// IsZero returns false if the given kind of resource is not less than min value
func (r *Resource) IsZero(rn v1.ResourceName) bool {
	switch rn {
	case v1.ResourceCPU:
		return r.MilliCPU < minResource
	case v1.ResourceMemory:
		return r.Memory < minResource
	default:
		if r.ScalarResources == nil {
			return true
		}

		_, found := r.ScalarResources[rn]
		assert.Assertf(found, "unknown resource %s", rn)

		return r.ScalarResources[rn] < minResource
	}
}

// Add is used to add two given resources
func (r *Resource) Add(rr *Resource) *Resource {
	r.MilliCPU += rr.MilliCPU
	r.Memory += rr.Memory

	for rName, rQuant := range rr.ScalarResources {
		if r.ScalarResources == nil {
			r.ScalarResources = map[v1.ResourceName]float64{}
		}
		r.ScalarResources[rName] += rQuant
	}

	return r
}

// Sub subtracts two Resource objects with assertion.
func (r *Resource) Sub(rr *Resource) *Resource {
	assert.Assertf(rr.LessEqual(r, Zero), "resource is not sufficient to do operation: <%v> sub <%v>", r, rr)
	return r.sub(rr)
}

// sub subtracts two Resource objects.
func (r *Resource) sub(rr *Resource) *Resource {
	r.MilliCPU -= rr.MilliCPU
	r.Memory -= rr.Memory

	if r.ScalarResources == nil {
		return r
	}
	for rrName, rrQuant := range rr.ScalarResources {
		r.ScalarResources[rrName] -= rrQuant
	}

	return r
}

// Multi multiples the resource with ratio provided
func (r *Resource) Multi(ratio float64) *Resource {
	r.MilliCPU *= ratio
	r.Memory *= ratio
	for rName, rQuant := range r.ScalarResources {
		r.ScalarResources[rName] = rQuant * ratio
	}
	return r
}

// SetMaxResource compares with ResourceList and takes max value for each Resource.
func (r *Resource) SetMaxResource(rr *Resource) {
	if r == nil || rr == nil {
		return
	}

	if rr.MilliCPU > r.MilliCPU {
		r.MilliCPU = rr.MilliCPU
	}
	if rr.Memory > r.Memory {
		r.Memory = rr.Memory
	}

	for rrName, rrQuant := range rr.ScalarResources {
		if r.ScalarResources == nil {
			r.ScalarResources = make(map[v1.ResourceName]float64)
			for k, v := range rr.ScalarResources {
				r.ScalarResources[k] = v
			}
			return
		}
		_, ok := r.ScalarResources[rrName]
		if !ok || rrQuant > r.ScalarResources[rrName] {
			r.ScalarResources[rrName] = rrQuant
		}
	}
}

//FitDelta Computes the delta between a resource object representing available
//resources an operand representing resources being requested.  Any
//field that is less than 0 after the operation represents an
//insufficient resource.
func (r *Resource) FitDelta(rr *Resource) *Resource {
	if rr.MilliCPU > 0 {
		r.MilliCPU -= rr.MilliCPU + minResource
	}

	if rr.Memory > 0 {
		r.Memory -= rr.Memory + minResource
	}

	if r.ScalarResources == nil {
		r.ScalarResources = make(map[v1.ResourceName]float64)
	}

	for rrName, rrQuant := range rr.ScalarResources {
		if rrQuant > 0 {
			_, ok := r.ScalarResources[rrName]
			if !ok {
				r.ScalarResources[rrName] = 0
			}
			r.ScalarResources[rrName] -= rrQuant + minResource
		}
	}

	return r
}

// Less returns true only on condition that all dimensions of resources in r are less than that of rr,
// Otherwise returns false.
// @param defaultValue "default value for resource dimension not defined in ScalarResources. Its value can only be one of 'Zero' and 'Infinity'"
func (r *Resource) Less(rr *Resource, defaultValue DimensionDefaultValue) bool {
	lessFunc := func(l, r float64) bool {
		return l < r
	}

	if !lessFunc(r.MilliCPU, rr.MilliCPU) {
		return false
	}
	if !lessFunc(r.Memory, rr.Memory) {
		return false
	}

	for resourceName, leftValue := range r.ScalarResources {
		rightValue, ok := rr.ScalarResources[resourceName]
		if !ok && defaultValue == Infinity {
			continue
		}

		if !lessFunc(leftValue, rightValue) {
			return false
		}
	}
	return true
}

// LessEqual returns true only on condition that all dimensions of resources in r are less than or equal with that of rr,
// Otherwise returns false.
// @param defaultValue "default value for resource dimension not defined in ScalarResources. Its value can only be one of 'Zero' and 'Infinity'"
func (r *Resource) LessEqual(rr *Resource, defaultValue DimensionDefaultValue) bool {
	lessEqualFunc := func(l, r, diff float64) bool {
		if l < r || math.Abs(l-r) < diff {
			return true
		}
		return false
	}

	if !lessEqualFunc(r.MilliCPU, rr.MilliCPU, minResource) {
		return false
	}
	if !lessEqualFunc(r.Memory, rr.Memory, minResource) {
		return false
	}

	for resourceName, leftValue := range r.ScalarResources {
		rightValue, ok := rr.ScalarResources[resourceName]
		if !ok && defaultValue == Infinity {
			continue
		}

		if !lessEqualFunc(leftValue, rightValue, minResource) {
			return false
		}
	}
	return true
}

// LessPartly returns true if there exists any dimension whose resource amount in r is less than that in rr.
// Otherwise returns false.
// @param defaultValue "default value for resource dimension not defined in ScalarResources. Its value can only be one of 'Zero' and 'Infinity'"
func (r *Resource) LessPartly(rr *Resource, defaultValue DimensionDefaultValue) bool {
	lessFunc := func(l, r float64) bool {
		return l < r
	}

	if lessFunc(r.MilliCPU, rr.MilliCPU) || lessFunc(r.Memory, rr.Memory) {
		return true
	}

	for resourceName, leftValue := range r.ScalarResources {
		rightValue, ok := rr.ScalarResources[resourceName]
		if !ok && defaultValue == Infinity {
			return true
		}

		if lessFunc(leftValue, rightValue) {
			return true
		}
	}
	return false
}

// LessEqualPartly returns true if there exists any dimension whose resource amount in r is less than or equal with that in rr.
// Otherwise returns false.
// @param defaultValue "default value for resource dimension not defined in ScalarResources. Its value can only be one of 'Zero' and 'Infinity'"
func (r *Resource) LessEqualPartly(rr *Resource, defaultValue DimensionDefaultValue) bool {
	lessEqualFunc := func(l, r, diff float64) bool {
		if l < r || math.Abs(l-r) < diff {
			return true
		}
		return false
	}

	if lessEqualFunc(r.MilliCPU, rr.MilliCPU, minResource) || lessEqualFunc(r.Memory, rr.Memory, minResource) {
		return true
	}

	for resourceName, leftValue := range r.ScalarResources {
		rightValue, ok := rr.ScalarResources[resourceName]
		if !ok && defaultValue == Infinity {
			return true
		}

		if lessEqualFunc(leftValue, rightValue, minResource) {
			return true
		}
	}
	return false
}

// Equal returns true only on condition that values in all dimension are equal with each other for r and rr
// Otherwise returns false.
// @param defaultValue "default value for resource dimension not defined in ScalarResources. Its value can only be one of 'Zero' and 'Infinity'"
func (r *Resource) Equal(rr *Resource, defaultValue DimensionDefaultValue) bool {
	equalFunc := func(l, r, diff float64) bool {
		return l == r || math.Abs(l-r) < diff
	}

	if !equalFunc(r.MilliCPU, rr.MilliCPU, minResource) || !equalFunc(r.Memory, rr.Memory, minResource) {
		return false
	}

	for resourceName, leftValue := range r.ScalarResources {
		rightValue := rr.ScalarResources[resourceName]
		if !equalFunc(leftValue, rightValue, minResource) {
			return false
		}
	}
	return true
}

// Diff calculate the difference between two resource object
// Note: if `defaultValue` equals `Infinity`, the difference between two values will be `Infinity`, marked as -1
func (r *Resource) Diff(rr *Resource, defaultValue DimensionDefaultValue) (*Resource, *Resource) {
	leftRes := r.Clone()
	rightRes := rr.Clone()
	increasedVal := EmptyResource()
	decreasedVal := EmptyResource()
	r.setDefaultValue(leftRes, rightRes, defaultValue)

	if leftRes.MilliCPU > rightRes.MilliCPU {
		increasedVal.MilliCPU = leftRes.MilliCPU - rightRes.MilliCPU
	} else {
		decreasedVal.MilliCPU = rightRes.MilliCPU - leftRes.MilliCPU
	}

	if leftRes.Memory > rightRes.Memory {
		increasedVal.Memory = leftRes.Memory - rightRes.Memory
	} else {
		decreasedVal.Memory = rightRes.Memory - leftRes.Memory
	}

	increasedVal.ScalarResources = make(map[v1.ResourceName]float64)
	decreasedVal.ScalarResources = make(map[v1.ResourceName]float64)
	for lName, lQuant := range leftRes.ScalarResources {
		rQuant := rightRes.ScalarResources[lName]
		if lQuant == -1 {
			increasedVal.ScalarResources[lName] = -1
			continue
		}
		if rQuant == -1 {
			decreasedVal.ScalarResources[lName] = -1
			continue
		}
		if lQuant > rQuant {
			increasedVal.ScalarResources[lName] = lQuant - rQuant
		} else {
			decreasedVal.ScalarResources[lName] = rQuant - lQuant
		}
	}

	return increasedVal, decreasedVal
}

// AddScalar adds a resource by a scalar value of this resource.
func (r *Resource) AddScalar(name v1.ResourceName, quantity float64) {
	r.SetScalar(name, r.ScalarResources[name]+quantity)
}

// SetScalar sets a resource by a scalar value of this resource.
func (r *Resource) SetScalar(name v1.ResourceName, quantity float64) {
	// Lazily allocate scalar resource map.
	if r.ScalarResources == nil {
		r.ScalarResources = map[v1.ResourceName]float64{}
	}
	r.ScalarResources[name] = quantity
}

// MinDimensionResource is used to reset the r resource dimension which is less than rr
// e.g r resource is <cpu 2000.00, memory 4047845376.00, hugepages-2Mi 0.00, hugepages-1Gi 0.00>
// rr resource is <cpu 3000.00, memory 1000.00>
// return r resource is <cpu 2000.00, memory 1000.00, hugepages-2Mi 0.00, hugepages-1Gi 0.00>
// @param defaultValue "default value for resource dimension not defined in ScalarResources. Its value can only be one of 'Zero' and 'Infinity'"
func (r *Resource) MinDimensionResource(rr *Resource, defaultValue DimensionDefaultValue) *Resource {
	if rr.MilliCPU < r.MilliCPU {
		r.MilliCPU = rr.MilliCPU
	}
	if rr.Memory < r.Memory {
		r.Memory = rr.Memory
	}

	if r.ScalarResources == nil {
		return r
	}

	if rr.ScalarResources == nil {
		if defaultValue == Infinity {
			return r
		}

		for name := range r.ScalarResources {
			r.ScalarResources[name] = 0
		}
		return r
	}

	for name, quant := range r.ScalarResources {
		rQuant, ok := rr.ScalarResources[name]
		if ok {
			r.ScalarResources[name] = math.Min(quant, rQuant)
		} else {
			if defaultValue == Infinity {
				continue
			}

			r.ScalarResources[name] = 0
		}
	}
	return r
}

// setDefaultValue sets default value for resource dimension not defined of ScalarResource in leftResource and rightResource
// @param defaultValue "default value for resource dimension not defined in ScalarResources. It can only be one of 'Zero' or 'Infinity'"
func (r *Resource) setDefaultValue(leftResource, rightResource *Resource, defaultValue DimensionDefaultValue) {
	if leftResource.ScalarResources == nil {
		leftResource.ScalarResources = map[v1.ResourceName]float64{}
	}
	if rightResource.ScalarResources == nil {
		rightResource.ScalarResources = map[v1.ResourceName]float64{}
	}
	for resourceName := range leftResource.ScalarResources {
		_, ok := rightResource.ScalarResources[resourceName]
		if !ok {
			if defaultValue == Zero {
				rightResource.ScalarResources[resourceName] = 0
			} else if defaultValue == Infinity {
				rightResource.ScalarResources[resourceName] = -1
			}
		}
	}

	for resourceName := range rightResource.ScalarResources {
		_, ok := leftResource.ScalarResources[resourceName]
		if !ok {
			if defaultValue == Zero {
				leftResource.ScalarResources[resourceName] = 0
			} else if defaultValue == Infinity {
				leftResource.ScalarResources[resourceName] = -1
			}
		}
	}
}

// ParseResourceList parses the given configuration map into an API
// ResourceList or returns an error.
func ParseResourceList(m map[string]string) (v1.ResourceList, error) {
	if len(m) == 0 {
		return nil, nil
	}
	rl := make(v1.ResourceList)
	for k, v := range m {
		switch v1.ResourceName(k) {
		// CPU, memory, local storage, and PID resources are supported.
		case v1.ResourceCPU, v1.ResourceMemory, v1.ResourceEphemeralStorage:
			q, err := resource.ParseQuantity(v)
			if err != nil {
				return nil, err
			}
			if q.Sign() == -1 {
				return nil, fmt.Errorf("resource quantity for %q cannot be negative: %v", k, v)
			}
			rl[v1.ResourceName(k)] = q
		default:
			return nil, fmt.Errorf("cannot reserve %q resource", k)
		}
	}
	return rl, nil
}

func GetMinResource() float64 {
	return minResource
}

// ResourceNameList struct defines resource name collection
type ResourceNameList []v1.ResourceName

// Contains judges whether rr is subset of r
func (r ResourceNameList) Contains(rr ResourceNameList) bool {
	for _, rrName := range ([]v1.ResourceName)(rr) {
		isResourceExist := false
		for _, rName := range ([]v1.ResourceName)(r) {
			if rName == rrName {
				isResourceExist = true
				break
			}
		}
		if !isResourceExist {
			return false
		}
	}
	return true
}
