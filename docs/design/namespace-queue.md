# Namespace-scoped Queues for Tenant-authored Hierarchies

[@gitGurugu](https://github.com/gitGurugu); Jul 3, 2026

Tracking issue: [#5251](https://github.com/volcano-sh/volcano/issues/5251)

> **Status: DRAFT for design review.** Items marked `TODO(mentors)` are open
> questions to align on before implementation starts.

## Motivation

Volcano's `Queue` is a **cluster-scoped** resource. On multi-tenant platforms,
tenants live in namespaces and typically cannot create cluster-wide CRDs without
going through an admin review process. As a result, Volcano's hierarchical-queue
feature (implemented on the `capacity` plugin) is in practice only accessible to
cluster admins, not to the tenants who actually need it to configure their own
scheduling strategy.

Platforms that want to offer Volcano *as a service* are left with no native
solution. Each one ends up building its own abstraction layer: a namespaced CRD,
a controller that translates it into cluster `Queue` objects, and custom
validation to stop tenants from breaking the global hierarchy and the scheduling
cycle. This is duplicated work across every platform team, and it does not fully
solve the problem: hierarchy validation in Volcano happens **inside the scheduler
loop**, against the global queue tree, *after* the cluster `Queue` is already
admitted. Those rules also evolve over time, so out-of-tree controllers go stale
quickly.

## Proposal

We introduce a new **namespaced** CRD `NamespaceQueue` that participates natively
in Volcano's existing queue hierarchy. The naming follows the Kubernetes
`Role`/`ClusterRole` convention: `Queue` stays the cluster-scoped resource, and
`NamespaceQueue` is its namespace-scoped counterpart.

A `NamespaceQueue` explicitly names its parent, which is either another
`NamespaceQueue` in the same namespace, or a cluster `Queue`. There is no
artificial one-to-one binding between a namespace and a cluster `Queue`: multiple
namespaces may reference the same cluster `Queue`, and a namespace may host
`NamespaceQueue`s rooted under different cluster `Queue`s.

### Goals

- Provide a native namespaced queue primitive so platforms do not need to build
  their own abstractions and duplicate validation logic.
- Let tenants create and manage queue hierarchies **within their own namespaces**,
  without cluster-admin intervention for every change.
- Guarantee that a misconfiguration in one tenant's hierarchy cannot affect
  scheduling for other tenants.
- Remain **backward compatible**: clusters that do not opt in behave exactly as
  today.
- Support traditional workloads via the existing `PodGroup.spec.queue` /
  `scheduling.volcano.sh/queue-name` path, enabling smooth migration: if a
  `NamespaceQueue` with the same name as a cluster `Queue` exists in the
  workload's namespace, newly admitted workloads resolve to the namespace-scoped
  resource without changing workload semantics.
- Deliver an end-to-end test suite covering core flows, including negative paths.
- Provide user-facing docs in both the website and the repository.

### Non-Goals

- Replacing or wrapping the cluster-scoped `Queue`. `NamespaceQueue` is
  **additive**; the current model is unchanged for clusters that do not opt in.
- Introducing new tenant-isolation semantics.
- Changing the meaning of `guarantee`, `capability`, or `weight`. The fields
  `NamespaceQueueSpec` carries over from `Queue` keep their current definition and
  interpretation; this proposal does not alter existing resource semantics.

## User Stories

### Story 1

As a tenant with permissions only in my own namespace, I want to create a queue
hierarchy under the cluster `Queue` my platform assigned me, and attach my
`PodGroup`/`Job`s to it — without filing a ticket for the cluster admin on every
change.

### Story 2

As a platform operator, I want a tenant's broken hierarchy (cycle, missing
parent, over-committed child) to stay inert and never enter the global scheduler
tree, so it cannot disturb other tenants' scheduling.

### Story 3

As an existing user, I want my current cluster `Queue`s and workloads to behave
exactly as before if I don't enable this feature.

## Design detail

### The `NamespaceQueue` CRD

`NamespaceQueue` is namespace-scoped and defines its own **`NamespaceQueueSpec`** —
a curated subset of the cluster `Queue` surface (`parent`, `weight`, `capability`,
`guarantee`). Each reused field keeps the same meaning it has on `Queue` today; the
fields not carried over (`deserved`, `reclaimable`, `priority`, `dequeueStrategy`)
are intentionally out of the v1 surface.

Access control is **not** a field on `NamespaceQueueSpec`. Whether a namespace may
parent its queues under a given cluster `Queue` is governed by a new
`allowedNamespaces` field on the cluster `Queue` itself (see
[Parent-queue ownership enforcement](#parent-queue-ownership-enforcement) below) —
the authorization must be owned by the admin who owns the `Queue`, not declared by
the tenant who is requesting to bind to it.

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
    // ===== 1. hierarchy (core) =====
    // parent can be a cluster Queue or another NamespaceQueue
    // (resolution is namespace-first).
    Parent string `json:"parent"`

    // ===== 2. scheduling policy =====
    // queue weight used in fair sharing / DRF
    Weight int32 `json:"weight,omitempty"`

    // ===== 3. resource policy (optional) =====
    // max resources this queue can use
    Capability corev1.ResourceList `json:"capability,omitempty"`
    // guaranteed resources
    Guarantee corev1.ResourceList `json:"guarantee,omitempty"`
}
```

`NamespaceQueueStatus` is a purpose-built, trimmed status: a readiness gate the
scheduler keys on, a human-readable failure reason, the scheduler-reported
allocation, and the resolved child list for hierarchy introspection.

```go
type NamespaceQueueStatus struct {
    // ===== 1. scheduler gate =====
    // only Ready=true queues can enter the scheduling tree
    Ready bool `json:"ready"`

    // failure reason for debugging (why the queue is not Ready)
    Reason string `json:"reason,omitempty"`

    // ===== 2. runtime allocation =====
    // actual allocated resources from the scheduler
    Allocated corev1.ResourceList `json:"allocated,omitempty"`

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

Enforcement happens in the `NamespaceQueue` **validating webhook** at admission:

```
On NamespaceQueue admit/update, if spec.parent resolves to a cluster Queue Q:
    reject unless NamespaceQueue.metadata.namespace ∈ Q.spec.allowedNamespaces
When spec.parent resolves to a same-namespace NamespaceQueue:
    no cross-tenant check needed (both objects are tenant-owned in one namespace)
```

Because a tenant cannot edit the cluster-scoped `Queue` object (RBAC forbids it),
a tenant cannot add its own namespace to `Q.spec.allowedNamespaces`, so it cannot
grant itself access — the webhook denies `parent: queue-b` for an unlisted
namespace. This is the concrete answer to the ownership question: the deny/allow
decision is authored by the resource owner (admin) and merely *checked* against the
requester (tenant). A future revision may replace the static list with a
`namespaceSelector` for larger fleets.

> `TODO(mentors)`: how should `spec.parent` distinguish "same-namespace
> NamespaceQueue" from "cluster Queue"? Options:
> - **(A) namespace-first by convention** — a bare name resolves to a
>   same-namespace `NamespaceQueue` first, else a cluster `Queue`. Simplest, but
>   ambiguous if both exist with that name.
> - **(B) explicit `parentKind`** field (`NamespaceQueue` | `Queue`). Explicit,
>   no ambiguity, but adds surface area.
> Recommendation: **(A)** to mirror the PodGroup resolution rule and keep the
> spec unchanged; document the shadowing rule clearly.

### QueueID namespace-qualification (core mechanism)

The scheduler treats `api.QueueID` as an **opaque key**; it never assumes the ID
equals the queue name. There is exactly one place that turns a name into a
`QueueID` for the scheduler:

```go
// pkg/scheduler/api/queue_info.go  (NewQueueInfo)
UID: QueueID(queue.Name)
```

For a `NamespaceQueue` we derive a **namespace-qualified synthetic ID**:

- cluster `Queue`      → `QueueID(name)`            *(unchanged)*
- `NamespaceQueue`     → `QueueID(namespace + "/" + name)`
- synthetic root       → `QueueID("root")`          *(stays cluster-global)*

Because every downstream consumer — the `capacity` tree
(`queueAttr.children`/`ancestors`), leaf detection, bottom-up resource
aggregation, hierarchical allocatable checks, and the flat `proportion` map —
keys purely on `QueueID`, **they need no logic changes**. Only three
"translation" points become namespace-aware:

| Where | Today | Compatibility change |
| --- | --- | --- |
| `pkg/scheduler/api/queue_info.go` `NewQueueInfo` | `QueueID(name)` | qualify when the source object is a `NamespaceQueue` |
| `capacity` parent lookup (`updateAncestors`) | `ssn.Queues[QueueID(spec.Parent)]` | resolve `spec.parent` namespace-first into the synthetic ID |
| `pkg/scheduler/api/job_info.go` `SetPodGroup` | `QueueID(pg.Spec.Queue)` | resolve `(pg.Namespace, pg.Spec.Queue)` namespace-first |

The `volcano.sh/hierarchy` / `hierarchy-weights` annotations store **queue name
strings**, so their format is independent of namespace and the webhook mutator is
unaffected.

### PodGroup.spec.queue resolution (namespace-first)

Resolution for a `PodGroup` in namespace `N` referencing queue name `Q`:

1. If a `Ready` `NamespaceQueue` `N/Q` exists → use synthetic ID `QueueID("N/Q")`.
2. Otherwise → fall back to cluster `Queue` `Q`, exactly as today.

No new `PodGroup` field is required, and existing workloads are unaffected: with
no `NamespaceQueue` present, step 1 never matches and behavior is identical to
today. This is also the smooth-migration path — creating a same-named
`NamespaceQueue` shadows the cluster `Queue` for *new* admissions only.

> `TODO(mentors)`: migration semantics for **already-running** PodGroups when a
> shadowing NamespaceQueue is created mid-flight — re-resolve on next session, or
> pin at admission? Recommendation: resolve at admission and keep it stable for
> the PodGroup's lifetime.

### NamespaceQueue controller & status

A new controller under `pkg/controllers/namespacequeue/` mirrors
`pkg/controllers/queue/` (controller + handler + state machine + actions):

- **Cross-resource validation** reconciled into `status`: parent exists (same-ns
  NamespaceQueue or cluster Queue), no cycles, child `guarantee` sums ≤ parent,
  child `capability` ≤ parent — the same rules the capacity plugin enforces,
  surfaced early. (Parent-*ownership* — whether this namespace is allowed to
  parent under the target cluster `Queue` — is enforced earlier, at admission, by
  the validating webhook; see
  [Parent-queue ownership enforcement](#parent-queue-ownership-enforcement).) On
  success it sets `status.Ready = true`; on failure it sets `Ready = false` and
  records `status.Reason`.
- **Usage write-back**: watches `PodGroup`s in its namespace and populates
  `status.Allocated`, reusing the cluster queue controller's allocation logic. It
  also resolves and writes `status.Children` for hierarchy introspection.

The scheduler continues to be the source of truth for the *global* tree; the
controller provides fast, tenant-visible feedback and the readiness gate.

### Scheduler ready-gating & isolation

The scheduler cache watches `NamespaceQueue` (new `Add/Update/Delete` handlers in
`pkg/scheduler/cache/event_handlers.go`) and **admits into the queue tree only
those with `status.Ready == true`**. A misconfigured `NamespaceQueue` stays in
etcd but never becomes a `QueueInfo`, so it cannot enter the capacity hierarchy
tree or affect any other tenant — this is the isolation guarantee.

### Capacity / proportion plugin compatibility

No behavioral change is proposed for either plugin. They operate entirely on
`QueueID` + the `spec.parent`/hierarchy annotations, both of which the synthetic
ID and namespace-first parent resolution keep valid. The existing leaf-only
scheduling rule, bottom-up aggregation, and `ancestorReclaimLevel` reclaim scope
all apply unchanged to a mixed tree of cluster `Queue`s and `NamespaceQueue`s.

### Webhook & RBAC

- Validating/mutating webhooks for `NamespaceQueue` mirror
  `pkg/webhooks/admission/queues/` (basic spec checks + hierarchy annotation
  prepend). The webhook also enforces **parent-queue ownership**: when
  `spec.parent` targets a cluster `Queue`, it rejects the object unless the
  NamespaceQueue's namespace appears in that `Queue.spec.allowedNamespaces` (see
  [Parent-queue ownership enforcement](#parent-queue-ownership-enforcement)).
  Deeper cross-resource validation (cycles, resource sums) lives in the
  controller/status, to match how Volcano validates hierarchy today.
- A namespaced `Role` (+ optional aggregated `ClusterRole` label) grants tenants
  CRUD on `NamespaceQueue` in their own namespace — the concrete "no cluster
  admin needed" mechanism. Crucially, tenants get **no** write access to
  cluster-scoped `Queue`, so they cannot edit `Queue.spec.allowedNamespaces` to
  self-authorize.

## Feature gate

The whole feature sits behind a new gate, **default off**, so non-opting clusters
are byte-for-byte unchanged:

```go
// pkg/features/volcano_features.go
// NamespaceQueue enables the namespace-scoped NamespaceQueue CRD and its
// namespace-first PodGroup queue resolution.
NamespaceQueue featuregate.Feature = "NamespaceQueue"

// in defaultVolcanoFeatureGates:
NamespaceQueue: {Default: false, PreRelease: featuregate.Alpha},
```

When the gate is off, the cache does not watch `NamespaceQueue`, resolution is
name-only against cluster `Queue`s, and the controller does not run.

## Test plan

- **Unit**: synthetic-ID derivation; namespace-first PodGroup resolution
  (shadowing + fallback); controller validation → `status.Ready` transitions.
- **E2E (Ginkgo, `test/e2e/`)**:
  - tenant creates a `NamespaceQueue` under a cluster `Queue` and schedules a Job.
  - two namespaces with same-named `NamespaceQueue`s scheduled independently.
  - negative: cycle / missing parent / over-committed child ⇒ `Ready=false`, and
    other tenants keep scheduling.
  - backward-compat: gate off ⇒ existing cluster-queue e2e unchanged.

## Limitations

- Leaf-only scheduling is inherited from the capacity hierarchy design: jobs
  attach only to leaf queues.
- `TODO(mentors)`: cross-namespace parent (a NamespaceQueue parented by a
  NamespaceQueue in *another* namespace) is out of scope for v1 — parents are
  same-namespace NamespaceQueue or cluster Queue only.

## Open questions for review

1. `spec.parent` disambiguation: convention (A) vs explicit `parentKind` (B).
2. Mid-flight re-resolution when a shadowing NamespaceQueue appears.
3. `NamespaceQueueSpec` is a curated subset of `Queue` (`parent`, `weight`,
   `capability`, `guarantee`) rather than a reuse of `QueueSpec`. Confirm the
   dropped fields (`deserved`, `reclaimable`, `priority`, `dequeueStrategy`) are
   acceptable to leave out of the v1 surface.
4. `allowedNamespaces` model: is a static namespace list sufficient, or should it
   support label selectors for larger multi-tenant fleets?
5. RBAC packaging: ship a ready-made tenant `Role` in the Helm chart, or document
   only.
