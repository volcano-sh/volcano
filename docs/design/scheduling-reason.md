# Scheduling Reason

[@eggiter](https://github.com/eggiter); Aug 13, 2021
[@tgaddair](https://github.com/tgaddair); Dec 12, 2022

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

An additional reason was originally considered that conflicts with cluster autoscaler. Instead, this reason only appears in the detailed `Message` field, and shows up with `Reason: Unschedulable` in the pod:
  + `Undetermined`: means that the scheduler skips scheduling the pod which left the pod `Undetermined`, for example due to unschedulable pod already occurred.

- Case:
  + 3 nodes: node1, node2, node3;
  + 6 tasks in the PodGroup(`minAvailable = 6`);
    + only 1 task(Task6) is unschedulable and other 5 tasks(Task1 ~ 5) can be scheduled, thus the whole job is unscheduable.
  
- Current information:
  +  |Tasks|Reason|Message|
     |-|-|-|
     |PodGroup |NotEnoughResources|6/6 tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable |
     |Task1 ~ 5|Unschedulable|6/6 tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable|
     |Task6|Unschedulable| all nodes are unavailable: 1 plugin InterPodAffinity predicates failed node(s) didn't match pod affinity/anti-affinity, node(s) didn't match pod affinity rules, 2 node(s) resource fit failed.|

- Improved information:
  + |Tasks|Reason|Message|
    |-|-|-|
    |PodGroup |(same)| **3/6** tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable; **Pending: 1 Unschedulable, 2 Undetermined, 3 Schedulable** |
    |Task1 ~ 2|**Unschedulable**| **3/6** tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable; **Pending: 1 Unschedulable, 2 Undetermined, 3 Schedulable**  |
    |Task3|**Schedulable**| **Pod ns1/task-1 can possibly be assgined to node1** |
    |Task4|**Schedulable**| **Pod ns1/task-2 can possibly be assgined to node2** |
    |Task5|**Schedulable**| **Pod ns1/task-3 can possibly be assgined to node3** |
    |Task6|Unschedulable| all nodes are unavailable: 1 plugin InterPodAffinity predicates failed node(s) didn't match pod affinity/anti-affinity, node(s) didn't match pod affinity rules, 2 node(s) resource fit failed.|

    - Note: Task1 & 2 are `Unschedulable` maybe because that this two locate after task6 by `TaskOrderFn`;

- In improved information, we can easily find the one that breaks the whole scheduling cycle and why dose that happen. Additionally,  we can find the histogram of reason why there are some tasks whose status is pending.
