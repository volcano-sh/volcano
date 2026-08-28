# Dequeue Strategy

## Overview

`dequeueStrategy` is a per-queue setting on `Queue.spec`. It is evaluated in the **allocate** action when a job cannot allocate resources in the current scheduling cycle.

- **Job order** within a queue comes from `JobOrderFn` (e.g. `priority` plugin). Dequeue strategy does not change sorting.
- **Scope** is one scheduling cycle. Blocked jobs are retried in the next cycle; the `failedPriority` map is not persisted across cycles.

## API

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
spec:
  dequeueStrategy: traverse   # default
```

| Value | Constant | Behavior on allocation failure |
|-------|----------|--------------------------------|
| `traverse` | `DequeueStrategyTraverse` | No blocking; other jobs keep trying |
| `fifo` | `DequeueStrategyFIFO` | Skip jobs with `priority ≤` failed threshold |
| `priorityfifo` | `DequeueStrategyPriorityFIFO` | Skip jobs with `priority <` failed threshold |

Default: `traverse` when the field is empty or unset.

Priority is `job.Priority` (from `PriorityClass`; higher value = higher priority).

## Strategy Semantics

### `traverse`

No extra logic. After any job fails, the queue is re-queued and other jobs are attempted normally.

### `fifo`

**Goal**: strict ordering within a priority tier — if a job fails, nothing at the same or lower priority runs in this cycle.

When job A fails:

1. Record A's priority as the queue's **failed threshold** (keep the highest value if multiple jobs fail).
2. Re-queue the queue and continue.
3. For each subsequent job: if `job.Priority ≤ threshold`, skip it and set `FIFOBlocked` status.

Same-priority jobs are blocked even when they have smaller resource requests that could fit.

### `priorityfifo`

**Goal**: respect priority tiers without letting one oversized job block same-priority peers.

When job A fails:

1. Same threshold tracking as `fifo`.
2. Re-queue the queue and continue.
3. For each subsequent job: if `job.Priority < threshold`, skip it and set `PriorityFIFOBlocked` status.
4. Jobs with `job.Priority == threshold` may still be scheduled.

**Example** (node has 2 CPU):

| Job | Priority | Request | Result |
|-----|----------|---------|--------|
| A | 1000 | 4 CPU | Fails |
| B | 1000 | 1 CPU | `fifo`: blocked; `priorityfifo`: scheduled |
| C | 500 | 1 CPU | Both strategies: blocked |

## Implementation

Location: `pkg/scheduler/actions/allocate/allocate.go` → `allocateResources()`.

```
failedPriority := map[QueueID]*int32{}   // per-cycle, per queue

pop queue → pop job
  if fifo/priorityfifo and job matches block rule → record blocked status, re-push queue, continue
  try allocate
  if failed and strategy is fifo/priorityfifo:
      threshold = max(existing, job.Priority)
      re-push queue, continue
  re-push queue
```

Blocking check:

| Strategy | Skip when |
|----------|-----------|
| `fifo` | `job.Priority <= threshold` |
| `priorityfifo` | `job.Priority < threshold` |

Threshold update on failure: `threshold = max(existing, job.Priority)` so a later failure at a higher priority raises the bar.

## Status Reporting

Blocked jobs populate:

- `JobInfo.JobFitErrors` / `JobInfo.UnschedulableReason`
- Per-task fit errors for pending, non-gated tasks

The **gang** plugin reads `UnschedulableReason` when writing the PodGroup `Unschedulable` condition:

| Strategy | `reason` |
|----------|----------|
| `fifo` | `FIFOBlocked` |
| `priorityfifo` | `PriorityFIFOBlocked` |

Constants: `staging/src/volcano.sh/apis/pkg/apis/scheduling/types.go`.

## Tests

`pkg/scheduler/actions/allocate/allocate_priority_fifo_test.go` covers:

- `priorityfifo`: same-priority proceeds, lower-priority blocked
- `fifo`: same- and lower-priority blocked after failure

## Related Docs

User guide: [how_to_use_dequeue_strategy.md](../user-guide/how_to_use_dequeue_strategy.md)
