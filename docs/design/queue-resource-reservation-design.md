# Volcano Resource Reservation For Queue

@[Thor-wl](https://github.com/Thor-wl); Nov 3rd, 2020

## Motivation
As [issue 1101](https://github.com/volcano-sh/volcano/issues/1101) mentioned, Volcano should support resource reservation
for specified queue. Requirement detail as follows:
* Support reserving specified resources for specified queue
* Consider preemption reservation and non-preemption reservation. The former is used to emergency reservation, which may
evict existing running workloads but with better efficiency. The later is generally used for reserving without eviction.
* Support enable and disable resource reservation for specified queue dynamically without restarting Volcano.
* Support hard reservation resource specified and percentage reservation resource specified.

## Consideration
### Resource Request
* The request cannot be more than the total resource amount of cluster at all dimensions.
* If `capability` is set, request must be no more than it at all dimensions.
* The resource amount reserved must be no less than request at all dimensions but should not exceed too much. An algorithm 
ensuring the reserved amount to the point is necessary.
* Support total node number percentage of cluster as request. If reservation resource amount is also specified, it's
important to decide which configuration is adopted. This feature is more useful on the condition that resource specification
of all nodes are almost the same.

### Reservation Algorithm
* Take preemption and non-preemption reservation into consideration.
* Preemption reservation may results in running workloads exits abnormally.
* Non-preemption reservation needs gentle treatment to balance scheduling and reserving performance.
* Although preemption reservation configured, try to reserve resource without eviction if idle resource can satisfy demand.
* Nodes locked by target job cannot be chosen as locked node.
* Nodes locked by another queue cannot be chosen as locked node.
* Try to lock nodes, whose total resource is as less as possible on base of satisfying requirement, to avoid dramatic 
scheduling performance degradation.

### Safety
* Malicious application for large amount of resource reservation will cause jobs in other queue to block.
* Preemption reservation has a risk of hurting existing business.

## Design
### API
```
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: q1
spec:
  reclaimable: true
  weight: 1

  guarantee:            // reservation key word
    policy: Best-Effort // preemption reservation or non-preemption reservation
    percentage: 0.5     // locked nodes number percentage in cluster
    resource:           // specified reserving resource
      cpu: 2c
      memory: 4G

status:
  state: Open

  reservation:          // reservation status key word
    nodes:              // locked nodes list
      - n1
      - n2
    resource:           // total idle resource in locked nodes
      cpu: 1C
      memory: 2G 
```
### Detail
#### Fields
##### policy
Option Field, options are "Best-Effort" and "Guaranteed" and "Best-Effort" by default. If "Best-Effort" is set, scheduler
will reserve resource without evicting running workloads, while "Guaranteed" is opposite.
#### percentage
Option Field if `resource` not specified, legal value range is [0, 1]. Locked node number will be as follows: 
```
math.Floor(clusterNodeNumber * percentage)
```
It should be noted that nodes will be locked randomly util reaching the percentage. If `resource` is also specified, choose
the one with more reserved resources as the final result.
#### guarantee.resource
Option Field if `percentage` not specified. List of reserving resource categories and amount. 
#### nodes
List of locked nodes.
#### status.resource
Total idle resource of locked nodes. You can judge whether reservation has met demand by watching this field.

#### Algorithm
##### Node Selection
* Big Nodes

Sort all nodes according total resource amount of one or some dimensions. Select the least nodes which rank first and 
satisfy requirement. It's the most efficient way for preemption reservation.  

* Idle First

Sort all nodes according idle resource of one or some dimensions. Select the least nodes which rank first as locked nodes.
It's suitable for non-preemption. It's should to be noted that the locked nodes are often optimal solution but suboptimal
solution sometimes. 

##### Lock Strategy
* Disposable Lock

Disposable Lock to all selected nodes. It's certainly effective for preemption reservation. To non-preemption reservation,
It' a good practice but not always.

* Multiple Lock

Lock nodes through a few of scheduling cycle. Just lock one node in one cycle. It's more flexible and dynamic.

## Implementation
TODO