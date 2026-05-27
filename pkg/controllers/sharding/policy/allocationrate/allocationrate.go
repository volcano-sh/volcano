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

package allocationrate

import (
	"fmt"
	"math"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

// PolicyName is the user-visible name of this policy.
const PolicyName = "allocation-rate"

// Argument keys recognized by Initialize. Exported so config-synthesis
// callers can reference the same names without re-stating string literals.
const (
	ArgMinCPUUtil = "minCPUUtil"
	ArgMaxCPUUtil = "maxCPUUtil"
)

// utilQuant is the quantization step for CPU-utilization rounding (2 decimal
// places). Filter and Score both round through roundUtil so they cannot drift.
const utilQuant = 100

func roundUtil(u float64) float64 {
	return math.Round(u*utilQuant) / utilQuant
}

type allocationRatePolicy struct {
	minCPURounded   float64
	maxCPURounded   float64
	cpuRangeRounded float64
}

// New creates a new allocation-rate policy instance.
func New() policy.ShardPolicy {
	return &allocationRatePolicy{
		maxCPURounded:   1.0,
		cpuRangeRounded: 1.0,
	}
}

func (p *allocationRatePolicy) Name() string { return PolicyName }

func (p *allocationRatePolicy) Initialize(args policy.Arguments) error {
	var minCPU, maxCPU float64
	args.GetFloat64(&minCPU, ArgMinCPUUtil)
	args.GetFloat64(&maxCPU, ArgMaxCPUUtil)

	if minCPU < 0 || maxCPU > 1 || minCPU > maxCPU {
		return fmt.Errorf("invalid CPU utilization range [%.2f, %.2f]: must be within [0.0, 1.0] and min <= max",
			minCPU, maxCPU)
	}

	p.minCPURounded = roundUtil(minCPU)
	p.maxCPURounded = roundUtil(maxCPU)
	p.cpuRangeRounded = p.maxCPURounded - p.minCPURounded

	klog.V(3).Infof("Initialized allocation-rate policy: CPU range [%.2f, %.2f]",
		p.minCPURounded, p.maxCPURounded)
	return nil
}

func (p *allocationRatePolicy) Cleanup() {}

// Filter rejects nodes outside [minCPUUtil, maxCPUUtil] or with missing
// metrics. The framework excludes already-assigned nodes before Filter runs.
func (p *allocationRatePolicy) Filter(ctx *policy.PolicyContext, node *corev1.Node) bool {
	metrics := ctx.NodeMetrics[node.Name]
	if metrics == nil {
		return false
	}
	if metrics.CPUUtilization < 0 {
		return false
	}
	util := roundUtil(metrics.CPUUtilization)
	return util >= p.minCPURounded && util <= p.maxCPURounded
}

// Score normalizes CPUUtilization within the configured [minCPUUtil,
// maxCPUUtil] range so the output spans [0, 1] regardless of how narrow the
// range is. Higher util pulls the node forward in the sort so workloads pack
// onto already-busy nodes, leaving lightly-loaded nodes available for other
// schedulers.
func (p *allocationRatePolicy) Score(ctx *policy.PolicyContext, node *corev1.Node) float64 {
	metrics := ctx.NodeMetrics[node.Name]
	if metrics == nil {
		return 0
	}
	if metrics.CPUUtilization < 0 {
		return 0
	}
	if p.cpuRangeRounded == 0 {
		return 1
	}
	util := roundUtil(metrics.CPUUtilization)
	score := (util - p.minCPURounded) / p.cpuRangeRounded
	// Clamp to [0, 1] in case Score is invoked on a node Filter would reject.
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
