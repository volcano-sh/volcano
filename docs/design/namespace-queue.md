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




### Control Plane

Overview: The NamespaceQueue (NSQ) Controller operates as a background reconciliation loop (Reconcile Loop) based on the standard Kubernetes controller pattern. Its core mission is to act as a secure proxy and bridge between the tenant-facing NamespaceQueue (Namespaced-scoped) and the backend Volcano Queue (Cluster-scoped).

The controller watches both NamespaceQueue and backend Queue resources, driving the system toward the desired state through four sequential logical phases.




Step 1: Fetch NSQ and Handle Deletion

Design Intent:
Use NSQ events as the entry point of the control loop and determine whether the object still exists or is being deleted.

Logic Flow:

- The controller receives an NSQ reconcile request and fetches the NSQ object by namespace and name.

- If the NSQ no longer exists, it means the object has already been deleted from the cluster. The controller exits safely without further processing.

- If the NSQ is being deleted, Kubernetes garbage collection will clean up the corresponding physical Queue through the previously set OwnerReference. Therefore, no extra cleanup logic is required in the controller.


```go
func (r *NamespaceQueueReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// === STEP 1: Fetch Source Object ===
	nsq := &platformv1alpha1.NamespaceQueue{}
	if err := r.Get(ctx, req.NamespacedName, nsq); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil // Cascade deletion handled by OwnerReference
		}
		return ctrl.Result{}, err
	}

	// === STEP 2: Multi-Tenancy & Authorization Validation ===
	if err := r.validateNamespaceQueue(ctx, nsq); err != nil {
		logger.Error(err, "NSQ security validation failed, intercepting propagation", "nsq", nsq.Name)
		
		nsq.Status.Phase = "Rejected"
		nsq.Status.Reason = err.Error()
		if updateErr := r.Status().Update(ctx, nsq); updateErr != nil {
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, nil 
	}

	// === STEP 3: 1:1 Topology Translation ===
	backendQueueName := fmt.Sprintf("%s-%s", nsq.Namespace, nsq.Name)
	foundQueue := &volcanov1beta1.Queue{}

	err := r.Get(ctx, client.ObjectKey{Name: backendQueueName}, foundQueue)
	if err != nil && errors.IsNotFound(err) {
		// Scenario A: Backend Queue not exists -> Create
		logger.Info("Creating backend physical Volcano Queue", "queueName", backendQueueName)
		
		newQueue := r.buildVolcanoQueue(nsq, backendQueueName)
		if err := r.Create(ctx, newQueue); err != nil {
			return ctrl.Result{}, err // Trigger retry for self-healing
		}

		nsq.Status.Phase = "Admitted"
		nsq.Status.Reason = "Successfully validated and admitted by platform controller."
		nsq.Status.State = platformv1alpha1.QueueStateOpen
		if err := r.Status().Update(ctx, nsq); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// Scenario B: Backend Queue exists -> Update (Enforce Eventual Consistency)
	logger.Info("Updating backend physical Volcano Queue spec", "queueName", backendQueueName)
	expectedQueue := r.buildVolcanoQueue(nsq, backendQueueName)
	
	foundQueue.Spec = expectedQueue.Spec
	if err := r.Update(ctx, foundQueue); err != nil {
		return ctrl.Result{}, err
	}

	// === STEP 4: Bidirectional Status Sync ===
	if err := r.syncQueueStatusToNSQ(ctx, nsq, foundQueue); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
```


Step 2: Validate Hierarchy and Authorization

Design Intent:
Prevent tenants from using `spec.parent` to attach to unauthorized queues or consume resources beyond their allowed scope.

Logic Flow:

- The controller first checks whether `spec.parent` points to a `NamespaceQueue` in the same Namespace.

- If a parent `NamespaceQueue` exists, the controller validates that the child queue’s `Capability` does not exceed the parent’s limit. If it does, the request is rejected.

- If no parent `NamespaceQueue` is found in the Namespace, the queue is treated as attaching to a cluster-level `Queue`.

- In this case, the controller checks whether the tenant Namespace is included in the cluster Queue’s `spec.allowedNamespaces`.

- If the Namespace is not authorized, the controller stops reconciliation, prevents the physical Queue from being created, and updates `NSQ.Status.Phase` to `Rejected` with the rejection reason.



