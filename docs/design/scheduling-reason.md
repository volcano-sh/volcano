# Scheduling Reason

[@eggiter](https://github.com/eggiter); Aug 13, 2021
[@tgaddair](https://github.com/tgaddair); Dec 12, 2022
[@ouyangshengjia](https://github.com/ouyangshengjia); Mar 20, 2025

## Table of Contents

* [Table of Contents](#table-of-contents)
* [Problem](#problem)
* [Solution](#solution)
* [Stable messages](#stable-messages)

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
    + 3 tasks(Task1 ~ 3) can be scheduled and other 3 tasks(Task4 ~ 6) is unschedulable, thus the whole job is unschedulable.
  
- Current information:
  +  | Tasks     | Reason             | Message                                                                                                                                                                                         |
     |-----------|--------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
     | PodGroup  | NotEnoughResources | 6/6 tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable                                                                                                              |
     | Task1 ~ 3 | Unschedulable      | 6/6 tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable                                                                                                              |
     | Task4 ~ 6 | Unschedulable      | all nodes are unavailable: 1 plugin InterPodAffinity predicates failed node(s) didn't match pod affinity/anti-affinity, node(s) didn't match pod affinity rules, 2 node(s) resource fit failed. |

- Improved information (**Deprecated**: [Stable messages](#stable-messages) replaces the messages in this table):
  + | Tasks     | Reason             | Message                                                                                                                                                                                         |
    |-----------|--------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
    | PodGroup  | NotEnoughResources | **3/6** tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable; **Pending: 3 Unschedulable, 3 Schedulable**                                                             |
    | Task1     | **Schedulable**    | **Pod ns1/task-1 can possibly be assigned to node1, once minAvailable is satisfied**                                                                                                            |
    | Task2     | **Schedulable**    | **Pod ns1/task-2 can possibly be assigned to node2, once minAvailable is satisfied**                                                                                                            |
    | Task3     | **Schedulable**    | **Pod ns1/task-3 can possibly be assigned to node3, once minAvailable is satisfied**                                                                                                            |
    | Task4     | Unschedulable      | all nodes are unavailable: 1 plugin InterPodAffinity predicates failed node(s) didn't match pod affinity/anti-affinity, node(s) didn't match pod affinity rules, 2 node(s) resource fit failed. |
    | Task5 ~ 6 | Unschedulable      | **3/6** tasks in gang unschedulable: pod group is not ready, 6 Pending, 6 minAvailable; **Pending: 3 Unschedulable, 3 Schedulable**                                                             |

    - Note: Task5 ~ 6 are `Unschedulable` maybe because that this two locate after task4 by `TaskOrderFn`;

- In improved information (**Deprecated**), we can easily find the one that breaks the whole scheduling cycle and why dose that happen. Additionally,  we can find the histogram of reason why there are some tasks whose status is pending.

## Stable messages

[Stable Pod and PodGroup Scheduling Diagnostics](stable-scheduling-diagnostics.md) replaces the message texts above. Their counts and trial node names change between scheduling sessions while the blocker stays the same, which causes repeated Pod status updates and Events.

### Example

The same case with stable messages:

- PodGroup. The condition and the Event share one message.

  | Condition | Reason | Message |
  | --- | --- | --- |
  | `Unschedulable=True` | `NotEnoughResources` | Gang requirements are not satisfied. Pod ns1/task-4 has no node with enough resources. Pod ns1/task-4 has no node satisfying Pod affinity constraints. |

- Tasks. Each row is the `PodScheduled=False` condition of the Pods.

  | Tasks | Reason | Message |
  | --- | --- | --- |
  | Task1 ~ 3 | `Unschedulable` | Waiting for PodGroup ns1/job to satisfy gang requirements. |
  | Task4 | `Unschedulable` | No eligible node satisfies this Pod's Pod affinity constraints and resource requests. |
  | Task5 ~ 6 | `Unschedulable` | Waiting for PodGroup ns1/job to satisfy gang requirements. |

  - Note: Task1 ~ 3 found a node, but their allocations are rolled back because the gang cannot proceed. They report `Unschedulable` and wait for the PodGroup. `Schedulable` is kept for a Pod that retains its allocation.

Task4 still identifies the Pod that breaks the scheduling cycle, and the PodGroup names it and its blockers.

### Message reference

The tables list every message that the scheduler publishes. No message contains a node total, a per-reason count, a trial Node name or a candidate HyperNode name.

**Pod.** The condition is `PodScheduled=False`. The `FailedScheduling` Event repeats its message.

| Pod state | Reason | Message |
| --- | --- | --- |
| No node fits across all its permitted nodes | `Unschedulable` | `No eligible node satisfies this Pod's <constraints>.` |
| Placement rolled back, or evaluation stopped by gang failure | `Unschedulable` | `Waiting for PodGroup <namespace>/<name> to satisfy gang requirements.` |
| Allocation retained while the gang is not ready | `Schedulable` | `Waiting for PodGroup <namespace>/<name> to satisfy gang requirements.` |
| Pipeline retained | `Unschedulable` | `Waiting for resources to be released for this Pod.` |
| Scheduler error | `SchedulerError` | The explicit error |
| Admission rejection | Supplied by the rejecting path | Supplied by the rejecting path |
| Not evaluated in this session | — | The observed condition is kept |

`<constraints>` names the distinct constraints that no node satisfies, in sorted order: `node affinity`, `taint tolerations`, `Pod affinity constraints`, `<resource> request`, and `scheduling predicates` for a failure without a structured category. For example:

```text
No eligible node satisfies this Pod's node affinity.
No eligible node satisfies this Pod's GPU request and node affinity.
```

**PodGroup.** The condition and the Event share one message.

| Group state | Condition | Message |
| --- | --- | --- |
| Ready | `Scheduled=True` | `PodGroup satisfies its gang requirements.` |
| Unready, blockers recorded | `Unschedulable=True`, reason `NotEnoughResources` | `Gang requirements are not satisfied.`, then one sentence for each blocker |
| Unready, no blocker recorded | `Unschedulable=True`, reason `NotEnoughResources` | `Gang requirements are not satisfied.` |
| Unready, not evaluated in this session | — | The observed condition is kept |

Each blocker adds one of these sentences:

| Blocker | Sentence |
| --- | --- |
| A member fails node affinity | `Pod ns/A has no node satisfying node affinity.` |
| A member has no tolerated node | `Pod ns/A has no node with tolerated taints.` |
| A member fails Pod affinity | `Pod ns/A has no node satisfying Pod affinity constraints.` |
| A member finds no node with enough of a resource | `Pod ns/A has no node with enough GPU.` |
| A member fails a predicate without a structured category | `Pod ns/A does not satisfy scheduling predicates.` |
| A HyperNode cannot hold the minimum resources | `Insufficient HyperNode GPU capacity.` |
| A HyperNode is rejected by placement requirements | `HyperNode placement requirements are not satisfied.` |

A member sentence found during a HyperNode search ends with `within the checked HyperNodes`. Inside an established placement it ends with `within HyperNode <name>`. Three complete messages:

```text
A member fails node affinity, without HyperNodes:
Gang requirements are not satisfied. Pod ns/A has no node satisfying node affinity.

A HyperNode lacks GPU capacity, and a member fails node affinity in the HyperNodes that were searched:
Gang requirements are not satisfied. Insufficient HyperNode GPU capacity. Pod ns/A has no node satisfying node affinity within the checked HyperNodes.

Two members inside an established placement in H1:
Gang requirements are not satisfied. Pod ns/A has no node with enough GPU within HyperNode H1. Pod ns/B has no node with tolerated taints within HyperNode H1.
```
