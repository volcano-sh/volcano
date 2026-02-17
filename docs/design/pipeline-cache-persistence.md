# Pipeline Cache Persistence

[@hajnalmt](https://github.com/hajnalmt); Feb 2026

## Table of Contents

* [Introduction](#introduction)
* [Problem Statement](#problem-statement)
* [Background: kube-scheduler Node Nomination](#background-kube-scheduler-node-nomination)
* [Volcano Architecture: Session vs Cache](#volcano-architecture-session-vs-cache)
* [Current NominatedNodeName Handling](#current-nominatednodename-handling)
* [Proposed Solution](#proposed-solution)
* [NominatedNodeName: Dual Update Safety](#nominatednodename-dual-update-safety)
* [Expected Impact](#expected-impact)
* [Affected Code](#affected-code)
* [Related Issues and PRs](#related-issues-and-prs)

## Introduction

Volcano's scheduler uses **pipelining** to tentatively assign tasks to nodes when resources are not
yet available but are expected to become available soon (e.g., after eviction completes or running
pods finish). Pipelining is central to gang scheduling: when a job's `minAvailable` cannot be fully
satisfied, individual tasks may be pipelined to nodes, reserving resources in the session view
until the full gang can be scheduled.

Currently, the pipelining mechanism operates only at the **session level**:
[`Statement.Pipeline()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L157)
reserves resources on session-cloned nodes and updates task status to `Pipelined` within the
scheduling session. There is no corresponding **cache level** persistence —
[`Statement.pipeline()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L218-L219)
(the commit-time counterpart) has an empty body, meaning pipeline decisions are silently discarded
at the end of every scheduling cycle.

This design document proposes adding `Cache.Pipeline()` to persist pipeline decisions into the
scheduler cache, along with fixes to the `allocate` action's statement handling and job ordering
for pipelined jobs.

## Problem Statement

Three related bugs exist in the current pipelined task handling:

### 1. Empty `pipeline()` commit function

[`Statement.pipeline()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L218-L219)
has an empty body:

```go
func (s *Statement) pipeline(task *api.TaskInfo) {
}
```

When [`Statement.Commit()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L428-L429)
is called, the `Pipeline` case invokes this empty function — no cache
state is updated, no `NominatedNodeName` is set, no node resources are accounted in the cache.
This is in contrast to `Statement.evict()` and `Statement.allocate()`, which both delegate to
their respective `Cache` methods
([`Cache.Evict()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L875),
[`Cache.Bind()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L933)).

### 2. No statement commit/discard for pipelined jobs in allocate

The [`allocate`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/actions/allocate/allocate.go)
action only commits statements when a job reaches `JobReady` status
(at [line 307](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/actions/allocate/allocate.go#L307)
for hard-topology jobs and
[line 323](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/actions/allocate/allocate.go#L323)
for non-topology jobs):

```go
if stmt != nil && ssn.JobReady(job) { // do not commit stmt when job is pipelined
    stmt.Commit()
    ...
}
```

When a job is `JobPipelined` (some tasks pipelined but `minAvailable` not satisfied), the statement
is **neither committed nor discarded**. This causes:

- Phantom resource reservations in the session that are never cleaned up
- Tasks pipelined for jobs that can never run (gang scheduling requires all `minAvailable` tasks)
  blocking resources from other jobs within the same scheduling cycle

### 3. Pipelined jobs not prioritized in allocation

All jobs — both previously pipelined and fresh pending — are processed in a single allocation pass
using the same priority queue.
[`buildAllocateContext()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/actions/allocate/allocate.go#L139)
builds a single `allocateContext`, and
[`organizeJobWorksheet()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/actions/allocate/allocate.go#L205)
only includes
[`Pending` tasks](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/actions/allocate/allocate.go#L253):

```go
for _, task := range subJob.TaskStatusIndex[api.Pending] {
    ...
    sjWorksheet.tasks.Push(task)
}
```

Previously pipelined jobs compete with new arrivals, leading to unnecessary re-evaluation and
potential loss of already-pipelined slots.

### Consequences

- **Cyclic evictions** ([#4947](https://github.com/volcano-sh/volcano/issues/4947)): Higher-priority
  reclaimed pods are rescheduled instead of the lower-priority reclaimers because the pipeline
  decision is lost, causing repeated eviction/reschedule cycles.
- **Resource accounting drift**: Cache nodes show stale resource availability because pipelined
  task reservations are never persisted.
- **Scheduling inefficiency**: Every cycle re-evaluates pipelined tasks from scratch.

## Background: kube-scheduler Node Nomination

Understanding how kube-scheduler handles `NominatedNodeName` provides context for the proposed
approach. The two schedulers differ fundamentally in architecture but share the same pod status
field for signaling scheduling intent.

### kube-scheduler Architecture

kube-scheduler processes **one pod at a time** via
[`ScheduleOne()`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/schedule_one.go#L65)
— there is no session or batch concept. Each pod goes through a synchronous **scheduling cycle**
followed by an asynchronous **binding cycle**.

### Preemption Path (nomination WITH preemption)

When `schedulePod()` fails with `FitError`, the scheduler runs `PostFilter` plugins. The
[`DefaultPreemption`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/framework/plugins/defaultpreemption/default_preemption.go#L121)
plugin delegates to
[`Evaluator.Preempt()`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/framework/preemption/preemption.go#L268)
which:

1. Checks pod eligibility via
   [`PodEligibleToPreemptOthers()`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/framework/preemption/preemption.go#L284)
2. Finds preemption candidates via
   [`findCandidates()`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/framework/preemption/preemption.go#L343)
3. Selects the best candidate via
   [`SelectCandidate()`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/framework/preemption/preemption.go#L431)
4. Evicts victims via
   [`prepareCandidate()`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/framework/preemption/preemption.go#L464)
5. Returns `PostFilterResult` with the nominated node name

The result flows to
[`handleSchedulingFailure()`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/schedule_one.go#L1033)
which calls
[`AddNominatedPod()`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/schedule_one.go#L1100)
(in-memory nominator) and
[`updatePod()`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/schedule_one.go#L1131)
(API server patch).

### Expectation Path (nomination WITHOUT preemption) — K8s 1.34+

The
[`NominatedNodeNameForExpectation`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/features/kube_features.go#L674)
feature gate (Alpha in 1.34, Beta/default-on in 1.35, [KEP 5278](https://kep.k8s.io/5278)) adds a
second nomination path. In the
[binding cycle](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/schedule_one.go#L281-L298),
**before** actual binding, the scheduler sets `nominatedNodeName` to the suggested host using
`ModeOverride`. This signals intent to external components (Cluster Autoscaler, Karpenter, Kueue)
so they avoid scaling down the target node while binding is in progress.

This is significant because it establishes that `NominatedNodeName` is not exclusively a preemption
signal — it is a general "this pod intends to run on this node" signal.

### In-Memory Nominator

kube-scheduler maintains an in-memory
[`nominator`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/backend/queue/nominator.go#L35)
that tracks nominations separately from the API server pod status. This handles the window between
making a nomination decision and the API server reflecting it.

Key data structures:
- `nominatedPods map[string][]podRef` — node name to list of nominated pod references
- `nominatedPodToNode map[types.UID]string` — pod UID to nominated node name

The
[`NominatingMode`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/staging/src/k8s.io/kube-scheduler/framework/interface.go#L299)
type controls update behavior: `ModeNoop` (keep existing nomination) vs `ModeOverride` (overwrite).

### Previously Nominated Node Evaluation

[`evaluateNominatedNode()`](https://github.com/kubernetes/kubernetes/blob/v1.35.1/pkg/scheduler/schedule_one.go#L575)
checks whether a pod's previously nominated node still works. If it passes filters, the scheduler
uses it directly, skipping the broader node search. This is the mechanism that makes nomination
meaningful across scheduling cycles.

### Comparison with Volcano

| Aspect | kube-scheduler | Volcano (current) |
|--------|---------------|-------------------|
| Scheduling unit | Single pod | Session (batch of jobs/tasks) |
| Concurrency model | One `ScheduleOne()` per worker | Session-based with `OpenSession`/`CloseSession` |
| Nomination | Per-pod `nominatedNodeName` on status | Per-task via `TransactionContext` (session-only, not persisted to cache) |
| Preemption | Per-pod, `DefaultPreemption` plugin | Per-job/task, integrated with gang scheduling |
| Batch awareness | None (one pod at a time) | Core concept — `PodGroup` with `minAvailable` |
| State persistence | In-memory nominator + API patch | Session-end API patch only (no cache persistence for pipelines) |
| External coordination | `NominatedNodeNameForExpectation` | `NominatedNodeName` set only for eviction cases at session end |

## Volcano Architecture: Session vs Cache

Understanding the session vs cache architecture is fundamental to understanding why cache-level
pipeline persistence is necessary.

### Snapshot: The Clone Boundary

When a scheduling cycle starts,
[`Snapshot()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1423)
**clones** every job and task from the cache:

```go
// cache.go — inside Snapshot()
clonedJob := value.Clone()

// job_info.go — inside JobInfo.Clone()
for _, task := range ji.Tasks {
    info.AddTaskInfo(task.Clone())
}
```

[`TaskInfo.Clone()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/api/job_info.go#L282)
creates a **new** `TransactionContext`
(independent copy) but **shares the same `*v1.Pod` pointer**:

```go
func (ti *TaskInfo) Clone() *TaskInfo {
    return &TaskInfo{
        Pod: ti.Pod,  // shared pointer
        TransactionContext: TransactionContext{
            NodeName: ti.NodeName,
            Status:   ti.Status,
        },
        LastTransaction: ti.LastTransaction.Clone(),
    }
}
```

### Two Separate Object Graphs

| Layer | Objects | TransactionContext | Pod |
|-------|---------|-------------------|-----|
| **Session** | Cloned `JobInfo`/`TaskInfo` | Independent copy, mutated by `Statement` | Shared `*v1.Pod` pointer |
| **Cache** | Original `JobInfo`/`TaskInfo` | Unchanged by session operations | Same shared `*v1.Pod` pointer |

Session-level operations
([`Statement.Pipeline()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L157),
`Statement.Allocate()`) mutate the session's
cloned copies. Cache methods
([`Cache.Bind()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L933),
[`Cache.Evict()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L875))
mutate the cache's originals.
The [`findJobAndTask()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L856)
method in cache operations looks up `sc.Jobs` — the **cache's** copy, not the session's.

Because [`Statement.pipeline()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L218-L219)
is currently empty, the cache never learns about pipeline decisions.
The next cycle's
[`Snapshot()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1423)
clones stale cache state — the task appears as `Pending` again.

### TransactionContext

**Definition**
([`TransactionContext`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/api/job_info.go#L82)):

```go
type TransactionContext struct {
    NodeName              string
    EvictionOccurred      bool
    JobAllocatedHyperNode string
    Status                TaskStatus
}
```

`TransactionContext` is
[**embedded**](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/api/job_info.go#L130)
in `TaskInfo`, not a pointer — so `task.NodeName`
is actually `task.TransactionContext.NodeName`.

[`LastTransaction *TransactionContext`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/api/job_info.go#L132)
is a snapshot pointer used for post-discard
reporting: when a statement is discarded,
[`GenerateLastTxContext()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/api/job_info.go#L238)
saves the current state before
[`UnPipeline()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L221)
resets everything.

### TransactionContext Lifecycle (Current)

1. **Session-level pipeline**
   ([`Statement.Pipeline()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L157)):
   Sets session task's `NodeName`, `EvictionOccurred`, status to `Pipelined`, reserves node resources
   in the session view.

2. **Commit path**
   ([`Statement.Commit()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L418),
   [Pipeline case](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L428-L429)):
   Calls [`ClearLastTxContext()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/api/job_info.go#L244)
   then `pipeline(task)` — which is currently a no-op.

3. **Discard path**
   ([`Statement.Discard()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L392),
   [Pipeline case](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L403-L406)):
   Calls [`GenerateLastTxContext()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/api/job_info.go#L238)
   (snapshot into `LastTransaction`) then
   [`UnPipeline()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L221)
   (resets NodeName, status to Pending, frees node resources in the session view).

4. **Session close** → `JobUpdater.UpdateAll()` → `cache.UpdateJobStatus()` →
   [`RecordJobStatusEvent()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1582)
   → iterates Pipelined tasks →
   [`TaskSchedulingReason()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/api/job_info.go#L810)
   → reports conditions and (sometimes) `NominatedNodeName` to the API server.

## Current Volcano NominatedNodeName Handling

The codebase has a path that sets `NominatedNodeName` at session end, but it is incomplete — it
only covers the eviction case and does not persist any state into the cache.

### Session-End Path

```
closeSession(ssn)
  -> JobUpdater.UpdateAll()
    -> cache.UpdateJobStatus(job, ...)
      -> RecordJobStatusEvent(job, ...)            // cache.go:1582
        -> for each Pipelined task:
            job.TaskSchedulingReason(taskInfo.UID)  // job_info.go:810
              -> reads TransactionContext (or LastTransaction if discarded)
              -> if ctx.Status == Pipelined && ctx.EvictionOccurred:
                    nominatedNodeName = ctx.NodeName
            taskUnschedulable(task, reason, msg, nominatedNodeName)  // cache.go:1027
              -> if len(nominatedNodeName) > 0 && needsUpdate:
                    pod.Status.NominatedNodeName = nominatedNodeName
                    UpdatePodStatus(pod)
```

### Limitations

1. **Eviction-only**:
   [`TaskSchedulingReason()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/api/job_info.go#L810)
   only returns `nominatedNodeName` when `EvictionOccurred == true`. Non-eviction pipelining
   (waiting for resources without preemption) never triggers nomination.

2. **API-only, not cached**:
   [`taskUnschedulable()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1027)
   patches the pod in the API server via `UpdatePodStatus()`. It does **not** update the cache's
   `TaskStatusIndex`, node resource accounting, or `TransactionContext` fields.

3. **Ephemeral**: Even when `NominatedNodeName` is set on the pod, the cache still shows the task
   as `Pending`. The next cycle's
   [`Snapshot()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1423)
   starts from stale state.

## Proposed Solution

The fix spans three changes:

### 1. Add `Cache.Pipeline()` Interface and Implementation

A new `Pipeline()` method is added to the `Cache` interface
(`pkg/scheduler/cache/interface.go`):

```go
// Pipeline pipelines Task to the target host, which means the task is
// expected to be scheduled to the target host in one of the next
// scheduling cycles.
Pipeline(task *api.TaskInfo, nodeName string) error
```

The implementation (`pkg/scheduler/cache/cache.go`) follows the same async pattern as
[`Cache.Evict()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L875):

- **Under lock** (synchronous): Look up the cache's job and task via
  [`findJobAndTask()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L856),
  update task status to `Pipelined` via `job.UpdateTaskStatus()`, update node resource accounting
  via `node.UpdateTask()`. If node update fails, rollback task status and call
  [`resyncTask()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1153).

- **Outside lock** (async goroutine): `DeepCopy()` the pod, set
  `pod.Status.NominatedNodeName = nodeName`, call `UpdatePodStatus()`. On failure, call
  [`resyncTask()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1153)
  to reconcile.

This separation ensures the API server call (which may be slow or fail) does not block the
scheduling cycle while cache state is updated atomically.

`Statement.pipeline()` is updated to delegate to `Cache.Pipeline()`:

```go
func (s *Statement) pipeline(task *api.TaskInfo) error {
    if err := s.ssn.cache.Pipeline(task, task.NodeName); err != nil {
        return err
    }
    return nil
}
```

`Statement.Commit()` is updated to handle `pipeline()` errors — if `Cache.Pipeline()` fails during
commit, the task is unpipelined in the session to maintain consistency:

```go
case Pipeline:
    err := s.pipeline(op.task)
    if err != nil {
        if e := s.unpipeline(op.task); e != nil {
            klog.Errorf("Failed to unpipeline task ...")
        }
    }
```

Additionally, `UnPipeline` is renamed to an unexported `unpipeline()` for internal use, and a new
exported `UnPipeline()` wrapper is added for external callers (e.g., the `allocate` action's
rollback on `Statement.Pipeline()` failure).

### 2. Three-Way Statement Handling in Allocate

The `allocate` action is updated to use three-way commit/discard logic
(`pkg/scheduler/actions/allocate/allocate.go`):

```go
if ssn.JobReady(job) {
    stmt.Commit()       // Full allocation — persist everything
} else if ssn.JobPipelined(job) {
    stmt.Commit()       // Pipeline state — persist to cache
} else {
    stmt.Discard()      // Neither ready nor pipelined — roll back
}
```

This ensures:
- `JobReady`: Full allocation is persisted (existing behavior, unchanged).
- `JobPipelined`: Pipeline state is persisted to cache via `Cache.Pipeline()`.
- Neither: All session-level changes are rolled back, freeing phantom-reserved resources.

### 3. Pipelined-First Allocation

`buildAllocateContext()` is updated to produce **two** separate `allocateContext` instances:

```go
actx, pipelineActx := alloc.buildAllocateContext()
alloc.allocateResources(pipelineActx)  // Pipelined jobs first
alloc.allocateResources(actx)          // Then pending jobs
```

Jobs are classified during context building:

```go
if ssn.JobPipelined(job) {
    addJobToContext(pipelineActx, job, worksheet)
} else {
    addJobToContext(actx, job, worksheet)
}
```

Within each job, `organizeJobWorksheet()` is updated to include both `Pipelined` and `Pending`
tasks, with pipelined tasks having higher priority in the task ordering function:

```go
for _, status := range []api.TaskStatus{api.Pipelined, api.Pending} {
    for _, task := range subJob.TaskStatusIndex[status] {
        ...
        sjWorksheet.tasks.Push(task)
    }
}
```

```go
// Pipelined tasks shall have higher priority than Pending tasks
if lv.Status != rv.Status {
    return lv.Status == api.Pipelined
}
```

This ensures pipelined tasks are re-evaluated on their nominated nodes first, before any new
pending tasks are considered.

## NominatedNodeName: Dual Update Safety

With the proposed `Cache.Pipeline()`, `NominatedNodeName` would be set in two places:
1. **Cache.Pipeline() goroutine** — at commit time, for all pipelined tasks
2. **[`RecordJobStatusEvent`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1582)
   at session end** — for pipelined tasks with `EvictionOccurred == true`

These two paths are complementary and safe:

| Scenario | Cache.Pipeline() goroutine | RecordJobStatusEvent |
|----------|---------------------------|----------------------|
| Pipelined WITH eviction | Sets `NominatedNodeName = X` | Also sets same value (idempotent — `podNominatedNodeNameNeedUpdate` returns false) |
| Pipelined WITHOUT eviction | Sets `NominatedNodeName = X` | Returns empty `nominatedNodeName`, skips update |
| Pipelined then DISCARDED | Already set before discard | Uses `LastTransaction`, idempotent if eviction occurred |

The goroutine fires first (at commit time). By the time session close runs, the pod already has the
correct `NominatedNodeName`. The session-end path either confirms it (no-op) or skips it.

### Current vs Proposed Comparison

| What | Current (no `Cache.Pipeline()`) | Proposed (with `Cache.Pipeline()`) |
|------|---------------------------------|------------------------------------|
| Cache task status | Stays `Pending` forever | Updated to `Pipelined` |
| Cache node resource accounting | Not tracked | Resources properly reserved |
| Next cycle sees pipeline decision | No — snapshot shows `Pending` | Yes — shows `Pipelined` on correct node |
| `NominatedNodeName` (eviction case) | Set at session end | Set immediately + confirmed at session end |
| `NominatedNodeName` (no eviction) | **Never set** | Set by `Cache.Pipeline()` goroutine |
| Pod condition (Unschedulable) | Set at session end | Set at session end (unchanged) |

[`RecordJobStatusEvent`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1582)
is the **reporting** mechanism — it tells the API server about pod conditions
and (for eviction cases) `NominatedNodeName`. `Cache.Pipeline()` is the **persistence** mechanism —
it records the scheduling decision in the cache so it survives to the next cycle. This mirrors the
existing pattern:
[`Cache.Bind()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L933)
persists binding + `RecordJobStatusEvent` reports status;
[`Cache.Evict()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L875)
triggers eviction + `RecordJobStatusEvent` reports status.

## Expected Impact

- **Cluster Autoscaler / Karpenter**: `NominatedNodeName` will be set for all pipelined tasks (not
  just eviction cases), giving external components visibility into scheduling intent and preventing
  premature scale-down of nominated nodes.
- **Monitoring / kubectl**: `kubectl get pod -o wide` will show `NOMINATED NODE` for pipelined
  pods, improving operator visibility.
- **Scheduling stability**: Pipeline decisions will persist across cycles. Pipelined jobs will be
  processed first, reducing churn and preventing displacement by new arrivals.
- **Resource accounting**: Cache will accurately reflect reserved resources, preventing
  double-booking.

## Affected Code

| File | Changes |
|------|---------|
| `pkg/scheduler/cache/cache.go` | Add `Pipeline()` implementation with async NominatedNodeName update |
| `pkg/scheduler/cache/interface.go` | Add `Pipeline()` method to `Cache` interface |
| `pkg/scheduler/framework/statement.go` | `pipeline()` delegates to `Cache.Pipeline()`, rename `UnPipeline` to `unpipeline()`, add `Commit()` rollback on pipeline failure |
| `pkg/scheduler/actions/allocate/allocate.go` | `buildAllocateContext()` returns two contexts, three-way commit/discard, `addJobToContext` helper, pipelined task priority in `organizeJobWorksheet()` |

## Related Issues and PRs

- [#5044](https://github.com/volcano-sh/volcano/issues/5044) — Issue describing the pipelined statement handling bugs
- [#5045](https://github.com/volcano-sh/volcano/pull/5045) — PR implementing the fix
- [#4947](https://github.com/volcano-sh/volcano/issues/4947) — Cyclic eviction issue caused by lost pipeline state
- [#4936](https://github.com/volcano-sh/volcano/pull/4936) — Earlier attempted fix that did not address the root cause
