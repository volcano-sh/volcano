# Enqueue

## Introduction

In Volcano there are 6 actions such as `enqueue`, `allocate`, `preempt`, `reclaim`, `backfill`, `shuffle` and with the help of
plugins like `conformance`, `drf`, `gang`, `nodeorder` and more plugins. All these plugins provides
behavioural characteristics how scheduler make scheduling decisions.

## Enqueue Action

Enqueue is one of the actions in Volcano scheduler.  Enqueue action comes into play when
a new job is created, this action will filter qualified jobs and take them to the next action.

When the minimum of resource requests under the job can not be met, pods under the job will not be schedule 
because the "Gang" constraint is not reached. State of job allowed change from `Pending` to `Inqueue` 
only when the minimum resource of the job is met, then the pod under the job can enter the scheduling stage.

Enqueue action is the preparatory stage in the scheduling process. It can prevent a large number of
unscheduled pods in the cluster and Improve the throughput performance of the scheduler in the case of 
insufficient cluster resources. Therefore, the Enqueue action is an essential action for the scheduler configuration.

In Enqueue Action, there are multiple plugin functions that are getting used like

1. QueueOrderFn(Plugin: Priority, DRF, Proportion),
2. JobOrderFn(Plugin: Priority, DRF, Gang),
3. JobEnqueueableFn(Plugin: OverCommit, Proportion, ResourceQuota, SLA),
4. JobEnqueuedFn(Plugin: OverCommit),

### 1. QueueOrderFn:
#### Priority:
Compares queuePriority set in Spec(using PriorityClass) and returns the decision of comparison between two priorities.

#### DRF:
The queue with the lowest share of dominant resource will have higher priority.

#### Proportion:
The queue with the lowest share will have higher priority.

### 2. JobOrderFn:
#### Priority:
Compares jobPriority set in Spec(using PriorityClass) and returns the decision of comparison between two priorities.

#### DRF:
The job having the lowest share will have higher priority.

#### Gang:
The job which is not yet ready(i.e. minAvailable number of task is not yet in Bound, Binding, Running, Allocated, Succeeded, Pipelined state) will have high priority.

### 3. JobEnqueueableFn:
If there is a plugin that voted to allow in the `JobEnqueueableFn` while other plugins voted abstain in the same tier, the job `enqueueable` is allowed and jobEnqueueable will not check the next tier.
#### OverCommit:
After the resource is overCommit, the minResource of the current job plus the inqueue resource of other jobs are lessEqual than the idle resource, the job status is allowed to change from `Pending` to  `Inqueue`, otherwise it will be rejected.

#### Proportion:
The minResource of the current job plus the allocated & inqueue resources of current queue and minus the elastic resources of current queue are lessEqual than the actual capacity of current queue, the job status is allowed to change from `Pending` to  `Inqueue`, otherwise it will be rejected.

#### ResourceQuota:
Each resource type of the minResource of the current job lessEqual the resourceQuota of the namespace, the job status is allowed to change from `Pending` to  `Inqueue`, otherwise it will be rejected.

#### SLA:
Job pending state waiting timeout, the job status is allowed to change from `Pending` to  `Inqueue`, otherwise it will be abstained.

### 4. JobEnqueuedFn:
#### OverCommit:
This interface was used to update resource infos after one job turned `Inqueue`. OverCommit plugin need this interface to update `Inqueue` resources in each queue and whole cluster.