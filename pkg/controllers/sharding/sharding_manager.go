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

package sharding

import (
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	shardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/sharding/policy"

	// Blank-import registers all built-in shard policies (allocation-rate,
	// warmup) with the policy registry at process init. Removing this import
	// will leave the registry empty and break policy lookup.
	_ "volcano.sh/volcano/pkg/controllers/sharding/policy/builtin"
)

const (
	defaultBatchSize = 50
)

// resolvedPolicy pairs a configured PolicyRef with its constructed instance
// and pre-resolved interface views. The Filter/Score/Select nil checks let
// the hot path skip type assertions on every reconcile.
type resolvedPolicy struct {
	Ref      PolicyRef
	Instance policy.ShardPolicy
	Filter   policy.Filterer // nil if Instance does not implement Filterer
	Score    policy.Scorer   // nil if Instance does not implement Scorer
	Select   policy.Selector // nil if Instance does not implement Selector
}

// ShardingManager calculates shard assignments
type ShardingManager struct {
	schedulerConfigs []SchedulerConfig
	metricsProvider  NodeMetricsProvider
	// policyCache holds, for each scheduler, the ordered list of resolved
	// policies that participate in its pipeline. Keyed by SchedulerConfig.Name.
	policyCache map[string][]resolvedPolicy
}

// NewShardingManager creates a new sharding manager
func NewShardingManager(schedulerConfigs []SchedulerConfig, nodeMetricsProvider NodeMetricsProvider) *ShardingManager {
	manager := &ShardingManager{
		schedulerConfigs: schedulerConfigs,
		metricsProvider:  nodeMetricsProvider,
		policyCache:      make(map[string][]resolvedPolicy),
	}

	if err := manager.initializePolicies(); err != nil {
		klog.Errorf("Failed to initialize policies: %v", err)
	}

	return manager
}

