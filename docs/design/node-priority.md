## Node Priority in Kube-Batch

This feature allows `kube-batch` to schedule workloads based on the priority of the Nodes, Workloads will be scheduled on Nodes with higher priority and these priorities will be calculated based on different parameters like `ImageLocality`, `Most/Least Requested Nodes`...etc.
A basic flow for the Node priority functions is depicted below.

![Node Priority Flow](./images/Node-Priority.png)

Currently in kube-batch `Session` is opened every 1 sec and the workloads which are there in Queue goes through `Predicate` to find a suitable set of Nodes where workloads can be scheduled and after that it goes through `Allocate` function to assign the Pods to the Nodes and then goes to `Preempt` if applicable.

Node Priority can be introduced in the current flow for `Allocate` and `Preempt` function. Once we have set of Nodes where we can scheduled the workloads then flow will go through `Prioritize` function which will do the following things :

 - Run all the priority functions on all the list Nodes which is given by `Predicate` function in a parallel go-routine.
 - Score the Node based on whether the `Priority Rule` satisfies the Workload scheduling criteria.
 - Once the scores are returned from all the `PriorityFn` then aggregate the scoring and identify the Node with highest scoring.
 - Delegate this selected Node in last step to `AllocateFn` to Bind the workload to the Node.

Currently there are multiple `PriorityFn` available with default Scheduler of Kubernetes. Going forward with each release we will implement all the priority functions in kube-batch based on their importance to batch scheduling.

