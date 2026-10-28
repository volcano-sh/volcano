# Rollback for Unschedulable and Unresolvable Jobs

## Background

The underlying problem raised in #4617 and #5006 can be broken down into two parts:

1. To make a quick initial decision, `enqueue` only checks the aggregate quota accounting at the queue level; it does not evaluate node-level schedulability. As a result, it may admit jobs that cannot actually be placed on any node.
2. After scheduling fails in an `allocate` cycle, the job's failure type is uniformly recorded as `Unschedulable`, and `Discard()` rolls back the temporary resource accounting within the session. However, the queue-level `inqueue quota` remains reserved across sessions. As long as the job stays in `Inqueue`, its `inqueue quota` is not released. This is intentional: an admitted job should retain its queue quota so that later jobs cannot displace it. However, if the job is actually `UnschedulableAndUnresolvable`, it will occupy quota indefinitely without ever being able to run.

It is therefore necessary to perform a more precise check for `UnschedulableAndUnresolvable` cases that are not distinguished during `allocate`. Actions such as `preempt` and `reclaim` can already observe this failure type, but they only use it to prune nodes; they do not use it to roll back the job and release its queue quota.

`Unschedulable` and `UnschedulableAndUnresolvable` represent two fundamentally different types of scheduling failure. Kubernetes explicitly distinguishes between them, and the Volcano actions mentioned above already use Kubernetes filter results for node pruning:

- **`Unschedulable`**: This usually indicates a temporary resource shortage. The job may become schedulable after resources are released or preemption is performed. Keeping such a job in `Inqueue` and preserving its quota is one of the purposes of the current design: it prevents a large gang job from being continually displaced and starved by smaller jobs arriving later. Waiting is therefore meaningful for this type of job.
- **`UnschedulableAndUnresolvable`**: This usually indicates a mismatch between inherent node properties and the Pod specification. Examples include nodes having taints that the job does not tolerate, a single Pod requesting more resources than any individual node can provide, required node affinity matching no node in the cluster, or no node being available in the zone required by a PV. Under the current cluster state, these problems cannot be resolved merely by waiting or evicting other Pods and generally require external intervention. Allowing such a job to keep occupying quota only blocks later jobs that could otherwise run.

## Goal

Distinguish job scheduling failure types more precisely and roll back jobs confirmed as `UnschedulableAndUnresolvable` from `Inqueue`, thereby releasing the queue quota they occupy without making progress. A rolled-back job can re-enter the enqueue process through mechanisms such as scheduled retries or exponential backoff, preventing it from being enqueued too frequently and repeatedly blocking other jobs.

## Proposal

Introduce a new check named `checkJobUnresolvable`:

1. **Add a feature gate**: Introduce a dedicated feature gate for `checkJobUnresolvable` and run the check only when the gate is enabled.

2. **Restrict the jobs to be checked**: Only check jobs that simultaneously satisfy `Phase == Inqueue`, `ReadyTaskNum == 0`, and `WaitingTaskNum == 0`.

   > When `allocate` ultimately fails, it calls `Discard()` to roll back the resource operations performed in the current cycle.  Therefore, if `ReadyTaskNum` > 0, at least one task has already made allocation progress. The job should therefore be treated as potentially resolvable and excluded from the `unresolvable` check.
   >
   > When `preempt` ultimately fails, it similarly calls `Discard()`. If `WaitingTaskNum` > 0, at least one task has made progress through preemption. The job should also be treated as potentially `resolvable`.
   >
   > Here, “made progress” does not mean that the job has transitioned from Inqueue to Running. Tasks may still be in the Allocated, Pipelined, or Binding state.

3. **Determine job resolvability task by task**: Evaluate the tasks in a job individually. Because Pods in the same role have identical specifications, deduplicate by role and select one representative task for each role. Reuse `PredicateNodes` to scan all nodes concurrently.

   - For an individual task, as soon as one node returns `Unschedulable`, stop scanning and treat the task as `Unschedulable`.
   - Only when every node returns `UnschedulableAndUnresolvable` should the task be treated as `UnschedulableAndUnresolvable`.
   - Only when an unresolvable task is required to meet the gang's minimum requirement should the entire job be treated as `UnschedulableAndUnresolvable`.

   **Only treat `UnschedulableAndUnresolvable` results from trusted plugins as evidence that the failure is unresolvable**.

   - **Trusted plugins**: The `UnschedulableAndUnresolvable` results returned by the following plugins are considered reliable: NodeUnschedulable, NodeAffinity, TaintToleration (excluding taints caused by a node being NotReady or under resource pressure), VolumeZone, PodTopologySpread, VolumeBinding, and nodegroup.
   - **Add a node-capacity check**: Volcano currently does not run the `NodeResourcesFit` filter, so `task.InitResreq` must be compared against the node's allocatable capacity before running the other plugins.
   - **Results from other plugins are not treated as reliable evidence** for the following reasons: scheduling may become possible after Pods on the node are evicted; the failure may recover on its own and only be unresolvable within the current session; the plugin only returns `Unschedulable` and therefore does not need to be considered; or the exact failure semantics are determined by an external service.

