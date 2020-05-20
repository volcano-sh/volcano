# Preemption

## Introduction

In scheduler there are 4 actions such as `allocate`, `preempt`, `reclaim`, `backfill` and with the help of
plugins like `conformance`, `drf`, `gang`, `nodeorder` and more plugins. All these plugins provides
behavioural characteristics how scheduler make scheduling decisions.

## Preempt Action

As discussed in Introduction, preempt is one of the actions in kube-batch scheduler.  Preempt action comes into play
when a high priority task comes and there is no resource requested by that task is available in the cluster,
then few of the tasks should be evicted so that new task will get resource to run.

In preempt action, multiple plugin function are getting used like

1.  TaskOrderFn(Plugin: Priority),
2.  JobOrderFn(Plugin: Priority, DRF, Gang),
3.  NodeOrderFn(Plugin: NodeOrder),
4.  PredicateFn(Plugin: Predicates),
5.  PreemptableFn(Plugin: Conformance, Gang, DRF).

### 1. TaskOrderFn:
#### Priority:
Compares taskPriority set in PodSpec and returns the decision of comparison between two priorities.

### 2. JobOrderFn:
#### Priority:
Compares jobPriority set in Spec(using PriorityClass) and returns the decision of comparison between two priorities.

#### DRF:
The job having the lowest share will have higher priority.

#### Gang:
The job which is not yet ready(i.e. minAvailable number of task is not yet in Bound, Binding, Running, Allocated, Succeeded, Pipelined state) will have high priority.

### 3. NodeOrderFn:
#### NodeOrder:
NodeOrderFn returns the score of a particular node for a specific task by running through sets of priorities.

### 4. PredicateFn:
#### Predicates:
PredicateFn returns whether a task can be bounded to a node or not by running through set of predicates.

### 5. PreemptableFn:
Checks whether a task can be preempted or not, which returns set of tasks that can be preempted so that new task can be deployed.
#### Conformance:
In conformance plugin, it checks whether a task is critical or running in kube-system namespace, so that it can be avoided while computing set of tasks that can be preempted.
#### Gang:
It checks whether by evicting a task, it affects gang scheduling in kube-batch.  It checks whether by evicting particular task,
total number of tasks running for a job is going to be less than the minAvailable requirement for gang scheduling requirement.
#### DRF:
The preemptor can only preempt other tasks only if the share of the preemptor is less than the share of the preemptee after recalculating the resource allocation of the premptor and preemptee.
