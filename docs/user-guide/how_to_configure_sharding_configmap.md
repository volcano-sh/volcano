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

The configuration is stored under the `sharding.yaml` key inside the
ConfigMap. The format is:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: volcano-sharding-configmap
  namespace: volcano-system
data:
  sharding.yaml: |
    schedulerConfigs:
      - name: agent-scheduler
        type: agent
        cpuUtilizationMin: 0.7
        cpuUtilizationMax: 1.0
        preferWarmupNodes: true
        minNodes: 1
        maxNodes: 100
      - name: volcano
        type: volcano
        cpuUtilizationMin: 0.0
        cpuUtilizationMax: 0.69
        preferWarmupNodes: false
        minNodes: 1
        maxNodes: 100
    shardSyncPeriod: "60s"
    enableNodeEventTrigger: true
```

## Parameter Reference

### schedulerConfigs

A list of scheduler shard specifications. Each entry defines a scheduler
that participates in node sharding. At least one entry is required.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `name` | string | yes | Scheduler name. Must match the `schedulerName` field in NodeShard CRDs. |
| `type` | string | yes | Scheduler type, e.g. `"volcano"` or `"agent"`. |
| `cpuUtilizationMin` | float | yes | Lower bound (inclusive) of the CPU utilisation range `[0.0, 1.0]` that makes a node eligible for this scheduler's shard. |
| `cpuUtilizationMax` | float | yes | Upper bound (inclusive) of the CPU utilisation range. Must be >= `cpuUtilizationMin`. |
| `preferWarmupNodes` | bool | yes | When `true`, warmup nodes (label: `node.volcano.sh/warmup=true`) are sorted first when selecting shard members. |
| `minNodes` | int | yes | Minimum number of nodes the shard must contain. Must be >= 0. |
| `maxNodes` | int | yes | Maximum number of nodes the shard may contain. Must be >= `minNodes`. |

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
      - name: agent-scheduler
        type: agent
        cpuUtilizationMin: 0.7
        cpuUtilizationMax: 1.0
        preferWarmupNodes: true
        minNodes: 1
        maxNodes: 100
      - name: volcano
        type: volcano
        cpuUtilizationMin: 0.0
        cpuUtilizationMax: 0.69
        preferWarmupNodes: false
        minNodes: 1
        maxNodes: 100
    shardSyncPeriod: "60s"
    enableNodeEventTrigger: true
```

## Validation Rules

The controller validates the ConfigMap contents on every load/reload:

1. `schedulerConfigs` must contain at least one entry.
2. Each entry must have a non-empty `name`.
3. `cpuUtilizationMin` and `cpuUtilizationMax` must be in `[0.0, 1.0]`.
4. `cpuUtilizationMin` must be <= `cpuUtilizationMax`.
5. `minNodes` must be >= 0.
6. `maxNodes` must be >= `minNodes`.

If validation fails, the previous valid configuration is preserved and an
error is logged.
