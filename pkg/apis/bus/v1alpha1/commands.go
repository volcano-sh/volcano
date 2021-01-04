package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// Command defines command structure.
type Command struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Action defines the action that will be took to the target object.
	Action string `json:"action,omitempty" protobuf:"bytes,2,opt,name=action"`

	// TargetObject defines the target object of this command.
	TargetObject *metav1.OwnerReference `json:"target,omitempty" protobuf:"bytes,3,opt,name=target"`

	// Unique, one-word, CamelCase reason for this command.
	// +optional
	Reason string `json:"reason,omitempty" protobuf:"bytes,4,opt,name=reason"`

	// Human-readable message indicating details of this command.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,4,opt,name=message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// CommandList defines list of commands.
type CommandList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	Items []Command `json:"items" protobuf:"bytes,2,rep,name=items"`
}
