# Gate-Controlled Scheduling for Cluster Autoscalers Compatibility

## Table of Contents

- [Motivation](#motivation)
- [Proposal](#proposal)
  - [Goals](#goals)
  - [Non-Goals](#non-goals)
  - [High-Level Implementation](#high-level-implementation)
    - [API Constant Definition](#api-constant-definition)
    - [Changes to the Volcano MutatingAdmissionWebhook](#changes-to-the-volcano-mutatingadmissionwebhook)
    - [Changes to the Volcano Scheduler](#changes-to-the-volcano-scheduler)
      - [Queue Admission Checks](#queue-admission-checks)
      - [Queue Allocation and Gate Removal](#queue-allocation-and-gate-removal)
      - [Queue Capacity Accounting for Ungated Pods](#queue-capacity-accounting-for-ungated-pods)
        - [Dynamic Reserved Calculation](#dynamic-reserved-calculation)
        - [Cache Invalidation](#cache-invalidation)
      - [Gate Removal During Successful Bind](#gate-removal-during-successful-bind)
- [Conclusions](#conclusions)
- [Related Issues](#related-issues)

## Motivation

Cluster autoscalers such as [Cluster Autoscaler (CA)](https://github.com/kubernetes/autoscaler/tree/master) or
[Karpenter](https://github.com/kubernetes-sigs/karpenter) is present in almost all Kubernetes environments to
dynamically adjust node capacity based on scheduler signals. Both systems monitor pod scheduling conditions,
specifically looking for pods that match:

```yaml
type: PodScheduled
status: "False"
reason: Unschedulable
```

These pods, usually marked by the kube-scheduler, are interpreted as evidence of insufficient cluster capacity,
triggering scale-up simulations to eventually add new nodes.

Please refer to the links below regarding detecting `Unschedulable` Pods in Cluster Autoscaler and Karpenter:

> - [CA (v1.34.1): listers.go – func isUnschedulable(pod \*apiv1.Pod) bool](https://github.com/kubernetes/autoscaler/blob/cluster-autoscaler-1.34.1/cluster-autoscaler/utils/kubernetes/listers.go#L161-L170)
>   (also check
>   [FAQ.md](https://github.com/kubernetes/autoscaler/blob/cluster-autoscaler-release-1.34/cluster-autoscaler/FAQ.md#how-does-scale-up-work))
> - [Karpenter (v1.8.0): scheduling.go – func FailedToSchedule(pod \*corev1.Pod) bool](https://github.com/kubernetes-sigs/karpenter/blob/v1.8.0/pkg/utils/pod/scheduling.go#L116-L129)

This mechanism works as intended with the default `kube-scheduler`, but can cause unintended behavior when used with
Volcano. Volcano's current implementation marks pods as `Unschedulable` for any allocation failure, regardless of
whether the failure is due to insufficient cluster capacity (where autoscaling is appropriate) or queue capacity limits
(where autoscaling is not needed). This causes autoscalers to incorrectly trigger scale-up operations when pods are
simply waiting for queue admission (refer to
[Volcano's issue \#4710](https://github.com/volcano-sh/volcano/issues/4710)).

### How Volcano Currently Sets Unschedulable

> Note: Every mention of Volcano in this document refers to the latest
> [v1.13.0 release](https://github.com/volcano-sh/volcano/releases/tag/v1.13.0).

Volcano sets the `Unschedulable` condition on pods through its cache event recording mechanism in
[`pkg/scheduler/cache/cache.go`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/cache/cache.go). After
each scheduling cycle, the
[`RecordJobStatusEvent`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/cache/cache.go#L1515) function
examines tasks (pods) that have not been allocated and updates their `PodScheduled` condition with `status=False` and
`reason=Unschedulable`.

This happens for **any** task that fails allocation, regardless of whether the failure is due to insufficient cluster
resources (legitimately triggering a scale-up) or queue capacity limits (but also triggering a scale-up). Autoscalers
cannot distinguish between these scenarios, as they only see the `Unschedulable` condition.

## Proposal

This proposal leverages the use of Kubernetes `schedulingGates` (as defined in
[KEP-3521](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/3521-pod-scheduling-readiness#summary))
to control when a Pod is considered ready for scheduling. The core idea is to delay setting the `Unschedulable`
condition until the Queue has enough capacity to accommodate the Pod.

### Goals

- Prevent downstream integrators (like CA, Karpenter) from triggering unnecessary scale-ups caused by Pods marked as
  `Unschedulable`.
- Provide an opt-in mechanism, controlled via Pod annotation.
- Preserve existing Volcano scheduling semantics.
- Non-blocking implementation (avoid scheduler performance degradation).

### Non-Goals

- Altering the behavior or internal logic of Cluster Autoscaler or Karpenter.
- Rejecting or blocking Pod creation at admission time.
- Introducing custom autoscaler integrations or external controllers.

### High-Level Implementation

In this proposal, changes will be required to both the **admission process** and also the **scheduler routines**, as the
latter is responsible for applying the logic for actions and plugins.

#### API Constant Definition

The following annotation key should be defined as a constant in the
[Scheduling v1beta1 API package](https://github.com/volcano-sh/apis/blob/v1.13.0/pkg/apis/scheduling/v1beta1/labels.go#L17)
to be used throughout the implementation:

```go
const QueueAllocationGateKey = GroupName + "/queue-allocation-gate"
```

#### Changes to the Volcano `MutatingAdmissionWebhook`

Volcano's `MutatingAdmissionWebhook` must be extended to detect Pods annotated with
`schedulingv1beta1.QueueAllocationGateKey="true"`. At **creation time**, the webhook should patch the Pod's
`spec.schedulingGates` (through the
[`createPatch`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/webhooks/admission/pods/mutate/mutate_pod.go#L103)
function) by adding a new `schedulingGate` entry using the same constant:

```go
// Existing createPatch function needs to include the new optional patch
func createPatch(pod *v1.Pod) ([]byte, error) {

    // ...

    // Add scheduling gates if opted-in
    patchGates := patchSchedulingGates(pod)
    if patchGates != nil {
        patch = append(patch, *patchGates)
    }

    // ...
}

// patchSchedulingGates adds a scheduling gate for Volcano-managed pods that opted-in
func patchSchedulingGates(pod *v1.Pod) *patchOperation {

    // Check if opt-in annotation is present
    if pod.Annotations == nil || pod.Annotations[schedulingv1beta1.QueueAllocationGateKey] != "true" {
        return nil
    }

    // ...

    return &patchOperation{
        Op:    "add",
        Path:  "/spec/schedulingGates/-",
        Value: append(pod.Spec.SchedulingGates, v1.PodSchedulingGate{Name: schedulingv1beta1.QueueAllocationGateKey}),
    }
}
```

**`schedulingGates` Field Immutability**

Kubernetes `schedulingGates` can only be removed and **not added after pod creation**
([see PodSpec documentation](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#scheduling)).
The gates will then be gradually removed in the scheduler side, since the latter contains the actions and plugins that
determine if the Pod can be allocated to a queue and eventually a node:

- **During bind**: For successfully allocated pods, gates **should be** removed atomically with binding.
- **For lack of cluster capacity**: When queue has capacity but no node can fit the pod, gates are removed to signal the
  `Unschedulable` condition to autoscalers and trigger a scale-up.

#### Changes to the Volcano Scheduler

Volcano already adopted the concept of `schedulingGates` through the **Pod Scheduling Readiness** design document
([pod-scheduling-readiness.md](https://github.com/volcano-sh/volcano/blob/master/docs/design/pod-scheduling-readiness.md)),
which skips tasks of a job whose Pods are scheduling gated, ensuring that a scheduling gated pod will not be bound to a
node. In this case, the
[allocate](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/allocate/allocate.go#L159),
[backfill](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/backfill/backfill.go#L139-L141),
[reclaim](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/reclaim/reclaim.go#L85-L87), and
[preempt](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/preempt/preempt.go#L223-L226) actions
already skip Pods that
[have at least one gate](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/api/job_info.go#L246-L252).

##### Queue Admission Checks

The [`GetSchGatedPodResources()`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/api/job_info.go#L543)
method should be enhanced to exclude pods with only the new proposed Volcano scheduling gate from deduction. This
ensures these pods are counted in inqueue resources, making them **eligible for queue admission checks**:

```go
func (ji *JobInfo) GetSchGatedPodResources() *Resource {
    res := EmptyResource()
    for _, task := range ji.Tasks {
        if task.SchGated {
            // Exclude tasks that are only Volcano scheduling gated
            // These should be counted in inqueue resources, not deducted
            if hasOnlyVolcanoSchedulingGate(task.Pod) {
                continue
            }
            res.Add(task.Resreq)
        }
    }
    return res
}
```

This change allows
[`DeductSchGatedResources()`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/api/job_info.go#L557) to
treat Volcano-gated pods differently from other scheduling-gated pods, ensuring they remain visible to the enqueue
action while still being prevented from allocation until the gate is removed.

##### Queue Allocation and Gate Removal

Each time the `Allocate` action is executed,
[`allocateResourcesForTasks(...)`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/allocate/allocate.go#L356)
tries to allocate resources for each Task in a given Queue and
[`ssn.Allocatable(queue, task)`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/allocate/allocate.go#L365)
gives us the needed signal (by running every plugin check) for eventually removing the gate added previously by the
**MutatingAdmissionWebhook**. Gates should be removed for signaling the `Unschedulable` condition to autoscalers when no
node fits the pod, otherwise, the pod will be allocated to a node and gates are removed during the bind operation.

To avoid blocking the scheduler, gate removals for lack of cluster capacity are queued to background workers and
processed asynchronously. The following code snippet showcases the possible high-level changes to the function
[`allocateResourcesForTasks(...)`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/allocate/allocate.go#L356):

```go
// Enhance allocateResourcesForTasks with gate management
func (alloc *Action) allocateResourcesForTasks(...) {

    // ...

    for !tasks.Empty() {
        task := tasks.Pop().(*api.TaskInfo)

        // Check queue capacity (enhanced to account for reserved ungated pods)
        // See "Queue Capacity Accounting for Ungated Pods" section for details
        if !ssn.Allocatable(queue, task) {
            // ...
            continue
        }

        // Queue has capacity - mark task for gate removal during bind
        if task.SchGated && cache.HasOnlyVolcanoSchedulingGate(task.Pod) {
            task.RemoveGateDuringBind = true
        }

        // Skip tasks with external (non-Volcano) scheduling gates
        if task.SchGated && !task.RemoveGateDuringBind {
            continue
        }

        // ... predicate checks ...

        // Handle PrePredicate failure (e.g., topology constraints)
        // Remove gate so pod becomes Unschedulable - signals autoscaler
        if err := ssn.PrePredicateFn(task); err != nil {
            // ...
            alloc.schedulingGateRemoval(task, queue.UID)
            // ...
        }

        // Handle no predicate nodes (no nodes fit the pod)
        // Remove gate so pod becomes Unschedulable
        if len(predicateNodes) == 0 {
            // ...
            alloc.schedulingGateRemoval(task, queue.UID)
            // ...
        }

        // Handle no best node after prioritization
        // Remove gate so pod becomes Unschedulable
        if bestNode == nil {
            // ...
            alloc.schedulingGateRemoval(task, queue.UID)
            // ...
        }

        // Allocate task to best node
        if err := alloc.allocateResourcesForTask(stmt, task, bestNode, job); err == nil {
            // ... allocation successful ...
        }
    }

    // ...
}

// schedulingGateRemoval queues async gate removal for node-fit failures
func (alloc *Action) schedulingGateRemoval(task *api.TaskInfo, queueID api.QueueID) {
    if cache.HasOnlyVolcanoSchedulingGate(task.Pod) {
        op := schGateRemovalOperation{namespace: task.Namespace, name: task.Name}
        alloc.schGateRemovalStopCh <- op
        task.SchGated = false  // Mark as ungated in cache

        // Invalidate capacity plugin's reserved cache for this queue
        // See "Cache Invalidation" section for details
        alloc.session.InvalidateCapacityReservedCache(queueID)
    }
}
```

To support asynchronous gate removal, the `Action` struct must be **enhanced with channels and worker management
fields**:

```go
// Enhance the Action struct
type Action struct {
    session *framework.Session
    // ...

    // Async gate removal channel
    schGateRemovalStopCh chan schGateRemovalOperation
    schGateRemovalWorkersWg   sync.WaitGroup
    schGateRemovalShutdownCh  chan struct{}
}

type schGateRemovalOperation struct {
    namespace string
    name      string
}

// Background worker processes gate removal operations
func (alloc *Action) schGateRemovalWorker() {
    defer alloc.schGateRemovalWorkersWg.Done()
    for {
        select {
        case op := <-alloc.schGateRemovalStopCh:
            cache.RemoveVolcanoSchGate(kubeClient, op.namespace, op.name)
        case <-alloc.schGateRemovalShutdownCh:
            return
        }
    }
}

// Note: When starting the worker routine (e.g., in Action initialization), we should
// call alloc.schGateRemovalWorkersWg.Add(1) before launching the goroutine.
```

##### Queue Capacity Accounting for Ungated Pods

When a pod's gate is removed due to lack of cluster capacity, it becomes visible to autoscalers but remains unallocated
(waiting for matching nodes). This creates a potential race condition: between gate removal and actual allocation to a
node, other pods might consume the available queue capacity, leaving the ungated pod unable to allocate despite being
`Unschedulable`.

To prevent this, the queue capacity accounting logic must be enhanced to treat **ungated pods as "reservations" that
count toward the queue's capacity checks**. Specifically, the
[`ssn.Allocatable(queue, task)`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/framework/session_plugins.go#L325)
function (which invokes the capacity plugin) needs to account for:

1. Pods with `AllocatedStatus` (bound, binding, running) toward queue capacity _(current behavior)_.
2. **`Pending` pods that have had their gates removed** as "reserved" resources _(new behavior)_.

###### Dynamic Reserved Calculation

Rather than modifying the `allocated` attribute (which semantically represents bound resources), we add a new helper
method `queueAllocatableWithReserved` to the
[capacity plugin](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/plugins/capacity/capacity.go). This
new method extends the existing
[`queueAllocatable`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/plugins/capacity/capacity.go#L843)
function by dynamically calculating reserved resources from ungated Pending pods.

The capacity plugin's struct should be enhanced to include a cache:

```go
type capacityPlugin struct {
    // ... existing fields ...

    // Cache of reserved resources per queue (invalidated when task state changes)
    reservedCache map[api.QueueID]*api.Resource
}
```

A new method `queueAllocatableWithReserved` should be added to the capacity plugin:

```go
// queueAllocatableWithReserved checks if queue has capacity for candidate
// by dynamically calculating reserved resources (ungated Pending pods)
func (cp *capacityPlugin) queueAllocatableWithReserved(attr *queueAttr, candidate *api.TaskInfo, queue *api.QueueInfo) bool {
    // Check cache first
    reserved, cached := cp.reservedCache[queue.UID]
    if !cached {
        // Calculate reserved: ungated Pending pods with queue allocation annotation
        reserved = api.EmptyResource()
        for _, job := range cp.session.Jobs {
            if job.Queue != queue.UID {
                continue
            }
            for _, task := range job.Tasks {
                if !task.SchGated && task.Status == api.Pending &&
                    cache.HasQueueAllocationGateAnnotation(task.Pod) {
                    reserved.Add(task.Resreq)
                }
            }
        }
        // Cache the result
        cp.reservedCache[queue.UID] = reserved
    }

    // Exclude candidate from reserved if it's already counted (avoid double-counting)
    adjustedReserved := reserved.Clone()
    if !candidate.SchGated && candidate.Status == api.Pending &&
        cache.HasQueueAllocationGateAnnotation(candidate.Pod) {
        // Check if we have enough reserved resources to subtract (avoid panic)
        if candidate.Resreq.LessEqual(adjustedReserved, api.Zero) {
            // Candidate was counted in reserved, subtract it to avoid double-counting in futureUsed
            adjustedReserved.Sub(candidate.Resreq)
        }
    }

    // Check capacity: allocated + adjustedReserved + candidate <= queue capacity
    futureUsed := attr.allocated.Clone().Add(adjustedReserved).Add(candidate.Resreq)
    allocatable, _ := futureUsed.LessEqualWithDimensionAndResourcesName(attr.realCapability, candidate.Resreq)
    return allocatable
}

// InvalidateReservedCache clears the reserved cache for a specific queue
// Called when a task's SchGated state changes during a cycle
func (cp *capacityPlugin) InvalidateReservedCache(queueID api.QueueID) {
    delete(cp.reservedCache, queueID)
}
```

And the existing
[`queueAllocatable`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/plugins/capacity/capacity.go#L838)
method should be modified to call the new helper:

```go
func (cp *capacityPlugin) queueAllocatable(queue *api.QueueInfo, candidate *api.TaskInfo) bool {
    attr := cp.queueOpts[queue.UID]
    return cp.queueAllocatableWithReserved(attr, candidate, queue)
}
```

###### Cache Invalidation

The reserved resources cache must be invalidated when a task's gate is removed during a scheduling cycle. This ensures
subsequent capacity checks see the updated state and account for the newly ungated pod in their reserved calculations.

```go
func (cp *capacityPlugin) InvalidateReservedCache(queueID api.QueueID) {
    delete(cp.reservedCache, queueID)
}
```

This targeted invalidation ensures only the affected queue's cache is cleared, while other queues' cached values remain
valid for the remainder of the scheduling cycle.

##### Gate Removal During Successful Bind

Finally, the last case is when a task successfully passes **all allocation checks** (i.e.,
`ssn.Allocatable(queue, task)` returns `true` and there's a node that can fit the pod), then the gate will only be
removed during the [`Bind(...)`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/cache/cache.go#L209)
operation:

```go
func (db *DefaultBinder) Bind(...) map[schedulingapi.TaskID]string {
    // ...

    for _, task := range tasks {
        if task.RemoveGateDuringBind {
            if err := removeVolcanoSchGateFromPodByName(kubeClient, task.Namespace, task.Name); err != nil {
                // ...
            }
        }

        // Standard bind
        if err := db.kubeclient.CoreV1().Pods(p.Namespace).Bind(context.TODO(), &v1.Binding{...}); err != nil {
            // ...
        }
        // ...
    }
    // ...
}
```

## Conclusions

Scheduling Gates are a Kubernetes feature that allows external controllers to delay pod scheduling until specific
conditions are met. The proposed design leverages this mechanism to defer scheduling until the queue has sufficient
capacity, preventing pods from appearing as `Unschedulable` when they're simply waiting for queue admission and falsely
trigger cluster scale-ups.

This implementation requires pods to opt in via the `schedulingv1beta1.QueueAllocationGateKey: "true"` annotation
(defined as `volcano.sh/queue-allocation-gate`), making it a conservative approach ensuring backward compatibility
whilst allowing users to adopt the feature gradually. Future iterations **could enable this behavior by default** once
the feature maturity is validated in production environments.

## Related Issues

The following issues are related to the matters discussed in this proposal:

- [PodGroup isn't triggering scaling up in Kubernetes, when using Cluster Autoscaler \#2558](https://github.com/volcano-sh/volcano/issues/2558)
- [Remove Undetermined reason to fix cluster autoscaler compatibility \#2602](https://github.com/volcano-sh/volcano/pull/2602)
- [Support Pod Scheduling Readiness \#3555](https://github.com/volcano-sh/volcano/issues/3555)
