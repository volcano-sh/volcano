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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	shardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/sharding/policy"
)

const (
	defaultBatchSize = 50
)

// ShardingManager calculates shard assignments
type ShardingManager struct {
	schedulerConfigs []SchedulerConfig
	metricsProvider  NodeMetricsProvider
	policyCache      map[string]policy.ShardPolicy // NEW: Policy instances
}

// NewShardingManager creates a new sharding manager
func NewShardingManager(schedulerConfigs []SchedulerConfig, nodeMetricsProvider NodeMetricsProvider) *ShardingManager {
	manager := &ShardingManager{
		schedulerConfigs: schedulerConfigs,
		metricsProvider:  nodeMetricsProvider,
		policyCache:      make(map[string]policy.ShardPolicy),
	}

	// Initialize policies
	if err := manager.initializePolicies(); err != nil {
		klog.Fatalf("Failed to initialize policies: %v", err)
	}

	return manager
}

// initializePolicies initializes policy instances for all schedulers
func (sm *ShardingManager) initializePolicies() error {
	for _, config := range sm.schedulerConfigs {
		// Get policy builder from registry
		builder, err := policy.GetPolicy(config.PolicyName)
		if err != nil {
			return fmt.Errorf("policy %s not found for scheduler %s: %v",
				config.PolicyName, config.Name, err)
		}

		// Create policy instance
		policyInstance := builder()

		// Initialize with arguments
		policyArgs := policy.Arguments(config.PolicyArguments)
		if err := policyInstance.Initialize(policyArgs); err != nil {
			return fmt.Errorf("failed to initialize policy %s for scheduler %s: %v",
				config.PolicyName, config.Name, err)
		}

		sm.policyCache[config.Name] = policyInstance
		klog.V(3).Infof("Initialized policy %s for scheduler %s with args: %v",
			policyInstance.Name(), config.Name, config.PolicyArguments)
	}

	return nil
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

		if duration > 1*time.Second {
			klog.Warningf("Slow shard assignment calculation: %v for %d nodes", duration, len(nodes))
		}
	}()

	// Batch processing for large clusters
	if len(nodes) > defaultBatchSize {
		return sm.calculateShardAssignmentsBatched(nodes, currentShards)
	}

	// Get all node metrics and convert once (not per-scheduler)
	allMetrics := sm.metricsProvider.GetAllNodeMetrics()
	policyMetrics := sm.convertNodeMetrics(allMetrics)

	// Track assigned nodes to ensure mutual exclusivity
	assignedNodes := make(map[string]string) // node name -> scheduler name

	// Prepare current shards map
	currentShardsMap := make(map[string]*shardv1alpha1.NodeShard)
	for i := range currentShards {
		currentShardsMap[currentShards[i].Name] = currentShards[i]
	}

	// Calculate assignments for each scheduler using policies
	assignments := make(map[string]*ShardAssignment)

	for _, config := range sm.schedulerConfigs {
		klog.V(4).Infof("Calculating assignment for scheduler: %s using policy %s", config.Name, config.PolicyName)

		// Get policy instance
		policyInstance := sm.policyCache[config.Name]
		if policyInstance == nil {
			klog.Errorf("Policy not initialized for scheduler %s", config.Name)
			continue
		}

		// Build policy context
		ctx := &policy.PolicyContext{
			SchedulerName:   config.Name,
			SchedulerType:   config.Type,
			AllNodes:        nodes,
			NodeMetrics:     policyMetrics,
			AssignedNodes:   assignedNodes,
			CurrentShard:    currentShardsMap[config.Name],
			PolicyArguments: policy.Arguments(config.PolicyArguments),
		}

		// Execute policy
		result, err := policyInstance.Calculate(ctx)
		if err != nil {
			klog.Errorf("Policy execution failed for scheduler %s: %v", config.Name, err)
			continue
		}

		// Mark nodes as assigned
		for _, nodeName := range result.SelectedNodes {
			assignedNodes[nodeName] = config.Name
		}

		// Create assignment
		assignment := &ShardAssignment{
			SchedulerName: config.Name,
			NodesDesired:  result.SelectedNodes,
			StrategyUsed:  policyInstance.Name(),
			Version:       fmt.Sprintf("%d", time.Now().UnixNano()),
			Reason:        result.Reason,
		}

		assignments[config.Name] = assignment

		klog.Infof("Scheduler %s assigned %d nodes using policy %s: %v",
			config.Name, len(result.SelectedNodes), policyInstance.Name(), result.SelectedNodes)
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

// DEPRECATED: The following methods have been moved to policy implementations
// - filterEligibleNodes -> allocationrate.filterEligibleNodes
// - prioritizeNodes -> allocationrate.prioritizeNodes
// - selectNodesWithinConstraints -> allocationrate.selectNodesWithinConstraints
// - calculateDesiredNodeCount -> allocationrate.calculateDesiredNodeCount

// convertNodeMetrics converts NodeMetrics to policy.NodeMetrics
func (sm *ShardingManager) convertNodeMetrics(metrics map[string]*NodeMetrics) map[string]*policy.NodeMetrics {
	policyMetrics := make(map[string]*policy.NodeMetrics, len(metrics))
	for nodeName, m := range metrics {
		if m == nil {
			continue
		}
		policyMetrics[nodeName] = &policy.NodeMetrics{
			NodeName:          m.NodeName,
			ResourceVersion:   m.ResourceVersion,
			LastUpdated:       m.LastUpdated,
			CPUCapacity:       m.CPUCapacity,
			CPUAllocatable:    m.CPUAllocatable,
			MemoryCapacity:    m.MemoryCapacity,
			MemoryAllocatable: m.MemoryAllocatable,
			CPUUtilization:    m.CPUUtilization,
			MemoryUtilization: m.MemoryUtilization,
			IsWarmupNode:      m.IsWarmupNode,
			PodCount:          m.PodCount,
			Labels:            m.Labels,
			Annotations:       m.Annotations,
		}
	}
	return policyMetrics
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
func (sm *ShardingManager) calculateSingleSchedulerAssignment(
	config SchedulerConfig,
	ctx *AssignmentContext,
) (*ShardAssignment, error) {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		klog.V(4).Infof("Calculated single scheduler assignment for %s in %v", config.Name, duration)
	}()

	// Get policy instance
	policyInstance := sm.policyCache[config.Name]
	if policyInstance == nil {
		return nil, fmt.Errorf("policy not initialized for scheduler %s", config.Name)
	}

	// Get all node metrics
	allMetrics := sm.metricsProvider.GetAllNodeMetrics()
	policyMetrics := sm.convertNodeMetrics(allMetrics)

	// Build policy context
	policyCtx := &policy.PolicyContext{
		SchedulerName:   config.Name,
		SchedulerType:   config.Type,
		AllNodes:        ctx.AllNodes,
		NodeMetrics:     policyMetrics,
		AssignedNodes:   ctx.AssignedNodes,
		CurrentShard:    ctx.CurrentShards[config.Name],
		PolicyArguments: policy.Arguments(config.PolicyArguments),
	}

	// Execute policy
	result, err := policyInstance.Calculate(policyCtx)
	if err != nil {
		return nil, fmt.Errorf("policy execution failed: %v", err)
	}

	// Mark nodes as assigned in context
	for _, nodeName := range result.SelectedNodes {
		ctx.AssignedNodes[nodeName] = config.Name
	}

	// Create assignment
	assignment := &ShardAssignment{
		SchedulerName: config.Name,
		NodesDesired:  result.SelectedNodes,
		StrategyUsed:  policyInstance.Name(),
		Version:       fmt.Sprintf("%d", time.Now().UnixNano()),
		Reason:        result.Reason,
	}

	klog.Infof("Scheduler %s single assignment completed: %d nodes selected using policy %s",
		config.Name, len(result.SelectedNodes), policyInstance.Name())

	return assignment, nil
}

// DEPRECATED: generateAssignmentReason - now handled by policy.PolicyResult.Reason
