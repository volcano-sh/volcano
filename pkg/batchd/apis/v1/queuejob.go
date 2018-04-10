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
)

const QueueJobPlural = "queuejobs"

const QueueJobType = "QueueJob"

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type QueueJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	// Specification of the desired behavior of a cron job, including the minAvailable
	Spec QueueJobSpec `json:"spec"`

	// Current status of QueueJob
	Status QueueJobStatus `json:"status"`
}

// QueueJobSpec describes how the job execution will look like and when it will actually run
type QueueJobSpec struct {
	// Replicas is the number of desired replicas
	Replicas int32 `json:"replicas"`

	// The minimal available pods to run for this QueueJob; the default value is nil
	MinAvailable *int32 `json:"minavailable"`

	// The controller of Pods; two valid value as follow:
	//   k8s: the pods are managed by QueueJobController, e.g. creation, termination; default value
	//   customized: the pods are managed by customized controller, QueueJobController
	//               only update status accordingly
	Controller string `json:"controller"`

	// Specifies the pod that will be created when executing a QueueJob
	Template v1.PodTemplateSpec `json:"template"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type QueueJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []QueueJob `json:"items"`
}

// QueueJobStatus represents the current state of a QueueJob
type QueueJobStatus struct {
	// The number of actively running pods.
	// +optional
	Running int32 `json:"running"`

	// The number of pods which reached phase Succeeded.
	// +optional
	Succeeded int32 `json:"succeeded"`

	// The number of pods which reached phase Failed.
	// +optional
	Failed int32 `json:"failed"`

	// The minimal available pods to run for this QueueJob
	// +optional
	MinAvailable int32 `json:"minavailable"`
}

const (
	K8sController        string = "k8s"
	CustomizedController string = "customized"
)
