/*
Copyright 2014 The Kubernetes Authors.

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

package queuejobresources

import (
	qjobv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	schedulerapi "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/api"

)

// Interface is an abstract interface for queue job resource management.
type Interface interface {
	SyncQueueJob(queuejob *qjobv1.XQueueJob, qjobRes *qjobv1.XQueueJobResource) error
	UpdateQueueJobStatus(queuejob *qjobv1.XQueueJob) error
	GetAggregatedResources(queuejob *qjobv1.XQueueJob) *schedulerapi.Resource
	GetAggregatedResourcesByPriority(priority int, queuejob *qjobv1.XQueueJob) *schedulerapi.Resource
	Cleanup(queuejob *qjobv1.XQueueJob, qjobRes *qjobv1.XQueueJobResource) error
	Run(stopCh <-chan struct{})
}
