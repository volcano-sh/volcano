/*
Copyright 2018-2025 The Volcano Authors.

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

package proportion

import (
	"math"
	"sort"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
)

// fillTolerance is the relative tolerance used to decide that the unclamped
// weighted shares already add up to the pool.
const fillTolerance = 1e-9

// computeDeserved computes attr.deserved of every queue as a weighted max-min
// fair allocation, solved independently for each resource dimension d:
//
//	deserved_i,d = clamp(weight_i * lambda_d, floor_i,d, cap_i,d)
//
// where lambda_d is chosen so that the deserved values add up to the capacity
// of the dimension (or every queue reached its cap).
//
//   - floor is the queue guarantee, limited by the queue's real capability. It is
//     reserved even when the queue has no demand, but it never adds a bonus on top
//     of the weighted share: a queue whose weighted share already covers its
//     guarantee is not affected by it.
//   - cap is min(real capability, request), and never lower than the floor.
//   - Guarantees of queues without any job are reserved before the pool is shared.
//   - If the guarantees of a dimension exceed its capacity they are scaled down
//     proportionally, so the deserved values never over-subscribe the cluster.
//
// Dimensions are independent: a guarantee on one resource never changes how any
// other resource is shared.
func (pp *proportionPlugin) computeDeserved(ssn *framework.Session) {
	attrs := make([]*queueAttr, 0, len(pp.queueOpts))
	for _, attr := range pp.queueOpts {
		attrs = append(attrs, attr)
	}
	// Iterate in a stable order so the result and the logs are deterministic.
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].name < attrs[j].name })

	// Guarantees of queues without jobs are not part of queueOpts, but stay reserved.
	var idleGuarantees []*api.Resource
	for id, queue := range ssn.Queues {
		if _, active := pp.queueOpts[id]; active || len(queue.Queue.Spec.Guarantee.Resource) == 0 {
			continue
		}
		idleGuarantees = append(idleGuarantees, api.NewResource(queue.Queue.Spec.Guarantee.Resource))
	}

	deserved := make([]*api.Resource, len(attrs))
	for i := range attrs {
		deserved[i] = api.EmptyResource()
	}

	for _, dim := range pp.fillDimensions(attrs, idleGuarantees) {
		weights := make([]float64, len(attrs))
		floors := make([]float64, len(attrs))
		caps := make([]float64, len(attrs))

		totalFloor := 0.0
		for i, attr := range attrs {
			weights[i] = float64(attr.weight)
			capLimit, capped := dimensionLimit(attr.realCapability, dim)
			floors[i] = attr.guarantee.Get(dim)
			if capped && floors[i] > capLimit {
				floors[i] = capLimit
			}
			caps[i] = attr.request.Get(dim)
			if capped && caps[i] > capLimit {
				caps[i] = capLimit
			}
			totalFloor += floors[i]
		}
		idleFloor := 0.0
		for _, guarantee := range idleGuarantees {
			idleFloor += guarantee.Get(dim)
		}
		totalFloor += idleFloor

		// Scale the guarantees down when they exceed the capacity of the dimension.
		total := pp.totalResource.Get(dim)
		scale := 1.0
		if totalFloor > total && totalFloor > 0 {
			scale = total / totalFloor
		}
		for i := range floors {
			floors[i] *= scale
			caps[i] = math.Max(caps[i], floors[i])
		}
		pool := math.Max(total-idleFloor*scale, 0)

		for i, value := range fillDimension(weights, floors, caps, pool) {
			setDimension(deserved[i], dim, value)
		}
	}

	for i, attr := range attrs {
		attr.deserved = deserved[i]
		pp.updateShare(attr)
		klog.V(4).Infof("The attributes of queue <%s> in proportion: deserved <%v>, realCapability <%v>, allocate <%v>, request <%v>, elastic <%v>, share <%0.2f>",
			attr.name, attr.deserved, attr.realCapability, attr.allocated, attr.request, attr.elastic, attr.share)
		metrics.UpdateQueueDeserved(attr.name, attr.deserved.MilliCPU, attr.deserved.Memory, attr.deserved.ScalarResources)
	}
}

// fillDimensions returns every resource dimension that needs a deserved value:
// cpu, memory, and the scalar resources of the cluster or of any guarantee.
func (pp *proportionPlugin) fillDimensions(attrs []*queueAttr, idleGuarantees []*api.Resource) []v1.ResourceName {
	scalars := map[v1.ResourceName]struct{}{}
	collect := func(res *api.Resource) {
		for name := range res.ScalarResources {
			scalars[name] = struct{}{}
		}
	}
	collect(pp.totalResource)
	for _, attr := range attrs {
		collect(attr.guarantee)
	}
	for _, guarantee := range idleGuarantees {
		collect(guarantee)
	}

	dims := make([]v1.ResourceName, 0, len(scalars)+2)
	for name := range scalars {
		dims = append(dims, name)
	}
	sort.Slice(dims, func(i, j int) bool { return dims[i] < dims[j] })
	return append([]v1.ResourceName{v1.ResourceCPU, v1.ResourceMemory}, dims...)
}

// dimensionLimit returns the limit of a dimension and whether the dimension is
// limited at all. A scalar resource missing from the limit is unlimited.
func dimensionLimit(limit *api.Resource, dim v1.ResourceName) (float64, bool) {
	if limit == nil {
		return 0, false
	}
	switch dim {
	case v1.ResourceCPU:
		return limit.MilliCPU, true
	case v1.ResourceMemory:
		return limit.Memory, true
	}
	value, found := limit.ScalarResources[dim]
	return value, found
}

func setDimension(res *api.Resource, dim v1.ResourceName, value float64) {
	switch dim {
	case v1.ResourceCPU:
		res.MilliCPU = value
	case v1.ResourceMemory:
		res.Memory = value
	default:
		res.SetScalar(dim, value)
	}
}

// fillDimension solves the weighted max-min fair allocation of pool among the
// queues with a lower and an upper bound each:
//
//	x_i = clamp(weight_i * lambda, floor_i, cap_i),  sum(x_i) = pool
//
// A queue clamped at its floor or its cap is pegged and the rest of the pool is
// shared again by weight, until no queue is clamped any more. If the floors
// already use the whole pool every queue gets its floor, and if the caps need
// less than the pool every queue gets its cap. The caller guarantees floor <= cap.
func fillDimension(weights, floors, caps []float64, pool float64) []float64 {
	n := len(weights)
	result := make([]float64, n)

	floorSum, capSum := 0.0, 0.0
	for i := 0; i < n; i++ {
		floorSum += floors[i]
		capSum += caps[i]
	}
	if floorSum >= pool {
		copy(result, floors)
		return result
	}
	if capSum <= pool {
		copy(result, caps)
		return result
	}

	pegged := make([]bool, n)
	free := n
	poolLeft := pool
	for free > 0 {
		weightSum := 0.0
		for i := 0; i < n; i++ {
			if !pegged[i] {
				weightSum += weights[i]
			}
		}
		if weightSum <= 0 {
			// Only zero-weight queues are left: they cannot share anything beyond their floor.
			for i := 0; i < n; i++ {
				if !pegged[i] {
					result[i] = floors[i]
				}
			}
			return result
		}

		share := make([]float64, n)
		clampedSum := 0.0
		for i := 0; i < n; i++ {
			if pegged[i] {
				continue
			}
			share[i] = poolLeft * weights[i] / weightSum
			clampedSum += math.Min(math.Max(share[i], floors[i]), caps[i])
		}

		diff := clampedSum - poolLeft
		if math.Abs(diff) <= fillTolerance*math.Max(1, math.Abs(poolLeft)) {
			for i := 0; i < n; i++ {
				if !pegged[i] {
					result[i] = math.Min(math.Max(share[i], floors[i]), caps[i])
				}
			}
			return result
		}

		// Over budget: the queues raised to their floor keep it. Under budget: the queues
		// cut down to their cap keep it. Either way lambda moves away from them.
		progressed := false
		for i := 0; i < n; i++ {
			if pegged[i] {
				continue
			}
			switch {
			case diff > 0 && share[i] < floors[i]:
				result[i] = floors[i]
			case diff < 0 && share[i] > caps[i]:
				result[i] = caps[i]
			default:
				continue
			}
			pegged[i] = true
			poolLeft -= result[i]
			free--
			progressed = true
		}
		if !progressed {
			// Only reachable through floating point noise; settle with the clamped shares.
			for i := 0; i < n; i++ {
				if !pegged[i] {
					result[i] = math.Min(math.Max(share[i], floors[i]), caps[i])
				}
			}
			return result
		}
	}
	return result
}
