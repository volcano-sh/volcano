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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	//QueueVersionV1Alpha1 represents PodGroupVersion of V1Alpha1
	QueueVersionV1Alpha1 string = "v1alpha1"

	//QueueVersionV1Alpha2 represents PodGroupVersion of V1Alpha2
	QueueVersionV1Alpha2 string = "v1alpha2"
)

// Queue is a queue of PodGroup.
type Queue struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of the queue.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Spec QueueSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// The status of queue.
	// +optional
	Status QueueStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`

	//Version is used to retrieve information about queue version
	Version string
}

// QueueStatus represents the status of Queue.
type QueueStatus struct {
	// The number of 'Unknown' PodGroup in this queue.
	Unknown int32 `json:"unknown,omitempty" protobuf:"bytes,1,opt,name=unknown"`
	// The number of 'Pending' PodGroup in this queue.
	Pending int32 `json:"pending,omitempty" protobuf:"bytes,2,opt,name=pending"`
	// The number of 'Running' PodGroup in this queue.
	Running int32 `json:"running,omitempty" protobuf:"bytes,3,opt,name=running"`
	// The number of `Inqueue` PodGroup in this queue.
	Inqueue int32 `json:"inqueue,omitempty" protobuf:"bytes,4,opt,name=inqueue"`
}

// QueueSpec represents the template of Queue.
type QueueSpec struct {
	Weight     int32           `json:"weight,omitempty" protobuf:"bytes,1,opt,name=weight"`
	Capability v1.ResourceList `json:"capability,omitempty" protobuf:"bytes,2,opt,name=capability"`
}

// QueueID is UID type, serves as unique ID for each queue
type QueueID types.UID

// QueueInfo will have all details about queue
type QueueInfo struct {
	UID  QueueID
	Name string

	Weight int32

	Queue *Queue
}

// NewQueueInfo creates new queueInfo object
func NewQueueInfo(queue *Queue) *QueueInfo {
	return &QueueInfo{
		UID:  QueueID(queue.Name),
		Name: queue.Name,

		Weight: queue.Spec.Weight,

		Queue: queue,
	}
}

// Clone is used to clone queueInfo object
func (q *QueueInfo) Clone() *QueueInfo {
	return &QueueInfo{
		UID:    q.UID,
		Name:   q.Name,
		Weight: q.Weight,
		Queue:  q.Queue,
	}
}
