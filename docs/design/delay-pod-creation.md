# Delay Pod Creation

@k82cn; Jan 7, 2019

## Table of Contents

   * [Delay Pod Creation](#delay-pod-creation)
      * [Table of Contents](#table-of-contents)
      * [Motivation](#motivation)
      * [Function Detail](#function-detail)
         * [State](#state)
         * [Action](#action)
         * [Admission Webhook](#admission-webhook)
      * [Feature interaction](#feature-interaction)
         * [Queue](#queue)
         * [Quota](#quota)
         * [Operator/Controller](#operatorcontroller)
      * [Others](#others)
         * [Compatibility](#compatibility)
      * [Roadmap](#roadmap)
      * [Reference](#reference)

Created by [gh-md-toc](https://github.com/ekalinin/github-markdown-toc)

## Motivation

For a batch system, there're always several pending jobs because of limited resources and throughput.
Different with other kubernetes type, e.g. Deployment, DaemonSet, it's better to delay pods creation for
batch workload to reduce apiserver pressure and speed up scheduling (e.g. less pending pods to consider).
In this document, several enhancements are introduced to delay pod creation.

## Function Detail

### State

A new state, named `InQueue`, will be introduced to denote the phase that jobs are ready to be allocated.
After `InQueue`, the state transform map is updated as follow.

| From          | To             | Reason  |
|---------------|----------------|---------|
| Pending       | InQueue        | When it's ready to allocate resource to job |
| InQueue       | Pending        | When there's not enough resources anymore |
| InQueue       | Running        | When every pods of `spec.minMember` are running |

The `InQueue` is a new state between `Pending` and `Running`; and it'll let operators/controllers start to
create pods. If it meets errors, e.g. unschedulable, it rollbacks to `Pending` instead of `InQueue` to
avoid retry-loop.

### Action

Currently, `kube-batch` supports several actions, e.g. `allocate`, `preempt`; but all those actions are executed
based on pending pods. To support `InQueue` state, a new action, named `enqueue`, will be introduced.

By default, `enqueue` action will handle `PodGroup`s in FCFS policy; `enqueue` will go through all PodGroup
(by creation timestamp) and update PodGroup's phase to `InQueue` if:

* there're enough idle resources for `spec.minResources` of `PodGroup`
* there're enough quota for `spec.minResources` of `PodGroup`

As `kube-batch` handling `PodGroup` by `spec.minResources`, the operator/controller may create more `Pod`s than
`spec.minResources`; in such case, `preempt` action will be enhanced to evict overused `PodGroup` to release
resources.

### Admission Webhook

To guarantee the transaction of `spec.minResources`, a new `MutatingAdmissionWebhook`, named `PodGroupMinResources`,
is introduced. `PodGroupMinResources` make sure

* the summary of all PodGroups' `spec.minResources` in a namespace not more than `Quota`
* if resources are reserved by `spec.minResources`, the resources can not be used by others

Generally, it's better to let total `Quota` to be more than available resources in cluster, as some pods maybe
unschedulable because of scheduler's algorithm, e.g. predicates.

## Feature interaction

### Queue

The resources will be shared between `Queue`s algorithm, e.g. proportion by default. If the resources can not be
fully used because of fragment, `backfill` action will help on that. If `Queue` used more resources than its
deserved, `reclaim` action will help to balance resources. The Pod can not be evicted currently if eviction will
break `spec.minMember`; it'll be enhanced for job level eviction.

### Quota

To delay pod creation, both `kube-batch` and `PodGroupMinResources` will watch `ResourceQuota` to decide which
`PodGroup` should be in queue firstly. The decision maybe invalid because of race condition, e.g. other
controllers create Pods. In such case, `PodGroupMinResources` will reject `PodGroup` creation and keep `InQueue`
state until `kube-batch` transform it back to `Pending`. To avoid race condition, it's better to let `kube-batch`
manage `Pod` number and resources (e.g. CPU, memory) instead of `Quota`.

### Operator/Controller

The Operator/Controller should follow the above "protocol" to work together with scheduler. A new component,
named `PodGroupController`, will be introduced later to enforce this protocol if necessary.

## Others

### Compatibility

To support this new feature, a new state and a new action are introduced; so when the new `enqueue` action is
disabled in the configuration, it'll keep the same behaviour as before.

## Roadmap

* `InQueue` phase and `enqueue` action (v0.5+)
* Admission Controller (v0.6+)

## Reference

* [Coscheduling](https://github.com/kubernetes/enhancements/pull/639)
* [Delay Pod creation](https://github.com/kubernetes-sigs/kube-batch/issues/539)
* [PodGroup Status](https://github.com/kubernetes-sigs/kube-batch/blob/master/doc/design/podgroup-status.md)
* [Support 'spec.TotalResources' in PodGroup](https://github.com/kubernetes-sigs/kube-batch/issues/401)
* [Dynamic Admission Control](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#write-an-admission-webhook-server)
* [Add support for podGroup number limits for one queue](https://github.com/kubernetes-sigs/kube-batch/issues/452)
