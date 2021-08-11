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

package cache

import (
	v1 "k8s.io/api/core/v1"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
)

// Cache Interface.
type Cache interface {
	Run(stopCh <-chan struct{})

	Get(key string) (*apis.JobInfo, error)
	GetStatus(key string) (*v1alpha1.JobStatus, error)
	Add(obj *v1alpha1.Job) error
	Update(obj *v1alpha1.Job) error
	Delete(obj *v1alpha1.Job) error

	AddPod(pod *v1.Pod) error
	UpdatePod(pod *v1.Pod) error
	DeletePod(pod *v1.Pod) error

	TaskCompleted(jobKey, taskName string) bool
	TaskFailed(jobKey, taskName string) bool
}
