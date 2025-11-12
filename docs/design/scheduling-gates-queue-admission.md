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

The gates will then be removed in the scheduler side once the Pod is ready to be considered for scheduling.

#### Changes to the Volcano Scheduler (`Allocate` Action)

Volcano already adapted to the concept of `schedulingGates` through the Pod Scheduling Readiness design document ([pod-scheduling-readiness.md](https://github.com/volcano-sh/volcano/blob/master/docs/design/pod-scheduling-readiness.md)), which skips tasks of a job whose Pods are scheduling gated, ensuring that a scheduling gated pod will not be bound to a node. In this case, the [allocate](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/allocate/allocate.go#L159), [backfill](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/backfill/backfill.go#L139-L141), [reclaim](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/reclaim/reclaim.go#L85-L87), and [preempt](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/actions/preempt/preempt.go#L223-L226) actions already skip Pods that [have at least one gate](https://github.com/volcano-sh/volcano/blob/v1.13.0/pkg/scheduler/api/job_info.go#L246-L252).

Each time the `Allocate` action is executed, `alloc.allocateResourcesForTasks` tries to allocate resources for each Task in a given Queue and `ssn.Allocatable(queue, task)` gives us the needed signal (by running every plugin check) for eventually removing the gate added previously by the **MutatingAdmissionWebhook**.

The goal here would be to enhance the `alloc.allocateResourcesForTasks` function with asynchronous removal of the gate to avoid blocking the main scheduler thread on API calls. The following code snippet showcases the high-level changes to the function `allocateResourcesForTasks`:

```go
// Enhance allocateResourcesForTasks with the gate removal decision
func (alloc *Action) allocateResourcesForTasks(...) {

  // ...

    for !tasks.Empty() {
        task := tasks.Pop().(*api.TaskInfo)
            
        // Check if queue can accept this task
        if !ssn.Allocatable(queue, task) {
                continue
        }
        
        // Task can be allocated
        if task.SchGated && hasOnlyVolcanoGate(task.Pod) {

            // ...hasOnlyVolcanoGate checks if the pod has 
            // only the volcano.sh/queue-allocation-gate gate
                
            // Queue the removal request to background workers
            req := &schGateRemovalRequest{
            podNamespace: task.Namespace,
            podName:      task.Name,
                podUID:       types.UID(task.Pod.UID),
                sessionID:    ssn.UID,
            }
        
            select {
            case alloc.schGateRemovalRequests <- req:
            klog.V(3).Infof("Queued gate removal for %s/%s", task.Namespace, task.Name)
            default:
            // ...
            }
        }
    }
	
  // ...

}
```

Gate removal requests are then queued to background workers, and eventually the API server and informer cache will have time to process the gate removal before attempting to bind the pod. The following changes are suggested to support this process:

```go

// Enhance the Action struct (used in each scheduling cycle)
type Action struct {
  session *framework.Session
  // ...
  
  // Async gate removal
  kubeClient             kubernetes.Interface
  schGateRemovalRequests chan *schGateRemovalRequest
  numSchGateWorkers      int
  schGateShutdownCh      chan struct{}

  // ...
}

// Add new type with metadata for pod gate removals
type schGateRemovalRequest struct {
    podNamespace string
    podName      string
    podUID       types.UID
    sessionID    types.UID
}

// Add background worker routines to process gate removals
func (alloc *Action) processSchGateRemovalRequests(...) {
    for {
        select {
        case req := <-alloc.schGateRemovalRequests:
            if err := removeVolcanoSchGateFromPodByName(alloc.kubeClient, req.podNamespace, req.podName); err != nil {
	            // ...
            }
        case <-alloc.schGateShutdownCh:
            return
        }
    }
}
```

## Conclusions

Scheduling Gates are a Kubernetes feature that allows external controllers to delay pod scheduling until specific conditions are met. The proposed design leverages this mechanism to defer scheduling until the queue has sufficient capacity, preventing pods from appearing as `Unschedulable` when they're simply waiting for queue admission.

This implementation requires pods to opt in via the `volcano.sh/enable-scheduling-gates: "true"` annotation. This conservative approach ensures backward compatibility and allows users to adopt the feature incrementally. Future iterations may enable this behavior by default once the feature maturity is validated in production environments.

## Related Issues

The following issues are related to the matters discussed in this proposal:

* [PodGroup isn't triggering scaling up in Kubernetes, when using Cluster Autoscaler \#2558](https://github.com/volcano-sh/volcano/issues/2558)  
* [Remove Undetermined reason to fix cluster autoscaler compatibility \#2602](https://github.com/volcano-sh/volcano/pull/2602)  
* [Support Pod Scheduling Readiness \#3555](https://github.com/volcano-sh/volcano/issues/3555)

