# PodGroup Status Enhancement

@k82cn; Jan 2, 2019

## Table of Contents

* [Table of Contents](#table-of-contents)
* [Motivation](#motivation)
* [Function Detail](#function-detail)
* [Feature Interaction](#feature-interaction)
   * [Cluster AutoScale](#cluster-autoscale)
   * [Operators/Controllers](#operatorscontrollers)
* [Reference](#reference)

## Motivation

In [Coscheduling v1alph1](https://github.com/kubernetes/enhancements/pull/639) design, `PodGroup`'s status
only includes counters of related pods which is not enough for `PodGroup` lifecycle management. More information
about PodGroup's status will be introduced in this design doc for lifecycle management, e.g. `PodGroupPhase`.

## Function Detail

To include more information for PodGroup current status/phase, the following types are introduced:

```go
// PodGroupPhase is the phase of a pod group at the current time.
type PodGroupPhase string

// These are the valid phase of podGroups.
const (
    // PodPending means the pod group has been accepted by the system, but scheduler can not allocate
    // enough resources to it.
    PodGroupPending PodGroupPhase = "Pending"
    // PodRunning means `spec.minMember` pods of PodGroups has been in running phase.
    PodGroupRunning PodGroupPhase = "Running"
	// PodGroupRecovering means part of `spec.minMember` pods have exception, e.g. killed; scheduler will
	// wait for related controller to recover it.
    PodGroupRecovering PodGroupPhase = "Recovering"
	// PodGroupUnschedulable means part of `spec.minMember` pods are running but the other part can not
	// be scheduled, e.g. not enough resource; scheduler will wait for related controller to recover it.
    PodGroupUnschedulable PodGroupPhase = "Unschedulable"
)

const (
	// PodFailedReason is probed if pod of PodGroup failed
	PodFailedReason string = "PodFailed"
	// PodDeletedReason is probed if pod of PodGroup deleted
	PodDeletedReason string = "PodDeleted"
	// NotEnoughResourcesReason is probed if there're not enough resources to schedule pods
	NotEnoughResourcesReason string = "NotEnoughResources"
	// NotEnoughPodsReason is probed if there're not enough tasks compared to `spec.minMember`
	NotEnoughPodsReason string = "NotEnoughTasks"
)

// PodGroupState contains details for the current state of this pod group.
type PodGroupState struct {
    // Current phase of PodGroup.
    Phase PodGroupPhase `json:"phase,omitempty" protobuf:"bytes,1,opt,name=phase"`
	
    // Last time we probed to this Phase.
    // +optional
    LastProbeTime metav1.Time `json:"lastProbeTime,omitempty" protobuf:"bytes,2,opt,name=lastProbeTime"`
    // Last time the phase transitioned from another to current phase.
    // +optional
    LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,3,opt,name=lastTransitionTime"`
    // Unique, one-word, CamelCase reason for the phase's last transition.
    // +optional
    Reason string `json:"reason,omitempty" protobuf:"bytes,4,opt,name=reason"`
    // Human-readable message indicating details about last transition.
    // +optional
    Message string `json:"message,omitempty" protobuf:"bytes,5,opt,name=message"`
}

type PodGroupStatus struct {
    ......

    // +optional
    State PodGroupState `json:"state,omitempty" protobuf:"bytes,1,opt,name=state,casttype=State"`
}
```

According to the PodGroup's lifecycle, the following phase/state transactions are reasonable. And related
reasons will be appended to `Reason` field.  

| From          | To            | Reason  |
|---------------|---------------|---------|
| Pending       | Running       | When every pods of `spec.minMember` are running |
| Pending       | Recovering    | When only part of `spec.minMember` are running and the other part pod are rejected by kubelet |
| Running       | Recovering    | When part of `spec.minMember` have exception, e.g. kill |
| Recovering    | Running       | When the failed pods re-run successfully |
| Recovering    | Unschedulable | When the new pod can not be scheduled |
| Unschedulable | Pending       | When all pods (`spec.minMember`) in PodGroups are deleted |
| Unschedulable | Running       | When all pods (`spec.minMember`) are deleted |


## Feature Interaction

### Cluster AutoScale

[Cluster Autoscaler](https://github.com/kubernetes/autoscaler/tree/master/cluster-autoscaler) is a tool that
automatically adjusts the size of the Kubernetes cluster when one of the following conditions is true:

* there are pods that failed to run in the cluster due to insufficient resources,
* there are nodes in the cluster that have been underutilized for an extended period of time and their pods can be placed on other existing nodes.

When Cluster-Autoscaler scale-out a new node, it leverage predicates in scheduler to check whether the new node can be
scheduled. But Coscheduling is not an implementation of predicates for now; so it'll not work well together with
Cluster-Autoscaler right now. Alternative solution will be proposed later for that.

### Operators/Controllers

The lifecycle of `PodGroup` are managed by operators/controllers, the scheduler only probes related state for
controllers. For example, if `PodGroup` is `Unschedulable` for MPI job, the controller need to re-start all
pods in `PodGroup`.  

## Reference

* [Coscheduling](https://github.com/kubernetes/enhancements/pull/639)
* [Add phase/conditions into PodGroup.Status](https://github.com/kubernetes-sigs/kube-batch/issues/521)
* [Add Pod Condition and unblock cluster autoscaler](https://github.com/kubernetes-sigs/kube-batch/issues/526)

