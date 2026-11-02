# Multilevel Label-Affinity Allocation

## Background

Volcano schedules a gang job's tasks over the cluster as independent scheduling
units. For many workloads this flat placement is not what the user wants:

- **Large-model training and inference**: pods of different "kinds" (embedding
  tables, pipeline stage replicas, prefill/decode shards, guard replicas) have
  strong reasons to be co-located with each other *up to a point* and
  *spread apart beyond that point*. Communication-heavy kinds want to share a
  leaf-switch domain for wide bandwidth; independent replicas want to sit on
  different cabinets so one cabinet failure does not take the whole service
  down.
- **Multi-level envelopes**: the same group of pods often has to fit inside a
  coarse domain (a rack, a "super-pod") and simultaneously split across finer
  domains inside it (leaf switches). Today this requires several ad-hoc
  mechanisms glued together.

Kubernetes gives two primitive tools — node selector/affinity and Pod topology
spread constraints — but both express **single-level** relationships only, and
topology spread does not enforce *co-location*, only *dispersion*. Volcano's
`network-topology` / HyperNode path can express multi-level co-location, but it
relies on a separate topology CR tree whose nodes must mirror the physical
topology, which is costly to maintain and couples scheduling to an external
model.

What is missing is a way to declare **multi-level node-to-node affinity**
directly against labels the cluster already carries on its nodes (rack id,
leaf-switch id, super-pod id, ...), without any managed CR.

## Goal

Let a job declare, on the PodGroup itself, a **chain of node-label keys with
block sizes** (coarse → fine). The job's tasks are then scheduled so that:

- every *level* of the chain is honored: the whole declared set goes together
  into one domain of the coarsest label, each sub-block into one domain of the
  next-finer label, and so on down to the finest level;
- the declaration has **two equivalent forms** — an *opaque count form* when the
  workload cannot be divided into named kinds beforehand, and an *explicit
  sub-job form* when it can (the two forms express the same intent and produce
  the same internal structure);
- a **flat job** (no affinity declared) schedules **byte-for-byte like the
  existing `allocate` action**, so the feature is purely additive and provably
  non-regressing; and
- the whole model is **label-only**: node domains are derived from node labels,
  no topology CR is ever consulted.

## Proposal

Introduce a new scheduler action (`allocate-unified` in the reference
implementation) that models scheduling as a **dual-tree registration**: the job
is factored into a *block tree* (job root → level-k blocks → … → leaf task
groups), and the cluster's nodes are factored into a *node-domain tree* by the
same label chain
(scoped by levels, all nodes at level 0 = "ClusterTop"). Scheduling = placing
each leaf block into one node domain at its level, with ancestors locked.

```
job ──► block tree (job → level-1 blocks → … → leaf task groups)
nodes ─► node-domain tree (ClusterTop → level-1 label domains → … → leaf domains)

block at level k binds to a level-k label domain; children must stay inside
their parent's domain (parent–child lock); siblings default to different
label-value domains (spread), with co-location as a capacity fallback.
```

### 1. Affinity chain declaration

A PodGroup declares the chain through two annotations:

- `affinity-config` (required to engage the feature): a comma-separated list of
  `label-key=count` pairs, ordered coarse → fine, e.g.

  ```yaml
  annotations:
    volcano.sh/affinity-config: "leaf-id=8,super-pod-id=4"
  ```

  means: *all 8 tasks go into one `leaf-id` domain; within it, groups of 4 go
  into `super-pod-id` domains*. The declared count is the absolute block size
  at that level; the label key identifies the node label that defines the
  domain. Keys containing `/` are used verbatim as full node label keys; bare
  short names resolve through a small built-in alias table (e.g. `leaf-id`,
  `super-pod-id`, `rack-id`), which a plugin may override (see §6).

- `affinity-policy` (optional): names a built-in policy that pins the key chain,
  e.g. `leaf-superpod` = `{leaf-id, super-pod-id}` coarse → fine. Declaring a
  policy demotes the name/order/depth validation to the policy: the config's
  keys must match the policy chain verbatim. Without a policy the chain is
  free-form and levels are numbered in declaration order.

Validation performed at parse time:

- every size must be a positive integer;
- adjacent sizes must *divide* cleanly coarse → fine (`size[i] % size[i+1] == 0`)
  so the block tree is a complete factorization — a job of `leaf-id=8,
  super-pod-id=3` is rejected as "not decomposable";
- no label key may repeat across levels (a repeated key would split the node
  domain by the same label twice, redundantly);
- with a policy, the config must match the policy's key order and depth.

### 2. Two equivalent declaration forms

The *count form* above is for jobs whose pods cannot be pre-classified into
kinds. When they can, the **sub-job form** expresses the same intent with named
groups: tasks carry Volcano's existing `volcano.sh/task-spec` (the sub-job /
role key), each task's Pod additionally declares a *group* affinity config — a
single `label-key=count` pair — and each task-spec group becomes an **explicit
leaf block** whose size is the group's *actual task count* (the annotation value
is only an assertion of the level, not a slicing instruction). The job-level
chain is merged with the group level (merged as the finest level), so:

