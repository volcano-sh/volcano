# Node Scoring Extension Points

This document explains the available node scoring extension points in the Volcano scheduler, when to use each one, and the precautions to take when implementing them.

## Extension Points

### NodeOrderFn

```go
type NodeOrderFn func(*TaskInfo, *NodeInfo) (float64, error)
```

Called once per node. The framework automatically parallelizes execution across all candidate nodes using `workqueue.ParallelizeUntil`.

Use this for per-node scoring logic that does not require knowledge of other nodes.

### BatchNodeOrderFn

```go
type BatchNodeOrderFn func(*TaskInfo, []*NodeInfo) (map[string]float64, error)
```

Called once with the full node list. Executes concurrently with the `NodeOrderMapFn`/`NodeOrderReduceFn` pipeline.

Use this when scoring requires cross-node state, such as topology relationships, global resource distribution, or batching external API calls.

> **Note:** The framework does not parallelize `BatchNodeOrderFn` internally. If you iterate over nodes inside this function, you must implement parallelization yourself using `workqueue.ParallelizeUntil`, otherwise it will become a bottleneck in large clusters.

### NodeOrderMapFn / NodeOrderReduceFn

```go
type NodeOrderMapFn func(*TaskInfo, *NodeInfo) (map[string]float64, float64, error)
type NodeOrderReduceFn func(*TaskInfo, map[string]fwk.NodeScoreList) (map[string]float64, error)
```

A two-phase map-reduce pattern. The map phase is parallelized by the framework. Use this for advanced cases requiring custom score aggregation across plugins. Most plugins do not need this.

## When to Use Which

| Scenario | Extension Point |
|---|---|
| Per-node scoring, no cross-node state needed | `NodeOrderFn` |
| Scoring requires seeing all nodes (topology, global stats) | `BatchNodeOrderFn` |
| Batching external API calls for efficiency | `BatchNodeOrderFn` |
| Custom score aggregation across plugins | `NodeOrderMapFn` / `NodeOrderReduceFn` |

## Precautions

### Parallelizing BatchNodeOrderFn

A serial loop inside `BatchNodeOrderFn` will block the entire scoring phase. Always use `workqueue.ParallelizeUntil`:

```go
func (p *myPlugin) batchNodeOrderFn(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
    numNodes := len(nodes)
    scores := make([]float64, numNodes)

    workqueue.ParallelizeUntil(context.TODO(), 16, numNodes, func(index int) {
        scores[index] = p.scoreNode(task, nodes[index])
    })

    nodeScores := make(map[string]float64, numNodes)
    for i, node := range nodes {
        nodeScores[node.Name] = scores[i]
    }
    return nodeScores, nil
}
```

### Avoid Locks in the Scoring Hot Path

The framework parallelizes node scoring. Introducing a mutex inside the scoring function serializes execution and negates the parallelism. Pre-compute any shared state in `OnSessionOpen` and access it as read-only inside the scoring function.

### Pre-compute Expensive State

Scoring functions are called once per node per scheduling cycle. Any expensive computation inside them is multiplied by the number of nodes. Move such work to `OnSessionOpen`:

```go
func (p *myPlugin) OnSessionOpen(ssn *framework.Session) {
    // Compute once per scheduling cycle
    p.clusterStats = p.computeClusterStatistics(ssn)

    ssn.AddNodeOrderFn(p.Name(), func(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
        return p.score(task, node, p.clusterStats), nil
    })
}
```

## References

- [Custom Plugin Development](../design/custom-plugin.md)
- [Scheduler Configuration](../user-guide/how_to_configure_scheduler.md)
- [Network Topology Aware Scheduling](../design/Network%20Topology%20Aware%20Scheduling.md)
