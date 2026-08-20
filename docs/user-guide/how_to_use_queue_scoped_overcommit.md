# How to Use Queue-Scoped Overcommit

Queue-scoped overcommit lets a cluster administrator apply an additional
admission limit to selected Queues. It is useful when different Queues need
different limits for admitted-but-waiting workloads. For example, a production
Queue can use a conservative limit while a batch Queue can admit a larger
backlog.

The feature affects only whether a PodGroup may enter the `Inqueue` phase. It
does not change Queue capability, deserved resources, guarantees, allocation,
reclaim, or preemption behavior.

## Prerequisites

- Volcano with the `overcommit` plugin and the `enqueue` action enabled.
- Permission to update cluster-scoped Queue resources and Volcano component
  configuration.
- A Queue with a positive `spec.deserved` value for every resource dimension
  that should have a queue-scoped admission limit.

## 1. Enable the Feature Gate

`QueueScopedOvercommit` is Alpha and disabled by default. Enable it on both
the scheduler and webhook-manager. The scheduler performs admission checks and
the webhook validates Queue annotations.

### Using Helm

```bash
helm upgrade --install volcano volcano/volcano \
  --namespace volcano-system \
  --create-namespace \
  --set custom.scheduler_feature_gates="QueueScopedOvercommit=true" \
  --set custom.admission_feature_gates="QueueScopedOvercommit=true"
```

### Using manifests

Add the following flag to both the `volcano-scheduler` and
`volcano-admission` deployments:

```text
--feature-gates=QueueScopedOvercommit=true
```

When the gate is disabled, the scheduler keeps the existing global-only
behavior. The webhook rejects a Queue create or update that adds or changes the
queue-scoped annotation.

## 2. Configure the Overcommit Plugin

The existing `overcommit-factor` remains the global cluster admission limit.
`max-queue-overcommit-factor` is an administrator-controlled cap for Queue
annotations. If it is omitted, it defaults to `overcommit-factor`.

```yaml
actions: "enqueue, allocate, backfill"
tiers:
- plugins:
  - name: priority
  - name: gang
- plugins:
  - name: overcommit
    arguments:
      overcommit-factor: 1.2
      max-queue-overcommit-factor: 1.5
  - name: predicates
  - name: proportion
  - name: nodeorder
```

The global check always runs first. A Queue annotation cannot admit work that
exceeds the cluster-wide `overcommit-factor` budget. The Queue cap may be
greater than the global factor because it limits an individual Queue relative
to that Queue's policy, while the global factor limits total cluster admission.

## 3. Opt a Queue In

Add `volcano.sh/overcommit-factor` to the Queue. The value must be a finite
decimal number greater than or equal to `1`.

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

For each resource dimension explicitly set in `spec.deserved`, the Queue
budget is:

```text
min(real capability, max(deserved, guarantee) * effective Queue factor)
```

The effective Queue factor is:

```text
min(Queue annotation factor, max-queue-overcommit-factor)
```

In this example, `cpu` can admit up to `min(80, 40 * 1.5) = 60` CPUs, subject
to the global admission check. A resource omitted from `spec.deserved` does not
receive a queue-scoped limit; the global admission check still applies to it.

## 4. Hierarchical Queues

When Queue hierarchy is enabled for the `overcommit` plugin, Volcano checks the
job's leaf Queue and every annotated ancestor. Inqueue resource accounting is
propagated from the leaf Queue to its ancestors.

An ancestor is checked only when it explicitly has the
`volcano.sh/overcommit-factor` annotation. The root Queue is not implicitly
given a queue-scoped budget.

## 5. Verify Admission Decisions

When a PodGroup exceeds an annotated Queue's admission budget, it remains
`Pending` and Volcano records an `Unschedulable` event that identifies the
Queue and constrained resource dimensions.

```bash
kubectl get podgroup -n <namespace>
kubectl describe podgroup <podgroup-name> -n <namespace>
```

Look for an event similar to:

```text
queue overcommit admission budget insufficient for batch
```

## Limitations

- Queue-scoped overcommit uses static `Queue.spec.deserved`, optionally raised
  to a matching `Queue.spec.guarantee.resource`; it does not use Proportion's
  dynamic deserved calculation.
- Only resource dimensions explicitly configured in `spec.deserved` are
  covered by the Queue-level check.
- Queue annotations inherit normal permissions for updating cluster-scoped
  Queue resources. Grant those permissions only to trusted administrators.
