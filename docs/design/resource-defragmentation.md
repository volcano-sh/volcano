# Resource Defragmentation for Volcano

## 1. Summary

GPU clusters often have enough free GPUs in total, but spread across nodes, so a pod that needs a whole node cannot be scheduled. Today the only remedies are `preempt` and `reclaim`, which kill running pods.

This proposal frees a contiguous chunk by **relocating** running pods to other nodes. No pod is killed for good and no queue loses resources.

The design is centred on a single object, the **`DefragPlan`**. A plan records which pods will move where, which space is reserved, and how far execution has progressed. Everything else (planning, reservation, execution, validation) is a role that reads or writes a plan. Which process plays which role is a packaging question, covered in section 8.

## 2. Motivation

Two 8-GPU nodes, four GPUs free on each:

```
Node A: [job1 x2][job2 x2][ free x4 ]
Node B: [job3 x3][job4 x1][ free x4 ]
```

A pending pod requests 8 GPUs. Eight GPUs are free in total, but no single node has them, so the pod waits.

- `allocate` cannot place it.
- `gangpreempt` / `gangreclaim` could only help by killing other jobs, and only if they have lower priority or exceed their queue share. Here, nobody has to lose anything.

Moving job1 and job2 from node A to node B fits everything:

```
Node A: [ pending pod x8 ]
Node B: [job3 x3][job4 x1][job1 x2][job2 x2]
```

## 3. Goals and Non-Goals

**Goals**
- Unblock pending pods that need a contiguous chunk, without killing running work.
- Plan at pod level, and respect gang semantics (`minAvailable`).
- Be safe by default: opt-in jobs, PDBs respected, and a dry-run mode.

**Non-goals**
- Live migration of running containers.
- Fractional or shared GPUs in the first version.
- General rebalancing or load-based descheduling.

## 4. Overview

```
 stuck pods ──► PLANNER ──writes──►  DefragPlan  ◄──updates── EXECUTOR ──► evictions
                                         │                    (Eviction API,
                                         │ read by              PDBs respected)
                       ┌─────────────────┴───────────────┐
                       ▼                                 ▼
                   RESERVER                          VALIDATOR
        (scheduler holds the planned space)       (read-only checks)
```

| Role | Responsibility |
|---|---|
| **Planner** | Finds stuck pods and writes a `DefragPlan`. Uses the scheduler's own simulation. |
| **Executor** | Advances a plan one step at a time and keeps its status up to date. |
| **Reserver** | Lives in the scheduler. Subtracts the plan's reserved capacity from idle resources, so other pods cannot take it. |
| **Validator** | Read-only. Re-checks every plan and reports violations. |

**Relation to preemption.** Defrag only acts on pods that stay Pending after the scheduler's own actions (`allocate`, `preempt`, `reclaim`) could not place them, so it never competes with them. It handles the case they cannot: free resources exist, but not in one place.

## 5. Detailed Design

### 5.1 The DefragPlan

A plan is the single source of truth for one relocation.

```yaml
apiVersion: scheduling.volcano.sh/v1alpha1     # proposed
kind: DefragPlan
metadata: {name: defrag-train-big-7f3a, namespace: team-a}
spec:
  dryRun: false
  ttl: 15m
  target:
    podGroup: train-big
    pods: [{name: train-big-worker-0, request: {nvidia.com/gpu: "8"}, node: node-a}]
  moves:
  - {order: 1, podGroup: job1, bundle: safe,
     pods: [{name: job1-worker-2, from: node-a, to: node-b, request: {nvidia.com/gpu: "2"}}]}
  - {order: 2, podGroup: job2, bundle: safe,
     pods: [{name: job2-worker-1, from: node-a, to: node-b, request: {nvidia.com/gpu: "2"}}]}
  reservations:
  - {node: node-a, for: [train-big-worker-0], resources: {nvidia.com/gpu: "8"}}
  - {node: node-b, for: [job1-worker-2, job2-worker-1], resources: {nvidia.com/gpu: "4"}}
status:
  phase: Executing      # Planned -> Executing -> Succeeded | Failed | Obsolete | Aborted
  currentMove: 1
```

- **The spec is immutable** once created. It says what should happen.
- **The status is written only by the executor.** It says what has happened.
- **`dryRun: true`** records the plan and leaves it `Planned`. Nothing is evicted or reserved.
- **One owner per plan.** A plan names the instance that executes it, so two instances never advance the same plan.
- **Reads and writes** go through the status subresource and never block a scheduling session.

### 5.2 Trigger

