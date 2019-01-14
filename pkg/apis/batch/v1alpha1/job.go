/*
Copyright 2018 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Job struct {
	metav1.TypeMeta `json:",inline"`

	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of a cron job, including the minAvailable
	// +optional
	Spec JobSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Current status of Job
	// +optional
	Status JobStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// JobSpec describes how the job execution will look like and when it will actually run
type JobSpec struct {
	// SchedulerName is the default value of `taskSpecs.template.spec.schedulerName`.
	// +optional
	SchedulerName string `json:"schedulerName,omitempty" protobuf:"bytes,1,opt,name=schedulerName"`

	// The minimal available pods to run for this Job
	// +optional
	MinAvailable int32 `json:"minAvailable,omitempty" protobuf:"bytes,2,opt,name=minAvailable"`

	// The volume mount for input of Job
	Input *VolumeSpec `json:"input,omitempty" protobuf:"bytes,3,opt,name=input"`

	// The volume mount for output of Job
	Output *VolumeSpec `json:"output,omitempty" protobuf:"bytes,4,opt,name=output"`

	// Tasks specifies the task specification of Job
	// +optional
	Tasks []TaskSpec `json:"taskSpecs,omitempty" protobuf:"bytes,5,opt,name=taskSpecs"`

	// Specifies the default lifecycle of tasks
	// +optional
	Policies []LifecyclePolicy `json:"policies,omitempty" protobuf:"bytes,6,opt,name=policies"`
}

// VolumeSpec defines the specification of Volume, e.g. PVC
type VolumeSpec struct {
	v1.VolumeMount `json:",inline"`

	// VolumeClaim defines the PVC used by the VolumeMount.
	VolumeClaim *v1.PersistentVolumeClaim `json:"claim,omitempty" protobuf:"bytes,1,opt,name=claim"`
}

// Event represent the phase of Job, e.g. pod-failed.
type Event string

const (
	// AllEvent means all event
	AllEvents             Event = "*"
	// PodFailedEvent is triggered if Pod was failed
	PodFailedEvent        Event = "PodFailed"
	// PodEvictedEvent is triggered if Pod was deleted
	PodEvictedEvent       Event = "PodEvicted"
	// JobUnschedulableEvent is triggered if part of pod can be scheduled
	// when gang-scheduling enabled
	JobUnschedulableEvent Event = "Unschedulable"
)

// Action is the action that Job controller will take according to the event.
type Action string

const (
	// AbortJobAction if this action is set, the whole job will be aborted:
	// all Pod of Job will be evicted, and no Pod will be recreated
	AbortJobAction     Action = "AbortJob"
	// RestartJobAction if this action is set, the whole job will be restarted
	RestartJobAction   Action = "RestartJob"
	// RestartTaskAction if this action is set, only the task will be restarted; default action.
	// This action can not work togther with job level events, e.g. JobUnschedulable
	RestartTaskAction  Action = "RestartTask"
	// TerminateJobAction if this action is set, the whole job wil be terminated
	// and can not be resumed: all Pod of Job will be evicted, and no Pod will be recreated.
	TerminateJobAction Action = "TerminateJob"
)

// LifecyclePolicy specifies the lifecycle and error handling of task and job.
type LifecyclePolicy struct {
	// The action that will be taken to the PodGroup according to Event.
	// One of "Restart", "None".
	// Default to None.
	// +optional
	Action Action `json:"action,omitempty" protobuf:"bytes,1,opt,name=action"`

	// The Event recorded by scheduler; the controller takes actions
	// according to this Event.
	// One of "PodFailed", "Unschedulable".
	// +optional
	Event Event `json:"event,omitempty" protobuf:"bytes,2,opt,name=event"`

	// Timeout is the grace period for controller to take actions.
	// Default to nil (take action immediately).
	// +optional
	Timeout *metav1.Duration `json:"timeout,omitempty" protobuf:"bytes,3,opt,name=timeout"`
}

// TaskSpec specifies the task specification of Job
type TaskSpec struct {
	// Name specifies the name of tasks
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// Replicas specifies the replicas of this TaskSpec in Job
	Replicas int32 `json:"replicas,omitempty" protobuf:"bytes,2,opt,name=replicas"`

	// Specifies the pod that will be created for this TaskSpec
	// when executing a Job
	Template v1.PodTemplateSpec `json:"template,omitempty" protobuf:"bytes,3,opt,name=template"`

	// Specifies the lifecycle of task
	// +optional
	Policies []LifecyclePolicy `json:"policies,omitempty" protobuf:"bytes,4,opt,name=policies"`
}

type JobPhase string

const (
	// Pending is the phase that job is pending in the queue, waiting for scheduling decision
	Pending     JobPhase = "Pending"
	// Aborted is the phase that job is aborted by user or error handling
	Aborted     JobPhase = "Aborted"
	// Running is the phase that minimal available tasks of Job are running
	Running     JobPhase = "Running"
	// Completed is the phase that all tasks of Job are completed successfully
	Completed   JobPhase = "Completed"
	// Teriminated is the phase that the job is finished unexpected, e.g. events
	Teriminated JobPhase = "Terminated"
)

// JobConditionType is a valid value for JobCondition.Type
type JobConditionType string

// ConditionStatus is a value of valid condition statuses
type ConditionStatus string

// These are valid condition statuses. "ConditionTrue" means a resource is in the condition.
// "ConditionFalse" means a resource is not in the condition. "ConditionUnknown" means kubernetes
// can't decide if a resource is in the condition or not. In the future, we could add other
// intermediate conditions, e.g. ConditionDegraded.
const (
	ConditionTrue    ConditionStatus = "True"
	ConditionFalse   ConditionStatus = "False"
	ConditionUnknown ConditionStatus = "Unknown"
)

// JobCondition contains details for the current condition of this job.
type JobCondition struct {
	// Type is the type of the condition.
	Type JobConditionType `json:"type" protobuf:"bytes,1,opt,name=type,casttype=JobConditionType"`
	// Status is the status of the condition.
	// Can be True, False, Unknown.
	Status ConditionStatus `json:"status" protobuf:"bytes,2,opt,name=status,casttype=ConditionStatus"`
	// Last time we probed the condition.
	// +optional
	LastProbeTime metav1.Time `json:"lastProbeTime,omitempty" protobuf:"bytes,3,opt,name=lastProbeTime"`
	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty" protobuf:"bytes,4,opt,name=lastTransitionTime"`
	// Unique, one-word, CamelCase reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,5,opt,name=reason"`
	// Human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,6,opt,name=message"`
}

// JobStatus represents the current state of a Job
type JobStatus struct {
	// The phase of a Pod is a simple, high-level summary of where the Pod is in its lifecycle.
	// The conditions array, the reason and message fields, and the individual container status
	// arrays contain more detail about the pod's status.
	// There are five possible phase values:
	// +optional
	Phase JobPhase `json:"phase,omitempty" protobuf:"bytes,1,opt,name=phase,casttype=JobPhase"`
	// Current service state of pod.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#pod-conditions
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []JobCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,2,rep,name=conditions"`

	// The number of pending pods.
	// +optional
	Pending int32 `json:"pending,omitempty" protobuf:"bytes,3,opt,name=pending"`

	// The number of running pods.
	// +optional
	Running int32 `json:"running,omitempty" protobuf:"bytes,4,opt,name=running"`

	// The number of pods which reached phase Succeeded.
	// +optional
	Succeeded int32 `json:"Succeeded,omitempty" protobuf:"bytes,5,opt,name=succeeded"`

	// The number of pods which reached phase Failed.
	// +optional
	Failed int32 `json:"failed,omitempty" protobuf:"bytes,6,opt,name=failed"`

	// The minimal available pods to run for this Job
	// +optional
	MinAvailable int32 `json:"minAvailable,omitempty" protobuf:"bytes,7,opt,name=minAvailable"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type JobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Items []Job `json:"items" protobuf:"bytes,2,rep,name=items"`
}
