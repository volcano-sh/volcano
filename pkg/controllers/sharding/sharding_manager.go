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

		// 修复: 精确的范围比较，使用小数点后两位精度
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

	// 创建节点索引
	nodeIndex := make(map[string]int)
	for i, node := range nodes {
		nodeIndex[node.Name] = i
	}

	// 首先按 warmup 状态分组
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

	// 对每组内部按 CPU 利用率排序
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

	// 合并结果：优先选择 warmup 节点，然后是非 warmup 节点
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
	// 计算所需节点数
	desiredCount := sm.calculateDesiredNodeCount(config, len(nodes))

	// 确保不低于最小节点数
	if desiredCount < config.ShardStrategy.MinNodes {
		desiredCount = config.ShardStrategy.MinNodes
	}

	// 如果合格节点数小于所需节点数，使用所有合格节点
	if len(nodes) < desiredCount {
		desiredCount = len(nodes)
	}

	// 选择节点
	selected := make([]string, 0, desiredCount)

	// 首先，按优先级选择节点
	for i := 0; i < desiredCount && i < len(nodes); i++ {
		selected = append(selected, nodes[i].Name)
	}

	// 检查是否满足最小节点约束
	if len(selected) < config.ShardStrategy.MinNodes && len(nodes) > len(selected) {
		// 添加额外节点直到满足最小约束
		additionalNeeded := config.ShardStrategy.MinNodes - len(selected)
		for i := len(selected); i < len(nodes) && additionalNeeded > 0; i++ {
			selected = append(selected, nodes[i].Name)
			additionalNeeded--
		}
	}

	// 移除重复节点
	uniqueSelected := make(map[string]bool)
	finalSelection := make([]string, 0, len(selected))

	for _, node := range selected {
		if !uniqueSelected[node] {
			uniqueSelected[node] = true
			finalSelection = append(finalSelection, node)
		}
	}

	return finalSelection
}

// calculateDesiredNodeCount calculates desired node count based on strategy constraints
func (sm *ShardingManager) calculateDesiredNodeCount(config SchedulerConfig, eligibleNodeCount int) int {
	// 基本策略: 选择所有合格节点
	desired := eligibleNodeCount

	// 应用 max 约束
	if desired > config.ShardStrategy.MaxNodes {
		desired = config.ShardStrategy.MaxNodes
	}

	// 应用 min 约束
	if desired < config.ShardStrategy.MinNodes {
		desired = config.ShardStrategy.MinNodes
	}

	return desired
}

// validateMutualExclusivity ensures no node is assigned to multiple schedulers
// func (sm *ShardingManager) validateMutualExclusivity(assignments []*ShardAssignment) error {
// 	nodeToScheduler := make(map[string]string)

// 	for _, assignment := range assignments {
// 		for _, node := range assignment.NodesDesired {
// 			if prevScheduler, exists := nodeToScheduler[node]; exists {
// 				return fmt.Errorf("node %s is assigned to both %s and %s", node, prevScheduler, assignment.SchedulerName)
// 			}
// 			nodeToScheduler[node] = assignment.SchedulerName
// 		}
// 	}

// 	return nil
// }

