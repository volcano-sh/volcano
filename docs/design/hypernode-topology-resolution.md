# Improving Topology Visibility and Integration with the HyperNodeTopology API

Author: [@kevin-wangzefeng](https://github.com/kevin-wangzefeng) · Oct. 24, 2026

---

## 1. Background

### 1.1 HyperNode Design Goals Revisited

Distributed AI workloads exchange data across accelerators and Kubernetes Nodes. Their placement depends on communication locality within racks, switches, zones, and other network performance domains. Cluster operators and schedulers need a common way to describe those domains and their relationships.

The [original network topology design](./Network%20Topology%20Aware%20Scheduling.md#network-topology-management) introduced HyperNode as a structured API for performance domains. It gives users and components explicit domain identities, tiers, and membership declarations rather than requiring each integration to establish its own label conventions. Labels remain useful as discovery inputs and membership selectors.

HyperNode also expresses hierarchy. A domain can contain Kubernetes Nodes or child domains, allowing users to describe communication locality at different levels. This is more descriptive than a set of independent topology attributes and provides a common model for topology-aware placement.

Topology management is separate from scheduling. Administrators can maintain HyperNodes directly, or use the HyperNode Controller and pluggable Discoverers to maintain them from sources such as Node labels and vendor management systems. Adapting those sources to one model lets scheduling and management tools work with the same API across environments where the corresponding integrations are available.

The original design also called for a topology that engineers can inspect without constructing the tree themselves, that agrees with the scheduler's interpretation, and that other components can use. This shared understanding is important both for troubleshooting placement and for integrating dashboards or other cluster-management tools.

Topology domains provide a useful basis for operational information as well. Resource summaries and domain-level health observations can help operators locate problems affecting a device, a Node, or a communication domain. These are extension use cases, not a claim that HyperNode already provides a general health federation capability.

### 1.2 A Shared Topology View

The existing API describes topology through individual domains. This supports independent discovery and updates, while the complete topology brings those domains and their actual Node members together into a view that users and components can query.

This proposal builds on the original design by making that complete view easier to inspect and share. It starts with a CLI command and public helpers, then introduces the `HyperNodeTopology` API and gradually adopts it in the CLI and scheduler. HyperNode remains the source of topology declarations.

## 2. Problem Statement

### 2.1 Individual HyperNodes Do Not Provide a Complete Topology View

HyperNode can record topology domains and their membership relationships, but `kubectl get hypernodes` presents individual resources. An administrator still has to follow references, resolve selectors against current Nodes, identify roots, and assemble the resulting tree or forest.

Domain-level resources allow a local change without rewriting a cluster-wide declaration. An operator inspecting the cluster, however, needs to see how those domains fit together and which Nodes belong to them.

For example, an operator checking a rack membership change needs to establish which root contains the rack, which Nodes actually match its selectors, and whether another domain selects those Nodes as well. The declarations contain the inputs to those answers, but the operator must reconstruct the answers manually or write a tool.

The topology information exists, but users lack a direct way to inspect it as a complete tree or forest.

### 2.2 Duplicated Topology-building Logic in Downstream Integrations

Custom schedulers, dashboards, and cluster-management systems need domain relationships and actual Node membership. Generated clients can retrieve individual HyperNodes, but do not assemble that topology. Without public helpers or a topology-view API, downstream developers must copy Volcano's logic or build their own.

Each implementation repeats work such as matching Node selectors, connecting parent and child domains, and identifying roots. Differences in handling overlapping members or missing references can produce different views of the same inputs. Importing scheduler implementation packages couples downstream applications to internal code rather than a supported integration interface.

### 2.3 The Scheduler's Topology View Is Not Exposed to Users

The Volcano scheduler already maintains a structured topology view in memory, including domain relationships and Node membership. Users can inspect individual HyperNode resources, but cannot directly query that assembled view through a supported topology API.

The CLI and public helpers can first reuse the scheduler's topology-building logic. Publishing the topology through an API then lets users and components read identified snapshots without assembling the source objects themselves. The consistency guarantees and limits of these steps are described in Section 7.2.

## 3. Existing Foundation

### 3.1 API and Discovery

The existing HyperNode API declares tiers, optional tier names, and members. Members can reference HyperNodes or select Kubernetes Nodes using the supported exact, regex, and label forms. The discovery framework already converts source-specific information into this shared representation.

These capabilities remain in place. Users do not need to author a second topology, and Discoverers do not need to produce a second source model.

### 3.2 Topology View Maintained by the Scheduler

The scheduler already maintains the following implementation types:

| Type | Existing responsibility |
| --- | --- |
| `HyperNodesInfo` | Manage HyperNode relationships, tier groupings, subtree Node membership, readiness, and an effective topology generation |
| `HyperNodeInfo` | Record one domain's parent, children, tier information, and associated HyperNode object |
| `HyperNodeIndex` | Derive ancestry and membership indexes for a topology snapshot |

The existing entry points for building and querying this view include `NewHyperNodesInfo`, `UpdateHyperNode`, `HyperNodes`, and `RealNodesSet` in `pkg/scheduler/api`. Node member selection is shared through `pkg/util/hypernode.GetMembers`.

These implementations provide a starting point for the CLI and helpers. The public API should expose domain relationships and membership rather than serialize scheduler memory. Locks, queues, redundant indexes, and process-local generations have implementation-specific lifetimes; a public model needs explicit object identities, compatibility rules, and lifecycle semantics.

The proposal first extracts the shared topology-building logic, then moves responsibility for maintaining the API view to a controller. Scheduling state remains inside the scheduler.

## 4. Goals and Non-Goals

### 4.1 Goals

- Let cluster administrators inspect the complete HyperNode topology, including independent roots, domain relationships, Kubernetes Node leaves, and errors, without manually assembling it.
- Give Go integration developers public helpers for building and querying topology without copying or importing scheduler internals.
- Let dashboards and independently deployed components read a controller-maintained topology view through Kubernetes APIs, without embedding Go topology-building logic.
- Keep topology interpretation consistent across the CLI, helpers, controller, and scheduler by reusing the same logic for equivalent inputs.
- Gradually move the CLI and scheduler to the shared topology API while preserving existing scheduling behavior and providing explicit migration and rollback paths.

### 4.2 Non-Goals

- Replace the HyperNode API, existing discovery integrations, or user-authored topology declarations.
- Change topology selection, scoring, preemption, reclaim, or Gang-scheduling policy.
- Expose scheduler Sessions, workload occupancy, or allocations through the topology API.
- Introduce domain-wide health federation, free-device accounting, or cross-vendor resource normalization. Node and resource summaries may enrich the CLI without adding these capabilities to the structural API snapshot.
- Provide transactional consistency across source resources or support arbitrary graph relationships.

Further HyperNode scheduling algorithm improvements should use the public topology model but be designed and tracked in a separate proposal/enhancement.

## 5. Proposal

### 5.1 HyperNode Declarations and the Shared Topology View

The two APIs have different responsibilities:

| Representation | Role | Writer |
| --- | --- | --- |
| `HyperNode` | Declare one topology domain, its tier, and direct members or member selectors | Existing discovery controller or an authorized user/integration |
| `HyperNodeTopology` | Publish a topology view containing domain relationships and direct Kubernetes Node membership | HyperNodeTopology Controller |
| Consumer-local indexes | Support traversal, ancestry lookup, subtree membership, and consumer-specific processing | Each consumer, derived from a published snapshot |

The intended data flow is:

```text
Discovery systems / user declarations
                  |
                  v
              HyperNodes ----+
                             |
Kubernetes Nodes ------------+--> Shared resolver
                                      |
                       +--------------+--------------+
                       |                             |
                 Phase 1                         Phase 2+
                 client view           HyperNodeTopology Controller
                                                     |
                                                     v
                                            HyperNodeTopology
                                               /     |     \
                                            vcctl  adopters  scheduler
                                                             (Phase 3)
```

The new controller derives topology; it does not discover the underlying fabric. Existing HyperNode discovery remains responsible for producing the source objects.

`HyperNodeTopology` names the public concept, not an internal data structure. The supported domain relationships remain hierarchical: each HyperNode has at most one parent, and the result is a tree or forest. The name does not imply support for arbitrary graphs or make a future change to graph semantics automatically compatible.

A tree is one rooted HyperNode hierarchy; a forest is a collection of independent trees. Each `HyperNodeTopology` object stores exactly one complete tree, including all domain records, relationships, and resolved Kubernetes Node identities. A forest is represented by multiple objects. Readers do not need to fetch source HyperNodes or Nodes to reconstruct the topology; current Node status and resources remain separate enrichment.

### 5.2 A Shared Topology View for Users and Components

The CLI lets administrators inspect the topology as a tree or forest. Public helpers let integrations build and query the same structure without duplicating the scheduler's logic. `HyperNodeTopology` makes that view accessible across languages and processes. The scheduler can then read the shared view while keeping scheduling decisions local.

Each step can be reviewed and used before the next one is enabled. The API representation and publication protocol need not block the initial client command.

## 6. Implementation Phases

The proposal has three main phases: client-side inspection and helpers, a shared topology API, and scheduler adoption. Work items within a phase describe implementation dependencies, not separate top-level phases. A phase may span multiple pull requests or releases.

### 6.1 Phase 1: CLI Inspection and Public Helpers

Improve inspection and programmatic integration without adding a CRD or controller. The CLI initially reuses existing scheduler code; public helpers then provide a supported integration interface and a common implementation.

#### 6.1.1 CLI Inspection

Add a read-only command:

```shell
vcctl hypernode tree
vcctl hypernode tree --kubeconfig /path/to/config
vcctl hypernode tree --charset=unicode
vcctl hypernode tree --charset=ascii
```

The command lists HyperNodes and Kubernetes Nodes, builds their actual forest, and displays the direct Node leaves under each domain. It retains the existing vcctl connection and authentication conventions.

Initially, the implementation reuses the scheduler entry points and shared member-selection helper described in Section 3.2.

A local Node indexer/lister can adapt a one-shot snapshot to the current resolver without starting an informer or watch. The CLI must not copy the parent/child construction algorithm. Direct member expansion must not attach every recursive descendant Node to every ancestor.

Example topology-only output:

```text
NAME                  TIER    STATUS  NODES  READY  CORDONED
independent-domain    1/rack  -       1      1/1    0
└── node-05           -       Ready   -      -      -
zone-a                2/zone  -       4      4/4    0
├── rack-a            1/rack  -       2      2/2    0
│   ├── node-01       -       Ready   -      -      -
│   └── node-02       -       Ready   -      -      -
└── rack-b            1/rack  -       2      2/2    0
    ├── node-03       -       Ready   -      -      -
    └── node-04       -       Ready   -      -      -
```

Unicode line drawing has an ASCII fallback and an explicit override. Roots and siblings are sorted for reproducible output.

Node status and resource summaries are presentation-layer enrichment:

- Node status reports Ready and cordon state, not whether a particular workload can schedule.
- Traditional extended resources are displayed as Allocatable/Capacity, not free/total.
- DRA resources are displayed separately as advertised device inventory by driver, not as inferred Allocatable/Capacity.
- HyperNode summaries deduplicate member Nodes or device identities as appropriate.

For this proposal, the CLI computes these summaries separately from the Topology Resolver and the structural API snapshot. It reads the relevant Node and, when enabled, ResourceSlice data separately. Resource enrichment must not require Pod or ResourceClaim queries.

The client-only phase requires no new CRD, controller, or scheduler deployment change.

#### 6.1.2 Public Helpers

Provide a documented helper package in `volcano.sh/apis` for downstream consumers. The proposed package location is `volcano.sh/apis/pkg/helpers/topology`; its final exported names are subject to API review.

Changes are developed under `staging/src/volcano.sh/apis` in the main Volcano repository and published through the existing synchronization process. They are not submitted directly to the read-only `volcano-sh/apis` mirror.

The helper contract has two parts:

| Capability | Expected behavior |
| --- | --- |
| Build a topology view | Accept HyperNode and Node inputs and return a deterministic forest with structured diagnostics |
| Query a topology snapshot | Enumerate roots and children; retrieve direct and subtree Node members; query ancestors and containing domains |

The topology-building helper, referred to as the Topology Resolver in the technical design, accepts caller-supplied objects. It does not own API clients, informers, background goroutines, terminal output, or process configuration. Downstream applications can obtain inputs through the generated clients or existing informer/lister infrastructure. Documentation must show both a one-shot query and cache-backed integration.

Results must preserve overlapping Node membership; a reverse lookup cannot silently reduce a Node to one parent domain. Returned data must have documented ownership so one consumer cannot mutate another consumer's snapshot.

The dependency direction is essential:

```text
vcctl / scheduler / HyperNodeTopology Controller / adopters
                         |
                         v
           volcano.sh/apis topology helpers
                         |
                         v
           Volcano API types + Kubernetes libraries
```

The APIs module must not import `volcano.sh/volcano/pkg/scheduler` or another main-module implementation package. Extract the topology-only logic from the existing resolver and make Volcano's internal adapter delegate to it. Keep scheduler cache locks, queues, readiness transitions, incremental event handling, and workload state internal.

This extraction is not Phase 3: the scheduler still reads raw HyperNodes and builds its view locally, but uses the shared implementation. Existing incremental behavior must be validated against the shared snapshot contract.

Compatibility tests must compare the CLI adapter, scheduler adapter, and public helper on the same fixtures, including selector changes, overlapping members, deletions, missing references, cycles, and multiple parents. Existing edge cases, including empty selectors, must be characterized before extraction; any intentional semantic change needs explicit review rather than an incidental helper rewrite.

### 6.2 Phase 2: HyperNodeTopology API and Controller

Publish a controller-maintained topology view, then make the CLI its first reader. Scheduler adoption remains in Phase 3.

#### 6.2.1 API and Controller

Introduce the cluster-scoped `HyperNodeTopology` custom resource and the HyperNodeTopology Controller. The controller uses the shared helpers to maintain the topology view as HyperNodes and relevant Node inputs change.

This phase includes API schema review, generated clients/informers/listers, controller runtime integration, RBAC, lifecycle handling, and topology publication. Section 7 describes the one-complete-tree-per-object contract and the working representation. Final fields, source observations, and the publication mechanism require detailed review before implementation.

#### 6.2.2 CLI Integration

Change `vcctl hypernode tree` to query the `HyperNodeTopology` API by default rather than build topology from HyperNode declarations. Keep the command name: `tree` describes the supported presentation, while `HyperNodeTopology` is the API Kind.

Extend the public Topology Helpers to validate and query published snapshots. Preserve Node and resource enrichment as separate reads, and provide an explicit topology-source choice during migration.

### 6.3 Phase 3: Scheduler Adoption

Change the scheduler to read the published topology rather than build it from source declarations. The scheduler still builds local indexes and manages resource state, workload placement, and policy.

API-backed mode starts behind an explicit configuration gate. Its prerequisites include semantic parity, complete snapshot acceptance, freshness handling, Session-boundary adoption, scale validation, and a documented rollback path. This phase changes the structural topology source, not the scheduling algorithm.

### 6.4 Phase Completion Criteria

Work items can be reviewed and used individually. The following criteria must be met before advancing to the next phase; completion does not automatically enable a new topology source in existing installations.

| Phase | Work item | Completion criteria |
| --- | --- | --- |
| Phase 1 | CLI inspection | Read-only command shows the actual forest and Node leaves, reuses the scheduler's topology-building logic, and diagnoses invalid snapshots; no new cluster component is required |
| Phase 1 | Public helpers | Public helper is available through `volcano.sh/apis`; no reverse scheduler dependency; CLI and scheduler adapters pass shared semantic fixtures; downstream integration examples are documented |
| Phase 2 | API and controller | API review resolves schema, provenance, publication, lifecycle, freshness signaling, and size limits; controller reconciles HyperNode and Node changes with complete, observable publications |
| Phase 2 | CLI integration | CLI reads published snapshots, preserves rendering and membership semantics, enriches current status separately, and offers explicit migration behavior without hidden fallback |
| Phase 3 | Scheduler adoption | Scheduler uses compatible published topology with parity, readiness/freshness gates, Session-boundary adoption, scale validation, and documented rollout/rollback |

## 7. Design Details

### 7.1 Topology Semantics

For the same valid HyperNode and Node inputs, all phases must preserve the existing interpretation:

- A HyperNode with no parent is a root. Each independent root produces an independent tree; no cluster-wide synthetic root is added to the public representation.
- Parent/child relationships come from HyperNode membership declarations, not naming conventions or tier ordering.
- HyperNode members use the selector forms supported by the existing scheduler. Node members support the existing exact, regex, and label selection semantics.
- Direct Node membership and subtree Node membership are different concepts. Direct members determine displayed edges; subtree membership is the deduplicated union of direct members and descendants' members.
- A Kubernetes Node may be selected in more than one domain, including domains in different trees. This does not merge otherwise independent HyperNode trees. Node leaves are references and may occur on more than one path.
- Empty selector matches do not create fictional Nodes. Empty domains remain visible.
- Cycles, multiple HyperNode parents, invalid selectors, and unresolved exact references must be diagnosed rather than arbitrarily repaired.
- Input-list ordering must not change the result. Sorting for presentation does not change topology semantics.

The existing scheduler tolerates some transient informer states and unresolved references. A one-shot CLI may reject an incomplete snapshot rather than display it. Sharing topology-building logic does not, by itself, make all error handling identical. The public helpers in Section 6.1.2 must document and test the shared validity and diagnostic contract without silently changing scheduler behavior.

### 7.2 Consistency Guarantees

This proposal distinguishes three guarantees:

| Guarantee | Meaning |
| --- | --- |
| Algorithm and semantic consistency | The same valid HyperNode and Node inputs, under the same topology-building rules, produce equivalent relationships and membership |
| Published-snapshot consistency | Components accepting the same published topology revision interpret the same structure |
| Observation freshness | How far an accepted revision has caught up with changes to source resources |

Phase 1 targets the first guarantee. It cannot promise equality with the scheduler's live memory: the scheduler and CLI read at different times, and their inputs are not an atomic cross-resource snapshot.

Phase 2 introduces published snapshots with explicit revisions, readiness, and source observations. These remain eventually consistent with HyperNode and Node changes. Phase 3 must preserve that distinction rather than describe the API as a synchronous mirror of scheduler state.

Structural validity and freshness are independent. An old snapshot may remain internally valid after a Node moves to another domain. Current Node readiness and resource checks do not detect that change in topology membership.

Snapshot age is not a measure of synchronization lag: unchanged topology can remain current without a new content revision. The API review must distinguish published content, the controller's progress observing inputs, and controller or watch availability. A live controller or a recent heartbeat alone does not prove that source watches have caught up. Observation or liveness updates must not change the topology content revision or force consumers to rebuild indexes.

These limits define the failure and recovery requirements in Section 7.6 and the scheduler acceptance rules in Section 7.8.2. The concrete freshness signals and thresholds remain design decisions that must be resolved before API-backed scheduling is enabled.

### 7.3 `HyperNodeTopology` Resource Model

`HyperNodeTopology` is a derived, cluster-scoped resource in the topology API group. Each object stores one complete rooted tree. Its domain records include source identities, tiers, child relationships, and resolved direct Node membership. Neither another topology object nor source HyperNode or Node reads are needed to fill gaps in that tree. This does not require copying complete source objects, their selectors, or their operational status.

The working representation is **Alternative B**: a flat domain list with child-name references and direct Node identities. This retains ordinary structural-schema fields without a fixed hierarchy depth. Public helpers construct tree-shaped runtime views and query indexes. Other representations and their tradeoffs remain available for community review in [Appendix A](#9-appendix-a-api-representation-alternatives).

The controller publishes topology data in `spec`; `status` records conditions and synchronization diagnostics. Users continue to edit HyperNodes, not the generated topology. A controller-published data resource need not treat `spec` as a second user-authored topology declaration; Kubernetes ResourceSlice provides a related example of a producer publishing data through `spec`.

| Area | Working placement and meaning |
| --- | --- |
| Root | `spec.root` identifies the root domain within the same object's domain list |
| Publication identity | `spec.revision` identifies effective topology content; `spec.resolverVersion` identifies the interpretation contract |
| Domain records | `spec.domains` contains domain name/UID, tier information, child names, and direct Node name/UID pairs |
| Conditions and synchronization diagnostics | `status` describes the generation examined and its validity or publication state |
| Source observations | Status records the source-observation evidence needed for diagnostics and freshness; its final fields require API review |

#### 7.3.1 Working Go Types

The following types describe the core fields, not final generated API code. Source-observation and forest-publication metadata still require review under Sections 7.2 and 7.5. Health summaries and statistics are not part of the initial status API.

```go
import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type HyperNodeTopology struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HyperNodeTopologySpec   `json:"spec"`
	Status HyperNodeTopologyStatus `json:"status,omitempty"`
}

type HyperNodeTopologySpec struct {
	Revision        string `json:"revision"`
	ResolverVersion string `json:"resolverVersion"`

	// Root references a domain within Domains.
	Root string `json:"root"`

	// +listType=map
	// +listMapKey=name
	Domains []HyperNodeTopologyDomain `json:"domains"`
}

type HyperNodeTopologyDomain struct {
	Name string    `json:"name"`
	UID  types.UID `json:"uid"`

	Tier     int    `json:"tier"`
	TierName string `json:"tierName,omitempty"`

	// Children names reference domains within the same snapshot.
	// +listType=set
	Children []string `json:"children,omitempty"`

	// Nodes contains direct members, excluding descendants' members.
	// +listType=map
	// +listMapKey=name
	Nodes []HyperNodeTopologyNodeReference `json:"nodes,omitempty"`
}

type HyperNodeTopologyNodeReference struct {
	Name string    `json:"name"`
	UID  types.UID `json:"uid"`
}

type HyperNodeTopologyStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

`Tier` retains the existing HyperNode field type. Child names refer to domain records whose UIDs are already present in the same object, avoiding repeated reference wrappers. Node references carry UIDs so a same-name replacement cannot inherit membership.

#### 7.3.2 Example

This example shows branches with different depths. All children and Node members are resolved records, not selectors. It illustrates a controller-published object, not an apply-ready user declaration or a finalized publication protocol.

```yaml
apiVersion: topology.volcano.sh/v1alpha1
kind: HyperNodeTopology
metadata:
  name: zone-a
  generation: 1
spec:
  revision: opaque-topology-revision
  resolverVersion: v1
  root: zone-a
  domains:
  - name: zone-a
    uid: root-hypernode-uid
    tier: 3
    tierName: zone
    children: [rack-a, rack-c]
  - name: rack-a
    uid: rack-a-hypernode-uid
    tier: 2
    tierName: rack
    nodes:
    - name: node-01
      uid: node-01-uid
    - name: node-02
      uid: node-02-uid
  - name: rack-c
    uid: rack-c-hypernode-uid
    tier: 2
    tierName: rack
    children: [tor-c]
  - name: tor-c
    uid: tor-c-hypernode-uid
    tier: 1
    tierName: tor
    nodes:
    - name: node-05
      uid: node-05-uid
    - name: node-06
      uid: node-06-uid
status:
  observedGeneration: 1
  conditions:
  - type: Ready
    status: "True"
    reason: Published
    message: Complete topology published for this generation
    observedGeneration: 1
    lastTransitionTime: "2026-10-24T00:00:00Z"
```

#### 7.3.3 Validation, Ordering, and Status

Domain names must be unique within the object. `spec.root` must identify the only parentless domain; every other domain has exactly one parent and is reachable from it. Child references must resolve within the object, and cycles or disconnected records are invalid. An empty domain is valid; an object without its root domain is not. Nodes are unique by name within each domain and may occur in multiple domains, but the same name must resolve to the same UID within a snapshot.

The publisher uses a canonical order: domains by numeric tier descending, then name ascending; children and direct Node members by name ascending. This improves raw readability and reproducible comparison without adding tier buckets. Ordering is not topology semantics or a source of edges, and readers must resolve relationships correctly regardless of array order. Normalization occurs before effective-content comparison or revision calculation; status and API metadata are excluded, as is the revision field itself. Sorting is publication preparation, not a reason to rebuild or write on every unrelated event.

Object names are not identities. Deleting and recreating a HyperNode or Node under the same name must not make an old reference appear current. The final naming and lifecycle rules must account for source UIDs, root replacement, merges, and splits.

Spec and status updates are separate operations. A new spec may temporarily coexist with an earlier `Ready=True` condition. A reader must check that `status.observedGeneration` and the readiness condition's observed generation match `metadata.generation`, in addition to validating content, publication completeness, and freshness.

`status.observedGeneration` observes the `HyperNodeTopology` object's own spec. It does not establish that the controller has observed every HyperNode spec update or Node label change. Source observations and topology content identity therefore require separate fields. Resource versions are opaque and must not be numerically compared across resources.

Readiness describes the controller's observed inputs, not a linearizable guarantee that no source has changed since publication. Consumers must not interpret a recent timestamp or `Ready=True` alone as proof of real-time freshness.

### 7.4 HyperNodeTopology Controller Responsibilities

The HyperNodeTopology Controller:

1. Waits for its HyperNode and Node caches to synchronize.
2. Watches HyperNode creation, relevant updates, and deletion.
3. Watches Node creation, deletion, identity replacement, and label updates that can change selector results.
4. Builds a topology view from the observed inputs through the public helper in Section 6.1.2.
5. Validates the complete affected topology before publishing a replacement snapshot.
6. Reconciles tree creation, updates, retirement, and error conditions without modifying source HyperNodes.

Node Ready, cordon, resource quantities, and ResourceSlice updates do not change structural topology and must not cause topology publications solely for display enrichment. Conversely, listening only to HyperNode changes would miss Node selector changes and is insufficient.

The controller should coalesce related events, normalize content before comparison, avoid no-op spec and status writes, and retry conflicts using normal controller workqueue behavior. A topology revision changes only when effective content or its interpretation contract changes; observing unrelated Node status changes must not invalidate consumer indexes.

Implement reconciliation once and wire it into the supported controller runtimes. The existing integrated and optional standalone HyperNode deployment models should reuse that implementation. This proposal does not require a separate operator product or a second deployment-mode design.

A single elected writer owns the materialized tree set within the installation's scope. Reconcile status and source errors must be observable through conditions, Events where appropriate, logs, and metrics. API write failures must not be reported as successful publication.

### 7.5 Topology Publication and Cross-object Consistency

Each `HyperNodeTopology` object represents one complete rooted tree, as required by Section 7.3. A forest spans multiple objects. This section describes how complete per-tree records and a compatible forest publication are accepted; it does not allow splitting a tree across objects.

Writing `spec` replaces the tree content atomically within one object, but does not atomically replace its status. Until status acknowledges the new generation, readers must not accept an old readiness condition for the new content. Matching generations is necessary, but does not by itself establish freshness or coherence across objects.

For example, attaching one root below another changes two trees into one. A consumer must not accidentally use the new merged tree alongside the old independent tree, or accept half of a split as a complete forest.

Phase 2 must define how consumers identify and accept a complete, compatible set of per-tree snapshots. The mechanism must provide:

- an unambiguous set of roots and tree snapshots for a committed forest;
- a way to detect missing, superseded, or incompatible snapshots;
- publication ordering and recovery across controller restarts; and
- safe retirement of old tree objects after their replacement is committed.

Per-tree revision strings alone, or listing whichever objects currently have `Ready=True`, are insufficient. The concrete publication mechanism is an API-review decision, not an implementation detail to defer until the scheduler has switched over.

The publication protocol must distinguish a complete valid forest, an explicitly empty forest, a failed attempt, and an earlier retained publication. Empty domains and root-only trees are valid. An empty forest contains no accepted tree snapshots; the protocol must explicitly identify that empty set even if older objects remain for diagnostics. It must not be represented by an object without a root. Failure handling and recovery are described in Section 7.6.

Complete publication does not strengthen the source-observation guarantees in Section 7.2. The controller publishes a validated view of its observed inputs and converges as events arrive.

### 7.6 Failure Handling and Recovery

The following requirements apply to the controller and API readers. They build on the consistency limits in Section 7.2 and the complete-publication rules in Section 7.5. Scheduler-specific acceptance, rollout, and rollback rules are in Section 7.8.2.

| Failure scenario | Required behavior and recovery |
| --- | --- |
| HyperNode and Node changes arrive at different times | Diagnose missing references or invalid relationships, coalesce related events, and reconcile again as inputs change. Do not mark a partial result valid or assume retries make invalid input valid. Distinguish a pending observation from a successful empty result. |
| A spec write succeeds but its status update fails or is delayed | Readers reject readiness from an earlier generation. Retry status against the current spec and publication state; never stamp an obsolete computation as ready for a newer generation. |
| Publication stops partway through a merge, split, or replacement | Readers accept only a complete compatible publication. After restart, the controller must inspect existing publication state before completing or superseding the interrupted attempt. Retire old objects only after the replacement becomes authoritative under the publication protocol; this does not require acknowledgement from every reader. |
| Leadership changes, API writes conflict, or an older computation finishes late | Stop writes after leadership loss and prevent former writers or superseded computations from replacing a newer publication. Re-read publication state and re-evaluate inputs when retrying conflicts. Leader election alone is not a publication-ordering protocol; writer fencing and ordering require API review. |
| Source or topology Watch disconnects and needs to resume or relist | Use the standard informer recovery path and track whether observations are sufficiently current for the reader's policy. An available local cache is not proof of current data. Revalidate the publication after recovery; do not treat disconnection as topology deletion. |
| API is unavailable, authorization fails, or the CRD is absent | Report the failure explicitly. The CLI must not print an empty topology as success or silently switch sources. The controller retries transient failures with backoff and exposes persistent failures; it must not report a failed write as published. |
| A HyperNode or Node is deleted and recreated under the same name | Compare source UIDs, reconcile replacement, and reject stale identity bindings. An earlier valid snapshot must not authorize use of a replacement Node under its predecessor's membership. |
| A previous publication is valid, but newer inputs are invalid or have not been observed | Keep the previous result distinct from the latest reconciliation outcome. Retention for diagnostics is not permission to use it for new placements; acceptance requires the explicit validity and freshness policy. |
| A topology exceeds size limits or changes faster than publication can keep up | Report size or lag diagnostics, bound queued work and retry rates, and coalesce superseded work without losing the need to reconcile current inputs. Never truncate a snapshot or call an incomplete result ready. |

Failures must be visible through conditions and diagnostics when API writes are possible, and through logs and metrics when the API itself is unavailable. An old `Ready=True` condition cannot be used as evidence that a failed reconciliation or publication succeeded.

The failure policy must also define its scope. A problem in one domain does not automatically justify stopping unrelated scheduling, but independent roots alone do not prove isolation: Node membership may overlap. Limit the impact to a domain or tree only when validation establishes that the retained publication and the proposed placement do not depend on the failed part. Otherwise, block new topology-dependent placements within the scope whose safety cannot be established. Already-running workloads are not evicted solely because topology publication fails.

After recovery, the controller must reconcile current inputs, and readers must validate identity, compatibility, completeness, and freshness before accepting the result. Exact diagnostic fields, transient-input handling, writer fencing, lag limits, and isolation rules are Phase 2 design decisions, with scheduler acceptance verified in Phase 3. This proposal defines their required behavior rather than prescribing an unreviewed publication protocol.

### 7.7 CLI Migration and Enrichment

Keep the command and forest rendering stable while changing the default topology source to published `HyperNodeTopology` snapshots:

- Fetch and validate the published topology rather than rerunning source selectors.
- Reuse the public query helper to derive traversal and aggregate membership from the published structure.
- Read current Nodes and optional ResourceSlices separately for status and resource enrichment.
- Show the topology source, accepted revision, and readiness or lag diagnostics so users can distinguish a published view from one built by the CLI.
- Preserve all published topology leaves; a Node reference no longer present in the current Node read is shown as unresolved/missing rather than silently removed or enriched from a same-name replacement.

Provide an explicit source choice during migration, for example `--source=api|client`, with the final flag reviewed in Phase 2. `client` uses the shared resolver; `api` requires a valid supported publication.

Do not silently fall back to client-side topology construction when the new API is missing, unauthorized, or unready. A fallback that changes the topology source without telling the user would hide deployment and publication failures.

Phase 1 installations continue to work without the new CRD or controller. Changing the default source is a documented migration step with the corresponding RBAC and component prerequisites, not an incidental CLI upgrade.

### 7.8 Using `HyperNodeTopology` in the Scheduler

#### 7.8.1 Topology Source and Scheduler Responsibilities

Replace the scheduler's topology-building path with a List/Watch reader of published `HyperNodeTopology` snapshots. The scheduler no longer evaluates HyperNode member selectors to construct its structural topology.

The scheduler may still derive local traversal, ancestry, tier, and membership indexes from the accepted snapshot. These indexes support queries over a published topology rather than rebuilding it from source declarations.

Keep these responsibilities in the scheduler:

- current Node readiness, resources, predicates, and workload-specific eligibility;
- task occupancy, allocation, pipelining, reservations, and rollback;
- topology constraints, candidate selection, scoring, preemption, and reclaim; and
- immutable per-Session views and consumer-local indexes.

The scheduler continues to watch Nodes for its existing scheduling responsibilities. Using `HyperNodeTopology` does not remove those watches or authorize scheduling onto a missing or replaced Node.

#### 7.8.2 Rollout and Failure Handling

Introduce an explicit, gated topology-source configuration during rollout. Initially, building topology in the scheduler remains the supported default; API-backed mode is opt-in until parity, scale, and operational validation are complete. A scheduler instance uses one structural topology source at a time and must not combine relationships from both.

In API-backed mode, the scheduler must:

- wait for required informer caches and a complete, valid published topology before topology-dependent scheduling;
- accept readiness only when status and condition observations match the current spec generation;
- reject incomplete, invalid, or unsupported publications rather than weakening topology constraints;
- adopt a new accepted revision at a defined Session boundary rather than mutating a topology during a scheduling cycle;
- invalidate topology-derived indexes and placement summaries when the effective topology revision changes; and
- distinguish an explicitly empty topology from an unavailable topology source.

When the source reports invalid or unready topology, fail closed for new topology-dependent placements in the affected scope, following the isolation requirements in Section 7.6. Unrelated scheduling need not stop, and already-running workloads are not evicted solely because publication is unavailable.

The freshness policy must follow the distinctions in Section 7.2 rather than expire unchanged content based only on snapshot age. The maximum accepted lag, detection mechanism, treatment of retained snapshots, and restart behavior must be specified and tested before API-backed scheduling is enabled; a retained `Ready=True` object alone cannot answer these questions. Watch and controller recovery follow Section 7.6.

Rollback selects the legacy topology source through documented configuration and synchronization steps. It must not happen as a silent per-request fallback. Deprecating the legacy path is a later compatibility decision after the API-backed path is proven.

#### 7.8.3 Relationship to Scheduling Optimization

This phase changes where the scheduler obtains topology. Reading it through an API does not, by itself, improve placement quality, scheduling throughput, or resource utilization.

Future HyperNode scheduling improvements should use the public topology API. Changes to scheduling algorithms, data structures chosen for scheduling performance, incremental placement evaluation, or topology-aware preemption should be proposed and tracked in a separate, focused proposal/enhancement.

Phase 3 can preserve existing scheduling behavior while that longer-term work proceeds independently.

### 7.9 Compatibility and Source Ownership

Existing HyperNode objects, discovery configuration, and workload topology fields remain valid. Later phases add derived resources and consumer configuration; they do not require users to rewrite topology declarations.

The public helper and materialized API need explicit compatibility contracts. API version, resolver semantics, and effective topology revision are different concepts. A consumer must reject unsupported semantics rather than assume a readable JSON object is safe to use.

Before extraction, preserve scheduler behavior with parity fixtures. Before CLI migration, validate rendering and diagnostics against both sources. Before scheduler migration, compare topology and scheduling outcomes using equivalent accepted snapshots.

### 7.10 Resource Access

| Component | Structural topology access | Other access |
| --- | --- | --- |
| Phase 1 CLI | List HyperNodes and Nodes | Optional ResourceSlice List for DRA inventory |
| Public topology-building helper | No owned API access; inputs supplied by caller | None |
| HyperNodeTopology Controller | List/Watch HyperNodes and Nodes; manage HyperNodeTopology resources, status, and any publication metadata required by the final design | Leader-election and observability resources |
| API-backed CLI | Read published HyperNodeTopology snapshots | Read Nodes and optional ResourceSlices for enrichment; raw HyperNodes only in explicit client mode |
| API-backed scheduler | List/Watch published HyperNodeTopology snapshots | Existing Node/workload access remains; raw HyperNode access only if otherwise required |

Derived topology spec and status writes belong to the controller ServiceAccount, not ordinary readers. Moving data into spec does not grant users permission to maintain a second topology source. Authorization must cover the main resource, status subresource, and publication mechanism. Node references and topology relationships are cluster-wide information; granting read access must be deliberate.

### 7.11 Topology State, Health, and Resource Summaries

Structural validity, infrastructure health, and resource allocation are separate observations. A topology view can be valid while one Node is unhealthy or all devices are allocated. Conditions must not collapse these facts into one undefined health or readiness value.

The current HyperNode status contains conditions and a Node count; it does not define a dedicated `status.health` field or a general domain-wide health federation contract.

| Information | Meaning and proposed treatment |
| --- | --- |
| Domain relationships and direct Node membership | Core topology view in spec, maintained from HyperNode and Node inputs |
| Resolution conditions, diagnostics, and source observations | Status explains whether the controller produced a valid snapshot and which inputs it observed |
| Node readiness, cordon state, device or fabric health | Operational observations with separate authorities and update rates; do not embed their streams in the structural snapshot |
| Node or accelerator counts | Useful summaries with explicit units and deduplication rules; derive in consumers unless a later API requirement justifies storing them |
| Pod requests, allocated devices, and remaining resources | Allocation accounting outside this proposal |

The initial status API contains conditions and synchronization diagnostics, not health summaries or statistics. Domain-level health federation remains a possible extension. It requires definitions for source authority, unknown and stale observations, aggregation rules, and how consumers use the result. It is not established merely by adding a status field or counting unhealthy Nodes.

The initial CLI can combine structural topology with current Node status and device inventory without requiring the HyperNodeTopology Controller to republish every operational change. The output must distinguish the topology snapshot from enrichment read at another time.

### 7.12 Performance and Scalability

A controller-maintained topology view can reduce duplicated topology-building work across components, but adds reconciliation, API storage, spec/status writes, and watch traffic. A small source change may rewrite a large snapshot and distribute it to every watcher.

For topology changes, distinguish affected-domain discovery, incremental relationship and membership updates, effective-content comparison, and object publication. The controller may retain parent, child, reverse-member, and subtree caches regardless of which fields are persisted. Publishing more indexes does not automatically provide an incremental algorithm. Representation-specific comparisons and Node or HyperNode update scenarios are in Appendix A; this section addresses system-wide costs.

#### 7.12.1 Cost Model

Topology size depends on more than the number of HyperNodes. Relevant dimensions include Node count, domain relationships, selector count and type, direct membership references, membership overlap, and hierarchy depth. Regex or label matching can require examining many Nodes, and subtree membership or reverse indexes can repeat information across domains. A linear pass over domains and Nodes must not be assumed to describe the complete construction cost.

| Cost | Main factors |
| --- | --- |
| Construction and validation | Input size, selector matching, overlap, hierarchy depth, and whether changes trigger full or incremental work |
| Storage and serialization | Direct membership references, object metadata and provenance, complete-tree object size, retained revisions, and any publication metadata |
| Reconciliation and publication | Relevant event rate, coalescing, effective content changes, conflict retries, and number and size of objects rewritten per publication |
| Reading and indexing | Reader count, List/Watch traffic, relists, local index construction, and retention of current and previous snapshots |

For readers watching the full topology set, a rough model for topology-object distribution traffic is:

```text
distribution bytes/second ~ publications/second
                           * total object bytes distributed per publication
                           * watcher count
```

This is not a benchmark result or a model of all API-server and etcd costs. Relists, transport overhead, retries, observation-status updates, and publication metadata add traffic. It explains why a local source change can have a wider cost when it rewrites a large snapshot.

Source HyperNodes permit domain-level updates; this alone does not prove they use less total storage than another representation. Flat direct-membership records avoid persisting every ancestor's expanded Node set, but a large root can still exceed practical object-size or update-cost limits.

#### 7.12.2 Cost by Phase

| Phase | Expected work and tradeoff |
| --- | --- |
| Phase 1 | Each CLI invocation reads inputs and builds a view on demand. Public helpers remove duplicated implementations, not repeated execution by independent applications. |
| Phase 2 | The controller adds ongoing construction, storage, and publication. API-backed readers no longer construct topology from declarations, but the scheduler still does so locally until Phase 3; temporary duplication must be included in measurements. |
| Phase 3 | The scheduler replaces source construction with snapshot reading and local indexing. Node watches, scheduling state, and current resource checks remain necessary. |

The controller should reuse shared HyperNode and Node informers rather than issue full API Lists for every event. Filter non-structural updates, coalesce related changes, and avoid no-op publications. Coalescing must not indefinitely postpone publication during sustained changes. Freshness or liveness reporting may require separate updates, whose frequency and fanout must be included in the cost model without changing the topology content revision.

The CLI still reads Nodes and optional ResourceSlices for enrichment. Publishing topology does not eliminate those reads or the scheduler's Node watches. The API must not be updated for resource availability or Pod allocation changes.

#### 7.12.3 Evaluation Scenarios and Metrics

| Representative scenario | Measurements and design questions |
| --- | --- |
| Large topology with infrequent changes | Construction time, object size, initial List cost, and controller and reader memory |
| Broad selectors, overlapping members, or deep hierarchies | Matching and validation time, membership expansion, deduplication, and local index cost |
| Sustained Node label and topology changes | Queue age, reconciliation and publication rates, source-change-to-reader-acceptance latency, and write traffic after coalescing |
| One very large root versus many independent roots | Maximum object size, bytes rewritten for a local change, and cross-object publication overhead |
| Many readers, Watch reconnects, or slow readers | Watch fanout, relist load, reader catch-up time, and retained snapshot memory and storage |
| Frequent Node status changes without membership changes | No-op processing cost and confirmation that structural publications and content revisions do not change |

Record distributions and tail latency, not only averages. Separate construction time, publication delay, and reader catch-up time; use controlled source changes to measure end-to-end convergence. Track API write frequency and failures, serialized bytes, CPU, memory, and retry or backlog growth alongside those timings.

#### 7.12.4 Limits and Design Gates

Before finalizing the Phase 2 schema, representative measurements must establish supported topology and object sizes, acceptable convergence delay, and API write and distribution budgets. The initial proposal does not assign unmeasured numerical limits or promise a net performance improvement.

If a complete tree is too large, report that limit explicitly and evaluate a more compact representation within the single-object contract. The controller must not truncate the tree or silently shard it. Supporting trees beyond the validated object-size limits would require a separately reviewed change to that contract. Oversize and lag conditions must be explicit.

Phase 3 must additionally measure scheduling behavior during normal updates, reader lag, and controller failure under the acceptance policy in Section 7.8.2. Confirm semantic parity before comparing throughput or latency. The measurements determine whether the chosen layout and freshness policy are suitable for scheduler adoption.

### 7.13 Alternatives

| Alternative | Assessment |
| --- | --- |
| Keep a client-only command indefinitely | Solves inspection, but non-Go applications still reconstruct topology and there is no shared API view |
| Copy topology-building logic into each component | Easy initially, but creates independent selector, validation, and topology semantics; rejected |
| Expose scheduler internals as the public helper | Couples adopters to scheduler implementation and creates an invalid dependency direction for `volcano.sh/apis`; rejected |
| Add all derived relationships to each HyperNode status | Keeps one resource kind, but distributes the derived tree across source objects and still needs a coherent publication contract |
| One object containing the complete forest, or several objects for one tree | Outside this proposal's one-complete-tree-per-object contract; neither is a fallback for publication or size failures |
| One object per root without collection publication | Natural per-tree reads, but unsafe for coherent forest consumption during merges and splits; insufficient for scheduler adoption |
| Serve the scheduler's live cache through a debugging endpoint | Useful for a different debugging problem, but ties topology access to scheduler deployment and availability rather than a shared Kubernetes API |

The proposed direction is shared helpers first, followed by a controller-maintained `HyperNodeTopology` view. Alternative B is the working per-tree representation; Appendix A compares other encodings and optional persisted indexes. Its final schema and publication mechanism require Phase 2 consistency and scale validation.

### 7.14 Test Plan

Tests must cover multiple roots, mixed direct and nested membership, overlapping selectors, empty domains, Node label changes and deletion/recreation, root merge/split, invalid references, cycles, multiple parents, controller restart, write conflicts, partial publication, unsupported resolver versions, API/RBAC failures, and large topologies.

For the working API, check Go/YAML agreement, single-root connectivity, internal child references, consistent name/UID identities, and directly selected Nodes that also occur through descendants. Permuted input lists must normalize to identical content without a new content revision. Tier changes may alter canonical position, but readers must not infer relationships from ordering. Test successful spec replacement followed by delayed or failed status acknowledgement, including rejection of old-generation readiness.

Use the scenarios in Section 7.6 as recovery and failure-injection cases: delayed and reordered source observations, interrupted publication, leadership handover, late computations, Watch relist, and recovery after API unavailability. Verify both the result that may be accepted and the conditions under which acceptance must stop. Include a structurally valid but outdated snapshot and an unchanged snapshot that remains current, so freshness is not reduced to snapshot age.

For partial-failure isolation, test overlapping memberships as well as independent roots. If isolation cannot be demonstrated, verify that the scheduler does not use the affected scope for new topology-dependent placements. Check that already-running workloads are not evicted solely because topology publication fails.

Performance evaluation follows Section 7.12. Include sustained churn and reconnect bursts, not just steady-state construction benchmarks, and verify eventual convergence after inputs settle without unbounded work or retained publications.

For Phase 3, compare scheduling outcomes on fixed equivalent input snapshots before measuring performance. Switching to the topology API must not change scheduling policy within this proposal.

### 7.15 Open Questions

The staged direction does not depend on finalizing the new CRD now. Before Phase 2 implementation, community review must resolve:

1. Does Alternative B provide the best per-tree representation, or should one of the Appendix A encodings replace it while preserving the same completeness contract?
2. Which publication representation provides a coherent forest revision without excessive duplication or write amplification?
3. What source provenance, observation-progress, and liveness signals distinguish current unchanged content from stalled synchronization without unbounded metadata or status-write traffic?
4. What are the exact invalid-input and transient-reference contracts shared by the batch resolver and incremental scheduler adapter?
5. Which existing controller runtimes own reconciliation, and how are writer exclusivity, publication ordering, restart recovery, and installation upgrades enforced?
6. What compatibility window, freshness policy, and validated failure-isolation scope are required before API-backed scheduler consumption becomes the default?
7. What complete-tree size, convergence-delay, and API-load budgets can be supported without splitting a tree across objects?

These are implementation gates for later phases, not reasons to delay the client inspection command and public helper work.

## 8. Glossary

API Kind names use code formatting, such as `HyperNodeTopology`. Terms such as HyperNode tree describe topology structure without implying a resource of that name. Architecture roles such as Topology Resolver and Topology Helpers do not prescribe exported Go type or package names.

| Term | Definition |
| --- | --- |
| `HyperNode` | Existing API resource declaring one topology domain, its tier, and members or member selectors |
| Kubernetes Node | A Kubernetes compute-node resource; references to Node in this document mean this resource, not an arbitrary tree vertex |
| Topology Domain | A grouping of Nodes or child domains with particular communication locality or performance characteristics; represented by a HyperNode, not necessarily one physical switch |
| Topology Declarations | The tiers, membership references, and selectors declared through HyperNode resources |
| Discoverer | Existing extension component that adapts a topology discovery source into HyperNode information |
| HyperNode Controller | Existing controller responsible for coordinating discovery results and maintaining source HyperNode resources |
| Topology Construction (Topology Resolution) | Building a topology view by evaluating HyperNode references and selectors against HyperNode and Kubernetes Node inputs |
| Topology Resolver | The shared implementation of topology construction; an architecture role rather than a promised Go type |
| Topology View (Resolved Topology) | The domain relationships and actual Node membership produced by topology construction, independent of storage or transport |
| HyperNode Tree | A rooted hierarchy of HyperNode domains with Kubernetes Node membership references; a Node may appear on multiple paths |
| HyperNode Forest | A collection of independent HyperNode trees; sharing a Node member does not connect their roots, and each tree is stored in its own HyperNodeTopology object |
| Topology Snapshot | A topology view for a particular observation of inputs; it does not imply a transactionally consistent source read |
| Materialized Topology View | A topology view maintained by a controller and published through Kubernetes APIs |
| `HyperNodeTopology` | Proposed cluster-scoped API Kind storing one complete rooted tree in spec, with conditions and synchronization diagnostics in status |
| HyperNodeTopology Controller | Proposed controller that builds topology views from HyperNode and Node inputs and maintains HyperNodeTopology resources |
| Topology Helpers | Public capabilities for building and querying topology provided through `volcano.sh/apis`; exported names require API review |
| Direct Node Membership | Nodes selected by the current HyperNode's own Node member declarations |
| Subtree Node Membership | The deduplicated union of direct Node members of a HyperNode and its descendants |
| Topology Index | A consumer-local query structure derived from a topology snapshot, such as ancestry or reverse membership indexes |
| Topology Revision | An identity for effective published topology content; its scope and encoding require API review and differ from API version, source resourceVersion, and process-local cache generation |
| Observation Freshness | How far observed inputs and an accepted publication have caught up with source changes; neither snapshot age nor controller liveness alone establishes it |
| Resolution Status | Conditions and diagnostics about building a valid topology snapshot, distinct from infrastructure health and resource allocation |

## 9. Appendix A: API Representation Alternatives

### A.1 Common Contract and Fields

All candidates store one complete rooted tree in one object. They retain domain identity and tier semantics, actual direct Node identities, empty domains, uneven branch depths, and overlapping Node membership. No candidate may require source reads to fill missing topology or split a tree across objects. Copying full HyperNode and Node resources is not required.

Representation is separate from field ownership: topology content belongs in spec, with conditions and synchronization diagnostics in status. All candidates need the revision, interpretation contract, and generation-aware acceptance rules described in Section 7.3. Those common fields and the outer resource are omitted below.

The sketches reuse these domain fields and the Node reference from Section 7.3. They illustrate alternatives, not additional exported helper types or finalized API declarations; serialization tags and generation annotations are omitted.

```go
type DomainData struct {
	Name     string
	UID      types.UID
	TierName string
	Nodes    []HyperNodeTopologyNodeReference
}
```

`Nodes` always means direct membership. Derived subtree membership and query indexes are separate decisions.

### A.2 Candidate Representations

#### Alternative A: Flat Domains with Parent References

Spec contains `root` and a `domains` list. Each non-root record identifies its parent by name within the same object.

```go
type DomainA struct {
	DomainData
	Tier   int
	Parent string
}
```

This directly expresses the single-parent rule. A helper derives child lists and a name index once. Without those runtime indexes, downward traversal would require repeated searches.

#### Alternative B: Flat Domains with Child References

This is the working definition in Section 7.3: `root` plus `domains`, with `children` names rather than `parent`. It resembles the source API's child-membership expression without persisting source selectors. The helper derives parent and name indexes; validation must reject a child assigned to multiple parents.

Both A and B use canonical tier/name ordering for publication. This makes equal-tier records adjacent without introducing tier buckets. It does not encode edges, replace indexes, or require readers to depend on array positions.

#### Alternative C: Nested Domains

Spec contains an embedded root domain. Each child is another complete domain record, so the serialized structure directly displays hierarchy.

```go
type DomainC struct {
	DomainData
	Tier     int
	Children []DomainC
}

type TreeC struct {
	Root DomainC
}
```

This is a valid Go model, but not by itself a complete CRD-schema solution. CRD validation does not support recursive `$ref` or `definitions`. Two implementation choices need explicit review:

| Variant | Schema and operational tradeoff |
| --- | --- |
| C1: Expand the schema to a declared maximum depth | Retains field validation through that depth but introduces a hierarchy limit. Uneven branches are still possible; choosing a limit requires community agreement. |
| C2: Preserve unknown fields in recursive subtrees | Does not predefine business hierarchy depth, but deep fields lose native schema coverage and complete `kubectl explain` support. Helpers/controller must validate the full tree; enforcing that validation for all API writes requires admission validation such as a webhook. |

Both remain subject to object-size and processing limits. Preservation of unknown fields must be explicit to avoid pruning data. Nested representation does not require embedding complete source resources.

#### Alternative D: Basic Topology with Persisted Derived Sets

Use a self-contained basic topology, such as B, and add selected scheduler-style caches:

```go
type TreeD struct {
	Root                string
	Domains             []HyperNodeTopologyDomain
	HyperNodesSetByTier map[string][]string
	RealNodesSet        map[string][]string
}
```

`HyperNodesSetByTier` groups domain names by numeric tier. `RealNodesSet` maps each domain to the Node names in its entire subtree. Node names resolve to the identity records already present in direct membership within the same object.

The two sets alone are insufficient: tier groups do not encode edges, and subtree membership cannot reconstruct direct membership when a Node belongs both directly to a domain and through a child. Basic relationships and resolved direct members must remain.

Tier grouping adds one reference per domain and can be derived from domain metadata. Persisted subtree sets can avoid initial subtree-union computation for consumers relying on validated producer output, but repeat membership along ancestor paths and require coordinated updates with the base records. Recomputing every set to check agreement would reduce that initialization benefit. Either cache can also be added to A, C, E, or F; persisting derived results is an independent choice from the base encoding. Parent and children could both be persisted as another redundancy tradeoff, but would need agreement checks.

#### Alternative E: Tier-grouped Domains with Parent References

Replace the flat `domains` list with `tierDomainMap`, whose values are lists containing only domains of that tier. Each domain is stored once.

```go
type DomainE struct {
	DomainData
	Parent string
}

type TreeE struct {
	Root          string
	TierDomainMap map[string][]DomainE
}
```

The numeric tier is supplied by the bucket key instead of repeated in each domain. JSON object keys are strings, such as `"3"`; map order is not meaningful, so displays must parse and sort numeric keys. Numeric tier keys are preferable to optional tier names: name-based keys need rules for missing names, collisions, and renames. A helper still needs a global name index and child index.

#### Alternative F: Tier-grouped Domains with Child References

Use the same tier buckets as E, but store child names instead of parent names.

```go
type DomainF struct {
	DomainData
	Children []string
}

type TreeF struct {
	Root          string
	TierDomainMap map[string][]DomainF
}
```

E and F are not D's additional tier index: domains are partitioned into buckets, not duplicated into another domain table. Domain names must be unique across all buckets. A tier change moves a record between buckets, and tier grouping must not be mistaken for tree depth or ancestry.

### A.3 Representation Tradeoffs

Let `H` be the domain count, `M` the number of direct Node membership records including overlaps, and `d` the maximum domain depth. The orders below describe record counts, not measured serialized bytes.

| Candidate | Raw readability | Storage characteristics | Schema and maintenance |
| --- | --- | --- | --- |
| A | Parent links require following names; tier sorting groups peers | Basic `O(H + M)` records; one parent reference per non-root domain | Ordinary schema; derive child indexes and validate connectivity |
| B | Child links show the next level but require name lookup | Basic `O(H + M)` records; one reference per child edge | Ordinary schema; derive parent indexes and validate single parenthood |
| C | Directly shows paths and uneven branch depth | Basic `O(H + M)` records; avoids edge-name references but adds nested containers | C1 depth limit or C2 reduced schema coverage; subtree moves change serialized paths |
| D | Several complementary views, not a nested tree | Base records plus tier-group references and subtree sets; repeated membership can grow with `M * d` | Must keep derived fields consistent; more data to serialize and distribute |
| E | Explicit tier grouping, with parent links | Basic `O(H + M)` records; bucket overhead may be offset by omitting repeated tier fields | Ordinary schema plus bucket-key and global-uniqueness rules |
| F | Explicit tier grouping, with child links | Same broad tradeoff as E | E's bucket rules plus child-reference and single-parent checks |

For similarly populated A/B/E/F records, name lengths, omitted fields, and serialization choices determine actual byte differences. A repeats a parent's name for its children; B references each child's name, so fanout and name lengths affect the comparison. Pretty-printed YAML indentation is not a measure of stored size. D changes the amount of persisted information, not just its arrangement; C2 may change admission and validation costs.

API shape need not be the helper's runtime shape. Any candidate can be converted once into a tree with name, parent, child, and reverse-direct-membership indexes. Full traversal of a valid basic topology is normally `O(H + M)` with appropriate indexes. Persisting all ancestor paths can add up to `O(H * d)` records, while persisting subtree Node sets can add up to `O(M * d)`. These are optional query costs, not requirements for a complete API.

### A.4 Change Processing and Publication Cost

The main update question is the cost of refreshing topology after source changes, not only static lookup speed. Compare affected-domain discovery, relationship/member recomputation, effective-content comparison, and publication separately. Use equivalent runtime indexes when comparing encodings so an implementation difference is not mistaken for an API advantage.

| Change | Potential incremental work | Representation implications |
| --- | --- | --- |
| Node labels, creation, or deletion change direct membership | Identify old and new matching domains, update direct members, and propagate actual differences along affected ancestor paths | A/B/E/F can all use runtime indexes. Tier grouping alone does not identify relevant selectors; D's old sets do not identify newly matching selectors. |
| A domain's Node selector changes | Re-evaluate that selector against relevant Nodes, compare direct members, then update affected ancestors | Matching cost depends on selector type and available indexes, not only API layout. |
| A subtree moves within one rooted tree | Change the edge and update old/new ancestor paths; unchanged internal membership may be reused | A/E update a parent reference; B/F update child lists; C changes nesting paths. Runtime indexes can support all of them. |
| A domain's tier or tier name changes | Update the domain, validate semantics, and normalize publication order; avoid unrelated membership rebuilds | A/B update fields and possibly array position. E/F move records when the tier key changes. D updates its tier grouping too. |
| A subtree moves across roots, or roots merge/split | Reconcile all affected complete-tree objects and forest publication | No encoding removes the cross-object protocol required by Section 7.5. |
| Node status changes without topology changes | No new structural publication | Runtime enrichment remains separate for every encoding. |

For example, moving a Node between two racks under the same zone changes both racks' membership, while the zone's final union may remain unchanged. Incremental code can avoid recomputing unchanged results, but the tree object still changes. With overlapping membership, removing a Node from one child does not justify removing it from every ancestor: another child or a direct member may still contribute it. Correct propagation needs set recomputation or suitable runtime contribution counts.

The controller can retain subtree sets even when the API does not. Conversely, publishing D does not automatically implement incremental updates. The current scheduler selectively rebuilds affected ancestor state and performs selector matching and set unions; it is not a constant-time single-entry update algorithm. Compare cold reconstruction and warm-cache updates separately.

Canonical sorting has a cost: a full domain sort is generally `O(H log H)`, and incremental maintenance can move array elements. Normalize before comparison and publication, rather than sorting and writing for every unrelated event.

Once a complete-tree object changes, normal full-object encoding and distribution depend on its serialized size. PATCH can reduce upload bytes but does not guarantee leaf-level storage or Watch deltas. Reducing recomputation and reducing publication traffic are separate goals. Under comparable content and publication frequency, A/B/E/F differences may be smaller than tree size, event coalescing, or watcher fanout; that is a hypothesis to measure, not a performance claim.

### A.5 Evaluation and Open Decisions

Alternative B remains the main-text working design. A/B are simple flat baselines; E/F trade bucket rules for more visible tier grouping; C prioritizes direct tree readability with explicit schema tradeoffs; D requires evidence that persisted query results justify their size and update burden. No candidate has a measured performance advantage in this proposal.

Use the same trees, name/UID lengths, memberships, source events, and runtime indexing assumptions to compare compact encoded bytes, encode/decode cost, helper reconstruction, warm incremental updates, memory, and API validation. Include deep chains, wide trees, long names, and overlapping members.

For refresh tests, record Nodes examined, domains and sets recomputed, effective publications, bytes written/distributed, and source-change-to-reader-acceptance latency. Test single-Node moves, selector changes, tier changes, subtree relocation, sustained churn, and cold recovery. C1 needs a depth-limit decision; C2 needs schema/pruning and admission checks; D needs base/derived consistency checks; E/F need bucket migration and cross-bucket uniqueness checks.

Semantic completeness, single-object feasibility, and a supportable validation/versioning contract are prerequisites, not weighted preferences. Among candidates meeting those requirements, community review can weigh raw readability, storage, refresh cost, and maintenance. Public helpers reduce the importance of raw layout but do not eliminate the need for a clear API and a documented runtime result type.

## 10. References

- [Network Topology Aware Scheduling](./Network%20Topology%20Aware%20Scheduling.md)
- [HyperNode Auto Discovery](./hyperNode-auto-discovery.md)
- [HyperNode Controller Standalone Deployment](./hypernode-controller-standalone.md)
- [`vcctl` Command Line Enhancement](./command-line-enhancement.md)
- [Volcano APIs staging and contribution model](../../staging/src/volcano.sh/apis/README.md)
- [Current HyperNode API](../../staging/src/volcano.sh/apis/pkg/apis/topology/v1alpha1/hypernode_types.go)
- [Current scheduler topology implementation](../../pkg/scheduler/api/hyper_node_info.go)
- [Current shared member-selection helper](../../pkg/util/hypernode/member_selector.go)
- [Kubernetes API conventions: Spec and Status](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#spec-and-status)
- [Kubernetes ResourceSlice API](https://github.com/kubernetes/api/blob/master/resource/v1/types.go)
- [Kubernetes CRD pruning and unknown fields](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#controlling-pruning)
- [CRD schema validation restrictions](https://github.com/kubernetes/apiextensions-apiserver/blob/master/pkg/apis/apiextensions/validation/validation.go)