```yaml
# job (8 tasks total: worker-a ×4, worker-b ×4)
annotations:
  volcano.sh/affinity-config: "leaf-id=8"        # whole gang in one leaf-id domain
# every worker-a / worker-b pod additionally:
annotations:
  volcano.sh/task-spec: worker-a                  # (native Volcano role key)
  volcano.sh/affinity-config: "super-pod-id=4"    # per-group domain at the fine level
```

produces exactly the same block tree as the count form `leaf-id=8,
super-pod-id=4`. The merge enforces the same coarse → fine divisibility
invariant (group size must divide the job chain's finest size), and the group
keys must be uniform across the job (same key everywhere, same group size), or
configuration is rejected.

### 3. Dual-tree model and binding rules

**Block** — logical grouping unit. Leaf blocks hold the full task list (the
independent scheduling unit); coarser blocks only constrain which domains their
children may reach. Every chain block is `BindHard`: it must land *whole* inside
one child domain of its parent's domain. The level-0 top block is `BindNone`
(binds only to the ClusterTop root).

**Node domain** — all nodes at level 0; at each chain level, every domain's
nodes are re-grouped by the value of that level's label. Nodes carrying no value
for a chain label are excluded from the subtree (and logged), because they
cannot satisfy that level's affinity. Child order is deterministic (label values
sorted). No hyperNode / topology CR is consulted.

**Binding** — each block dry-runs against the candidate label-value domains of
its parent domain:

1. **Capacity pre-filter**: a candidate must fit the block's *whole* aggregate
   `Need` against the domain's aggregate headroom, otherwise it is pruned;
2. **Dry-run**: for a leaf, run the existing per-task allocation/predicate pass
   restricted to the candidate's nodes — only a *full placement* of all of the
   block's tasks inside one domain counts (partial candidates are rejected so a
   block is never split across domains); for a coarser block, recurse into its
   children per candidate;
3. **Score & commit best**: candidates are scored by *remaining headroom* after
   occupancy (more headroom wins — spreads load, still packs when tight); the
   winner's statement is recovered, losers are discarded via the framework
   `Statement` backup/recover/discard machinery;
4. **Sibling-distinct default**: siblings bound under one candidate domain
   prefer *different* label-value domains (two-phase candidate selection: the
   still-unused capable domains come first) and fall back to co-location only
   when no other capable domain exists. This is the "spread for isolation"
   default; affinity *envelopes* still come from the shared ancestors.

A block with no fitting domain yields "nothing to do" — its tasks stay pending
and are retried next cycle; the job is not committed.

### 4. Session-scoped placement snapshot

The placement (block tree + node-domain tree + resolved chain) is built **once
per session** and cached on the session. Rescheduling reads the *bound domain*
of an evicted task straight from the snapshot (`BoundDomain`), so an evicted
task is re-placed back into the same domain — grouping keys and domain
assignments never drift between cycles. The cache is revalidated by *set
equality* of pending task UIDs, and a committed snapshot is never overwritten by
a later empty rebuild.

### 5. Commit gate

Block-level work is always a dry-run. A statement is committed only when the
gang plugin's `JobReady` agrees (job-level commit); a pipelined-but-not-ready
job keeps its dry-run allocations in the session; a job that is neither ready
nor pipelined has them rolled back. This mirrors the existing `allocate` commit
gate exactly.

### 6. Session-level extension seams

The four stages — label-chain resolution, block-tree building, node-domain
building, domain scoring — are session-level seams registered under plugin
names. The action installs default implementations; a plugin registered in a
tier overrides the corresponding stage for the session (e.g. a production plugin
can introduce its own policies, chain orderings, or a placement-aware domain
scorer). With no overrides, the default path is byte-identical to the inline
implementation.

## Details

### Node-domain construction

| Level | Key | Example value | Meaning |
| --- | --- | --- | --- |
| 0 | — | `cluster` | ClusterTop: all candidate nodes |
| 1 | `leaf-id` | `leaf-1` | coarsest affinity domain |
| 2 | `super-pod-id` | `sp-4` | fine domain inside the leaf |

Nodes are grouped by level-1 label value, then each level-1 domain is split by
level-2 label value, etc. *Parent–child lock*: a level-2 leaf block bound to
`sp-4` automatically lives inside whatever `leaf-id` domain contains that
super-pod domain — the coarse chain level is honored by construction, not by an
extra constraint.

**Missing labels**: a node without `leaf-id` (or any chain label) is excluded
from the whole domain tree and reported (logged / `MissedNodes`). The placement
only ever sees nodes that can honor the declared affinity.

### Binding algorithm (per top block)

```text
bindAndAllocateBlock(B, parentDomain):
  if B is a leaf:
    candidates = leafCandidates(B, parentDomain)   # capacity-filtered, used-aware
    for each candidate: dry-run full placement of B.Tasks → score headroom
    keep best statement, discard losers, bind B.Domain
  else:
    for each candidate domain of B:
      childCtx = fresh sibling context
      for each child: recurse bindAndAllocateBlockCtx(child, domain, childCtx)
      mark child-bound domains used in childCtx   # siblings drift apart
      reject partial BindHard candidate (whole block in one domain)
      score domain, merge children's statements, keep best
```

