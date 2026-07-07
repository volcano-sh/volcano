# Namespace-scoped Queues for Tenant-authored Hierarchies

[@gitGurugu](https://github.com/gitGurugu); Jul 3, 2026

Tracking issue: [#5251](https://github.com/volcano-sh/volcano/issues/5251)


## Design detail

### The `NamespaceQueue` CRD


```go
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=namespacequeues,scope=Namespaced,shortName=nq

// NamespaceQueue is a namespace-scoped queue abstraction in Volcano scheduling.
type NamespaceQueue struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   NamespaceQueueSpec   `json:"spec,omitempty"`
    Status NamespaceQueueStatus `json:"status,omitempty"`
}

type NamespaceQueueSpec struct {
    // +kubebuilder:validation:Required 
    Parent string `json:"parent"`

    // +optional
	  // +kubebuilder:default:=1
	  Weight int32 `json:"weight,omitempty"`

    // +optional
	  Capability corev1.ResourceList `json:"capability,omitempty"`

    // +optional
	  Deserved corev1.ResourceList `json:"deserved,omitempty"`

    // +optional
	  Guarantee Guarantee `json:"guarantee,omitempty"`

    // +optional
	  Priority int32 `json:"priority,omitempty"`

    // +optional
	  // +kubebuilder:default:=fifo
	  // +kubebuilder:validation:Enum=fifo;traverse
	  DequeueStrategy DequeueStrategy `json:"dequeueStrategy,omitempty"`

}
```

```go
type NamespaceQueueStatus struct {

  // +optional
  Phase string `json:"phase,omitempty" protobuf:"bytes,1,opt,name=phase"`
  // State is state of Namespacequeue
	// +kubebuilder:validation:Enum=Open;Closed;Closing;Unknown
	// +optional
	State QueueState `json:"state,omitempty" protobuf:"bytes,2,opt,name=state"`

	// The number of 'Unknown' PodGroup in this queue.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Unknown int32 `json:"unknown,omitempty" protobuf:"bytes,3,opt,name=unknown"`
	// The number of 'Pending' PodGroup in this queue.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Pending int32 `json:"pending,omitempty" protobuf:"bytes,4,opt,name=pending"`
	// The number of 'Running' PodGroup in this queue.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Running int32 `json:"running,omitempty" protobuf:"bytes,5,opt,name=running"`
	// The number of `Inqueue` PodGroup in this queue.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Inqueue int32 `json:"inqueue,omitempty" protobuf:"bytes,6,opt,name=inqueue"`
	// The number of `Completed` PodGroup in this queue.
	// +kubebuilder:validation:Minimum=0
	// +optional
	Completed int32 `json:"completed,omitempty" protobuf:"bytes,7,opt,name=completed"`

	// Reservation is the profile of resource reservation for queue
	Reservation Reservation `json:"reservation,omitempty" protobuf:"bytes,8,opt,name=reservation"`

	// Allocated is allocated resources in queue
	// +optional
	Allocated v1.ResourceList `json:"allocated" protobuf:"bytes,9,opt,name=allocated"`
}
```


```go
type NamespaceQueueList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NamespaceQueue `json:"items"`
}
```



### Parent-queue ownership enforcement

Cluster `Queue`s are partitioned by cluster admins, who own the cluster-level
quotas. Once tenants can freely set `NamespaceQueue.spec.parent`, nothing stops a
tenant assigned to `queue-a` from creating a `NamespaceQueue` with
`spec.parent: queue-b` and drawing from another team's slice. The authorization to
attach under a cluster `Queue` must therefore live on the **`Queue`** side — owned
by the admin who owns the `Queue` — never on the tenant-authored `NamespaceQueue`.

We add a new field to the existing cluster **`QueueSpec`** (following the
`AllowedRoutes` pattern from the Gateway API, where the infrastructure-owned
`Gateway` — not the app-owned `Route` — declares which namespaces may attach):

```go
// QueueSpec (cluster-scoped Queue) — new field
type QueueSpec struct {
    // ... existing fields (weight, capability, deserved, guarantee, parent, ...)

    // AllowedNamespaces lists the namespaces whose NamespaceQueues are permitted
    // to set this Queue as their spec.parent. It is set by the cluster admin who
    // owns this Queue. An empty/unset list means no namespace may parent under
    // this Queue (deny by default), never "all namespaces".
    // +optional
    AllowedNamespaces []string `json:"allowedNamespaces,omitempty"`
}
```





