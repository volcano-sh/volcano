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

package scheduling

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodGroupPhase is the phase of a pod group at the current time.
type PodGroupPhase string

// QueueState is state type of queue.
type QueueState string

const (
	// QueueStateOpen indicate `Open` state of queue
	QueueStateOpen QueueState = "Open"
	// QueueStateClosed indicate `Closed` state of queue
	QueueStateClosed QueueState = "Closed"
	// QueueStateClosing indicate `Closing` state of queue
	QueueStateClosing QueueState = "Closing"
	// QueueStateUnknown indicate `Unknown` state of queue
	QueueStateUnknown QueueState = "Unknown"
)

// These are the valid phase of podGroups.
const (
	// PodGroupPending means the pod group has been accepted by the system, but scheduler can not allocate
	// enough resources to it.
	PodGroupPending PodGroupPhase = "Pending"

	// PodGroupRunning means `spec.minMember` pods of PodGroup has been in running phase.
	PodGroupRunning PodGroupPhase = "Running"

	// PodGroupUnknown means part of `spec.minMember` pods are running but the other part can not
	// be scheduled, e.g. not enough resource; scheduler will wait for related controller to recover it.
	PodGroupUnknown PodGroupPhase = "Unknown"

	// PodGroupInqueue means controllers can start to create pods,
	// is a new state between PodGroupPending and PodGroupRunning
	PodGroupInqueue PodGroupPhase = "Inqueue"

	// PodGroupCompleted means all the pods of PodGroup are completed
	PodGroupCompleted PodGroupPhase = "Completed"
)

type PodGroupConditionType string

const (
	// PodGroupUnschedulableType is Unschedulable event type
	PodGroupUnschedulableType PodGroupConditionType = "Unschedulable"

	// PodGroupScheduled is scheduled event type
	PodGroupScheduled PodGroupConditionType = "Scheduled"
)

type PodGroupConditionDetail string

const (
	// PodGroupReady is that PodGroup has reached scheduling restriction
	PodGroupReady PodGroupConditionDetail = "pod group is ready"
	// PodGroupNotReady is that PodGroup has not yet reached the scheduling restriction
	PodGroupNotReady PodGroupConditionDetail = "pod group is not ready"
)

// PodGroupCondition contains details for the current state of this pod group.
type PodGroupCondition struct {
	// Type is the type of the condition
	Type PodGroupConditionType

	// Status is the status of the condition.
	Status v1.ConditionStatus

	// The ID of condition transition.
	TransitionID string

	// Last time the phase transitioned from another to current phase.
	// +optional
	LastTransitionTime metav1.Time

	// Unique, one-word, CamelCase reason for the phase's last transition.
	// +optional
	Reason string

	// Human-readable message indicating details about last transition.
	// +optional
	Message string
}

const (
	// PodFailedReason is probed if pod of PodGroup failed
	PodFailedReason string = "PodFailed"

	// PodDeletedReason is probed if pod of PodGroup deleted
	PodDeletedReason string = "PodDeleted"

	// NotEnoughResourcesReason is probed if there're not enough resources to schedule pods
	NotEnoughResourcesReason string = "NotEnoughResources"

	// NotEnoughPodsReason is probed if there're not enough tasks compared to `spec.minMember`
	NotEnoughPodsReason string = "NotEnoughTasks"
)

// QueueEvent represent the phase of queue.
type QueueEvent string

const (
	// QueueOutOfSyncEvent is triggered if PodGroup/Queue were updated
	QueueOutOfSyncEvent QueueEvent = "OutOfSync"
	// QueueCommandIssuedEvent is triggered if a command is raised by user
	QueueCommandIssuedEvent QueueEvent = "CommandIssued"
)

// QueueAction is the action that queue controller will take according to the event.
type QueueAction string

