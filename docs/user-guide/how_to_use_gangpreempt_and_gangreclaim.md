# How to Use GangPreempt and GangReclaim

## Introduction

`gangpreempt` and `gangreclaim` are gang-aware eviction actions for Volcano workloads. They try to free enough resources for a waiting gang while preserving a victim gang's `minAvailable` whenever possible.

- `gangpreempt` is for a higher-priority gang that is blocked by lower-priority, preemptable workloads in the **same Queue**.
- `gangreclaim` is for a gang in one Queue that needs resources borrowed by another Queue. It uses Queue deserved resources and can evict from a different Queue when reclamation is allowed.

Both actions first prefer a victim gang's surplus tasks: tasks above its gang, per-role, and sub-group minimums. By default, they can evict a whole victim gang only when surplus tasks are not enough. A later scheduling cycle performs the final predicate and resource checks before binding Pods.

Use the action names exactly as shown: `gangpreempt` and `gangreclaim` are lowercase.

## When to Use Each Action

| Action | Use it when | Main requirements | It does not do |
| --- | --- | --- | --- |
| `gangpreempt` | A higher-priority gang must run before a lower-priority gang in the same Queue. | The requester has higher priority and the victim Pods are preemptable. | Preempt workloads from another Queue. |
| `gangreclaim` | A Queue needs back resources that another Queue has borrowed. | The victim Queue is reclaimable and is using more than its deserved resources. The requester must be eligible to reclaim. | Reclaim from a Queue that is within its deserved resources or is not reclaimable. |

## Supported Versions and Validation

The actions are available in Volcano `v1.15.0` and later releases that include them. Do not use the action name casing from older examples or issue reports such as `gangPreempt` or `gangReclaim`. Those names are not registered actions.

The configuration in this guide was validated with the repository E2E suite on 2026-09-01:

| Item | Value |
| --- | --- |
| Kubernetes test cluster | Kind using `kindest/node:v1.36.1` |
| Volcano build | `v1.16.0-alpha.1-62-g9f6821b8d` |
| Test command | `make e2e-test-gangevict FORCE_REBUILD=false` |
| Result | `28 Passed`, `0 Failed` |
| Test source | `test/e2e/gangevict/gangevict.go` |

The suite covers same-Queue priority preemption, cross-Queue reclamation, safe surplus eviction, whole-gang fallback, equal/higher-priority protection, Queue reclaimability, Queue deserved-resource boundaries, role and sub-group minimums, and topology constraints. Re-run the suite against the exact Volcano image and Kubernetes version that you plan to deploy.

## Prerequisites

Before enabling these actions:

1. Install Volcano and use `volcano` as the workload scheduler name.
2. Define a `PriorityClass` for the priority-preemption scenario.
3. Mark workloads that may be evicted with `volcano.sh/preemptable: "true"`.
4. For reclamation, configure Queue `deserved` resources and set potential victim Queues to `reclaimable: true`. See the [Capacity Plugin User Guide](./how_to_use_capacity_plugin.md) for more Queue-capacity details.
5. Size the cluster so that the examples can fill the relevant resources. The examples below assume at least 4 allocatable CPUs.

## Tested Scheduler Configuration

The following is the complete configuration used by the gang eviction E2E suite. It is a validated baseline for using both actions together. It uses the flat capacity model (`enableHierarchy: false`).

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
      - name: overcommit
      - name: drf
        enablePreemptable: false
      - name: predicates
        arguments:
          predicate.DynamicResourceAllocationEnable: true
      - name: nodeorder
      - name: binpack
      - name: network-topology-aware
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
- `sla` is part of the tested pipeline and supplies gang pipeline behavior used by the actions.

The remaining plugins in the configuration are also part of the tested baseline. You may add plugins for your own scheduling needs, but retain the required components above and validate the resulting full configuration in a representative cluster. Do not enable `proportion` together with `capacity`. They are alternative Queue-capacity models.

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

The actions also accept `maxDomains`, which limits how many candidate topology domains are inspected for one waiting gang. The default is `8`. Leave it unchanged unless you understand the topology trade-off.

## Example 1: Same-Queue Priority Preemption

This example runs a four-Pod low-priority gang in one Queue, then submits a two-Pod higher-priority gang to the same full cluster. The low-priority gang has `minAvailable: 2`, so two of its four Pods are safe surplus victims. The higher-priority gang should run and the victim gang should retain two running Pods.

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

Save the victim gang as `low-priority-victim.yaml` and apply the prerequisites before it:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: low-priority-victim
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
    replicas: 4
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
kubectl apply -f low-priority-victim.yaml
kubectl wait --for=condition=Ready pod -l volcano.sh/job-name=low-priority-victim --timeout=180s
```

Save the requester as `high-priority-requester.yaml`, then apply it:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: high-priority-requester
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
kubectl apply -f high-priority-requester.yaml
```

Expected behavior:

```shell
kubectl get pods -w
```

- The two requester Pods become `Running`.
- Two surplus Pods from `low-priority-victim` are evicted or become pending according to the Job retry policy.
- At least two victim Pods remain running, satisfying `minAvailable: 2`.

The requester remains pending if it has equal or lower priority, if the victim Pods are not preemptable, if the gangs are in different Queues, or if the cluster cannot satisfy the requester even after eligible eviction.

## Example 2: Cross-Queue Resource Reclamation

This example gives two Queues two CPUs of deserved resource each. A victim gang in `borrower` first occupies all four CPUs. When the `owner` Queue submits a two-Pod gang, `gangreclaim` returns two CPUs to `owner`. The borrower gang keeps its two-Pod minimum.

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

Save this preemptable borrower gang as `borrower-gang.yaml` and apply it after the Queues:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: borrower-gang
  namespace: default
  annotations:
    volcano.sh/preemptable: "true"
spec:
  schedulerName: volcano
  queue: borrower
  minAvailable: 2
  tasks:
  - name: worker
    replicas: 4
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
kubectl apply -f borrower-gang.yaml
kubectl wait --for=condition=Ready pod -l volcano.sh/job-name=borrower-gang --timeout=180s
```

After its four Pods are running, save the owner gang as `owner-gang.yaml` and apply it:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: owner-gang
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
kubectl apply -f owner-gang.yaml
```

Expected behavior:

- The two `owner-gang` Pods become `Running`.
- `borrower-gang` loses only its two surplus Pods and retains its two-Pod gang minimum.
- The result gives each Queue its two CPUs of deserved resources.

The requester should remain pending when the victim Queue is not reclaimable, is not over its deserved resources, the requester Queue is already overused, or eligible resources are insufficient to satisfy the requester's gang minimum.

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

Volcano currently provides both legacy `preempt`/`reclaim` actions and gang-aware `gangpreempt`/`gangreclaim` actions. The tested configuration above uses only the gang-aware actions. It does not mix legacy and gang-aware eviction actions in one pipeline.

The project may unify the legacy and gang-aware implementations in a future release. Treat the current action names and configuration details as version-specific behavior, and review the release notes before upgrading.

## Reproduce the Validation

To run the same E2E coverage from a Volcano source checkout, use a disposable Kind environment with Docker available:

```shell
unset CLUSTER_NAME
export CLEANUP_CLUSTER=0
export ARTIFACTS_PATH="$PWD/e2e-gangevict-logs"
make e2e-test-gangevict FORCE_REBUILD=false
```

The runner uses the `integration` cluster name by default. Keep that default for the current suite because its scheduler ConfigMap test helper expects `integration-scheduler-configmap`.
