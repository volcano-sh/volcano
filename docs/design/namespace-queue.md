# Namespace-scoped Queues in Volcano

[gitGurugu](https://github.com/gitGurugu); Jul 10, 2026

Tracking issue: [#5251](https://github.com/volcano-sh/volcano/issues/5251)

## 1. Motivation

Volcano Queue is cluster-scoped, so creating or modifying a queue normally requires cluster-level permissions. In a multi-tenant cluster, tenants typically manage resources within their own namespaces. Requiring cluster administrators to create or modify queues limits tenant self-service, while granting tenants access to cluster-scoped Queues weakens namespace isolation and complicates resource ownership.

`NamespaceQueue` addresses this gap by providing a namespaced queue resource that tenants can manage within their own namespaces. It supports namespace-local queue hierarchies and resource governance while allowing cluster administrators to control how those hierarchies attach to cluster-scoped Queues. Workloads can reference a NamespaceQueue explicitly, and existing cluster Queue behavior remains unchanged.

## 2. Scope

In Scope:

- introduce the namespaced `NamespaceQueue` CRD and its resource-governance fields;
- support namespace-local hierarchies and attachment to authorized cluster-scoped Queues;
- allow workloads to reference NamespaceQueues through the common scheduler model; and
- preserve existing Queue workload references and scheduling behavior while extending `QueueSpec` with optional namespace authorization.

Out of Scope:

- cross-namespace NamespaceQueue parent references;
- materializing NamespaceQueues as cluster-scoped Queues or creating ownership relationships between them;
- changing the Kubernetes namespace and RBAC model; and
- NamespaceQueue support in scheduling plugins beyond the initial capacity plugin.

## 3. Detailed Design

### 3.1 Implementation Overview

NamespaceQueue integrates with the API, admission, controller, and scheduler components. Their responsibilities are defined as follows:

| Component | Responsibility |
| --- | --- |
| API and CRD | Define the `NamespaceQueue` resource, schema, object-local validation, defaulting, and status subresource. |
| Admission Webhook | Perform request-time validation of workload queue references and best-effort validation of `NamespaceQueue` parent authorization. It does not maintain dynamic hierarchy state or determine scheduling readiness. |
| `NamespaceQueue` controller | Resolve parent references, reconcile hierarchy and authorization, manage lifecycle and status, and requeue affected subtrees. |
| Scheduler | Watch `Queue` and `NamespaceQueue` resources, build the unified `QueueInfo` cache, and perform the final scheduling-readiness check. |
| Existing workload controllers | Preserve raw queue references when creating or updating `PodGroup` and `Pod` objects. They do not create shadow `Queue` resources. |

### 3.2 API and CRD

#### 3.2.1 Resource Definition

`NamespaceQueue` is a namespaced custom resource used by Volcano scheduling. It contains the desired queue configuration in `spec` and the observed state in `status`. The `status` subresource is updated independently from the desired configuration.

```go
// NamespaceQueue is a namespaced queue resource used by Volcano scheduling.
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=namespacequeues,scope=Namespaced,shortName=nq
// +kubebuilder:printcolumn:name="PARENT",type=string,JSONPath=`.spec.parent`
// +kubebuilder:printcolumn:name="STATE",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="AGE",type=date,JSONPath=`.metadata.creationTimestamp`
type NamespaceQueue struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Desired queue configuration.
	// +optional
	Spec NamespaceQueueSpec `json:"spec,omitempty"`

	// Observed queue state.
	// +optional
	Status NamespaceQueueStatus `json:"status,omitempty"`
}
```


#### 3.2.2 NamespaceQueueSpec

`NamespaceQueueSpec` defines the desired state of a `NamespaceQueue`.

The controller reports the observed lifecycle in `status.state`. A queue is
open for scheduling after its parent authorization and readiness conditions
are true. Before deleting a NamespaceQueue, the user must manually stop or
remove its workloads and wait until workload counters and scheduler-owned
runtime resources are drained. The user can inspect the NamespaceQueue tree
and status to determine when deletion is safe.


```go
// NamespaceQueueSpec defines the desired state of a NamespaceQueue.
type NamespaceQueueSpec struct {

	// Capability specifies the maximum resource allocation for this queue.
	//
	// +optional
	Capability v1.ResourceList `json:"capability,omitempty"`

	// Reclaimable indicates whether unused resources can be reclaimed.
	//
	// +optional
	Reclaimable *bool `json:"reclaimable,omitempty"`

	// Guarantee specifies the resources reserved for this queue.
	//
	// +optional
	Guarantee Guarantee `json:"guarantee,omitempty"`

	// Parent identifies a NamespaceQueue in the same namespace or a
	// cluster-scoped Queue. The reference format is defined in Section 3.4.1.
	//
	// +optional
	// +kubebuilder:default:=cluster/default
	Parent string `json:"parent,omitempty"`

	// Deserved specifies the expected resource allocation. Excess resources
	// may be shared and reclaimed when required.
	//
	// +optional
	Deserved v1.ResourceList `json:"deserved,omitempty"`

	// Priority specifies the scheduling priority of workloads in this queue.
	//
	// +optional
	// +kubebuilder:validation:Minimum=0
	Priority int32 `json:"priority,omitempty"`

	// DequeueStrategy specifies how workloads are selected from this queue.
	//
	// +optional
	// +kubebuilder:default:=traverse
	// +kubebuilder:validation:Enum=fifo;traverse
	DequeueStrategy DequeueStrategy `json:"dequeueStrategy,omitempty"`

}
```

The resource fields define the NamespaceQueue's resource partition. API-level and hierarchy-level validation rules are described in Sections 3.2 and 3.4.3.

#### 3.2.3 Basic Example

The following example creates a `NamespaceQueue` named `training` in the `team-a` namespace. It uses the cluster-scoped Queue `research` as its parent. The parent Queue must exist and include `team-a` in its `spec.allowedNamespaces`.

The `status` field is omitted because it is maintained by the `NamespaceQueue` controller. The `dequeueStrategy` field is shown explicitly; when omitted, it defaults to `traverse`.

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: NamespaceQueue
metadata:
  name: training
  namespace: team-a
spec:
  parent: cluster/research
  capability:
    cpu: "100"
    memory: "200Gi"
  deserved:
    cpu: "60"
    memory: "120Gi"
  guarantee:
    resource:
      cpu: "20"
      memory: "40Gi"
  reclaimable: true
  dequeueStrategy: traverse
```


#### 3.2.4 API Validation and Defaulting

API validation is limited to object-local invariants. These invariants are enforced by the CRD structural schema and CEL where possible. Request-time reference validation is performed by the admission webhook, while validation that depends on parent or sibling state is performed by the `NamespaceQueue` controller.

The following rules apply during API admission:

- `spec.parent` must use the supported parent-reference format;
- resource quantities must be valid Kubernetes quantities;
- for each resource for which the corresponding values are specified, `guarantee <= deserved <= capability`;
- `spec.dequeueStrategy` must be `fifo` or `traverse`;
- `spec.priority` must be non-negative; and
- cross-namespace parent references are not supported.

The API applies the following defaults to omitted fields:

- `spec.parent` defaults to `cluster/default`;
- `spec.dequeueStrategy` defaults to `traverse`.

Defaulting does not grant authorization, establish parent existence, or make a `NamespaceQueue` ready for scheduling. Parent existence, authorization, hierarchy constraints, and scheduling readiness are evaluated by the `NamespaceQueue` controller as described in Section 3.4.

#### 3.2.5 Cluster Queue Authorization

Cluster administrators control which namespaces may attach NamespaceQueue hierarchies to a cluster-scoped Queue. The cluster-scoped `QueueSpec` includes the following optional field:

```go
type QueueSpec struct {
	// Existing scheduling fields are omitted.

	// AllowedNamespaces lists the namespaces whose NamespaceQueues may use
	// this Queue as their cluster-scoped parent. Each entry must be a valid
	// Kubernetes namespace name or the literal "*". The literal "*" allows
	// all namespaces and must be the only entry when present.
	//
	// +optional
	AllowedNamespaces []string `json:"allowedNamespaces,omitempty"`
}
```

An empty or omitted list denies NamespaceQueue attachment. A literal `*` allows NamespaceQueues from every namespace and must not be combined with namespace names. Each non-wildcard entry must be a valid Kubernetes namespace name. Regular-expression and other pattern matching are out of scope for this design. A newly created cluster-scoped `default` Queue is initialized with `allowedNamespaces: ["*"]` when the NamespaceQueue feature gate is enabled. Existing Queue objects are not modified automatically by the scheduler.

When upgrading from a Volcano version without NamespaceQueue support, an existing `default` Queue may not contain `spec.allowedNamespaces`. The scheduler preserves that existing configuration. If the administrator wants NamespaceQueues to use `cluster/default`, the administrator must explicitly configure the authorization:

```bash
kubectl patch queue default --type=merge \\
  -p '{"spec":{"allowedNamespaces":["*"]}}'
```

An administrator may authorize only selected namespaces by listing them in `allowedNamespaces`. An empty or omitted list remains a deny-all policy.

For example:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: research
spec:
  allowedNamespaces:
    - team-a
    - team-b
```

A NamespaceQueue in `team-a` may use `cluster/research` as its parent, while a NamespaceQueue in `team-c` may not unless `team-c` is listed in `research.spec.allowedNamespaces`. A Queue with `allowedNamespaces: ["*"]`, including the initialized `cluster/default` Queue, permits attachment from any namespace.

A NamespaceQueue has a cluster-scoped effective parent when its `spec.parent` resolves to `cluster/<queue-name>`. An update to `Queue.spec.allowedNamespaces` must be rejected if the resulting value would make an existing NamespaceQueue with that effective parent unauthorized. An administrator must first update each affected NamespaceQueue to use another valid parent before removing the namespace from `allowedNamespaces`.

#### 3.2.6 NamespaceQueueStatus

`NamespaceQueueStatus` represents the observed state of a `NamespaceQueue`. It includes:

- the queue lifecycle `state`;
- `status.conditions`, including the `Authorized` and `Ready` condition types;
- workload counters derived from associated `PodGroup` resources; and
- runtime resource fields such as `allocated` and `reservation`.

The `NamespaceQueue` controller owns the lifecycle state, conditions, and workload counters. The Scheduler owns runtime allocation and reservation fields. Both components update their owned fields through the `status` subresource. The condition semantics and status transitions are defined in Section 3.4.2.

### 3.3 Admission Webhook

The admission webhook performs synchronous validation for API requests. It is responsible for:

- validating queue references in `Job`, `PodGroup`, and supported queue annotations against the shared queue-reference grammar defined in Section 3.6;
- honoring existing workload queue defaulting and annotation precedence rules; an omitted workload queue annotation is not a validation error;
- validating the effective `NamespaceQueue.spec.parent` after API defaulting; an omitted parent resolves to `cluster/default`, while an explicitly empty parent is invalid;
- performing a best-effort check that the referenced cluster-scoped `Queue` exists and that the `NamespaceQueue` namespace is allowed by the Queue's `spec.allowedNamespaces`;
- rejecting an update to a cluster-scoped Queue's `spec.allowedNamespaces` if the resulting value would make an existing NamespaceQueue with that effective parent unauthorized;
- rejecting malformed or unsupported queue references and references that fail authorization checks; and
- performing an initial deletion-safety check for `NamespaceQueue` deletion requests.

Admission validation applies to individual API requests. The webhook does not maintain parent hierarchy state, reconcile subtrees, manage lifecycle transitions, or continuously reconcile existing objects. The `NamespaceQueue` controller remains responsible for parent resolution, dynamic authorization, readiness, lifecycle, and deletion safety, as described in Section 3.4. The Scheduler performs the final scheduling eligibility check.

The detailed workload reference contract is defined in Section 3.6.

### 3.4 NamespaceQueue Controller

The NamespaceQueue Controller resolves NamespaceQueue parents, evaluates authorization and hierarchy constraints, updates lifecycle and status, and reconciles affected subtrees when related NamespaceQueues, cluster-scoped Queues, or PodGroups change. Existing cluster Queue Controller behavior remains unchanged.


#### 3.4.1 Parent Resolution

The reference forms are:

| Reference | Meaning |
| --- | --- |
| `<name>` | NamespaceQueue `<name>` in the current namespace |
| `cluster/<name>` | Cluster-scoped Queue `<name>` |
| omitted | `cluster/default` |
| `cluster/root` | Invalid: the scheduler-managed root queue cannot serve as a tenant parent. |

The CRD default applies when `spec.parent` is omitted. An explicitly empty `spec.parent` is rejected by the CRD schema. During parent resolution, the admission webhook, `NamespaceQueue` controller, and Scheduler consume the defaulted value `cluster/default` for an omitted field and reject an empty value from an invalid or legacy object.

The NamespaceQueue Controller resolves each API reference to a controller-internal target identifying the referenced Kubernetes object. The target must exist and satisfy authorization, hierarchy, and resource constraints; the `cluster/` prefix identifies a cluster-scoped parent and is removed before the controller looks up the Queue by name.

The target can be represented internally as:

```go
type QueueScope string

const (
	ClusterQueueScope   QueueScope = "cluster"
	NamespaceQueueScope QueueScope = "namespace"
)

type QueueTarget struct {
	Scope     QueueScope
	Namespace string     // empty for a cluster-scoped Queue
	Name      string
}
```

The target scope determines which lister is used:

```txt
Cluster target
    -> cluster Queue lister, keyed by name

Namespace target
    -> NamespaceQueue lister, keyed by namespace and name
```

The resolver may be implemented as follows:

```go
func ResolveNamespaceQueueParent(nq *NamespaceQueue) (QueueTarget, error) {
	parent := nq.Spec.Parent
	if parent == "" {
		return QueueTarget{}, fmt.Errorf("NamespaceQueue parent reference must not be empty")
	}

	if strings.HasPrefix(parent, "cluster/") {
		name := strings.TrimPrefix(parent, "cluster/")
		if name == "" || name == "root" {
			return QueueTarget{}, fmt.Errorf("invalid cluster NamespaceQueue parent")
		}
		return QueueTarget{Scope: ClusterQueueScope, Name: name}, nil
	}

	return QueueTarget{
		Scope:     NamespaceQueueScope,
		Namespace: nq.Namespace,
		Name:      parent,
	}, nil
}
```

After resolving the target, the controller looks up the parent object, validates its authorization and hierarchy state, and records the resulting parent-child relationship. A lightweight index is sufficient for reconciliation:

```go
childrenByParent map[QueueTarget]map[QueueTarget]struct{}
```

This index is used to find NamespaceQueue descendants and requeue them after a parent or sibling change.

#### 3.4.2 Authorization and Event-Driven Reconciliation

Multiple NamespaceQueues in one namespace may attach to the same cluster-scoped
Queue, and a namespace may attach NamespaceQueues to different cluster-scoped
Queues. Each attachment is authorized independently by the target Queue's
`spec.allowedNamespaces`; there is no one-root-per-namespace restriction.

Admission validation is the primary protection against authorization revocation. The controller re-evaluates `allowedNamespaces` on relevant Queue and NamespaceQueue informer events as a defense-in-depth mechanism. This protects against informer lag, concurrent updates, webhook configuration gaps, admission races, and pre-existing inconsistent states without requiring a periodic full rescan. Authorization is reported through status conditions; `State` (`Open`, `Closing`, `Closed`) remains the NamespaceQueue lifecycle.

The controller maintains `Authorized` and `Ready` conditions on each NamespaceQueue:

```yaml
status:
  state: Open
  conditions:
    - type: Authorized
      status: "True"
      reason: NamespaceAllowed
      message: namespace "team-a" is authorized to use research
    - type: Ready
      status: "True"
      reason: Ready
      message: NamespaceQueue is ready for scheduling
```

A NamespaceQueue participates in scheduling only when both conditions hold:

```txt
State == Open  &&  Ready == True
```

`Ready=True` requires a valid parent, authorization, hierarchy, resource constraints, and plugin support. `Authorized` remains separate to distinguish authorization failures from other readiness failures.

`Authorized` is an observed condition on the NamespaceQueue. It indicates whether the NamespaceQueue's namespace is authorized to use its resolved cluster-scoped parent according to the parent's current `spec.allowedNamespaces` value. It does not replace admission-time validation of `Queue.spec.allowedNamespaces` updates.

If the controller observes that an existing NamespaceQueue is no longer authorized despite admission protection, it sets `Authorized=False` and `Ready=False` on the affected NamespaceQueue subtree:

- the subtree becomes non-admitting;
- new workloads targeting the unauthorized subtree are rejected by admission where applicable, and pending workloads are not scheduled;
- running workloads are not evicted and may complete;
- NamespaceQueue objects are preserved.

When authorization is restored, the controller revalidates the complete subtree. Scheduling resumes only after all readiness checks pass.

Changes to a cluster parent, local parent, or sibling that affect authorization, hierarchy, or resource constraints requeue the affected subtree. A NamespaceQueue with a missing parent remains persisted with `Ready=False` and is reconsidered when the parent becomes available.

NamespaceQueue hierarchy depth is configured independently from the existing cluster Queue depth limit. The first NamespaceQueue attached to a cluster Queue has depth one; each NamespaceQueue parent adds one level. Cluster Queue ancestors are not included. The controller and admission webhook receive the same `--max-namespacequeue-depth` value and apply the same counting rule.

#### 3.4.3 Dynamic Validation and Resource Constraints

A NamespaceQueue fails dynamic validation if any of the following conditions apply:

- the referenced parent does not exist;
- the referenced cluster queue does not authorize the current namespace;
- the queue references itself as its parent;
- the parent relationship creates a hierarchy cycle;
- the child queue configuration violates constraints imposed by its parent.

Resource constraints are checked at both the individual queue and hierarchy levels.

The controller adds hierarchy-level checks that require parent and sibling state; object-local inequalities are covered by Section 3.2.

Capability is checked along the parent chain. A child capability must not exceed the nearest ancestor capability for the same resource. If no ancestor defines a capability for that resource, the child has no explicit capability constraint at that level.

Guarantee and deserved are aggregate constraints across direct children. For each resource, the following must hold:

```txt
sum(direct children guarantee) <= parent guarantee
sum(direct children deserved)  <= parent deserved
```

Sibling capabilities are not summed. Capability is a per-queue maximum rather than a reserved amount, so sibling queues may each have a capability below the parent's capability. Runtime allocation is still bounded by the parent and enforced by the capacity plugin.

If a parent does not configure a limit for a resource, that parent imposes no explicit limit for that resource. A missing resource in a child contributes zero to the corresponding sibling aggregate.

The NamespaceQueue controller performs these checks because parent and sibling state may change after object creation.

Dynamic validation failure does not delete the NamespaceQueue. The object remains available for repair and is reported as:

```txt
State = Open
Ready = False
Reason = ParentConstraintViolation
```


An invalid NamespaceQueue must not participate in workload scheduling.

The validation result is exposed through `NamespaceQueue.status.conditions` with an appropriate reason, such as:

```txt
ParentNotFound
NamespaceNotAllowed
RootParentForbidden
HierarchyCycle
InvalidParentReference
ParentConstraintViolation
```


#### 3.4.4 Compatibility with Existing Cluster Queue Hierarchies

Existing cluster-scoped Queue parent semantics and hierarchy behavior remain unchanged. A cluster Queue continues to reference another cluster Queue by name:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: gpu
spec:
  parent: research
```

This represents:

```txt
research
└── gpu
```

An authorized NamespaceQueue may attach below a cluster Queue and create further descendants in the same namespace:

```txt
root
└── research
    └── gpu
        └── team-a/department
            ├── team-a/training
            └── team-a/inference
```

#### 3.4.5 Lifecycle, Status, and Deletion

NamespaceQueues are open for scheduling when their parent, authorization, hierarchy, resource, and readiness checks pass. The controller reports the observed lifecycle in `status.state`.

Authorization state and lifecycle state are independent. If the Controller observes an authorization failure despite admission protection, it updates `Authorized` and `Ready` without changing `status.state`.

To detach a namespace from a cluster-scoped Queue, an administrator must first update each affected NamespaceQueue to use another valid parent. The namespace may be removed from `allowedNamespaces` only after no existing NamespaceQueue with that effective parent would become unauthorized.

Before deletion, users must first detach all child NamespaceQueues and
manually drain workload and scheduler-owned runtime state. The NamespaceQueue
admission webhook rejects deletion while either condition is false. After the
delete is accepted, the controller finalizer rechecks those conditions to
protect against a deletion race, then completes Kubernetes object cleanup; it
does not terminate workloads or perform the drain on the user's behalf.

The Controller owns lifecycle state, conditions, and workload counters derived from PodGroups. The Scheduler owns runtime allocation and reservation fields. Both components update only their owned status fields.

Deletion of a Queue or NamespaceQueue is blocked while it is referenced by
child queues. NamespaceQueue deletion is also blocked while workload counters
or scheduler-owned runtime resources remain. Users are responsible for
draining these resources before issuing the delete request.

### 3.5 Scheduler Integration

The Scheduler watches Queue and NamespaceQueue resources, converts them into a common `QueueInfo` model, maintains the internal queue cache and runtime resource accounting, and performs the final readiness check at the scheduling boundary.

#### 3.5.1 Queue Identity

Canonical `QueueID`s use `<queue-name>` for cluster-scoped Queues and `<namespace>/<queue-name>` for NamespaceQueues. Construction is centralized, and `QueueInfo` stores scope and namespace explicitly.

```go
func NamespaceQueueID(namespace, name string) QueueID {
	return QueueID(namespace + "/" + name)
}
```

Scheduling logic must not infer resource scope solely by parsing `QueueID`.

#### 3.5.2 QueueInfo Conversion

Queue and NamespaceQueue are converted into the same `QueueInfo` representation before entering the scheduling framework. The shared model contains the canonical identity, resolved parent, common scheduling fields, and runtime state. Existing cluster Queue fields such as `Weight`, `Hierarchy`, and `Weights` remain available for compatibility but are not NamespaceQueue API fields. Plugins consume the normalized fields through the same scheduling path for both resource types.

`NewQueueInfo` preserves the existing cluster Queue identifier, parent semantics, and scheduling behavior. `NewNamespaceQueueInfo` assigns the namespaced `QueueID`, independently resolves `spec.parent` using the shared parent grammar, and maps the NamespaceQueue fields into `QueueInfo`. An invalid or non-ready NamespaceQueue is excluded from active scheduling.

#### 3.5.3 Event Processing

Separate Queue and NamespaceQueue informers feed the shared scheduler cache:

```txt
add    -> convert and insert by QueueID
update -> re-resolve, validate, and replace by QueueID
delete -> remove by QueueID
```

Invalid objects remain available for repair but are excluded from active scheduling.

#### 3.5.4 Plugin Scope

The initial implementation supports NamespaceQueue only in the capacity plugin. Its hierarchical model partitions resources by `deserved`, `capability`, and `guarantee`, with each NamespaceQueue subtree bounded by its cluster-scoped parent's allocation.

The proportion plugin and other scheduling plugins are out of scope until NamespaceQueue support is extended to them.

#### 3.5.5 Metrics

Queue and NamespaceQueue metrics use the canonical `QueueID` as the value of the existing `queue_name` label. A cluster-scoped Queue uses `<queue-name>`, while a NamespaceQueue uses `<namespace>/<queue-name>`.

For example:

```text
volcano_queue_allocated_milli_cpu{queue_name="training"} 4000
volcano_queue_allocated_milli_cpu{queue_name="team-a/training"} 2000
```

The `volcano_queue_allocated_milli_cpu` metric reports the CPU allocated to each queue in millicores. The `/` character is part of the label value and does not require escaping or replacement.

Using the canonical `QueueID` prevents a cluster-scoped Queue and a NamespaceQueue with the same name from producing ambiguous metric series. A separate `queue_scope` label is not required because the `QueueID` already identifies the resource scope.

### 3.6 Workload Integration and Queue Resolution

Workloads retain their raw queue reference in the API object. The Webhook and Scheduler use the same resolver to map that reference and the workload namespace to a canonical `QueueID`; no shadow Queue is created.

#### 3.6.1 Queue Reference Rules

| Reference | Resolves to |
| --- | --- |
| `<name>` | Cluster-scoped Queue `<name>` |
| `namespace/<name>` | NamespaceQueue `<name>` in the workload's namespace |
| omitted | Existing workload defaulting behavior, normally cluster Queue `default` |

The `namespace/` prefix identifies the resource type, not a Kubernetes namespace. Cross-namespace NamespaceQueue references are not supported, and resolution never falls back to another resource type or to a default Queue.

#### 3.6.2 Resolution and Readiness

The shared resolver validates the reference, selects the resource scope, and returns the canonical `QueueID`. The workload retains the raw reference, while scheduling uses only the resolved ID. Missing, malformed, or non-ready references do not fall back to another Queue; the workload remains pending or unschedulable and is reconsidered when the target becomes available and ready.

#### 3.6.3 Workload Sources and Compatibility

Job, PodGroup, and the existing `scheduling.volcano.sh/queue-name` annotation use the same resolver. The raw reference remains unchanged in the workload API object and annotation; labels are compatibility metadata and are not used as the scheduler's source of truth. Existing defaulting and annotation precedence behavior remains unchanged, while `namespace/default` explicitly selects a NamespaceQueue named `default` in the workload's namespace.

#### 3.6.4 Admission and Scheduling Enforcement

The webhook validates the reference grammar and performs a best-effort existence and authorization check. The Scheduler repeats resolution and performs the authoritative readiness check at the scheduling boundary. The workload API schema must accept both `<name>` and `namespace/<name>` forms, and malformed or cross-namespace references must be rejected.

## 4. Optimization Roadmap

The initial implementation keeps the existing Volcano mechanisms: a
namespaced `NamespaceQueue` CRD, the NamespaceQueue controller, the shared
`QueueInfo` scheduler model, and the scheduler as the final readiness gate.
The following optimizations are intentionally separated from the core API and
are applied incrementally.

### 5.1 Implemented in the initial controller hardening

- A NamespaceQueue parent can change only after the old queue is `Closed` and
  fully drained. An omitted parent is defaulted to `cluster/default`; an
  explicitly empty parent is invalid.
- The NamespaceQueue feature gate controls controller startup. Users should
  drain and delete NamespaceQueues before disabling the feature gate; the
  controller does not terminate workloads as part of feature-gate changes.
- Changes to a NamespaceQueue, its parent, or its cluster Queue requeue the
  affected old and new subtrees through informer indexes.
- DeletionTimestamp transitions are queued immediately.
- `Authorized` and `Ready` condition changes emit Events only when their
  status, reason, or message changes.

### 5.2 Next-stage validation and optimization

The following items require cluster-level verification and remain outside the
current implementation:

- NamespaceQueue E2E coverage for authorization, hierarchy, parent changes,
  close-and-drain, deletion, scheduler accounting, and Feature Gate rollback.
- Controller readiness reporting when informer cache synchronization fails.
- Metrics and load testing for descendant propagation and high-frequency
  PodGroup updates before adding event batching.
- Evaluation of a selector-based authorization API or a richer stop policy.
  These are not part of the current compatibility contract and should not be
  introduced without a separate API proposal.
