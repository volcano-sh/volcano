# ResourceQuota Plugin

## Motivation

As [issue 1014](https://github.com/volcano-sh/volcano/issues/1014) mentioned, When Kubernetes ResourceQuota is insufficient, podgroup should not be able to enqueue.

## Design

In order to solve this problem, we provide `minQuotas` field which defines the minimal resource quota of tasks to run the pod group, and introduce resourcequota plugin to determine whether the podgroup be able to enqueue according to Kubernetes ResourceQuota.

1. add `RQStatus` map in namespace collection to cache all resourcequotas of namespace;

``` golang
RQStatus map[string]v1.ResourceQuotaStatus
```

2. calc minQuotas in job controller, and backfill to podGroup;

3. add resouceQuota plugin, and register `AddJobEnqueueableFn` function. This plugin will look at pending podgroups and will enqueue them only if there is enough capacity in the namespace according to Kubernetes ResourceQuota. And the plugin also consider podgroups that have already been permitted in the scheduling round to prevent it from enqueueing too many podgroups and exceeding the namespace resource quota.