A PodGroup is a candidate when it has been Pending beyond a threshold (so the scheduler's own actions, including preemption and reclaim, could not place it) and:
- its queue has quota for it, and
- the cluster's total free resources would cover its need.

If either fails, defragmentation cannot help. Only preemption or reclaim can.

### 5.3 Planning

The planner builds a snapshot with the scheduler framework, so predicates, binpack and `TaskOrderFn` behave exactly as in the scheduler.

1. **Stuck pods.** Take the pending pods in `TaskOrderFn` order and keep the smallest prefix that satisfies `minAvailable`. Skip pods that already fit somewhere. Resource requests live on pods, so every node-level number below is per pod.
2. **Candidate nodes.** Keep a node if `free + movable >= pod request` and it passes the non-resource predicates. Rank by smallest deficit (`request - free`) and cap how many are tried.
3. **Movable sets.** Group the pods on a node by job into bundles:
   - **Safe bundle:** after the pods leave, the job still has at least `minAvailable` ready pods. The job keeps running.
   - **Whole bundle:** the job would drop below `minAvailable`. The gang breaks, so the cost is the whole job.
4. **Eligibility.** Only jobs marked relocatable, only jobs at or below the pending job's priority (configurable), and no pod blocked by a PDB.
5. **Selection.** Choose bundles that cover the deficit at the lowest cost: safe before whole, then lowest priority, then lowest restart cost.
6. **Simulation.** Free the chosen pods, place the stuck pod, then place every moved pod on another node. A plan is valid only if every moved pod finds a home.

The planner plans on top of in-flight plans: their reservations are already subtracted from the snapshot, and their evictions already count against PDB budgets.

### 5.4 Reservations

Each scheduling session, the reserver reads `Executing` plans and subtracts two pieces of capacity from idle resources for every pod except the plan's own pods:
- the freed space on the target node, held for the pending pods, and
- the destination space, held for the moved pods.

Reservations expire after the plan's TTL, and are released when the pods bind or the plan ends. A pod that binds must release its reservation at the same moment, so the space is never counted twice. Reserved space should not count as allocated to any queue.

### 5.5 Execution

The executor moves one bundle at a time:
1. Evict the bundle's pods with the Eviction API, so PDBs are respected.
2. Wait until each pod is running on its destination node.
3. Continue with the next bundle. When the target pods are scheduled, mark the plan `Succeeded` and start a cooldown.

Before every step it re-validates the plan: the target is still pending and still does not fit, and the remaining moves are still valid. If not, it stops or re-plans.

### 5.6 Concurrency

Plans run concurrently only if they cannot invalidate each other. A new plan is rejected, and re-planned later, if it overlaps an active plan on:
- a moved job (one in-flight plan per job, so gang math stays valid),
- a source node that another plan is freeing, or a node that is another plan's target, or
- a target pod or PodGroup.

A global maximum, set by the cluster owner, caps concurrent plans. Stuck pods are picked by priority, then by how long they have waited.

## 6. Safety and Failure Handling

| Situation | Behavior |
|---|---|
| Target pod gets scheduled by itself (another job ended) | Stop, mark the plan `Obsolete`, release both reservations. |
| A moved pod cannot land at its destination | Stop, mark `Failed`, release the reservations. Finished moves stay where they are. |
| The executor restarts | The plan resumes from its status. The reservations expire at the TTL if it does not. |
| PDB blocks an eviction | Pause that step, retry until the TTL, then abort. |
| Target job deleted, or its priority or quota changes | The pre-step check aborts the plan. |
| Another job tries to use the freed space | Blocked by the reservation. |

Other guards: relocation is opt-in per job, the number of concurrent plans is capped, and every plan is followed by a cooldown.

## 7. Rollout

Behind a flag, off by default, enabled per queue or namespace, with a kill switch that stops new plans and releases all reservations. Each phase widens what the system may do only after the previous one has proven its plans correct.

1. **Dry-run.** Generate `DefragPlan` objects with `dryRun: true`. No evictions and no reservations. The validator re-checks every plan (invariants, replay on a fresh snapshot, whether the pod scheduled anyway).
2. **Safe bundles.** Enable execution for safe bundles only, with reservations. One plan at a time.
3. **Whole bundles.** Enable whole bundles, then gangs and topology domains. One plan at a time. The validator keeps running.
4. **Plan concurrency.** Configurable, capped by a maximum set by the cluster owner.

Gating signals for every phase: validator violations (must be zero), moved pods that fail to land, reservation leaks, PDB-blocked evictions, and jobs restarted per plan.

## 8. Packaging: standalone component or scheduler action

The design above does not depend on which process plays the planner, executor and reserver roles. There are two ways to package them.

### Option A: standalone component (`volcano-defrag`)

A leader-elected controller is the planner, executor and validator. It embeds the scheduler framework to plan. A small scheduler plugin is the reserver.

- **For:**
  - Long-running work (terminate, restart, verify) stays out of the scheduling loop.
  - Planning cost never slows a scheduling cycle.
  - It can be enabled, scaled and fail independently of the scheduler.
  - It matches the roadmap wording of a new component.
- **Against:**
  - A new binary, with RBAC, Helm and operations to maintain.
  - The planner works on its own snapshot, which can drift from the scheduler's live view.
  - Two code paths that must stay consistent, and a scheduler plugin is still needed.

### Option B: scheduler action (`defrag`)

An action in the scheduler is the planner, executor and reserver. It creates and updates the `DefragPlan`, and takes one step per scheduling session, so a plan finishes over several sessions. A read-only watcher is the validator.

- **For:**
  - Planning runs inside the real session, with the exact predicates and state, and no snapshot to drift.
  - No new binary, leader election or deployment, and the scheduler's single-threaded loop makes concurrency simple.
  - `preempt` and `gangpreempt` already evict from inside a session and finish over later cycles.
- **Against:**
  - Planning cost sits in the hot path, so it must be time-boxed and rate-limited.
  - The scheduler becomes a status writer for a new object, and must keep those writes off the hot path and free of conflicts, particularly with multiple schedulers or shards.
  - Progress depends on the scheduler's cadence and restarts.

## 9. Preference

I prefer **Option B**, the scheduler action: it plans inside the real scheduling session and uses the `DefragPlan` object to carry progress across sessions, with no new binary to run.
