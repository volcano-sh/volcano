package random

import (
	v1 "k8s.io/api/core/v1"
	"math"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func (rp *randomPlugin) Calculate() {
	guarantees := make([]*api.Resource, 0)
	capacities := make([]*api.Resource, 0)
	requests := make([]*api.Resource, 0)
	weights := make([]int32, 0)
	queueMap := make(map[int]api.QueueID)
	queueIndex := 0
	var totalWeight int32
	for queueId, attr := range rp.queueOpts {
		guarantees = append(guarantees, attr.guarantee)
		capacities = append(capacities, attr.realCapability)
		requests = append(requests, attr.request)
		weights = append(weights, attr.weight)
		totalWeight += attr.weight
		queueMap[queueIndex] = queueId
		queueIndex++
	}
	totalResource := rp.totalResource.Clone()
	weightResources := make([]*api.Resource, 0)
	for _, w := range weights {
		weightResources = append(weightResources, totalResource.Multi(float64(w)/float64(totalWeight)))
	}

	deserves := rp.calculate(rp.totalResource, rp.totalGuarantee, guarantees, capacities, requests, weightResources,
		len(rp.queueOpts), rp.tryNum, rp.weightCostWeight, rp.requestCostWeight)
	for i, d := range deserves {
		queueId := queueMap[i]
		rp.queueOpts[queueId].deserved = d
	}
}

func (rp *randomPlugin) calculate(total, totalGuarantees *api.Resource, guarantees, capacities, requests, weightResources []*api.Resource,
	queueNum, tryNum, weightCostWeight, requestCostWeight int) []*api.Resource {

	remaining := total.Clone().Sub(totalGuarantees)
	min := math.MaxFloat64
	var d []*api.Resource
	// find the smallest cost in tryNum tries
	for i := 0; i < tryNum; i++ {
		// try with random split
		deserves := try(remaining.Clone(), guarantees, capacities, queueNum)
		// calculate cost
		costw := costs(deserves, weightResources, rp.costFunction)
		costr := costs(deserves, requests, rp.costFunction)
		cost := float64(weightCostWeight)*costw + float64(requestCostWeight)*costr
		if min > cost {
			min = cost
			d = deserves
		}
	}
	return d
}

// calculate the queue's deserved, and make sure that a queue's deserved is in [guarantee,capacity]
func try(remaining *api.Resource, guarantees, capacities []*api.Resource, queueNum int) []*api.Resource {
	deserves := make([]*api.Resource, 0)
	divides := divide(remaining, queueNum)
	deserve := api.EmptyResource()
	extra := api.EmptyResource()
	for i := 0; i < queueNum; i++ {
		deserve, extra = mix(extra, divides[i], guarantees[i], capacities[i])
		deserves = append(deserves, deserve)
	}
	return deserves
}

// divide remaining into queueNum parts
func divide(remaining *api.Resource, queueNum int) []*api.Resource {
	result := make([]*api.Resource, 0)
	// can only be 123.0/345.0.0 etc
	cpuSplit := randomNumFloat64(remaining.MilliCPU, queueNum, 0)
	memSplit := randomNumFloat64(remaining.Memory, queueNum, 0)
	scalarSplit := make(map[v1.ResourceName][]float64)
	if len(remaining.ScalarResources) > 0 {
		for r, v := range remaining.ScalarResources {
			// can only be 1000.0/2000.0 etc
			rSplit := randomNumFloat64(v, queueNum, -3)
			scalarSplit[r] = rSplit
		}
	}

	for i := 0; i < queueNum; i++ {
		q := &api.Resource{
			MilliCPU:        cpuSplit[i],
			Memory:          memSplit[i],
			ScalarResources: make(map[v1.ResourceName]float64),
		}
		result = append(result, q)
		if len(remaining.ScalarResources) == 0 {
			continue
		}
		for r, v := range scalarSplit {
			q.ScalarResources[r] = v[i]
		}
	}
	return result
}

// shaving peaks and filling valleys
// make sure that deserve is in [guarantee,capacity], if extra + divide > deserve, return new extra
func mix(extra, divide, guarantee, capacity *api.Resource) (*api.Resource, *api.Resource) {
	deserve := extra.Add(divide).Add(guarantee)
	if deserve.LessEqual(capacity, api.Zero) {
		return deserve, api.EmptyResource()
	}
	d := deserve.MinDimensionResource(capacity, api.Zero)
	return d, deserve.Sub(d)
}
