# Queue State Management

[@sivanzcw](https://github.com/sivanzcw); Oct 17, 2019

## Table of Contents

- [Queue State Management](#queue-state-management)
  * [Table of Contents](#table-of-contents)
  * [Motivation](#motivation)
  * [Function Detail](#function-detail)
    + [Data Structure](#data-structure)
    + [Queue State](#queue-state)
    + [Queue Lifecycle Management](#queue-lifecycle-management)
    + [Queue Status Refreshment](#queue-status-refreshment)
    + [Queue Placement Restriction](#queue-placement-restriction)
    + [Queue State on The Scheduling Process](#queue-state-on-the-scheduling-process)
    + [Queue State on `vcctl`](#queue-state-on--vcctl-)

## Motivation

The queue is an object of resource management in the cluster and the cornerstone of resource scheduling, which is
closely related to the allocation of resources and the scheduling of tasks. The resources under the cluster are
allocated according to the `weight` ratio of the queue. The configuration of queue guarantees the number of cluster
resources that tasks can use under the queue and limits the maximum resources that can be used. A single user or
user group is correspond to one or more queues, which is assigned and determined by the administrator. When queues
splitting cluster resources, single queue obtains the resource guarantees and quotas for using resources, so that uses
or user groups under the queue have opportunity to use cluster resources, Simultaneously due to the resource limitation
of queue, the ability of users or user groups to user cluster resources is limited to prevent cluster from being
overwhelmed by a single user to deliver a large number or tasks, thereby ensuring the `multi-tenancy` feature of
scheduling. When task is delivered, it will be placed to a specific queue and pod scheduling will by affected by queue
priority and queue resource status. It is worth mentioning that the resource allocation of queue and limitation of
queue resource can be dynamically adjusted. The queue can flexibly acquire remaining resources under cluster if there
are idle resources, when a queue is busy, and there are idle resources under the cluster, the queue may break the
original resource limit and try to occupy the remaining cluster resources.

Based on the above description, it can be found that queue is a crucial object in the process of resource scheduling.
There should have a complete guarantee mechanism to ensure the stability of queue without losing the flexibility of
queue. Firstly, the queue should not be deleted arbitrarily, since if the queue is deleted, the unscheduled tasks in
the queue will not be scheduled normally and the resources occupied by running tasks in the queue will not be normally
counted. However, considering the flexibility of resource control, queue should not be forbidden to delete. In addition,
considering the decisive role of queue in resource management, the administrator will control which user or user group
can use cluster resources by controlling queue which also requires queue to provide corresponding capabilities.

Therefore, we need to provide `State Management` capabilities for queue. Add the state configuration for queue and
adjust capabilities of queue by judging the state of queue, thereby achieving the management of queue lifecycle and
scheduling of tasks under the queue.

## Function Detail

### Data Structure

Add `state` to `properties` in `spec` of CRD `queues.scheduling.sigs.dev`. The `state` of queue controller the status
of queue.

```go
spec:
  properties:
    ...

    state:
      type: string

    ...
```

Add `state` to `properties` in `status` of CRD `queues.scheduling.sigs.dev`. The `state` of queue display the status of
current queue.

```go
status:
  properties:
    ...

    state:
      type: string

    ...
```
### Queue State

Valid queue state includes:

* `Open`, indicates that the queue is available, the queue receives new task delivery
* `Closed`, indicated that the queue is unavailable, the queue will wait for the subordinate tasks to gracefully exit,
which does not mean that the system will actively delete tasks under the queue. However, the queue does not receive new
task delivery
* `Closing`, is a intermediate state between `Open` and `Closed`. When the state of queue is `Open` and there
are tasks running or waiting to be scheduled under the queue. At this time, we try to change the state of queue to
`Closed`. The state of queue will changes to `Closing` firstly and then changes to `Closed` when all the tasks under
the queue exist.

The ability of queue corresponding to queue state as show in the following table:

| state     | default | can be set | receive delivery | can be deleted | can be scheduled | deserved resources |
| :-------: | :-----: | :--------: | :--------------: | :------------: |:---------------: | :----------------: |
| `Open`    | Y       | Y          | Y                | N              | Y                | Normal             |
| `Closed`  | N       | Y          | N                | Y              | Y                | Normal             |
| `Closing` | N       | N          | N                | N              | Y                | Normal             |

* If the state of queue is not specified during the creating of queue, the queue will use default state `Open`
* When creating a new queue, the user can only specify `Open` or `Closed` state for queue
* Only the queue with `Open` state accept new task delivery. the task will be rejected when it is posted to the queue
with `Closed` or `Closing` state
* Only the queue with `Closed` state can be deleted

### Queue Lifecycle Management

In the lifecycle management of queue, we need to guarantee the following three points:

* When creating a new queue, if the user does not specify a state for queue, we need to specify default `Open` state
for it, If the user specifies a state for queue, the specified state must be a valid value, valid values are `Open`
and `Closed`.
* When upgrading the queue, if state of queue changed, the specified state value must be valid.
* when deleting the queue, only queue with `Closed` status can be deleted successfully. The `status` here is the `state`
under the status of queue, not the `state` under the `spec` of queue.
* `default` queue can not be deleted

Add `validatingwebhookconfiguration` for queue validation during creating, updating or deleting of queue.

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: {{ .Release.Name }}-validate-queue
webhooks:
  - clientConfig:
      caBundle: ""
      service:
        name: {{ .Release.Name }}-admission-service
        namespace: {{ .Release.Namespace }}
        path: /queues
    failurePolicy: Fail
    name: validatequeue.volcano.sh
    namespaceSelector: {}
    rules:
      - apiGroups:
          - "scheduling.sigs.dev"
        apiVersions:
          - "v1alpha2"
        operations:
          - CREATE
          - UPDATE
        resources:
          - queues
```

Add implementation function `AdmitQueues`

```go
func AdmitQueues(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	...
	queue, err := DecodeQueue(ar.Request.Object, ar.Request.Resource)
	reviewResponse := v1beta1.AdmissionResponse{}
	validateQueue(queue, &reviewResponse)
	...
}
```

The above function will complete the following verification:

* During creating or upgrading queue, verify the validity of the queue state
* During deleting queue, check if queue can be deleted

We need another `webhook` to set default state value for queue during queue creating, add `mutatingwebhookconfiguration`
and `MutateQueues` function

```yaml
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: {{ .Release.Name }}-mutate-queue
webhooks:
  - clientConfig:
      caBundle: ""
      service:
        name: {{ .Release.Name }}-admission-service
        namespace: {{ .Release.Namespace }}
        path: /mutating-queues
    failurePolicy: Fail
    name: mutatequeue.volcano.sh
    namespaceSelector: {}
    rules:
      - apiGroups:
          - "scheduling.sigs.dev"
        apiVersions:
          - "v1alpha2"
        operations:
          - CREATE
        resources:
          - queues
```

```go
func MutateQueues(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	...
	queue, err := DecodeQueue(ar.Request.Object, ar.Request.Resource)
	reviewResponse := v1beta1.AdmissionResponse{}
	createPatch(queue)
	...
}
```

### Queue Status Refreshment

When refreshing the status of queue, the `state` value under `spec.properties` and podgroup condition under the queue will be
considered:

* If the `state` value is empty, the status of queue will be set as `Open`
* If the `state` value is `Open`, then the status of queue will also be `Open`
* If the `state` value is `Closed`, then we need to further consider whether there is a podgroup under the queue. if
there is a podgroup under the queue, the status of the queue will be set as `Closing`, while if there is no podgroup
under the queue, the status of queue will be set as `Closed`.

### Queue Placement Restriction

When creating job, we need to verify the status of queue specified by the job:

* Allow job to be create, if the job does not specify a queue name
* If the job specifies a queue name and the status of the queue is `Open`, the job is allowed to create
* If the status of queue is not `Open`, the job creation request will be rejected.

### Queue State on The Scheduling Process

The above three states of queue have no effect on the existing scheduling process, for there is no pod under queue with
`Closed` state, while pods under queues with `Open` or `Closing` state should be scheduled normally.

### Queue State on `vcctl`

We need to add support for `queue state management` in `vcctl`, mainly including the following changes:

* Support for passing state of queue when creating queue
* When getting queue detail or queue list, we need to display the status of the queue
* Provide update function of queue, the function supports updating the `weight` or `state` of queue
* Provide delete function of queue
* Add queue operation interface, add `queue open` `queue close` `queue update` support