4. **Roll back the PodGroup phase**: During session close, recognize an unresolvable result and move the corresponding PodGroup from `Inqueue` back to `Pending`. At the same time, emit an event explaining the specific reason to the user.

   There are two possible approaches for re-enqueuing a rolled-back job:

   - **Option 1: Delayed re-enqueue (recommended)**. Retry after a specified time or use exponential backoff to control the retry interval.
   - **Option 2: Lower the priority of the unresolvable job (not recommended)**. Lowering the priority only changes the relative enqueue order among jobs; it does not limit the retry frequency of the job itself. If the queue contains no other higher-priority job, the unresolvable job may still be immediately enqueued again in the next session, only to be detected and rolled back again. This would create repeated dequeue/enqueue churn and unnecessary scheduling overhead.

Example flow:

```text
Pending
  |
  v
Enqueue
  |
  v
Inqueue
  |
  |  Allocate fails (no ready task in the current session)
  v
checkJobUnresolvable (scan all nodes)
  |                                      |
  | UnschedulableAndUnresolvable         | Unschedulable (for example, only due to temporary resource pressure)
  v                                      v
Pending (re-enqueue using the             Remain Inqueue and continue waiting
configured rollback strategy)             to be scheduled
```

## Details

### Prerequisite Change

In the predicates plugin, populate `filterStatus.Plugin` with the plugin name so that the subsequent check can filter results by plugin. For example, `filterStatus.Plugin = name` can be set in the two filter loops in `predicates.go`.

The statuses returned by Kubernetes filters currently do not include plugin names. Volcano invokes `Filter()` directly and does not go through the kube-scheduler framework path that populates the plugin name field. The predicates plugin therefore needs to populate the names first; Volcano plugins such as nodegroup that do not yet populate a plugin name need the same change. This patch is useful not only for this proposal but also for failure diagnosis, so it can be submitted separately as a prerequisite PR.

### Avoiding Duplicate Checks

`preempt` uses `FilterOutUnschedulableAndUnresolvableNodesForTask` to read existing `UnschedulableAndUnresolvable` results recorded during `allocate` and prune nodes. Similarly, for tasks already checked by `allocate` in the current session, nodes that a trusted plugin has marked as `UnschedulableAndUnresolvable` can be skipped to avoid duplicate checks.

If `preempt` is enabled, its results could also be retained and reused to further reduce duplicate work in `checkJobUnresolvable`.

### Placement

The final placement of `checkJobUnresolvable` remains open for discussion. Based on the current analysis, the strongest candidate is as follows.

Run `checkJobUnresolvable` from the gang plugin's `OnSessionClose`, inside the `!job.IsReady()` branch, for the following reasons:

1. **The trigger is a subset of the existing branch condition**: The `checkJobUnresolvable` conditions (`Phase == Inqueue`, `ReadyTaskNum == 0`, and `WaitingTaskNum == 0`) further narrow the `!job.IsReady()` condition. Empty jobs and ready jobs are already excluded by the existing logic.
2. **The result can reuse the existing condition update path**: This branch already constructs `msg` and a `PodGroupUnschedulableType` condition. The unresolvable reason produced by `checkJobUnresolvable` can be written directly into `msg`, with `UnschedulableAndUnresolvable` written into `Reason`. Running the check before constructing the condition allows the update to be completed with a single call to `UpdatePodGroupCondition`.
3. **It connects naturally to the rollback operation**: The session-close logic can move the job back to `Pending` based on the condition's `Reason`. The gang plugin is only responsible for the determination and does not operate on the API directly.

## Feasibility

This proposal requires an additional round of job/task-to-node feasibility checks, so its impact on scheduling latency is the primary feasibility concern. The existing `preempt` action provides a useful cost reference: both operations traverse target jobs, tasks, and nodes and perform predicate evaluation, but the jobs checked by `checkJobUnresolvable` are a strict subset of starving jobs, and `checkJobUnresolvable` does not need to simulate evictions on every candidate node. Its per-check cost is therefore expected to be comparable to, but generally lower than, that of `preempt`. At the same time, it releases `inqueue quota` held indefinitely by unresolvable jobs and eliminates unnecessary `preempt` work for those jobs in subsequent sessions, potentially offsetting the added checking cost.

