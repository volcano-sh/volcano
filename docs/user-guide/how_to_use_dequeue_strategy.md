# Dequeue Strategy

When a job in a queue fails to allocate resources, `dequeueStrategy` decides whether the scheduler keeps trying other jobs in that queue **in the same scheduling cycle**.

Job order within a queue (by `PriorityClass` or creation time) is controlled by scheduler plugins such as `priority`, not by this field.

## Configure

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: my-queue
spec:
  dequeueStrategy: traverse   # default; also: fifo, priorityfifo
```

| Value | Effect after a job fails |
|-------|--------------------------|
| `traverse` *(default)* | All other jobs in the queue keep trying |
| `fifo` | Jobs with **priority ≤** the failed job are skipped for this cycle |
| `priorityfifo` | Only jobs with **priority <** the failed job are skipped; same-priority jobs may still run |

Omit `dequeueStrategy` or set an invalid value → `traverse`.

## How the three strategies differ

Jobs are dequeued in priority order (higher `PriorityClass` value first). Suppose job A fails because it needs more resources than the cluster can offer right now:

| Strategy | Job B (same priority as A) | Job C (lower priority than A) |
|----------|---------------------------|-------------------------------|
| `traverse` | Tries to schedule | Tries to schedule |
| `fifo` | Blocked | Blocked |
| `priorityfifo` | Tries to schedule | Blocked |

`priorityfifo` is useful when a large high-priority job blocks the queue but smaller jobs at the same priority could still fit.

## Blocked jobs (`fifo` / `priorityfifo`)

Blocked jobs receive a PodGroup `Unschedulable` condition:

| Strategy | `reason` |
|----------|----------|
| `fifo` | `FIFOBlocked` |
| `priorityfifo` | `PriorityFIFOBlocked` |

```bash
kubectl get podgroup <name> -n <namespace> -o jsonpath='{.status.conditions}'
```

## Choose a strategy

| You need… | Use |
|-----------|-----|
| Highest utilization; do not hold up the queue for one failing job | `traverse` |
| Strict blocking: nothing at or below the failed job's priority runs this cycle | `fifo` |
| Honor priority tiers, but allow same-priority jobs to proceed past a failed peer | `priorityfifo` |
