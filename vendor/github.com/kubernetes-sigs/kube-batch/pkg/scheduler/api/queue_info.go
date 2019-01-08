/*
Copyright 2018 The Kubernetes Authors.

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

package api

import (
	"k8s.io/apimachinery/pkg/types"

	arbcorev1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
)

type QueueID types.UID

type QueueInfo struct {
	UID  QueueID
	Name string

	Weight int32

	Queue *arbcorev1.Queue
}

func NewQueueInfo(queue *arbcorev1.Queue) *QueueInfo {
	return &QueueInfo{
		UID:  QueueID(queue.Name),
		Name: queue.Name,

		Weight: queue.Spec.Weight,

		Queue: queue,
	}
}

func (q *QueueInfo) Clone() *QueueInfo {
	return &QueueInfo{
		UID:    q.UID,
		Name:   q.Name,
		Weight: q.Weight,
		Queue:  q.Queue,
	}
}
