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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// JobFlowSpec defines the desired state of JobFlow
type JobFlowSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of JobFlow. Edit jobflow_types.go to remove/update
	Flows           []Flow `json:"flows,omitempty"`
	JobRetainPolicy string `json:"jobRetainPolicy,omitempty"`
}

// Flow defines the dependent of jobs
type Flow struct {
	Name      string     `json:"name"`
	DependsOn *DependsOn `json:"dependsOn,omitempty"`
}

type DependsOn struct {
	Targets []string `json:"targets,omitempty"`
	Probe   *Probe   `json:"probe,omitempty"`
}

type Probe struct {
	HttpGetList    []HttpGet    `json:"httpGetList,omitempty"`
	TcpSocketList  []TcpSocket  `json:"tcpSocketList,omitempty"`
	TaskStatusList []TaskStatus `json:"taskStatusList,omitempty"`
}

type HttpGet struct {
	TaskName   string        `json:"taskName,omitempty"`
	Path       string        `json:"path,omitempty"`
	Port       int           `json:"port,omitempty"`
	HTTPHeader v1.HTTPHeader `json:"httpHeader,omitempty"`
}

type TcpSocket struct {
	TaskName string `json:"taskName,omitempty"`
	Port     int    `json:"port"`
}

type TaskStatus struct {
	TaskName string `json:"taskName,omitempty"`
	Phase    string `json:"phase,omitempty"`
}

// JobFlowStatus defines the observed state of JobFlow
type JobFlowStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	PendingJobs    []string             `json:"pendingJobs,omitempty"`
	RunningJobs    []string             `json:"runningJobs,omitempty"`
	FailedJobs     []string             `json:"failedJobs,omitempty"`
	CompletedJobs  []string             `json:"completedJobs,omitempty"`
	TerminatedJobs []string             `json:"terminatedJobs,omitempty"`
	UnKnowJobs     []string             `json:"unKnowJobs,omitempty"`
	JobStatusList  []JobStatus          `json:"jobStatusList,omitempty"`
	Conditions     map[string]Condition `json:"conditions,omitempty"`
	State          State                `json:"state,omitempty"`
}

type JobStatus struct {
	Name             string              `json:"name,omitempty"`
	State            v1alpha1.JobPhase   `json:"state,omitempty"`
	StartTimestamp   metav1.Time         `json:"startTimestamp,omitempty"`
	EndTimestamp     metav1.Time         `json:"endTimestamp,omitempty"`
	RestartCount     int32               `json:"restartCount,omitempty"`
	RunningHistories []JobRunningHistory `json:"runningHistories,omitempty"`
}

type JobRunningHistory struct {
	StartTimestamp metav1.Time       `json:"startTimestamp,omitempty"`
	EndTimestamp   metav1.Time       `json:"endTimestamp,omitempty"`
	State          v1alpha1.JobPhase `json:"state,omitempty"`
}

type State struct {
	Phase Phase `json:"phase,omitempty"`
}

type Phase string

const (
	Retain = "retain"
	Delete = "delete"
)

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
