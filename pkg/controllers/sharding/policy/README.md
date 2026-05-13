# Volcano Shard Policy Framework

This package implements the pluggable policy framework used by the NodeShard
controller to assign nodes to schedulers. Each policy implements the
`ShardPolicy` interface (see `interface.go`) and is configured via the
sharding ConfigMap.

## Package layout

```
policy/
├── interface.go      // ShardPolicy interface, PolicyContext, PolicyResult
├── registry.go       // Thread-safe RegisterPolicy / GetPolicy
├── arguments.go      // Type-safe argument parsing helpers
├── types.go          // Shared types
├── allocationrate/   // Default: CPU-utilization range filter
├── capability/       // Capacity-headroom filter
└── warmup/           // Warmup-label-prioritized selection
```

## Configuration

Policies are selected per-scheduler via the sharding ConfigMap (key
`sharding.yaml`). The full ConfigMap schema and reload behaviour are
documented in
[`docs/user-guide/how_to_configure_sharding_configmap.md`](../../../../docs/user-guide/how_to_configure_sharding_configmap.md);
this section covers only the `policy` and `arguments` fields.

```yaml
schedulerConfigs:
  - name: <scheduler-name>      # must match NodeShard.schedulerName
    type: <workload-class>      # e.g. "volcano", "agent"
    policy: <policy-name>       # see "Built-in policies" below
    minNodes: <int>
    maxNodes: <int>
    arguments:                  # policy-specific, see below
      <key>: <value>
```

A complete example lives at
[`example/sharding/sharding-config-configmap.yaml`](../../../../example/sharding/sharding-config-configmap.yaml).

If `policy` is omitted, the controller defaults to `utilization` and
translates `cpuUtilizationMin` / `cpuUtilizationMax` into the equivalent
arguments. The legacy `preferWarmupNodes` field is no longer honored — use
`policy: warmup` to prioritize warmup nodes; a deprecation warning is logged
when it is set. The colon-separated `--scheduler-configs` CLI flag is still
parsed for backward compatibility but is deprecated; new deployments should
use the ConfigMap.

> **Renamed**: the previous policy name `allocation-rate` was renamed to
> `utilization` because it is more precise (the policy filters by CPU
> utilization range, not a rate) and generalises cleanly to future resource
> dimensions like memory. `allocation-rate` is still accepted as a
> deprecated alias and emits a warning when used. Plan to remove the alias
> in a future release.

## Built-in policies

### `utilization`

Selects nodes whose resource utilization falls within configured per-resource
ranges, then bin-packs onto the highest-utilization eligible nodes (so that
lightly-loaded nodes remain free for other schedulers). This is the default
policy and the successor of the legacy `allocation-rate` name.

| Argument | Type | Description |
|---|---|---|
| `minCPUUtil` | float | Lower bound of the CPU utilization range, `[0.0, 1.0]`. Default `0.0`. |
| `maxCPUUtil` | float | Upper bound of the CPU utilization range, `[0.0, 1.0]`. Default `1.0`. |
| `minMemoryUtil` | float | _(planned)_ Lower bound of memory utilization, `[0.0, 1.0]`. |
| `maxMemoryUtil` | float | _(planned)_ Upper bound of memory utilization, `[0.0, 1.0]`. |

All configured ranges are ANDed: a node is eligible only if every configured
resource is within its `[min, max]` range. New resource dimensions extend the
schema by adding more `min<Resource>Util` / `max<Resource>Util` keys without
introducing a new policy.

```yaml
- name: volcano
  type: volcano
  policy: utilization
  minNodes: 2
  maxNodes: 100
  arguments:
    minCPUUtil: 0.0
    maxCPUUtil: 0.6
```

### `capability`

Selects nodes with enough free capacity for new workloads. Useful for
latency-sensitive schedulers (e.g. Agent) that need guaranteed headroom.

| Argument | Type | Description |
|---|---|---|
| `maxCapacityPercent` | float | Required headroom, `[0.0, 1.0]`. A node is eligible if `cpuUtilization <= 1.0 - maxCapacityPercent`. |

```yaml
- name: agent-scheduler
  type: agent
  policy: capability
  minNodes: 2
  maxNodes: 50
  arguments:
    maxCapacityPercent: 0.30
```

### `warmup`

Prioritizes pre-warmed nodes (identified by a label) for ultra-low-latency
scheduling.

| Argument | Type | Description |
|---|---|---|
| `warmupLabel` | string | Label key identifying warmup nodes. Default: `node.volcano.sh/warmup`. |
| `warmupLabelValue` | string | Label value identifying warmup nodes. Default: `"true"`. |
| `allowNonWarmup` | bool | If true, fall back to non-warmup nodes when warmup nodes alone do not meet `minNodes`. |

```yaml
- name: warmup-scheduler
  type: agent
  policy: warmup
  minNodes: 5
  maxNodes: 100
  arguments:
    allowNonWarmup: true
```

## Adding a custom policy

### 1. Implement the `ShardPolicy` interface

```go
package mypolicy

import (
    "fmt"

    "volcano.sh/volcano/pkg/controllers/sharding/policy"
)

const PolicyName = "my-policy"

type myPolicy struct {
    param1 float64
    param2 int
}

func New() policy.ShardPolicy {
    return &myPolicy{param1: 0.5, param2: 10}
}

func (p *myPolicy) Name() string { return PolicyName }

func (p *myPolicy) Initialize(args policy.Arguments) error {
    args.GetFloat64(&p.param1, "param1")
    args.GetInt(&p.param2, "param2")
    if p.param1 < 0 || p.param1 > 1 {
        return fmt.Errorf("invalid param1: must be in [0, 1]")
    }
    return nil
}

func (p *myPolicy) Calculate(ctx *policy.PolicyContext) (*policy.PolicyResult, error) {
    selected := []string{}
    for _, node := range ctx.AllNodes {
        if _, assigned := ctx.AssignedNodes[node.Name]; assigned {
            continue
        }
        // your selection logic here
        selected = append(selected, node.Name)
    }
    return &policy.PolicyResult{
        SelectedNodes: selected,
        Reason:        "selected by my-policy",
    }, nil
}

func (p *myPolicy) Cleanup() {}
```

### 2. Register the policy

Add the registration to `policy/builtin/builtin.go` so it fires as part of
the standard registration `init()` (importing your policy subpackage from
`policy` itself would create an import cycle):

```go
// in pkg/controllers/sharding/policy/builtin/builtin.go
import "volcano.sh/volcano/pkg/controllers/sharding/policy/mypolicy"

func init() {
    // ...existing registrations...
    policy.RegisterPolicy(mypolicy.PolicyName, mypolicy.New)
}
```

### 3. Reference it from the sharding ConfigMap

```yaml
schedulerConfigs:
  - name: my-scheduler
    type: volcano
    policy: my-policy
    minNodes: 2
    maxNodes: 100
    arguments:
      param1: 0.7
      param2: 15
```

## Tests

```bash
go test ./pkg/controllers/sharding/policy/...
```
