# How to Use GangPreempt and GangReclaim

## Introduction

`gangpreempt` and `gangreclaim` are gang-aware eviction actions for Volcano workloads. They try to free enough resources for a waiting gang while preserving a victim gang's `minAvailable` whenever possible.

- `gangpreempt` is for a higher-priority gang that is blocked by lower-priority, preemptable workloads in the **same Queue**.
- `gangreclaim` is for a gang in one Queue that needs resources borrowed by another Queue. It uses Queue deserved resources and can evict from a different Queue when reclamation is allowed.

Both actions first prefer a victim gang's surplus tasks: tasks above its gang, per-role, and sub-group minimums. By default, they can evict a whole victim gang only when surplus tasks are not enough. A later scheduling cycle performs the final predicate and resource checks before binding Pods.

Use the action names exactly as shown: `gangpreempt` and `gangreclaim` are lowercase.

## Difference from Legacy Preempt and Reclaim

Legacy `preempt` and `reclaim` consider victims as individual tasks on candidate nodes. They do not select victim Jobs as bundles or preserve a victim gang’s minAvailable by themselves. Depending on task ordering and node placement, they can evict one Pod from each of two victim gangs with no surplus tasks, causing both gangs to fall below `minAvailable`.

`gangpreempt` and `gangreclaim` evaluate victim Jobs first. They select a Job's surplus tasks as a bundle when that preserves the gang's minimums. If no safe surplus is available and whole-bundle eviction is allowed, they select a whole victim gang rather than taking individual Pods from several gangs. This keeps as many victim Jobs intact as possible.

For example, consider a four-CPU cluster with two low-priority Jobs. Each Job has two running Pods and `minAvailable: 2`. A new gang needs two CPUs.

| Action type | Possible victim selection | Result |
| --- | --- | --- |
| Legacy `preempt` | One Pod from each victim Job | Both victim gangs fall below their minimum. |
| `gangpreempt` | Both Pods from one victim Job | One victim gang remains completely intact. |

The same job-integrity rule applies to `gangreclaim` when one Queue reclaims resources from another Queue.

## When to Use Each Action

| Action | Use it when | Main requirements | It does not do |
| --- | --- | --- | --- |
| `gangpreempt` | A higher-priority gang must run before a lower-priority gang in the same Queue. | The requester has higher priority and the victim Pods are preemptable. | Preempt workloads from another Queue. |
| `gangreclaim` | A Queue needs back resources that another Queue has borrowed. | The victim Queue is reclaimable and is using more than its deserved resources. The requester must be eligible to reclaim. | Reclaim from a Queue that is within its deserved resources or is not reclaimable. |

## Supported Versions

`gangpreempt` and `gangreclaim` are supported in Volcano `v1.15.0` and later. Do not use action names such as `gangPreempt` or `gangReclaim`. Those names are not registered.

These gang-aware actions are still at an early stage. They currently coexist with legacy `preempt` and `reclaim` actions. The project may unify the legacy and gang-aware implementations in the future, so review release notes before upgrading.

## Prerequisites

Before enabling these actions:

1. Install Volcano and use `volcano` as the workload scheduler name.
2. Define a `PriorityClass` for the priority-preemption scenario.
3. Mark workloads that may be evicted with `volcano.sh/preemptable: "true"`.
4. For reclamation, configure Queue `deserved` resources and set potential victim Queues to `reclaimable: true`. See the [Capacity Plugin User Guide](./how_to_use_capacity_plugin.md) for more Queue-capacity details.
5. Use a test cluster with four schedulable CPUs, or adjust the resource requests and replica counts so the victim Jobs fill the available CPU before you submit the requester.

## Recommended Scheduler Configuration

The following configuration is a recommended baseline for using both actions together. It uses the flat capacity model (`enableHierarchy: false`).

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "allocate, backfill, gangreclaim, gangpreempt"
    tiers:
    - plugins:
      - name: priority
      - name: gang
        enablePreemptable: false
      - name: conformance
      - name: sla
      - name: capacity
        enableHierarchy: false
    - plugins:
      - name: predicates
