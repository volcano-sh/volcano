# Hierarchy Queue

## Motivation

The hierarchy queue is derived from the queue management mechanism of YARN, refer to: https://blog.cloudera.com/yarn-capacity-scheduler/.

It is designed with multiple levels of resource constraints to better support fairshare scheduling in Volcano.

## Function Specification

The hierarchy queue has two important concepts: parent queues and leaf queues.

1. Parent queues do not themselves accept any application submissions directly. And they can contain more parent queues or leaf queues.
2. Leaf queues are the queues that live under a parent queue and accept applications. Leaf queues do not have any child queues.

### API

```go
type QueueSpec struct {
    ...
    // ParentQueue indicates who its parent queue is
    ParentQueue string `json:"parentQueue,omitempty" protobuf:"bytes,4,opt,name=parentQueue"`

    // ChildQueues indicates which child queues it has
    ChildQueues []string `json:"childQueues,omitempty" protobuf:"bytes,5,opt,name=childQueues"`
}

type QueueStatus struct {
    ...
    // QueuePath indicates the top down named path to the queue
    QueuePath string `json:"queuePath,omitempty" protobuf:"bytes,6,opt,name=queuePath"`

    // Role indicates whether the queue is parent queue or child queue 
    Role string `json:"role,omitempty" protobuf:"bytes,7,opt,name=role"`
}
```

### Features

1. `root` is a top-level parent queue which represents the cluster itself. All other queues are direct or indirect subqueues of `root`.
2. Initially, we only have one queue called `default` which belongs to `root`.
3. All queue names are unique.
4. `ParentQueue` is optional. If its value is null when you create a queue, it defaults to `root` as its parent queue.
5. `ChildQueues` is optional. If its value is null when you create a queue, the queue will be a leaf queue.
6. If there are four queues other than `root` in the cluster: `root.default` (weight is 1), `root.A` (weight is 1), `root.A.A1` (weight is 2), `root.A.A2` (weight is 3). The resources available to leaf queues are: 50% in default, 20% in A1, 30% in A2.
7. The total number of `PodGroup`/`Job` owned by child queues is the number of `PodGroup`/`Job` owned by the parent queue.
8. When a user submits a `Job` to a parent queue, it will be rejected.
9. If you do not specify a queue when you submit a `Job`, it will be submitted to the queue `root.default` by default.

### Cli

1. Because child queue's resource allocation depends on its parent queue's. We need `QueuePath` to help us understand the hierarchy of each queue so that the administrator can clearly assign weights to queues.
2. We need to know who leaf queue is, because just leaf queue can accept applications.

So when we use `vqueues` to list all queues of the cluster, we can just replace `name` with `QueuePath` and show the `Role` as well. Look at the following example.

```shell
$ vqueues
Name             Weight  State  Inqueue  Pending  Running  Unknown  Role
root                  1   Open        6        0        6        0  Parent
root.default          5   Open        3        0        3        0  Leaf
root.dev              5   Open        3        0        3        0  Parent
root.dev.test1        1   Open        1        0        1        0  Leaf
root.dev.test2        2   Open        2        0        2        0  Leaf
```