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

package sharding

import (
	"fmt"
	"math"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	shardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
)

const (
	defaultBatchSize = 50
)

// ShardingManager calculates shard assignments
type ShardingManager struct {
	schedulerConfigs []SchedulerConfig
	metricsProvider  NodeMetricsProvider
}

// NewShardingManager creates a new sharding manager
func NewShardingManager(schedulerConfigs []SchedulerConfig, nodeMetricsProvider NodeMetricsProvider) *ShardingManager {
	manager := &ShardingManager{
		schedulerConfigs: schedulerConfigs,
		metricsProvider:  nodeMetricsProvider,
	}

	return manager
}

// CalculateShardAssignments calculates shard assignments for all schedulers
func (sm *ShardingManager) CalculateShardAssignments(
	nodes []*corev1.Node,
	currentShards []*shardv1alpha1.NodeShard,
) (map[string]*ShardAssignment, error) {
	startTime := time.Now()

	defer func() {
		duration := time.Since(startTime)
		klog.V(4).Infof("Calculated shard assignments in %v for %d nodes", duration, len(nodes))

		// FIX: Add performance metrics
		if duration > 1*time.Second {
			klog.Warningf("Slow shard assignment calculation: %v for %d nodes", duration, len(nodes))
		}
	}()

	// Batch processing for large clusters
	if len(nodes) > defaultBatchSize {
		return sm.calculateShardAssignmentsBatched(nodes, currentShards)
	}

	// Prepare node resources info
	nodeResources := sm.prepareNodeResourcesInfo(nodes)
	nodeMap := make(map[string]*corev1.Node)
	for i := range nodes {
		node := nodes[i]
		nodeMap[node.Name] = node
	}

	// Track assigned nodes to ensure mutual exclusivity
	assignedNodes := make(map[string]string) // node name -> scheduler name

	// Prepare current shards map
	currentShardsMap := make(map[string]*shardv1alpha1.NodeShard)
	for i := range currentShards {
		currentShardsMap[currentShards[i].Name] = currentShards[i]
	}

	// Calculate assignments for each scheduler
	assignments := make(map[string]*ShardAssignment)

	for _, config := range sm.schedulerConfigs {
		klog.V(4).Infof("Calculating assignment for scheduler: %s", config.Name)

		// Apply hard filtering to get eligible nodes
		eligibleNodes := sm.filterEligibleNodes(config, nodeResources, nodeMap, assignedNodes)

		// Prioritize nodes based on warmup preference and utilization
		prioritizedNodes := sm.prioritizeNodes(config, eligibleNodes, nodeResources)

		// Select nodes within constraints
		selectedNodes := sm.selectNodesWithinConstraints(config, prioritizedNodes)

		// Mark nodes as assigned
		for _, node := range selectedNodes {
			assignedNodes[node] = config.Name
		}

		// Create assignment
		assignment := &ShardAssignment{
			SchedulerName: config.Name,
			NodesDesired:  selectedNodes,
			StrategyUsed:  "hard-filtering",
			Version:       fmt.Sprintf("%d", time.Now().UnixNano()),
			Reason: fmt.Sprintf("Selected %d nodes within CPU range [%.2f, %.2f]",
				len(selectedNodes), config.ShardStrategy.CPUUtilizationRange.Min,
				config.ShardStrategy.CPUUtilizationRange.Max),
		}

		assignments[config.Name] = assignment

		klog.Infof("Scheduler %s assigned %d nodes: %v",
			config.Name, len(selectedNodes), selectedNodes)
	}

	klog.Infof("Completed shard assignments: %d nodes assigned to %d schedulers",
		len(assignedNodes), len(assignments))

	return assignments, nil
}

// calculateShardAssignmentsBatched processes nodes in batches for large clusters
func (sm *ShardingManager) calculateShardAssignmentsBatched(
	nodes []*corev1.Node,
	currentShards []*shardv1alpha1.NodeShard,
) (map[string]*ShardAssignment, error) {
	batchSize := defaultBatchSize
	assignments := make(map[string]*ShardAssignment)

	// Process nodes in batches
	for i := 0; i < len(nodes); i += batchSize {
		end := i + batchSize
		if end > len(nodes) {
			end = len(nodes)
		}

		batch := nodes[i:end]
		batchAssignments, err := sm.CalculateShardAssignments(batch, currentShards)
		if err != nil {
			return nil, err
		}

		// Merge assignments
		for scheduler, assignment := range batchAssignments {
			if existing, exists := assignments[scheduler]; exists {
				existing.NodesDesired = append(existing.NodesDesired, assignment.NodesDesired...)
			} else {
				assignments[scheduler] = assignment
			}
		}

		// Small delay between batches to prevent resource starvation
		time.Sleep(10 * time.Millisecond)
	}

	return assignments, nil
}