Only a *full* placement is a winner at every level: a partially-filled leaf, or
a coarser block whose children are only partially placed under one candidate, is
rejected and reverted, keeping the invariant that a block is never split across
domains.

### Flat degeneracy (no affinity)

No `affinity-config` at all → flat placement: a single ClusterTop node domain
plus one leaf block per pending task (`BindNone`). The binding path then walks
exactly one candidate (the parent domain), and per-task allocation order follows
`TaskOrderFn` — reproducing the legacy `allocate` behavior. A **PARITY test**
(`TestParityNoAffinity` in the reference implementation) asserts that a
no-affinity job produces task-for-task identical bindings under both actions,
so the feature provably cannot regress the default path.

## Feasibility

The cost is one extra factoring pass and one extra level of candidate loops
compared to `allocate`, both bounded and cheap:

| | `allocate` (today) | `allocate-unified` (proposed) |
| --- | --- | --- |
| Build | queue → job → per-task | + block-tree factor (O(tasks)) + node-domain build (O(nodes × levels)) once per session |
| Placement | task → node | per block: candidate domains (capacity-filtered), each a full-placement dry-run, scored |
| Commit | gang `JobReady` gate | identical gate (job-level) |
| No-affinity | — | byte-identical path (PARITY baseline) |
| Rescheduling | tasks restored into the session | evicted task reads its `BoundDomain` from the snapshot |
| Overhead sensitivity | — | extra cost exists only for jobs that declare affinity; flat jobs pay nothing |

- Reusing the existing per-task predicate pass inside each leaf-candidate
  dry-run keeps the binding faithful to the current predicate semantics
  (predicate-error caching is enabled by default).
- The node-domain tree is built once per session and aliased by every block
  candidacy, so the per-block cost is candidate iteration, not reconstruction.

### Advantages

- **No managed topology CR**: co-location and spread both come from node labels
  the cluster already carries; nothing to provision or mirror.
- **Multi-level in one declaration**: the chain is arbitrarily deep (>2), and
  envelope + spread are expressed in the same annotation — no glue between
  mechanisms.
- **Two equivalent forms, one structure**: opaque count form and named sub-job
  form produce the same block tree, so the model works whether or not the
  workload can be pre-classified.
- **Additive and non-regressing**: no-affinity jobs behave byte-for-byte like
  the legacy action, verified by a PARITY test.
- **Plugin-extensible by design**: the four session seams let a plugin swap the
  chain semantics, the block factoring, the domain construction, or the scoring
  without touching the action.
- **Deterministic**: chain order, node-domain child order, and candidate
  iteration are fully sorted; first-fit tie-breaking is reproducible.

### Possible Concerns

- **Label availability is authority**: nodes without a chain label are excluded,
  and their capacity is invisible to affinity jobs. Operators must ensure the
  chain labels are applied everywhere they expect placement; excluded nodes are
  logged so the gap is observable.
- **Complete-factorization requirement**: block sizes must divide coarse → fine,
  and the total pending count must fill whole blocks. Unaligned tails stay
  pending and are retried next cycle — no partial block is ever committed. This
  is a config-author responsibility; parse errors are rejected loudly.
- **Annotation namespace/keys are a proposal**: `volcano.sh/affinity-config` /
  `volcano.sh/affinity-policy` and the built-in short label aliases are
  provisional names open for review.
- **Interaction with other actions**: preempt/reclaim currently treat these jobs
  as ordinary jobs. Behaviors such as *reclaim only within the original bound
  domain* or *preempt from the same domain* remain open follow-ups.

## Status and Next Steps

This document is a design proposal, modeled on the same shape as the
`unresolvable-job-rollback` proposal. A reference implementation of the model
above already exists in a working fork (`pkg/scheduler/actions/allocate-unified`
in the fork) and has been exercised against the current scheduler code: the
protocol parser, dual-form block factoring (with the equivalence test between
count and sub-job forms), the label-only node-domain builder, the
sibling-distinct binder, the session snapshot with `BoundDomain`, the four
extension seams, and the no-affinity PARITY baseline are all present and
covered by tests. This doc is documentation-only: it introduces no scheduler
behavior change to upstream.

Open items for community discussion:

1. **API naming** — annotation keys (`volcano.sh/affinity-config` /
   `volcano.sh/affinity-policy`), the built-in policy name, and the short label
   aliases;
2. **Action vs. plugin** — ship as a new action `allocate-unified`, or fold the
   model into the existing `allocate` action behind an annotation trigger;
3. **Validation** — a webhook to validate chain divisibility and duplication at
   admission instead of at scheduling time;
4. **Semantics with rescheduling / preempt / reclaim** — whether evictions may
   cross domain boundaries, and whether reclaimed tasks are pinned to their
   bound domain;
5. **Interplay with existing topology mechanisms** — how a chain job should
   behave when it also uses `network-topology` / HyperNode tiers or Pod spread
   constraints;
6. **User documentation** for volcano.sh.

We intend to iterate on these points with the community before submitting an
implementation PR.