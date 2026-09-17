# Checkpoint-aware task victim ordering

## Motivation

When the `preempt` and `reclaim` actions choose which tasks to evict, tasks of
equal priority are ordered by pod creation time (`CompareTask`). For
long-running batch/ML workloads this ignores the factor that actually determines
the cost of an eviction: how much progress is lost.

A task that has just checkpointed can be evicted and resumed with almost no lost
compute, while a task that has never checkpointed loses all of its work. Creation
time is only a weak proxy for this — two equal-priority tasks created together
can have very different checkpoint progress.

## Design

### VictimOrderFn extension point

A new plugin extension point, `VictimOrderFn`, is consumed **only** on the
eviction path (`BuildVictimsPriorityQueue`, used by `preempt` and `reclaim`).
`allocate` and `backfill` continue to use `TaskOrderFn` and are unaffected.

`VictimOrderFn` first walks the enabled victim-order plugins (in tier/plugin
order, first non-zero wins). If no victim-order plugin decides, it falls back to
`TaskOrderFn`. This makes the eviction ordering identical to the previous
`!TaskOrderFn` behavior whenever no victim-order plugin is enabled, so the change
is a no-op by default.

Plugins opt in via `enableVictimOrder` (defaults to `true`, like
`enableTaskOrder`). The `priority` plugin registers the same comparator on both
`TaskOrderFn` and `VictimOrderFn`, so priority stays dominant on the victim path.

### checkpoint plugin

The `checkpoint` plugin registers a `VictimOrderFn` that orders victims by
checkpoint recency:

- Each task's last-checkpoint time is read from the pod annotation
  `volcano.sh/last-checkpoint-time` (RFC3339, UTC). The key is configurable via
  the `checkpointTimeKey` argument.
- The **most recently checkpointed** task is evicted **first** (least work lost).
- A task with a missing or unparseable annotation is treated as never
  checkpointed (zero time) and is evicted **last** — it has the most progress to
  lose.

An annotation, not a label, is used deliberately: the value changes on every
checkpoint and is never used for object selection. Labels are indexed by the API
server for querying, so mutating them frequently adds unnecessary etcd write
overhead; annotations are the correct carrier for a frequently-updated,
non-selectable value. The workload (or a sidecar) updates the annotation after
each successful checkpoint; the scheduler stays application-agnostic.

With `priority` listed before `checkpoint`, the resulting victim order is:

```
victim queue -> priority -> checkpoint recency -> creation time -> UID
```

Priority stays dominant; checkpoint only breaks ties among equal-priority tasks.

## Configuration

```yaml
actions: "enqueue, allocate, preempt, reclaim, backfill"
tiers:
  - plugins:
      - name: priority
      - name: checkpoint   # opt-in; listed after priority
      - name: gang
      - name: conformance
```

The application updates the annotation on each checkpoint, for example:

```
volcano.sh/last-checkpoint-time: "2025-01-01T12:00:00Z"
```
