# Volcano Scheduler: Task State Persistence Across Sessions 


## Background

Under heavy preemption/reclaim, high-priority jobs could reclaim resources too aggressively, leading to excessive evictions (“overkill”) of low-priority jobs and instability. Pipelined tasks (pre-allocated but not bound) lost state across sessions, causing duplicate decisions, mis-accounting, and oscillations. Plugins could observe inconsistent resource accounting during rapid pipeline → allocate → unallocate transitions.

## Goals

- Preserve and reconcile pipelined state across scheduler sessions.
- Reduce unnecessary evictions and duplicate work.
- Make allocation deterministic for pipelined tasks.
- Prevent negative/invalid plugin accounting under churn.
- Improve resiliency against concurrent state changes in the Kubernetes API.

## Non-Goals

- Redesign Volcano’s event model or plugin API.
- Introduce new CRDs or controllers.
- Change gang scheduling semantics.

---

## High-Level Design

### Persist pipelined state on Pods (annotations)

```go
// pkg/scheduler/api/helpers.go
const (
  VolcanoPipelinedStatusAnnotation   = "volcano.sh/pipelined-status"
  VolcanoPipelinedNodeAnnotation     = "volcano.sh/pipelined-node"
  VolcanoEvictionOccurredAnnotation  = "volcano.sh/eviction-occurred"
)
```

- On pipeline: set `pipelined-status=true` and `pipelined-node=<name>`.
- On allocate (final bind): clear pipeline annotations.
- On unallocate originating from a pipelined state: restore `pipelined-*` instead of resetting to `Pending`.
- During reclaim: record `eviction-occurred=true` when victims are chosen.

### Deterministic allocation for pipelined tasks

- If a task is already pipelined to a node and the node becomes available, allocate via a fast path that avoids duplicate node ops and stale pipeline state.
- Predicate helper recognizes pipelined tasks to the same node and short-circuits checks to prevent redundant work.

### Harden plugins and resource comparisons

- DRF, Capacity, Proportion plugins guard against underflow/negative deallocations and improve logs.
- Resource comparisons (`LessEqualWithDimension`) ignore dimensions not configured for a queue (e.g., GPU scalar), avoiding spurious failures.

### Safer cache/binder and framework updates

- Binder: treat “already bound to same node” as success; if bound elsewhere, fail and clear annotations.
- Annotation updates via a single abstraction to avoid GET/UPDATE drift; prefer PATCH.
- Session/Statement ensure plugin callbacks are invoked exactly once on final allocation.

---

## Detailed Changes and Rationale

### Actions

**Allocate**
- Add fast path to bind already-pipelined tasks once resources are free on the pipelined node.
- Filter nil nodes, add defensive checks, and prevent duplicate AddTask on nodes for pipelined → final allocation.
- Predicate: if task is pipelined to the node, skip full resource check; if the task moved, release stale pipelined accounting.

**Reclaim**
- When selecting victims, set `EvictionOccurred` and propagate via annotations; improves traceability and reduces unnecessary follow-on evictions.

### Framework (Session/Statement)

- On pipeline/unpipeline/allocate, set/clear pipelined annotations through a **single** annotations updater.
- Unallocate from a pipelined origin restores pipelined status and avoids double deallocation & node removal.
- Add `Operation.String()` helper and targeted annotations update to avoid clobbering third‑party annotations.

### Cache/Binder

- Binder checks current binding:
  - If already bound to target node → succeed & clear pipeline annotations.
  - If bound to a different node → error and clear pipeline annotations for consistency.
- Add retry for conflict-prone updates (Unschedulable condition, nominated node).
- Expose `GetStatusUpdater` and `UpdatePodAnnotations` for annotations‑only writes.

### API / Helpers

- `getTaskStatus` prefers annotations-derived `Pipelined` over Pod phase for accuracy across sessions.
- `NewTaskInfo` restores pipelined node and eviction flag; includes `Pipelined` in readiness/allocated counts.
- Helpers to set/clear/read pipelined annotations.

### Plugins

- **DRF/Capacity/Proportion:** guard deallocation to never subtract more than allocated, protect ancestor totals, and clarify logs.
- **Capacity:** relax strict root queue capability/deserved/guarantee enforcement; enforce only on non-root queues.

### Resources

- `LessEqualWithDimension`: ignore dimensions with zero/undefined config in the queue (e.g., GPUs not configured), keeping CPU/memory scheduling unblocked.

### Utils

- `predicate_helper.go`: early exit on matching pipelined node; avoid re-alloc attempts to nodes where the same task is present in another status.

---

## Behavioral Impact

- **Stability:** pipelined tasks resume correctly after session reopen; fewer duplicate pipeline/allocate loops.
- **Efficiency:** reduced overkill in reclaim; fewer unnecessary evictions.
- **Correctness:** plugins avoid underflow and negatives under churn.
- **Compatibility:** queues without GPU dimensions schedule CPU/memory workloads without scalar-related blocks.

---

## Backward Compatibility

- New annotations are additive and benign for older components.
- Plugin changes are defensive (no behavior breaks).
- Capacity plugin’s relaxed root semantics may affect deployments depending on strict root validation—documented below.


## Risks and Mitigations

- **Plugin accounting drift:** If AllocateFunc is skipped for pipelined tasks, plugins may under-account.
  - *Mitigation:* Ensure AllocateFunc executes exactly once on final allocation (even for pipelined tasks). Only skip if a future PipelineFunc provides equivalent accounting.
- **API write amplification:** Frequent annotation updates on large clusters.
  - *Mitigation:* Batch/coalesce updates and switch to PATCH; avoid writes when values unchanged.
- **Get → Bind races:** Still possible in extreme churn.
  - *Mitigation:* Binder clears annotations on success/failure; consider binding preconditions or further retries.

---

## TL;DR

- Persist pipelined state via Pod annotations and reconcile on allocate/unallocate; reclaim propagates eviction context.
- Plugins are hardened against deallocation underflow; resource dimensionality checks are queue-aware; root queue constraints are eased.
- Next steps: keep AllocateFunc for pipelined allocations, switch annotations to PATCH, and add metrics + tests for pipeline fast-path.