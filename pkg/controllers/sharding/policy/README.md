# Volcano Shard Policy Framework

This package implements the pluggable policy framework used by the NodeShard
controller to assign nodes to schedulers. Each policy implements the
`ShardPolicy` interface (see `interface.go`) and is configured via the
sharding ConfigMap.

## Package layout

```
policy/
├── interface.go      // ShardPolicy / Filterer / Scorer / Selector, PolicyContext
├── registry.go       // Thread-safe RegisterPolicy / GetPolicy
├── arguments.go      // Type-safe argument parsing helpers
├── types.go          // Shared types
├── builtin/          // init() that registers all built-in policies
├── allocationrate/   // CPU-utilization range filter + score
├── nodelimit/        // Node-count Selector (truncate to maxNodes)
└── warmup/           // Warmup-label score
```

## Configuration

Policies are configured per-scheduler via the sharding ConfigMap (key
`sharding.yaml`). The full ConfigMap schema and reload behaviour are
documented in
[`docs/user-guide/how_to_configure_sharding_configmap.md`](../../../../docs/user-guide/how_to_configure_sharding_configmap.md);
this section covers only the `policies` and `arguments` fields.

### Policy chain

A scheduler declares an ordered list of policies. Each policy
contributes to the pipeline phases its struct implements: a `Filterer`
contributes to filter, a `Scorer` contributes to sort. The framework
runs `filter → sort → select` per scheduler:

1. **filter** — AND-intersection: a node passes only when every configured
   `Filterer` accepts it. When no policy implements `Filterer`, all
   candidates advance.
2. **sort** — weighted sum: `final[node] = Σ (policy.weight × policy.Score(node))`,
   stable-sorted descending.
3. **select** — every `Selector` runs in config order; each receives the previous's output. The built-in `node-limit` Selector truncates to `maxNodes`. `minNodes` is informational only: the framework cannot synthesize nodes, so a shorter result is logged but not padded.

```yaml
schedulerConfigs:
  - name: volcano
    type: volcano
    policies:
      - name: allocation-rate
        weight: 1
        arguments:
          minCPUUtil: 0.0
          maxCPUUtil: 0.6
      - name: warmup
        weight: 2                # higher weight = prefer warmup nodes first
        arguments:
          warmupLabel: "node.volcano.sh/warmup"
          warmupLabelValue: "true"
      - name: node-limit
        arguments:
          minNodes: 2
          maxNodes: 100

  - name: agent-scheduler
    type: agent
    policies:
      - name: allocation-rate
        weight: 1
        arguments:
          minCPUUtil: 0.7
          maxCPUUtil: 1.0
      - name: warmup
        weight: 5                # very strong warmup preference for agent shard
      - name: node-limit
        arguments:
          minNodes: 2
          maxNodes: 50
```

How the volcano scheduler config above behaves:

- **Filter:** `allocation-rate` rejects nodes outside `[0.0, 0.6]` CPU
  util or with missing metrics. `warmup` is `Scorer`-only, so it does
  not contribute to filter.
- **Sort:** combined score is `1 × normalized_util + 2 × warmup_signal`,
  where `normalized_util` rescales CPU utilization within `[0.0, 0.6]` to
  `[0, 1]`. A 0.05-util warmup node scores `1 × 0.083 + 2 × 1 ≈ 2.08`; a
  0.55-util non-warmup node scores `1 × 0.917 + 2 × 0 ≈ 0.92`. Warmup
  nodes rank first within the surviving filter cohort.
- **Select:** `node-limit` trims to `[2, 100]` (best-effort min, hard max).

Field reference for a policies entry:

| Field | Type | Notes |
|---|---|---|
| `name` | string, required | registered policy name (`allocation-rate`, `warmup`, `node-limit`) |
| `weight` | int ≥ 0 | default `1`. Only meaningful for `Scorer` policies. |
| `arguments` | map | policy-specific. Use the `node-limit` policy for `minNodes`/`maxNodes`. |

Scheduler-level `minNodes` / `maxNodes` are deprecated. They still synthesize
a `node-limit` policy for compatibility and emit a warning, but new
configuration should add `node-limit` explicitly.

