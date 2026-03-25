/*
Copyright 2025 The Volcano Authors.
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
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

const PolicyName = "allocation-rate"

type allocationRatePolicy struct {
	minCPUUtil        float64
	maxCPUUtil        float64
	preferWarmupNodes bool
	minNodes          int
	maxNodes          int
}

// New creates a new allocation-rate policy instance
func New() policy.ShardPolicy {
	return &allocationRatePolicy{
		minCPUUtil:        0.0,
		maxCPUUtil:        1.0,
		preferWarmupNodes: false,
		minNodes:          1,
		maxNodes:          1000,
	}
}

func (p *allocationRatePolicy) Name() string {
	return PolicyName
}

func (p *allocationRatePolicy) Initialize(args policy.Arguments) error {
	args.GetFloat64(&p.minCPUUtil, "minCPUUtil")
	args.GetFloat64(&p.maxCPUUtil, "maxCPUUtil")
	args.GetBool(&p.preferWarmupNodes, "preferWarmupNodes")
	args.GetInt(&p.minNodes, "minNodes")
	args.GetInt(&p.maxNodes, "maxNodes")

	// Validate parameters
	if p.minCPUUtil < 0 || p.maxCPUUtil > 1 || p.minCPUUtil > p.maxCPUUtil {
		return fmt.Errorf("invalid CPU utilization range [%.2f, %.2f]: must be within [0.0, 1.0] and min <= max",
			p.minCPUUtil, p.maxCPUUtil)
	}

	if p.minNodes < 0 || p.maxNodes < p.minNodes {
		return fmt.Errorf("invalid node constraints [%d, %d]: minNodes must be >= 0 and <= maxNodes",
			p.minNodes, p.maxNodes)
	}

	klog.V(3).Infof("Initialized allocation-rate policy: CPU range [%.2f, %.2f], nodes [%d, %d], preferWarmup=%v",
		p.minCPUUtil, p.maxCPUUtil, p.minNodes, p.maxNodes, p.preferWarmupNodes)

	return nil
}

func (p *allocationRatePolicy) Calculate(ctx *policy.PolicyContext) (*policy.PolicyResult, error) {
	klog.V(4).Infof("Calculating allocation-rate policy for scheduler %s", ctx.SchedulerName)

	// 1. Filter nodes by CPU utilization range [minCPUUtil, maxCPUUtil]
	eligibleNodes := p.filterEligibleNodes(ctx)
	klog.V(4).Infof("Scheduler %s has %d eligible nodes (CPU range: [%.2f, %.2f])",
		ctx.SchedulerName, len(eligibleNodes), p.minCPUUtil, p.maxCPUUtil)

	// 2. Prioritize: warmup nodes first (if preferWarmupNodes), sorted by utilization
	prioritizedNodes := p.prioritizeNodes(eligibleNodes, ctx)

	// 3. Select nodes within [minNodes, maxNodes] constraints
	selectedNodes := p.selectNodesWithinConstraints(prioritizedNodes)

	return &policy.PolicyResult{
		SelectedNodes: selectedNodes,
		Reason: fmt.Sprintf("Selected %d nodes from %d eligible in CPU range [%.2f, %.2f]%s",
			len(selectedNodes), len(eligibleNodes), p.minCPUUtil, p.maxCPUUtil,
			p.warmupPreferenceString()),
		Metadata: map[string]interface{}{
			"eligible_count":  len(eligibleNodes),
			"selected_count":  len(selectedNodes),
			"prefer_warmup":   p.preferWarmupNodes,
			"min_cpu_util":    p.minCPUUtil,
			"max_cpu_util":    p.maxCPUUtil,
		},
	}, nil
}

func (p *allocationRatePolicy) Cleanup() {
	// No cleanup needed for stateless policy
}

// filterEligibleNodes applies hard filtering based on CPU utilization range
func (p *allocationRatePolicy) filterEligibleNodes(ctx *policy.PolicyContext) []*corev1.Node {
	var eligibleNodes []*corev1.Node

	for _, node := range ctx.AllNodes {
		// Skip already assigned nodes
		if _, exists := ctx.AssignedNodes[node.Name]; exists {
			continue
		}

		// Get node metrics
		metrics := ctx.NodeMetrics[node.Name]
		if metrics == nil {
			klog.V(5).Infof("Skipping node %s: no metrics available", node.Name)
			continue
		}

		// Skip nodes with invalid utilization data
		if metrics.CPUUtilization < 0 {
			klog.V(5).Infof("Skipping node %s: invalid CPU utilization %.2f", node.Name, metrics.CPUUtilization)
			continue
		}

		// Round the utilization with 2 floating points for comparison
		cpuUtilRounded := math.Round(metrics.CPUUtilization*100) / 100
		minCPURounded := math.Round(p.minCPUUtil*100) / 100
		maxCPURounded := math.Round(p.maxCPUUtil*100) / 100

		// Check if node is within CPU utilization range
		if cpuUtilRounded < minCPURounded || cpuUtilRounded > maxCPURounded {
			klog.V(5).Infof("Skipping node %s: CPU utilization %.2f outside range [%.2f, %.2f]",
				node.Name, cpuUtilRounded, minCPURounded, maxCPURounded)
			continue
		}

		// Node is eligible
		eligibleNodes = append(eligibleNodes, node)
	}

	return eligibleNodes
}

// prioritizeNodes orders nodes based on warmup preference and utilization
func (p *allocationRatePolicy) prioritizeNodes(nodes []*corev1.Node, ctx *policy.PolicyContext) []*corev1.Node {
	if len(nodes) <= 1 {
		return nodes
	}

	// Divide nodes into two groups using the warmup label
	warmupNodes := make([]*corev1.Node, 0)
	nonWarmupNodes := make([]*corev1.Node, 0)

	for _, node := range nodes {
		metrics := ctx.NodeMetrics[node.Name]
		if metrics != nil && metrics.IsWarmupNode {
			warmupNodes = append(warmupNodes, node)
		} else {
			nonWarmupNodes = append(nonWarmupNodes, node)
		}
	}

	// Sort nodes by CPU utilization within groups (higher utilization first)
	sort.Slice(warmupNodes, func(i, j int) bool {
		metricsI := ctx.NodeMetrics[warmupNodes[i].Name]
		metricsJ := ctx.NodeMetrics[warmupNodes[j].Name]
		if metricsI == nil || metricsJ == nil {
			return false
		}
		return metricsI.CPUUtilization > metricsJ.CPUUtilization
	})

	sort.Slice(nonWarmupNodes, func(i, j int) bool {
		metricsI := ctx.NodeMetrics[nonWarmupNodes[i].Name]
		metricsJ := ctx.NodeMetrics[nonWarmupNodes[j].Name]
		if metricsI == nil || metricsJ == nil {
			return false
		}
		return metricsI.CPUUtilization > metricsJ.CPUUtilization
	})

	// Combine results: prefer warmup nodes if configured
	prioritized := make([]*corev1.Node, 0, len(nodes))
	if p.preferWarmupNodes {
		prioritized = append(prioritized, warmupNodes...)
		prioritized = append(prioritized, nonWarmupNodes...)
	} else {
		prioritized = append(prioritized, nonWarmupNodes...)
		prioritized = append(prioritized, warmupNodes...)
	}

	return prioritized
}

// selectNodesWithinConstraints selects nodes within min/max constraints
func (p *allocationRatePolicy) selectNodesWithinConstraints(nodes []*corev1.Node) []string {
	// Compute the desired node count
	desiredCount := p.calculateDesiredNodeCount(len(nodes))

	// Ensure the desired node count is at least minNodes
	if desiredCount < p.minNodes {
		desiredCount = p.minNodes
	}

	// If eligible nodes is smaller than desired, use all eligible nodes
	if len(nodes) < desiredCount {
		desiredCount = len(nodes)
	}

	// Select nodes
	selected := make([]string, 0, desiredCount)

	// Select nodes according to priority up to desiredCount
	// desiredCount already accounts for minNodes constraint
	for i := 0; i < desiredCount && i < len(nodes); i++ {
		selected = append(selected, nodes[i].Name)
	}

	return selected
}

// calculateDesiredNodeCount calculates desired node count based on constraints
func (p *allocationRatePolicy) calculateDesiredNodeCount(eligibleNodeCount int) int {
	// Select all eligible nodes
	desired := eligibleNodeCount

	// Apply max constraint
	if desired > p.maxNodes {
		desired = p.maxNodes
	}

	// Apply min constraint
	if desired < p.minNodes {
		desired = p.minNodes
	}

	return desired
}

// warmupPreferenceString returns a string describing warmup preference
func (p *allocationRatePolicy) warmupPreferenceString() string {
	if p.preferWarmupNodes {
		return " with warmup preference"
	}
	return " without warmup preference"
}
