/*
Copyright 2021 The Volcano Authors.

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ResourceInfo is the sets about resource capacity and allocatable
type ResourceInfo struct {
	Allocatable string `json:"allocatable,omitempty"`
	Capacity    int    `json:"capacity,omitempty"`
}

// CPUInfo is the cpu topology detail
type CPUInfo struct {
	NUMANodeID int `json:"numa,omitempty"`
	SocketID   int `json:"socket,omitempty"`
	CoreID     int `json:"core,omitempty"`
}

// PolicyName is the policy name type
type PolicyName string

const (
	// CPUManagerPolicy shows cpu manager policy type
	CPUManagerPolicy PolicyName = "CPUManagerPolicy"
	// TopologyManagerPolicy shows topology manager policy type
	TopologyManagerPolicy PolicyName = "TopologyManagerPolicy"
)

// NumatopoSpec defines the desired state of Numatopology
type NumatopoSpec struct {
	// Specifies the policy of the manager
	// +optional
	Policies map[PolicyName]string `json:"policies,omitempty"`

	// Specifies the reserved resource of the node
	// Key is resource name
	// +optional
	ResReserved map[string]string `json:"resReserved,omitempty"`

	// Specifies the numa info for the resource
	// Key is resource name
	// +optional
	NumaResMap map[string]ResourceInfo `json:"numares,omitempty"`

	// Specifies the cpu topology info
	// Key is cpu id
	// +optional
	CPUDetail map[string]CPUInfo `json:"cpuDetail,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=numatopo,scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Numatopology is the Schema for the Numatopologies API
type Numatopology struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Specification of the numa information of the worker node
	Spec NumatopoSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NumatopologyList contains a list of Numatopology
type NumatopologyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Numatopology `json:"items"`
}
