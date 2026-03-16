# Node Scoring Extension Points: Best Practices and Guidelines

## Overview

Volcano scheduler provides multiple extension points for implementing custom node scoring logic. This document provides comprehensive guidelines for plugin developers to choose the appropriate extension point and implement high-performance scoring functions.

## Background

The node scoring phase (PrioritizeNodes) is a critical component of the scheduling pipeline. In large-scale clusters with thousands of nodes, inefficient scoring implementations can become a significant bottleneck. This document addresses common performance pitfalls and provides best practices for implementing efficient scoring plugins.

## Extension Points for Node Scoring

Volcano provides three primary extension points for node scoring:

### 1. NodeOrderFn

**Signature:**
```go
type NodeOrderFn func(*TaskInfo, *NodeInfo) (float64, error)
```

**Characteristics:**
- Called once per node for each task
- Automatically parallelized by the scheduler framework using `workqueue.ParallelizeUntil`
- Lock-free execution with pre-allocated result slices
- Ideal for CPU-bound, per-node calculations

**When to Use:**
- Simple per-node scoring calculations
- CPU-bound operations that don't require cross-node state
- Stateless scoring functions
- Most common use case for custom scoring logic

**Example Use Cases:**
- Resource utilization scoring
- Node affinity/anti-affinity calculations
- Simple preference-based scoring

**Performance Characteristics:**
- ✅ Automatically parallelized across worker pool (default: 16 workers)
- ✅ Lock-free execution
- ✅ Optimal for large node counts
- ✅ No manual concurrency management required

### 2. BatchNodeOrderFn

**Signature:**
```go
type BatchNodeOrderFn func(*TaskInfo, []*NodeInfo) (map[string]float64, error)
```

**Characteristics:**
- Called once with the entire node list
- Executes concurrently with NodeOrderMapFn/NodeOrderReduceFn pipeline
- Plugin developer is responsible for implementing parallelization
- Provides access to all nodes for cross-node analysis

**When to Use:**
- Operations requiring aggregated state across all nodes
- I/O batching (e.g., external API calls, webhook requests)
- Topology-aware scoring requiring cross-node relationships
- Pre-computation that benefits from seeing all nodes at once

**Example Use Cases:**
- Network topology-aware scheduling
- Bin-packing with global optimization
- External scoring services (extenders)
- Cross-node resource balancing

**Performance Characteristics:**
- ⚠️ Runs concurrently with map/reduce pipeline (as of optimization)
- ✅ Useful for I/O batching and reducing external API calls

> **Note:** If a plugin developer needs to use `BatchNodeOrderFn` to score a batch of nodes, it's important to use parallelism to improve efficiency and avoid performance degradation in large-scale clusters. For best practices, please refer to the [Performance Best Practices](#performance-best-practices) section.

### 3. NodeOrderMapFn / NodeOrderReduceFn

**Signatures:**
```go
type NodeOrderMapFn func(*TaskInfo, *NodeInfo) (map[string]float64, float64, error)
type NodeOrderReduceFn func(*TaskInfo, map[string]fwk.NodeScoreList) (map[string]float64, error)
```

**Characteristics:**
- Two-phase map-reduce pattern
- Map phase is automatically parallelized
- Reduce phase aggregates scores from all plugins
- Used internally by the framework

**When to Use:**
- Advanced use cases requiring plugin score aggregation
- Custom normalization or weighting logic
- Rarely needed for typical plugin development

**Note:** Most plugins should use `NodeOrderFn` or `BatchNodeOrderFn` instead.

## Performance Best Practices

### 1. Prefer NodeOrderFn for CPU-Bound Operations

**DO:**
```go
func (p *myPlugin) OnSessionOpen(ssn *framework.Session) {
    ssn.AddNodeOrderFn(p.Name(), func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
        // Simple per-node calculation
        score := calculateNodeScore(task, node)
        return score, nil
    })
}
```

**DON'T:**
```go
func (p *myPlugin) OnSessionOpen(ssn *framework.Session) {
    ssn.AddBatchNodeOrderFn(p.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
        scores := make(map[string]float64)
        // Serial loop - this is a performance bottleneck!
        for _, node := range nodes {
            scores[node.Name] = calculateNodeScore(task, node)
        }
        return scores, nil
    })
}
```

### 2. Parallelize BatchNodeOrderFn Implementations

If you must use `BatchNodeOrderFn`, always parallelize the node iteration:

**DO:**
```go
import (
    "context"
    "k8s.io/client-go/util/workqueue"
)

func (p *myPlugin) batchNodeOrderFn(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
    numNodes := len(nodes)
    scores := make([]float64, numNodes)
    
    // Parallel execution using workqueue
    scoreNode := func(index int) {
        node := nodes[index]
        scores[index] = calculateNodeScore(task, node)
    }
    
    workqueue.ParallelizeUntil(context.TODO(), 16, numNodes, scoreNode)
    
    // Aggregate results
    nodeScores := make(map[string]float64, numNodes)
    for i, node := range nodes {
        nodeScores[node.Name] = scores[i]
    }
    
    return nodeScores, nil
}
```

