/*
Copyright 2025 The Volcano Authors.

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

package v1beta1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=namespacequeues,scope=Namespaced,shortName=nsq
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="STATE",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="SHADOW-QUEUE",type=string,JSONPath=`.status.shadowQueueName`
// +kubebuilder:printcolumn:name="AGE",type=date,JSONPath=`.metadata.creationTimestamp`

// NamespaceQueue is a namespace-scoped queue that tenants can create and manage
// without cluster-admin permission. The controller synthesizes a cluster-scoped
// shadow Queue from each NamespaceQueue so that the existing Volcano scheduler
// and all its plugins (capacity, proportion, gang) require no modification.
type NamespaceQueue struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec is the desired state of this NamespaceQueue.
	// +optional
	Spec NamespaceQueueSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Status is the observed state of this NamespaceQueue, including the name
	// of the shadow cluster-scoped Queue synthesized by the controller.
	// +optional
	Status NamespaceQueueStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// NamespaceQueueSpec mirrors the resource-governing fields of QueueSpec so that
// tenants get identical semantics (capability, guarantee, deserved, hierarchy)
// without needing cluster-admin access to create a cluster-scoped Queue.
type NamespaceQueueSpec struct {
	// Weight is the relative weight of this queue used by the proportion plugin
	// to calculate a fair-share deserved value. Mirrors QueueSpec.Weight.
	// +optional
	// +kubebuilder:default:=1
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Weight int32 `json:"weight,omitempty" protobuf:"bytes,1,opt,name=weight"`

	// Capability is the hard upper limit of resources usable by this queue.
	// Jobs are held Pending when their request would exceed this limit.
	// Mirrors QueueSpec.Capability.
	// +optional
	Capability v1.ResourceList `json:"capability,omitempty" protobuf:"bytes,2,opt,name=capability"`

	// Reclaimable indicates whether resources used above Deserved can be
	// reclaimed by other queues. Defaults to true. Mirrors QueueSpec.Reclaimable.
	// +optional
	Reclaimable *bool `json:"reclaimable,omitempty" protobuf:"bytes,3,opt,name=reclaimable"`

	// Guarantee defines resources that are reserved for this queue and cannot
	// be reclaimed by other queues. Mirrors QueueSpec.Guarantee.
	// +optional
	Guarantee Guarantee `json:"guarantee,omitempty" protobuf:"bytes,4,opt,name=guarantee"`

	// Deserved is the fair-share resource target for this queue. Resources used
	// above Deserved are eligible for reclamation. Mirrors QueueSpec.Deserved.
	// +optional
	Deserved v1.ResourceList `json:"deserved,omitempty" protobuf:"bytes,5,opt,name=deserved"`

	// Priority is the scheduling priority of this queue: higher values are
	// scheduled first and reclaimed last. Mirrors QueueSpec.Priority.
	// +optional
	// +kubebuilder:validation:Minimum=0
	Priority int32 `json:"priority,omitempty" protobuf:"bytes,6,opt,name=priority"`

	// Parent is the name of a NamespaceQueue in the same namespace that acts as
	// the parent in a namespace-local queue hierarchy. Hierarchy rules (child
	// deserved/guarantee sum ≤ parent) mirror cluster Queue hierarchy semantics.
	// +optional
	// +kubebuilder:validation:MaxLength=253
	Parent string `json:"parent,omitempty" protobuf:"bytes,7,opt,name=parent"`
}

// NamespaceQueueStatus is the observed state of a NamespaceQueue, populated by
// the namespacequeue-controller. It mirrors the status of the shadow Queue so
// tenants have full visibility into resource usage without access to cluster resources.
type NamespaceQueueStatus struct {
	// State is the current state of this queue, mirrored from the shadow Queue.
	// +kubebuilder:validation:Enum=Open;Closed;Closing;Unknown
	// +optional
	State QueueState `json:"state,omitempty" protobuf:"bytes,1,opt,name=state"`

	// ShadowQueueName is the name of the cluster-scoped Queue synthesized and
	// managed by the controller for this NamespaceQueue. PodGroups referencing
	// this NamespaceQueue will have their spec.queue patched to this value by
	// the admission webhook.
	// +optional
	ShadowQueueName string `json:"shadowQueueName,omitempty" protobuf:"bytes,2,opt,name=shadowQueueName"`

	// Allocated is the sum of resources currently allocated to running PodGroups
	// in this queue, mirrored from the shadow Queue status updated by the scheduler.
	// +optional
	Allocated v1.ResourceList `json:"allocated,omitempty" protobuf:"bytes,3,opt,name=allocated"`

	// The number of PodGroups in each phase. Mirrored from the shadow Queue.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Pending int32 `json:"pending,omitempty" protobuf:"bytes,4,opt,name=pending"`

	// +kubebuilder:validation:Minimum=0
	// +optional
	Running int32 `json:"running,omitempty" protobuf:"bytes,5,opt,name=running"`

	// +kubebuilder:validation:Minimum=0
	// +optional
	Inqueue int32 `json:"inqueue,omitempty" protobuf:"bytes,6,opt,name=inqueue"`

	// +kubebuilder:validation:Minimum=0
	// +optional
	Completed int32 `json:"completed,omitempty" protobuf:"bytes,7,opt,name=completed"`

	// +kubebuilder:validation:Minimum=0
	// +optional
	Unknown int32 `json:"unknown,omitempty" protobuf:"bytes,8,opt,name=unknown"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// NamespaceQueueList is a collection of NamespaceQueues.
type NamespaceQueueList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Items           []NamespaceQueue `json:"items" protobuf:"bytes,2,rep,name=items"`
}

// ---- DeepCopy methods (pre-codegen placeholder) ----
// These will be moved into zz_generated.deepcopy.go after running `make generate-code`.

func (in *NamespaceQueue) DeepCopyInto(out *NamespaceQueue) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *NamespaceQueue) DeepCopy() *NamespaceQueue {
	if in == nil {
		return nil
	}
	out := new(NamespaceQueue)
	in.DeepCopyInto(out)
	return out
}

func (in *NamespaceQueue) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *NamespaceQueueSpec) DeepCopyInto(out *NamespaceQueueSpec) {
	*out = *in
	if in.Capability != nil {
		in, out := &in.Capability, &out.Capability
		*out = make(v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	if in.Reclaimable != nil {
		in, out := &in.Reclaimable, &out.Reclaimable
		*out = new(bool)
		**out = **in
	}
	in.Guarantee.DeepCopyInto(&out.Guarantee)
	if in.Deserved != nil {
		in, out := &in.Deserved, &out.Deserved
		*out = make(v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
}

func (in *NamespaceQueueSpec) DeepCopy() *NamespaceQueueSpec {
	if in == nil {
		return nil
	}
	out := new(NamespaceQueueSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *NamespaceQueueStatus) DeepCopyInto(out *NamespaceQueueStatus) {
	*out = *in
	if in.Allocated != nil {
		in, out := &in.Allocated, &out.Allocated
		*out = make(v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
}

func (in *NamespaceQueueStatus) DeepCopy() *NamespaceQueueStatus {
	if in == nil {
		return nil
	}
	out := new(NamespaceQueueStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *NamespaceQueueList) DeepCopyInto(out *NamespaceQueueList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]NamespaceQueue, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *NamespaceQueueList) DeepCopy() *NamespaceQueueList {
	if in == nil {
		return nil
	}
	out := new(NamespaceQueueList)
	in.DeepCopyInto(out)
	return out
}

func (in *NamespaceQueueList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
