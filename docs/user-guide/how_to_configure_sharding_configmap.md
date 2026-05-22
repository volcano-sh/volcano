# How to Configure Sharding via ConfigMap

## Overview

The Volcano Sharding Controller supports live-reloadable configuration via
a Kubernetes ConfigMap. When a sharding ConfigMap is present, the controller
watches it and hot-reloads the scheduler shard specifications whenever the
data changes, without requiring a controller restart.

## Enabling the ConfigMap

When deploying with Helm, the ConfigMap is enabled by default via
`custom.sharding_configmap_enable: true` in `values.yaml`. The ConfigMap
name follows the pattern `{{ .Release.Name }}-sharding-configmap` and is
placed in the release namespace.

You can also deploy the ConfigMap manually using the example at
`example/sharding/sharding-config-configmap.yaml`:

```bash
kubectl apply -f example/sharding/sharding-config-configmap.yaml
```

The controller is told which ConfigMap to watch via two flags:
- `--sharding-configmap=<name>` (default: `volcano-sharding-configmap`)
- `--sharding-configmap-namespace=<namespace>` (default: `volcano-system`)

## ConfigMap Format

Each scheduler declares an ordered list of policies. The framework runs
`filter → sort → select` per scheduler, dispatching to whichever interface
each policy implements:

1. **filter** — every Filterer runs; a node passes only when *every* Filterer accepts it (AND-intersection).
2. **sort** — every Scorer runs; the framework combines scores via
   `final = Σ (policy.weight × policy.Score(node))` and stable-sorts
   descending.
3. **select** — every Selector runs in config order; each receives the
   previous's output. The built-in `node-limit` Selector truncates to
   `maxNodes`. `minNodes` is informational only (the framework cannot
   synthesize nodes that don't exist). Deprecated scheduler-level
   `minNodes` / `maxNodes` still synthesize `node-limit` for compatibility
   and emit a warning; new configuration should add `node-limit` explicitly.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: volcano-sharding-configmap
  namespace: volcano-system
data:
  sharding.yaml: |
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
            weight: 2          # higher weight, prefer warmup nodes first
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
            weight: 5          # very strong warmup preference for agent shard
          - name: node-limit
            arguments:
              minNodes: 2
              maxNodes: 50
    shardSyncPeriod: "60s"
    enableNodeEventTrigger: true
```

### policies entry reference

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Registered policy name (`allocation-rate`, `warmup`, `node-limit`). |
| `weight` | int | no (default 1) | Scales the policy's `Score` contribution. Only meaningful for policies that implement Scorer. |
| `arguments` | map | no | Policy-specific arguments. Use the `node-limit` policy for `minNodes`/`maxNodes`. |

### Built-in policies

- **`allocation-rate`** — implements Filterer + Scorer. Filter rejects nodes
  outside `[minCPUUtil, maxCPUUtil]` or with missing metrics. Score normalizes
  CPU utilization within the configured range to `[0, 1]`, so workloads pack
  onto already-busy nodes.
- **`warmup`** — implements Scorer only. Returns `1.0` if the node carries
  the configured warmup label/value, else `0.0`. Combined with `weight: N`,
  warmup-labeled nodes get a `+N` boost in the weighted sort.
- **`node-limit`** — implements Selector only. Applies `minNodes` /
  `maxNodes` from its `arguments`; `maxNodes` truncates the selected nodes,
  while `minNodes` is informational only.

See `pkg/controllers/sharding/policy/README.md` for the full policy
framework reference and how to add a custom policy.

## Parameter Reference

### schedulerConfigs

A list of scheduler shard specifications. Each entry defines a scheduler
that participates in node sharding. At least one entry is required.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Scheduler name. Must match the `schedulerName` field in NodeShard CRDs. |
| `type` | string | yes | Scheduler type, e.g. `"volcano"` or `"agent"`. |
| `policies` | list | yes | Ordered policy chain. See the ConfigMap Format section above. |
| `minNodes` | int | no | Deprecated. Still synthesizes `node-limit` for compatibility and logs a warning; use `policies[].arguments.minNodes` on the `node-limit` policy instead. |
| `maxNodes` | int | no | Deprecated. Still synthesizes `node-limit` for compatibility and logs a warning; use `policies[].arguments.maxNodes` on the `node-limit` policy instead. |

### Top-level Options

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `shardSyncPeriod` | string | no | `"60s"` | Interval between periodic full shard reconciliations. Accepts Go duration strings (`"30s"`, `"2m"`, `"1h"`). |
| `enableNodeEventTrigger` | bool | no | `true` | Controls whether node/pod events immediately trigger shard reconciliation in addition to the periodic sync. |

## Behaviour

| Scenario | Behaviour |
|----------|-----------|
| ConfigMap exists at startup | ConfigMap config is used; `--scheduler-configs` flags are ignored. |
| ConfigMap absent at startup | Falls back to flag-based defaults. ConfigMap will be watched for later creation. |
| ConfigMap updated at runtime | Controller re-parses, validates, atomically swaps config, invalidates caches, and triggers a shard re-sync. No restart needed. |
| ConfigMap deleted | Warning logged; last known config is retained. |
| Invalid YAML or constraint violation | Error logged; previous valid config is kept. |

## Helm Values

When using Helm, configure the sharding ConfigMap through `values.yaml`:

```yaml
custom:
  sharding_configmap_enable: true
  sharding_configmap_data: |
    schedulerConfigs:
      - name: volcano
        type: volcano
        policies:
          - name: allocation-rate
            weight: 1
            arguments:
              minCPUUtil: 0.0
              maxCPUUtil: 0.6
          - name: node-limit
            arguments:
              minNodes: 1
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
            weight: 2
          - name: node-limit
            arguments:
              minNodes: 1
              maxNodes: 100
    shardSyncPeriod: "60s"
    enableNodeEventTrigger: true
```

## Validation Rules

The controller validates the ConfigMap contents on every load/reload:

1. `schedulerConfigs` must contain at least one entry.
2. Each entry must have a non-empty `name`.
3. Deprecated scheduler-level `minNodes` must be >= 0 and `maxNodes` must be
   >= `minNodes` when present.
4. Each `policies[].name` must be a registered policy.
5. `policies[].weight` must be >= 0 (omit for default of 1).
6. `policies[].arguments` may contain `minNodes` / `maxNodes` only for the
   `node-limit` policy.

If validation fails, the previous valid configuration is preserved and an
error is logged.
