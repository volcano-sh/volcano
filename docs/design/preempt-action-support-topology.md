# Preempt Action Support Topology

## Motivation

In cloud-native task scheduling scenarios, preemption is a key feature to ensure timely scheduling of high-priority tasks. Compared to the K8s scheduler, Volcano's current preemption implementation is relatively simple, especially in handling affinity judgments. To improve the accuracy and efficiency of the preemption mechanism, the existing implementation needs to be optimized, particularly in supporting topology awareness.

## In Scope

- Optimize Volcano's preemption mechanism to support affinity judgments
- Improve single Pod preemption process
- Implement simulation scheduling interface to ensure simulated addition and removal of pods won't cause topology changes

## Out of Scope

- Gang scheduling preemption scenario optimization

## User Stories

### Story 1

As a cluster administrator, I want the system to accurately judge Pod affinity constraints during preemption scheduling to avoid scheduling failures caused by topology changes.

### Story 2

As a user, I expect high-priority Pod preemption to minimize impact on existing Pods while maintaining consistency of affinity rules.

### Story 3

When topology-sensitive resources like GPUs exist, the preemption process needs to consider resource topology relationships to ensure resource allocation after preemption still satisfies original topology constraints.

For example, if a node has 2 GPUs (8GB each), Pod A and Pod B each use 4GB, and Pod C needs 8GB. When Pod C needs to be scheduled, it triggers the preemption mechanism. During the simulation scheduling process, the system will try to preempt Pod A and reschedule it. There are two possible scenarios:

1. If topology changes during simulation scheduling:

   - System chooses to preempt Pod A
   - The predicate function check successfully for Pod C
   - When simulating the re-addition of Pod A, the binpack strategy causes Pod A to be scheduled to a different GPU
   - After re-adding Pod A, the predicate function check still passes for Pod C
   - This means Pod C can be scheduled without actually removing Pod A
   - Therefore, the preemption is considered unnecessary and fails
2. If topology remains consistent during simulation scheduling:

   - System chooses to preempt Pod A
   - The predicate function check successfully for Pod C
   - When simulating the re-addition of Pod A, the original topology relationship is maintained
   - After re-adding Pod A, the predicate function check fails for Pod C
   - This confirms that Pod A must be removed for Pod C to be scheduled
   - Therefore, the preemption is considered necessary and succeeds

Therefore, when implementing the preemption mechanism, it's crucial to verify the necessity of preemption by checking if the topology changes during pod re-addition would affect the scheduling of the preempting pod.

![preempt-action-support-topology-1](images/preempt-action-support-topology/preempt-action-support-topology-1.png)

## Design Detail

### Preemption Process

![preempt-action-support-topology-2](images/preempt-action-support-topology/preempt-action-support-topology-2.png)

1. Execute Predicate on all nodes that are not UnschedulableAndUnresolvable to obtain candidate node list, and perform parallel simulation scheduling on all candidate nodes.
2. The simulation scheduling process for each node is as follows:

   1. First consider Pods with lower priority as potential victims on the node
   2. Sort the victim list (lower priority and non-PDB-violating victims come first)
   3. Remove victims in order, add each removed one to eviction candidates, and observe if the verification function passes
   4. Verification function: Try to add pods (pipelined) with higher priority targeting the current node, observe if they can pass predicate; then remove them and observe if they can pass predicate
   5. If passed, try to add back the previous eviction candidates in PDB and priority order (to minimize impact), calling verification function after each addition; if verification fails, add to final eviction list
   6. If final eviction list is not empty, return it
3. Sort filtered nodes using PreemptNodeOrderFn
4. Schedule Pod to the top-ranked node, evict victims list, and cancel nominatedNodeName of lower priority pods that had nominated this node, moving them from pipeline to pending schedule

### Key Function Modifications

- `SimulateRemoveTaskFn`: Simulate the removal of a task from a node, plugins implement this function to ensure the removal action does not cause topology changes

  ```go
  type SimulateRemoveTaskFn func(ctx context.Context, state *k8sframework.CycleState, taskToSchedule *TaskInfo, taskInfoToRemove *TaskInfo, nodeInfo *NodeInfo) error
  ```
- `SimulateAddTaskFn`: Simulate the addition of a task to a node, plugins implement this function to ensure the addition action does not cause topology changes

  ```go
  type SimulateAddTaskFn func(ctx context.Context, state *k8sframework.CycleState, taskToSchedule *TaskInfo, taskInfoToAdd *TaskInfo, nodeInfo *NodeInfo) error
  ```
- `SimulatePredicateFn`: Simulate the predicate check for a task on a node, plugins implement this function to verify if the task can be scheduled to the node while maintaining topology constraints

  ```go
  type SimulatePredicateFn func(ctx context.Context, state *k8sframework.CycleState, task *TaskInfo, nodeInfo *NodeInfo) error
  ```
- `SimulateAllocatableFn`: Simulate the allocatable check for a node, plugins implement this function to verify if the queue has enough resources to schedule the task while maintaining topology constraints

  ```go
  type SimulateAllocatableFn func(ctx context.Context, state *k8sframework.CycleState, queue *QueueInfo, task *TaskInfo) bool
  ```

### Limitations

- Current design focuses on single pod preemption scenarios. Does not handle complex topology changes in gang scheduling
- For complex combinations of affinity rules, multiple attempts may be needed to find the optimal solution. Performance impact of simulation scheduling needs to be evaluated in large-scale clusters
