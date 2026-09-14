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
	// +optional
	Allocatable string `json:"allocatable,omitempty" protobuf:"bytes,1,opt,name=allocatable"`
	// +optional
	Capacity int `json:"capacity,omitempty" protobuf:"varint,2,opt,name=capacity"`
}

// CPUInfo is the cpu topology detail
type CPUInfo struct {
	// +kubebuilder:validation:Minimum=0
	// +optional
	NUMANodeID int `json:"numa,omitempty" protobuf:"varint,1,opt,name=numa"`
	// +kubebuilder:validation:Minimum=0
	// +optional
	SocketID int `json:"socket,omitempty" protobuf:"varint,2,opt,name=socket"`
	// +kubebuilder:validation:Minimum=0
	// +optional
	CoreID int `json:"core,omitempty" protobuf:"varint,3,opt,name=core"`
}

// PolicyName is the policy name type
// +kubebuilder:validation:Enum=CPUManagerPolicy;TopologyManagerPolicy
type PolicyName string

const (
	// CPUManagerPolicy shows cpu manager policy type
	CPUManagerPolicy PolicyName = "CPUManagerPolicy"
	// TopologyManagerPolicy shows topology manager policy type
	TopologyManagerPolicy PolicyName = "TopologyManagerPolicy"
)

// ContainerAllocation records the numa resource allocation for a single container
type ContainerAllocation struct {
	// Specifies the name of the container
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// Specifies the numa resource allocations of the container
	// Key is resource name, e.g., "cpu", "memory", "nvidia.com/gpu"
	// For "cpu", the value is a cpuset string, e.g., "0,2-4"
	// For other resources, the value is a quantity string, e.g., "2", "4Gi"
	// +optional
	Allocations map[string]string `json:"allocations,omitempty" protobuf:"bytes,2,rep,name=allocations"`
}

// PodAllocation records the numa resource allocation for all containers in a pod
type PodAllocation struct {
	// Specifies the uid of the pod
	// +optional
	UID string `json:"uid,omitempty" protobuf:"bytes,1,opt,name=uid"`

	// Specifies the name of the pod
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,2,opt,name=name"`

	// Specifies the namespace of the pod
	// +optional
	Namespace string `json:"namespace,omitempty" protobuf:"bytes,3,opt,name=namespace"`

	// Specifies the per-container numa resource allocation of the pod
	// +optional
	ContainerAllocations []ContainerAllocation `json:"containerAllocations,omitempty" protobuf:"bytes,4,rep,name=containerAllocations"`
}

// NumatopoSpec defines the desired state of Numatopology
type NumatopoSpec struct {
	// Specifies the policy of the manager
	// +optional
	Policies map[PolicyName]string `json:"policies,omitempty" protobuf:"bytes,1,rep,name=policies"`

	// Specifies the reserved resource of the node
	// Key is resource name
	// +optional
	ResReserved map[string]string `json:"resReserved,omitempty" protobuf:"bytes,2,rep,name=resReserved"`

	// Specifies the numa info for the resource
	// Key is resource name
	// +optional
	NumaResMap map[string]ResourceInfo `json:"numares,omitempty" protobuf:"bytes,3,rep,name=numares"`

	// Specifies the cpu topology info
	// Key is cpu id
	// +optional
	CPUDetail map[string]CPUInfo `json:"cpuDetail,omitempty" protobuf:"bytes,4,rep,name=cpuDetail"`

	// Specifies the per-pod numa resource allocation of the node
	// +optional
	PodAllocations []PodAllocation `json:"podAllocations,omitempty" protobuf:"bytes,5,rep,name=podAllocations"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=numatopo,scope=Cluster
// +kubebuilder:metadata:annotations="helm.sh/resource-policy=keep"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Numatopology is the Schema for the Numatopologies API
type Numatopology struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the numa information of the worker node
	Spec NumatopoSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NumatopologyList contains a list of Numatopology
type NumatopologyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []Numatopology `json:"items" protobuf:"bytes,2,rep,name=items"`
}
