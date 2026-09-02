# Fair share scheduling

This document covers two related design proposals for fair share scheduling among
tenants sharing a `Queue`:

1. [Namespace fair share](#namespace-fair-share-2019) (2019, [@lminzhw](http://github.com/lminzhw)) —
   static per-namespace weights recorded on `ResourceQuota`, compared via a new
   `NamespaceOrderFn` scheduling-loop stage.
2. [Per-namespace fair share with decayed usage tracking](#per-namespace-fair-share-with-decayed-usage-tracking-2026)
   (2026) — a self-contained scheduler plugin that tracks historical resource-seconds per
   namespace with exponential decay, addressing the same underlying problem without
   requiring new `Session`/`SchedulerCache` fields or a `NamespaceOrderFn` stage.

For usage instructions (arguments, configuration examples, interaction with other plugins),
see the [fairshare user guide](../user-guide/how_to_use_fairshare_plugin.md).

---

## Namespace fair share (2019)

[@lminzhw](http://github.com/lminzhw); May 8, 2019

### Motivation

`Queue` was introduced in [kube-batch](http://github.com/kubernetes-sigs/kube-batch) to share resources among users.

But, the user in the same `Queue` are equivalent during scheduling. For example, we have a `Queue` contains a small amount of resources, and there are 10 pods belong to UserA and 1000 pods belong to UserB. In this case, pods of UserA would have less probability to bind with node.

So, we need a more fine-grained strategy to balance resource usage among users in the same `Queue`.

In consideration of multi-user model in kubernetes, we use namespace to distinguish different user. Each namespace would have its weight to control resources usage.

### Function Specification

Weight have these features:
> 1. `Queue` level
> 2. an `integer` with default value 1
> 3. record in namespace `quota`
> 4. higher value means more resources after balancing

#### where is the weight

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  namespace: default
spec:
  hard:
    limits.memory: 2Gi
    volcano.sh/namespace.weight: 1  <- this field represent the weight of this namespace
```

If many `ResourceQuota` in the same namespace have weight, the weight for this namespace is the highest one of them.

This weight should be positive, any invalid value is treated as default value 1.

### Scheduler Framework

Introduce two new fields in SchedulerCache

```go
type NamespaceInfo struct {
    Weight int
}

type SchedulerCache struct {
    ...
    quotaInformer    infov1.ResourceQuotaInformer
    ...
    NamespaceInfo  map[string]*kbapi.NamespaceInfo
    ...
}
```

The Scheduler will watch the lifecycle of `ResourceQuota` by `quotaInformer`, and refresh the info in `NamespaceInfo`.

In `openSession` function, we should pass the `NamespaceInfo` through function `cache.Snapshot` into `Session` by using a new filed in `Session`/`ClusterInfo` struct.

```go
type Session struct {
    ...
    NamespaceInfo  map[string]*kbapi.NamespaceInfo
    ...
}
type ClusterInfo struct {
    ...
    NamespaceInfo  map[string]*kbapi.NamespaceInfo
    ...
}
```

### Allocate Action

#### Scheduling loop

The behavior of `allocate` action is scheduling job in `Queue` one by one.

At the beginning of scheduling loop, it will take a job with highest priority from `Queue`. And try to schedule tasks that belong to it until job is ready (matches the minMember) then go to next round.

The priority of job mentioned above is defined by `JobOrder` functions registered by plugins. Such as job ready order of Gang plugin, priority order of Priority plugin, and also the share order of DRF plugin.

#### JobOrder

Namespace weight `should not` implement with JobOrder func. Because the scheduling of job would affect priority of the others.

> e.g.
>
> ns1 has job1, job2, ns2 has job3, job4. The original order is job1-job2-job3-job4.
>
> After the scheduling of job1, right order should be job3-job4-job2. But in priority queue, we have no chance to fix the priority for job2

#### Namespace Order

To add namespace weight, we introduce a new order function named `NamespaceOrder` in `Session`.

```go
type Session struct {
    ...
    NamespaceOrderFns map[string]api.CompareFn
    ...
}
```

The scheduling loop in allocate should change as follows.

In scheduling loop, firstly, choose a namespace having highest priority by calling `NamespaceOrderFn`, and then choose a job having highest priority using `JobOrderFn` in this namespace.

After scheduling of job, push the namespace and job back to priority queue in order to refresh its priority. Because once a job is scheduled, assigned resource may decrease the priority of this namespace, the other jobs in the same namespace may be scheduled later.

Always assigning resources to namespace with highest priority (lower resource usage) in every turn will make the resource balanced.

### DRF plugin

DRF plugin use preemption and order of job to balance resource among jobs. The `share` in this plugin is defined as resource usage, the higher `share` means this job occupies the more resource now.

#### Namespace Compare

To introduce namespace weight into this plugin, we should define how to compare namespace having weight firstly.

For namespace n1 having weight w1 and namespace n2 having weight w2, we can compute the `share` of resource and recorded as u1 and u2. Now, the resource usage of n1 less than n2 can be defined as (u1 / w1 < u2 / w2)

`e.g.` ns1 having weight w1=2 use 6cpu, ns2 having weight w2=1 use 2cpu. In the scope of cpu, the ns1 use less resource than ns2. (6 / 3 < 3 / 1)

#### Namespace Order

Register `NamespaceOrder` function using the strategy mentioned above.

#### preemption

> The `preempt` action is disabled now. Do this later.

In the `preemption` function now, strategy is just simply comparing the resource share among jobs .

After adding namespace weight, we should check namespace of preemptor and preemptee firstly. The job in namespace with less resources can preempt others, or when namespace resource usage are the same, compare share of job instead.

### Feature Interaction

#### preempt action

Preempt is a strategy set to choose victims and finally evict it.

The way to choose victims is a function set named `Preemptable` registered by plugins. Such as job ready protection of Gang plugin, special pod protection of Conformance plugin, job share balance strategy of DRF plugin.

All these plugin would choose some victims respective, and the intersection of them would be the final victim set. So, the choice made by DRF plugin would never break the requirement of others.

### short hand

1. Preempt may cause killing of some running pod.

### Cases:

- cluster have __16 cpu__, queue and namespace have default weight.

    | queue | namespace | requested | queue assigned | namespace assigned |
    | ----- | --------- | --------- | -------------- | ------------------ |
    | q1    | ns1       | 5 cpu     | 8 cpu          | 4 cpu              |
    |       | ns2       | 10 cpu    |                | 4 cpu              |
    | q2    | ns3       | 10 cpu    | 8 cpu          | 6 cpu              |
    |       | ns4       | 2 cpu     |                | 2 cpu              |

- cluster have __16 cpu__, q1 with weight 1, q2 with weight 3. ns1 with weight 3, ns2 have weight 1, ns3 have weight 2, ns4 have weight 6.

    | queue | namespace | requested | queue assigned | namespace assigned |
    | ----- | --------- | --------- | -------------- | ------------------ |
    | q1 w1 | ns1 w3    | 5 cpu     | 4 cpu          | 3 cpu              |
    |       | ns2 w1    | 10 cpu    |                | 1 cpu              |
    | q2 w3 | ns3 w2    | 10 cpu    | 12 cpu         | 10 cpu             |
    |       | ns4 w6    | 2 cpu     |                | 2 cpu              |

- cluster have __16 cpu__, q1 with weight 1, q2 with weight 3. ns1 have weight 2, ns2 have weight 6.

    | queue | namespace | requested | queue assigned | namespace assigned |
    | ----- | --------- | --------- | -------------- | ------------------ |
    | q1 w1 | ns1 w2    |           | 4 cpu          |                    |
    | q2 w3 | ns1 w2    | 5 cpu     | 12 cpu         | 3 cpu              |
    |       | ns2 w6    | 20 cpu    |                | 9 cpu              |

---

## Per-namespace fair share with decayed usage tracking (2026)

### Background

Volcano's existing DRF plugin provides dominant resource fairness at the namespace/queue level,
but it only considers the **current allocation snapshot** — it has no memory of past usage.
This leads to a well-documented problem in multi-tenant GPU clusters (see [#4165](https://github.com/volcano-sh/volcano/issues/4165)):

- Namespace A submits hundreds of GPU jobs and fills the cluster
- Namespace B arrives later with a handful of jobs
- As namespace A's jobs complete, its new pending jobs immediately regain priority
  (they have equal or lower current allocation), effectively starving namespace B

This "submission-order bias" means the first tenant to flood the queue monopolizes resources
indefinitely, even when other tenants have legitimate demand. SLURM solves this with its
fair share algorithm that tracks historical usage with exponential decay.

This is the same underlying gap the 2019 [Namespace fair share](#namespace-fair-share-2019)
proposal above targeted, but that design requires new `NamespaceOrderFn` scheduling-loop
stages and `NamespaceInfo` plumbing through `SchedulerCache`/`Session`/`ClusterInfo` that were
never landed. The proposal below is intentionally scoped to a single self-contained plugin —
no framework changes — trading the static, admin-configured `ResourceQuota` weight for a
usage-based measure that adapts automatically as tenants' consumption rises and falls.

It's also worth calling out an assumption both designs share and that's worth being explicit
about: Volcano already supports fair sharing *across* queues (via DRF/proportion at the queue
level). Both the 2019 proposal and this one are about fair sharing **within** a single queue,
for the case — common when tenants map to namespaces rather than to dedicated queues — where
multiple tenants submit jobs into the same queue and need to be balanced against each other
there.

### Proposal

Add a new `fairshare` scheduler plugin that tracks cumulative resource-seconds per namespace
and applies exponential half-life decay so that past consumption is gradually forgiven.

### Namespace identity

Tenants are identified by their job's **namespace** directly — there is no separate "user"
concept, since not every cluster maps individual users to namespaces one-to-one (a namespace
may itself represent a team or project shared by several users). No additional labels are
required.

### Algorithm

Each scheduling cycle (~1 second):

1. **Decay** all historical usage: `usage × 2^(-elapsed / halfLife)`
2. **Accumulate** running usage: for each allocated task, add `resource_count × elapsed_seconds`
   to the namespace's cumulative total
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

A namespace that consumed 10 GPU-hours will see its usage penalty halve every 4 hours of
inactivity. After 24 hours, the penalty is effectively forgotten.

### State persistence

Volcano recreates plugin instances via `New()` every scheduling cycle, so instance-level
state is lost between cycles. The fairshare plugin uses package-level globals protected
by a `sync.Mutex` to persist usage history across scheduling cycles:

```go
var (
    globalMu        sync.Mutex
    globalUsage     = make(map[string]map[string]float64) // [queue][namespace] → resource-seconds
    globalLastCycle time.Time
)
```

#### Durable persistence (ConfigMap)

To survive scheduler restarts, the plugin can optionally persist state to a ConfigMap.
When `fairshare.persistState` is set to `"true"`:

1. On the **first scheduling cycle**, a `sync.Once` block loads any existing state from
   the ConfigMap into `globalUsage` / `globalLastCycle`.
2. **`OnSessionClose`** (called once per scheduling cycle, ~1s) flushes the current state
   to the ConfigMap whenever at least `flushIntervalSeconds` has elapsed since the last
   flush (default: 30 seconds). Writes use the ConfigMap's `resourceVersion` for
   optimistic concurrency. Tying the flush to the session lifecycle rather than a
   detached goroutine+ticker means persistence stops the moment the plugin stops being
   invoked (e.g. removed from the tier list via a config hot-reload) — a ticker would
   otherwise keep writing stale state forever with no shutdown hook.
3. On restart, the loaded `globalLastCycle` is used to compute the elapsed time and apply
   the correct decay, so namespaces are not unfairly penalized or forgiven by the downtime.

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
          "team-a": 12345.67,
          "team-b": 8901.23
        }
      }
    }
```

**Design considerations:**

- **Leader election**: Volcano already elects a single active scheduler. Only the leader writes.
- **Data loss window**: At most `flushInterval` seconds of usage data is lost on a crash.
  With the default 30-second interval and a 4-hour half-life, this is negligible.
- **Size**: Even with 1000 namespaces across 50 queues, the JSON payload is ~50 KB — well
  within the 1 MB ConfigMap limit.
- **Backward compatibility**: Persistence is disabled by default. Existing deployments are
  unaffected.

### Scheduler hooks

| Hook | Purpose |
|------|---------|
| `JobOrderFn` | Orders jobs by cumulative usage (lower wins), with running-resource tiebreaker |
| `JobEnqueueableFn` | (Optional) Blocks namespaces at/above their max-min fair share from the scheduling pipeline |
| `EventHandler` | Tracks allocations/deallocations in real-time during the scheduling cycle |

### Testing

#### Unit tests (49 tests)

- Max-min fair share algorithm correctness (single namespace, equal demand, asymmetric demand, progressive elimination)
- Decay factor math (one/two half-lives, zero elapsed, zero half-life, small elapsed)
- `decayAllUsage` (halves after one half-life, cleans up negligible entries, multi-queue)
- Usage ordering (lower usage wins, equal usage falls to running tiebreaker, realistic multi-namespace scenario)
- Decay scenario (10-hour job decay over 4h and 24h)
- Helpers (namespace extraction, resource key defaults/overrides)
- `targetQueues` allowlist behavior (defaults to all queues when unset, restricts to the allowlist when set)
- `shouldAbstainOrdering` (abstains unless both jobs are in the same targeted queue)
- A failed persistence flush is retried on the next cycle instead of waiting a full `flushIntervalSeconds`
- Persistence: flush creates ConfigMap, flush updates existing, load populates globals,
  load handles missing ConfigMap, load handles empty data, flush→load round-trip,
  disabled persistence is no-op, corrupt JSON returns error
- `maybeFlush` rate limiting (disabled is a no-op, first call flushes immediately, a second
  call within the interval is skipped, flushes again once the interval elapses)

#### Integration tests

Validated on a test cluster with 2 GPU nodes and 4 tenant namespaces:

1. **Without decay tracking**: FIFO behavior, last tenant waited ~7 minutes
2. **With decay tracking**: All tenants got GPUs within ~2 minutes, scheduling rotated between tenants
3. **Burst asymmetry** (1 tenant = 8 jobs, others = 1 each): Minority tenants' jobs completed within ~2 minutes
4. **DAG/workflow simulation**: GPU steps interleaved across tenants by cumulative usage
5. **PriorityClass interaction**: High-priority bypassed fair share as expected
6. **Scheduler restart**: Running jobs survived, new jobs scheduled fairly post-restart

### Limitations

- Without `persistState`, state is in-memory only and lost on scheduler restart
- Namespace-based identity only; no support for arbitrary custom labels (can be extended)
- ConfigMap persistence has a small data-loss window equal to the flush interval on crashes
- Fairness is scoped within a queue (as in the 2019 proposal above); this plugin does not
  change how DRF/proportion balance resources across queues
- `fairshare` registers no `PreemptableFn`/`ReclaimableFn`; it only reorders the pending
  queue and (optionally) gates enqueue. It cannot preempt already-running jobs, so a
  namespace with long-lived running jobs keeps its resources regardless of how stale its
  usage penalty becomes