| Comparison | `preempt` | `checkJobUnresolvable` |
| --- | --- | --- |
| Purpose | Find `Unschedulable` jobs that can be made schedulable through preemption and perform the preemption | Find `UnschedulableAndUnresolvable` jobs, roll them back, and release their `inqueue quota` |
| Jobs checked | Starving jobs | Jobs that simultaneously satisfy `Inqueue`, `ReadyTaskNum == 0`, and `WaitingTaskNum == 0`; a strict subset of starving jobs |
| Traversal logic | Evaluate each task in every target job, then evaluate nodes for each task, with early termination | Similar to `preempt`; terminate as soon as one `Unschedulable` node is found, and treat the task as `UnschedulableAndUnresolvable` only when every node is unresolvable |
| Predicate function | Call `ssn.PredicateFn` to find a task/node combination that can be made schedulable through preemption | Also call `ssn.PredicateFn`, but retain only results from trusted plugins |
| Avoiding duplicate checks | Use `FilterOutUnschedulableAndUnresolvableNodesForTask` for pruning | Reuse the same pruning results and potentially reuse `preempt` results as well |
| **Cost** | Requires traversal and eviction simulation on candidate nodes | Traverses a smaller set and performs no per-node eviction simulation; the cost is of the same order and should generally be lower than `preempt` |
| **Benefit** | Allow a high-priority job to obtain resources through preemption | Release `inqueue quota` held indefinitely by unresolvable jobs and improve queue throughput |
| **Relationship** | After `checkJobUnresolvable` rolls back an unresolvable job, subsequent sessions no longer need to attempt preemption for it | Remove jobs that cannot be helped by preemption early, reducing repeated checks in `preempt` |

### Advantages

- **Preserves existing responsibility boundaries**: `enqueue` remains responsible only for queue admission, and `allocate` remains responsible only for node allocation. The unresolvable check runs at session close and does not add serial work to the main scheduling path.
- **Handles unresolvable jobs quickly**: There is no need to wait for a timeout or use a single timeout value for both unresolvable and temporarily unschedulable cases. This proposal remains compatible with a timeout mechanism: unresolvable jobs can be rolled back immediately, while resolvable jobs that wait too long can still be handled by the timeout as a fallback. The two mechanisms are complementary.
- **Reduces unnecessary `preempt` work**: After an `UnschedulableAndUnresolvable` job is rolled back, it leaves the starving set. In subsequent sessions, `preempt` no longer needs to repeatedly scan nodes and simulate evictions for a job that cannot be made schedulable through preemption, reducing unnecessary scheduling overhead in steady state.
- **Reuses existing failure semantics**: The proposal does not introduce a new category of scheduling failure. It bases the decision on existing `UnschedulableAndUnresolvable` results from selected Kubernetes filters and Volcano plugins, together with an equivalent node-capacity check.

### Possible Concerns

- **The result is time-sensitive**: The unresolvable determination is valid only for the current cluster snapshot. Later changes, such as cluster scale-out or node label updates, may make a previously unresolvable job schedulable. Re-enqueuing with exponential backoff allows the job to recover automatically, at the cost of at most one backoff interval of additional delay. Lowering the priority directly, by contrast, may have a longer-lasting scheduling impact.
- **Adds an `Inqueue → Pending` transition to the state machine**: Controllers, monitoring systems, and other automation that depend on the PodGroup phase must be checked for compatibility with this rollback transition.
- **Burst scan volume**: Under normal conditions, the strict trigger conditions limit the number of checks. During an operational incident, however—for example, if an incorrect taint is applied to an entire GPU node pool—many `Inqueue` jobs may trigger full-node scans in the same session. The per-job cost can be compared with `preempt`, but `checkJobUnresolvable` still introduces additional duplicate work in clusters where `preempt` is already enabled. Reusing existing `allocate`/`preempt` results can reduce this duplicate computation to some extent.

## Status and Next Steps

This proposal is based on the related issues and the current source code. It currently focuses on the design and has not yet been implemented. The optimal placement of `checkJobUnresolvable`, the re-enqueue strategy, and the details of reusing `allocate`/`preempt` results still need to be refined before implementation.

The proposal extends Volcano's distinction between `Unschedulable` and `UnschedulableAndUnresolvable`, carrying the unresolvable semantics already used by `preempt` and `reclaim` into job rollback and queue-quota release. Continuing the design and implementation along this direction could address the long-standing problem of unresolvable jobs blocking queues.