**DON'T:**
```go
func (p *myPlugin) batchNodeOrderFn(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
    nodeScores := make(map[string]float64)
    
    // Serial loop - bottleneck for large clusters!
    for _, node := range nodes {
        nodeScores[node.Name] = calculateNodeScore(task, node)
    }
    
    return nodeScores, nil
}
```

### 3. Use BatchNodeOrderFn Only When Necessary

**Valid Reasons to Use BatchNodeOrderFn:**
- Making external API calls (batch requests are more efficient)
- Computing global statistics (e.g., cluster-wide resource distribution)
- Topology-aware scoring requiring cross-node relationships
- Pre-scoring analysis that benefits from seeing all nodes

**Invalid Reasons:**
- Simple per-node calculations (use `NodeOrderFn` instead)
- Avoiding framework overhead (framework parallelization is optimized)
- Preference for batch APIs (unless there's actual I/O batching benefit)

### 4. Avoid Shared State and Locks

The scheduler framework is designed to minimize lock contention. Don't reintroduce it:

**DO:**
```go
// Use pre-computed read-only state
func (p *myPlugin) nodeOrderFn(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
    // Read from pre-computed cache (read-only, no locks needed)
    weight := p.nodeWeights[node.Name]
    return calculateScore(task, node, weight), nil
}
```

**DON'T:**
```go
// Avoid locks in hot path
func (p *myPlugin) nodeOrderFn(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
    p.mu.Lock()  // Lock contention across all workers!
    defer p.mu.Unlock()
    
    p.sharedState[node.Name]++
    return float64(p.sharedState[node.Name]), nil
}
```

### 5. Pre-compute State in OnSessionOpen

Expensive computations should happen once per scheduling cycle, not per node:

**DO:**
```go
func (p *myPlugin) OnSessionOpen(ssn *framework.Session) {
    // Pre-compute expensive state once
    p.clusterStats = p.computeClusterStatistics(ssn)
    
    ssn.AddNodeOrderFn(p.Name(), func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
        // Use pre-computed state (fast)
        return p.scoreWithStats(task, node, p.clusterStats), nil
    })
}
```

**DON'T:**
```go
func (p *myPlugin) OnSessionOpen(ssn *framework.Session) {
    ssn.AddNodeOrderFn(p.Name(), func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
        // Recompute expensive state for every node (slow!)
        clusterStats := p.computeClusterStatistics(ssn)
        return p.scoreWithStats(task, node, clusterStats), nil
    })
}
```

## Decision Tree: Choosing the Right Extension Point

```
Start: Need to implement node scoring?
│
├─ Is this a simple per-node calculation?
│  └─ YES → Use NodeOrderFn ✅
│
├─ Do you need to make external API calls?
│  ├─ YES → Can you batch the requests?
│  │  ├─ YES → Use BatchNodeOrderFn with parallelization ⚠️
│  │  └─ NO → Use NodeOrderFn (framework handles parallelism) ✅
│  └─ NO → Continue
│
├─ Do you need cross-node state or topology information?
│  ├─ YES → Use BatchNodeOrderFn with parallelization ⚠️
│  └─ NO → Use NodeOrderFn ✅
│
└─ Do you need custom score aggregation logic?
   └─ YES → Use NodeOrderMapFn/NodeOrderReduceFn (advanced) ⚠️
```

## Common Pitfalls

### Pitfall 1: Serial BatchNodeOrderFn Implementation

**Problem:** Implementing `BatchNodeOrderFn` with a simple for-loop creates a serial bottleneck.

**Impact:** In a cluster with 2000 nodes, a serial implementation can take 10-100x longer than a parallelized version.

**Solution:** Always use `workqueue.ParallelizeUntil` for node iteration in `BatchNodeOrderFn`.

### Pitfall 2: Using BatchNodeOrderFn for Simple Calculations

**Problem:** Using `BatchNodeOrderFn` when `NodeOrderFn` would suffice.

**Impact:** Unnecessary complexity and potential performance degradation if not properly parallelized.

**Solution:** Use `NodeOrderFn` unless you have a specific reason to see all nodes at once.

### Pitfall 3: Lock Contention in Scoring Functions

**Problem:** Using locks to protect shared state during scoring.

**Impact:** Serializes parallel execution, negating the benefits of the framework's parallelization.

**Solution:** Pre-compute state in `OnSessionOpen` or use lock-free data structures.

### Pitfall 4: Expensive Computations in Hot Path

**Problem:** Performing expensive calculations inside the per-node scoring function.

**Impact:** Multiplies the cost by the number of nodes, significantly slowing down scheduling.

**Solution:** Move expensive computations to `OnSessionOpen` or cache results.

## Performance Optimization Checklist

When implementing a scoring plugin, verify:

- [ ] Using `NodeOrderFn` for simple per-node calculations
- [ ] If using `BatchNodeOrderFn`, implemented parallelization with `workqueue.ParallelizeUntil`
- [ ] No locks or shared mutable state in scoring functions
- [ ] Expensive computations pre-computed in `OnSessionOpen`
- [ ] Read-only access to cached/pre-computed data in hot path
- [ ] Tested with large node counts (1000+ nodes) to verify performance
- [ ] Profiled to identify any unexpected bottlenecks

## Framework Optimizations (v1.15+)

As of Volcano v1.15, the following optimizations have been implemented in the scheduler framework:

1. **Lock-Free Score Aggregation:** The `PrioritizeNodes` function now uses pre-allocated slices instead of mutex-protected maps, eliminating lock contention during parallel score collection.

2. **Concurrent Batch and Map Execution:** `BatchNodeOrderFn` now executes concurrently with the `NodeOrderMapFn`/`NodeOrderReduceFn` pipeline, reducing overall latency.

3. **Optimized Memory Allocation:** Pre-allocated result structures reduce GC pressure during high-throughput scheduling.

These optimizations are transparent to plugin developers but significantly improve performance in large-scale clusters.

## Examples

### Example 1: Simple Resource-Based Scoring (NodeOrderFn)

```go
package myplugin

import (
    "volcano.sh/volcano/pkg/scheduler/api"
    "volcano.sh/volcano/pkg/scheduler/framework"
)

type resourceScorePlugin struct{}

func (p *resourceScorePlugin) Name() string {
    return "resource-score"
}

func (p *resourceScorePlugin) OnSessionOpen(ssn *framework.Session) {
    ssn.AddNodeOrderFn(p.Name(), func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
        // Simple CPU utilization scoring
        if node.Allocatable.MilliCPU == 0 {
            return 0, nil
        }
        cpuUsage := node.ResourceUsage.MilliCPU / node.Allocatable.MilliCPU
        return 1.0 - cpuUsage, nil
    })
}

func (p *resourceScorePlugin) OnSessionClose(ssn *framework.Session) {}
```

### Example 2: Topology-Aware Scoring (BatchNodeOrderFn with Parallelization)

```go
package myplugin

import (
    "context"
    "k8s.io/client-go/util/workqueue"
    "volcano.sh/volcano/pkg/scheduler/api"
    "volcano.sh/volcano/pkg/scheduler/framework"
)

type topologyScorePlugin struct {
    topologyCache map[string][]string // node -> rack mapping
}

func (p *topologyScorePlugin) Name() string {
    return "topology-score"
}

func (p *topologyScorePlugin) OnSessionOpen(ssn *framework.Session) {
    // Pre-compute topology information
    p.buildTopologyCache(ssn)
    
    ssn.AddBatchNodeOrderFn(p.Name(), func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
        return p.scoreNodesWithTopology(task, nodes)
    })
}

func (p *topologyScorePlugin) scoreNodesWithTopology(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
    numNodes := len(nodes)
    scores := make([]float64, numNodes)
    
    // Parallel execution
    scoreNode := func(index int) {
        node := nodes[index]
        // Calculate topology-aware score using pre-computed cache
        scores[index] = p.calculateTopologyScore(task, node)
    }
    
    workqueue.ParallelizeUntil(context.TODO(), 16, numNodes, scoreNode)
    
    // Aggregate results
    nodeScores := make(map[string]float64, numNodes)
    for i, node := range nodes {
        nodeScores[node.Name] = scores[i]
    }
    
    return nodeScores, nil
}

func (p *topologyScorePlugin) calculateTopologyScore(task *api.TaskInfo, node *api.NodeInfo) float64 {
    // Use pre-computed topology cache (read-only, no locks)
    rack := p.topologyCache[node.Name]
    // ... topology-aware scoring logic
    return 1.0
}

func (p *topologyScorePlugin) buildTopologyCache(ssn *framework.Session) {
    // Build topology cache once per scheduling cycle
    p.topologyCache = make(map[string][]string)
    // ... cache building logic
}

func (p *topologyScorePlugin) OnSessionClose(ssn *framework.Session) {}
```

## Testing Performance

When developing scoring plugins, always test with realistic node counts:

```go
func BenchmarkNodeScoring(b *testing.B) {
    // Create test data with 2000 nodes
    nodes := createTestNodes(2000)
    task := createTestTask()
    plugin := &myPlugin{}
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        scores, err := plugin.batchNodeOrderFn(task, nodes)
        if err != nil {
            b.Fatal(err)
        }
        _ = scores
    }
}
```

## References

- [Custom Plugin Development](../design/custom-plugin.md)
- [Scheduler Configuration](../user-guide/how_to_configure_scheduler.md)
- [Network Topology Aware Scheduling](../design/Network%20Topology%20Aware%20Scheduling.md)

## Changelog

- **v1.15.0**: Added lock-free score aggregation and concurrent batch/map execution
- **v1.15.0**: Initial documentation for node scoring best practices