// initializePolicies constructs and Initializes one policy instance per
// PolicyRef in each scheduler's chain. Policies are constructed once at
// startup and reused across reconciles.
func (sm *ShardingManager) initializePolicies() error {
	for _, config := range sm.schedulerConfigs {
		resolved := make([]resolvedPolicy, 0, len(config.Policies))
		for _, ref := range config.Policies {
			builder, err := policy.GetPolicy(ref.Name)
			if err != nil {
				return fmt.Errorf("policy %s not found for scheduler %s: %v",
					ref.Name, config.Name, err)
			}
			instance := builder()
			if err := instance.Initialize(policy.Arguments(ref.Arguments)); err != nil {
				return fmt.Errorf("failed to initialize policy %s for scheduler %s: %v",
					ref.Name, config.Name, err)
			}
			rp := resolvedPolicy{Ref: ref, Instance: instance}
			if f, ok := instance.(policy.Filterer); ok {
				rp.Filter = f
			}
			if s, ok := instance.(policy.Scorer); ok {
				rp.Score = s
			}
			if sel, ok := instance.(policy.Selector); ok {
				rp.Select = sel
			}
			resolved = append(resolved, rp)
			klog.V(3).Infof("Initialized policy %s (weight=%d) for scheduler %s with args: %v",
				instance.Name(), ref.Weight, config.Name, ref.Arguments)
		}
		sm.policyCache[config.Name] = resolved
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

	if len(nodes) > defaultBatchSize {
		return sm.calculateShardAssignmentsBatched(nodes, currentShards)
	}

	allMetrics := sm.metricsProvider.GetAllNodeMetrics()
	policyMetrics := sm.convertNodeMetrics(allMetrics)

	assignedNodes := make(map[string]string) // node name -> scheduler name

	currentShardsMap := make(map[string]*shardv1alpha1.NodeShard)
	for i := range currentShards {
		currentShardsMap[currentShards[i].Name] = currentShards[i]
	}

	assignments := make(map[string]*ShardAssignment)

	for _, config := range sm.schedulerConfigs {
		klog.V(4).Infof("Calculating assignment for scheduler %s with %d policies",
			config.Name, len(sm.policyCache[config.Name]))

		ctx := &policy.PolicyContext{
			SchedulerName: config.Name,
			SchedulerType: config.Type,
			AllNodes:      nodes,
			NodeMetrics:   policyMetrics,
			AssignedNodes: assignedNodes,
			CurrentShard:  currentShardsMap[config.Name],
		}

		selected := sm.runPipeline(config, ctx)

		for _, nodeName := range selected {
			assignedNodes[nodeName] = config.Name
		}

		assignments[config.Name] = &ShardAssignment{
			SchedulerName: config.Name,
			NodesDesired:  selected,
			Version:       fmt.Sprintf("%d", time.Now().UnixNano()),
		}

		klog.Infof("Scheduler %s assigned %d nodes via %s: %v",
			config.Name, len(selected), summarizePolicies(config.Policies), selected)
	}

	klog.Infof("Completed shard assignments: %d nodes assigned to %d schedulers",
		len(assignedNodes), len(assignments))

	return assignments, nil
}

// runPipeline executes Filter → Score → Select for one scheduler.
//
// Filter is AND-intersection: a node passes only when every configured
// Filterer accepts it. When no policy implements Filterer, every candidate
// advances.
//
// Score is the weighted sum Σ (policy.Weight × Score(node)). Weight defaults
// to 1 when omitted (see toPolicyRefs). To disable a Scorer's contribution,
// remove it from the chain rather than relying on weight = 0.
//
// Select runs every Selector in config order; each receives the previous's
// output. An empty result stops the chain. The framework synthesizes a
// node-limit Selector from SchedulerConfigSpec.MinNodes/MaxNodes via
// applyPolicyDefaults so MaxNodes is honored without an inline clamp.
func (sm *ShardingManager) runPipeline(config SchedulerConfig, ctx *policy.PolicyContext) []string {
	resolved := sm.policyCache[config.Name]
	candidates := dropAssigned(ctx.AllNodes, ctx.AssignedNodes)

	filterers := make([]policy.Filterer, 0, len(resolved))
	for _, rp := range resolved {
		if rp.Filter != nil {
			filterers = append(filterers, rp.Filter)
		}
	}
	if len(filterers) > 0 {
		kept := candidates[:0]
		for _, n := range candidates {
			pass := true
			for _, f := range filterers {
				if !f.Filter(ctx, n) {
					pass = false
					break
				}
			}
			if pass {
				kept = append(kept, n)
			}
		}
		candidates = kept
	}

	// Parallel slice indexed by candidate position avoids per-node string
	// hashing in the sort comparator.
	if len(candidates) > 1 {
		scores := make([]float64, len(candidates))
		anyScorer := false
		for _, rp := range resolved {
			if rp.Score == nil {
				continue
			}
			anyScorer = true
			w := float64(rp.Ref.Weight)
			for i, n := range candidates {
				scores[i] += w * rp.Score.Score(ctx, n)
			}
		}
		if anyScorer {
			indices := make([]int, len(candidates))
			for i := range indices {
				indices[i] = i
			}
			sort.SliceStable(indices, func(i, j int) bool {
				return scores[indices[i]] > scores[indices[j]]
			})
			sorted := make([]*corev1.Node, len(candidates))
			for i, idx := range indices {
				sorted[i] = candidates[idx]
			}
			candidates = sorted
		}
	}

	// Phase 3: Select. Every Selector runs in config order; an empty
	// return stops the chain.
	for _, rp := range resolved {
		if rp.Select == nil {
			continue
		}
		candidates = rp.Select.Select(ctx, candidates)
		if len(candidates) == 0 {
			klog.V(4).Infof("Selector %s returned 0 nodes for scheduler %s; stopping chain",
				rp.Ref.Name, config.Name)
			break
		}
	}

	names := make([]string, len(candidates))
	for i, n := range candidates {
		names[i] = n.Name
	}
	return names
}

// dropAssigned returns nodes not already claimed by another scheduler. The
// returned slice is freshly allocated so phase loops can reslice it freely.
func dropAssigned(nodes []*corev1.Node, assigned map[string]string) []*corev1.Node {
	out := make([]*corev1.Node, 0, len(nodes))
	for _, n := range nodes {
		if _, taken := assigned[n.Name]; taken {
			continue
		}
		out = append(out, n)
	}
	return out
}

func (sm *ShardingManager) calculateShardAssignmentsBatched(
	nodes []*corev1.Node,
	currentShards []*shardv1alpha1.NodeShard,
) (map[string]*ShardAssignment, error) {
	batchSize := defaultBatchSize
	assignments := make(map[string]*ShardAssignment)

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

		for scheduler, assignment := range batchAssignments {
			if existing, exists := assignments[scheduler]; exists {
				existing.NodesDesired = append(existing.NodesDesired, assignment.NodesDesired...)
			} else {
				assignments[scheduler] = assignment
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	return assignments, nil
}

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

func (sm *ShardingManager) calculateSingleSchedulerAssignment(
	config SchedulerConfig,
	ctx *AssignmentContext,
) (*ShardAssignment, error) {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		klog.V(4).Infof("Calculated single scheduler assignment for %s in %v", config.Name, duration)
	}()

	allMetrics := sm.metricsProvider.GetAllNodeMetrics()
	policyMetrics := sm.convertNodeMetrics(allMetrics)

	policyCtx := &policy.PolicyContext{
		SchedulerName: config.Name,
		SchedulerType: config.Type,
		AllNodes:      ctx.AllNodes,
		NodeMetrics:   policyMetrics,
		AssignedNodes: ctx.AssignedNodes,
		CurrentShard:  ctx.CurrentShards[config.Name],
	}

	selected := sm.runPipeline(config, policyCtx)

	for _, nodeName := range selected {
		ctx.AssignedNodes[nodeName] = config.Name
	}

	klog.Infof("Scheduler %s single assignment: %d nodes selected via %s",
		config.Name, len(selected), summarizePolicies(config.Policies))

	return &ShardAssignment{
		SchedulerName: config.Name,
		NodesDesired:  selected,
		Version:       fmt.Sprintf("%d", time.Now().UnixNano()),
	}, nil
}
