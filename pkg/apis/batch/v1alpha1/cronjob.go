/*
Copyright 2019 The Volcano Authors.

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

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CronJob struct {
	metav1.TypeMeta `json:",inline"`

	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of a CronJob
	Spec CronJobSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Current status of CronJob
	// +optional
	Status CronJobStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

type CronJobSpec struct {
	// Schedule is a cron schedule for Job.
	Schedule string `json:"schedule" protobuf:"bytes,1,opt,name=schedule"`
	// Template is a template based on which the Job should run.
	Template JobSpec `json:"template" protobuf:"bytes,2,opt,name=spec"`
	// Whether to suspend current CronJob
	Suspend *bool `json:"suspend,omitempty" protobuf:"bytes,3,opt,name=suspend"`
}

type CronJobEvent string

const (
	JobTriggered CronJobEvent = "JobTriggered"
)

type CronJobStatus struct {
	// CronState is the current scheduling state of the application.
	State CronJobState `json:"state,omitempty"`
	// Reason shows detail on CronJob.
	Reason string `json:"reason,omitempty"`
	// LastRun is the time when the last job started.
	LastRun metav1.Time `json:"lastRun,omitempty" protobuf:"bytes,1,opt,name=lastRun"`
	// NextRun is the time when the next run of Job will start.
	NextRun metav1.Time `json:"nextRun,omitempty" protobuf:"bytes,2,opt,name=NextRun"`
	// LastRunName is the name of the last run Job.
	LastRunName string `json:"lastRunName,omitempty" protobuf:"bytes,3,opt,name=LastRunName"`
}

type CronJobState string

const (
	Scheduled CronJobState = "Scheduled"
	Stopped   CronJobState = "Stopped"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type CronJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Items []CronJob `json:"items" protobuf:"bytes,2,rep,name=items"`
}
