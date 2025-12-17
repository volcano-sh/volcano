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
// +kubebuilder:printcolumn:name="QUEUE",type=string,priority=1,JSONPath=`.spec.queue`
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
	// SubGroupPolicy covers all capabilities of minTaskMember, while providing richer network topology and Gang scheduling management capabilities.
	// Recommend using SubGroupPolicy to uniformly manage Gang scheduling for each Task group.
	MinTaskMember map[string]int32

	// Queue defines the queue to allocate resource for PodGroup; if queue does not exist,
	// the PodGroup will not be scheduled.
	// +kubebuilder:default:="default"
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

	// NetworkTopology defines the NetworkTopology config, this field works in conjunction with network topology feature and hyperNode CRD.
	// +optional
	NetworkTopology *NetworkTopologySpec

	// SubGroupPolicy provides secondary grouping capability for Pods within a PodGroup, supporting subgroup-level Gang scheduling and network topology affinity scheduling.
	// 1. Supports dividing Pods in a PodGroup into multiple subgroups as required;
	// 2. Supports configuring subgroup-level Gang scheduling (e.g., scheduling is allowed only when resource requirements of at least N subgroups are satisfied);
	// 3. Supports specifying that Pods within a subgroup are scheduled to the same network topology domain (such as HyperNode);

	// Compared with minTaskMember, it offers more comprehensive topology scheduling and Gang scheduling management capabilities.
	// Concurrent use with minTaskMember is not recommended, and SubGroupPolicy is the long-term evolution direction.
	// +optional
	SubGroupPolicy []SubGroupPolicySpec
}

type SubGroupPolicySpec struct {
	// Name specifies the name of SubGroupPolicy
	Name string

	// NetworkTopology defines the NetworkTopology config, this field works in conjunction with network topology feature and hyperNode CRD.
	// +optional
	NetworkTopology *NetworkTopologySpec

	// SubGroupSize defines the number of pods in each sub-affinity group.
	// Only when a subGroup of pods, with a size of "subGroupSize", can satisfy the network topology constraint then will the subGroup be scheduled.
	SubGroupSize *int32

	// MinSubGroups: Minimum number of subgroups required to trigger scheduling. Scheduling is initiated only if cluster resources meet the requirements of at least this number of subgroups.
	// Subgroup-level Gang Scheduling
	// +kubebuilder:default:=0
	// +optional
	MinSubGroups *int32

	// LabelSelector is used to find matching pods.
	// Pods that match this label selector are counted to determine the number of pods
	// in their corresponding topology domain.
	// +optional
	LabelSelector *metav1.LabelSelector

	// MatchLabelKeys: A label-based grouping configuration field for Pods, defining filtering rules for grouping label keys
	// Core function: Refine grouping of Pods that meet LabelSelector criteria by label attributes, with the following rules and constraints:
	// 1. Scope: Only applies to Pods matching the predefined LabelSelector
	// 2. Grouping rule: Specify one or more label keys; Pods containing the target label keys with exactly the same corresponding label values are grouped together
	// 3. Policy constraint: Pods in the same group follow a unified NetworkTopology policy to achieve group-level network behavior governance
	// +listType=atomic
	// +optional
	MatchLabelKeys []string
}

// NetworkTopologyMode represents the networkTopology mode, valid values are "hard" and "soft".
// +kubebuilder:validation:Enum=hard;soft
type NetworkTopologyMode string

const (
	// HardNetworkTopologyMode represents a strict network topology constraint that jobs must adhere to.
	HardNetworkTopologyMode NetworkTopologyMode = "hard"

	// SoftNetworkTopologyMode represents a flexible network topology constraint that
	// allows jobs to cross network boundaries under certain conditions.
	SoftNetworkTopologyMode NetworkTopologyMode = "soft"
)

type NetworkTopologySpec struct {
	// Mode specifies the mode of the network topology constrain.
	// +kubebuilder:default=hard
	// +optional
	Mode NetworkTopologyMode

	// HighestTierAllowed specifies the highest tier that a job allowed to cross when scheduling.
	// +optional
	HighestTierAllowed *int

	// HighestTierName specifies the highest tier name that a job allowed to cross when scheduling.
	// HighestTierName and HighestTierAllowed cannot be set simultaneously.
	// +optional
	HighestTierName string
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
	Reservation Reservation

	// Allocated is allocated resources in queue
	// +optional
	Allocated v1.ResourceList
}

