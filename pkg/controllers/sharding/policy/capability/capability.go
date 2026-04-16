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

package capability

import (
	"fmt"
	"sort"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

const PolicyName = "capability"

type capabilityPolicy struct {
	maxCapacityPercent float64 // Default 0.30 (30%)
	minNodes           int
	maxNodes           int
	preferWarmupNodes  bool
}

// New creates a new capability policy instance
func New() policy.ShardPolicy {
	return &capabilityPolicy{
		maxCapacityPercent: 0.30,
		minNodes:           1,
		maxNodes:           1000,
		preferWarmupNodes:  false,
	}
}

func (p *capabilityPolicy) Name() string {
	return PolicyName
}

func (p *capabilityPolicy) Initialize(args policy.Arguments) error {
	args.GetFloat64(&p.maxCapacityPercent, "maxCapacityPercent")
	args.GetInt(&p.minNodes, "minNodes")
	args.GetInt(&p.maxNodes, "maxNodes")
	args.GetBool(&p.preferWarmupNodes, "preferWarmupNodes")

	// Validate parameters
	if p.maxCapacityPercent <= 0 || p.maxCapacityPercent > 1 {
		return fmt.Errorf("invalid maxCapacityPercent %.2f: must be within (0.0, 1.0]",
			p.maxCapacityPercent)
	}

	if p.minNodes < 0 || p.maxNodes < p.minNodes {
		return fmt.Errorf("invalid node constraints [%d, %d]: minNodes must be >= 0 and <= maxNodes",
			p.minNodes, p.maxNodes)
	}

	klog.V(3).Infof("Initialized capability policy: maxCapacity %.0f%%, nodes [%d, %d], preferWarmup=%v",
		p.maxCapacityPercent*100, p.minNodes, p.maxNodes, p.preferWarmupNodes)

	return nil
}

func (p *capabilityPolicy) Calculate(ctx *policy.PolicyContext) (*policy.PolicyResult, error) {
	klog.V(4).Infof("Calculating capability policy for scheduler %s", ctx.SchedulerName)

	// Filter nodes where: CPUUtilization <= (1.0 - maxCapacityPercent)
	// This ensures room for maxCapacityPercent% more allocation
	eligibleNodes := p.filterEligibleNodes(ctx)
	klog.V(4).Infof("Scheduler %s has %d eligible nodes with capacity for %.0f%% allocation",
		ctx.SchedulerName, len(eligibleNodes), p.maxCapacityPercent*100)

	// Sort by lowest utilization (most headroom)
	p.sortByUtilization(eligibleNodes, ctx)

	// Apply min/max constraints
	selected := p.selectWithConstraints(eligibleNodes)

	return &policy.PolicyResult{
		SelectedNodes: selected,
		Reason: fmt.Sprintf("Selected %d nodes with capacity for %.0f%% allocation (from %d eligible)",
			len(selected), p.maxCapacityPercent*100, len(eligibleNodes)),
		Metadata: map[string]interface{}{
			"eligible_count":       len(eligibleNodes),
			"selected_count":       len(selected),
			"max_capacity_percent": p.maxCapacityPercent,
			"prefer_warmup":        p.preferWarmupNodes,
		},
	}, nil
}

func (p *capabilityPolicy) Cleanup() {
	// No cleanup needed for stateless policy
}

// filterEligibleNodes filters nodes that have sufficient capacity headroom
func (p *capabilityPolicy) filterEligibleNodes(ctx *policy.PolicyContext) []string {
	eligibleNodes := make([]string, 0, len(ctx.AllNodes))
	maxUtilThreshold := 1.0 - p.maxCapacityPercent

	for _, node := range ctx.AllNodes {
		// Skip already assigned nodes
		if _, assigned := ctx.AssignedNodes[node.Name]; assigned {
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

		// Check if node has sufficient headroom
		if metrics.CPUUtilization <= maxUtilThreshold {
			eligibleNodes = append(eligibleNodes, node.Name)
			klog.V(5).Infof("Node %s eligible: utilization %.2f <= threshold %.2f",
				node.Name, metrics.CPUUtilization, maxUtilThreshold)
		} else {
			klog.V(5).Infof("Skipping node %s: utilization %.2f > threshold %.2f",
				node.Name, metrics.CPUUtilization, maxUtilThreshold)
		}
	}

	return eligibleNodes
}

// sortByUtilization sorts nodes by utilization (lowest first for most headroom).
// capability policy prefers nodes with the most capacity headroom so that
// agent workloads can burst without resource pressure. This is the opposite
// of allocation-rate which prefers highest utilization first.
// If preferWarmupNodes is true, warmup nodes are prioritized first.
func (p *capabilityPolicy) sortByUtilization(nodes []string, ctx *policy.PolicyContext) {
	sort.Slice(nodes, func(i, j int) bool {
		metricsI := ctx.NodeMetrics[nodes[i]]
		metricsJ := ctx.NodeMetrics[nodes[j]]

		if metricsI == nil || metricsJ == nil {
			return false
		}

		// If preferWarmupNodes is enabled, prioritize warmup nodes
		if p.preferWarmupNodes {
			if metricsI.IsWarmupNode && !metricsJ.IsWarmupNode {
				return true
			}
			if !metricsI.IsWarmupNode && metricsJ.IsWarmupNode {
				return false
			}
		}

		// Sort by lowest utilization (most headroom)
		return metricsI.CPUUtilization < metricsJ.CPUUtilization
	})
}

// selectWithConstraints applies min/max node constraints
func (p *capabilityPolicy) selectWithConstraints(nodes []string) []string {
	// Clamp desired count: at least minNodes, at most maxNodes, but never
	// more than available nodes.
	desiredCount := min(len(nodes), max(p.minNodes, min(p.maxNodes, len(nodes))))

	if desiredCount == 0 {
		return []string{}
	}

	selected := make([]string, desiredCount)
	copy(selected, nodes[:desiredCount])

	return selected
}
