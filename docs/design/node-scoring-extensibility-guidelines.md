# Node Scoring Extensibility Guidelines

Volcano provides multiple extensibility points to customize the node scoring process during scheduling. This document outlines best practices and standard guidelines for plugin developers implementing these points. 

## Architectural Overview

The `PrioritizeNodes` phase in Volcano calculates the optimal placement for tasks by generating a score for every node. The framework calls multiple extension points (`NodeOrderFn`, `NodeOrderMapFn`/`NodeOrderReduceFn`, and `BatchNodeOrderFn`). Because node scoring is extremely frequent and often operates over thousands of nodes, performance and concurrency are critical.

## 1. NodeOrderFn vs BatchNodeOrderFn

### NodeOrderFn
- **Primary Use Case:** Independent, scalar-based node assignments. For example, simple checks matching node labels to task requirements or localized node statistics.
- **Concurrency Model:** Managed by the framework. The scheduling framework evaluates `NodeOrderFn` using `workqueue.ParallelizeUntil`, executing the evaluation for all nodes concurrently. 
- **Guideline:** Strictly direct CPU-bound operations to `NodeOrderFn` whenever possible to automatically benefit from framework-level parallelism without implementing your own locking primitives.

### BatchNodeOrderFn
- **Primary Use Case:** When a plugin requires evaluating aggregated state across the *entire* node list simultaneously before scoring (similar to PreScore semantics) or when performing operations that benefit from I/O consolidation (such as Extender webhooks).
- **Concurrency Model:** Managed by the plugin. Unlike `NodeOrderFn`, the framework passes the entire node slice to the plugin in a single goroutine. 
- **Guideline:** Do **not** use sequential/serial `for _, node := range nodes` loops for CPU-bound evaluations in `BatchNodeOrderFn`, as this will serialize the scoring process and bottleneck the entire scheduler. Plugin developers **must** utilize `workqueue.ParallelizeUntil` to evaluate node scores concurrently if they iterate through nodes within this function.

## 2. Lock-Free Map Execution

When using `workqueue.ParallelizeUntil` within your `BatchNodeOrderFn`, you must avoid global locks (e.g., `sync.Mutex`) when assigning scores, as this leads to heavy lock contention.

**Optimization approach:**
Pre-allocate an array/slice of the same length as the nodes slice (since the index is guaranteed unique per goroutine) to collect the intermediate scores. After the `workqueue.ParallelizeUntil` successfully exits, aggregate these array values into the returned `map[string]float64` cleanly in a fast serial loop.

### Bad Example (Lock Contention)

```go
var mu sync.Mutex
nodeScores := make(map[string]float64)

workqueue.ParallelizeUntil(context.TODO(), 16, len(nodes), func(index int) {
    node := nodes[index]
    score := calculateHeavyScore(node)
    
    // BAD: Heavy lock contention on thousands of nodes
    mu.Lock()
    nodeScores[node.Name] = score
    mu.Unlock()
})
```

### Good Example (Lock-Free)

```go
nodeScores := make(map[string]float64)
nodeScoresList := make([]float64, len(nodes))

// Lock-Free concurrent execution
workqueue.ParallelizeUntil(context.TODO(), 16, len(nodes), func(index int) {
    node := nodes[index]
    nodeScoresList[index] = calculateHeavyScore(node)
})

// Fast serial aggregation
for i, node := range nodes {
    nodeScores[node.Name] = nodeScoresList[i]
}
```

## Summary
- Use `NodeOrderFn` for simple, independent node scoring to get automatic framework parallelism.
- Use `BatchNodeOrderFn` only for I/O consolidation or cross-node aggregated calculations.
- Always use `workqueue.ParallelizeUntil` and lock-free slice pre-allocation if iterating through nodes inside `BatchNodeOrderFn`.
