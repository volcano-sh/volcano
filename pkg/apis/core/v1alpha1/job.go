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
	v1 "k8s.io/api/core/v1"
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

	// TaskSpecs specifies the task specification of Job
	// +optional
	TaskSpecs []TaskSpec `json:"taskSpecs,omitempty" protobuf:"bytes,3,opt,name=taskSpecs"`
}

type Event string

const (
	PodFailed     Event = "PodFailed"
	PodEvicted    Event = "PodEvicted"
	Unschedulable Event = "Unschedulable"
)

type Action string

const (
	None        Action = "None"
	RestartJob  Action = "RestartJob"
	RestartTask Action = "RestartTask"
)

// Lifecycle specifies the lifecycle and error handling of task and job.
type Lifecycle struct {
	Event  Event  `json:"event,omitempty" protobuf:"bytes,1,opt,name=event"`
	Action Action `json:"action,omitempty" protobuf:"bytes,2,opt,name=action"`
}

// TaskSpec specifies the task specification of Job
type TaskSpec struct {
	// A label query over pods that should match the pod count.
	// Normally, the system sets this field for you.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty" protobuf:"bytes,1,opt,name=selector"`

	// Replicas specifies the replicas of this TaskSpec in Job
	Replicas int32 `json:"replicas,omitempty" protobuf:"bytes,2,opt,name=replicas"`

	// Specifies the pod that will be created for this TaskSpec
	// when executing a Job
	Template v1.PodTemplateSpec `json:"template,omitempty" protobuf:"bytes,3,opt,name=template"`

	// Specifies the lifecycle of task
	// +optional
	Lifecycle Lifecycle `json:"lifecycle,omitempty" protobuf:"bytes,4,opt,name=lifecycle"`
}

// JobStatus represents the current state of a Job
type JobStatus struct {
	// The number of pending pods.
	// +optional
	Pending int32 `json:"pending,omitempty" protobuf:"bytes,1,opt,name=pending"`

	// The number of running pods.
	// +optional
	Running int32 `json:"running,omitempty" protobuf:"bytes,2,opt,name=running"`

	// The number of pods which reached phase Succeeded.
	// +optional
	Succeeded int32 `json:"Succeeded,omitempty" protobuf:"bytes,3,opt,name=succeeded"`

	// The number of pods which reached phase Failed.
	// +optional
	Failed int32 `json:"failed,omitempty" protobuf:"bytes,4,opt,name=failed"`

	// The minimal available pods to run for this Job
	// +optional
	MinAvailable int32 `json:"minAvailable,omitempty" protobuf:"bytes,5,opt,name=minAvailable"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type JobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Items []Job `json:"items" protobuf:"bytes,2,rep,name=items"`
}
