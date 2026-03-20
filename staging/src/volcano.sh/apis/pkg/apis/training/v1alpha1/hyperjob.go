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
	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=hyperjobs,shortName=hyperjob;hj
// +kubebuilder:subresource:status
type HyperJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Spec   HyperJobSpec   `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`
	Status HyperJobStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

type HyperJobSpec struct {
	// ReplicatedJobs is a group of volcano jobs managed by the hyperjob.
	// +listType=map
	// +listMapKey=name
	ReplicatedJobs []ReplicatedJob `json:"replicatedJobs,omitempty" protobuf:"bytes,1,opt,name=replicatedJobs"`
	// MinAvailable is the minimal available volcano jobs to run the hyperjob.
	// +optional
	MinAvailable *int32 `json:"minAvailable,omitempty" protobuf:"varint,2,opt,name=minAvailable"`
	// MaxDomains is the maximum number of domains to split the hyperjob, used in multi-cluster splitting.
	// +optional
	MaxDomains *int32 `json:"maxDomains,omitempty" protobuf:"varint,3,opt,name=maxDomains"`
	// Plugins specifies the plugins to be enabled for the hyperjob.
	// Key is the plugin name, and value is the list of arguments for the plugin.
	Plugins map[string][]string `json:"plugins,omitempty" protobuf:"bytes,4,opt,name=plugins"`
}

type ReplicatedJob struct {
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
	// TemplateSpec is the spec of the volcano job that will be replicated.
	TemplateSpec v1alpha1.JobSpec `json:"templateSpec" protobuf:"bytes,2,opt,name=templateSpec"`
	// SplitPolicy is used in multi-cluster splitting to specify the splitting strategy,
	// including the number and types of accelerators that need to be split
	SplitPolicy *SplitPolicy `json:"splitPolicy,omitempty" protobuf:"bytes,3,opt,name=splitPolicy"`
	// Replicas is the number of replicated volcano jobs.
	// +kubebuilder:default=1
	Replicas int32 `json:"replicas,omitempty" protobuf:"varint,4,opt,name=replicas"`
	// ClusterNames is the list of cluster names to which the replicated jobs prefer to be scheduled.
	ClusterNames []string `json:"clusterNames,omitempty" protobuf:"bytes,5,opt,name=clusterNames"`
}

type SplitPolicy struct {
	// Mode is the mode of the split policy.
	// +kubebuilder:validation:Enum=static;auto
	Mode SplitMode `json:"mode,omitempty" protobuf:"bytes,1,opt,name=mode"`
	// Accelerators is the number of accelerators to split.
	Accelerators *int `json:"accelerators,omitempty" protobuf:"varint,2,opt,name=accelerators"`
	// AcceleratorType is the type of the accelerator. Such as nvidia.com/gpu, amd.com/gpu, etc.
	AcceleratorType *string `json:"acceleratorType,omitempty" protobuf:"bytes,3,opt,name=acceleratorType"`
}

type SplitMode string

const (
	SplitModeStatic SplitMode = "static"
	SplitModeAuto   SplitMode = "auto"
)

type HyperJobStatus struct {
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" protobuf:"bytes,1,opt,name=conditions"`
	// ReplicatedJobsStatus tracks the status of each replicated job.
	// +listType=map
	// +listMapKey=name
	ReplicatedJobsStatus []ReplicatedJobStatus `json:"replicatedJobsStatus,omitempty" protobuf:"bytes,2,opt,name=replicatedJobsStatus"`
	// SplitCount represents the total number of volcano jobs this hyperjob is split into by the controller.
	// +optional
	SplitCount *int32 `json:"splitCount,omitempty" protobuf:"varint,3,opt,name=splitCount"`
	// The generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty" protobuf:"varint,4,opt,name=observedGeneration"`
}

type ReplicatedJobStatus struct {
	// Name of the replicated job.
	Name string `json:"name" protobuf:"bytes,1,opt,name=name"`
	// JobStates stores the state of each volcano job created by the replicated job.
	JobStates map[string]v1alpha1.JobState `json:"jobStates,omitempty" protobuf:"bytes,2,opt,name=jobStates"`
	// Pending is the total number of pods under the replicated job in pending state.
	Pending int32 `json:"pending,omitempty" protobuf:"varint,3,opt,name=pending"`
	// Running is the total number of pods under the replicated job in running state.
	Running int32 `json:"running,omitempty" protobuf:"varint,4,opt,name=running"`
	// Succeeded is the total number of pods under the replicated job in succeeded state.
	Succeeded int32 `json:"succeeded,omitempty" protobuf:"varint,5,opt,name=succeeded"`
	// Failed is the total number of pods under the replicated job in failed state.
	Failed int32 `json:"failed,omitempty" protobuf:"varint,6,opt,name=failed"`
	// Terminating is the total number of pods under the replicated job in terminating state.
	Terminating int32 `json:"terminating,omitempty" protobuf:"varint,7,opt,name=terminating"`
	// Unknown is the total number of pods under the replicated job in unknown state.
	Unknown int32 `json:"unknown,omitempty" protobuf:"varint,8,opt,name=unknown"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// HyperJobList defines the list of hyperjobs.
type HyperJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Items []HyperJob `json:"items" protobuf:"bytes,2,rep,name=items"`
}
