/*
Copyright 2024 The Volcano Authors.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +genclient:nonNamespaced
// +kubebuilder:resource:path=hypernodes,shortName=hn,scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Tier",type=string,JSONPath=`.spec.tier`
// +kubebuilder:printcolumn:name="TierName",type=string,JSONPath=`.spec.tierName`
// +kubebuilder:printcolumn:name="NodeCount",type=integer,JSONPath=`.status.nodeCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// HyperNode represents a collection of nodes sharing similar network topology or performance characteristics.
type HyperNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec defines the desired configuration of the HyperNode.
	// +optional
	Spec HyperNodeSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// Status provides the current state of the HyperNode.
	// +optional
	Status HyperNodeStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// MemberType represents the member type, valid values are "Node" and "HyperNode".
// +kubebuilder:validation:Enum=Node;HyperNode
type MemberType string

const (
	// MemberTypeNode means the member type is a node.
	MemberTypeNode MemberType = "Node"
	// MemberTypeHyperNode means the member type is a hyperNode.
	MemberTypeHyperNode MemberType = "HyperNode"
)

// HyperNodeSpec defines the desired state of a HyperNode.
type HyperNodeSpec struct {
	// Tier categorizes the performance level of the HyperNode.
	// +kubebuilder:validation:Minimum=0
	// +required
	Tier int `json:"tier,omitempty" protobuf:"varint,1,opt,name=tier"`

	// TierName represents the level name of the HyperNode.
	// +kubebuilder:validation:MaxLength=253
	// +optional
	TierName string `json:"tierName,omitempty" protobuf:"bytes,2,opt,name=tierName"`

	// Members defines a list of node groups or individual nodes included in the HyperNode.
	// +kubebuilder:validation:MinItems=1
	// +optional
	Members []MemberSpec `json:"members,omitempty" protobuf:"bytes,3,rep,name=members"`
}

// MemberSpec represents a specific node or a hyperNodes in the hyperNode.
type MemberSpec struct {
	// Type specifies the member type.
	// +required
	Type MemberType `json:"type,omitempty" protobuf:"bytes,1,opt,name=type"`

	// Selector defines the selection rules for this member.
	// +optional
	Selector MemberSelector `json:"selector,omitempty" protobuf:"bytes,2,opt,name=selector"`
}

// MemberSelector defines the criteria for selecting nodes.
//
// Example for Exact match:
//
//		members:
//	 - type: Node
//		  selector:
//		    exactMatch:
//		      name: "node1"
//
// Example for Regex match:
//
//	 members:
//	 - type: Node
//	     selector:
//		   regexMatch:
//		     pattern: "^node-[0-9]+$"
//
// Example for Label match:
//
//	members:
//	- type: Node
//	  selector:
//	    labelMatch:
//	      matchLabels:
//	        topology-rack: rack1
//
// +kubebuilder:validation:XValidation:rule="has(self.exactMatch) || has(self.regexMatch) || has(self.labelMatch)",message="Either ExactMatch or RegexMatch or LabelMatch must be specified"
// +kubebuilder:validation:XValidation:rule="(has(self.exactMatch) ? 1 : 0) + (has(self.regexMatch) ? 1 : 0) + (has(self.labelMatch) ? 1 : 0) <= 1",message="Only one of ExactMatch, RegexMatch, or LabelMatch can be specified"
type MemberSelector struct {
	// ExactMatch defines the exact match criteria.
	// +optional
	ExactMatch *ExactMatch `json:"exactMatch,omitempty" protobuf:"bytes,1,opt,name=exactMatch"`

	// RegexMatch defines the regex match criteria.
	// +optional
	RegexMatch *RegexMatch `json:"regexMatch,omitempty" protobuf:"bytes,2,opt,name=regexMatch"`

	// LabelMatch defines the labels match criteria (only take effect when Member Type is "Node").
	// +optional
	LabelMatch *metav1.LabelSelector `json:"labelMatch,omitempty" protobuf:"bytes,3,opt,name=labelMatch"`
}

// ExactMatch represents the criteria for exact name matching.
type ExactMatch struct {
	// Name specifies the exact name of the node to match.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	// +required
	Name string `json:"name" protobuf:"bytes,1,req,name=name"`
}

// RegexMatch represents the criteria for regex-based matching.
type RegexMatch struct {
	// Pattern defines the regex pattern to match node names.
	// +kubebuilder:validation:MinLength=1
	// +required
	Pattern string `json:"pattern" protobuf:"bytes,1,opt,name=pattern"`
}

// HyperNodeStatus represents the observed state of a HyperNode.
type HyperNodeStatus struct {
	// Conditions provide details about the current state of the HyperNode.
	Conditions []metav1.Condition `json:"conditions,omitempty" protobuf:"bytes,1,rep,name=conditions"`

	// NodeCount is the total number of nodes currently in the HyperNode.
	// +kubebuilder:validation:Minimum=0
	NodeCount int64 `json:"nodeCount,omitempty" protobuf:"varint,2,opt,name=nodeCount"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true

// HyperNodeList contains a list of HyperNode resources.
type HyperNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is the list of HyperNodes.
	Items []HyperNode `json:"items" protobuf:"bytes,2,rep,name=items"`
}
