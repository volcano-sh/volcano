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

    // PodGroupUnknown means part of `spec.minMember` pods are running but the other part can not
    // be scheduled, e.g. not enough resource; scheduler will wait for related controller to recover it.
    PodGroupUnknown PodGroupPhase = "Unknown"
)

type PodGroupConditionType string

const (
    PodGroupUnschedulableType PodGroupConditionType = "Unschedulable"
)

// PodGroupCondition contains details for the current state of this pod group.
type PodGroupCondition struct {
    // Type is the type of the condition
    Type PodGroupConditionType `json:"type,omitempty" protobuf:"bytes,1,opt,name=type"`

    // Status is the status of the condition.
    Status v1.ConditionStatus `json:"status,omitempty" protobuf:"bytes,2,opt,name=status"`

    // The ID of condition transition.
    TransitionID string `json:"transitionID,omitempty" protobuf:"bytes,3,opt,name=transitionID"`

    // Last time the phase transitioned from another to current phase.
    // +optional
    LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,4,opt,name=lastTransitionTime"`

    // Unique, one-word, CamelCase reason for the phase's last transition.
    // +optional
    Reason string `json:"reason,omitempty" protobuf:"bytes,5,opt,name=reason"`

    // Human-readable message indicating details about last transition.
    // +optional
    Message string `json:"message,omitempty" protobuf:"bytes,6,opt,name=message"`
}

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

// PodGroupStatus represents the current state of a pod group.
type PodGroupStatus struct {
    // Current phase of PodGroup.
    Phase PodGroupPhase `json:"phase,omitempty" protobuf:"bytes,1,opt,name=phase"`

    // The conditions of PodGroup.
    // +optional
    Conditions []PodGroupCondition `json:"conditions,omitempty" protobuf:"bytes,2,opt,name=conditions"`

    // The number of actively running pods.
    // +optional
    Running int32 `json:"running,omitempty" protobuf:"bytes,3,opt,name=running"`

    // The number of pods which reached phase Succeeded.
    // +optional
    Succeeded int32 `json:"succeeded,omitempty" protobuf:"bytes,4,opt,name=succeeded"`

    // The number of pods which reached phase Failed.
    // +optional
    Failed int32 `json:"failed,omitempty" protobuf:"bytes,5,opt,name=failed"`
}

```

According to the PodGroup's lifecycle, the following phase/state transactions are reasonable. And related
reasons will be appended to `Reason` field.

| From    | To            | Reason  |
|---------|---------------|---------|
| Pending | Running       | When every pods of `spec.minMember` are running |
| Running | Unknown       | When some pods of `spec.minMember` are restarted but can not be rescheduled |
| Unknown | Pending       | When all pods (`spec.minMember`) in PodGroups are deleted |

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
controllers. For example, if `PodGroup` is `Unknown` for MPI job, the controller need to re-start all pods in `PodGroup`.

## Reference

* [Coscheduling](https://github.com/kubernetes/enhancements/pull/639)
* [Add phase/conditions into PodGroup.Status](https://github.com/kubernetes-sigs/kube-batch/issues/521)
* [Add Pod Condition and unblock cluster autoscaler](https://github.com/kubernetes-sigs/kube-batch/issues/526)

