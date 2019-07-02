# Bucket: Task Affinity/Anti-Affinity within Job

[@k82cn](http://github.com/k82cn); April 2, 2019

## Motivation

When high performance workload running on Kubernetes, the performance is greatly affected by pod affinity/anti-affinity, e.g.
ps/worker mode of Tensorflow. The default scheduler support pod affinity/anti-affinity, but there're some weaknesses:

- The default scheduler will check PodAffinity/PodAntiAffinity against all running pods which impacts schedule performance/throughput
- The default scheduler only check current pod, the coming pods may not have enough resources to meet affinity

To improve the performance of scheduler and workload, a new algorithm is introduced, named as Bucket. This doc describes the function
specification of the new algorithm.

## Function Specification

### Scope

* In Scope

  - Provide a way for user to define task affinity/anti-affinity within a job
  - Support selecting a better node for the tasks based on affinity/anti-affinity

* Out Of Scope

  - Do not support "Network Topology", e.g. switches  
  
### API

A new plugin is introduced to avoid Job API changes, named as `task-topology`. The arguments of the plugin are defined
as follow: 

```yaml
plugins
  task-topology: ["--affinity", "[[ps-task, worker-task]]", "--anti-affinity", "[[ps-task]]"]
```

**affinity:**

The `--affinity` parameter defines which tasks/pods should be placed on the same node. It's a json two-dimension array:

- First dimension defines which tasks affinity, e.g. `[ps-task, worker-task]`; it only defines the affinity between tasks.
  The array with only one tasks defines affinity to itself.  
- Second dimension defines multiple affinity configuration, e.g. `[[ps, worker], [worker]]` means `ps` and `worker` affinity
  to each other, and `worker` affinity to itself, but not self-affinity preference for `ps` 

**anti-affinity:**

The `--anti-affinity` parameter defines which tasks/pods should not be placed on the same node. It's also a json two-dimension array:

- First dimension defines which tasks anti-affinity, e.g. `[ps-task, worker-task]`; it only defines the anti-affinity between tasks.
  The array with only one tasks defines anti-affinity to itself.  
- Second dimension defines multiple anti-affinity configuration, e.g. `[[ps, worker], [ps]]` means `ps` and `worker` anti-affinity
  to each other, and `ps` anti-affinity to itself, but not self-affinity preference for `worker` 

If there's any conflict, the pod will not be placed in any bucket.

### Function Details

#### Job controller

In `OnJobAdd` callback, the `task-topology` plugin will encode the arguments of `task-topology` into a json string and
put them into `Job`'s annotation if `volcano.sh/task-topology` not found.

When job controller create `PodGroup` for the job, all its annotations and labels will be passed on to the `PodGroup`. 

#### Bucket Plugin

##### OnSessionOpen

In `OnSessionOpen` of `bucket` plugin, it'll get `task-topology` from `PodGroup`'s annotation and build bucket accordingly.
The buckets of job is only maintained in `bucket` plugin, similar with other plugins. 

In `bucket` plugin, it'll register a `nodeOrderFn` and `taskOrderFn` to find a better node to place the bucket:
 
 - `taskOrderFn` will sort all tasks by buckets, e.g. ps1, worker1, worker2, ps2, worker3, worker4, the pods without bucket are
   placed at the end of task queue.
 - in `nodeOrderFn`, it'll calculate a score for the node as follow:
   - if node has enough resource to place bucket, return `bucket / node`
   - if node does not have enough resource to place the bucket, keeps removing items from the bucket until the node can place the bucket;
     and then return `bucket / node`. When removing items, balance Pod of `taskSpec`s in the bucket.

##### OnSessionClose

Remove the `bucket` data in the plugin

## Feature Interaction

### Gang Scheduling

If the `minAvailable` is smaller than the total pods, the other Pods may consume the resources that for coming Pods.
So `bucket` is better to work together with `gang` plugin (`minAvailable == total`) together to make sure a better placement. 

### Priority

`priority` plugin also support `taskOrder` based on Pod's priority; if tasks are ordered by `priority` plugin, `bucket` will
still place Pod by balancing between different buckets. 

## Reference

1. [Optimus: An Efficient Dynamic Resource Scheduler for Deep Learning Clusters](https://dl.acm.org/citation.cfm?id=3190517)
1. [Pod Affinity and anti-affinity](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#affinity-and-anti-affinity)