// filterEligibleNodes applies hard filtering based on strategy
func (sm *ShardingManager) filterEligibleNodes(
	config SchedulerConfig,
	nodeResources map[string]*NodeResourceInfo,
	nodeMap map[string]*corev1.Node,
	assignedNodes map[string]string,
) []*corev1.Node {
	var eligibleNodes []*corev1.Node

	minCPU := config.ShardStrategy.CPUUtilizationRange.Min
	maxCPU := config.ShardStrategy.CPUUtilizationRange.Max

	for nodeName, resourceInfo := range nodeResources {
		// Skip already assigned nodes
		if _, exists := assignedNodes[nodeName]; exists {
			continue
		}

		// Skip nodes with invalid utilization data
		if resourceInfo.CPUUtilization < 0 {
			continue
		}

		// round the utilization with 2 floating points
		cpuUtilRounded := math.Round(resourceInfo.CPUUtilization*100) / 100
		minCPURounded := math.Round(minCPU*100) / 100
		maxCPURounded := math.Round(maxCPU*100) / 100

		if cpuUtilRounded < minCPURounded || cpuUtilRounded > maxCPURounded {
			continue
		}

		// Node is eligible
		if node, exists := nodeMap[nodeName]; exists {
			eligibleNodes = append(eligibleNodes, node)
		}
	}

	klog.V(4).Infof("Scheduler %s has %d eligible nodes (CPU range: [%.2f, %.2f])",
		config.Name, len(eligibleNodes), minCPU, maxCPU)

	return eligibleNodes
}

// prioritizeNodes orders nodes based on warmup preference and utilization
func (sm *ShardingManager) prioritizeNodes(
	config SchedulerConfig,
	nodes []*corev1.Node,
	nodeResources map[string]*NodeResourceInfo,
) []*corev1.Node {
	if len(nodes) <= 1 {
		return nodes
	}

	// create node index
	nodeIndex := make(map[string]int)
	for i, node := range nodes {
		nodeIndex[node.Name] = i
	}

	// divide nodes into two groups using the warmup label
	warmupNodes := make([]*corev1.Node, 0)
	nonWarmupNodes := make([]*corev1.Node, 0)

	for _, node := range nodes {
		info := nodeResources[node.Name]
		if info.IsWarmupNode {
			warmupNodes = append(warmupNodes, node)
		} else {
			nonWarmupNodes = append(nonWarmupNodes, node)
		}
	}

	// sort nodes by CPU utilization within groups
	sort.Slice(warmupNodes, func(i, j int) bool {
		infoI := nodeResources[warmupNodes[i].Name]
		infoJ := nodeResources[warmupNodes[j].Name]
		return infoI.CPUUtilization > infoJ.CPUUtilization
	})

	sort.Slice(nonWarmupNodes, func(i, j int) bool {
		infoI := nodeResources[nonWarmupNodes[i].Name]
		infoJ := nodeResources[nonWarmupNodes[j].Name]
		return infoI.CPUUtilization > infoJ.CPUUtilization
	})

	// combine results：prefer warmup node
	prioritized := make([]*corev1.Node, 0, len(nodes))

	if config.ShardStrategy.PreferWarmupNodes {
		prioritized = append(prioritized, warmupNodes...)
		prioritized = append(prioritized, nonWarmupNodes...)
	} else {
		prioritized = append(prioritized, nonWarmupNodes...)
		prioritized = append(prioritized, warmupNodes...)
	}

	return prioritized
}

// selectNodesWithinConstraints selects nodes within min/max constraints
func (sm *ShardingManager) selectNodesWithinConstraints(
	config SchedulerConfig,
	nodes []*corev1.Node,
) []string {
	// compute the number of necessary nodes
	desiredCount := sm.calculateDesiredNodeCount(config, len(nodes))

	// ensure the desired node count is smaller than the minimum nodes
	if desiredCount < config.ShardStrategy.MinNodes {
		desiredCount = config.ShardStrategy.MinNodes
	}

	// if eligible nodes is smaller than desired, then use all eligible nodes
	if len(nodes) < desiredCount {
		desiredCount = len(nodes)
	}

	// select nodes
	selected := make([]string, 0, desiredCount)

	// first select nodes according to priority
	for i := 0; i < desiredCount && i < len(nodes); i++ {
		selected = append(selected, nodes[i].Name)
	}

	// check whether it meets the minimum node constraint
	if len(selected) < config.ShardStrategy.MinNodes && len(nodes) > len(selected) {
		// add extra nodes until the minimum node constraint is met
		additionalNeeded := config.ShardStrategy.MinNodes - len(selected)
		for i := len(selected); i < len(nodes) && additionalNeeded > 0; i++ {
			selected = append(selected, nodes[i].Name)
			additionalNeeded--
		}
	}

	return selected
}

