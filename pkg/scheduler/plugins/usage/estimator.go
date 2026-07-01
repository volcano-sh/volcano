/*
Copyright 2026 The Volcano Authors.

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

package usage

import (
	v1 "k8s.io/api/core/v1"
	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// CalcLoadCompositePercentage computes the weighted composite node load from
// CPU and memory composite utilization values. Inputs and output use the 0.0-1.0
// range, where 0.6 means 60%.
func CalcLoadCompositePercentage(cpuComposite, memComposite float64, cpuWeight, memWeight int) float64 {
	totalWeight := cpuWeight + memWeight
	if totalWeight == 0 {
		return 0
	}
	return (cpuComposite*float64(cpuWeight) + memComposite*float64(memWeight)) / float64(totalWeight)
}

// CalcAppliedRiskFactor returns riskFactor once the node composite load reaches
// riskThreshold. riskFactor values lower than 1.0 are ignored because the risk
// multiplier should never make estimates less conservative under pressure.
func CalcAppliedRiskFactor(loadCompositePercentage, riskThreshold, riskFactor float64) float64 {
	if riskFactor < 1.0 {
		return 1.0
	}
	if loadCompositePercentage >= riskThreshold {
		return riskFactor
	}
	return 1.0
}

// EstimatePodResource estimates a single dimension (CPU or MEM) resource
// consumption for a Guaranteed/Burstable pod.
//
// Formula:
//
//	estimated = (request*requestWeight + (limit-request)*burstWeight) * appliedRiskFactor
//
// The result is clamped to [0, effectiveLimit]. If limit is missing or lower
// than request, request is used as the effective limit.
func EstimatePodResource(request, limit, requestWeight, burstWeight, appliedRiskFactor float64) float64 {
	if request < 0 {
		request = 0
	}
	effectiveLimit := limit
	if effectiveLimit <= 0 || effectiveLimit < request {
		effectiveLimit = request
	}
	burst := effectiveLimit - request
	if burst < 0 {
		burst = 0
	}
	estimate := (request*requestWeight + burst*burstWeight) * appliedRiskFactor
	return clampFloat64(estimate, 0, effectiveLimit)
}

// EstimateBestEffortResource estimates a BestEffort pod from the configured
// fixed value. BestEffort pods are also affected by the node risk factor.
func EstimateBestEffortResource(configuredValue, appliedRiskFactor float64) float64 {
	if configuredValue < 0 {
		configuredValue = 0
	}
	return configuredValue * appliedRiskFactor
}

func clampFloat64(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// CalcCompositeUtilization computes the composite utilization for a single dimension (CPU or MEM).
// Formula: U_r = (realLoadPercent/100 * capacity + shadowEstAbs) / capacity
// The result is clamped to [0.0, 1.0].
//
// Parameters:
//   - realLoadPercent: real load percentage from Prometheus (0-100)
//   - shadowEstAbs: absolute shadow estimated load for this dimension
//   - capacity: node capacity for this dimension (absolute value)
func CalcCompositeUtilization(realLoadPercent float64, shadowEstAbs, capacity float64) float64 {
	if capacity <= 0 {
		return 0
	}
	realAbs := realLoadPercent / 100.0 * capacity
	composite := (realAbs + shadowEstAbs) / capacity
	if composite > 1.0 {
		return 1.0
	}
	if composite < 0 {
		return 0
	}
	return composite
}

// CalcNodeScore computes the node score from composite utilization values.
// Formula: score = ((1 - cpuComp) * cpuWeight + (1 - memComp) * memWeight) / (cpuWeight + memWeight) * MaxNodeScore * usageWeight
//
// Parameters:
//   - cpuComp: composite CPU utilization (0.0 - 1.0)
//   - memComp: composite MEM utilization (0.0 - 1.0)
//   - cpuWeight: CPU weight from usagePlugin.cpuWeight
//   - memWeight: MEM weight from usagePlugin.memoryWeight
//   - usageWeight: plugin weight in the overall scheduler scoring
func CalcNodeScore(cpuComp, memComp float64, cpuWeight, memWeight, usageWeight int) float64 {
	totalWeight := cpuWeight + memWeight
	if totalWeight == 0 {
		return 0
	}
	cpuScore := (1.0 - cpuComp) * float64(cpuWeight)
	memScore := (1.0 - memComp) * float64(memWeight)
	score := (cpuScore + memScore) / float64(totalWeight)
	return score * float64(fwk.MaxNodeScore) * float64(usageWeight)
}

// getPodResourceRequestLimit extracts CPU (milliCPU) and memory (bytes) request/limit for a pod.
func getPodResourceRequestLimit(pod *v1.Pod) (cpuRequest, cpuLimit, memRequest, memLimit float64) {
	request := api.GetPodResourceRequest(pod)
	limit := api.GetPodResourceLimit(pod)
	return request.MilliCPU, limit.MilliCPU, request.Memory, limit.Memory
}
