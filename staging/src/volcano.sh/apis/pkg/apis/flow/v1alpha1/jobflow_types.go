/*
Copyright 2021.

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
	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

// JobFlowSpec defines the desired state of JobFlow
type JobFlowSpec struct {
	// +optional
	Flows []Flow `json:"flows,omitempty"`
	// +optional
	JobRetainPolicy RetainPolicy `json:"jobRetainPolicy,omitempty"`
}

// Flow defines the dependent of jobs
type Flow struct {
	// +kubebuilder:validation:MinLength=1
	// +required
	Name string `json:"name"`
	// +optional
	DependsOn *DependsOn `json:"dependsOn,omitempty"`
	// +optional
	Patch *Patch `json:"patch,omitempty"`
}

type DependsOn struct {
	// +optional
	Targets []string `json:"targets,omitempty"`
	// +optional
	Probe *Probe `json:"probe,omitempty"`
}

type Patch struct {
	// +optional
	v1alpha1.JobSpec `json:"jobSpec,omitempty"`
}

type Probe struct {
	// +optional
	HttpGetList []HttpGet `json:"httpGetList,omitempty"`
	// +optional
	TcpSocketList []TcpSocket `json:"tcpSocketList,omitempty"`
	// +optional
	TaskStatusList []TaskStatus `json:"taskStatusList,omitempty"`
}

type HttpGet struct {
	// +kubebuilder:validation:MaxLength=253
	// +optional
	TaskName string `json:"taskName,omitempty"`
	// +optional
	Path string `json:"path,omitempty"`
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	Port int `json:"port,omitempty"`
	// +optional
	HTTPHeader v1.HTTPHeader `json:"httpHeader,omitempty"`
}

type TcpSocket struct {
	// +kubebuilder:validation:MaxLength=253
	// +optional
	TaskName string `json:"taskName,omitempty"`
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	// +required
	Port int `json:"port"`
}

type TaskStatus struct {
	// +kubebuilder:validation:MaxLength=253
	// +optional
	TaskName string `json:"taskName,omitempty"`
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Phase string `json:"phase,omitempty"`
}

// JobFlowStatus defines the observed state of JobFlow
type JobFlowStatus struct {
	// +optional
	PendingJobs []string `json:"pendingJobs,omitempty"`
	// +optional
	RunningJobs []string `json:"runningJobs,omitempty"`
	// +optional
	FailedJobs []string `json:"failedJobs,omitempty"`
	// +optional
	CompletedJobs []string `json:"completedJobs,omitempty"`
	// +optional
	TerminatedJobs []string `json:"terminatedJobs,omitempty"`
	// +optional
	UnKnowJobs []string `json:"unKnowJobs,omitempty"`
	// +optional
	JobStatusList []JobStatus `json:"jobStatusList,omitempty"`
	// +optional
	Conditions map[string]Condition `json:"conditions,omitempty"`
	// +optional
	State State `json:"state,omitempty"`
}

type JobStatus struct {
	// +optional
	Name string `json:"name,omitempty"`
	// +optional
	State v1alpha1.JobPhase `json:"state,omitempty"`
	// +optional
	StartTimestamp metav1.Time `json:"startTimestamp,omitempty"`
	// +optional
	EndTimestamp metav1.Time `json:"endTimestamp,omitempty"`
	// +kubebuilder:validation:Minimum=0
	// +optional
	RestartCount int32 `json:"restartCount,omitempty"`
	// +optional
	RunningHistories []JobRunningHistory `json:"runningHistories,omitempty"`
}

type JobRunningHistory struct {
	// +optional
	StartTimestamp metav1.Time `json:"startTimestamp,omitempty"`
	// +optional
	EndTimestamp metav1.Time `json:"endTimestamp,omitempty"`
	// +optional
	State v1alpha1.JobPhase `json:"state,omitempty"`
}

type State struct {
	// +optional
	Phase Phase `json:"phase,omitempty"`
}

// +kubebuilder:validation:Enum=retain;delete
type RetainPolicy string

const (
	Retain RetainPolicy = "retain"
	Delete RetainPolicy = "delete"
)

// +kubebuilder:validation:Enum=Succeed;Terminating;Failed;Running;Pending
type Phase string

const (
	Succeed     Phase = "Succeed"
	Terminating Phase = "Terminating"
	Failed      Phase = "Failed"
	Running     Phase = "Running"
	Pending     Phase = "Pending"
)

type Condition struct {
	Phase           v1alpha1.JobPhase             `json:"phase,omitempty"`
	CreateTimestamp metav1.Time                   `json:"createTime,omitempty"`
	RunningDuration *metav1.Duration              `json:"runningDuration,omitempty"`
	TaskStatusCount map[string]v1alpha1.TaskState `json:"taskStatusCount,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true
//+kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.state.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:resource:path=jobflows,shortName=jf
//+kubebuilder:subresource:status

// JobFlow is the Schema for the jobflows API
type JobFlow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   JobFlowSpec   `json:"spec,omitempty"`
	Status JobFlowStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true

// JobFlowList contains a list of JobFlow
type JobFlowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []JobFlow `json:"items"`
}
