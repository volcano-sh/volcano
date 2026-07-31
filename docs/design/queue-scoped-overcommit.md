# Queue-Scoped Overcommit Admission

@[miantalha45](https://github.com/miantalha45)

## Status

Proposal for [issue #5687](https://github.com/volcano-sh/volcano/issues/5687).

## Motivation

The `overcommit` plugin currently uses one cluster-wide `overcommit-factor` to
decide whether a PodGroup may enter the `Inqueue` phase. This is a safe global
admission boundary, but it applies the same admitted backlog policy to every
queue.

Clusters can have queues with different workload characteristics. For example,
a production queue may need a small and predictable admitted backlog, while a
batch queue can tolerate more admitted jobs waiting for allocation. Today,
operators cannot express that distinction without changing the cluster-wide
factor for every queue.

Allowing a queue to bypass the cluster-wide factor would be unsafe. Queue
settings must therefore add a stricter queue-level admission check while the
existing global check remains the authoritative cluster-wide protection.

## Goals

- Preserve the current global `overcommit-factor` behavior by default.
- Let cluster administrators opt in to an additional admission limit for an
  individual Queue.
- Bound every queue factor by administrator-controlled and cluster-wide limits.
- Respect queue `deserved`, `guarantee`, `capability`, and hierarchical Queue
  policies without changing their existing allocation or reclaim semantics.
- Keep enqueue-time work proportional to queue depth and requested resource
  dimensions, not to the number of queues or jobs in the cluster.

## Non-Goals

- Replacing the global `overcommit-factor` with queue settings.
- Changing queue capability, deserved, guarantee, allocation, reclaim, or
  preemption behavior.
- Adding a field to `QueueSpec` or introducing a new Queue CRD version.
- Adding field-level RBAC or a Queue subresource in the first implementation.
- Applying queue-scoped overcommit to DRA DeviceClass resources. This design
  covers the `ResourceList` represented by PodGroup `minResources`.

## Background

At session open, the current overcommit plugin computes one global admission
budget:

```text
global admission budget = cluster total resources * global overcommit factor
```

It subtracts resources already used by nodes and tracks resources reserved by
admitted jobs in a session-local `inqueueResource` counter. A new job can enter
`Inqueue` when:

```text
used resources + global inqueue resources + job minResources
    <= cluster total resources * global overcommit factor
```

The Capacity plugin currently calculates queue-level `realCapability` and
maintains `allocated`, `inqueue`, `capability`, `deserved`, and `guarantee`
state. When hierarchy is enabled, it propagates resource changes from a leaf
Queue to its ancestors. This proposal keeps overcommit's admission state owned
by the overcommit plugin while moving the effective-capability calculation into
shared scheduler code.

## User Configuration

### Feature Gate

The feature is controlled by the alpha `QueueScopedOvercommit` feature gate and
is disabled by default:

```text
--feature-gates=QueueScopedOvercommit=true
```

The scheduler and webhook-manager must both receive the same feature-gate
setting. The scheduler enforces the admission behavior, the webhook validates
Queue annotations.

When the feature gate is disabled:

- Existing scheduling behavior is unchanged.
- The scheduler ignores the queue-scoped annotation.
- The webhook rejects a Queue create or an update that adds or changes the
  annotation.
- A Queue that already has an unchanged annotation may be updated so that an
  administrator can remove the annotation while the gate is disabled.

### Queue Annotation

An administrator opts a Queue into queue-scoped admission with the following
annotation:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: batch
  annotations:
    volcano.sh/overcommit-factor: "1.5"
spec:
  deserved:
    cpu: "40"
    memory: "160Gi"
  capability:
    cpu: "80"
    memory: "320Gi"
```

`volcano.sh/overcommit-factor` is intentionally an annotation, not
a `QueueSpec` field. This keeps the experimental configuration out of the
versioned Queue API until the behavior has matured.

The annotation value is a finite decimal factor greater than or equal to `1`.
The Queue webhook validates this value when the feature gate is enabled.

### Scheduler Plugin Configuration

The existing global plugin argument remains required for the global check. A
new scheduler-controlled argument caps all Queue annotations:

```yaml
actions: "enqueue,allocate,backfill"
tiers:
- plugins:
  - name: overcommit
    arguments:
      overcommit-factor: 1.2
      max-queue-overcommit-factor: 1.2
```

`max-queue-overcommit-factor` defaults to the global `overcommit-factor` when
it is omitted. This conservative default preserves the familiar global factor
as the initial Queue-factor ceiling. An administrator may explicitly configure
a larger Queue maximum without increasing the global admission budget. The
scheduler configuration validator must reject a non-finite value or a value
below `1`.

## Admission Semantics

### Global Check

For every job entering `Inqueue`, the existing global check always runs first:

```text
used + global inqueue + job minResources
    <= cluster total resources * global overcommit factor
```

No Queue annotation can permit a job that fails this check.

### Effective Queue Factor

For a Queue with the annotation, the effective factor is:

```text
min(
  Queue annotation factor,
  max-queue-overcommit-factor
)
```

The global overcommit factor is deliberately not part of this calculation. It
limits the cluster's total admitted workload, while the effective Queue factor
limits a single Queue relative to its deserved resources. These factors have
different bases and therefore represent separate policies.

The scheduler-controlled maximum lets an administrator reduce the largest
factor that any Queue may request. A Queue factor greater than the global
factor is safe because the global admission check still runs for every job and
cannot be bypassed.

### Effective Queue Budget

For each resource dimension explicitly configured in `Queue.spec.deserved`,
the queue-level admission budget is:

```text
min(realCapability, max(Queue.spec.deserved, Queue.spec.guarantee.resource) * effective queue factor)
```

The terms have the following meaning:

- `Queue.spec.deserved` is the Queue's configured soft share.
- `Queue.spec.guarantee.resource` is the Queue's protected minimum.
- `max(Queue.spec.deserved, Queue.spec.guarantee.resource)` is the static
  effective deserved value used by this feature.
- `realCapability` is the existing effective hard limit after capability and
  guarantee policy are applied. A missing configured capability uses the
  existing derived limit, no additional capability field is introduced.

Queue-scoped overcommit deliberately does not use the dynamic `deserved` value
calculated by the Proportion plugin. That value changes with active Queue
weights, requests, and available cluster resources in each scheduling session.
Using it for admission would make a Queue's admission budget unstable and make
this feature depend on the Proportion plugin and its initialization order.

The Queue `weight` remains owned by Proportion and continues to affect fair
allocation ordering only. The queue-scoped overcommit factor affects only
admission against the Queue's explicitly configured resource policy.

#### Shared Effective-Capability Helper

The implementation will extract the current Capacity-plugin calculation into a
shared, read-only scheduler helper. The helper accepts the resource budget and
Queue policy inputs needed for the current calculation, including the relevant
parent or cluster capacity, configured Queue capability, the Queue guarantee,
and the guarantees of sibling Queues.

The Capacity plugin will call this helper for its existing `realCapability`
calculation, preserving its current behavior. The overcommit plugin will call
the same helper when queue-scoped overcommit is enabled. Overcommit must not
read Capacity's private `queueAttr` state or rely on the Capacity plugin being
enabled or initialized before it.

This makes `realCapability` a shared Queue-policy calculation rather than a
value owned by one plugin. It also ensures that a Queue with no configured
`spec.capability` receives the same derived limit in both plugins.

### Missing Resource Dimensions

Queue-scoped overcommit only applies to resource dimensions explicitly covered
by a positive `Queue.spec.deserved` value. A guarantee can increase the budget
for such a dimension, but cannot introduce a new queue-scoped admission
dimension by itself.

```text
effective deserved = max(Queue.spec.deserved, Queue.spec.guarantee.resource)
```

If CPU is configured in `Queue.spec.deserved` but GPU is omitted, CPU receives
the queue-level admission limit and GPU does not, even if the Queue has a GPU
guarantee. The global check still applies to both CPU and GPU.

This rule prevents an omitted resource dimension from accidentally becoming a
zero queue budget:

```text
min(realCapability, 0 * factor) = 0
```

A Queue may set the annotation only when `spec.deserved` is non-empty and has
at least one positive resource quantity. The webhook rejects invalid
annotation/Queue combinations. A Queue with no annotation keeps the existing
global-only behavior.

### Queue Check

After the global check succeeds, a job in an annotated Queue must satisfy:

```text
queue allocated + queue inqueue + job minResources
    <= effective queue budget
```

The comparison examines only resource dimensions covered by that Queue's
effective deserved resources and requested by the job. If the queue-level
check fails, the scheduler keeps the PodGroup pending and records an event
identifying the constrained Queue and resource dimensions.

## Hierarchical Queues

When hierarchy is enabled for the overcommit plugin, the scheduler evaluates
the job's leaf Queue and then each ancestor that has the overcommit annotation.

```text
root
 └── research
      └── batch
           └── training-job
```

For a job in `batch`, the scheduler always runs the global check. It also runs
queue checks for `batch`, `research`, and `root` only when each Queue explicitly
sets `volcano.sh/overcommit-factor`.

The root Queue is not implicitly given a queue-scoped budget. If an
administrator explicitly annotates it, it is checked like any other annotated
Queue. The global check remains active in all cases.

Only leaf Queues schedule jobs when the existing hierarchical Queue behavior is
enabled. Queue-scoped overcommit follows that same parent chain and does not
change the leaf-only scheduling rule.

## Accounting and Scheduling Cost

The overcommit plugin maintains its own session-local state:

```text
queue admission state
├── effective factor and budget for annotated queues
├── allocated resources for each relevant queue
├── inqueue resources for each relevant queue
└── parent chain for each relevant leaf queue
```

At session open, the plugin builds this state from the Queue tree and existing
jobs. For every existing job, its allocated and inqueue contribution is added
to its leaf Queue and propagated to ancestors. The same rules used by the
current global `inqueueResource` calculation apply, including deduction of
scheduling-gated resources and prevention of double-counting allocated
resources.

When a job is admitted during the session, the plugin increments the leaf and
ancestor inqueue counters in `JobEnqueuedFn`. It does not rescan all queues or
jobs for each enqueue attempt.

With `D` requested resource dimensions and `H` Queue ancestors, the additional
enqueue-time work is:

```text
check:  O(H * D)
update: O(H * D)
```

The webhook-manager defaults `--max-queue-depth` to `5`, although deployments
can configure another limit. The design therefore treats depth as a bounded
configuration value rather than assuming a fixed tree size. Memory use is
`O(number of relevant queues * resource dimensions)`.

## Validation and Failure Handling

The Queue validating webhook must enforce the following when the feature gate
is enabled:

1. The annotation is a finite decimal number greater than or equal to `1`.
2. A Queue using the annotation has non-empty `spec.deserved` with at least one
   positive resource quantity.
3. Existing Queue resource validation continues to enforce
   `guarantee <= deserved <= capability` for dimensions where capability is
   configured.

The scheduler must defensively parse the annotation as well. If an invalid
annotation reaches the scheduler because the webhook was bypassed or was
misconfigured, the scheduler rejects admission for jobs in that Queue instead
of silently ignoring the invalid policy.

The scheduler configuration validator must enforce:

```text
1 <= max-queue-overcommit-factor
```

## RBAC and Security

This design introduces no field-level RBAC. The annotation inherits the
existing permission to update the cluster-scoped Queue resource. Deployments
must grant Queue write permission only to trusted cluster administrators.

The global admission check prevents Queue annotations from bypassing
cluster-wide overcommit policy. The effective-factor cap is an additional
administrator control over the Queue-local burst policy.

## Compatibility and Rollout

The feature gate is disabled by default, so existing Queue objects and
scheduler configurations continue to behave exactly as before.

Recommended rollout:

1. Upgrade the scheduler and webhook-manager with the feature gate disabled.
2. Enable `QueueScopedOvercommit` on both components.
3. Configure `max-queue-overcommit-factor` in the overcommit plugin.
4. Add annotations only to Queue objects with explicit `deserved` resources.
5. Observe PodGroup admission events and Queue inqueue metrics before enabling
   the annotation on additional queues.

To disable the feature, remove Queue annotations first, then disable the gate
on both components. Existing annotations have no scheduler effect while the
gate is disabled.

## Implementation Plan

1. Add `QueueScopedOvercommit` as an alpha Volcano feature gate, defaulting to
   `false`.
2. Define the annotation key and validation in the Queue webhook.
3. Add `max-queue-overcommit-factor` validation to the overcommit plugin
   configuration.
4. Extract the existing effective-capability calculation into shared scheduler
   code, refactor Capacity to use it without changing its behavior, and use it
   from overcommit for queue-scoped admission budgets.
5. Extend the overcommit plugin with queue and ancestor session-local counters.
6. Preserve the current global check, then run the queue and ancestor checks
   only when the feature gate and annotation are enabled.
7. Add events and verbose logs identifying queue-level rejections.
8. Add user documentation and configuration examples in the Volcano website
   repository after the API and behavior are accepted.

## Test Plan

Unit tests must cover:

- feature gate disabled: annotations do not change scheduling behavior
- annotation parsing, invalid factors, and missing/zero deserved resources
- global admission remains required even when a Queue factor is larger than
  the global factor
- queue-level admission and rejection for CPU, memory, and scalar resources
- factors capped by `max-queue-overcommit-factor`
- a Queue factor greater than the global factor still cannot exceed the global
  cluster admission budget
- derived real capability when `spec.capability` is omitted
- omitted deserved resource dimensions do not create a zero queue budget
- leaf and ancestor counter initialization and incremental updates
- rejection at an annotated parent when the leaf still has budget
- no queue-level check for an unannotated Queue or ancestor
- Queue webhook behavior for create, update, and feature-gate transitions.

Integration or E2E coverage must verify that jobs in two Queues with different
annotations have different admission limits while a job that would exceed the
global budget remains pending.

## Documentation Updates

Once implemented, the Volcano website documentation should include:

- feature-gate enablement for scheduler and webhook-manager
- the Queue annotation format and validation requirements
- the global and queue-level admission formulas
- hierarchy behavior and missing-resource-dimension semantics
- example Queue and scheduler configuration and
- the RBAC recommendation for Queue updates.
