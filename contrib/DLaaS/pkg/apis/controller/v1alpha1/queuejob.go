/*
Copyright 2018 The Kubernetes Authors.

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

const QueueJobPlural = "queuejobs"

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type QueueJob struct {
	metav1.TypeMeta `json:",inline"`

	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of a cron job, including the minAvailable
	Spec QueueJobSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Current status of QueueJob
	Status QueueJobStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// QueueJobSpec describes how the job execution will look like and when it will actually run
type QueueJobSpec struct {
	SchedulerName string `json:"schedulerName,omitempty" protobuf:"bytes,1,opt,name=schedulerName"`

	// SchedSpec specifies the parameters for scheduling.
	SchedSpec SchedulingSpecTemplate `json:"schedulingSpec,omitempty" protobuf:"bytes,2,opt,name=schedulingSpec"`

	// TaskSpecs specifies the task specification of QueueJob
	TaskSpecs []TaskSpec `json:"taskSpecs,omitempty" protobuf:"bytes,3,opt,name=taskSpecs"`
}

// TaskSpec specifies the task specification of QueueJob
type TaskSpec struct {
	// A label query over pods that should match the pod count.
	// Normally, the system sets this field for you.
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty" protobuf:"bytes,1,opt,name=selector"`

	// Replicas specifies the replicas of this TaskSpec in QueueJob.
	Replicas int32 `json:"replicas,omitempty" protobuf:"bytes,2,opt,name=replicas"`

	// Specifies the pod that will be created for this TaskSpec
	// when executing a QueueJob
	Template v1.PodTemplateSpec `json:"template,omitempty" protobuf:"bytes,3,opt,name=template"`
}

// QueueJobStatus represents the current state of a QueueJob
type QueueJobStatus struct {
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

	// The minimal available pods to run for this QueueJob
	// +optional
	MinAvailable int32 `json:"minAvailable,omitempty" protobuf:"bytes,5,opt,name=minAvailable"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type QueueJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Items []QueueJob `json:"items" protobuf:"bytes,2,rep,name=items"`
}
