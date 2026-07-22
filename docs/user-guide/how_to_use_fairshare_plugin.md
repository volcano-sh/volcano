# Fairshare Plugin User Guide

## Introduction

The `fairshare` plugin provides per-namespace temporal fair share scheduling within a
queue, using decayed cumulative usage tracking (a SLURM-inspired algorithm). It complements
the existing DRF plugin: DRF balances resources based on the **current allocation snapshot**
and has no memory of past usage, so a namespace that floods a queue with jobs first keeps
regaining priority as its jobs complete, effectively starving later arrivals. `fairshare`
tracks historical resource-seconds consumed per namespace and applies exponential half-life
decay, so heavy past consumers are gradually deprioritized relative to namespaces that have
consumed less — without requiring any admin-set weights.

For the design rationale and algorithm details, see the
[fairshare design doc](../design/fairshare.md).

## Can it be used together with DRF?

Yes — `fairshare` and `drf` address different axes of fairness and are meant to be run
together, not as alternatives:

- **DRF** balances *which resource dimension* dominates a job's share (CPU vs. memory vs.
  GPU) at the current snapshot.
- **fairshare** balances *which namespace* gets scheduled next based on historical
  consumption over time, within the same queue.

**Plugin order matters here, and it isn't a "same tier vs. different tier" question.**
`Session.JobOrderFn` walks every tier's plugin list in configured order (tiers first, then
plugins within each tier) and returns as soon as *any* plugin's comparison is non-zero — see
[`pkg/scheduler/framework/session_plugins.go`](https://github.com/volcano-sh/volcano/blob/master/pkg/scheduler/framework/session_plugins.go),
`Session.JobOrderFn`. Whichever plugin appears earliest in that flattened order and returns a
non-zero result decides the comparison; every later plugin is never consulted for that pair.

Concretely: **list `fairshare` before `drf`** (as in the example config below, where
`fairshare` is the 4th plugin overall and `drf` is the 5th). If `drf` were listed first
instead, its dominant-resource-share comparison — which returns non-zero for almost any pair
of jobs with differing resource requests — would decide most comparisons on its own,
and `fairshare`'s historical-usage ordering would rarely get a chance to run at all. `priority`
should still come before both, so a higher `PriorityClass` always wins regardless of either
resource-share or historical-usage comparisons.

## Environment setup

### Install volcano

Refer to the [Install Guide](https://github.com/volcano-sh/volcano/blob/master/installer/README.md) to install volcano.

After installing, update the scheduler configuration:

```shell
kubectl edit cm -n volcano-system volcano-scheduler-configmap
```

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, backfill"
    tiers:
    - plugins:
      - name: priority
      - name: gang
      - name: conformance
      - name: fairshare
        arguments:
          fairshare.targetQueues: "gpu-queue"
          fairshare.resourceKey: "nvidia.com/gpu"
          fairshare.halfLifeMinutes: "240"
          fairshare.enableEnqueueGate: "false"
          fairshare.persistState: "true"
    - plugins:
      - name: drf
      - name: predicates
      - name: nodeorder
```

## Arguments

| Argument | Default | Description |
|----------|---------|-------------|
| `fairshare.targetQueues` | _(all queues)_ | Comma-separated queue names to apply fair share to; if unset, applies to every queue |
| `fairshare.resourceKey` | `nvidia.com/gpu` | Default resource to track |
| `fairshare.resourceKey.<queue>` | _(none)_ | Per-queue resource override (e.g., `amd.com/gpu`, `cpu`) |
| `fairshare.halfLifeMinutes` | `240` | Half-life for usage decay in minutes |
| `fairshare.enableEnqueueGate` | `false` | When `true`, blocks namespaces at/above their calculated share from entering the scheduling pipeline |
| `fairshare.persistState` | `false` | When `true`, persists usage state to a ConfigMap so it survives scheduler restarts |
| `fairshare.stateNamespace` | `volcano-system` | Namespace for the state ConfigMap |
| `fairshare.stateConfigMap` | `fairshare-usage-state` | Name of the state ConfigMap |
| `fairshare.flushIntervalSeconds` | `30` | How often to flush state to the ConfigMap (in seconds) |

## Interaction with existing plugins

### priority plugin

The `priority` plugin should be listed before `fairshare` (see [Can it be used together with
DRF?](#can-it-be-used-together-with-drf) above for why list order — not tier grouping — is
what determines this). A higher PriorityClass always wins. Fair share only breaks ties at
equal priority levels.

When all competing jobs use the same PriorityClass, fair share is fully effective as the
tiebreaker. This means using a high PriorityClass only helps when others don't — if everyone
uses it, fair share still distributes resources equitably.

### DRF plugin

See [Can it be used together with DRF?](#can-it-be-used-together-with-drf) above. The two
plugins are complementary and typically run together.

### gang plugin

No interaction — gang scheduling handles minimum member requirements, while fairshare
handles ordering among eligible jobs.

## Logging

| klog level | What it shows |
|------------|---------------|
| V(2) | Plugin config on creation, per-cycle queue summary (namespace count, running, demand) |
| V(3) | Decay factor per cycle, fair shares, usage maps, ordering winner decisions |
| V(4) | Session open/close, individual allocate/deallocate events |
| V(5) | Every job comparison, every enqueue evaluation |