// prepareNodeResourcesInfo prepares resource information for nodes
func (sm *ShardingManager) prepareNodeResourcesInfo(nodes []*corev1.Node) map[string]*NodeResourceInfo {
	// Get all metrics from provider
	allMetrics := sm.metricsProvider.GetAllNodeMetrics()

	resources := make(map[string]*NodeResourceInfo)

	for _, node := range nodes {
		metrics := allMetrics[node.Name]
		if metrics == nil {
			// Create default metrics if not available
			metrics = &NodeMetrics{
				CPUUtilization:    0.3,
				MemoryUtilization: 0.4,
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

// func (sm *ShardingManager) prepareNodeResourcesInfo(nodes []*corev1.Node) map[string]*NodeResourceInfo {
// 	resources := make(map[string]*NodeResourceInfo)

// 	for i := range nodes {
// 		node := nodes[i]
// 		info := &NodeResourceInfo{
// 			NodeName:     node.Name,
// 			Labels:       node.Labels,
// 			Annotations:  node.Annotations,
// 			IsWarmupNode: node.Labels["node.volcano.sh/warmup"] == "true",
// 		}

// 		// Get allocatable resources
// 		info.CPUAllocatable = node.Status.Allocatable[corev1.ResourceCPU]
// 		info.MemoryAllocatable = node.Status.Allocatable[corev1.ResourceMemory]
// 		info.CPUCapacity = node.Status.Capacity[corev1.ResourceCPU]
// 		info.MemoryCapacity = node.Status.Capacity[corev1.ResourceMemory]

// 		// Estimate utilization
// 		info.CPUUtilization = sm.estimateCPUUtilization(node)
// 		info.MemoryUtilization = sm.estimateMemoryUtilization(node)

// 		// Get pod count (simplified)
// 		info.PodCount = sm.estimatePodCount(node)

// 		resources[node.Name] = info
// 	}

// 	return resources
// }

// estimateCPUUtilization 从节点状态缓存获取CPU利用率
// func (sm *ShardingManager) estimateCPUUtilization(node *corev1.Node) float64 {
// 	state := sm.getNodeState(node.Name)
// 	return state.CPUUtilization
// }

// // estimateMemoryUtilization 从节点状态缓存获取内存利用率
// func (sm *ShardingManager) estimateMemoryUtilization(node *corev1.Node) float64 {
// 	state := sm.getNodeState(node.Name)
// 	return state.MemoryUtilization
// }

// // getNodeState 安全获取节点状态
// func (sm *ShardingManager) getNodeState(nodeName string) *NodeState {
// 	if sm.getNodeStateFn == nil {
// 		return &NodeState{CPUUtilization: 0.3, MemoryUtilization: 0.4}
// 	}

// 	state := sm.getNodeStateFn(nodeName)
// 	if state == nil {
// 		// 缓存未命中，返回默认值
// 		return &NodeState{CPUUtilization: 0.3, MemoryUtilization: 0.4}
// 	}

// 	return state
// }

// // estimateCPUUtilization estimates CPU utilization for a node
// func (sm *ShardingManager) estimateCPUUtilization(node *corev1.Node) float64 {
// 	// Try to get from annotation first
// 	if utilStr, exists := node.Annotations["node.volcano.sh/cpu-utilization"]; exists {
// 		if utilFloat, err := strconv.ParseFloat(utilStr, 64); err == nil {
// 			// Clamp to [0.0, 1.0] range
// 			if utilFloat < 0.0 {
// 				return 0.0
// 			}
// 			if utilFloat > 1.0 {
// 				return 1.0
// 			}
// 			return utilFloat
// 		}
// 	}

// 	// Default estimation
// 	return 0.3
// }

// // estimateMemoryUtilization estimates memory utilization for a node
// func (sm *ShardingManager) estimateMemoryUtilization(node *corev1.Node) float64 {
// 	// Try to get from annotation first
// 	if utilStr, exists := node.Annotations["node.volcano.sh/memory-utilization"]; exists {
// 		if utilFloat, err := strconv.ParseFloat(utilStr, 64); err == nil {
// 			// Clamp to [0.0, 1.0] range
// 			if utilFloat < 0.0 {
// 				return 0.0
// 			}
// 			if utilFloat > 1.0 {
// 				return 1.0
// 			}
// 			return utilFloat
// 		}
// 	}

// 	// Default estimation
// 	return 0.4
// }

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

	// Prepare node resources if not provided in context
	// if ctx.NodeResources == nil {
	// 	ctx.NodeResources = sm.prepareNodeResourcesInfo(ctx.AllNodes)
	// }
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

// estimatePodCount estimates the number of pods running on a node
// func (sm *ShardingManager) estimatePodCount(node *corev1.Node) int {
// 	// Try to get from annotation
// 	if countStr, exists := node.Annotations["node.volcano.sh/pod-count"]; exists {
// 		if count, err := strconv.Atoi(countStr); err == nil {
// 			return count
// 		}
// 	}

// 	// Default estimation based on node size
// 	cpuCapacity := node.Status.Capacity.Cpu().Value()
// 	if cpuCapacity >= 32 {
// 		return 110 // Large node
// 	} else if cpuCapacity >= 16 {
// 		return 80 // Medium-large node
// 	} else if cpuCapacity >= 8 {
// 		return 50 // Medium node
// 	} else if cpuCapacity >= 4 {
// 		return 30 // Small-medium node
// 	}

// 	return 15 // Small node default
// }
