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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status

// DataSource represents a cached query result for data sources in the federated environment.
// It is a cluster-scoped resource.
type DataSource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataSourceSpec   `json:"spec,omitempty"`
	Status DataSourceStatus `json:"status,omitempty"`
}

// DataSourceSpec defines the desired state of DataSource.
type DataSourceSpec struct {
	// System specifies the underlying data system.
	// This provides context for the 'name' and 'attributes' fields.
	// e.g., "hive", "s3", "hdfs".
	// +required
	System string `json:"system"`

	// Type specifies the category of the data source within the system.
	// e.g., for system="hive", type could be "table", "view".
	// e.g., for system="s3", type could be "bucket", "object", "prefix".
	// +required
	Type string `json:"type"`

	// Name is the identifier of the data source, its format is interpreted
	// in the context of the 'system' and 'type'.
	// +required
	Name string `json:"name"`

	// Locality defines which clusters this data source is available on.
	// +required
	Locality *DataSourceLocality `json:"locality"`

	// Attributes provides extra, non-identifying metadata.
	// +optional
	Attributes map[string]string `json:"attributes,omitempty"`

	// ReclaimPolicy defines what happens to this DataSource when its last bound DataSourceClaim is deleted.
	// Defaults to "Retain".
	// +optional
	ReclaimPolicy DataSourceReclaimPolicy `json:"reclaimPolicy,omitempty"`
}

// DataSourceLocality specifies the cached location information of the data source.
type DataSourceLocality struct {
	// ClusterNames is a list of cluster names where the cached data source information indicates availability.
	// This provides a simple and direct way to specify cached data source location
	// without interfering with user-defined ResourceBinding cluster affinity.
	// +required
	ClusterNames []string `json:"clusterNames"`
}

// DataSourceStatus defines the observed state of DataSource.
type DataSourceStatus struct {
	// ClaimRefs is a list of references to DataSourceClaims that are bound to this DataSource.
	// The presence of items in this list indicates the DataSource is in use.
	// +optional
	ClaimRefs []corev1.ObjectReference `json:"claimRefs,omitempty"`

	// BoundClaims counts the number of DataSourceClaims currently bound to this DataSource.
	// This provides a quick summary of its usage.
	// +optional
	BoundClaims int32 `json:"boundClaims,omitempty"`

	// Conditions store the available observations of the DataSource's state.
	// This is more flexible than a single phase.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DataSourceList contains a list of DataSource.
type DataSourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataSource `json:"items"`
}

type DataSourceReclaimPolicy string

const (
	ReclaimPolicyRetain DataSourceReclaimPolicy = "Retain"
	ReclaimPolicyDelete DataSourceReclaimPolicy = "Delete"
)

// ==================================  DataSourceClaim  ==================================
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status

// DataSourceClaim is a request for a DataSource by a user.
// It is a namespaced resource.
type DataSourceClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataSourceClaimSpec   `json:"spec,omitempty"`
	Status DataSourceClaimStatus `json:"status,omitempty"`
}

// WorkloadRef defines a reference to a workload resource that can be used with Dynamic Client.
// It provides the minimal fields needed to precisely identify and retrieve a workload.
type WorkloadRef struct {
	// APIVersion is the API version of the workload resource.
	// e.g., "apps/v1", "batch.volcano.sh/v1alpha1"
	// +required
	APIVersion string `json:"apiVersion"`

	// Kind is the kind of the workload resource.
	// e.g., "Deployment", "Job"
	// +required
	Kind string `json:"kind"`

	// Name is the name of the workload resource.
	// +required
	Name string `json:"name"`

	// Namespace is the namespace of the workload resource.
	// If empty, defaults to the namespace of the DataSourceClaim.
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// DataSourceClaimSpec defines the desired state of DataSourceClaim.
type DataSourceClaimSpec struct {
	// System is the required underlying data system of the data source.
	// +required
	System string `json:"system"`

	// DataSourceType is the required category of the data source within the system.
	// +required
	DataSourceType string `json:"dataSourceType"`

	// DataSourceName specifies the logical name of the cached data source to claim.
	// It will be matched against DataSource's spec.name field.
	// +required
	DataSourceName string `json:"dataSourceName"`

	// Workload specifies the workload that this claim is associated with.
	// This enables the controller to precisely identify and manage the workload
	// using Dynamic Client without requiring complex selectors or UIDs.
	// +required
	Workload WorkloadRef `json:"workload"`

	// Attributes provides extra, non-identifying metadata.
	// +optional
	Attributes map[string]string `json:"attributes,omitempty"`
}

// DataSourceClaimStatus defines the observed state of DataSourceClaim.
type DataSourceClaimStatus struct {
	// Phase indicates the current lifecycle phase of the claim.
	// +optional
	// +default="Pending"
	Phase DSCPhase `json:"phase"`

	// BoundDataSource specifies the name of the DataSource object
	// that is bound to this claim for scheduling.
	// +optional
	BoundDataSource string `json:"boundDataSource,omitempty"`

	// Conditions store the available observations of the claim's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DataSourceClaimList contains a list of DataSourceClaim.
type DataSourceClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataSourceClaim `json:"items"`
}

type DSCPhase string

const (
	DSCPhasePending DSCPhase = "Pending"
	DSCPhaseBound   DSCPhase = "Bound"
)
