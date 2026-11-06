# Asynchronous Pod status updates

By default, Volcano waits for ordinary unschedulable Pod conditions to be
published before closing a scheduling session. Large batches can delay the next
session while these API requests finish.

Enable background publication with the scheduler arguments:

```text
--async-pod-status-updates=true
--async-pod-status-update-qps=100
```

The second argument limits background publication attempts per second, including
failed attempts and retries, with a burst of one. It is not a limit on total
scheduler API traffic. A retry can read the current Pod before attempting its
status update. The worker count follows `--task-updater-worker-num`.

The queue keeps the latest desired condition per Pod UID. It admits up to 16,384
UIDs and an estimated 32 MiB of pending condition data. These are pending-data
admission limits, not a process-memory ceiling. If admission fails, publication
uses the synchronous path.

Before writing, the worker checks the current scheduling state and Pod identity.
Writes use resourceVersion checks; rejected attempts refresh the Pod and check
the decision again. Bound, replaced, deleting, nominated, gated and terminal
Pods are skipped. Each request has a five-second timeout. A failed intent is
retried at most five times, with exponential backoff from 100 ms to two seconds.
After retries are exhausted, its error is logged and its pending state is
released. A subsequent scheduling observation may enqueue a new intent.

Scheduling-cache reconciliation, PodGroup and Queue updates remain synchronous.
Pipelined tasks, nominations and binding errors also retain synchronous status
publication. Background attempts share per-UID locking with synchronous writes.

## Observable behavior

Ordinary Pod conditions become eventually visible. During a sustained backlog,
the rate limit can delay individual updates. Repeated observations are coalesced,
so intermediate `FailedScheduling` events may be omitted; an event is emitted
after a successful background write. This option does not change placement
constraints or the number of feasible nodes searched.

To restore synchronous publication, restart the scheduler with
`--async-pod-status-updates=false` or remove the enabling argument. Pending
background intents are not persisted across scheduler restarts; subsequent
scheduling observations regenerate applicable conditions.
