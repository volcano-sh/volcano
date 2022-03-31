# Volcano Resource Reservation For Queue

@[qiankunli](https://github.com/qiankunli); Oct 11rd, 2021

## Motivation

In my case, we use volcano to schedule training job(tfjob/pytorchjob/vcjob) in k8s cluster, there are many groups such as ad/recommend/tts, a queue represents a group.
In order to ensure the full utilization of resources, we generally do not configure `queue.capability`. but this will cause a queue to running out all resources, and make the new job of other queue unable to execute.
so we want to reserve some resources for a queue, so that any new job in the queue can be submitted immediately.

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

In order to support guarantee mechanism, there are two scenarios to consider
1. support `spec.guarantee` during scheduling
2. create a new queue whose `spec.guarantee` is not nil or an existed queue's `spec.guarantee` becomes bigger

### support `spec.guarantee` during scheduling

if there are three queues and 30 GPUs in cluster.

|queue/attr|guarantee GPUs|capability GPUs|realCapability GPUs|
|---|---|---|---|
|queue1|5|nil|30|
|queue2|nil|nil|25|
|queue3|nil|10|10|


```go
// /volcano/pkg/scheduler/plugins/proportion/proportion.go
type queueAttr struct {
	queueID api.QueueID
	name    string
	
	deserved  *api.Resource
	allocated *api.Resource
	request   *api.Resource
	// inqueue represents the resource request of the inqueue job
	inqueue    *api.Resource
	capability *api.Resource
	realCapability *api.Resource
	guarantee      *api.Resource
}
```

on each schedule cycle, proportion plugin will calculate `queueAttr.deserved` for a queue which means how many resources the queue can use. when consider a new task, 
if `queueAttr.deserved` is bigger than `queueAttr.allocated`, the new task can be scheduled.
1. `queueAttr.deserved` must be bigger than `queueAttr.guarantee`
2. if `queueAttr.guarantee` is not nil(like queue1), it means the 5 GPUs only can be used by queue1 even there is no job running in queue1. we use `queueAttr.realCapability` to represent the upper limit resources that a queue can use. 
   1. if `queueAttr.capability` is nil(like queue2), `realCapability = total resources - sum(other-queue.guarantee)`
   2. if `queueAttr.capability` is not nil(like queue3), `realCapability = min(capability,total resources - sum(other-queue.guarantee))`
3. replace `queueAttr.capability` with `queueAttr.realCapability` everywhere

After doing this, a queue owns the resources which is bigger than `queueAttr.guarantee` and less than `queueAttr.realCapability`

### create a new queue whose `spec.guarantee` is not nil

if there are three queues and 30 GPUs in cluster, there are many task in queue1/queue2/queue3 and running out the 30 GPUs,

|queue/attr|weight|deserved GPUs|guarantee GPUs|capability GPUs|realCapability GPUs|
|---|---|---|---|---|---|
|queue1|1|10|5|nil|30|
|queue2|1|10|nil|nil|25|
|queue3|1|10|nil|10|10|

then we create queue4 and submit a new job(request 2GPUs) in queue4

|queue/attr|weight|deserved GPUs|guarantee GPUs|capability GPUs|realCapability GPUs|
|---|---|---|---|---|---|
|queue1|1|6|5|nil|20|
|queue2|1|6|nil|nil|15|
|queue3|1|6|nil|10|10|
|queue4|2|12|10|nil|25|

1. the overcommit plugin will deny the new job in queue4 because there is no free GPUs in cluster. so,we should change the logic, if `job.request < queue4.guarantee`, the job can be `Inqueue` whether there are free GPUs or not.
2. we should enable the reclaim action, so that volcano can reclaim the task in overused queue

## Usage
Configure guarantee for queue
```yaml
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
Enable reclaim action for scheduler.
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue,allocate,reclaim,backfill"
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




