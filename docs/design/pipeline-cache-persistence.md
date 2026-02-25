# Pipeline Cache Persistence

[@hajnalmt](https://github.com/hajnalmt); Feb 2026

## Table of Contents

* [Introduction](#introduction)
* [Problem Statement](#problem-statement)
* [Background: kube-scheduler Node Nomination](#background-kube-scheduler-node-nomination)
* [Volcano Architecture: Session vs Cache](#volcano-architecture-session-vs-cache)
* [Current NominatedNodeName Handling](#current-nominatednodename-handling)
* [Proposed Solution](#proposed-solution)
* [NominatedNodeName: Single Update Path](#nominatednodename-single-update-path)
* [Expected Impact](#expected-impact)
* [Affected Code](#affected-code)
* [Pipeline-to-Allocate Transition Fixes](#pipeline-to-allocate-transition-fixes)
  * [Problem 1: Pipelined task cannot transition to Allocated](#problem-1-pipelined-task-cannot-transition-to-allocated)
  * [Problem 2: FutureIdle self-competition on nominated node](#problem-2-futureidle-self-competition-on-nominated-node)
  * [Problem 3: NominatedNodeName async race condition](#problem-3-nominatednodename-async-race-condition)
  * [Problem 4: HA cache coherence](#problem-4-ha-cache-coherence)
  * [Problem 5: Pipelined task cannot transition to Binding](#problem-5-pipelined-task-cannot-transition-to-binding)
  * [Affected Code (Pipeline-to-Allocate Fixes)](#affected-code-pipeline-to-allocate-fixes)
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
   Sets session task's `NodeName` and status to `Pipelined`, reserves node resources
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
   → reports conditions and `NominatedNodeName` to the API server.

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
              -> if ctx.Status == Pipelined:
                    nominatedNodeName = ctx.NodeName
            taskUnschedulable(task, reason, msg, nominatedNodeName)  // cache.go:1027
              -> if len(nominatedNodeName) > 0 && needsUpdate:
                    pod.Status.NominatedNodeName = nominatedNodeName
                    UpdatePodStatus(pod)
```

### Limitations, Consequences

1. **API-only, not cached**:
   [`taskUnschedulable()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1027)
   patches the pod in the API server via `UpdatePodStatus()`. It does **not** update the cache's
   `TaskStatusIndex`, node resource accounting, or `TransactionContext` fields.

2. **Ephemeral**: Even when `NominatedNodeName` is set on the pod, the cache still shows the task
   as `Pending`. The next cycle's
   [`Snapshot()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1423)
   starts from stale state.

3. **`NominatedNodeName` potentially not visible to the next scheduling cycle**:
   [`taskUnschedulable()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1027)
   calls `UpdatePodStatus()` to patch `NominatedNodeName` on the API server. This is an async
   call — the cache does not wait for it to complete. For the updated `NominatedNodeName` to be
   visible in the cache, the full round-trip must complete before the next
   [`Snapshot()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1423):
   the API server must process the update, the informer watch must deliver the event, and the
   cache's `updatePod()` must process it. If any part of this chain is slow (API server churn,
   informer lag, scheduling cycle running faster than the round-trip), the next cycle's
   `Snapshot()` clones the cache task whose `Pod` pointer still references the old pod object —
   `pod.Status.NominatedNodeName` is empty. The `allocate` action in that cycle has no knowledge
   that the task was nominated to a node in the previous cycle. As a consequence, the task goes
   through the full scheduling cycle again — predicate evaluation, node scoring, and placement —
   with no affinity toward its previously nominated node. This can cause **allocation drift**:
   the task may be placed on a different node than the one it was nominated to, wasting the
   eviction or resource reservation that justified the original nomination.

## Proposed Solution

The fix spans six changes:

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

### 4. Remove `EvictionOccurred` from `TransactionContext`

The `EvictionOccurred` field is removed from
[`TransactionContext`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/api/job_info.go#L82)
and the `evictionOccurred` parameter is removed from
[`Statement.Pipeline()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L157).

Previously, `EvictionOccurred` was the only signal that controlled whether
[`TaskSchedulingReason()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/api/job_info.go#L810)
returned a `nominatedNodeName` for pipelined tasks. Non-eviction pipelining (waiting for resources
without preemption) never triggered nomination. With `Cache.Pipeline()` now unconditionally setting
`NominatedNodeName` on the pod via the API server at commit time, this distinction is no longer
needed.

Additionally, the `nominatedNodeName` return value is removed from `TaskSchedulingReason()`
entirely — it now returns `(reason, msg string)` only. The `nominatedNodeName` parameter is also
removed from `taskUnschedulable()`, simplifying it to `(task, reason, message string)`. The helper
function `podNominatedNodeNameNeedUpdate()` becomes dead code and is deleted. `NominatedNodeName`
is now set exclusively by the `Cache.Pipeline()` async goroutine at commit time — the session-end
`RecordJobStatusEvent` path no longer touches it.

The `evictionOccurred` parameter is removed from all callers:
[`allocate`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/actions/allocate/allocate.go),
[`reclaim`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/actions/reclaim/reclaim.go),
and [`preempt`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/actions/preempt/preempt.go).

### 5. Prevent Duplicate Re-Pipelining

A guard is added at the top of
[`Statement.Pipeline()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/framework/statement.go#L157)
to skip re-pipelining when a task is already `Pipelined` on the same node:

```go
if task.Status == api.Pipelined && task.NodeName == hostname {
    return nil
}
```

This is placed in `Statement.Pipeline()` rather than in individual action code so that all callers
(`allocate`, `reclaim`, `preempt`) are protected uniformly. Without this guard,
`organizeJobWorksheet()` includes both `Pipelined` and `Pending` tasks, and already-pipelined tasks
flow through `allocateResourcesForTask()` which calls `stmt.Pipeline()` again. On commit,
`Cache.Pipeline()` fires again, emitting a duplicate "Pipeline" event on the podgroup every
scheduling session. The guard ensures that tasks already pipelined on their target node are left in
place without re-processing.

### 6. Guard `allocatedPodInCache()` Against Informer Overwrites

The `Cache.Pipeline()` async goroutine calls `UpdatePodStatus()` to set `NominatedNodeName` on the
pod in the API server. This triggers an informer `updatePod` event back into the cache. The
existing [`updatePod()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/event_handlers.go#L316)
handler calls `deletePod()` + `addPod()`, which reconstructs the task from pod fields — but since
the pod is not yet bound to a node (`Spec.NodeName == ""`), the reconstructed task gets
`Status = Pending`, overwriting the `Pipelined` status that `Cache.Pipeline()` just set.

The existing guard
[`allocatedPodInCache()`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/event_handlers.go#L303)
protects against this by short-circuiting `updatePod()` when the cache already has the task in an
allocated state. However, it only checked `AllocatedStatus()` (which covers `Allocated`, `Binding`,
`Bound`), not `Pipelined`. Adding `Pipelined` to `AllocatedStatus()` itself is wrong because 25+
call sites use `AllocatedStatus()` for resource accounting, and `Pipelined` tasks have different
resource semantics.

The fix extends `allocatedPodInCache()` with an explicit `Pipelined` check:

```go
func (sc *SchedulerCache) allocatedPodInCache(pod *v1.Pod) bool {
    pi := schedulingapi.NewTaskInfo(pod)
    if job, found := sc.Jobs[pi.Job]; found {
        if t, found := job.Tasks[pi.UID]; found {
            return schedulingapi.AllocatedStatus(t.Status) || t.Status == schedulingapi.Pipelined
        }
    }
    return false
}
```

This ensures that when the informer fires `updatePod()` after `Cache.Pipeline()` sets
`NominatedNodeName`, the cache task's `Pipelined` status and node reservation are preserved.

## NominatedNodeName: Single Update Path

With the removal of `nominatedNodeName` from `TaskSchedulingReason()` and `taskUnschedulable()`,
`NominatedNodeName` is now set in exactly one place:

1. **`Cache.Pipeline()` async goroutine** — at commit time, for all pipelined tasks

The session-end `RecordJobStatusEvent` path no longer touches `NominatedNodeName`. It only reports
pod conditions (`PodScheduled=False` with reason and message) via `taskUnschedulable()`.

This is a simplification from the v1.14.0 baseline where `RecordJobStatusEvent` attempted to set
`NominatedNodeName` through `taskUnschedulable()` (gated by `EvictionOccurred`). With
`Cache.Pipeline()` setting `NominatedNodeName` unconditionally at commit time, the session-end
path became redundant for nomination and was removed entirely.

### Current vs Proposed Comparison

| What | Current (no `Cache.Pipeline()`) | Proposed (with `Cache.Pipeline()`) |
|------|---------------------------------|------------------------------------|
| Cache task status | Stays `Pending` forever | Updated to `Pipelined` |
| Cache node resource accounting | Not tracked | Resources properly reserved |
| Next cycle sees pipeline decision | No — snapshot shows `Pending` | Yes — shows `Pipelined` on correct node |
| `NominatedNodeName` | Set at session end only for eviction cases | Set by `Cache.Pipeline()` goroutine only — session-end path no longer touches it |
| Pod condition (Unschedulable) | Set at session end | Set at session end (unchanged) |
| Duplicate events | Pipelined tasks re-pipelined every session | Skipped when already pipelined on same node |
| Informer overwrite risk | `updatePod()` overwrites `Pipelined` → `Pending` | `allocatedPodInCache()` guards against overwrite for `Pipelined` tasks |

[`RecordJobStatusEvent`](https://github.com/volcano-sh/volcano/blob/v1.14.0/pkg/scheduler/cache/cache.go#L1582)
is the **reporting** mechanism — it tells the API server about pod conditions
(scheduling reason and message). `Cache.Pipeline()` is the **persistence** mechanism —
it records the scheduling decision in the cache so it survives to the next cycle, and sets
`NominatedNodeName` on the pod via an async API server call. This mirrors the existing pattern:
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
| `pkg/scheduler/cache/cache.go` | Add `Pipeline()` implementation with async NominatedNodeName update, simplify `taskUnschedulable()` to 3 params (remove `nominatedNodeName`), delete `podNominatedNodeNameNeedUpdate()` |
| `pkg/scheduler/cache/interface.go` | Add `Pipeline()` method to `Cache` interface |
| `pkg/scheduler/cache/event_handlers.go` | Extend `allocatedPodInCache()` guard to include `Pipelined` status, preventing informer-triggered cache overwrites |
| `pkg/scheduler/framework/statement.go` | `pipeline()` delegates to `Cache.Pipeline()`, rename `UnPipeline` to `unpipeline()`, add `Commit()` rollback on pipeline failure, remove `evictionOccurred` parameter from `Pipeline()`, add duplicate re-pipelining guard (skip when task already `Pipelined` on same node) |
| `pkg/scheduler/actions/allocate/allocate.go` | `buildAllocateContext()` returns two contexts, three-way commit/discard, `addJobToContext` helper, pipelined task priority in `organizeJobWorksheet()` |
| `pkg/scheduler/api/job_info.go` | Remove `EvictionOccurred` from `TransactionContext`, simplify `TaskSchedulingReason()` to return `(reason, msg string)` only (remove `nominatedNodeName` return value) |
| `pkg/scheduler/actions/reclaim/reclaim.go` | Remove `evictionOccurred` variable and parameter from `Pipeline()` call |
| `pkg/scheduler/actions/preempt/preempt.go` | Remove `evictionOccurred` variable and parameter from `Pipeline()` calls |

## Pipeline-to-Allocate Transition Fixes

The initial six changes above establish cache-level pipeline persistence. However, the transition
from Pipelined to Allocated (and Binding) reveals five additional interconnected problems that
must be fixed together.

### Problem 1: Pipelined task cannot transition to Allocated

When `Statement.Allocate()` processes a task that was previously pipelined on the same node,
`node.AddTask(task)` fails with "task already on node" because the task is already in `ni.Tasks`
from the pipeline step. The error path calls `stmt.UnAllocate()` but `job.UpdateTaskStatus(task,
Allocated)` was already called, leaving job state inconsistent.

**Fix**: `Statement.Allocate()` checks whether the task is already present on the node. If so,
it uses `node.UpdateTask(task)` (which does `RemoveTask` + `AddTask` internally, correctly
transitioning resource accounting from `ni.Pipelined` to `ni.Used`). If the task is not on the
node, the original `node.AddTask(task)` path is used.

```go
key := api.PodKey(task.Pod)
if _, taskOnNode := node.Tasks[key]; taskOnNode {
    node.UpdateTask(task)  // Pipelined -> Allocated
} else {
    node.AddTask(task)     // Fresh allocation
}
```

### Problem 2: FutureIdle self-competition on nominated node

The nominated-node fast-path in `allocate.go` checks whether the task fits on its nominated node
using `nominatedNodeInfo.FutureIdle()`. However, `FutureIdle() = Idle + Releasing - Pipelined`,
and the task's own pipelined reservation is included in the `Pipelined` subtraction. The task
competes with itself, potentially failing the fit check even when the node has sufficient
resources.

**Fix**: Keep using plain `FutureIdle()` in allocation checks and do not add the task's own
resource request back to the calculated value. Adding it back can over-admit and make the fit
check pass too easily. Instead, when nominated-node fast-path fails the `FutureIdle()` check
for a currently pipelined task on that nominated node, explicitly `UnPipeline` it so the stale
reservation is removed and the task can be reconsidered through the normal node scan.

When the nominated-node fast-path determines the task no longer fits (even excluding itself),
the task is un-pipelined from the node so it can be rescheduled elsewhere. This happens when
other pods have been scheduled to the node since the pipeline decision was made, consuming
resources that were previously in `Releasing` or `Idle` state. At this point the pipelined
reservation is stale — even after all currently releasing pods finish, there will not be
enough resources for this task on this node. Un-pipelining frees the reservation and allows
the task to fall through to the normal full-node-scan path where it may find a better fit:

```go
if len(task.NominatedNodeName) > 0 {
    if nominatedNodeInfo, ok := ssn.Nodes[task.NominatedNodeName]; ok {
        futureIdle := nominatedNodeInfo.FutureIdle()
        if task.InitResreq.LessEqual(futureIdle, api.Zero) {
            predicateNodes, fitErrors = ph.PredicateNodes(...)
        } else if task.Status == api.Pipelined && task.NodeName == nominatedNodeInfo.Name {
            stmt.UnPipeline(task)
        }
    }
}
```

### Problem 3: NominatedNodeName async race condition

`Cache.Pipeline()` sets `NominatedNodeName` on the pod via an async `UpdatePodStatus()` goroutine.
The round-trip (API server -> informer -> cache) may not complete before the next `Snapshot()`.
The next cycle's task has an empty `Pod.Status.NominatedNodeName`, defeating the nominated-node
fast-path.

**Fix**: Add `NominatedNodeName` to `TransactionContext` and set it synchronously:

```go
type TransactionContext struct {
    NodeName              string
    NominatedNodeName     string
    JobAllocatedHyperNode string
    Status                TaskStatus
}
```

- `Statement.Pipeline()` sets `task.NominatedNodeName = hostname` synchronously
- `Cache.Pipeline()` sets `task.NominatedNodeName = nodeName` on the cache task synchronously
- `NewTaskInfo()` initializes `NominatedNodeName` from `pod.Status.NominatedNodeName`
- `TaskInfo.Clone()` preserves `NominatedNodeName`
- The nominated-node fast-path reads `task.NominatedNodeName` instead of
  `task.Pod.Status.NominatedNodeName`
- `unpipeline()` and `unallocate()` clear `task.NominatedNodeName`

### Problem 4: HA cache coherence

In HA mode, each scheduler instance maintains its own cache. When Instance A pipelines a task,
Instance B never learns about it from informer events alone because `getTaskStatus()` derives
status from pod fields only: `Phase=Pending, NodeName=""` maps to `Pending`, not `Pipelined`.

**Fix**: `getTaskStatus()` in `helpers.go` reconstructs `Pipelined` status from pod metadata.
When a pod has `Phase=Pending`, `Spec.NodeName=""`, and `Status.NominatedNodeName!=""`, the
function returns `Pipelined` directly:

```go
case v1.PodPending:
    if pod.DeletionTimestamp != nil { return Releasing }
    if len(pod.Spec.NodeName) == 0 {
        if len(pod.Status.NominatedNodeName) > 0 { return Pipelined }
        return Pending
    }
    return Bound
```

`NewTaskInfo()` then assigns `nodeName = nominatedNodeName` for Pipelined tasks so the task
is placed on the correct `NodeInfo`.

This flows through all informer paths (`addPod`, `updatePod` via `deletePod` + `addPod`,
`syncTask`) because they all call `NewTaskInfo()`. The local scheduler instance is protected by
`allocatedPodInCache()` which already returns `true` for `Pipelined` tasks, causing `updatePod()`
to skip the update.

### Problem 5: Pipelined task cannot transition to Binding

`Cache.AddBindTask()` uses `node.AddTask(task)` to place the task on a node during binding. Like
`Statement.Allocate()`, this fails with "task already on node" for previously pipelined tasks.

**Fix**: Same pattern as Statement.Allocate() — check task existence on node, use `UpdateTask()`
for existing tasks and `AddTask()` for fresh ones. The original rollback semantics are preserved
for both paths.

### Affected Code (Pipeline-to-Allocate Fixes)

| File | Changes |
|------|---------|
| `pkg/scheduler/api/helpers.go` | `getTaskStatus()` returns `Pipelined` for `Phase=Pending, NodeName="", NominatedNodeName!=""` |
| `pkg/scheduler/api/job_info.go` | Add `NominatedNodeName` to `TransactionContext`, update `Clone()`, `NewTaskInfo()` assigns `nodeName = nominatedNodeName` for Pipelined tasks |
| `pkg/scheduler/framework/statement.go` | `Pipeline()` sets `NominatedNodeName`, `Allocate()` uses `UpdateTask()` for existing tasks, `unpipeline()` and `unallocate()` clear `NominatedNodeName` |
| `pkg/scheduler/cache/cache.go` | `Pipeline()` sets `task.NominatedNodeName` synchronously, `AddBindTask()` uses `UpdateTask()` for existing tasks |
| `pkg/scheduler/actions/allocate/allocate.go` | Nominated-node fast-path uses `FutureIdle()` and un-pipelines tasks that no longer fit; `predicate()`, candidate categorization, and pipeline decision use `FutureIdle()` |

## Related Issues and PRs

- [#5044](https://github.com/volcano-sh/volcano/issues/5044) — Issue describing the pipelined statement handling bugs
- [#5045](https://github.com/volcano-sh/volcano/pull/5045) — PR implementing the fix
- [#4947](https://github.com/volcano-sh/volcano/issues/4947) — Cyclic eviction issue caused by lost pipeline state
- [#4936](https://github.com/volcano-sh/volcano/pull/4936) — Earlier attempted fix that did not address the root cause
