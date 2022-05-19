# Rescheduling

@[Thor-wl](https://github.com/Thor-wl); Dec 25th, 2021

## Motivation
As what [Issue1777](https://github.com/volcano-sh/volcano/issues/1777) describes, **Rescheduling** is important for the 
following reasons:
* Unbalanced resource utilization due to unreasonable scheduling strategies and dynamic changes in jobs' lifecycle.
* Node status changes such as add/remove nodes, pod/node taint/affinity changes.

In order to rebalance the cluster resource utilization among nodes, we want to achieve these goals:
* Rescheduling pods based on real resource utilization instead of request resource.
* Support custom configured rescheduling strategies.

## Design
### WorkFlow
1. Filter Resources which are selected to be evicted potentially according to the filter chain, for example, queue filter
and label filter.
2. Get through the chain of rescheduling strategies and filter resources which are to be evicted.
3. Evict the Pods attached to these resources.
4. Execute the process above periodically.

### Resource Filter
* Queue Filter

  `Queue filtering` will filter resources in specified queue. Then the rescheduling process only works on the result set.

* Label Filter

  `Label Filter` will filter pods with specified labels. Then the rescheduling process only works on the result set.

### Rescheduling Strategy
* OfflineOnly

    `OfflineOnly`, abbreviated as `OLO`, will only pick out offline workloads, which are attached with annotation
`preemptable: true`, and then reschedule them only.

* LowPriorityFirst

    `LowPriorityFirst`, abbreviated as `LPF`, will sort workloads by priority and then reschedule pods in ascending 
order.

* ShortLifeTimeFirst

  `ShortLifeTimeFirst`, abbreviated as `SLTF`, will sort workloads by running time. Pods with the shortest lift time 
will be rescheduled first. This strategy can make sure workloads with long task type goes healthily without interruptions.

* BigObjectFirst

    `BigObjectFirst`, abbreviated as `BOF`, will select workloads which request the most **dominate resource** and reschedule
them first. It helps improve system throughout and avoid small workloads' starvation.

* MoreReplicasFirst
  
    `MoreReplicasFirst`, abbreviated as `MRF`, will sort workloads by replicas number. Workloads with the most replicas
will be rescheduled first. This strategy is friendly to `gang scheduling` for it will consider the `minAvailable` in 
volcano jobs.

* Others
    Implement the [Policy and Strategies](https://github.com/kubernetes-sigs/descheduler#policy-and-strategies) listed 
for [Descheduler](https://github.com/kubernetes-sigs/descheduler)

### Metrics
All the decisions made by rescheduling strategies will consider the metrics from `Prometheus`. Namely, Volcano will 
list the real node resource utilization and pod distribution instead of request resource. Basically, usage of`CPU` and 
`Memroy` will be collected. Other resource such as `GPU` can be extended.

## Implementation(Beta)
```yaml
## Configuration Option 
actions: "enqueue, allocate, backfill, shuffle"  ## add 'shuffle' at the end of the actions
tiers:
  - plugins:
      - name: priority
      - name: gang
      - name: conformance
      - name: rescheduling       ## rescheduling plugin
        arguments:
          interval: 5m           ## optional, the strategies will be called in this duration periodcally. 5 minuters by default. 
          strategies:            ## required, strategies working in order
            - name: offlineOnly
            - name: lowPriorityFirst
            - name: lowNodeUtilization
              params:
                thresholds:
                  "cpu" : 20
                  "memory": 20
                  "pods": 20
                targetThresholds:
                  "cpu" : 50
                  "memory": 50
                  "pods": 50
          queueSelector:         ## optional, select workloads in specified queues as potential evictees. All queues by default.
            - default
            - test-queue
          labelSelector:         ## optional, select workloads with specified labels as potential evictees. All labels by default.
            business: offline
            team: test
  - plugins:
      - name: overcommit
      - name: drf
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
```

Implementation Profile:
* Load and parse user configurations about rescheduling.
* Update the cache by metrics collected by `Prometheus`.
* Create a new action named "shuffle", implement the workflow above.
* Create a new plugin named "rescheduling", implement the strategies above.

As the plan, we will implement the following functions in v1.6:
* Support configuration resolution.
* Implement 'shuffle' to support rescheduling process.
* Implement 'rescheduling' plugin generally.
* Implement [LowNodeUtilization](https://github.com/kubernetes-sigs/descheduler#lownodeutilization) strategy.
* Implement the rescheduling process based on metrics provided by `Prometheus`.

Functions to be implemented later:
* Other strategies listed above.
* Resource Filter

## TODO
* Make sure pod rescheduled will not be scheduled to original node or other unfit nodes.

## Reference
* [kubernetes-sigs/descheduler](https://github.com/kubernetes-sigs/descheduler)
* [prometheus/prometheus](https://github.com/prometheus/prometheus)