```

Apply the configuration according to your installation method, then restart or roll out the scheduler if your deployment does not automatically reload its ConfigMap:

```shell
kubectl apply -f scheduler-config.yaml
kubectl -n volcano-system rollout restart deployment/volcano-scheduler
kubectl -n volcano-system rollout status deployment/volcano-scheduler
```

### Why These Components Are Required

- `gangpreempt` and `gangreclaim` are the actions being enabled.
- `priority` supplies the priority ordering required by `gangpreempt`.
- `gang` supplies gang readiness, minimums, and gang-aware victim protection.
- `conformance` prevents eviction of protected workloads.
- `capacity` supplies Queue deserved-resource accounting and the eligibility decisions needed by `gangreclaim`.
- `sla` supplies gang pipeline behavior used by the actions.

You may add plugins for your own scheduling needs, such as Queue ordering, node scoring, bin packing, or topology-aware scheduling. Retain the components above and validate the resulting full configuration in a representative cluster. Do not enable `proportion` together with `capacity`. They are alternative Queue-capacity models.

### Whole-Gang Fallback

Both actions default to `allowWholeBundle: true`. This allows a whole victim gang to be selected when safe surplus tasks cannot satisfy the waiting gang. If preserving every victim gang is more important than starting the requester, disable this fallback:

```yaml
configurations:
- name: gangpreempt
  arguments:
    allowWholeBundle: false
- name: gangreclaim
  arguments:
    allowWholeBundle: false
```

## Example 1: Same-Queue Priority Preemption

This example runs two low-priority victim gangs in one Queue, then submits a two-Pod higher-priority gang to the same full cluster. Each victim gang has two Pods and `minAvailable: 2`, so neither has spare Pods. The higher-priority gang should run by evicting one whole victim gang and leaving the other intact.

Save the Queue and priority classes as `priority-demo-prerequisites.yaml`:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: priority-demo
spec:
  reclaimable: true
  deserved:
    cpu: "4"
---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: gang-low
value: 10
globalDefault: false
description: Low-priority preemptable gang.
---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: gang-high
value: 100
globalDefault: false
description: Higher-priority requesting gang.
```

Save the two preemptable victim gangs as `preemption-victims.yaml`:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: preemption-victim-a
  namespace: default
  annotations:
    volcano.sh/preemptable: "true"
spec:
  schedulerName: volcano
  queue: priority-demo
  priorityClassName: gang-low
  minAvailable: 2
  tasks:
  - name: worker
    replicas: 2
    template:
      spec:
        restartPolicy: Never
        containers:
        - name: worker
          image: registry.k8s.io/pause:3.10
          resources:
            requests:
              cpu: "1"
---
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: preemption-victim-b
  namespace: default
  annotations:
    volcano.sh/preemptable: "true"
spec:
  schedulerName: volcano
  queue: priority-demo
  priorityClassName: gang-low
  minAvailable: 2
  tasks:
  - name: worker
    replicas: 2
    template:
      spec:
        restartPolicy: Never
        containers:
        - name: worker
          image: registry.k8s.io/pause:3.10
          resources:
            requests:
              cpu: "1"
```

```shell
kubectl apply -f priority-demo-prerequisites.yaml
kubectl apply -f preemption-victims.yaml
kubectl wait --for=condition=Ready pod -l volcano.sh/job-name=preemption-victim-a --timeout=180s
kubectl wait --for=condition=Ready pod -l volcano.sh/job-name=preemption-victim-b --timeout=180s
```

Save the requester as `preemption-requester.yaml`, then apply it:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: preemption-requester
  namespace: default
spec:
  schedulerName: volcano
  queue: priority-demo
  priorityClassName: gang-high
  minAvailable: 2
  tasks:
  - name: worker
    replicas: 2
    template:
      spec:
        restartPolicy: Never
        containers:
        - name: worker
          image: registry.k8s.io/pause:3.10
          resources:
            requests:
              cpu: "1"
```

```shell
kubectl apply -f preemption-requester.yaml
```

Expected behavior with the default `allowWholeBundle: true`:

```shell
kubectl get pods -w
```

- The two `preemption-requester` Pods become `Running`.
- `gangpreempt` evicts both Pods from either `preemption-victim-a` or `preemption-victim-b`.
- The other victim gang retains both Pods and remains ready.

For comparison, legacy actions can choose one Pod from each victim gang because they select individual tasks. The exact legacy victim choice depends on task ordering and node placement, so do not use it as a deterministic validation result.

Set `allowWholeBundle: false` when it is unacceptable to break either victim gang. In that configuration, `preemption-requester` remains pending instead.

Clean up the preemption example before running the reclamation example:

```shell
kubectl delete -f preemption-requester.yaml --ignore-not-found
kubectl delete -f preemption-victims.yaml --ignore-not-found
kubectl delete -f priority-demo-prerequisites.yaml --ignore-not-found
```

## Example 2: Cross-Queue Resource Reclamation

This example gives two Queues two CPUs of deserved resource each. Two victim gangs in `borrower` first occupy all four CPUs. Each has two Pods and `minAvailable: 2`, so neither has spare Pods. When `owner` submits a two-Pod gang, `gangreclaim` returns two CPUs to `owner` by evicting one whole borrower gang and leaving the other intact.

