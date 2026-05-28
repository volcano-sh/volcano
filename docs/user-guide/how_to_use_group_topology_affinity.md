# Group Topology Affinity User Guide

## 1 Background

[Network Topology Aware Scheduling](./how_to_use_network_topology_aware_scheduling.md) (`network-topology-aware` plugin) focuses on **intra-group** placement: Pods or SubJobs are scheduled **within** a HyperNode performance domain (for example, gang scheduling inside one cabinet, or keeping an entire Job under one supernode tier).

Production workloads often need **inter-group** topology rules as well:

- **Across PodGroups:** Multiple inference **instances** (each instance is a PodGroup) should not land on the same supernode, so one hardware failure does not take down every online replica.
- **Within one PodGroup:** Prefill–Decode or multi-shard inference needs **shards on different cabinets** but the **whole instance on one supernode**, which cannot be expressed cleanly with Pod-level `podAffinity` alone.

**Group topology affinity** adds PodGroup-level fields and the `group-topology-affinity` scheduler plugin. It works together with `network-topology-aware`: group-internal rules use `networkTopology`; group-external rules use `topologyAffinity` and `subGroupTopologyAffinity`.

For API and design details, see the [Group Topology Affinity design proposal](../design/group-topology-affinity.md).

## 2 Features

### 2.1 Three layers of topology rules

| Layer | Field | Scope | Typical goal |
|-------|--------|--------|----------------|
| Intra-group | `networkTopology` on PodGroup or `subGroupPolicy` | Pods / SubJobs inside one policy | Gang in one cabinet; whole Job under one tier |
| Inter-group (same PodGroup) | `subGroupTopologyAffinity` | SubJobs from different `subGroupPolicy` names | Shard spread across cabinets; optional prefill/decode colocation |
| Inter-group (cross PodGroup) | `topologyAffinity.podGroupAntiAffinity` | Other PodGroups | Multiple instances on different supernodes |

Hard constraints use `requiredDuringSchedulingIgnoredDuringExecution`. Soft preferences use `preferredDuringSchedulingIgnoredDuringExecution` with `weight`.

### 2.2 Cross-PodGroup anti-affinity with `podGroupSelector`

Peers are selected with a standard Kubernetes **`podGroupSelector`** (`metav1.LabelSelector`) on **PodGroup `metadata.labels`**. Volcano does **not** provide a dedicated `topologyGroup` string field.

You must **set labels on each PodGroup yourself** when creating it. Pods that should avoid each other at a given tier should share the **same label key and value** on that key; use `podGroupSelector.matchLabels` (or `matchExpressions`) to reference them.

**Label assignment (recommended practice):**

| Item | Guidance |
|------|----------|
| Who sets labels | Platform or workload owner on PodGroup create/update; the scheduler does not auto-generate them. |
| What the value means | A **fault-domain or capacity pool** (for example `llama-70b-prod` = all production replicas of one model that must spread at supernode tier). |
| Environment isolation | Use different values per environment or tenant (`…-staging` vs `…-prod`). |
| Selector scope | Only put labels in `podGroupSelector` that define the peer set; unrelated labels (for example `app` used only for ops filtering) need not appear in the selector. |

### 2.3 Intra-PodGroup SubJob rules

`subGroupTopologyAffinity` is defined on **PodGroup spec** (not on each `subGroupPolicy` entry):

- **`subGroupAffinity`:** Listed policies share one domain at the tier given in `topologyDomain` (for example prefill and decode on the same supernode).
- **`subGroupAntiAffinity`:** Uses **`subGroupSelector`** and **`antiSubGroupSelector`** (both required). Use the **same** policy names on both sides for shard-to-shard exclusion within one role; use **different** names for cross-role exclusion (optional).

`matchSubGroupPolicyNames` refers to `subGroupPolicy[].name` only (for example `prefill`), **not** shard suffixes like `prefill-0`.

### 2.4 Topology domain tier: name or integer

