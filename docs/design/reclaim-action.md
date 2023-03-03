# Reclaim

## Introduction

In kube-batch there are 4 actions such as allocate, preempt, reclaim, backfill and with the help of plugins like conformance, drf, gang, nodeorder and more plugins. All these plugins provides behavioural characteristics how scheduler make scheduling decisions.

## Reclaim Action

Reclaim is one of the actions in kube-batch scheduler.  Reclaim action comes into play when
a new queue is created, and new job comes under that queue but there is no resource / less resource
in cluster because of change of deserved share for previous present queues.

When a new queue is created, resource is divided among queues depending on its respective weight ratio.
Consider two queues is already present and entire cluster resource is used by both the queues.  When third queue
is created, deserved share of previous two queues is reduced since resource should be given to third queue as well.
So jobs/tasks which is under old queues will not be evicted until, new jobs/tasks comes to new queue(Third Queue).  At that point of time,
resource for third queue(i.e. New Queue) should be reclaimed(i.e. few tasks/jobs should be evicted) from previous two queues, so that new job in third queue can
be created.

Reclaim is basically evicting tasks from other queues so that present queue can make use of it's entire deserved share for
creating tasks.

In Reclaim Action, there are multiple plugin functions that are getting used like,

1. QueueOrderFn(Plugin: Priority, DRF, Proportion),
2. TaskOrderFn(Plugin: Priority),
3. JobOrderFn(Plugin: Priority, DRF, Gang),
4. NodeOrderFn(Plugin: NodeOrder),
5. PredicateFn(Plugin: Predicates),
6. ReclaimableFn(Plugin: Conformance, Gang, Proportion). 
7. QueueScoreOrderFn(Plugin: Priority, DRF, Proportion),

### 1. QueueOrderFn:
#### Priority:
Compares queuePriority set in Spec(using PriorityClass) and returns the decision of comparison between two priorities.

#### DRF:
The queue with the lowest share of dominant resource will have higher priority.

#### Proportion:
The queue with the lowest share will have higher priority.

### 2. TaskOrderFn:
#### Priority:
Compares taskPriority set in PodSpec and returns the decision of comparison between two priorities.

### 3. JobOrderFn:
#### Priority:
Compares jobPriority set in Spec(using PriorityClass) and returns the decision of comparison between two priorities.

#### DRF:
The job having the lowest share will have higher priority.

#### Gang:
The job which is not yet ready(i.e. minAvailable number of task is not yet in Bound, Binding, Running, Allocated, Succeeded, Pipelined state) will have high priority.

### 4. NodeOrderFn:
#### NodeOrder:
NodeOrderFn returns the score of a particular node for a specific task by running through sets of priorities.

### 5. PredicateFn:
#### Predicates:
PredicateFn returns whether a task can be bounded to a node or not by running through set of predicates.

### 6. ReclaimableFn:
Checks whether a task can be evicted or not, which returns set of tasks that can be evicted so that new task can be created in new queue.
#### Conformance:
In conformance plugin, it checks whether a task is critical or running in kube-system namespace, so that it can be avoided while computing set of tasks that can be preempted.
#### Gang:
It checks whether by evicting a task, it affects gang scheduling in kube-batch.  It checks whether by evicting particular task,
total number of tasks running for a job is going to be less than the minAvailable requirement for gang scheduling requirement.
#### Proportion:
It checks whether by evicting a task, that task's queue has allocated resource less than the deserved share.  If so, that task
is added as a victim task that can be evicted so that resource can be reclaimed.

### 7. QueueScoreOrderFn:
According to the weight of plugins priority, drf, and proportion, calculate the queue's total score and sort the queue by total score.
#### Priority:
The queue with the high priority class will have higher score.

#### DRF:
The queue with the lowest share of dominant resource will have higher score.

#### Proportion:
The queue with the lowest share will have higher score.