## Built-in policies

### `allocation-rate`

Implements `Filterer` + `Scorer`. Filter rejects nodes outside
`[minCPUUtil, maxCPUUtil]` (or with missing metrics). Score normalizes
CPU utilization within the configured range to `[0, 1]`, so higher util
ranks higher — workloads pack onto already-busy nodes, leaving
lightly-loaded nodes available for other schedulers. This is the
default policy.

| Argument | Type | Description |
|---|---|---|
| `minCPUUtil` | float | Lower bound of the CPU utilization range, `[0.0, 1.0]`. |
| `maxCPUUtil` | float | Upper bound of the CPU utilization range, `[0.0, 1.0]`. |

### `warmup`

Implements `Scorer` only. Score returns `1.0` if the node carries the
configured warmup label/value, else `0.0`. Combined with `weight: N` in
config, warmup-labeled nodes receive a `+N` boost in the framework's
weighted-sum sort — pulled ahead of non-warmup candidates without
filtering non-warmup nodes out entirely.

| Argument | Type | Description |
|---|---|---|
| `warmupLabel` | string | Label key identifying warmup nodes. Default: `node.volcano.sh/warmup`. |
| `warmupLabelValue` | string | Label value identifying warmup nodes. Default: `"true"`. |

### `node-limit`

Implements `Selector` only. Truncates the sorted candidate list to at most
`maxNodes`. Add `node-limit` explicitly when a scheduler needs node-count
selection. Deprecated scheduler-level `minNodes` / `maxNodes` still synthesize
this policy for compatibility, but that path logs a warning.

| Argument | Type | Description |
|---|---|---|
| `minNodes` | int | Informational lower bound; the framework cannot synthesize nodes. |
| `maxNodes` | int | Hard upper cap. `<= 0` means no upper bound. |

## Adding a custom policy

### 1. Implement `ShardPolicy` plus `Filterer`, `Scorer`, and/or `Selector`

Every policy implements `ShardPolicy` for lifecycle (`Name`,
`Initialize`, `Cleanup`). Pipeline participation is opt-in: a policy
that filters implements `Filterer`, a policy that ranks implements
`Scorer`. Both is fine. The framework type-asserts at runtime to
discover which phases the policy contributes to.

```go
package mypolicy

import (
    "fmt"

    corev1 "k8s.io/api/core/v1"

    "volcano.sh/volcano/pkg/controllers/sharding/policy"
)

const PolicyName = "my-policy"

type myPolicy struct {
    param1 float64
}

func New() policy.ShardPolicy {
    return &myPolicy{param1: 0.5}
}

func (p *myPolicy) Name() string { return PolicyName }

func (p *myPolicy) Initialize(args policy.Arguments) error {
    args.GetFloat64(&p.param1, "param1")
    if p.param1 < 0 || p.param1 > 1 {
        return fmt.Errorf("invalid param1: must be in [0, 1]")
    }
    return nil
}

func (p *myPolicy) Cleanup() {}

// Filter implements policy.Filterer. The framework excludes already-assigned
// nodes before this is called; the predicate only needs to express the
// per-policy eligibility rule.
func (p *myPolicy) Filter(ctx *policy.PolicyContext, node *corev1.Node) bool {
    metrics := ctx.NodeMetrics[node.Name]
    if metrics == nil {
        return false
    }
    return metrics.CPUUtilization <= p.param1
}

// Score implements policy.Scorer. Return a value in [0, 1]; the framework
// does not clamp.
func (p *myPolicy) Score(ctx *policy.PolicyContext, node *corev1.Node) float64 {
    metrics := ctx.NodeMetrics[node.Name]
    if metrics == nil {
        return 0
    }
    return 1.0 - metrics.CPUUtilization // prefer idle nodes
}
```

### 2. Register the policy

Add an entry to `policy/builtin/builtin.go` (importing your subpackage from
the parent `policy` package would create an import cycle):

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
    policies:
      - name: my-policy
        weight: 1
        arguments:
          param1: 0.7
      - name: node-limit
        arguments:
          minNodes: 2
          maxNodes: 100
```

## Tests

```bash
go test ./pkg/controllers/sharding/policy/...
```
