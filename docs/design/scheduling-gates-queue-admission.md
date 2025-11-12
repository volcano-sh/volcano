# Gate-Controlled Scheduling for Cluster Autoscalers Compatibility
    
## Motivation

The use of cluster autoscalers such as [Cluster Autoscaler (CA)](https://github.com/kubernetes/autoscaler/tree/master) or [Karpenter](https://github.com/kubernetes-sigs/karpenter) is common in Kubernetes environments to dynamically adjust node capacity based on scheduler signals. Both autoscalers monitor pod scheduling conditions, specifically looking for pods that match:

```yaml
type: PodScheduled
status: "False"
reason: Unschedulable
```

These pods are interpreted as evidence of insufficient cluster capacity, triggering a scale-up operation to add new nodes.

> Please refer to the following pointers regarding detecting `Unschedulable` Pods:
> * [CA (v1.34.1): listers.go – func isUnschedulable(pod \*apiv1.Pod) bool](https://github.com/kubernetes/autoscaler/blob/cluster-autoscaler-1.34.1/cluster-autoscaler/utils/kubernetes/listers.go#L161-L170) (also check [FAQ.md](https://github.com/kubernetes/autoscaler/blob/cluster-autoscaler-release-1.34/cluster-autoscaler/FAQ.md#how-does-scale-up-work))  
> * [Karpenter (v1.8.0): scheduling.go – func FailedToSchedule(pod \*corev1.Pod) bool](https://github.com/kubernetes-sigs/karpenter/blob/v1.8.0/pkg/utils/pod/scheduling.go#L116-L129)

This mechanism works as intended with the default `kube-scheduler`, but can cause unintended behavior when used with Volcano, which introduces an additional scheduling layer that includes Queue constraints, for example. Because these scheduling layers are not visible to autoscalers, there are scenarios (refer to [Volcano's issue \#4710](https://github.com/volcano-sh/volcano/issues/4710)) where Volcano correctly marks pods as `Unschedulable` due to Queue capacity limits, but the autoscaler interprets this as a signal to scale up.

### How Volcano Currently Sets Unschedulable

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

In this proposal, changes will be required to both the admission process and also the scheduler flow, as the latter already contains all the logic for actions and plugin decisions.

> Note: All the changes proposed reflect the Volcano latest [v1.13.0 release](https://github.com/volcano-sh/volcano/releases/tag/v1.13.0).

#### Changes to the Volcano `MutatingAdmissionWebhook`

Volcano's `MutatingAdmissionWebhook` will be extended to detect Pods annotated with `volcano.sh/enable-queue-allocation-gate="true"` and patch them accordingly with a new `schedulingGate`, for instance, called `volcano.sh/queue-allocation-gate`:

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
        Value: []v1.PodSchedulingGate{ {Name: "volcano.sh/queue-allocation-gate"} },
    }
}
```

The gates will then be gradually removed in the scheduler side since the latter contains the actions and plugins that determine if the Pod can be allocated to a queue and eventually a node.

#### Changes to the Volcano Scheduler

Volcano already adapted to the concept of `schedulingGates` through the Pod Scheduling Readiness design document ([pod-scheduling-readiness.md](https://github.com/volcano-sh/volcano/blob/master/docs/design/pod-scheduling-readiness.md)), which skips tasks of a job whose Pods are scheduling gated, ensuring that a scheduling gated pod will not be bound to a node. In this case, the [allocate](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/allocate/allocate.go#L159), [backfill](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/backfill/backfill.go#L139-L141), [reclaim](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/reclaim/reclaim.go#L85-L87), and [preempt](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/preempt/preempt.go#L223-L226) actions already skip Pods that [have at least one gate](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/api/job_info.go#L246-L252).

Each time the `Allocate` action is executed, `alloc.allocateResourcesForTasks` tries to allocate resources for each Task in a given Queue and `ssn.Allocatable(queue, task)` gives us the needed signal (by running every plugin check) for eventually removing the gate added previously by the **MutatingAdmissionWebhook**.

##### Asynchronous Gate Management

The scheduler must handle both **gate removal** (when queue capacity becomes available) and **gate re-addition** (when queue capacity becomes unavailable after previously being available). Queue capacity is dynamic - as pods complete or are preempted, capacity is freed; as new pods are allocated, capacity is consumed. This means a pod that was previously ungated due to available capacity might later need to be re-gated if the queue fills up.

All gate operations (add/remove) involve Kubernetes API calls, which can take hundreds of milliseconds. If these were performed synchronously in the main scheduling loop, they would significantly degrade scheduler performance and throughput. Instead, gate operations are queued to background workers and processed asynchronously, allowing the scheduler to continue processing other tasks without blocking.

The following code snippet showcases the high-level changes to the function `allocateResourcesForTasks`:

```go
// Enhance allocateResourcesForTasks with gate management
func (alloc *Action) allocateResourcesForTasks(...) {

    // ...

    for !tasks.Empty() {
        task := tasks.Pop().(*api.TaskInfo)
        
        // Check if queue can accept this task
        if !ssn.Allocatable(queue, task) {
            // ADD: Queue capacity is now unavailable (may have been available before)
            // Re-add gate to prevent false autoscaler signals. This handles dynamic
            // queue capacity changes - e.g., when pods complete/get preempted, capacity
            // frees up; when new pods are allocated, capacity is consumed.
            alloc.enqueueSchedulingGateAddition(task)
            continue
        }
        
        // Queue has capacity - mark task for gate removal during bind
        // Note: Gate is NOT removed here to avoid race condition. If we removed it
        // now, the pod could become visible to autoscalers before it's actually bound,
        // and if binding fails, the pod would appear Unschedulable without a gate.
        if task.SchGated && hasOnlyVolcanoSchedulingGate(task.Pod) {
            task.RemoveGateDuringBind = true
        }

        // Skip tasks with non-Volcano gates
        if task.SchGated && !task.RemoveGateDuringBind {
            continue
        }

        // ...

        // ADD: Handle PrePredicate failure (e.g., topology constraints)
        // Remove gate so pod becomes Unschedulable - this IS a legitimate signal
        // for autoscalers since it indicates node resource/constraint issues
        if err := ssn.PrePredicateFn(task); err != nil {
            // ...
            alloc.enqueueSchedulingGateRemoval(task)
            // ...
        }

        // ADD: Handle no predicate nodes (no nodes fit the pod)
        // Remove gate - legitimate autoscaler signal for node shortage
        if len(predicateNodes) == 0 {
            // ...
            alloc.enqueueSchedulingGateRemoval(task)
            // ...
        }

        // ADD: Handle no best node after prioritization
        // Remove gate - legitimate autoscaler signal
        if bestNode == nil {
            // ...
            alloc.enqueueSchedulingGateRemoval(task)
            // ...
        }

        // ... rest of allocation logic ...
    }
    
    // ...
}

// enqueueSchedulingGateRemoval queues async gate removal if scheduling failed
func (alloc *Action) enqueueSchedulingGateRemoval(task *api.TaskInfo) {
    if hasOnlyVolcanoSchedulingGate(task.Pod) {
        op := schGateOperation{opType: schGateOperationRemove, ...}
        alloc.schGateOperationCh <- op
        task.SchGated = false  // Mark as ungated in cache
    }
}

// enqueueSchedulingGateAddition queues async gate re-addition when queue becomes unavailable
func (alloc *Action) enqueueSchedulingGateAddition(task *api.TaskInfo) {
    if !task.SchGated && !hasOnlyVolcanoSchedulingGate(task.Pod) {
        op := schGateOperation{opType: schGateOperationAdd, ...}
        alloc.schGateOperationCh <- op
        task.SchGated = true  // Mark as gated in cache
    }
}
```

##### Atomic Bind + Gate Removal

When a task successfully passes all allocation checks (queue capacity available, node found, predicates pass), the gate is **not** removed immediately. Instead, the task is marked with `RemoveGateDuringBind = true`, and the gate removal happens in the `Bind` function, just before the actual bind operation.


Gate operations are queued to background workers through a unified channel, and processed asynchronously. The following changes support this process:

```go
// Enhance the Action struct
type Action struct {
    session *framework.Session
    // ...
    
    // Async gate management (unified for add/remove)
    schGateOperationCh chan schGateOperation
    schGateWorkersWg   sync.WaitGroup
    schGateShutdownCh  chan struct{}
}

type schGateOperationType string

const (
    schGateOperationAdd    schGateOperationType = "add"
    schGateOperationRemove schGateOperationType = "remove"
)

type schGateOperation struct {
    opType    schGateOperationType
    namespace string
    name      string
}

// Background worker processes both add and remove operations
func (alloc *Action) schGateOperationWorker() {
    for {
        select {
        case op := <-alloc.schGateOperationCh:
            switch op.opType {
            case schGateOperationRemove:
                cache.RemoveVolcanoSchGate(...)
            case schGateOperationAdd:
                cache.AddVolcanoSchGate(...)
            }
        case <-alloc.schGateShutdownCh:
            return
        }
    }
}
```

##### Gate Removal During Successful Bind

When a pod successfully passes all allocation checks (queue has capacity, suitable node found, all predicates pass), its gate is removed atomically during the bind operation. The `RemoveGateDuringBind` flag signals the binder to remove the gate immediately before binding the pod to its assigned node:

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

This implementation requires pods to opt in via the `volcano.sh/enable-queue-allocation-gate: "true"` annotation. This conservative approach ensures backward compatibility and allows users to adopt the feature incrementally. Future iterations may enable this behavior by default once the feature maturity is validated in production environments.

## Related Issues

The following issues are related to the matters discussed in this proposal:

* [PodGroup isn't triggering scaling up in Kubernetes, when using Cluster Autoscaler \#2558](https://github.com/volcano-sh/volcano/issues/2558)  
* [Remove Undetermined reason to fix cluster autoscaler compatibility \#2602](https://github.com/volcano-sh/volcano/pull/2602)  
* [Support Pod Scheduling Readiness \#3555](https://github.com/volcano-sh/volcano/issues/3555)

