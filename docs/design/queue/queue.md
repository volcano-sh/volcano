# Queue

[@k82cn](http://github.com/k82cn); April 17, 2019

## Motivation

`Queue` was introduced in [kube-batch](http://github.com/kubernetes-sigs/kube-batch) long time ago as an internal feature, which makes all jobs are submitted to the same queue, named `default`. As more and more users would like to share resources with each other by queue, this proposal is going to cover primary features of queue achieve that.

## Function Specification

The queue is cluster level, so the user from different namespaces can share resource within a `Queue`. The following section defines the api of queue.

### API

```go
type Queue struct {
    metav1.TypeMeta `json:",inline"`

    metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

    // Specification of the desired behavior of a queue
    // +optional
    Spec QueueSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

    // Current status of Queue
    // +optional
    Status QueueStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

type QueueSpec struct {
    // The weight of queue to share the resources with each other.
    Weight int32 `json:"weight,omitempty" protobuf:"bytes,1,opt,name=weight"`
}

type QueueStatus struct {
    // The number of job in Unknown status
    Unknown int32 `json:"running,omitempty" protobuf:"bytes,1,opt,name=running"`
    // The number of job in Running status
    Running int32 `json:"running,omitempty" protobuf:"bytes,2,opt,name=running"`
    // The number of job in Pending status
    Pending int32 `json:"pending,omitempty" protobuf:"bytes,3,opt,name=pending"`
    // The number of job in Completed status
    Completed int32 `json:"completed,omitempty" protobuf:"bytes,4,opt,name=completed"`
    // The number of job in Failed status
    Failed int32 `json:"failed,omitempty" protobuf:"bytes,5,opt,name=failed"`
    // The number of job in Aborted status
    Aborted int32 `json:"aborted,omitempty" protobuf:"bytes,6,opt,name=aborted"`
}
```

### QueueController

The `QueueController` will manage the lifecycle of queue:

1. Watching `PodGroup`/`Job` for status
2. If `Queue` was deleted, also delete all related `PodGroup`/`Job` in the queue

### Admission Controller

The admission controller will check `PodGroup`/`Job` 's queue when creation:

1. if the queue does not exist, the creation will be rejected
2. if the queue is releasing, the creation will be also rejected

### Feature Interaction

#### Customized Job/PodGroup

If the `PodGroup` is created by customized controller, the `QueueController` will count those `PodGroup` into `Unknown` status; because `PodGroup` focus on scheduling specification which did not include customized job's status.

#### cli

Command line is also enhanced for operator engineers. Three sub-commands are introduced as follow:

__create__:

`create` command is used to create a queue with weight; for example, the following command will create a queue named `myqueue` with weight 10.

```shell
$ vcctl queue create --name myqueue --weight 10
```

__view__:

`view` command is used to show the detail of a queue, e.g. creation time; the following command will show the detail of queue `myqueue`

```shell
$ vcctl queue view myqueue
```

__list__:

`list` command is used to show all available queues to current user

```shell
$ vcctl queue list
Name      Weight  Total  Pending  Running ...
myqueue   10      10     5        5
```

#### Scheduler

* Proportion plugin:

  Proportion plugin is used to share resource between `Queue`s by weight. The deserved resource of a queue is `(weight/total-weight) * total-resource`. When allocating resources, it will not allocate resource more than its deserved resources.

* Reclaim action:

  `reclaim` action will go through all queues to reclaim others by `ReclaimableFn`'s return value; the time complexity is `O(n^2)`. In `ReclaimableFn`, both `proportion` and `gang` will take effect: 1. `proportion` makes sure the queue will not be under-used after reclaim, 2. `gang` makes sure the job will not be reclaimed if its `minAvailable` > 1.

* Backfill action:

  When `allocate` action assign resources to each queue, there's a case that ([kube-batch#492](<https://github.com/kubernetes-sigs/kube-batch/issues/492>)) the resources maybe unnecessary idle because of `proportion` plugin: there are one pending job in two queue each, and the deserved resources of each queue can not meet the requirement of their jobs. In such case, `backfill` action will ignore deserved guarantee of queue to fill idle resources as much as possible. This introduces another potential case that the coming smaller job is blocked; this case will be handle by reserved resources of each queue in other project.
