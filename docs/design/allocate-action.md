# Allocate

## Introduction

In Volcano there are 6 actions such as `enqueue`, `allocate`, `preempt`, `reclaim`, `backfill`, `shuffle` and with the help of
plugins like `conformance`, `drf`, `gang`, `nodeorder` and more plugins. All these plugins provides
behavioural characteristics how scheduler make scheduling decisions.

## Allocate Action

Allocate is one of the actions in Volcano scheduler. Allocate action is used to schedule pods with resource requests 
in the pod list. it's including pre-selection and further selection. PrePredicateFn & PredicateFn is used to 
filter out nodes that cannot be allocated, and NodeOrderFn is used to score the nodes to find the one that best fits.

The Allocate action follows the commit mechanism. When a pod’s scheduling request is satisfied, a binding action is not 
necessarily performed for that pod. This step also depends on whether the gang constraint of the Job in which the pod resides is 
satisfied. Only if the gang constraint of the Job in which the pod resides is satisfied can the pod be scheduled; otherwise, 
the pod cannot be scheduled.

In Allocate Action, there are multiple plugin functions that are getting used like,

1. NamespaceOrderFn(Plugin: DRF),
2. QueueOrderFn(Plugin: Priority, DRF, Proportion),
3. TaskOrderFn(Plugin: Priority),
4. JobOrderFn(Plugin: Priority, DRF, Gang),
5. AllocatableFn(Plugin: Proportion),
6. PrePredicateFn(Plugin: Predicates),
7. PredicateFn(Plugin: Predicates),
8. BatchNodeOrderFn(Plugin: NodeOrder)

### 1. NamespaceOrderFn:
#### DRF:
The namespace having the lowest share will have higher priority.

### 2. QueueOrderFn:
#### Priority:
Compares queuePriority set in Spec(using PriorityClass) and returns the decision of comparison between two priorities.

#### DRF:
The queue with the lowest share of dominant resource will have higher priority.

#### Proportion:
The queue with the lowest share will have higher priority.

### 3. TaskOrderFn:
#### Priority:
Compares taskPriority set in PodSpec and returns the decision of comparison between two priorities.

### 4. JobOrderFn:
#### Priority:
Compares jobPriority set in Spec(using PriorityClass) and returns the decision of comparison between two priorities.

#### DRF:
The job having the lowest share will have higher priority.

#### Gang:
The job which is not yet ready(i.e. minAvailable number of task is not yet in Bound, Binding, Running, Allocated, Succeeded, Pipelined state) will have high priority.

### 5. AllocatableFn:
#### Proportion:
The task is allowed to allocate when resource request of task is less than free resource in queue, otherwise it will be rejected.

### 6. PrePredicateFn:
#### Predicates:
PrePredicateFn returns whether a task can be bounded to some node or not by running through set of predicates.
PrePredicateFn returns whether a task can be bounded to some node or not by running through set of predicates.
Mainly the `preFilter` func of NodePorts, InterPodAffinity and PodTopologySpread plugin in the k8s native scheduler framework takes effect.

### 7. PredicateFn:
#### Predicates:
PredicateFn returns whether a task can be bounded to a node or not by running through set of predicates.
Mainly the `filter` func of NodeUnschedulable, NodeAffinity, TaintToleration, NodePorts, InterPodAffinity, CSILimits, VolumeZone
and PodTopologySpread plugin in the k8s native scheduler framework takes effect.

### 8. BatchNodeOrderFn:
#### NodeOrder:
BatchNodeOrderFn calculate the score of each node under the influence of different strategies.
Mainly the `Score` func of InterPodAffinity, TaintToleration, PodTopologySpread and SelectorSpread plugin in the k8s native scheduler framework takes effect.