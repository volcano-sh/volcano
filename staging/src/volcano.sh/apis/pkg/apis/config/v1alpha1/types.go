/*
Copyright 2025 The Volcano Authors.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ColocationConfigurationSpec defines the desired state of ColocationConfiguration
type ColocationConfigurationSpec struct {
	// Selector is a label selector to match the target pods
	// +optional
	Selector *metav1.LabelSelector `json:"selector,omitempty" protobuf:"bytes,1,opt,name=selector"`

	// Configuration defines the colocation configuration for the target pods
	Configuration `json:",inline" protobuf:"bytes,2,opt,name=options"`
}

type Configuration struct {
	// MemoryQos defines the memory QoS configuration
	// +optional
	MemoryQos *MemoryQos `json:"memoryQos,omitempty" protobuf:"bytes,1,opt,name=memoryQos"`
}

type MemoryQos struct {
	// HighRatio is the memory throttling ratio; default=100, range: 0~100
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=100
	HighRatio int `json:"highRatio,omitempty" protobuf:"varint,1,opt,name=highRatio"`

	// LowRatio is the memory priority protection ratio; default=0, range: 0~100
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=0
	LowRatio int `json:"lowRatio,omitempty" protobuf:"varint,2,opt,name=lowRatio"`

	// MinRatio is the absolute memory protection ratio; default=0, range: 0~100
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	// +kubebuilder:default=0
	MinRatio int `json:"minRatio,omitempty" protobuf:"varint,3,opt,name=minRatio"`
}

// ColocationConfigurationStatus defines the observed state of ColocationConfiguration.
type ColocationConfigurationStatus struct {
	// conditions represent the current state of the ColocationConfiguration resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty" protobuf:"bytes,1,rep,name=conditions"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ColocationConfiguration is the Schema for the colocationconfigurations API
type ColocationConfiguration struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero" protobuf:"bytes,1,opt,name=metadata"`

	// spec defines the desired state of ColocationConfiguration
	// +required
	Spec ColocationConfigurationSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// status defines the observed state of ColocationConfiguration
	// +optional
	Status ColocationConfigurationStatus `json:"status,omitempty,omitzero" protobuf:"bytes,3,opt,name=status"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// ColocationConfigurationList contains a list of ColocationConfiguration
type ColocationConfigurationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []ColocationConfiguration `json:"items" protobuf:"bytes,2,rep,name=items"`
}