// CluterSpec represents the template of Cluster
type Cluster struct {
	Name     string
	Weight   int32
	Capacity v1.ResourceList
}

// Affinity is a group of affinity scheduling rules.
type Affinity struct {
	// Describes nodegroup affinity scheduling rules for the queue.
	// +optional
	NodeGroupAffinity *NodeGroupAffinity `json:"nodeGroupAffinity,omitempty" protobuf:"bytes,1,opt,name=nodeGroupAffinity"`

	// Describes nodegroup affinity scheduling rules for the queue.
	// +optional
	NodeGroupAntiAffinity *NodeGroupAntiAffinity `json:"nodeGroupAntiAffinity,omitempty" protobuf:"bytes,2,opt,name=nodeGroupAntiAffinity"`
}

type NodeGroupAffinity struct {
	// +optional
	RequiredDuringSchedulingIgnoredDuringExecution []string `json:"requiredDuringSchedulingIgnoredDuringExecution,omitempty" protobuf:"bytes,1,opt,name=requiredDuringSchedulingIgnoredDuringExecution"`
	// +optional
	PreferredDuringSchedulingIgnoredDuringExecution []string `json:"preferredDuringSchedulingIgnoredDuringExecution,omitempty" protobuf:"bytes,2,rep,name=preferredDuringSchedulingIgnoredDuringExecution"`
}

type NodeGroupAntiAffinity struct {
	// +optional
	RequiredDuringSchedulingIgnoredDuringExecution []string `json:"requiredDuringSchedulingIgnoredDuringExecution,omitempty" protobuf:"bytes,1,opt,name=requiredDuringSchedulingIgnoredDuringExecution"`
	// +optional
	PreferredDuringSchedulingIgnoredDuringExecution []string `json:"preferredDuringSchedulingIgnoredDuringExecution,omitempty" protobuf:"bytes,2,rep,name=preferredDuringSchedulingIgnoredDuringExecution"`
}

// QueueSpec represents the template of Queue.
type QueueSpec struct {
	Weight     int32
	Capability v1.ResourceList

	// Reclaimable indicate whether the queue can be reclaimed by other queue
	Reclaimable *bool

	// extendCluster indicate the jobs in this Queue will be dispatched to these clusters.
	ExtendClusters []Cluster

	// Guarantee indicate configuration about resource reservation
	Guarantee Guarantee `json:"guarantee,omitempty" protobuf:"bytes,4,opt,name=guarantee"`

	// If specified, the queue's scheduling constraints
	// +optional
	Affinity *Affinity `json:"affinity,omitempty" protobuf:"bytes,6,opt,name=affinity"`

	// Type define the type of queue
	Type string `json:"type,omitempty" protobuf:"bytes,7,opt,name=type"`

	// Parent define the parent of queue
	// +optional
	Parent string `json:"parent,omitempty" protobuf:"bytes,8,opt,name=parent"`

	// The amount of resources configured by the user. This part of resource can be shared with other queues and reclaimed back.
	// +optional
	Deserved v1.ResourceList `json:"deserved,omitempty" protobuf:"bytes,9,opt,name=deserved"`

	// Priority define the priority of queue. Higher values are prioritized for scheduling and considered later during reclamation.
	// +optional
	Priority int32 `json:"priority,omitempty" protobuf:"bytes,10,opt,name=priority"`

	// DequeueStrategy defines the dequeue strategy of queue
	// +optional
	DequeueStrategy DequeueStrategy `json:"dequeueStrategy,omitempty" protobuf:"bytes,11,opt,name=dequeueStrategy"`
}

type DequeueStrategy string

const (
	// DequeueStrategyFIFO defines a strict FIFO strategy. If the head of the queue cannot be scheduled,
	// the system will not attempt to dequeue other jobs from the queue.
	DequeueStrategyFIFO DequeueStrategy = "fifo"
	// DequeueStrategyTraverse defines a strategy that traverses the queue. If the head of the queue cannot be scheduled,
	// it will be skipped and the scheduler will attempt to dequeue subsequent jobs.
	DequeueStrategyTraverse DequeueStrategy = "traverse"

	// DefaultDequeueStrategy is the default dequeue strategy.
	DefaultDequeueStrategy DequeueStrategy = DequeueStrategyTraverse
)

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
