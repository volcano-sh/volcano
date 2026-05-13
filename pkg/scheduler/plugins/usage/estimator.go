/*
Copyright 2024 The Volcano Authors.

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
	"math"

	v1 "k8s.io/api/core/v1"
	fwk "k8s.io/kube-scheduler/framework"
)

// EstimatorConfig holds configuration for the dynamic sigma resource estimation model.
type EstimatorConfig struct {
	// SigmaBase is the base risk floor. Even when a node is completely idle,
	// we reserve SigmaBase * (Limit - Request) extra beyond Request.
	// Default: 0.15
	SigmaBase float64

	// Threshold is the utilization watermark. When node utilization exceeds this,
	// the scheduling strategy transitions from aggressive to conservative.
	// Default: 0.5
	Threshold float64

	// Sensitivity (k) determines how quickly the estimated load approaches Limit
	// when crossing the threshold watermark.
	// Default: 12.0
	Sensitivity float64

	// BERatio is the default resource ratio for BestEffort pods.
	// A single BestEffort pod is estimated to consume nodeCap * BERatio.
	// Default: 0.1
	BERatio float64

	// BEPenalty is the exponential penalty factor for BestEffort pod density.
	// When there are already n BestEffort pods on a node, the next one is estimated
	// at nodeCap * BERatio * BEPenalty^n.
	// Default: 1.2
	BEPenalty float64
}

// DefaultEstimatorConfig returns an EstimatorConfig with default values.
func DefaultEstimatorConfig() *EstimatorConfig {
	return &EstimatorConfig{
		SigmaBase:   0.15,
		Threshold:   0.5,
		Sensitivity: 12.0,
		BERatio:     0.1,
		BEPenalty:   1.2,
	}
}

// CalcDynamicSigma computes the dynamic risk coefficient using a sigmoid function.
// Formula: sigma = sigmaBase + (1 - sigmaBase) / (1 + exp(-k * (nodeUtilization - threshold)))
// nodeUtilization is the weighted average real utilization of the node (0.0 - 1.0).
func CalcDynamicSigma(config *EstimatorConfig, nodeUtilization float64) float64 {
	exponent := -config.Sensitivity * (nodeUtilization - config.Threshold)
	sigmoid := 1.0 / (1.0 + math.Exp(exponent))
	return config.SigmaBase + (1.0-config.SigmaBase)*sigmoid
}

// CalcNodeRealUtilization computes the weighted average real utilization of a node.
// U_node,cpu = realCPUPercent / 100
// U_node,mem = realMemPercent / 100
// U_node = (U_node,cpu * cpuWeight + U_node,mem * memWeight) / (cpuWeight + memWeight)
func CalcNodeRealUtilization(realCPUPercent, realMemPercent float64, cpuWeight, memWeight int) float64 {
	if cpuWeight+memWeight == 0 {
		return 0
	}
	cpuUtil := realCPUPercent / 100.0
	memUtil := realMemPercent / 100.0
	return (cpuUtil*float64(cpuWeight) + memUtil*float64(memWeight)) / float64(cpuWeight+memWeight)
}

// EstimatePodResource estimates a single dimension (CPU or MEM) resource consumption for a pod.
//
// For pods with Request/Limit configured:
//
//	estimated = request + (limit - request) * sigmaDynamic
//
// For BestEffort pods (request == 0 && limit == 0):
//
//	estimated = nodeCap * BERatio * BEPenalty^bestEffortCount
//
// Parameters:
//   - config: estimator configuration (only BERatio/BEPenalty used for BestEffort)
//   - request: pod's resource request for this dimension (absolute value)
//   - limit: pod's resource limit for this dimension (absolute value)
//   - sigmaDynamic: pre-computed dynamic sigma via CalcDynamicSigma
//   - nodeCap: node capacity for this dimension (absolute value, used only for BestEffort)
//   - bestEffortCount: number of BestEffort pods already on this node
//   - isBestEffort: whether this pod is BestEffort
func EstimatePodResource(config *EstimatorConfig, request, limit, sigmaDynamic, nodeCap float64,
	bestEffortCount int, isBestEffort bool) float64 {
	if isBestEffort {
		// BestEffort: nodeCap * ratio * beta^n
		return nodeCap * config.BERatio * math.Pow(config.BEPenalty, float64(bestEffortCount))
	}
	// Guaranteed/Burstable: Request + (Limit - Request) * sigmaDynamic
	if limit <= request {
		// If limit <= request (e.g., Guaranteed QoS where limit == request),
		// the estimated usage is simply the request value scaled by sigmaDynamic.
		return request * sigmaDynamic
	}
	return request + (limit-request)*sigmaDynamic
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
// Formula: score = ((1 - cpuComp) * cpuWeight + (1 - memComp) * memWeight) / (cpuWeight + memWeight) * MaxNodeScore
//
// Note: usageWeight is the plugin's weight in the overall scheduler scoring,
// handled by the framework layer, NOT multiplied here.
//
// Parameters:
//   - cpuComp: composite CPU utilization (0.0 - 1.0)
//   - memComp: composite MEM utilization (0.0 - 1.0)
//   - cpuWeight: CPU weight from usagePlugin.cpuWeight
//   - memWeight: MEM weight from usagePlugin.memoryWeight
func CalcNodeScore(cpuComp, memComp float64, cpuWeight, memWeight int) float64 {
	totalWeight := cpuWeight + memWeight
	if totalWeight == 0 {
		return 0
	}
	cpuScore := (1.0 - cpuComp) * float64(cpuWeight)
	memScore := (1.0 - memComp) * float64(memWeight)
	score := (cpuScore + memScore) / float64(totalWeight)
	return score * float64(fwk.MaxNodeScore)
}

// getPodCPURequestLimit extracts the total CPU request and limit from a pod (in milliCPU).
func getPodCPURequestLimit(pod *v1.Pod) (request, limit float64) {
	for _, c := range pod.Spec.Containers {
		if req, ok := c.Resources.Requests[v1.ResourceCPU]; ok {
			request += float64(req.MilliValue())
		}
		if lim, ok := c.Resources.Limits[v1.ResourceCPU]; ok {
			limit += float64(lim.MilliValue())
		}
	}
	for _, c := range pod.Spec.InitContainers {
		if req, ok := c.Resources.Requests[v1.ResourceCPU]; ok {
			if float64(req.MilliValue()) > request {
				request = float64(req.MilliValue())
			}
		}
		if lim, ok := c.Resources.Limits[v1.ResourceCPU]; ok {
			if float64(lim.MilliValue()) > limit {
				limit = float64(lim.MilliValue())
			}
		}
	}
	return request, limit
}

// getPodMemRequestLimit extracts the total Memory request and limit from a pod (in bytes).
func getPodMemRequestLimit(pod *v1.Pod) (request, limit float64) {
	for _, c := range pod.Spec.Containers {
		if req, ok := c.Resources.Requests[v1.ResourceMemory]; ok {
			request += float64(req.Value())
		}
		if lim, ok := c.Resources.Limits[v1.ResourceMemory]; ok {
			limit += float64(lim.Value())
		}
	}
	for _, c := range pod.Spec.InitContainers {
		if req, ok := c.Resources.Requests[v1.ResourceMemory]; ok {
			if float64(req.Value()) > request {
				request = float64(req.Value())
			}
		}
		if lim, ok := c.Resources.Limits[v1.ResourceMemory]; ok {
			if float64(lim.Value()) > limit {
				limit = float64(lim.Value())
			}
		}
	}
	return request, limit
}
