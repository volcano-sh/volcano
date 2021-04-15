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

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// QueueID is UID type, serves as unique ID for each queue
type QueueID types.UID

// QueueInfo will have all details about queue
type QueueInfo struct {
	UID  QueueID
	Name string

	Weight int32

	// Weights is a list of slash sperated float numbers.
	// Each of them is a weight corresponding the
	// hierarchy level.
	Weights string
	// Hierarchy is a list of node name along the
	// path from the root to the node itself.
	Hierarchy string

	Queue *scheduling.Queue
}

// NewQueueInfo creates new queueInfo object
func NewQueueInfo(queue *scheduling.Queue) *QueueInfo {
	return &QueueInfo{
		UID:  QueueID(queue.Name),
		Name: queue.Name,

		Weight:    queue.Spec.Weight,
		Hierarchy: queue.Annotations[v1beta1.KubeHierarchyAnnotationKey],
		Weights:   queue.Annotations[v1beta1.KubeHierarchyWeightAnnotationKey],

		Queue: queue,
	}
}

// Clone is used to clone queueInfo object
func (q *QueueInfo) Clone() *QueueInfo {
	return &QueueInfo{
		UID:       q.UID,
		Name:      q.Name,
		Weight:    q.Weight,
		Hierarchy: q.Hierarchy,
		Weights:   q.Weights,
		Queue:     q.Queue,
	}
}

// Reclaimable return whether queue is reclaimable
func (q *QueueInfo) Reclaimable() bool {
	if q == nil {
		return false
	}

	if q.Queue == nil {
		return false
	}

	if q.Queue.Spec.Reclaimable == nil {
		return true
	}

	return *q.Queue.Spec.Reclaimable
}