const (
	// SyncQueueAction is the action to sync queue status.
	SyncQueueAction QueueAction = "SyncQueue"
	// OpenQueueAction is the action to open queue
	OpenQueueAction QueueAction = "OpenQueue"
	// CloseQueueAction is the action to close queue
	CloseQueueAction QueueAction = "CloseQueue"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodGroup is a collection of Pod; used for batch workload.
// +kubebuilder:printcolumn:name="STATUS",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="minMember",type=integer,JSONPath=`.spec.minMember`
// +kubebuilder:printcolumn:name="RUNNINGS",type=integer,JSONPath=`.status.running`
// +kubebuilder:printcolumn:name="AGE",type=date,JSONPath=`.metadata.creationTimestamp`
type PodGroup struct {
	metav1.TypeMeta
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta

	// Specification of the desired behavior of the pod group.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Spec PodGroupSpec

	// Status represents the current information about a pod group.
	// This data may not be up to date.
	// +optional
	Status PodGroupStatus
}

// PodGroupSpec represents the template of a pod group.
type PodGroupSpec struct {
	// MinMember defines the minimal number of members/tasks to run the pod group;
	// if there's not enough resources to start all tasks, the scheduler
	// will not start anyone.
	MinMember int32

	// MinTaskMember defines the minimal number of pods to run for each task in the pod group;
	// if there's not enough resources to start each task, the scheduler
	// will not start anyone.
	MinTaskMember map[string]int32

	// Queue defines the queue to allocate resource for PodGroup; if queue does not exist,
	// the PodGroup will not be scheduled.
	Queue string

	// If specified, indicates the PodGroup's priority. "system-node-critical" and
	// "system-cluster-critical" are two special keywords which indicate the
	// highest priorities with the former being the highest priority. Any other
	// name must be defined by creating a PriorityClass object with that name.
	// If not specified, the PodGroup priority will be default or zero if there is no
	// default.
	// +optional
	PriorityClassName string

	// MinResources defines the minimal resource of members/tasks to run the pod group;
	// if there's not enough resources to start all tasks, the scheduler
	// will not start anyone.
	MinResources *v1.ResourceList
}

// PodGroupStatus represents the current state of a pod group.
type PodGroupStatus struct {
	// Current phase of PodGroup.
	Phase PodGroupPhase

	// The conditions of PodGroup.
	// +optional
	Conditions []PodGroupCondition

	// The number of actively running pods.
	// +optional
	Running int32

	// The number of pods which reached phase Succeeded.
	// +optional
	Succeeded int32

	// The number of pods which reached phase Failed.
	// +optional
	Failed int32
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// PodGroupList is a collection of pod groups.
type PodGroupList struct {
	metav1.TypeMeta
	// Standard list metadata
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ListMeta

	// items is the list of PodGroup
	Items []PodGroup
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Queue is a queue of PodGroup.
type Queue struct {
	metav1.TypeMeta
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ObjectMeta

	// Specification of the desired behavior of the queue.
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#spec-and-status
	// +optional
	Spec QueueSpec

	// The status of queue.
	// +optional
	Status QueueStatus
}

// Guarantee represents configuration of queue resource reservation
type Guarantee struct {
	// The amount of cluster resource reserved for queue. Just set either `percentage` or `resource`
	// +optional
	Resource v1.ResourceList `json:"resource,omitempty" protobuf:"bytes,3,opt,name=resource"`
}

// Reservation represents current condition about resource reservation
type Reservation struct {
	// Nodes are Locked nodes for queue
	// +optional
	Nodes []string `json:"nodes,omitempty" protobuf:"bytes,1,opt,name=nodes"`
	// Resource is a list of total idle resource in locked nodes.
	// +optional
	Resource v1.ResourceList `json:"resource,omitempty" protobuf:"bytes,2,opt,name=resource"`
}

// QueueStatus represents the status of Queue.
type QueueStatus struct {
	// State is status of queue
	State QueueState

	// The number of 'Unknown' PodGroup in this queue.
	Unknown int32
	// The number of 'Pending' PodGroup in this queue.
	Pending int32
	// The number of 'Running' PodGroup in this queue.
	Running int32
	// The number of `Inqueue` PodGroup in this queue.
	Inqueue int32
	// The number of `Completed` PodGroup in this queue.
	Completed int32

	// Reservation is the profile of resource reservation for queue
	Reservation Reservation `json:"reservation,omitempty" protobuf:"bytes,6,opt,name=reservation"`
}

// CluterSpec represents the template of Cluster
type Cluster struct {
	Name string
	Weight int32
	Capacity v1.ResourceList
}

// QueueSpec represents the template of Queue.
type QueueSpec struct {
	Weight     int32
	Capability v1.ResourceList

	// Depreicated: replaced by status.State
	State QueueState
	// Reclaimable indicate whether the queue can be reclaimed by other queue
	Reclaimable *bool

	// extendCluster indicate the jobs in this Queue will be dispatched to these clusters.
	ExtendClusters []Cluster

	// Guarantee indicate configuration about resource reservation
	Guarantee Guarantee `json:"guarantee,omitempty" protobuf:"bytes,4,opt,name=guarantee"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// QueueList is a collection of queues.
type QueueList struct {
	metav1.TypeMeta
	// Standard list metadata
	// More info: https://git.k8s.io/community/contributors/devel/api-conventions.md#metadata
	// +optional
	metav1.ListMeta

	// items is the list of PodGroup
	Items []Queue
}