```go
func (r *NamespaceQueueReconciler) validateNamespaceQueue(ctx context.Context, nsq *platformv1alpha1.NamespaceQueue) error {
	currentNS := nsq.Namespace
	parentName := nsq.Spec.Parent

	// 1. Check if parent is a local NSQ within the same namespace (Sub-tree validation)
	parentNSQ := &platformv1alpha1.NamespaceQueue{}
	err := r.Get(ctx, client.ObjectKey{Namespace: currentNS, Name: parentName}, parentNSQ)
	if err == nil {
		if r.isResourceExceeded(nsq.Spec.Capability, parentNSQ.Spec.Capability) {
			return fmt.Errorf("quota sub-tree violation: child capability exceeds parent NSQ '%s' limits", parentName)
		}
		return nil
	}

	// 2. Check if parent is a cluster-scoped Queue (Reverse Authorization ACL check)
	globalQueue := &volcanov1beta1.Queue{}
	err = r.Get(ctx, client.ObjectKey{Name: parentName}, globalQueue)
	if err != nil {
		if errors.IsNotFound(err) {
			return fmt.Errorf("invalid parent target: '%s' is neither a local NamespaceQueue nor a cluster Queue", parentName)
		}
		return err
	}

	// Access Control List Check via AllowedNamespaces
	allowed := false
	for _, ns := range globalQueue.Spec.AllowedNamespaces { 
		if ns == currentNS {
			allowed = true
			break
		}
	}

	if !allowed {
		return fmt.Errorf("multi-tenancy security violation: namespace '%s' is NOT permitted to parent under cluster Queue '%s'", currentNS, parentName)
	}

	// Validate against global queue capacity limits
	if r.isResourceExceeded(nsq.Spec.Capability, globalQueue.Spec.Capability) {
		return fmt.Errorf("quota root violation: root NSQ capability exceeds cluster Queue '%s' total capacity", parentName)
	}

	return nil
}

func (r *NamespaceQueueReconciler) isResourceExceeded(child, parent corev1.ResourceList) bool {
	for resName, childQty := range child {
		if parentQty, exists := parent[resName]; exists {
			if childQty.Cmp(parentQty) > 0 {
				return true // Overcommit detected
			}
		} else {
			return true // Resource type not permitted by parent
		}
	}
	return false
}
```

Step 3: Translate and Clone Queue

Design Intent:
Convert the validated `NamespaceQueue` into a cluster-level Volcano `Queue`.

Implementation Details:

- The controller generates a globally unique physical Queue name using the format `[Namespace]-[NSQ-Name]` to avoid naming conflicts.

- The controller copies scheduling fields from `NSQ.Spec` into Volcano `QueueSpec`, including `Weight`, `Capability`, `Deserved`, `Guarantee`, `Priority`, and `DequeueStrategy`.

- The controller also handles the parent queue mapping. If the parent is another `NamespaceQueue` in the same Namespace, it rewrites the parent name to the corresponding physical Queue name.

- This ensures that the backend Volcano queue hierarchy matches the frontend NSQ hierarchy.


```go
func (r *NamespaceQueueReconciler) buildVolcanoQueue(nsq *platformv1alpha1.NamespaceQueue, name string) *volcanov1beta1.Queue {
	return &volcanov1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			// Inject OwnerReference link to establish parent-child relationship for cascade deletion
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(nsq, platformv1alpha1.GroupVersion.WithKind("NamespaceQueue")),
			},
		},
		Spec: volcanov1beta1.QueueSpec{
			Weight:          nsq.Spec.Weight,
			Capability:      nsq.Spec.Capability,
			Deserved:        nsq.Spec.Deserved,
			Guarantee:       nsq.Spec.Guarantee,
			Priority:        nsq.Spec.Priority,
			DequeueStrategy: volcanov1beta1.DequeueStrategy(nsq.Spec.DequeueStrategy),
			
			// Flattens the hierarchical relationship for backend volcano engine
			Parent:          fmt.Sprintf("%s-%s", nsq.Namespace, nsq.Spec.Parent), 
		},
	}
}
```

Step 4: Reconcile Queue and Sync Status

Design Intent:
Keep the physical Volcano `Queue` consistent with the tenant `NamespaceQueue`, and expose the backend queue status back to tenants.

Reconciliation Mechanism:

- The controller submits the physical `Queue` to the API Server.

- If the physical `Queue` does not exist, the controller creates it.

- If it already exists, the controller updates its `Spec` with the latest computed configuration, so tenant quota changes can take effect in the backend scheduler.

- By declaring `Owns(&volcano.Queue{})`, the controller can detect when a physical `Queue` is accidentally deleted and recreate it automatically.

- At the end of reconciliation, the controller copies key runtime fields from `Queue.Status` back to `NSQ.Status`, such as `State`, `Running`, `Pending`, and `Allocated`.

- This allows tenants to view their queue state and workload scheduling progress directly from their own `NamespaceQueue`.


``` go
func (r *NamespaceQueueReconciler) syncQueueStatusToNSQ(ctx context.Context, nsq *platformv1alpha1.NamespaceQueue, q *volcanov1beta1.Queue) error {
	// Pixels-level state back-filling to achieve observable closed-loop
	nsq.Status.Phase = "Admitted"
	nsq.Status.State = platformv1alpha1.QueueState(q.Status.State)
	nsq.Status.Unknown = q.Status.Unknown
	nsq.Status.Pending = q.Status.Pending
	nsq.Status.Running = q.Status.Running
	nsq.Status.Inqueue = q.Status.Inqueue
	nsq.Status.Completed = q.Status.Completed
	nsq.Status.Reservation = platformv1alpha1.Reservation(q.Status.Reservation)
	nsq.Status.Allocated = q.Status.Allocated

	return r.Status().Update(ctx, nsq)
}

func (r *NamespaceQueueReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.NamespaceQueue{}). 
		Owns(&volcanov1beta1.Queue{}).           // Informer watches backend cluster Queue for Self-healing
		Complete(r)
}
```




