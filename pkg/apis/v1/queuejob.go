/*
Copyright 2017 The Kubernetes Authors.

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

package v1

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const QueueJobPlural string = "queuejobs"

// Definition of QueueJob class
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type QueueJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              QueueJobSpec   `json:"spec"`
	Status            QueueJobStatus `json:"status,omitempty"`
}

// QueueJobList is a collection of queuejobs.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type QueueJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []QueueJob `json:"items"`
}

// JobSpec describes how the queue job will look like.
type QueueJobSpec struct {
	Priority      int                  `json:"priority,omitempty"`
	Service       QueueJobService      `json:"service"`
	AggrResources QueueJobResourceList `json:"resources"`
}

// QueueJobService is queue job service definition
type QueueJobService struct {
	Spec v1.ServiceSpec `json:"spec"`
}

// QueueJobResource is queue job aggration resource
type QueueJobResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	MinReplicas       int32                `json:"minreplicas"`
	DesiredReplicas   int32                `json:"desiredreplicas"`
	Priorty           float64              `json:"priority"`
	Type              ResourceType         `json:"type"`
	Template          runtime.RawExtension `json:"template"`
}

//a collection of QueueJobResource
type QueueJobResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []QueueJobResource
}

// queue job resources type
type ResourceType string

//only support resource type pod for now
const (
	ResourceTypePod ResourceType = "Pod"
)

// QueueJobStatus represents the current state of a QueueJob.
type QueueJobStatus struct {
	State   QueueJobState `json:"state,omitempty"`
	Message string        `json:"message,omitempty"`
}

type QueueJobState string

//enqueued, active, deleting, succeeded, failed
const (
	QueueJobStateEnqueued QueueJobState = "Enqueued"
	QueueJobStateActive   QueueJobState = "Active"
	QueueJobStateDeleted  QueueJobState = "Deleted"
	QueueJobStateFailed   QueueJobState = "Failed"
)
