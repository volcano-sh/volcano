# Per-User Fair Share Scheduling with Decayed Usage Tracking

## Background

Volcano's existing DRF plugin provides dominant resource fairness at the namespace/queue level,
but it only considers the **current allocation snapshot** — it has no memory of past usage.
This leads to a well-documented problem in multi-user GPU clusters (see [#4165](https://github.com/volcano-sh/volcano/issues/4165)):

- User A submits hundreds of GPU jobs and fills the cluster
- User B arrives later with a handful of jobs
- As User A's jobs complete, User A's new pending jobs immediately regain priority
  (they have equal or lower current allocation), effectively starving User B

This "submission-order bias" means the first user to flood the queue monopolizes resources
indefinitely, even when other users have legitimate demand. SLURM solves this with its
fair share algorithm that tracks historical usage with exponential decay.

## Proposal

Add a new `fairshare` scheduler plugin that tracks cumulative resource-seconds per user
(identified by namespace) and applies exponential half-life decay so that past consumption
is gradually forgiven.

### User identity

Users are identified by their job's **namespace**. This follows the namespace-per-user
pattern common in multi-tenant Kubernetes GPU clusters. No additional labels are required.

### Algorithm

Each scheduling cycle (~1 second):

1. **Decay** all historical usage: `usage × 2^(-elapsed / halfLife)`
2. **Accumulate** running usage: for each allocated task, add `resource_count × elapsed_seconds`
   to the user's cumulative total
3. **Order** pending jobs via `JobOrderFn`:
   - **Primary**: Lower cumulative usage wins (with a 1.0 resource-second epsilon for float comparison)
   - **Secondary**: Fewer currently-running resources wins (within-cycle tiebreaker)

### Half-life decay

| Time since usage | Remaining weight (4h half-life) |
|-----------------|--------------------------------|
| 0 hours         | 100%                           |
| 4 hours         | 50%                            |
| 8 hours         | 25%                            |
| 12 hours        | 12.5%                          |
| 24 hours        | 1.6%                           |

A user who consumed 10 GPU-hours will see their usage penalty halve every 4 hours of
inactivity. After 24 hours, the penalty is effectively forgotten.

### State persistence

Volcano recreates plugin instances via `New()` every scheduling cycle, so instance-level
state is lost between cycles. The fairshare plugin uses package-level globals protected
by a `sync.Mutex` to persist usage history across scheduling cycles:

```go
var (
    globalMu        sync.Mutex
    globalUsage     = make(map[string]map[string]float64) // [queue][user] → resource-seconds
    globalLastCycle time.Time
)
```

#### Durable persistence (ConfigMap)

To survive scheduler restarts, the plugin can optionally persist state to a ConfigMap.
When `fairshare.persistState` is set to `"true"`:

1. On the **first scheduling cycle**, a `sync.Once` block loads any existing state from
   the ConfigMap into `globalUsage` / `globalLastCycle`, then starts a background goroutine.
2. The **background goroutine** periodically flushes the current state to the ConfigMap
   (default: every 30 seconds). Writes use the ConfigMap's `resourceVersion` for
   optimistic concurrency.
3. On restart, the loaded `globalLastCycle` is used to compute the elapsed time and apply
   the correct decay, so users are not unfairly penalized or forgiven by the downtime.

The ConfigMap is stored in the scheduler's namespace (default: `volcano-system`):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: fairshare-usage-state
  namespace: volcano-system
  labels:
    app: volcano-scheduler
    component: fairshare
data:
  state.json: |
    {
      "lastCycle": "2026-04-07T12:00:00Z",
      "queues": {
        "gpu-queue": {
          "alice": 12345.67,
          "bob": 8901.23
        }
      }
    }
```

**Design considerations:**

- **Leader election**: Volcano already elects a single active scheduler. Only the leader writes.
- **Data loss window**: At most `flushInterval` seconds of usage data is lost on a crash.
  With the default 30-second interval and a 4-hour half-life, this is negligible.
- **Size**: Even with 1000 users across 50 queues, the JSON payload is ~50 KB — well within
  the 1 MB ConfigMap limit.
- **Backward compatibility**: Persistence is disabled by default. Existing deployments are
  unaffected.

## Configuration

```yaml
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
```

### Arguments

| Argument | Default | Description |
|----------|---------|-------------|
| `fairshare.targetQueues` | _(required)_ | Comma-separated queue names to apply fair share to |
| `fairshare.resourceKey` | `nvidia.com/gpu` | Default resource to track |
| `fairshare.resourceKey.<queue>` | _(none)_ | Per-queue resource override (e.g., `amd.com/gpu`, `cpu`) |
| `fairshare.halfLifeMinutes` | `240` | Half-life for usage decay in minutes |
| `fairshare.enableEnqueueGate` | `false` | When `true`, blocks users at/above their calculated share from entering the scheduling pipeline |
| `fairshare.persistState` | `false` | When `true`, persists usage state to a ConfigMap so it survives scheduler restarts |
| `fairshare.stateNamespace` | `volcano-system` | Namespace for the state ConfigMap |
| `fairshare.stateConfigMap` | `fairshare-usage-state` | Name of the state ConfigMap |
| `fairshare.flushIntervalSeconds` | `30` | How often to flush state to the ConfigMap (in seconds) |

## Interaction with existing plugins

### priority plugin

The `priority` plugin runs before `fairshare` in the same tier. A higher PriorityClass
always wins. Fair share only breaks ties at equal priority levels.

When all competing jobs use the same PriorityClass, fair share is fully effective as the
tiebreaker. This means using a high PriorityClass only helps when others don't — if everyone
uses it, fair share still distributes resources equitably.

### DRF plugin

The fairshare plugin is complementary to DRF. DRF handles multi-resource dominance
(which resource is the bottleneck), while fairshare handles temporal fairness (who has
consumed more historically). They can coexist or be used independently depending on
cluster needs.

### gang plugin

No interaction — gang scheduling handles minimum member requirements, while fairshare
handles ordering among eligible jobs.

## Scheduler hooks

| Hook | Purpose |
|------|---------|
| `JobOrderFn` | Orders jobs by cumulative usage (lower wins), with running-resource tiebreaker |
| `JobEnqueueableFn` | (Optional) Blocks users at/above their max-min fair share from the scheduling pipeline |
| `EventHandler` | Tracks allocations/deallocations in real-time during the scheduling cycle |

## Logging

| klog level | What it shows |
|------------|---------------|
| V(2) | Plugin config on creation, per-cycle queue summary (user count, running, demand) |
| V(3) | Decay factor per cycle, fair shares, usage maps, ordering winner decisions |
| V(4) | Session open/close, individual allocate/deallocate events |
| V(5) | Every job comparison, every enqueue evaluation |

## Testing

### Unit tests (33 tests)

- Max-min fair share algorithm correctness (single user, equal demand, asymmetric demand, progressive elimination)
- Decay factor math (one/two half-lives, zero elapsed, zero half-life, small elapsed)
- `decayAllUsage` (halves after one half-life, cleans up negligible entries, multi-queue)
- Usage ordering (lower usage wins, equal usage falls to running tiebreaker, realistic multi-user scenario)
- Decay scenario (10-hour job decay over 4h and 24h)
- Helpers (namespace extraction, resource key defaults/overrides)
- Persistence: flush creates ConfigMap, flush updates existing, load populates globals,
  load handles missing ConfigMap, load handles empty data, flush→load round-trip,
  disabled persistence is no-op, corrupt JSON returns error

### Integration tests

Validated on a test cluster with 2 GPU nodes and 4 user namespaces:

1. **Without decay tracking**: FIFO behavior, last user waited ~7 minutes
2. **With decay tracking**: All users got GPUs within ~2 minutes, scheduling rotated between users
3. **Burst asymmetry** (1 user = 8 jobs, others = 1 each): Minority users' jobs completed within ~2 minutes
4. **DAG/workflow simulation**: GPU steps interleaved across users by cumulative usage
5. **PriorityClass interaction**: High-priority bypassed fair share as expected
6. **Scheduler restart**: Running jobs survived, new jobs scheduled fairly post-restart

## Limitations

- Without `persistState`, state is in-memory only and lost on scheduler restart
- Namespace-based identity only; no support for arbitrary user labels (can be extended)
- ConfigMap persistence has a small data-loss window equal to the flush interval on crashes
