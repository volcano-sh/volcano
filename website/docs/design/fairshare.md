# Namespace fair share

[@lminzhw](http://github.com/lminzhw); May 8, 2019

## Motivation

`Queue` was introduced in [kube-batch](http://github.com/kubernetes-sigs/kube-batch) to share resources among users.

But, the user in the same `Queue` are equivalent during scheduling. For example, we have a `Queue` contains a small amount of resources, and there are 10 pods belong to UserA and 1000 pods belong to UserB. In this case, pods of UserA would have less probability to bind with node.

So, we need a more fine-grained strategy to balance resource usage among users in the same `Queue`.

In consideration of multi-user model in kubernetes, we use namespace to distinguish different user. Each namespace would have its weight to control resources usage.

## Function Specification

Weight have these features:
> 1. `Queue` level
> 2. an `integer` with default value 1
> 3. record in namespace `quota`
> 4. higher value means more resources after balancing

### where is the weight

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