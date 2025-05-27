# Scheduling Reason

[@eggiter](https://github.com/eggiter); Aug 13, 2021
[@tgaddair](https://github.com/tgaddair); Dec 12, 2022
[@ouyangshengjia](https://github.com/ouyangshengjia); Mar 20, 2025

## Table of Contents

* [Table of Contents](#table-of-contents)
* [Problem](#problem)
* [Solution](#solution)

## Problem

- Currently, the reason and message in `pod.status.conditions["PodScheduled"]` and `podgroup.status.conditions["Unschedulable"]` is **NOT** informative, for example `x/x tasks in gang unschedulable: pod group is not ready, x Pending, x minAvailable`.
- Users want to know which one breaks the scheduling cycle and why is that.
- (@tgaddair): However, cluster autoscaling systems like [Karpenter](https://github.com/aws/karpenter) explicitly check for the `Unschedulable` reason on the pod to trigger scale-up (see [here](https://github.com/aws/karpenter-core/blob/fedb036e598fb51cda28bd5ae150743df009312f/pkg/utils/pod/scheduling.go#L25)). As such, we need to keep the `Reason` field consistent with default scheduler behavior.

## Solution

- Introducing an extra scheduling reasons in PodScheduled PodCondition:
  + `Schedulable`: means that the scheduler can schedule the pod right now, but not bind yet.

- Case:
  + 3 nodes: node1, node2, node3;
  + 6 tasks in the PodGroup(`minAvailable = 6`);
    + 3 tasks(Task1 ~ 3) can be scheduled and other 3 tasks(Task4 ~ 6) is unschedulable, thus the whole job is unscheduable.
  
- Current information:
  +  | Tasks     | Reason             | Message                                                                                                                                                                                         |
     |-----------|--------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
     | PodGroup  | NotEnoughResources | 6/6 tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable                                                                                                              |
     | Task1 ~ 3 | Unschedulable      | 6/6 tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable                                                                                                              |
     | Task4 ~ 6 | Unschedulable      | all nodes are unavailable: 1 plugin InterPodAffinity predicates failed node(s) didn't match pod affinity/anti-affinity, node(s) didn't match pod affinity rules, 2 node(s) resource fit failed. |

- Improved information:
  + | Tasks     | Reason             | Message                                                                                                                                                                                         |
    |-----------|--------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
    | PodGroup  | NotEnoughResources | **3/6** tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable; **Pending: 3 Unschedulable, 3 Schedulable**                                                             |
    | Task1     | **Schedulable**    | **Pod ns1/task-1 can possibly be assgined to node1, once minAvailable is satisfied**                                                                                                            |
    | Task2     | **Schedulable**    | **Pod ns1/task-2 can possibly be assgined to node2, once minAvailable is satisfied**                                                                                                            |
    | Task3     | **Schedulable**    | **Pod ns1/task-3 can possibly be assgined to node3, once minAvailable is satisfied**                                                                                                            |
    | Task4     | Unschedulable      | all nodes are unavailable: 1 plugin InterPodAffinity predicates failed node(s) didn't match pod affinity/anti-affinity, node(s) didn't match pod affinity rules, 2 node(s) resource fit failed. |
    | Task5 ~ 6 | Unschedulable      | **3/6** tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable; **Pending: 3 Unschedulable, 3 Schedulable**                                                             |

    - Note: Task5 ~ 6 are `Unschedulable` maybe because that this two locate after task4 by `TaskOrderFn`;

- In improved information, we can easily find the one that breaks the whole scheduling cycle and why dose that happen. Additionally,  we can find the histogram of reason why there are some tasks whose status is pending.
