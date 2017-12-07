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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const TaskSetPlural = "tasksets"

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TaskSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              TaskSetSpec   `json:"spec"`
	Status            TaskSetStatus `json:"status,omitempty"`
}

type TaskSetSpec struct {
	Priority     int          `json:"priority"`
	ResourceUnit ResourceList `json:"resourceunit"`
	ResourceNo   int          `json:"resourceno"`
	Queue        string       `json:"queue"`
}

type TaskSetStatus struct {
	Allocated ResourceList `json:"allocated"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type TaskSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []TaskSet `json:"items"`
}
