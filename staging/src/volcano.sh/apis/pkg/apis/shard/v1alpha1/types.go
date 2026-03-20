/*
Copyright The Volcano Authors.

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
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeShard is a collection of nodes dedicated to a specific scheduler
// +kubebuilder:object:root=true
// +kubebuilder:resource:path=nodeshards,scope=Cluster,shortName=nsh
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="AGE",type=date,JSONPath=`.metadata.creationTimestamp`
type NodeShard struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Specification of the desired behavior of the NodeShard.
	Spec NodeShardSpec `json:"spec" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current information about a NodeShard.
	// This data may not be up to date.
	// +optional
	Status NodeShardStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// NodeShardSpec represents the template of a NodeShard.
type NodeShardSpec struct {
	// NodesDesired defines the list of nodes desired to be included in this NodeShard.
	NodesDesired []string `json:"nodesDesired" protobuf:"bytes,1,rep,name=nodesDesired"`
}

// NodeShardStatus represents the current state of a NodeShard.
type NodeShardStatus struct {
	// LastUpdateTime is the last time the status was updated.
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty" protobuf:"bytes,1,opt,name=lastUpdateTime"`

	// NodesInUse is the list of nodes currently in use by the NodeShard.
	// +optional
	NodesInUse []string `json:"nodesInUse,omitempty" protobuf:"bytes,2,rep,name=nodesInUse"`

	// NodesToRemove is the list of nodes preparing to be removed from the NodeShard.
	// +optional
	NodesToRemove []string `json:"nodesToRemove,omitempty" protobuf:"bytes,3,rep,name=nodesToRemove"`

	// NodesToAdd is the list of nodes preparing to be added to the NodeShard.
	// +optional
	NodesToAdd []string `json:"nodesToAdd,omitempty" protobuf:"bytes,4,rep,name=nodesToAdd"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NodeShardList is a collection of NodeShard.
type NodeShardList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// +optional
	metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Items is a list of NodeShard objects.
	Items []NodeShard `json:"items" protobuf:"bytes,2,rep,name=items"`
}
