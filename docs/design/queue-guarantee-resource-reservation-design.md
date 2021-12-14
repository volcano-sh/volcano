# Volcano Resource Reservation For Queue

@[qiankunli](https://github.com/qiankunli); Oct 11rd, 2021

## Motivation
As [issue 1101](https://github.com/volcano-sh/volcano/issues/1101) mentioned, Volcano should support resource reservation
for specified queue. Requirement detail as follows:
* Support reserving specified resources for specified queue
* We only Consider non-preemption reservation.
* Support enable and disable resource reservation for specified queue dynamically without restarting Volcano.
* Support hard reservation resource specified and percentage reservation resource specified.

@[Thor-wl](https://github.com/Thor-wl) already provide a design doc [Volcano Resource Reservation For Queue](https://github.com/volcano-sh/volcano/blob/master/docs/design/queue-resource-reservation-design.md)
I do not implement all features above, supported feature are as follows:

* Support reserving specified resources for specified queue
* We only Consider non-preemption reservation.
* Support enable and disable resource reservation for specified queue dynamically without restarting Volcano.
* Support hard reservation resource specified

## Consideration
### Resource Request
* The reserved resource cannot be more than the total resource amount of cluster at all dimensions.
* If `capability` is set in a queue, the reserved resource must be no more than it at all dimensions.

### Safety
* Malicious application for large amount of resource reservation will cause jobs in other queue to block.

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
  guarantee:             // reservation key word
    resource:            // specified reserving resource
      cpu: 2c
      memory: 4G
```

`guarantee.resource` list of reserving resource categories and amount.

## Implementation

保证 guarantee 机制，包含两种场景
1. queue 配置没有变化时 保证guarantee 机制
2. 新增一个带guarantee queue 或某个queue 的guarantee 变大时保证guarantee 机制

### queue guarantee 配置没有变化时 保证guarantee 机制

每一次开始调度之前，volcano 会为每个queue 计算好deserved 资源（代表了queue 本轮调度可以使用的资源），queue 中的task 要运行时，如果deserved > allocated, task 即可运行。为保证 guarantee 机制

1. 任何时候，queue 的deserved 应大于 queue.guarantee。
2. 如果有一个queue 设置了guarantee，相当于剩下的所有的queue 都设置了一个上限值，我们称之为realCapacity
   a. 如果一个queue 未设置capacity，`realCapacity = 资源总量 - 其它queue的guarantee 之和`
   b. 如果一个queue 设置了capacity，相当于每个queue 的`realCapacity = min(capacity, 资源总量 - 其它queue的guarantee 之和)`。
3. 应修改所有根据 queue.Queue.Spec.Capability 直接或间接做判断的逻辑。
   a. job enqueue 逻辑，具体的说是 proportion jobEnqueueableFns 实现，job 申请资源不能大于 queue.realCapacity
   b. proportion.OnSessionOpen 计算queue 的 capacity 的逻辑
   c. proportion.OnSessionOpen 中根据 queue 的capacity/request 等 计算deserved 的逻辑

总结一下

1. 给 deserved 赋一个大于guarantee 的值
2. 新增一个realCapacity 作为上限值来代替 之前capacity 判断。
   最终确保每个queue 使用的资源不会越过 `[guarantee,realCapacity]`  区间。 每个queue.deserved 和为total 资源，每个queue.deserved > guarantee，比如queue1 的guarantee 被queue1 的deserved 占住了，就不会被queue2 使用了。

###queue  guarantee 变大时保证guarantee 机制

假设已经有queue1到queue5，新增queue6

当新增包含guarantee queue 的时候，可能集群的 剩余可用资源不够 这个guarantee
1. 硬性保证 guarantee 语义，evict task 释放多占用的资源 直到空出guarantee，哪怕queue6 没有job
2. 软性保证 guarantee 语义，不为queue6 安排guarantee资源，但对queue6 新增的job只要没超过 guarantee 就保证一定会部署

先按软性语义做

1. 只是提交queue6，不提交job
   a. 此时queue6 没有job，已有代码不会为这个queue6 计算 deserved
   b. queue6 的guarantee 会参与 已有queue 的deserved 计算，导致已有queue 的 deserved 可能会变小，realCapacity 可能变小，queue 可能变成overused 状态。
2. 给queue6 提交一个job（queue.guarantee 内）有啥影响？主要是对已经running 的job 的影响
   a. 集群剩余资源本来就够运行job，影响不大
   b. 集群剩余资源不够运行job

queue6提交job，集群剩余资源不够运行job
1. 被overcommit plugin 拦截。job enqueue时，如果集群资源不够运行job 会被overcommit 拦截。此时要更改 overcommit 逻辑，只要job.req < queue6.guarantee，使pg 从pending 进入enqueue 状态，
2. 之后queue6 job 的task 处于pending 状态，触发reclaim 工作，找到overused queue 删除task（假设删除的是queue1 的task）
3. 删除queue1 task 可能会导致queue1 的某个job 从running 变为 饥饿状态(readyTaskNum < minAvailable)，且queue1 处于overused 状态。 这类job 会浪费资源
4. 为此需要清理 overused 状态下 饥饿 的job，这段逻辑
   a. 可以放在reclaim action下
   b. 可以专门新写一个action

这里的关键是
1. 新queue guarantee 内的job要能提交的上去：修改overcommit ，确保新增queue6 的job（申请资源小于queue6.guarantee） 可以提交
2. 提交的job 一定可以运行：集群资源富余就算了，集群资源不够，那么要牺牲overused queue running task 让出资源，这需要配置reclaim action
3. 由此带来一个 “overused 状态下 饥饿 的job 有running task” 浪费资源的问题， 需要新增一个清理逻辑