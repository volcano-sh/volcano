/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2026 The Volcano Authors.

Modifications made by Volcano authors:
- Added hierarchical queue support with weight and hierarchy configuration
- Enhanced queue management with reclaimable resource controls
- Migrated to v1beta1 API from v1alpha1/v1alpha2 for improved stability

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
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	commonutil "volcano.sh/volcano/pkg/util"
)

// QueueID is UID type, serves as unique ID for each queue
type QueueID types.UID

type QueueScope string

const (
	ClusterQueueScope   QueueScope = "cluster"
	NamespaceQueueScope QueueScope = "namespace"

	DefaultQueueWeight int32 = 1
)

// QueueInfo will have all details about queue
type QueueInfo struct {
	UID               QueueID
	Name              string
	Scope             QueueScope
	Namespace         string
	CreationTimestamp metav1.Time
	Parent            QueueID
	Weight            int32

	// Weights is a list of slash sperated float numbers.
	// Each of them is a weight corresponding the
	// hierarchy level.
	Weights string
	// Hierarchy is a list of node name along the
	// path from the root to the node itself.
	Hierarchy string

	Capability      v1.ResourceList
	Guarantee       scheduling.Guarantee
	Deserved        v1.ResourceList
	ReclaimableFlag *bool
	Priority        int32
	DequeueStrategy scheduling.DequeueStrategy

	State       scheduling.QueueState
	Allocated   v1.ResourceList
	Reservation scheduling.Reservation

	// Affinity and Annotations are normalized metadata consumed by plugins.
	Affinity    *scheduling.Affinity
	Annotations map[string]string

	// Queue is the source cluster Queue. It is nil for NamespaceQueue.
	Queue *scheduling.Queue
	// NamespaceQueue is the source NamespaceQueue. It is nil for cluster Queue.
	NamespaceQueue *scheduling.NamespaceQueue
}

// IsOpen reports whether the queue can accept scheduling work.
func (q *QueueInfo) IsOpen() bool {
	return q != nil && q.State == scheduling.QueueStateOpen
}

// NewQueueInfo creates new queueInfo object
func NewQueueInfo(queue *scheduling.Queue) *QueueInfo {
	return &QueueInfo{
		UID:               QueueID(queue.Name),
		Name:              queue.Name,
		Scope:             ClusterQueueScope,
		CreationTimestamp: queue.CreationTimestamp,
		Parent:            QueueID(queue.Spec.Parent),

		Weight:    queue.Spec.Weight,
		Hierarchy: queue.Annotations[v1beta1.KubeHierarchyAnnotationKey],
		Weights:   queue.Annotations[v1beta1.KubeHierarchyWeightAnnotationKey],

		Capability:      queue.Spec.Capability.DeepCopy(),
		Guarantee:       cloneGuarantee(queue.Spec.Guarantee),
		Deserved:        queue.Spec.Deserved.DeepCopy(),
		ReclaimableFlag: cloneBool(queue.Spec.Reclaimable),
		Priority:        queue.Spec.Priority,
		DequeueStrategy: queue.Spec.DequeueStrategy,

		State:       queue.Status.State,
		Allocated:   queue.Status.Allocated.DeepCopy(),
		Reservation: cloneReservation(queue.Status.Reservation),

		Affinity:    cloneAffinity(queue.Spec.Affinity),
		Annotations: cloneStringMap(queue.Annotations),

		Queue: queue,
	}
}

func namespaceQueueParentID(nq *scheduling.NamespaceQueue) (QueueID, error) {
	target, err := commonutil.ResolveNamespaceQueueParentReference(
		nq.Namespace,
		nq.Spec.Parent,
	)
	if err != nil {
		return "", err
	}

	if target.Scope == commonutil.ClusterQueueReferenceScope {
		return QueueID(target.Name), nil
	}

	return NamespaceQueueID(target.Namespace, target.Name), nil
}

func NamespaceQueueID(namespace, name string) QueueID {
	return QueueID(namespace + "/" + name)
}

func NewNamespaceQueueInfo(nq *scheduling.NamespaceQueue) (*QueueInfo, error) {
	if nq == nil {
		return nil, fmt.Errorf("namespace queue is nil")
	}

	parent, err := namespaceQueueParentID(nq)
	if err != nil {
		return nil, err
	}

	return &QueueInfo{
		UID:               NamespaceQueueID(nq.Namespace, nq.Name),
		Name:              nq.Name,
		Scope:             NamespaceQueueScope,
		Namespace:         nq.Namespace,
		CreationTimestamp: nq.CreationTimestamp,
		Parent:            parent,
		Weight:            DefaultQueueWeight,

		Capability:      nq.Spec.Capability.DeepCopy(),
		Guarantee:       cloneGuarantee(nq.Spec.Guarantee),
		Deserved:        nq.Spec.Deserved.DeepCopy(),
		ReclaimableFlag: cloneBool(nq.Spec.Reclaimable),
		Priority:        nq.Spec.Priority,
		DequeueStrategy: nq.Spec.DequeueStrategy,

		State:       nq.Status.State,
		Allocated:   nq.Status.Allocated.DeepCopy(),
		Reservation: cloneReservation(nq.Status.Reservation),

		Annotations: cloneStringMap(nq.Annotations),

		NamespaceQueue: nq.DeepCopy(),
	}, nil
}

// Clone is used to clone queueInfo object
func (q *QueueInfo) Clone() *QueueInfo {
	clone := &QueueInfo{
		UID:               q.UID,
		Name:              q.Name,
		Scope:             q.Scope,
		Namespace:         q.Namespace,
		CreationTimestamp: q.CreationTimestamp,
		Parent:            q.Parent,

		Weight:    q.Weight,
		Hierarchy: q.Hierarchy,
		Weights:   q.Weights,

		Capability:      q.Capability.DeepCopy(),
		Guarantee:       cloneGuarantee(q.Guarantee),
		Deserved:        q.Deserved.DeepCopy(),
		ReclaimableFlag: cloneBool(q.ReclaimableFlag),
		Priority:        q.Priority,
		DequeueStrategy: q.DequeueStrategy,

		State:       q.State,
		Allocated:   q.Allocated.DeepCopy(),
		Reservation: cloneReservation(q.Reservation),
		Affinity:    cloneAffinity(q.Affinity),
		Annotations: cloneStringMap(q.Annotations),
	}
	if q.Queue != nil {
		clone.Queue = q.Queue.DeepCopy()
	}
	if q.NamespaceQueue != nil {
		clone.NamespaceQueue = q.NamespaceQueue.DeepCopy()
	}

	return clone
}

// Reclaimable return whether queue is reclaimable
func (q *QueueInfo) Reclaimable() bool {
	if q == nil {
		return false
	}

	if q.ReclaimableFlag == nil {
		return true
	}

	return *q.ReclaimableFlag
}

func cloneGuarantee(guarantee scheduling.Guarantee) scheduling.Guarantee {
	return scheduling.Guarantee{Resource: guarantee.Resource.DeepCopy()}
}

func cloneReservation(reservation scheduling.Reservation) scheduling.Reservation {
	return scheduling.Reservation{
		Nodes:    append([]string(nil), reservation.Nodes...),
		Resource: reservation.Resource.DeepCopy(),
	}
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}

	clone := *value
	return &clone
}

func cloneAffinity(value *scheduling.Affinity) *scheduling.Affinity {
	if value == nil {
		return nil
	}

	return value.DeepCopy()
}

func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}

	clone := make(map[string]string, len(value))
	for key, item := range value {
		clone[key] = item
	}

	return clone
}
