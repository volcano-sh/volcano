# How to Configure Scheduler

## Requirements
* Before reading the guidance, please make sure you are aware of basic concepts such as `action` `plugin` `session` `tier`
`volcano job` `podgroup` `queue` and so on. If they are still strange to you, please refer to [Volcano Docs](https://volcano.sh/en/docs/)
for more details.
* Before reading the guidance, please make sure you have general understanding of Volcano scheduling workflow(this part will
be added later).

## Background
In order to adjust the scheduling process and algorithms to different scenarios, Volcano allows users to configure actions
and plugins for volcano scheduler. The scheduling pipeline consists of a series of actions. The plugins implement the algorithms,
which will be called in actions as registered session functions. As what you can see in the configmap `volcano-scheduler-configmap`,
plugins are divided into 2 tiers by default. It may confuse some users. Besides, it is necessary to provide a guidance about
how to configure volcano scheduler.

## Key Points
* All the configurations are configured in the configmap `volcano-scheduler-configmap`, which is under the namespace `volcano-system`.
* The configuration is made up of 2 parts: `actions` and `tiers`. 
* `actions` defines the scheduling pipeline. They will be executed in order in each session.
* `tiers` divides the plugins into several categories. All the functions defined in the plugins will be registered when
a session is open and called when actions are executed. 
* In some scenarios, users may configure different plugins which registers the same functions. It will depend on the business
requirement to decide how to combine these functions. That's why `tier` is required.

## Actions
* `Action` implements the main logic of scheduling. 
* Volcano allow users to make self-defined actions.
* Volcano provides 7 built-in actions until April 2022. The details are as follows.

| ID  | Name     | Required | Description                                                                                                                                                                                                                                                           |
|-----|----------|----------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 1   | enqueue  | Y        | Judge whether the idle resource in the cluster can satisfy the basic demand of a workload. If yes, set the podgroup of the workload to be `inqueue`, otherwise keep the podgroup `pending`. Notice that the default value for parameter `overcommit-factor` is `1.2`. |
| 2   | allocate | Y        | Try to allocate resource to workloads whose corresponding podgroup status is `inqueue`.                                                                                                                                                                               |
| 3   | backfill | N        | Try to allocate resource to workloads whose pods are `BestEffort`.                                                                                                                                                                                                    |
| 4   | preempt  | N        | Recognise workloads with high priority. Try to evict pods with low priority and allocate the resource to them.                                                                                                                                                        |
| 5   | reclaim  | N        | Pick out queues whose resources have been borrowed by other queues and reclaim them back.                                                                                                                                                                             |
| 6   | elect    | N        | Select a workload satisfying some conditions. It is designed to work with resource reservation for target workload. Will deprecated at future releases.                                                                                                               |
| 7   | reserve  | N        | Select a series of nodes and reserve resource. It is designed to work with resource reservation for target workload. Will deprecated at future releases.                                                                                                              |

## Tiers and Plugins
* `Plugin` provides implementation details about scheduling algorithms by registering a series of functions. These functions
will be called during actions are executed.
* In general, a plugin mainly consists of 3 functions: `Name` `OnSessionOpen` `OnSession`. `Name` provides the name of the
plugin. `OnSessionOpen` executes some operations when a session starts and register some functions about scheduling details.
`OnSessionClose` clean up some resource when a session finishes.
* Some plugins provide arguments for users to match their custom scenarios.
* Different plugins may register same functions with different logic. Please make sure they can work together when configuring
plugins.
* Volcano provides 15 built-in plugins until April 2022. The details are as follows.

| ID  | Name          | Arguments                                                                                                                                                                                                                                                                                                                                         | Registered Functions                                                                                                                    | Description                                                                                               |
|-----|---------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------|
| 1   | binpack       | * binpack.weight<br/> * binpack.cpu<br/> * binpack.memory<br/> * binpack.resources                                                                                                                                                                                                                                                                | * nodeOrderFn                                                                                                                           | Try to bind pods to nodes with high resource usage to reduce fragmentation.                               |
| 2   | conformance   | /                                                                                                                                                                                                                                                                                                                                                 | * preemptableFn<br/> * reclaimableFn                                                                                                    | Skip critical pods and not evict them.                                                                    |
| 3   | drf           | /                                                                                                                                                                                                                                                                                                                                                 | * preemptableFn<br/> * queueOrderFn<br/> * reclaimFn<br/> * jobOrderFn<br/> * namespaceOrderFn                                          | Provide fair resource shares for all queues.                                                              |
| 4   | extender      | * extender.urlPrefix<br/> * extender.httpTimeout<br/> * extender.onSessionOpenVerb<br/> * extender.onSessionCloseVerb<br/> * extender.predicateVerb<br/> * extender.prioritizeVerb<br/> * extender.preemptableVerb<br/> * extender.reclaimableVerb<br/> * extender.queueOverusedVerb<br/> * extender.jobEnqueueableVerb<br/> * extender.ignorable | * predicateFn<br/> * batchNodeOrderFn<br/> * preemptableFn<br/> * reclaimableFn<br/> * jobEnqueueableFn<br/> * overusedFn               | Add outer http server to execute custom actions.                                                          |
| 5   | gang          | /                                                                                                                                                                                                                                                                                                                                                 | * jobValidFn<br/> * reclaimableFn<br/> * preemptableFn<br/> * jobOrderFn<br/> * JobReadyFn<br/> * jobPipelineFn<br/> * jobStarvingFn    | Consider the minimal resource requirement or member number for a workload when allocate resource to it.   |
| 6   | nodeorder     | * nodeaffinity.weight<br/> * podaffinity.weight<br/> * leastrequested.weight<br/> * balancedresource.weight<br/> * mostrequested.weight<br/> * tainttoleration.weight<br/> * imagelocality.weight                                                                                                                                                 | * nodeOrderFn<br/> * batchNodeOrderFn                                                                                                   | Sort all nodes in custom way.                                                                             |
| 7   | numaaware     | * weight                                                                                                                                                                                                                                                                                                                                          | * predicateFn<br/> * batchNodeOrderFn                                                                                                   | Consider CPU Numa as a key factor when binding a pod to a node.                                           |
| 8   | overcommit    | * overcommit-factor                                                                                                                                                                                                                                                                                                                               | * jobEnqueueableFn<br/> * jobEnqueuedFn                                                                                                 | Set the available resource as the given times of the whole resource of the cluster.                       |
| 9   | predicate     | * predicate.GPUSharingEnable<br/> * predicate.CacheEnable<br/> * predicate.ProportionalEnable<br/> * predicate.resources<br/> * predicate.resources.nvidia.com/gpu.cpu<br/> * predicate.resources.nvidia.com/gpu.memory                                                                                                                           | * predicateFn<br/>                                                                                                                      | Add custom functions about how to filter nodes for pods.                                                  |
| 10  | priority      | /                                                                                                                                                                                                                                                                                                                                                 | * taskOrderFn<br/> * jobOrderFn<br/> * preemptableFn<br/> * jobStarvingFn                                                               | Defines priority for workloads.                                                                           |
| 11  | proportion    | /                                                                                                                                                                                                                                                                                                                                                 | * queueOrderFn<br/> * reclaimableFn<br/> * overusedFn<br/> * allocatableFn<br/> * jobEnqueueableFn<br/>                                 | Divide the whole resources of the cluster to all queues as proportion according to queues' configurations |
| 12  | reservation   | /                                                                                                                                                                                                                                                                                                                                                 | * targetJobFn<br/> * reservedNodesFn                                                                                                    | Sort nodes as resource usage and lock parts for target workload as reservation.                           |
| 13  | sla           | * sla-waiting-time                                                                                                                                                                                                                                                                                                                                | * jobOrderFn<br/> * jobEnqueueableFn<br/> * JobPipelinedFn                                                                              | Sort workloads according to the SLA settings.                                                             |
| 14  | task-topology | /                                                                                                                                                                                                                                                                                                                                                 | * taskOrderFn<br/> * nodeOrderFn                                                                                                        | Bind pods with different roles to nodes according to the given policy.                                    |
| 15  | tdm           | * tdm.revocable-zone.rz1<br/> * tdm.revocable-zone.rz2<br/> * tdm.evict.period                                                                                                                                                                                                                                                                    | * predicateFn<br/> * nodeOrderFn<br/> * preemptableFn<br/> * victimTasksFn<br/> * jobOrderFn<br/> * jobPipelinedFn<br/> * jobStarvingFn | Enable part of nodes to be in the charge of K8s and other clusters in different period.                   |

## Examples
```yaml
# default configuration for scheduler
actions: "enqueue, allocate, backfill"
tiers:
- plugins:
  - name: priority
  - name: gang
  - name: conformance
- plugins:
  - name: overcommit
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodeorder
  - name: binpack
```

Note:
* According to the default configuration, the scheduling process works as follows at a session. The scheduler will run the
following pipeline regularly. The period is `1s` by default.

```mermaid
graph LR
    1(Start) --> 2(OpenSession) --> 3(enqueue) --> 4(allocate) --> 5(backfill) --> 6(CloseSession) --> 7(End)
```

* All the functions in the configured plugins will be registered when executing `OpenSession` and called when executing
the configured actions. For example, `jobEnqueueable` function, which is registered in `overcommit` plugin and called at
`enqueue` action, aims to judge whether the idle resource of the cluster can satisfy the minimal demand of a workload. 
* Both `overcommit` and `proportion` plugin have registered function `jobEnqueueableFn`, which will be called in the function
`JobEnqueueable`. Besides, `overcommit` and `proportion` are in the same tier. According to the implementation of `JobEnqueueable`,
it will get through all the plugins in different tiers in order. If any `jobEnqueueableFn` returns a value belows `0`, it
stops executing the `jobEnqueueableFn` registered in the following plugins and returns `false`. Namely, if the `jobEnqueueableFn`
registered in `overcommit` returns a value belows `0`, `jobEnqueueableFn`, which is called in `enqueue` action, will return
`false` and never call the `jobEnqueueableFn` registered in the `proportion` plugin.

## FAQ
* How can I decide which plugins should be grouped into a tier? How many tiers should I set for my business?
> In most scenarios, users should not concern about how to divide plugins to different tiers. It's OK to configure all
plugins within a single tier. Only on condition that it is related with **eviction** will you need to think of how to organize
the plugins in different tiers. For example, when enable `reclaim` action, the scheduler will try to collect a set of victims.
In order to reduce the influence to users' business, it is reasonable to pick out victims as less as possible. Then you can 
configure the plugins evicting the least pods in the first tier. And configure other plugins with eviction at the second tier.
If the first tier can pick out victims, it will not call the functions registered in the plugins, which is configured at
the second tier.

