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

package warmup

import (
	"fmt"
	"sort"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

const PolicyName = "warmup"

type warmupPolicy struct {
	warmupLabel      string
	warmupLabelValue string
	minNodes         int
	maxNodes         int
	allowNonWarmup   bool // Fallback to non-warmup nodes if needed
}

// New creates a new warmup policy instance
func New() policy.ShardPolicy {
	return &warmupPolicy{
		warmupLabel:      "node.volcano.sh/warmup",
		warmupLabelValue: "true",
		minNodes:         1,
		maxNodes:         1000,
		allowNonWarmup:   true,
	}
}

func (p *warmupPolicy) Name() string {
	return PolicyName
}

func (p *warmupPolicy) Initialize(args policy.Arguments) error {
	args.GetString(&p.warmupLabel, "warmupLabel")
	args.GetString(&p.warmupLabelValue, "warmupLabelValue")
	args.GetInt(&p.minNodes, "minNodes")
	args.GetInt(&p.maxNodes, "maxNodes")
	args.GetBool(&p.allowNonWarmup, "allowNonWarmup")

	// Validate parameters
	if p.minNodes < 0 || p.maxNodes < p.minNodes {
		return fmt.Errorf("invalid node constraints [%d, %d]: minNodes must be >= 0 and <= maxNodes",
			p.minNodes, p.maxNodes)
	}

	if p.warmupLabel == "" {
		return fmt.Errorf("warmupLabel cannot be empty")
	}

	klog.V(3).Infof("Initialized warmup policy: label %s=%s, nodes [%d, %d], allowNonWarmup=%v",
		p.warmupLabel, p.warmupLabelValue, p.minNodes, p.maxNodes, p.allowNonWarmup)

	return nil
}

func (p *warmupPolicy) Calculate(ctx *policy.PolicyContext) (*policy.PolicyResult, error) {
	klog.V(4).Infof("Calculating warmup policy for scheduler %s", ctx.SchedulerName)

	// Separate warmup vs non-warmup nodes
	warmupNodes := []string{}
	nonWarmupNodes := []string{}

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

		// Check if node is a warmup node
		if metrics.IsWarmupNode {
			warmupNodes = append(warmupNodes, node.Name)
			klog.V(5).Infof("Node %s identified as warmup node", node.Name)
		} else {
			nonWarmupNodes = append(nonWarmupNodes, node.Name)
		}
	}

	klog.V(4).Infof("Scheduler %s: %d warmup nodes, %d non-warmup nodes",
		ctx.SchedulerName, len(warmupNodes), len(nonWarmupNodes))

	// Sort both groups by utilization (prefer lower)
	p.sortByUtilization(warmupNodes, ctx.NodeMetrics)
	p.sortByUtilization(nonWarmupNodes, ctx.NodeMetrics)

	// Select from warmup first, fallback to non-warmup if needed
	selected := p.selectNodesWithPriority(warmupNodes, nonWarmupNodes)

	warmupCount := 0
	for _, nodeName := range selected {
		if ctx.NodeMetrics[nodeName] != nil && ctx.NodeMetrics[nodeName].IsWarmupNode {
			warmupCount++
		}
	}

	return &policy.PolicyResult{
		SelectedNodes: selected,
		Reason: fmt.Sprintf("Selected %d nodes (%d warmup, %d non-warmup) with warmup priority",
			len(selected), warmupCount, len(selected)-warmupCount),
		Metadata: map[string]interface{}{
			"selected_count":     len(selected),
			"warmup_count":       warmupCount,
			"non_warmup_count":   len(selected) - warmupCount,
			"available_warmup":   len(warmupNodes),
			"available_nonwarmup": len(nonWarmupNodes),
		},
	}, nil
}

func (p *warmupPolicy) Cleanup() {
	// No cleanup needed for stateless policy
}

// sortByUtilization sorts nodes by CPU utilization (lowest first).
// warmup policy prefers nodes with the most available capacity so that
// pre-warmed nodes can accept new workloads immediately. This is the
// opposite of allocation-rate which prefers highest utilization first.
func (p *warmupPolicy) sortByUtilization(nodes []string, nodeMetrics map[string]*policy.NodeMetrics) {
	sort.Slice(nodes, func(i, j int) bool {
		metricsI := nodeMetrics[nodes[i]]
		metricsJ := nodeMetrics[nodes[j]]

		if metricsI == nil || metricsJ == nil {
			return false
		}

		// Sort by lowest utilization (most available capacity)
		return metricsI.CPUUtilization < metricsJ.CPUUtilization
	})
}

// selectNodesWithPriority selects nodes prioritizing warmup nodes first
func (p *warmupPolicy) selectNodesWithPriority(warmupNodes, nonWarmupNodes []string) []string {
	selected := []string{}

	// First, select from warmup nodes up to maxNodes
	for _, node := range warmupNodes {
		if len(selected) >= p.maxNodes {
			break
		}
		selected = append(selected, node)
	}

	// If we need more nodes (either to meet minNodes or use remaining capacity up to maxNodes)
	// and allowNonWarmup is true, add non-warmup nodes
	if len(selected) < p.maxNodes && p.allowNonWarmup {
		for _, node := range nonWarmupNodes {
			if len(selected) >= p.maxNodes {
				break
			}
			// Only add if we haven't reached maxNodes
			selected = append(selected, node)
		}
	}

	klog.V(4).Infof("Warmup policy selected %d nodes (min: %d, max: %d)",
		len(selected), p.minNodes, p.maxNodes)

	return selected
}
