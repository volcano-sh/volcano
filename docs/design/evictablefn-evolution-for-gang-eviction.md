# EvictableFn Evolution for Gang-aware Eviction

[@vzhou-p](https://github.com/vzhou-p); April 8, 2026

## Motivation

This document is a follow-up to `gang-aware-eviction-design.md`, and covers how plugins narrow the victim task set during preemption and reclaim.

Today, the `preempt` and `reclaim` actions use session hooks registered as `PreemptableFn` and `ReclaimableFn`. Both share the same underlying type, `EvictableFn`:

```go
type EvictableFn func(task *TaskInfo, candidates []*TaskInfo) ([]*TaskInfo, int)
```

Gang-aware eviction is job-oriented, while `EvictableFn` is task-oriented: the initiator is always a single `*TaskInfo`. The gang-aware eviction design proposes to flatten victim bundles to `[]*TaskInfo` before calling `ReclaimableFn` / `PreemptableFn` so gang actions can reuse the current API. Still, two problems remain:

1. **Representative task** — Callers must pick one task to stand in for the initiating job. Tasks in the same job can differ in priority, resource requests, and other fields, so one representative is not reliably correct for every plugin.
2. **Implicit mode** — The same `EvictableFn` is used for legacy `preempt` / `reclaim` and for `gangpreempt` / `gangreclaim`. The signature does not state whether the evaluation is for gang-aware or legacy task eviction.

## Goals

- Extend the plugin model so gang-aware eviction can be wired correctly.
- Preserve backward compatibility for existing `preempt` / `reclaim` actions and plugins.

## Non-goals

- Requiring every existing plugin to adopt gang-aware eviction

---

## Proposal: `UnifiedEvictableFn` with `EvictionContext`

Introduce a single victim-filter callback, `UnifiedEvictableFn`, driven by `EvictionContext`. The `EvictionKind` value distinguishes gang-aware vs legacy task eviction so plugins can branch without guessing.

- **Legacy actions** (`preempt`, `reclaim`) continue to use `EvictableFn` for the time being; migration to `UnifiedEvictableFn` is a later step.
- **Gang actions** (`gangpreempt`, `gangreclaim`) invoke `UnifiedEvictableFn` only for plugins that register it.

Plugins that do not register `UnifiedEvictableFn` are skipped on gang paths and do not take part in victim filtering there.

**Opt-in:** Plugins that only implement today’s `EvictableFn` are not automatically applied to gang actions. Any plugin that should filter victims on gang paths must register `UnifiedEvictableFn` explicitly. Gang actions do not fall back to legacy `EvictableFn` (no bridge or shim), so legacy victim-filter code is never run in a gang context unless the plugin opts in via the new callback.

### Proposed Approach

**Types:**

```go
type EvictionKind int

const (
	Unknown 		 EvictionKind = iota
	GangEvictPreempt
	GangEvictReclaim
	TaskEvictPreempt
	TaskEvictReclaim
)

type EvictionContext struct {
	Kind   EvictionKind
	Job    *JobInfo
	Task   *TaskInfo

	PendingTasks []*TaskInfo
	HyperNode string
}

type UnifiedEvictableFn func(ctx *EvictionContext, candidates []*TaskInfo) ([]*TaskInfo, int)
```

#### Pros

* `EvictionKind` states gang vs legacy preempt/reclaim directly in the context, and `EvictionContext` groups job, task, pending tasks, and hypernode so job-oriented gang eviction does not depend on one representative task for all plugins.
* A single `UnifiedEvictableFn` can eventually replace legacy `EvictableFn` after migration, shrinking the long-term API, while gang paths remain opt-in with no silent fallback to legacy `EvictableFn`.

#### Cons

* Every caller must assemble `EvictionContext` carefully; a wrong `Kind` or half-filled struct is easy to miss and hard to debug.
* More fields are likely to be added to the struct as requirements grow, so it needs clear ownership and review discipline.
* During transition, legacy actions keep `EvictableFn` and gang actions use `UnifiedEvictableFn`, so plugins may carry both implementations until the migration finishes.

---

## Alternatives considered

### Option 1: GangEvictableFn without eviction context

```go
type GangEvictableFn func(actorJob *JobInfo, candidates []*TaskInfo) ([]*TaskInfo, int)

type GangReclaimableFn = GangEvictableFn
type GangPreemptableFn = GangEvictableFn
```

#### Pros

The callbacks stay simple and align with the existing API model, while a distinct gang entry point still separates gang logic from legacy `EvictableFn`.

#### Cons

Metadata such as hypernode does not appear in the signature. When that data is required later, the API will need another change and will typically drift toward a dedicated context struct.

### Option 2: Add `Job` to existing `EvictableFn`

```go
type EvictableFn func(job *JobInfo, task *TaskInfo, candidates []*TaskInfo) ([]*TaskInfo, int)
```

#### Pros

* This is the least disruptive change to the current model: add `job` beside today’s task.
* Many plugins can migrate with a thin wrapper that forwards `task` to the old logic and ignores `job` until they are ready.

#### Cons

* Gang vs legacy mode remains implicit.
* Implicit contract such as “non-nil `job` means gang-aware” are fragile.
* Session and actions must populate `job` and `task` consistently, so correctness rests more on caller knowledge.

---

## Migration Plan

This section is forward-looking: it describes how current existing plugins should adopt `UnifiedEvictableFn` once the framework and gang actions expose it.

### Impacted Plugins

Plugins that today register `PreemptableFn` and/or `ReclaimableFn` should add `UnifiedEvictableFn` when they need to run on `gangpreempt` / `gangreclaim`. The four essential plugins below are expected to ship `UnifiedEvictableFn` together with the first framework and gang-action wiring.

Essential Plugins:

* `gang` enforces minAvailable and job-level victim rules.
* `conformance` keeps system and critical pods out of the victim set on gang paths.
* `capacity` applies guarantee and deserved constraints.
* `priority` preserves job- and task-priority ordering for victims.

Later:
`pdb`, `proportion`, `drf`, `tdm`, `cdp`, and `extender` can follow in subsequent work once the essential set and framework are stable.

### Deprecation of legacy `EvictableFn`

During the initial rollout, nothing deprecates `EvictableFn`. `preempt` and `reclaim` keep using `AddPreemptableFn` / `AddReclaimableFn` as today, while `UnifiedEvictableFn` is introduced only for gang paths.

After that, plugins need to do dual registration. Every victim-filter plugin should register both `EvictableFn` (for legacy actions until they switch) and `UnifiedEvictableFn`, and `EvictableFn` is marked deprecated.

The final step is to remove `EvictableFn`, route legacy `preempt` / `reclaim` through `UnifiedEvictableFn` (with `EvictionKind` set for task-level preempt/reclaim).

Within `UnifiedEvictableFn`, implementations should use `EvictionContext.Kind` to tell gang vs legacy preempt/reclaim apart and branch where behavior differs.