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
	"k8s.io/apimachinery/pkg/runtime"
)

const XQueueJobPlural string = "xqueuejobs"

// Definition of QueueJob class
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type XQueueJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              XQueueJobSpec   `json:"spec"`
	Status            XQueueJobStatus `json:"status,omitempty"`
}

// QueueJobList is a collection of queuejobs.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type XQueueJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []XQueueJob `json:"items"`
}

// JobSpec describes how the queue job will look like.
type XQueueJobSpec struct {
	Priority      int                   `json:"priority,omitempty"`
	Service       XQueueJobService      `json:"service"`
	AggrResources XQueueJobResourceList `json:"resources"`

	Selector *metav1.LabelSelector `json:"selector,omitempty" protobuf:"bytes,1,opt,name=selector"`

	// SchedSpec specifies the parameters for scheduling.
	SchedSpec SchedulingSpecTemplate `json:"schedulingSpec,omitempty" protobuf:"bytes,2,opt,name=schedulingSpec"`
}

// QueueJobService is queue job service definition
type XQueueJobService struct {
	Spec v1.ServiceSpec `json:"spec"`
}

// QueueJobSecret is queue job service definition
//type QueueJobSecret struct {
//	Spec v1.SecretSpec `json:"spec"`
//}

// QueueJobResource is queue job aggregation resource
type XQueueJobResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	// Replicas is the number of desired replicas
	Replicas int32 `json:"replicas,omitempty" protobuf:"bytes,2,opt,name=replicas"`

	// The minimal available pods to run for this QueueJob; the default value is nil
	MinAvailable *int32 `json:"minavailable,omitempty" protobuf:"bytes,3,opt,name=minavailable"`

	// The number of allocated replicas from this resource type
	AllocatedReplicas int32 `json:"allocatedreplicas"`

	// The priority of this resource
	Priority float64 `json:"priority"`

	//The type of the resource (is the resource a Pod, a ReplicaSet, a ... ?)
	Type ResourceType `json:"type"`

	//The template for the resource; it is now a raw text because we don't know for what resource
	//it should be instantiated
	Template runtime.RawExtension `json:"template"`
}

// a collection of QueueJobResource
type XQueueJobResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []XQueueJobResource
}

// queue job resources type
type ResourceType string

const (
	ResourceTypePod         ResourceType = "Pod"
	ResourceTypeService     ResourceType = "Service"
	ResourceTypeSecret      ResourceType = "Secret"
	ResourceTypeStatefulSet ResourceType = "StatefulSet"
	ResourceTypeDeployment  ResourceType = "Deployment"
	ResourceTypeReplicaSet  ResourceType = "ReplicaSet"
)

// QueueJobStatus represents the current state of a QueueJob
type XQueueJobStatus struct {
	// The number of pending pods.
	// +optional
	Pending int32 `json:"pending,omitempty" protobuf:"bytes,1,opt,name=pending"`

	// +optional
	Running int32 `json:"running,omitempty" protobuf:"bytes,1,opt,name=running"`

	// The number of resources which reached phase Succeeded.
	// +optional
	Succeeded int32 `json:"Succeeded,omitempty" protobuf:"bytes,2,opt,name=succeeded"`

	// The number of resources which reached phase Failed.
	// +optional
	Failed int32 `json:"failed,omitempty" protobuf:"bytes,3,opt,name=failed"`

	// The minimal available resources to run for this QueueJob (is this different from the MinAvailable from JobStatus)
	// +optional
	MinAvailable int32 `json:"template,omitempty" protobuf:"bytes,4,opt,name=template"`
	
	//Can run?
	CanRun bool `json:"canrun,omitempty" protobuf:"bytes,1,opt,name=canrun"`
	
	//State - Running, Queued, Deleted ?
	State             XQueueJobState `json:"state,omitempty"`

	Message string `json:"message,omitempty"`
}

type XQueueJobState string

//enqueued, active, deleting, succeeded, failed
const (
	QueueJobStateEnqueued XQueueJobState = "Pending"
	QueueJobStateActive   XQueueJobState = "Running"
	QueueJobStateDeleted  XQueueJobState = "Deleted"
	QueueJobStateFailed   XQueueJobState = "Failed"
)

