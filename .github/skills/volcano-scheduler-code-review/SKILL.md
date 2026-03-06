---
name: volcano-scheduler-code-review
description: Volcano scheduler code-review checklist and workflow for pull requests that touch scheduler, queue fairness, gang scheduling, preemption, reclaim, cache/session, or scheduling plugins. Use this when reviewing or implementing scheduler-related changes.
license: Apache-2.0
---

# Volcano Scheduler Code Review Skill

Use this skill when a task involves reviewing, changing, or validating scheduler behavior in `volcano-sh/volcano`, especially under `pkg/scheduler/**`.

## Goals

- Preserve scheduling correctness and policy invariants.
- Catch fairness, starvation, preemption, and reclamation regressions early.
- Keep scheduler changes observable, testable, and maintainable.

## Architecture context to apply in review

- Volcano scheduling is action-pipeline based (`enqueue`, `allocate`, `preempt`, `reclaim`, `backfill`, `shuffle`).
- Scheduling decisions are session/cache-driven; stale or inconsistent state can cause incorrect placement.
- `PodGroup` is the gang-scheduling unit (`minMember` matters).
- `Queue` enforces resource partitioning and fairness (including hierarchical cases).
- Plugin hooks influence predicates, ordering, and scoring; small changes can have cluster-wide effects.

## Review contract

For every scheduler-affecting PR, verify:

1. **Behavior contract**
   - What changed in scheduling semantics (before vs after)?
   - Is the policy intent explicit in PR description/commit message?

2. **Safety contract**
   - Is the change concurrency-safe (shared structures, snapshots, goroutines)?
   - Are retries/idempotency preserved for bind/evict/update paths?

3. **Operability contract**
   - Are user-visible reasons/events/logs present for new reject/failure paths?
   - Can operators diagnose why a workload is pending/preempted/reclaimed?

## Required checks by scheduling stage

### 1) Enqueue / admission

- Check queue-open/closed state handling and admission gates.
- Validate no accidental starvation introduced by new ordering logic.
- Confirm hierarchical queue constraints still hold.

### 2) Allocate / feasibility

- Ensure gang constraints are enforced: partial placement must not violate `PodGroup.minMember` semantics.
- Validate resource accounting before and after assignment.
- Confirm node feasibility checks remain deterministic and side-effect free.

### 3) Score / rank / plugin weighting

- Verify score ranges and weight combinations are intentional.
- Check tie-breaking behavior for determinism.
- Require rationale/examples when weights or scoring formulas change.

### 4) Preempt / reclaim

- Validate victim selection policy (priority/fairness) is consistent and justified.
- Ensure reclaim only happens under the intended queue overuse conditions.
- Check no eviction loops or oscillations are introduced.

### 5) Bind / commit / status update

- Confirm state transitions are ordered safely (API success vs local cache updates).
- Verify failures requeue appropriately without duplication or leaks.
- Ensure events/conditions reflect final outcome.

## Edge cases to explicitly test/reason about

- Empty cluster or zero feasible nodes.
- Large queue contention with mixed priorities (starvation risk).
- PodGroup where `minMember` cannot be met.
- Partial API failures during bind/evict/update.
- Concurrent scheduling cycles touching same queue/job.

## Test expectations

When behavior changes, request or add:

- **Unit tests** near changed scheduler code path (happy path + at least one boundary case).
- **Integration/e2e coverage** for cross-component behavior changes (queue fairness, gang admission, preemption/reclaim interactions).

Minimum test bar for non-trivial scheduler logic changes:

- 1 regression test for the new/changed policy.
- 1 failure-path or edge-case test.

## PR quality expectations (Volcano contribution alignment)

- Commits should follow Volcano conventions (clear subsystem + what/why).
- Changes should be split into logical, reviewable units.
- Reviewer-facing explanation should include why this policy change is safe.

## Suggested review comment templates

### Blocking

- “This changes queue ordering but I don’t see a regression test proving no starvation under sustained high-priority arrivals.”
- “The bind failure path appears to mutate local state before API confirmation; this can desynchronize cache vs cluster state.”
- “Scoring/weights changed without a before/after ranking example. Please add rationale and a test that locks intended ordering.”

### Non-blocking

- “Consider emitting an explicit event/condition for this reject path to improve operator debuggability.”
- “This code is on a hot scheduling path; can we avoid repeated allocation/lookup in this loop?”

## Implementation guidance when asked to modify code

When implementing scheduler changes:

1. Start by mapping current flow in touched actions/plugins.
2. Add/adjust tests first for intended behavior and one edge case.
3. Implement smallest change preserving session/cache invariants.
4. Run targeted unit tests for touched packages.
5. Confirm logs/events are actionable.

## Out of scope

- Do not broaden policy semantics beyond the PR intent without explicit request.
- Do not refactor unrelated scheduler modules in the same change unless required for correctness.
