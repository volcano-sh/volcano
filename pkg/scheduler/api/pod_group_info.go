/*
Copyright 2019 The Kubernetes Authors.

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

package api

import (
	"encoding/json"

	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"volcano.sh/volcano/pkg/apis/scheduling/v1alpha1"
	"volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
)

//PodGroupConditionType is of string type which represents podGroup Condition
type PodGroupConditionType string

const (
	//PodGroupUnschedulableType represents unschedulable podGroup condition
	PodGroupUnschedulableType PodGroupConditionType = "Unschedulable"
)

// PodGroupPhase is the phase of a pod group at the current time.
type PodGroupPhase string

// These are the valid phase of podGroups.
const (
	//PodGroupVersionV1Alpha1 represents PodGroupVersion of V1Alpha1
	PodGroupVersionV1Alpha1 string = "v1alpha1"

	//PodGroupVersionV1Alpha2 represents PodGroupVersion of V1Alpha2
	PodGroupVersionV1Alpha2 string = "v1alpha2"
	// PodGroupPending means the pod group has been accepted by the system, but scheduler can not allocate
	// enough resources to it.
	PodGroupPending PodGroupPhase = "Pending"

	// PodGroupRunning means `spec.minMember` pods of PodGroups has been in running phase.
	PodGroupRunning PodGroupPhase = "Running"

	// PodGroupUnknown means part of `spec.minMember` pods are running but the other part can not
	// be scheduled, e.g. not enough resource; scheduler will wait for related controller to recover it.
	PodGroupUnknown PodGroupPhase = "Unknown"

	// PodGroupInqueue means controllers can start to create pods,
	// is a new state between PodGroupPending and PodGroupRunning
	PodGroupInqueue PodGroupPhase = "Inqueue"
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

// PodGroup is a collection of Pod; used for batch workload.
type PodGroup struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of the pod group.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Spec PodGroupSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current information about a pod group.
	// This data may not be up to date.
	// +optional
	Status PodGroupStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`

	//Version represents the version of PodGroup
	Version string
}

// PodGroupSpec represents the template of a pod group.
type PodGroupSpec struct {
	// MinMember defines the minimal number of members/tasks to run the pod group;
	// if there's not enough resources to start all tasks, the scheduler
	// will not start anyone.
	MinMember int32 `json:"minMember,omitempty" protobuf:"bytes,1,opt,name=minMember"`

	// Queue defines the queue to allocate resource for PodGroup; if queue does not exist,
	// the PodGroup will not be scheduled.
	Queue string `json:"queue,omitempty" protobuf:"bytes,2,opt,name=queue"`

	// If specified, indicates the PodGroup's priority. "system-node-critical" and
	// "system-cluster-critical" are two special keywords which indicate the
	// highest priorities with the former being the highest priority. Any other
	// name must be defined by creating a PriorityClass object with that name.
	// If not specified, the PodGroup priority will be default or zero if there is no
	// default.
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty" protobuf:"bytes,3,opt,name=priorityClassName"`

	// MinResources defines the minimal resource of members/tasks to run the pod group;
	// if there's not enough resources to start all tasks, the scheduler
	// will not start anyone.
	MinResources *v1.ResourceList `json:"minResources,omitempty" protobuf:"bytes,4,opt,name=minResources"`
}

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

// ConvertPodGroupInfoToV1alpha1 converts api.PodGroup type to v1alpha1.PodGroup
func ConvertPodGroupInfoToV1alpha1(pg *PodGroup) (*v1alpha1.PodGroup, error) {
	marshalled, err := json.Marshal(*pg)
	if err != nil {
		glog.Errorf("Failed to Marshal podgroup %s with error: %v", pg.Name, err)
	}

	convertedPg := &v1alpha1.PodGroup{}
	err = json.Unmarshal(marshalled, convertedPg)
	if err != nil {
		glog.Errorf("Failed to Unmarshal Data into v1alpha1.PodGroup type with error: %v", err)
	}

	return convertedPg, nil
}

// ConvertV1alpha1ToPodGroupInfo converts v1alpha1.PodGroup to api.PodGroup type
func ConvertV1alpha1ToPodGroupInfo(pg *v1alpha1.PodGroup) (*PodGroup, error) {
	marshalled, err := json.Marshal(*pg)
	if err != nil {
		glog.Errorf("Failed to Marshal podgroup %s with error: %v", pg.Name, err)
	}

	convertedPg := &PodGroup{}
	err = json.Unmarshal(marshalled, convertedPg)
	if err != nil {
		glog.Errorf("Failed to Unmarshal Data into api.PodGroup type with error: %v", err)
	}
	convertedPg.Version = PodGroupVersionV1Alpha1

	return convertedPg, nil
}

// ConvertPodGroupInfoToV1alpha2 converts api.PodGroup type to v1alpha2.PodGroup
func ConvertPodGroupInfoToV1alpha2(pg *PodGroup) (*v1alpha2.PodGroup, error) {
	marshalled, err := json.Marshal(*pg)
	if err != nil {
		glog.Errorf("Failed to Marshal podgroup %s with error: %v", pg.Name, err)
	}

	convertedPg := &v1alpha2.PodGroup{}
	err = json.Unmarshal(marshalled, convertedPg)
	if err != nil {
		glog.Errorf("Failed to Unmarshal Data into v1alpha2.PodGroup type with error: %v", err)
	}

	return convertedPg, nil
}

// ConvertV1alpha2ToPodGroupInfo converts v1alpha2.PodGroup to api.PodGroup type
func ConvertV1alpha2ToPodGroupInfo(pg *v1alpha2.PodGroup) (*PodGroup, error) {
	marshalled, err := json.Marshal(*pg)
	if err != nil {
		glog.Errorf("Failed to Marshal podgroup %s with error: %v", pg.Name, err)
	}

	convertedPg := &PodGroup{}
	err = json.Unmarshal(marshalled, convertedPg)
	if err != nil {
		glog.Errorf("Failed to Unmarshal Data into api.PodGroup type with error: %v", err)
	}
	convertedPg.Version = PodGroupVersionV1Alpha2

	return convertedPg, nil
}
