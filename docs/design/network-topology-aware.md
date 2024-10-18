# Network Topology Aware Plugin

- [Network Topology Aware Plugin](#network-topology-aware-plugin)
  - [Backgrounds](#backgrounds)
  - [Motivation](#motivation)
  - [Proposal one](#proposal-one)
    - [Goals](#goals)
    - [Non-Goals](#non-goals)
    - [Design Action](#design-action)
      - [Pod scheduling process](#pod-scheduling-process)
      - [Usage](#usage)
    - [Drawbacks](#drawbacks)

## Backgrounds

A Kubernetes cluster typically comprises numerous nodes distributed across different IDCs, chassis, and switches.

Data transformations vary in performance across these different components.

For latency-sensitive workloads, it's crucial to execute tasks within the same IDC and ideally on the same chassis and switch.

## Motivation

The goal is to make the Kubernetes scheduler network-topology aware to achieve the following:

Ensure optimal scheduling of tasks from the same job onto nodes within the same topology, such as the same IDC, chassis, or switch.

There will be two types of network-topology aware

- **static**: `network-topology.type: static` is aiming to aware the network topology by nodes' labels
- **dynamic**: `network-topology.type: dynamic` is aiming to use some tools to detect the network topology dynamically. For example, `ibnetdiscover` can be used to discover the InfiniBand network topology

## Proposal one

This proposal requires cluster administrators to manage network topology labels on Kubernetes (K8s) nodes.

Nodes can be labeled to indicate identical topologies with the same label value.

### Goals

- **Single-Key Topology Configuration**: Support scheduling all tasks of a job onto nodes that share the same value for a specified key.
- **Multiple-Key Topology Policies**: Prioritize keys listed earlier for better scheduling preference.

### Non-Goals

- **Global Solutions**: This proposal does not aim to find solutions across nodes with all possible values of a topology key simultaneously.

### Design Action

#### Pod scheduling process

1. **Recording Topology Information**: When the first task of a job is assigned to a node, record the node's topology information in the scheduling plugin.
2. **Scoring Nodes for Subsequent Tasks**: During scheduling of subsequent tasks, nodes with the same topology as the initially allocated task receive a higher score; others receive a score of zero.
3. **Handling Multiple Keys**: If a node matches multiple keys from the configured list, the first key in the list is prioritized for scoring.

```go
nodeOrderFn := func(task *api.TaskInfo, node *api.NodeInfo) (float64, error){
    ...
    score := 0
    weight := np.weight
    tlabels := tNode.Node.Labels
    labels := node.Node.Labels
    lenth := len(np.topologyKeys)
    for i, key := range np.topologyKeys {
        if tlabels[key] == labels[key] {
            score += (lenth - i) //  key with more priority at front of which with less priority
            break
        }
    }
    return float64(score * weight), nil
}
```

#### Usage

1. Label nodes with key-value pairs (e.g., `switch=NvLink-A100`, `rack=rack1,rack2`, `idc=bj,sh`) to partition nodes into different topology zones
2. Add the `network-topology` plugin in the scheduler configuration to implement these policies.

```yaml
- plugins:
   - name: network-topology
     arguments:
       network-topology.type: static # static means it will use the node's labels to aware network topology
       network-topology.keys: rack,switch,idc # required when type is static
       network-topology.weight: 10
```

### Drawbacks

One drawback is that it's not a global solution that ensures all tasks of a job are placed on nodes within the same topology. For example, if nodes labeled with key-value1 lack sufficient resources while nodes labeled with key-value2 have them, and the first task is assigned to key-value1 nodes, subsequent tasks will still attempt to use key-value1 nodes, despite the resource constraints.
