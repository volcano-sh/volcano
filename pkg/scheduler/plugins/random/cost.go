package random

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"math"
	"volcano.sh/volcano/pkg/scheduler/api"
)

type costFunction func(r *api.Resource, rr *api.Resource) float64

var costFunctions = map[string]costFunction{}

func init() {
	costFunctions[DefaultCostFunction] = eulerDistance
}

func costs(l, r []*api.Resource, costFunction string) float64 {
	cost, ok := costFunctions[costFunction]
	if !ok {
		panic(fmt.Sprintf("illegal costFunction for %s", costFunction))
	}
	sum := 0.0
	for i := 0; i < len(l); i++ {
		sum += cost(l[i], r[i])
	}
	return sum
}

func eulerDistance(r *api.Resource, rr *api.Resource) float64 {
	costCpu := math.Pow(r.MilliCPU-rr.MilliCPU, 2)
	costMem := math.Pow(r.Memory-rr.Memory, 2)
	cost := costCpu + costMem
	for rName, rQuant := range rr.ScalarResources {
		if r.ScalarResources == nil {
			r.ScalarResources = map[v1.ResourceName]float64{}
		}
		cost += math.Pow(r.ScalarResources[rName]-rQuant, 2)
	}
	return cost
}
