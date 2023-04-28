## Introduction

Before scheduling and scoring, filter out the nodes that do not meet the requirements. The filter items mainly include: whether the node is available, port, stain, node affinity, container affinity, volume, container topology, idle resources, etc.

## Design
- PrePredicate  
    It mainly corresponds to the Prefilter stage of kube-scheduler. The current filter content is:
    - nodePort
    - podAffinity
    - podTopologySpread

- Predicate  
    It mainly corresponds to the Filter stage of kube-scheduler. The current filtering content is:
    - nodeUnscheduler
    - nodeAffinity
    - taintToleration
    - nodePort
    - podAffinity
    - nodeVolumeLimitsCSI
    - volumeZone
    - podTopologySpread

- PredicateResource  
    The current function is an independent function disassembled from the predicate, which is responsible for filtering the content related to the idle resources of the node, such as: the number of pods, CPU, Memory, user-defined resources, etc.

    Because both allocation and preemption (preempt and reclaim) require predicate operations, if resource filtering is also processed in the predicate function, the allocation and preemption actions are incompatible, see [#2739](https://github.com /volcano-sh/volcano/issues/2739). After the disassembly process, the allocation action (allocate) judges the Predicate and PredicateResource, and the preemption action (preempt, reclaim) judges the Predicate.

    Filter content mainly includes:
    - podNumber
    - CPU
    - Memory
    - ScalerResources

## Recommend practice
Configure the predicate plugin by:
```
  actions: "enqueue, allocate, preempt, backfill"
  tiers:
  - plugins:
    - name: priority
    - name: gang
    - name: predicate
      enablePredicate: false
```
`enablePredicate` is the switch of the predicate plug-in, the default is true, you can turn off the filtering function of the predicate plug-in according to the above method. `PrePredicate`, `Predicate` and `PredicateResource` are simultaneously controlled by the `enablePredicate` switch.