Save the Queues as `reclaim-queues.yaml`:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: owner
spec:
  reclaimable: true
  deserved:
    cpu: "2"
---
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: borrower
spec:
  reclaimable: true
  deserved:
    cpu: "2"
```

Save the two preemptable borrower gangs as `borrower-victims.yaml`:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: borrower-victim-a
  namespace: default
  annotations:
    volcano.sh/preemptable: "true"
spec:
  schedulerName: volcano
  queue: borrower
  minAvailable: 2
  tasks:
  - name: worker
    replicas: 2
    template:
      spec:
        restartPolicy: Never
        containers:
        - name: worker
          image: registry.k8s.io/pause:3.10
          resources:
            requests:
              cpu: "1"
---
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: borrower-victim-b
  namespace: default
  annotations:
    volcano.sh/preemptable: "true"
spec:
  schedulerName: volcano
  queue: borrower
  minAvailable: 2
  tasks:
  - name: worker
    replicas: 2
    template:
      spec:
        restartPolicy: Never
        containers:
        - name: worker
          image: registry.k8s.io/pause:3.10
          resources:
            requests:
              cpu: "1"
```

```shell
kubectl apply -f reclaim-queues.yaml
kubectl apply -f borrower-victims.yaml
kubectl wait --for=condition=Ready pod -l volcano.sh/job-name=borrower-victim-a --timeout=180s
kubectl wait --for=condition=Ready pod -l volcano.sh/job-name=borrower-victim-b --timeout=180s
```

Save the owner requester as `owner-requester.yaml`, then apply it:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: owner-requester
  namespace: default
spec:
  schedulerName: volcano
  queue: owner
  minAvailable: 2
  tasks:
  - name: worker
    replicas: 2
    template:
      spec:
        restartPolicy: Never
        containers:
        - name: worker
          image: registry.k8s.io/pause:3.10
          resources:
            requests:
              cpu: "1"
```

```shell
kubectl apply -f owner-requester.yaml
```

Expected behavior with the default `allowWholeBundle: true`:

- The two `owner-requester` Pods become `Running`.
- `gangreclaim` evicts both Pods from either `borrower-victim-a` or `borrower-victim-b`.
- The other borrower gang retains both Pods and remains ready.
- `owner` receives its two CPUs of deserved resources.

For comparison, legacy `reclaim` can choose one Pod from each borrower gang because it selects individual tasks. The exact legacy victim choice depends on task ordering and node placement, so do not use it as a deterministic validation result.

Set `allowWholeBundle: false` when it is unacceptable to break either borrower gang. In that configuration, `owner-requester` remains pending. The requester also remains pending when the borrower Queue is not reclaimable, is not over its deserved resources, the requester Queue is already overused, or no eligible whole or surplus bundle can satisfy the request.

Clean up the reclamation example when you are finished:

```shell
kubectl delete -f owner-requester.yaml --ignore-not-found
kubectl delete -f borrower-victims.yaml --ignore-not-found
kubectl delete -f reclaim-queues.yaml --ignore-not-found
```

## Observe and Troubleshoot

Use these commands while testing:

```shell
kubectl get queue
kubectl get job -A
kubectl get pod -A -o wide
kubectl -n volcano-system logs deployment/volcano-scheduler --tail=200
```

Common configuration problems:

| Symptom | Check |
| --- | --- |
| Scheduler fails to start with `failed to find Action gangPreempt` | Use lowercase `gangpreempt` and `gangreclaim`. |
| Same-Queue priority requester stays pending | Confirm priority values, same Queue placement, and the victim's `volcano.sh/preemptable: "true"` marker. |
| Cross-Queue requester stays pending | Confirm `capacity` is enabled, Queue `deserved` is set, victim Queue is reclaimable, and the victim is borrowing resources. |
| A victim gang is broken unexpectedly | `allowWholeBundle` is enabled by default. Set it to `false` if whole-gang eviction is unacceptable. |
| Requester still cannot start after eviction | Check its gang minimum, node predicates, resource requests, and topology constraints. |

## Relationship with Legacy Actions

Volcano currently provides both legacy `preempt`/`reclaim` actions and gang-aware `gangpreempt`/`gangreclaim` actions. The recommended configuration above uses only the gang-aware actions. It does not mix legacy and gang-aware eviction actions in one pipeline.

The project may unify the legacy and gang-aware implementations in a future release. Treat the current action names and configuration details as version-specific behavior, and review the release notes before upgrading.
