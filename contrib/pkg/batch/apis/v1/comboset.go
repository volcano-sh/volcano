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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const ComboSetPlural string = "combosets"

// Definition of ComboSet class
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ComboSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              ComboSetSpec   `json:"spec"`
	Status            ComboSetStatus `json:"status,omitempty"`
}

// ComboSetList is a collection of combosets.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ComboSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []ComboSet `json:"items"`
}

// JobSpec describes how the combo set will look like.
type ComboSetSpec struct {
	Priority      int                  `json:"priority,omitempty"`
	Service       ComboSetService      `json:"service"`
	AggrResources ComboSetResourceList `json:"resources"`
}

// ComboSetService is combo set service definition
type ComboSetService struct {
	Spec v1.ServiceSpec `json:"spec"`
}

// ComboSetResource is combo set aggration resource
type ComboSetResource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	MinReplicas       int32                `json:"minreplicas"`
	DesiredReplicas   int32                `json:"desiredreplicas"`
	Priorty           float64              `json:"priority"`
	Type              ResourceType         `json:"type"`
	Template          runtime.RawExtension `json:"template"`
}

//a collection of ComboSetResource
type ComboSetResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []ComboSetResource
}

// queue job resources type
type ResourceType string

//only support resource type pod for now
const (
	ResourceTypePod ResourceType = "Pod"
)

// ComboSetStatus represents the current state of a ComboSet.
type ComboSetStatus struct {
	State         ComboSetState       `json:"state,omitempty"`
	ResourceUsage ResourceUsageStatus `json:"resources,omitempty"`
	Message       string              `json:"message,omitempty"`
}

type ComboSetState string

//enqueued, active, deleting, succeeded, failed
const (
	ComboSetStateEnqueued ComboSetState = "Enqueued"
	ComboSetStateActive   ComboSetState = "Active"
	ComboSetStateDeleted  ComboSetState = "Deleted"
	ComboSetStateFailed   ComboSetState = "Failed"
)

// ResourceUsageStatus represents the current state of a QueueJob.
type ResourceUsageStatus struct {
	Deserved   ResourceList `json:"deserved"`
	Allocated  ResourceList `json:"allocated"`
	Used       ResourceList `json:"used"`
	Preempting ResourceList `json:"preempting"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ResourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Resources       map[ResourceName]resource.Quantity `json:"resources"`
}

type ResourceName string
