# Gate-Controlled Scheduling for Cluster Autoscalers Compatibility
    
## Motivation

The use of cluster autoscalers such as [Cluster Autoscaler (CA)](https://github.com/kubernetes/autoscaler/tree/master) or [Karpenter](https://github.com/kubernetes-sigs/karpenter) is common in Kubernetes environments to dynamically adjust node capacity based on scheduler signals. Both autoscalers monitor pod scheduling conditions, specifically looking for pods that match:

```yaml
type: PodScheduled
status: "False"
reason: Unschedulable
```

These pods are interpreted as evidence of insufficient cluster capacity, triggering a scale-up simulations to eventually add new nodes.

> Please refer to the following pointers regarding detecting `Unschedulable` Pods in CA and Karpenter:
> * [CA (v1.34.1): listers.go – func isUnschedulable(pod \*apiv1.Pod) bool](https://github.com/kubernetes/autoscaler/blob/cluster-autoscaler-1.34.1/cluster-autoscaler/utils/kubernetes/listers.go#L161-L170) (also check [FAQ.md](https://github.com/kubernetes/autoscaler/blob/cluster-autoscaler-release-1.34/cluster-autoscaler/FAQ.md#how-does-scale-up-work))  
> * [Karpenter (v1.8.0): scheduling.go – func FailedToSchedule(pod \*corev1.Pod) bool](https://github.com/kubernetes-sigs/karpenter/blob/v1.8.0/pkg/utils/pod/scheduling.go#L116-L129)

This mechanism works as intended with the default `kube-scheduler`, but can cause unintended behavior when used with Volcano, which introduces an additional scheduling layer that includes Queue constraints, for example. Because these scheduling layers are not visible to autoscalers, there are scenarios (refer to [Volcano's issue \#4710](https://github.com/volcano-sh/volcano/issues/4710)) where Volcano correctly marks pods as `Unschedulable` due to Queue capacity limits, but the autoscaler interprets this as a signal to scale up.

### How Volcano Currently Sets Unschedulable

> Note: Every mention of Volcano in this document refers to the latest [v1.13.0 release](https://github.com/volcano-sh/volcano/releases/tag/v1.13.0).

Volcano sets the `Unschedulable` condition on pods through its cache event recording mechanism in [`pkg/scheduler/cache/cache.go`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/cache/cache.go). After each scheduling cycle, the [`RecordJobStatusEvent`](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/cache/cache.go#L1515) function examines tasks (pods) that have not been allocated and updates their `PodScheduled` condition with `status=False` and `reason=Unschedulable`.

This happens for **any** task that fails allocation, regardless of whether the failure is due to insufficient node resources (legitimate scale-up scenario) or queue capacity limits (false signal for autoscalers). Autoscalers cannot distinguish between these scenarios, as they only see the `Unschedulable` condition.

## Proposal

This proposal introduces the use of Kubernetes `schedulingGates` (as defined in [KEP-3521](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/3521-pod-scheduling-readiness#summary)) to control when a Pod is considered ready for scheduling. The core idea is to delay setting the `Unschedulable` condition until the Queue has enough capacity to accommodate the Pod.

### Goals

* Prevent downstream integrators (like CA, Karpenter) from triggering unnecessary scale-ups caused by Pods marked as `Unschedulable`.  
* Provide an opt-in mechanism, controlled via Pod annotation.  
* Preserve existing Volcano scheduling semantics.  
* Non-blocking implementation (avoid scheduler performance degradation).

### Non-Goals

* Altering the behavior or internal logic of Cluster Autoscaler or Karpenter.  
* Rejecting or blocking Pod creation at admission time.  
* Introducing custom autoscaler integrations or external controllers.

### High-Level Implementation

In this proposal, changes will be required to both the **admission process** and also the **scheduler routines**, as the latter is responsible for applying the logic for actions and plugins.

#### Changes to the Volcano `MutatingAdmissionWebhook`

Volcano's `MutatingAdmissionWebhook` needs to be extended to detect Pods annotated with `volcano.sh/enable-queue-allocation-gate="true"` and must patch Pods at creation time with a new `schedulingGate` entry, for instance, called `volcano.sh/queue-allocation-gate`:

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
    if pod.Annotations == nil || pod.Annotations["volcano.sh/enable-queue-allocation-gate"] != "true" {
        return nil
    }

    // ...

    return &patchOperation{
        Op:    "add",
        Path:  "/spec/schedulingGates",
        Value: append(pod.Spec.SchedulingGates, v1.PodSchedulingGate{Name: "volcano.sh/queue-allocation-gate"}),
    }
}
```

**`schedulingGates` Field Immutability**

Kubernetes `schedulingGates` can only be removed, **not added after pod creation** ([see PodSpec documentation](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#scheduling)). The gates will then be gradually removed in the scheduler side, since the latter contains the actions and plugins that determine if the Pod can be allocated to a queue and eventually a node:

- **During bind**: For successfully allocated pods, gates are removed atomically with binding.
- **For node-fit failures**: When queue has capacity but no node fits the pod, gates are removed to signal the `Unschedulable` condition to autoscalers.

#### Changes to the Volcano Scheduler

Volcano already adapted to the concept of `schedulingGates` through the **Pod Scheduling Readiness** design document ([pod-scheduling-readiness.md](https://github.com/volcano-sh/volcano/blob/master/docs/design/pod-scheduling-readiness.md)), which skips tasks of a job whose Pods are scheduling gated, ensuring that a scheduling gated pod will not be bound to a node. In this case, the [allocate](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/allocate/allocate.go#L159), [backfill](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/backfill/backfill.go#L139-L141), [reclaim](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/reclaim/reclaim.go#L85-L87), and [preempt](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/preempt/preempt.go#L223-L226) actions already skip Pods that [have at least one gate](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/api/job_info.go#L246-L252).

Each time the `Allocate` action is executed, `alloc.allocateResourcesForTasks(...)` tries to allocate resources for each Task in a given Queue and `ssn.Allocatable(queue, task)` gives us the needed signal (by running every plugin check) for eventually removing the gate added previously by the **MutatingAdmissionWebhook**. Gates should be removed for signaling the `Unschedulable` condition to autoscalers when no node fits the pod, otherwise, the pod will be allocated to a node and gates are removed during the bind operation.

##### Asynchronous Gate Removal

To avoid blocking the scheduler, gate removals for node-fit failures are queued to background workers and processed asynchronously. The following code snippet showcases the possible high-level changes to the function `allocateResourcesForTasks(...)`:

```go
// Enhance allocateResourcesForTasks with gate management
func (alloc *Action) allocateResourcesForTasks(...) {

    // ...

    for !tasks.Empty() {
        task := tasks.Pop().(*api.TaskInfo)

        // The ssn.Allocatable(...) check needs to be enhanced to account for ungated pods
        // (see section "Queue Capacity Accounting for Ungated Pods" for more details).
        // E.g., If the task is Unschedulable, it means that the pod has been already ungated 
        // during the scheduling cycle, as such, it has "reserved" queue resources.
        if !alloc.IsTaskUnschedulable(task) && !ssn.Allocatable(queue, task) {
            // ...
            continue
        }
        
        // Queue has capacity - mark task for gate removal during bind
        if task.SchGated && hasOnlyVolcanoSchedulingGate(task.Pod) {
            task.RemoveGateDuringBind = true
        }

        // Skip tasks with non-Volcano gates
        if task.SchGated && !task.RemoveGateDuringBind {
            continue
        }

        // ...

        // ADD: Handle PrePredicate failure (e.g., topology constraints)
        // Remove gate so pod becomes Unschedulable - this is a legitimate signal
        // for autoscalers since it indicates node resource/constraint issues
        if err := ssn.PrePredicateFn(task); err != nil {
            // ...
            alloc.enqueueSchedulingGateRemoval(task)
            // ...
        }

        // ADD: Handle no predicate nodes (no nodes fit the pod)
        // Remove gate so pod becomes Unschedulable (same as PrePredicate failure)
        if len(predicateNodes) == 0 {
            // ...
            alloc.enqueueSchedulingGateRemoval(task)
            // ...
        }

        // ADD: Handle no best node
        // Remove gate so pod becomes Unschedulable (same as PrePredicate failure)
        if bestNode == nil {
            // ...
            alloc.enqueueSchedulingGateRemoval(task)
            // ...
        }

        // ... rest of allocation logic ...
    }
    
    // ...
}

// enqueueSchedulingGateRemoval queues async gate removal for node-fit failures
func (alloc *Action) enqueueSchedulingGateRemoval(task *api.TaskInfo) {
    if hasOnlyVolcanoSchedulingGate(task.Pod) {
        op := schGateRemovalOperation{namespace: task.Namespace, name: task.Name}
        alloc.schGateRemovalOperationCh <- op
        task.SchGated = false  // Mark as ungated in cache
    }
}
```

To support asynchronous gate removal, the `Action` struct is enhanced with channels and worker management fields:

```go
// Enhance the Action struct
type Action struct {
    session *framework.Session
    // ...
    
    // Async gate removal channel
    schGateRemovalOperationCh chan schGateRemovalOperation
    schGateRemovalWorkersWg   sync.WaitGroup
    schGateRemovalShutdownCh  chan struct{}
}

type schGateRemovalOperation struct {
    namespace string
    name      string
}

// Background worker processes gate removal operations
func (alloc *Action) schGateRemovalWorker() {
    for {
        select {
        case op := <-alloc.schGateRemovalOperationCh:
            cache.RemoveVolcanoSchGate(kubeClient, op.namespace, op.name)
        case <-alloc.schGateRemovalShutdownCh:
            return
        }
    }
}
```

##### Queue Capacity Accounting for Ungated Pods

When a pod's gate is removed due to node-fit failure, it becomes visible to autoscalers but remains unallocated (waiting for matching nodes). This creates a potential race condition: between gate removal and actual allocation, other pods might consume the available queue capacity, leaving the ungated pod unable to allocate despite being `Unschedulable`.

To prevent this, the queue capacity accounting logic must be enhanced to treat ungated pods as "reservations" that count toward the queue's "virtual" used capacity. Specifically, the `ssn.Allocatable(queue, task)` function needs to be modified to:

1. Count pods with `AllocatedStatus` (bound, binding, running) toward queue capacity *(current behavior)*.
2. **Additionally count `Pending` pods that have had their gates removed** toward queue capacity *(new behavior)*.

As such, in the capacity plugin, the `buildHierarchicalQueueAttrs` function should require the following modifications:

```go
func (cp *capacityPlugin) buildHierarchicalQueueAttrs(ssn *framework.Session) bool {

    // ...

    for status, tasks := range job.TaskStatusIndex {
        if api.AllocatedStatus(status) {
            //...
        } else if status == api.Pending {
            for _, t := range tasks {
                if !t.SchGated {
                    attr.allocated.Add(t.Resreq) // ADD: Accounting for ungated pods (in Pending status)
                }
                attr.request.Add(t.Resreq)
            }
        }
    }

    // ...

}
```

This ensures that once a gate is removed, the pod effectively "reserves" its queue slot, preventing other pods from consuming that capacity. The pod remains in this reserved state until it either successfully allocates to a node or is deleted.

##### Gate Removal During Successful Bind

Finally, the last case is when a task successfully passes **all allocation checks** (i.e., `ssn.Allocatable(queue, task)` returns `true` and there's a node that fits the pod), then the gate will only be removed during the `Bind(...)` operation:

```go
func (db *DefaultBinder) Bind(...) map[schedulingapi.TaskID]string {
    // ...

    for _, task := range tasks {
        if task.RemoveGateDuringBind && hasOnlyVolcanoSchedulingGate(task.Pod) {
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

Scheduling Gates are a Kubernetes feature that allows external controllers to delay pod scheduling until specific conditions are met. The proposed design leverages this mechanism to defer scheduling until the queue has sufficient capacity, preventing pods from appearing as `Unschedulable` when they're simply waiting for queue admission.

This implementation requires pods to opt in via the `volcano.sh/enable-queue-allocation-gate: "true"` annotation. This conservative approach ensures backward compatibility and allows users to adopt the feature incrementally. Future iterations **could enable this behavior by default** once the feature maturity is validated in production environments.

## Related Issues

The following issues are related to the matters discussed in this proposal:

* [PodGroup isn't triggering scaling up in Kubernetes, when using Cluster Autoscaler \#2558](https://github.com/volcano-sh/volcano/issues/2558)  
* [Remove Undetermined reason to fix cluster autoscaler compatibility \#2602](https://github.com/volcano-sh/volcano/pull/2602)  
* [Support Pod Scheduling Readiness \#3555](https://github.com/volcano-sh/volcano/issues/3555)

