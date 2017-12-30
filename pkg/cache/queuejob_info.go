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

package cache

import (
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
)

type QueueJobInfo struct {
	Name     string
	QueueJob *arbv1.QueueJob

	QueueName string

	// The total resources of running Pods belong to this QueueJob
	//   * UnderUsed: Used < Allocated
	//   * Meet: Used == Allocated
	//   * OverUsed: Used > Allocated
	Used *Resource

	// The total resources that a QueueJob can get currently, it's expected to
	// be equal or less than `Deserved` (when Preemption try to reclaim resource
	// for this QueueJob)
	Allocated *Resource

	TaskRequest *Resource
	// The total task number of this QueueJob
	TaskNum int
}

func NewQueueJobInfo(queueJob *arbv1.QueueJob) *QueueJobInfo {
	return &QueueJobInfo{
		Name:        queueJob.Name,
		QueueJob:    queueJob,
		QueueName:   queueJob.Spec.Queue,
		Used:        EmptyResource(),
		Allocated:   EmptyResource(),
		TaskRequest: NewResource(queueJob.Spec.ResourceUnit),
		TaskNum:     queueJob.Spec.ResourceNo,
	}
}

func (qj *QueueJobInfo) Clone() *QueueJobInfo {
	return &QueueJobInfo{
		Name:      qj.Name,
		QueueJob:  qj.QueueJob,
		QueueName: qj.QueueName,
		Used:      qj.Used.Clone(),
		Allocated: qj.Allocated.Clone(),

		TaskRequest: qj.TaskRequest.Clone(),
		TaskNum:     qj.TaskNum,
	}
}
func (qj *QueueJobInfo) UnderUsed() bool {
	return qj.Used.Less(qj.Allocated)
}