// calculateDesiredNodeCount calculates desired node count based on strategy constraints
func (sm *ShardingManager) calculateDesiredNodeCount(config SchedulerConfig, eligibleNodeCount int) int {
	// select all eligible nodes
	desired := eligibleNodeCount

	// apply max constraint
	if desired > config.ShardStrategy.MaxNodes {
		desired = config.ShardStrategy.MaxNodes
	}

	// apply min constraint
	if desired < config.ShardStrategy.MinNodes {
		desired = config.ShardStrategy.MinNodes
	}

	return desired
}

// prepareNodeResourcesInfo prepares resource information for nodes
func (sm *ShardingManager) prepareNodeResourcesInfo(nodes []*corev1.Node) map[string]*NodeResourceInfo {
	// Get all metrics from provider
	allMetrics := sm.metricsProvider.GetAllNodeMetrics()

	resources := make(map[string]*NodeResourceInfo)

	for _, node := range nodes {
		metrics := allMetrics[node.Name]
		if metrics == nil {
			// Create default metrics if not available
			klog.Warningf("Metrics not found for node %s, assuming zero utilization for sharding calculation.", node.Name)
			metrics = &NodeMetrics{
				CPUUtilization:    0.0,
				MemoryUtilization: 0.0,
				IsWarmupNode:      node.Labels["node.volcano.sh/warmup"] == "true",
				Labels:            node.Labels,
				Annotations:       node.Annotations,
				LastUpdated:       time.Now(),
			}
		}

		info := &NodeResourceInfo{
			NodeName:          node.Name,
			CPUAllocatable:    metrics.CPUAllocatable,
			CPUCapacity:       metrics.CPUCapacity,
			MemoryAllocatable: metrics.MemoryAllocatable,
			MemoryCapacity:    metrics.MemoryCapacity,
			CPUUtilization:    metrics.CPUUtilization,
			MemoryUtilization: metrics.MemoryUtilization,
			IsWarmupNode:      metrics.IsWarmupNode,
			PodCount:          metrics.PodCount,
			Labels:            metrics.Labels,
			Annotations:       metrics.Annotations,
		}

		resources[node.Name] = info
	}

	return resources
}

// calculateSingleSchedulerAssignment calculates assignment for a single scheduler
// This implementation uses hard filtering instead of complex scoring
func (sm *ShardingManager) calculateSingleSchedulerAssignment(
	config SchedulerConfig,
	ctx *AssignmentContext,
) (*ShardAssignment, error) {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		klog.V(4).Infof("Calculated single scheduler assignment for %s in %v", config.Name, duration)
	}()

	nodeResources := sm.prepareNodeResourcesInfo(ctx.AllNodes)

	// Create node map for quick lookup
	nodeMap := make(map[string]*corev1.Node)
	for _, node := range ctx.AllNodes {
		nodeMap[node.Name] = node
	}

	// Apply hard filtering to get eligible nodes
	eligibleNodes := sm.filterEligibleNodes(config, nodeResources, nodeMap, ctx.AssignedNodes)

	// Prioritize nodes based on warmup preference and utilization
	prioritizedNodes := sm.prioritizeNodes(config, eligibleNodes, nodeResources)

	// Select nodes within constraints
	selectedNodes := sm.selectNodesWithinConstraints(config, prioritizedNodes)

	// Mark nodes as assigned in context
	for _, nodeName := range selectedNodes {
		ctx.AssignedNodes[nodeName] = config.Name
	}

	// Create assignment
	assignment := &ShardAssignment{
		SchedulerName: config.Name,
		NodesDesired:  selectedNodes,
		StrategyUsed:  "hard-filtering",
		Version:       fmt.Sprintf("%d", time.Now().UnixNano()),
		Reason:        sm.generateAssignmentReason(config, len(selectedNodes), len(eligibleNodes)),
	}

	klog.Infof("Scheduler %s single assignment completed: %d nodes selected from %d eligible",
		config.Name, len(selectedNodes), len(eligibleNodes))

	return assignment, nil
}

// generateAssignmentReason creates a human-readable reason for the assignment
func (sm *ShardingManager) generateAssignmentReason(config SchedulerConfig, selectedCount, eligibleCount int) string {
	minCPU := config.ShardStrategy.CPUUtilizationRange.Min
	maxCPU := config.ShardStrategy.CPUUtilizationRange.Max
	warmupPref := "with warmup preference"
	if !config.ShardStrategy.PreferWarmupNodes {
		warmupPref = "without warmup preference"
	}

	return fmt.Sprintf("Selected %d nodes from %d eligible nodes in CPU range [%.2f, %.2f] %s",
		selectedCount, eligibleCount, minCPU, maxCPU, warmupPref)
}
