# PodGroup Topology Affinity Design

Author: wangyang0616 · May 28, 2026

---

## 1. Overview

Volcano can already schedule workloads on a HyperNode tree through `networkTopology`. That API defines the topology boundary within which a PodGroup or subGroup should be consolidated—for example, Gang-scheduling a workload within one rack or keeping an entire inference instance within one supernode.

This design adds group-level topology affinity and anti-affinity to the PodGroup API ([volcano-sh/volcano#5347](https://github.com/volcano-sh/volcano/issues/5347)). The new fields describe relationships between groups on the same topology tree rather than the placement of individual Pods within a subGroup.

The API models two classes of policy:

- **Across PodGroups:** separate multiple workload instances at a selected topology tier, such as placing inference replicas in different supernodes for fault isolation.
- **Within one PodGroup:** colocate or separate subGroups, such as spreading Prefill and Decode shards across racks while keeping a complete inference instance in one supernode.

These policies compose with `networkTopology`. They do not replace Pod-level `podAffinity` or `podAntiAffinity` in Pod templates.

### Supported Scope

The scheduler supports required and preferred **PodGroup anti-affinity** through `allocate`, including namespace selectors and composition with `networkTopology`.

**SubGroup affinity and anti-affinity** are proposed extensions. Their API types, CRD fields, and scheduling behavior are not implemented. The corresponding API, semantics, examples, and validation rules below describe the proposed capability, not the current API contract.

BestEffort Pods placed through backfill do not receive group topology filtering or scoring; enabling backfill does not disable enforcement for Pods placed through `allocate`.

See the [PodGroup Anti-Affinity User Guide](../user-guide/how_to_use_podgroup_anti_affinity.md) for supported configurations and operational requirements. Installing the API alone does not enable enforcement.

## 2. Background and Motivation

[Network Topology Aware Scheduling](./Network%20Topology%20Aware%20Scheduling.md) allows a workload to be Gang-scheduled within a HyperNode domain. It answers the question: **where should this group be consolidated on the topology tree?**

Inference workloads also need to express **how multiple groups relate to one another**. These relationships exist at two levels:

- between different PodGroups
- between subGroups in the same PodGroup

Pod-level affinity is not sufficient for these cases because it evaluates individual Pods rather than Volcano scheduling groups and does not operate on HyperNode domains.

A common example is a model service with multiple inference instances, each represented by one PodGroup. Operators may require every instance to use a different supernode so that a single hardware failure cannot affect all replicas. Without a PodGroup-level policy, multiple instances may be placed in the same failure domain.

Another example is a Prefill–Decode workload. Shards of each role may need to spread across racks, while all Prefill and Decode subGroups belonging to the same instance must remain under one supernode. Without group topology affinity, users must split the workload into multiple PodGroups or maintain duplicate Pod-level rules that are not aligned with HyperNode Gang scheduling.

The `topologyAffinity` field introduced by this design provides a single declarative entry point for these relationships. It builds on the existing HyperNode model and remains separate from both `networkTopology` and Pod-level affinity.

## 3. Goals

1. **Cross-PodGroup fault-domain isolation:** allow users to select PodGroups by label and namespace selectors and place them in distinct domains at a specified HyperNode tier.
2. **Intra-PodGroup subGroup placement:** allow subGroups to be colocated or separated at a specified tier, covering shard spreading, role colocation, and cross-role isolation.
3. **Required constraints and preferred policies:** support mandatory placement rules as well as best-effort placement preferences.
4. **Composition with existing topology scheduling:** allow `topologyAffinity` and `networkTopology` to be used together to express group relationships and workload consolidation boundaries independently.
5. **Tier-based policy expression:** allow users to target fault domains such as racks and supernodes through `topologyTierName` or `topologyTier`, which refer to HyperNode `spec.tierName` and `spec.tier`, without enumerating Kubernetes Nodes.
6. **Stable, understandable scheduling outcomes:** keep a workload Pending when required rules cannot be satisfied and report whether the failure occurred at the HyperNode or Node dimension.

## 4. Non-Goals

| Item | Treatment |
| --- | --- |
| Topology consolidation or Gang scope | Continue to use `networkTopology` and `network-topology-aware`. |
| Pod-level affinity within one subGroup | Continue to use Pod template `podAffinity` and `podAntiAffinity`. |
| Cross-PodGroup or cross-namespace `subGroupAffinity` / `subGroupAntiAffinity` | Not supported. Peer SubJobs always belong to the same PodGroup UID. |
| Cross-PodGroup `podGroupAffinity` | Not supported. Within one PodGroup, `networkTopology` provides a consolidation boundary; the proposed `subGroupAffinity` would support colocation of selected subGroups. |
| Group topology constraints for Pods placed through backfill | Not supported. Backfill handles BestEffort tasks without group topology filtering or scoring; this limitation is not defined by whether a member exceeds `minMember`. |
| Topology-driven preemption | The scheduler does not evict a matching PodGroup solely to clear a conflicting domain. Gang actions may reclaim resources only inside an already legal domain. |
| A new `TopologyUnsatisfiable` PodGroup condition | Reuse existing Events, Pod conditions, and fit summaries. |
| Batch Job or `PartitionPolicy` alignment | Not covered by this design. |

## 5. Proposal

### 5.1 Capability Model

Users may express placement intent at different tiers of the same HyperNode tree:

- **`networkTopology`** defines a consolidation boundary or Gang scope. It specifies where a PodGroup or subGroup may be placed, such as keeping all shards within one rack or an entire instance within one supernode.
- **`topologyAffinity.subGroupAffinity`** and **`subGroupAntiAffinity`** define the proposed relationships between SubJobs generated by `subGroupPolicy` entries in the same PodGroup.
- **`topologyAffinity.podGroupAntiAffinity`** separates the current PodGroup from other PodGroups selected by `podGroupSelector` and `namespaceSelector`.

The `network-topology-aware` and `group-topology-affinity` plugins participate in the same HyperNode scheduling path. For allocation, their gradients express topology constraints, while minimum-resource filtering runs after the Framework intersects the constrained results from enabled plugins. The allocate action then performs placement dry-runs, evaluates Node predicates, and binds Pods. For eviction planning, `network-topology-aware` also filters candidates by total allocatable capacity before intersection.

```mermaid
flowchart LR
    NT["networkTopology"] --> NTA["network-topology-aware"]
    TA["topologyAffinity.podGroupAntiAffinity"] --> GTA["group-topology-affinity"]
    NTA --> FW["Framework: intersect HyperNode candidates"]
    GTA --> FW
    FW --> RF["FilterGradientsByMinResource"]
    RF --> DR["Dry-run and Node predicates"]
    DR --> B["Bind"]
```

Gradient callbacks return one of three explicit results:

- `Unconstrained: true` permits the input subtree without enumerating candidates
- `Unconstrained: false` with non-empty `Gradients` supplies constrained candidates
- a zero-value result or empty `Gradients` with `Unconstrained: false` rejects the search, including fail-closed behavior after an internal error

Unconstrained plugins do not participate in intersection or exclusion statistics. When all participating plugins are unconstrained, the Framework generates the full input subtree once for preferred scoring. When no enabled gradient callbacks are registered, it preserves the input-root fallback.

### 5.2 PodGroup API

`PodGroupSpec` gains an optional `topologyAffinity` field. Existing `networkTopology`, `subGroupPolicy`, and Pod-level affinity fields remain unchanged.

The current `topologyAffinity` API contains only `podGroupAntiAffinity`. The design also proposes `subGroupAffinity` and `subGroupAntiAffinity` as future extensions. Each policy has `required` and `preferred` lists; each preferred term must set `weight` to a value from 1 to 100.

- **`podGroupAntiAffinity`:** separates the current PodGroup from other selected PodGroups. A `PodGroupAffinityTerm` contains a mandatory `podGroupSelector`, an optional `namespaceSelector`, and exactly one of `topologyTierName` or `topologyTier`. The current PodGroup is always excluded by UID.
- **`subGroupAffinity` (proposed):** colocates SubJobs generated by the listed `subGroupPolicy` entries in the current PodGroup.
- **`subGroupAntiAffinity` (proposed):** spreads or isolates SubJobs generated from the listed `subGroupPolicy` entries in the current PodGroup.

The proposed `subGroups` field contains `subGroupPolicy[].name` values, not runtime SubJob identifiers.

#### Proposed SubGroup semantics

| Concept | Meaning | API location |
| --- | --- | --- |
| Policy name | A `subGroupPolicy[].name`, such as `prefill` or `decode` | `SubGroupAffinityTerm.subGroups` |
| SubJob | A schedulable unit generated by a policy, for example a group of Pods that share the same tuple of `matchLabelKeys` values | Runtime scheduler state only |

For `subGroupAffinity`, all SubJobs generated by the listed policies must share the same domain at the term's comparison tier.

For `subGroupAntiAffinity`, the number of policy names determines peer selection:

- **A single policy name**, such as `[prefill]`, selects intra-policy spreading. Every `prefill` SubJob must use a different domain from every other `prefill` SubJob.
- **Two or more policy names**, such as `[prefill, decode]`, select cross-policy isolation only. SubJobs generated by different listed policies may not share a domain, but SubJobs generated by the same policy may share a domain unless a separate single-policy term forbids it.

Multiple required terms are combined with AND. Adding a second policy name changes a term from intra-policy spreading to cross-policy isolation. Users that need both behaviors must declare separate terms.

#### PodGroup selection semantics

`podGroupAntiAffinity` is directional. A term is evaluated only from the PodGroup currently being scheduled. An existing PodGroup does not retroactively constrain a later PodGroup that has no matching rule of its own. Users must configure equivalent rules on all participants when bidirectional isolation is required.

Namespace selection follows Kubernetes affinity conventions:

- an omitted `namespaceSelector` selects only the current namespace
- an empty selector `{}` selects all namespaces
- a non-empty selector matches Namespace labels

`podGroupSelector` is then evaluated against PodGroup `metadata.labels` in the selected namespaces. The current PodGroup is excluded by UID. Selector parsing or Namespace lookup failures make a required term fail closed.

If a matching PodGroup occupies multiple domains at the comparison tier, all of those domains are excluded. Anti-affinity never reduces a multi-domain placement to a single LCA.

#### Constraint lifetime

Required and preferred policies affect new placement decisions. Updating a PodGroup, PodGroup label, Namespace label, or HyperNode tree does not evict running Pods. Subsequent allocations and retries use the latest Session snapshot.

A group may occupy more than one domain at a comparison tier. Occupancy is therefore derived as a set of domains from task placement rather than from a single Job or SubJob least common ancestor (LCA).

Tasks in `Allocated`, `Binding`, `Bound`, or `Running` state occupy a domain. A `Pipelined` task with a `NodeName` reserves its planned domain. `Releasing`, `Succeeded`, and `Failed` tasks do not occupy a domain. Persisted allocated-HyperNode information is used only as a conservative fallback when task placement is temporarily unavailable.

Under the proposed SubGroup semantics, required affinity and anti-affinity for the same pair of SubJobs are valid only when the affinity tier is strictly coarser than the anti-affinity tier. Using the same tier would require that pair to be both colocated and separated.

### 5.3 Scheduling Flow

`network-topology-aware` evaluates `networkTopology`. `group-topology-affinity` currently evaluates `topologyAffinity.podGroupAntiAffinity` and derives occupied domains from Jobs and Tasks in the current Session. The Framework intersects the constrained results from enabled gradient callbacks. The allocate action aggregates resources from Session Nodes, runs `FilterGradientsByMinResource`, and then performs HyperNode dry-runs and Node binding.

`JobInfo.RequiresHyperNodeAllocate()` determines whether this path is required. A hard `networkTopology` rule, a SubJob policy, required PodGroup anti-affinity, or preferred PodGroup anti-affinity causes the Job to enter `allocateForJob`. A PodGroup with preferred terms only must still receive a full-subtree gradient so that `HyperNodeOrderFn` can score candidates. If a supported topology policy is declared before the HyperNode cache is ready, the Job waits instead of falling back to ordinary Node scheduling. An empty `topologyAffinity: {}` is a no-op.

The Framework passes a `SearchPurpose` to Job and SubJob gradient callbacks so that allocation and eviction planning preserve their respective ordering and capacity semantics. Scoring callbacks and the allocation-only candidate-filter callback do not take this parameter.

### 5.4 Representative Scenarios

#### Example 1: Multi-instance fault isolation

When applied consistently to all replicas, the following policy places PodGroups with the same service label in different supernodes:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: PodGroup
metadata:
  name: llama-70b-instance-0
  namespace: default
  labels:
    topology.volcano.sh/group: llama-70b-prod
spec:
  minMember: 8
  queue: default
  topologyAffinity:
    podGroupAntiAffinity:
      required:
      - podGroupSelector:
          matchLabels:
            topology.volcano.sh/group: llama-70b-prod
        topologyTierName: supernode
```

#### Example 2: Prefill–Decode shard spreading and instance colocation (proposed)

Each unique tuple of values for the configured `matchLabelKeys` creates one SubJob. Prefill and Decode SubJobs are colocated in one supernode, while SubJobs of each role are spread across racks.

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: PodGroup
metadata:
  name: llama-70b-prefill-decode
  namespace: default
spec:
  minMember: 44
  queue: default
  subGroupPolicy:
  - name: prefill
    labelSelector:
      matchLabels:
        volcano.sh/role: prefill
    matchLabelKeys:
    - volcano.sh/shard-id
    subGroupSize: 8
    minSubGroups: 4
    networkTopology:
      mode: hard
      highestTierName: rack
  - name: decode
    labelSelector:
      matchLabels:
        volcano.sh/role: decode
    matchLabelKeys:
    - volcano.sh/shard-id
    subGroupSize: 6
    minSubGroups: 2
    networkTopology:
      mode: hard
      highestTierName: rack
  topologyAffinity:
    subGroupAffinity:
      required:
      - subGroups: [prefill, decode]
        topologyTierName: supernode
    subGroupAntiAffinity:
      required:
      - subGroups: [prefill]
        topologyTierName: rack
      - subGroups: [decode]
        topologyTierName: rack
```

PodGroup-level `networkTopology.highestTierName: supernode` may be used instead of `subGroupAffinity` when the entire PodGroup must share the same supernode. The two forms describe different scopes: the former covers the whole PodGroup, while the latter covers only the listed policies.

Cross-policy rack isolation can be added with a separate term:

```yaml
subGroupAntiAffinity:
  required:
  - subGroups: [prefill]
    topologyTierName: rack
  - subGroups: [decode]
    topologyTierName: rack
  - subGroups: [prefill, decode]
    topologyTierName: rack
```

The first two terms spread each role internally. The third prevents a Prefill SubJob from sharing a rack with a Decode SubJob.

#### Example 3: Preferred shard spreading (proposed)

The following policy keeps the instance within one supernode but treats rack spreading as a preference:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: PodGroup
metadata:
  name: llama-70b-soft-shard
  namespace: default
spec:
  minMember: 44
  queue: default
  networkTopology:
    mode: hard
    highestTierName: supernode
  subGroupPolicy:
  - name: prefill
    labelSelector:
      matchLabels:
        volcano.sh/role: prefill
    matchLabelKeys: [volcano.sh/shard-id]
    subGroupSize: 8
    minSubGroups: 4
  - name: decode
    labelSelector:
      matchLabels:
        volcano.sh/role: decode
    matchLabelKeys: [volcano.sh/shard-id]
    subGroupSize: 6
    minSubGroups: 2
  topologyAffinity:
    subGroupAntiAffinity:
      preferred:
      - subGroups: [prefill]
        weight: 100
        topologyTierName: rack
      - subGroups: [decode]
        weight: 100
        topologyTierName: rack
```

#### Example 4: Combined PodGroup and subGroup topology (proposed)

A workload may use all three blocks together. Using the `subGroupPolicy` definitions from Example 2, the combined configuration fragment is:

```yaml
metadata:
  labels:
    topology.volcano.sh/group: llama-70b-prod
spec:
  topologyAffinity:
    podGroupAntiAffinity:
      required:
      - podGroupSelector:
          matchLabels:
            topology.volcano.sh/group: llama-70b-prod
        topologyTierName: supernode
    subGroupAffinity:
      required:
      - subGroups: [prefill, decode]
        topologyTierName: supernode
    subGroupAntiAffinity:
      required:
      - subGroups: [prefill]
        topologyTierName: rack
      - subGroups: [decode]
        topologyTierName: rack
```

This policy has the following effects:

- `podGroupAntiAffinity` at `supernode` separates service replicas.
- `subGroupAffinity` at `supernode` keeps the current instance together.
- `subGroupAntiAffinity` at `rack` spreads shards within each role.

The supernode affinity tier is coarser than the rack anti-affinity tier, so the rules are compatible.

## 6. Detailed Design

### 6.1 API Types

#### Current PodGroup API

The implemented API contract is defined in `staging/src/volcano.sh/apis/pkg/apis/scheduling/v1beta1/types.go`. It contains the following PodGroup anti-affinity types:

```go
type PodGroupSpec struct {
    // Existing fields omitted.
    TopologyAffinity *TopologyAffinitySpec `json:"topologyAffinity,omitempty"`
}

type TopologyAffinitySpec struct {
    PodGroupAntiAffinity *PodGroupAntiAffinity `json:"podGroupAntiAffinity,omitempty"`
}

type PodGroupAntiAffinity struct {
    Required  []PodGroupAffinityTerm `json:"required,omitempty"`
    Preferred []PodGroupAffinityTerm `json:"preferred,omitempty"`
}

type PodGroupAffinityTerm struct {
    Weight            int32                 `json:"weight,omitempty"`
    PodGroupSelector  *metav1.LabelSelector `json:"podGroupSelector"`
    NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`
    TopologyTierName  string                `json:"topologyTierName,omitempty"`
    TopologyTier      *int32                `json:"topologyTier,omitempty"`
}
```

#### Proposed SubGroup API

SubGroup affinity and anti-affinity would extend `TopologyAffinitySpec` as follows, without changing the existing `podGroupAntiAffinity` field. These fields and types are specified only in this design; they are not included in the current Go API, CRD schema, OpenAPI definitions, or generated clients.

```go
type TopologyAffinitySpec struct {
    PodGroupAntiAffinity *PodGroupAntiAffinity `json:"podGroupAntiAffinity,omitempty"`
    SubGroupAffinity     *SubGroupAffinity     `json:"subGroupAffinity,omitempty"`
    SubGroupAntiAffinity *SubGroupAntiAffinity `json:"subGroupAntiAffinity,omitempty"`
}

type SubGroupAffinity struct {
    Required  []SubGroupAffinityTerm `json:"required,omitempty"`
    Preferred []SubGroupAffinityTerm `json:"preferred,omitempty"`
}

type SubGroupAntiAffinity struct {
    Required  []SubGroupAffinityTerm `json:"required,omitempty"`
    Preferred []SubGroupAffinityTerm `json:"preferred,omitempty"`
}

type SubGroupAffinityTerm struct {
    SubGroups        []string `json:"subGroups"`
    Weight           int32    `json:"weight,omitempty"`
    TopologyTierName string   `json:"topologyTierName,omitempty"`
    TopologyTier     *int32   `json:"topologyTier,omitempty"`
}
```

For the current PodGroup API, `weight` is meaningful only in a `preferred` list. A required term must omit it from the submitted manifest; the omitted field decodes to the Go zero value. Explicit `weight: 0` is rejected by the CRD schema, which requires any supplied weight to be in `[1, 100]`. The admission webhook rejects non-zero weights on required terms and requires a weight in `[1, 100]` on preferred terms. This combines schema validation with contextual validation while retaining the shared term type. The proposed SubGroup API would follow the same rules.

Preferred scoring never weakens a required constraint. Candidate scores start at `1.0`. Every conflicting preferred term subtracts `term.weight / 100`, with the result clamped to zero. The plugin then multiplies the value by its configured weight and `MaxNodeScore`. Job-level selection sums the allocation scores of its SubJobs.

The tier fields align with the existing `networkTopology` API:

| Purpose | Named tier | Numeric tier |
| --- | --- | --- |
| Consolidation boundary (`networkTopology`) | `highestTierName` | `highestTierAllowed` |
| Affinity or anti-affinity term | `topologyTierName` | `topologyTier` |

### 6.2 Topology Domain Semantics

Rules compare **topology domain names**, not Kubernetes Node hostnames. For a candidate HyperNode and a term comparison tier, the scheduler considers the candidate itself and then walks toward the root until it finds a HyperNode at that tier. That HyperNode's name is the domain identifier, denoted `Domain_T`.

A candidate at or below the comparison tier maps to one `Domain_T` if its ancestor chain contains that tier. A coarser candidate spans multiple domains and is not a legal final candidate for a required term. A required term fails closed if its tier cannot be resolved or the candidate has no ancestor at that tier.

Required PodGroup anti-affinity excludes every domain occupied by an applicable peer. Under the proposed SubGroup affinity semantics, required affinity must match every established peer anchor. If peer SubJobs already occupy more than one domain at the affinity tier, no new candidate could satisfy the rule; existing Pods would continue running, while new placement would remain Pending.

A preferred term applies its penalty only when the candidate resolves to an occupied domain at the term tier. An unknown `topologyTierName` produces an error during term compilation. A numeric `topologyTier` is used directly without checking whether that tier exists in the current tree. If neither the candidate nor any ancestor belongs to the resolved tier, a required term rejects the candidate, while a preferred term applies no penalty. This includes numeric tiers absent from the tree. Preferred scoring never overrides required constraints.

The following reference topology uses increasing tier values toward the root:

```mermaid
flowchart BT
    NA["node-a"] --> R1["rack-r1 · rack"]
    NB["node-b"] --> R1
    NC["node-c"] --> R1
    ND["node-d"] --> R1
    NE["node-e"] --> R2["rack-r2 · rack"]
    NF["node-f"] --> R2
    NG["node-g"] --> R2
    NH["node-h"] --> R2
    R1 --> SN1["supernode-sn1 · supernode"]
    R2 --> SN1
    SN1 --> RD["rdma-domain-1 · rdmaDomain"]
    SN2["supernode-sn2 · supernode"] --> RD
```

`topologyTier` maps to HyperNode `spec.tier`: a larger value denotes a coarser domain closer to the root. In the reference topology, `supernode > rack`.

### 6.3 Occupancy and Placement State

The source of truth for occupancy is Session task placement. The scheduler does not require a separately maintained `TopologyOccupancyIndex`.

For every task in an occupying state, the scheduler resolves:

```text
NodeName -> finest HyperNode -> ancestor at the term tier -> Domain_T
```

This produces an exact set of occupied domains even when one Job spans sibling domains. Expanding a single LCA would incorrectly mark unoccupied siblings as occupied.

If task placement produces no occupied domain at the comparison tier, the scheduler resolves the recorded `AllocatedHyperNode` at that tier. When the recorded HyperNode is coarser, `ResolveHyperNodesAtTier` expands it to all term-tier domains below that subtree. This fallback can conservatively block extra domains; exact task placement is preferred whenever available. It is not a substitute for a complete, ready topology snapshot.

The scheduler cache increments an internal task-placement generation when occupancy or SubJob membership changes; it does not copy topology or recalculate placement while holding the cache lock. A Session reconciles Job/SubJob placement when the task-placement or topology generation changes, or when a previous Session invalidated placement by making a temporary reservation. This internal invalidation flag survives cache writeback and is cleared only after successful reconciliation against actual cached tasks. It prevents uncommitted Pipeline reservations and failed allocations from leaving stale occupancy, while unchanged Jobs retain the generation-only fast path. Dry-run rollback restores both placement and its invalidation state. Writeback acknowledges reconciliation only if no newer task event has arrived. `JobUpdater` persists placement changes to the PodGroup annotation.

The Session lazily builds a Node-to-finest-HyperNode index and ancestor lookup tables from its immutable topology snapshot. The plugin compiles term tiers, selectors, and matching PodGroup references once per Session. Occupied domains are deduplicated and reused across terms within one evaluation, but never cached across scheduling attempts: Statement operations update task state and `NodeName` transactionally, and discard restores task, Node, and Job/SubJob placement.

Ordinary Jobs retain the non-topology fast path when group placement is not needed. When an enabled group-topology-affinity hook has PodGroup rules to evaluate, ordinary Jobs are also tracked because they may be selected as peers without declaring rules themselves.

For the proposed SubGroup scheduling behavior, placed peers establish affinity anchors and anti-affinity exclusion domains. If no peer has been placed, the first feasible SubJob establishes the anchor. SubJobs must be evaluated in a deterministic order. Tentative placements selected for an earlier SubJob remain visible while later SubJobs are evaluated, and abandoning a Job-level candidate rolls back the complete trial. Preferred scores never make an empty required candidate set schedulable.

### 6.4 Plugin Gradient Composition

`group-topology-affinity` registers Job and SubJob gradient callbacks. Required PodGroup terms are evaluated at both levels so that an allocated Job or SubJob search root cannot escape the policy. The proposed SubGroup terms would be evaluated only for SubJob callbacks and against peers from the same PodGroup; the current callbacks do not enforce those terms.

Gradient callbacks return a `HyperNodeGradientResult` containing `Unconstrained` and `Gradients`. When no required term applies and the plugin does not narrow the input search root, it returns `Unconstrained: true`. Preferred terms are evaluated by scoring.

The Framework intersects gradients by HyperNode name, not by layer index:

```mermaid
flowchart LR
    G["group-topology-affinity gradient"] --> I["Set intersection"]
    N["network-topology-aware gradient"] --> I
    I --> R["Rebuild layers by tier"]
    R --> A["allocate"]
```

| Callback result | Meaning | Framework behavior |
| --- | --- | --- |
| `Unconstrained: true`, with no gradients | No restriction within the input subtree | Skip intersection and exclusion statistics for this plugin |
| `Unconstrained: false`, with non-empty gradients | Constrained candidates | Intersect names with the other constrained results, then rebuild layers by tier |
| Zero-value result, or `Unconstrained: false` with nil or empty gradients | No legal candidate, or evaluation failed closed | Participate in statistics and make the final intersection empty |

If all participating callbacks return `Unconstrained`, the Framework generates the full input-subtree gradient once so preferred-only scoring still receives candidates. If no enabled callbacks are registered, the input root HyperNode is returned as the compatibility fallback. A result that sets both `Unconstrained` and non-empty `Gradients` is a contract violation and is treated as rejection.

The `network-topology-aware` plugin returns an unconstrained Job result only when neither its topology policy nor existing placement narrows the search, and no total-allocatable filtering is needed. Total-allocatable checks remain in its eviction callback; allocation resource checks still run after intersection.

After intersection, `rebuildGradientsByTier` sorts HyperNodes by name within a tier to keep tests and victim selection deterministic. Tier order depends on `SearchPurpose`:

- `PurposeAllocate`: fine to coarse
- `PurposeEvict`: coarse to fine

Eviction candidates are intersected before `GetCandidateDomains(maxDomains)` applies its single global limit. No plugin may independently truncate its candidate set before intersection.

### 6.5 HyperNode Selection and Node Binding

HyperNode scheduling uses two levels:

1. **HyperNode selection** chooses a topology subtree that satisfies `networkTopology`, `topologyAffinity`, and, when a resource lower bound is available, minimum aggregate capacity.
2. **Node selection** chooses a Kubernetes Node for each task within that subtree by running the existing predicate and scoring plugins.

```mermaid
flowchart TB
    H1["Collect plugin gradients"] --> H2["Intersect and rebuild by tier"]
    H2 --> H3["FilterGradientsByMinResource"]
    H3 --> H4["Dry-run each HyperNode candidate"]
    H4 --> N1["Load RealNodesList for the candidate"]
    N1 --> N2["PrePredicateFn and PredicateNodes"]
    N2 --> N3["Node scoring"]
    N3 --> N4["Statement.Allocate"]
    N4 --> C{"Best feasible dry-run?"}
    C -->|yes| COMMIT["Recover operations and commit"]
    C -->|no| DISCARD["Discard and try the next candidate"]
```

Node-level behavior remains unchanged. Taints, resources, ports, volumes, DRA, queue overuse, and Node scoring are still evaluated by the existing Node path. Passing all topology checks does not guarantee that a member Node passes predicates.

The resource filter removes empty layers from the gradient. Allocate iterates the remaining HyperNodes, then proceeds to the next surviving layer. If a legal HyperNode fails its Node dry-run, allocate tries the next candidate. A candidate is committed only when the corresponding Job or SubJob dry-run satisfies the existing Gang readiness or pipelining rules. If no candidate remains, the workload stays Pending and the collected HyperNode and Node reasons are reported together.

#### HyperNode resource pre-filter

For allocation, gradient callbacks answer **where topology permits placement**. `FilterGradientsByMinResource` answers **which of those domains can provide the minimum aggregate resources** before an expensive dry-run.

The filter runs after plugin intersection because a topology-only plugin does not own a resource ledger, and intersection may produce candidates that were never checked by another plugin's internal traversal.

For Job allocation, the filter uses `job.GetMinResources()`. If the PodGroup does not declare `minResources`, existing semantics are preserved and no new inferred Job-wide resource threshold is introduced.

For SubJob allocation, `subJob.GetMinResources()` calculates a conservative lower bound for reaching `MinAvailable`, after accounting for ready, pipelined, and Pending BestEffort members. For each resource dimension, it sums the smallest requests among the remaining required number of Pending tasks. This avoids rejecting a feasible subset of heterogeneous tasks merely because all Pending requests do not fit. The bound is recalculated for each attempt; actual Node placement and Gang checks decide feasibility.

For each candidate, resources are aggregated from Session Nodes in `RealNodesSet[hyperNode]`:

```text
idle       = sum(node.Idle)
futureIdle = sum(node.FutureIdle())

eligible if minResource <= idle OR minResource <= futureIdle
```

The filter is skipped when placement is already established, when `minResource` is absent, or when no reliable real-Node membership is available. In the last case, the candidate is conservatively retained. The Node dry-run remains the final authority.

This filter applies only to `PurposeAllocate`. Gang preemption and reclaim use total allocatable capacity for `PurposeEvict`, because those actions are evaluating whether resources could become available after eviction.

### 6.6 Gang Preemption, Reclaim, and Nomination

Gang preemption and reclaim reuse the aggregated HyperNode gradient callbacks to choose legal candidate domains. They may reclaim resources inside a domain that already satisfies every required topology rule, but they do not evict a matching PodGroup solely to remove an anti-affinity conflict.

The following rules apply:

1. `GetCandidateDomains` evaluates Job gradients with `PurposeEvict`. If the result is empty, Jobs with hard network topology or required PodGroup anti-affinity return no candidate domains. The cluster-root fallback is retained only for Jobs without either of those hard constraints.
2. Required `podGroupAntiAffinity` filters Job-level eviction domains. If any required HyperNode constraint exists, Gang simulation must also evaluate SubJob gradients.
3. `network-topology-aware` checks total allocatable capacity for eviction planning. Group topology continues to filter legal domains even when no hard `networkTopology` is present.
4. The Framework intersects complete plugin results, orders them from coarse to fine, and only then applies `maxDomains`.
5. Statement operations applied to victims are transactional. A victim in `Releasing` no longer occupies a domain during that simulation; discarding the simulation restores the occupancy.
6. Preferred policies do not make a domain illegal and are not, by themselves, a reason to select eviction victims.
7. `NominatedHyperNode` is a planning hint, not an authorization to bypass current constraints. Allocation validates only domains that cover the nominated Nodes, requiring a common eligible domain across plugins. The topology plugins provide an optional candidate-filter fast path equivalent to their allocation gradients; other plugins retain full-gradient evaluation. The existing Job and SubJob gradient callbacks are unchanged. A stale nomination is cleared before normal gradient search resumes.
8. A successful nomination path updates task, SubJob, and Job placement consistently and clears the fulfilled nomination.

### 6.7 Failure Handling and Diagnostics

Scheduling failures are aggregated independently at the HyperNode and Node dimensions:

```text
Plugin gradients
  -> HyperNodeGradientStats
  -> HyperNodeMinResourceFilterStats
  -> FormatHyperNodeFitSummary
  -> JobInfo.JobFitErrors

Node dry-run and predicates
  -> JobInfo.NodesFitErrors

JobFitErrors + NodesFitErrors
  -> FormatSchedulingDimensions / JobInfo.FitError()
  -> PodGroup Unschedulable condition and Warning Event
  -> Pod PodScheduled=False and FailedScheduling Event
```

The HyperNode summary records candidate counts by plugin and tier, the post-intersection count, resource-filter exclusions, and stable exclusion labels. The Framework maps known plugin names to user-facing labels:

- `podGroupAntiAffinity` for `group-topology-affinity`
- `networkTopology` for `network-topology-aware`
- `minResource` for aggregate resource filtering

Unknown plugins fall back to their plugin name. Reasons and tiers are sorted to keep messages deterministic. The proposed SubGroup behavior would require distinct `subGroupAffinity` and `subGroupAntiAffinity` exclusion labels; these are not currently emitted.

`allocateForJob` stores a HyperNode summary even when the intersection is non-empty so that a later Node predicate failure retains the topology context. Job-level errors form the dry-run baseline. Candidate retries clear only Node errors; SubJob HyperNode failures are merged with a `subJob <id>:` prefix instead of replacing the Job-level reason.

Example:

```text
HyperNode: 1/4 hyperNodes available (minResource: cpu 8):
supernode 1/2 (1 podGroupAntiAffinity); rack 0/2 (1 networkTopology, 1 minResource);
Node: worker-0: In hyperNode sn-b: 0/3 nodes are unavailable: 2 Insufficient cpu, 1 node(s) pod number exceeded
```

The scheduler reuses existing conditions and Events:

- PodGroup: `Warning/Unschedulable` Event and the existing `Unschedulable` condition
- Pod: `PodScheduled=False` and `Warning/FailedScheduling` Event

Pod status is updated only when the condition reason, message, or nominated Node changes, preventing duplicate updates.

Logging uses the following levels:

- V(3): scheduling failures, candidate dry-runs, and final selection
- V(4): Session tier inventory and screening summaries
- V(5): gradient details, matched peer occupancy, preferred scores, and no-solution details for a tier or Job
- Error: internal construction, lookup, or restore failures

Internal errors retain full detail in logs while user-facing summaries report a fail-closed unschedulable result.

### 6.8 Admission Validation

Validation applies to both CREATE and UPDATE. UPDATE validates the new topology specification but must not repeat create-only checks, such as requiring the current Queue state to be Open, for unrelated metadata or status changes.

#### Current validation

The CRD schema and admission webhook together enforce the following rules:

1. Every PodGroup anti-affinity term must set exactly one of `topologyTierName` and `topologyTier`. The CRD schema requires a non-negative numeric tier and limits tier names to 253 characters. Admission does not check whether a tier exists in the live HyperNode tree.
2. Every PodGroup anti-affinity term contains a valid `podGroupSelector`. If present, `namespaceSelector` must also be a valid Kubernetes label selector.
3. A required term must omit `weight` from the submitted manifest. A preferred term must set `weight` to a value in `[1, 100]`.
4. An empty `topologyAffinity` object, an empty policy block, or an empty term list is a no-op. Malformed PodGroup anti-affinity terms are rejected rather than interpreted as unconstrained requests.

The current API and CRD schema do not define SubGroup affinity or anti-affinity fields, so the webhook has no SubGroup-specific validation.

Admission validates the policy, not the scheduling path that will place each Pod. Acceptance of a PodGroup policy therefore does not imply enforcement for BestEffort Pods placed through backfill, which does not perform group topology filtering or scoring.

At runtime, an unknown named tier causes a required term to return a rejected gradient result, or a preferred term to return an error from `HyperNodeOrderFn` when scoring is evaluated. Numeric tiers are not checked for existence: a missing comparison ancestor rejects a required candidate but adds no preferred penalty, as described in Section 6.2.

#### Proposed SubGroup validation

The following semantic checks belong to the proposed SubGroup API and would be introduced together with that capability:

1. Every `subGroups` entry must name an existing `spec.subGroupPolicy[].name`, with no duplicates within a term.
2. Every `subGroupAffinity` term must contain at least two distinct policy names.
3. Every `subGroupAntiAffinity` term must contain at least one policy name. A single-name term must reference a policy capable of producing multiple SubJobs; with the current grouping model, that requires non-empty `matchLabelKeys`.
4. Required affinity and anti-affinity for the same SubJob pair must use an affinity tier strictly coarser than the anti-affinity tier. Admission should reject statically identifiable contradictions, including the same tier in both terms.
5. Term tier selection and required/preferred weights must follow the same contextual rules as PodGroup anti-affinity.

### 6.9 Scheduler Configuration

Configure `group-topology-affinity` to enforce PodGroup anti-affinity. Also configure `network-topology-aware` when combining it with `networkTopology`. For configured plugins, `enabledHyperNodeGradient` and `enabledHyperNodeOrder` default to `true` and can be omitted.

Merge the following plugin entries into the deployment's scheduler configuration, retaining its queue, fairness, and other required plugins:

```yaml
tiers:
- plugins:
  - name: gang
  - name: predicates
  - name: group-topology-affinity
    arguments:
      weight: 10
  - name: network-topology-aware
    arguments:
      weight: 10
```

The optional plugin-level `arguments.weight` defaults to `1`; the example uses `10` to increase these plugins' contribution to scoring. This setting is distinct from the mandatory `weight` on each preferred affinity term and does not affect required constraints.

This fragment only shows plugin configuration and does not change the scheduler's action list. PodGroup anti-affinity is enforced for Pods placed through `allocate`, but not for BestEffort Pods placed through backfill. In a PodGroup with members using both paths, whole-group isolation cannot be guaranteed. Backfill can still serve workloads that do not depend on these rules; see the [user guide's supported scope and limitations](../user-guide/how_to_use_podgroup_anti_affinity.md#supported-scope-and-limitations).

The default scheduler configuration does not include `group-topology-affinity`. Installing the API alone does not enable enforcement; the scheduler serving these workloads must include the plugin. CRDs, admission components, and scheduler configuration must be deployed consistently.

### 6.10 Code Mapping

| Area | Location |
| --- | --- |
| API types | `staging/src/volcano.sh/apis/pkg/apis/scheduling/v1beta1/types.go` |
| Term helpers, selectors, task-placement occupancy, and placement predicates | `pkg/scheduler/api/topology_affinity_info.go`, `pkg/scheduler/api/job_info.go` |
| HyperNode ancestry, tier resolution, and tier logging | `pkg/scheduler/api/hyper_node_info.go` |
| Session topology index and compiled term evaluation | `pkg/scheduler/api/hyper_node_index.go`, `pkg/scheduler/plugins/group-topology-affinity/constraints.go` |
| Nomination candidate validation | `pkg/scheduler/framework/hyper_node_candidate.go` |
| Plugin and registration | `pkg/scheduler/plugins/group-topology-affinity/`, `pkg/scheduler/plugins/factory.go` |
| Gradient contract, intersection, and statistics | `pkg/scheduler/framework/session_plugins.go`, `pkg/scheduler/api/unschedule_info.go` |
| Resource pre-filter and allocate dry-runs | `pkg/scheduler/actions/allocate/allocate.go`, `pkg/scheduler/actions/allocate/recorder.go` |
| Placement invalidation, reconciliation, and persistence | `pkg/scheduler/cache/event_handlers.go`, `pkg/scheduler/framework/session.go`, `pkg/scheduler/framework/job_updater.go` |
| PodGroup and Pod Events and conditions | `pkg/scheduler/cache/cache.go`, `pkg/scheduler/plugins/gang/gang.go` |
| Gang domain planning and simulation | `pkg/scheduler/actions/utils/`, `pkg/scheduler/actions/gangpreempt/`, `pkg/scheduler/actions/gangreclaim/` |
| Admission validation | `pkg/webhooks/admission/podgroups/validate/validate_podgroup.go` |

### 6.11 Validation Strategy

The validation strategy covers API behavior, scheduling semantics, plugin composition, state lifecycle, and observability. SubGroup semantic validation and scheduling coverage describe requirements for the proposed behavior, not completed tests:

| Area | Required coverage |
| --- | --- |
| API and CRD | Published manifests preserve all supported PodGroup anti-affinity fields and omit the proposed SubGroup affinity/anti-affinity fields; required weight omitted and explicit zero rejected by schema; preferred weight range; selector validation; tier one-of; CREATE and UPDATE |
| Domain resolution | Named and numeric tiers; unknown named-tier errors; absent numeric tier rejects required candidates but adds no preferred penalty; candidates below, at, or above the comparison tier; heterogeneous trees and missing ancestors; one Job occupying multiple domains |
| PodGroup rules | Same namespace; `namespaceSelector: {}`; Namespace-label selection; self-exclusion; directional rules; required fail-closed behavior; preferred scoring; preferred-only routing; HyperNode cache readiness |
| SubGroup rules (proposed) | Policy references, duplicates, and term cardinality; affinity/anti-affinity tier compatibility; single-name intra-policy spreading; multi-name cross-policy isolation; combined terms; affinity anchors; partial placement; deterministic SubJob order; dry-run rollback |
| Plugin composition | Unconstrained plus constrained; all-unconstrained subtree fallback; multiple constrained plugins; zero-value and empty rejection; invalid result rejection; no-callback root fallback; empty intersection; stable ordering; Job and SubJob gradients; exact preferred scores |
| Resource pre-filter | Explicit Job minimum; absent Job minimum; Pending SubJob requests; rescheduling after membership changes; idle and future-idle; existing placement bypass; missing real-Node membership; statistics |
| Diagnostics | Job baseline plus SubJob summaries; HyperNode and Node dimensions; stable labels and tier order; PodGroup and Pod Events; status-update deduplication; log levels |
| Placement lifecycle | Task add and delete; annotation write-back; Pipelined task with NodeName; releasing and terminal tasks; Statement rollback; restart fallback; sibling-domain occupancy |
| Gang eviction | Reclaim inside legal domains; total-allocatable pruning; no root fallback for hard network topology or required PodGroup anti-affinity; root fallback retained without those hard constraints; intersection before ordering and limit; simulation visibility and rollback; nomination acceptance, invalidation, and normal search |
| Combined lifecycle | PodGroup plus `networkTopology` (SubGroup combinations are proposed); scheduler restart; release and deletion; label changes; HyperNode topology changes; scale and performance regression |

## 7. Future Extensions

The following capabilities require separate designs but can be added without invalidating the API semantics defined here:

| Topic | Description |
| --- | --- |
| Topology-driven preemption | Evict matching lower-priority PodGroups specifically to clear a conflicting domain. |
| Backfill support | Apply required filtering and preferred scoring to BestEffort Pods placed through backfill. |
| Enqueue pre-check | Optionally detect global domain exhaustion before allocate. |
| API-level observability | Add a resolved topology-domain annotation for subGroups or a `TopologyUnsatisfiable` PodGroup condition. |
| Cross-PodGroup affinity | Add `podGroupAffinity` for colocation between PodGroups. |
| Cross-PodGroup subGroup rules | Extend subGroup relationships beyond a single PodGroup UID or namespace. |
| Training workloads | Align group topology semantics with Batch Job and `PartitionPolicy`. |

## 8. References

- [Network Topology Aware Scheduling](./Network%20Topology%20Aware%20Scheduling.md)
- [Topology Support for the Preempt Action](./preempt-action-support-topology.md)