Each affinity/anti-affinity term includes **`topologyDomain`**. Inside it, set **either** `topologyTierName` (matches `HyperNode.spec.tierName`) **or** `topologyTier` (matches `HyperNode.spec.tier`), not both—the same rules as `networkTopology` (`highestTierName` / `highestTierAllowed`).

This is **not** Kubernetes `PodTopologySpread.topologyKey` (a Node label key). Here you choose a **HyperNode tier** at which `Domain_T` is computed. See [design decision 7](../design/group-topology-affinity.md#ad-7-inter-group-tier-naming-and-kubernetes-topologykey) in the design doc.

Example mapping (your cluster may differ):

| Tier name | Tier integer (example) | Typical use |
|-----------|-------------------------|-------------|
| `cabinet` | `1` | Shard / SubJob gang |
| `supernode` | `2` | Whole instance or cross-PodGroup spread |

## 3 Prerequisites

1. **Volcano** installed with scheduler configured for HyperNode allocation (same baseline as [network topology aware scheduling](./how_to_use_network_topology_aware_scheduling.md)).
2. **HyperNode** CRs describing your network tree ([build manually](./how_to_use_network_topology_aware_scheduling.md#322-build-manually) or [auto-discovery](./how_to_use_hypernode_auto_discovery.md)).
3. Workloads using **PodGroup** (directly or via controllers that create PodGroups) with `subGroupPolicy` / `matchLabelKeys` when you need multi-shard SubJobs.

## 4 User Guide

### 4.1 Enable scheduler plugins

Edit the scheduler ConfigMap:

```shell
kubectl edit cm -n volcano-system volcano-scheduler-configmap
```

Add **`group-topology-affinity`** next to **`network-topology-aware`**:

```yaml
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, backfill"
    tiers:
    - plugins:
      - name: priority
      - name: gang
      - name: predicates
    - plugins:
      - name: group-topology-affinity
        arguments:
          weight: 10
      - name: network-topology-aware
        arguments:
          weight: 10
          hypernode.binpack.cpu: 5
          hypernode.binpack.memory: 1
```

Plugin `arguments.weight` scales this plugin’s **HyperNodeOrderFn** score (same key as `network-topology-aware`). It is separate from **`preferred[].weight`** on each PodGroup term, which controls preference strength inside that term.

Restart or reload the scheduler after changing the ConfigMap.

### 4.2 PodGroup API overview

Fields are added to **`PodGroup.spec`** (`scheduling.volcano.sh/v1beta1`):

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: PodGroup
metadata:
  name: my-workload
  labels:
    # User-defined labels for cross-PodGroup peer matching (see section 2.2)
    topology.volcano.sh/spread-group: my-spread-group
spec:
  minMember: 1
  queue: default
  # Optional: whole PodGroup network envelope (intra-group, network-topology-aware)
  networkTopology:
    mode: hard
    highestTierName: supernode
  # Optional: cross-PodGroup anti-affinity (inter-group)
  topologyAffinity:
    podGroupAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        - podGroupSelector:
            matchLabels:
              topology.volcano.sh/spread-group: my-spread-group
          topologyTierName: supernode
  # Optional: same-PodGroup SubJob relationships (inter-group within PodGroup)
  subGroupTopologyAffinity:
    subGroupAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution: []
    subGroupAffinity:
      requiredDuringSchedulingIgnoredDuringExecution: []
  subGroupPolicy: []
```

**Do not** put `mode: hard` or `mode: soft` inside group topology **terms**; hard vs soft is determined only by `required` vs `preferred` lists.

### 4.3 Scenario: Multiple inference instances (cross-PodGroup)

**Goal:** Three PodGroups serving the same model run at the same time; each instance uses a **different supernode** so one supernode failure affects only one instance.

**Steps:**

1. Choose a label key/value for the spread group (all instances that must repel each other share the same value).
2. Set that label on **every** PodGroup `metadata.labels`.
3. Add the same `podGroupSelector` and `topologyTierName` (or `topologyTier`) on each PodGroup.

**Example (one instance):**

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: PodGroup
metadata:
  name: llama-70b-instance-0
  labels:
    # Required: you set this; Volcano does not add it automatically.
    topology.volcano.sh/spread-group: llama-70b-prod
spec:
  minMember: 8
  queue: default
  topologyAffinity:
    podGroupAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        - podGroupSelector:
            matchLabels:
              topology.volcano.sh/spread-group: llama-70b-prod
          topologyTierName: supernode
          # topologyTier: 2   # use either name or integer, not both
```

Create `llama-70b-instance-1`, `llama-70b-instance-2`, … with the **same** `topology.volcano.sh/spread-group: llama-70b-prod` label and the same anti-affinity term.

**Expected result:** Each PodGroup is placed in a different `Domain_supernode`. If only two supernodes are free, the third PodGroup stays Pending until a supernode domain is available.

**Verify:**

```shell
kubectl get podgroup -o wide
kubectl describe podgroup llama-70b-instance-0
```

Check scheduler logs if PodGroups remain Pending and no supernode domain is available for the next instance.

### 4.4 Scenario: Prefill–Decode on one instance (intra-PodGroup)

**Goal:** One inference instance with 4 prefill shards and 2 decode shards:

- Each shard’s Pods gang-schedule in **one cabinet** (intra-group).
- Prefill shards use **different cabinets**; decode shards use **different cabinets** (optional: prefill and decode may share a cabinet).
- The **whole instance** stays on **one supernode**.

Use two `subGroupPolicy` entries (`prefill`, `decode`) and `matchLabelKeys` to split shards into SubJobs. Do **not** create separate policy names per shard (`prefill-0` is wrong in `matchSubGroupPolicyNames`).

**Recommended layout (method 1):** Put the supernode envelope on **`spec.networkTopology`** and shard cabinet rules in **`subGroupTopologyAffinity`**.

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: PodGroup
metadata:
  name: pd-instance-0
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
      matchLabelKeys:
        - volcano.sh/shard-id
      subGroupSize: 8
      minSubGroups: 4
      networkTopology:
        mode: hard
        highestTierName: cabinet
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
        highestTierName: cabinet
  subGroupTopologyAffinity:
    subGroupAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        - subGroupSelector:
            matchSubGroupPolicyNames: [prefill]
          antiSubGroupSelector:
            matchSubGroupPolicyNames: [prefill]
          topologyTierName: cabinet
        - subGroupSelector:
            matchSubGroupPolicyNames: [decode]
          antiSubGroupSelector:
            matchSubGroupPolicyNames: [decode]
          topologyTierName: cabinet
```

**Alternative (method 2):** Omit top-level `networkTopology` and colocate prefill + decode with one affinity term:

```yaml
  subGroupTopologyAffinity:
    subGroupAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        - matchSubGroupPolicyNames: [prefill, decode]
          topologyTierName: supernode
    subGroupAntiAffinity:
      # same subGroupAntiAffinity terms as above
```

Use **either** method 1 **or** method 2 for supernode colocation, not both.

**Pod template labels:** Pods must carry labels matched by `subGroupPolicy.labelSelector` and any `matchLabelKeys` (for example `volcano.sh/role`, `volcano.sh/shard-id`).

### 4.5 Scenario: Production stack (multi-instance + Prefill–Decode)

Combine [section 4.3](#43-scenario-multiple-inference-instances-cross-podgroup) and [section 4.4](#44-scenario-prefilldecode-on-one-instance-intra-podgroup):

- `topologyAffinity.podGroupAntiAffinity` @ `supernode` with shared spread-group label across instances.
- `subGroupPolicy` + `subGroupTopologyAffinity` as in section 4.4 inside each instance.

Each instance then occupies one supernode; inside that supernode, shards spread across cabinets as configured.

### 4.6 Optional: Soft shard spread (resource-constrained clusters)

When cabinets are scarce, you may **prefer** shard separation but still allow scheduling if separation is impossible. Change `subGroupAntiAffinity` from `required` to `preferred` and set term `weight` (higher = stronger preference to avoid sharing a cabinet).

```yaml
  subGroupTopologyAffinity:
    subGroupAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 100
          term:
            subGroupSelector:
              matchSubGroupPolicyNames: [prefill]
            antiSubGroupSelector:
              matchSubGroupPolicyNames: [prefill]
            topologyTierName: cabinet
```

Keep `networkTopology` @ `supernode` as hard if the whole instance must stay on one supernode.

Do **not** add `mode: soft` on the term; soft is expressed only via the `preferred` list.

### 4.7 Optional: Force prefill and decode on different cabinets

By default, instance 4 does **not** require prefill and decode to use disjoint cabinets. To force cross-role cabinet separation, add a term with **disjoint** policy names:

```yaml
    subGroupAntiAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        # ... existing prefill/prefill and decode/decode terms ...
        - subGroupSelector:
            matchSubGroupPolicyNames: [prefill]
          antiSubGroupSelector:
            matchSubGroupPolicyNames: [decode]
          topologyTierName: cabinet
```

This improves role-level isolation but may increase cross-cabinet traffic between prefill and decode.

## 5 Configuration reference

### 5.1 `topologyAffinity.podGroupAntiAffinity`

| Field | Required | Description |
|-------|----------|-------------|
| `podGroupSelector` | Yes | Label selector matching **other** PodGroups’ `metadata.labels`. |
| `namespaceSelector` | No | Limit which namespaces peer PodGroups may come from. |
| `topologyTierName` / `topologyTier` | One of two | Tier at which domains must differ. |

Phase 1 supports **anti-affinity only** (no `podGroupAffinity` for cross-PodGroup colocation). Use `networkTopology` or `subGroupAffinity` for colocation within or across SubJobs in one PodGroup.

### 5.2 `subGroupTopologyAffinity`

| Sub-field | Use |
|-----------|-----|
| `subGroupAffinity` | `matchSubGroupPolicyNames`: all listed policies share one domain at the tier. |
| `subGroupAntiAffinity` | `subGroupSelector` + `antiSubGroupSelector`: subject SubJobs vs peer SubJobs must differ at the tier. |

### 5.3 What not to configure

| Mistake | Correct approach |
|---------|------------------|
| Use `podGroupAntiAffinity` for prefill vs decode inside one PodGroup | Use `subGroupTopologyAffinity` |
| Write `prefill-0` in `matchSubGroupPolicyNames` | Use policy name `prefill` + `matchLabelKeys` for shards |
| Set `mode: soft` on a group topology term | Use `preferredDuringSchedulingIgnoredDuringExecution` |
| Set both `topologyTierName` and `topologyTier` on one term | Choose one |
| Omit `podGroupSelector` on cross-PodGroup terms | `podGroupSelector` is required |
| Rely on Pod `podAntiAffinity` alone for PodGroup-level spread | Set PodGroup labels + `podGroupAntiAffinity` |

## 6 Troubleshooting

| Symptom | Things to check |
|---------|-----------------|
| PodGroup Pending, topology-related | Enough HyperNode domains at the required tier; `minMember` / gang; `filterGradientsByMinResource` (capacity). |
| Instances still on same supernode | Same `topology.volcano.sh/spread-group` (or your key) on all instances; selector matches labels; plugin `group-topology-affinity` enabled. |
| Shards on same cabinet | `subGroupAntiAffinity` terms present; both selectors list correct policy names; hard `required` not replaced by soft without intent. |
| Webhook rejects PodGroup | `subGroupPolicy` length ≥ 2 when `subGroupTopologyAffinity` is set; tier names exist in cluster HyperNodes; no forbidden fields in wrong section. |
| Rules seem ignored | Confirm both `network-topology-aware` and `group-topology-affinity` when using group + intra rules; hard rules need non-empty `required` lists. |

Enable verbose scheduler logging if needed and search for `group-topology-affinity` and `HyperNodeGradient` in scheduler logs.

## 7 See also

- [Group Topology Affinity design proposal](../design/group-topology-affinity.md)
- [Network Topology Aware Scheduling user guide](./how_to_use_network_topology_aware_scheduling.md)
- [How to configure scheduler](./how_to_configure_scheduler.md)
- Volcano issue: [volcano-sh/volcano#5347](https://github.com/volcano-sh/volcano/issues/5347)
