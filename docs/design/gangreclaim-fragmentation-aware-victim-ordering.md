# Proposal: fragmentation-aware gang reclaim victim ordering

Status: draft for discussion. Related issue: [#5958](https://github.com/volcano-sh/volcano/issues/5958).

This change proposes an optional resource-layout tie-breaker for `gangreclaim`.
It preserves the existing bundle type, victim queue and ROI ordering. It only
changes the final choice between otherwise equivalent victim bundles.

This draft contains a baseline reproducer and an opt-in proof of concept. The
option defaults to false. The policy remains under discussion, and the heuristic
does not claim to find an optimal victim set.

## Reproduction on current master

Baseline: `0854d3a4018c9304f8a25ded7a6f136c539f753c`.

Two worker nodes each have an effective budget of 4 CPU. All existing workload
Pods request 1 CPU. There are two victim jobs in the same queue, each with
`minMember: 1` and three Running Pods, one of which is a protected anchor. Only the other
two Pods of each victim job are preemptable. The jobs therefore each have one
SAFE bundle containing two 1-CPU Pods.

| Node | Running Pods | Idle CPU |
| --- | --- | ---: |
| n1 | spread-1, spread-anchor, packed-anchor, protected-1 | 0 |
| n2 | spread-2, packed-1, packed-2, protected-2 | 0 |

The reclaimer is a separate PodGroup in another queue, with `minMember: 1` and
one Pending Pod requesting 2 CPU. The victim queue has 8 CPU allocated; the
reclaimer queue has 2 CPU deserved. Both queues are open and reclaimable.

The spread and packed SAFE bundles have equal resource totals, equal ROI and
the same queue. The existing stable job identifier tie-breaker determines which
bundle is considered first. Renaming the victim PodGroups controls that order
without changing their resources, placement, priorities or gang requirements.

| Victim queue deserved | First bundle | Actual evictions | Reclaimer result |
| ---: | --- | ---: | --- |
| 4 CPU | spread | 4 | Running on n2 |
| 4 CPU | packed | 2 | Running on n2 |
| 6 CPU | spread | 0 | Pending throughout the 40-second observation |
| 6 CPU | packed | 2 | Running on n2 |

These results were observed with the unchanged scheduler on a disposable Kind
v1.36.1 cluster with one control-plane node and two real worker nodes. Protected
padding Pods account for system requests and leave exactly 4 CPU per worker for
the fixture. All initial Pods became Running before starting the scheduler.
The source-built scheduler ran on the host against the isolated cluster's API.
Pod UID disappearance, Events and the reclaimer's actual phase/node were
recorded. This is a small CPU functional reproduction, not a throughput benchmark
or validation on accelerator hardware.

The action-level test reproduces the same four cases with the option off and
checks the corresponding enabled cases through the real scheduler cache/session
and gang/capacity plugins. Its cache clients and eviction sink are fake; it
supplements the Kind evidence. A failed-predicate case checks that resource fit
cannot override placement validation and that speculative evictions roll back.

With the proof-of-concept option enabled, all four Kind cases evicted only
`packed-1` and `packed-2`, and the reclaimer became Running on n2. The same
source-built binary with the option disabled retained the baseline results.
The tainted control-plane node remained present in all runs.

## Why the current order matters

The relevant baseline paths are:

- [Bundle ordering](https://github.com/volcano-sh/volcano/blob/0854d3a4018c9304f8a25ded7a6f136c539f753c/pkg/scheduler/actions/utils/bundle.go#L269-L291)
  compares bundle type, victim queue and aggregate ROI, then stable identifiers.
- [Candidate filtering and ordering](https://github.com/volcano-sh/volcano/blob/0854d3a4018c9304f8a25ded7a6f136c539f753c/pkg/scheduler/actions/gangreclaim/gangreclaim.go#L195-L282)
  passes that order to `UnifiedEvictable` before constructing a plan.
- [Capacity filtering](https://github.com/volcano-sh/volcano/blob/0854d3a4018c9304f8a25ded7a6f136c539f753c/pkg/scheduler/plugins/capacity/capacity.go#L614-L652)
  consumes each victim queue's reclaimable budget in candidate order.
- [Plan construction](https://github.com/volcano-sh/volcano/blob/0854d3a4018c9304f8a25ded7a6f136c539f753c/pkg/scheduler/actions/gangreclaim/gangreclaim.go#L145-L192)
  simulates cumulative prefixes of the allowed bundles.

With spread first, releasing 2 CPU yields only 1 CPU on each node. With a
4-CPU reclaim budget, the planner then includes the packed bundle too and
commits four evictions. With a 2-CPU budget, capacity filtering has already
excluded the packed bundle, so the planner cannot find a placement. Packed
first frees 2 CPU on n2 and permits placement with two legal evictions.

This demonstrates a policy limitation in the current aggregate ordering.
It does not establish that every fragmentation problem can be solved by a
different order, or that the current behavior violates an existing guarantee.

## Proposed first increment

The proof of concept adds `enableFragmentationAwareVictimOrder` to the
`gangreclaim` action, defaulting to `false`. When enabled, order by:

1. Existing SAFE/WHOLE bundle type.
2. Existing victim queue order.
3. Existing aggregate ROI.
4. Resource-layout score, descending.
5. Existing stable identifiers.

For each candidate bundle, calculate a resource-only score from the current
domain snapshot: how many pending target tasks gain at least one newly
resource-feasible node when that bundle is released. For each affected node,
compare the target's `InitResreq` against both `FutureIdle()` and `FutureIdle()`
plus the bundle's per-node released requests. Count a target once if it did not
fit before but fits after on at least one affected node. Compute immutable
scores before sorting; do not mutate the session or invoke stateful predicates
from the comparator.

Capacity on unaffected nodes is excluded. A domain-wide absolute fit count can
hide all layout differences when an unrelated node has spare resources but is
unusable because of taints or another predicate. This occurred in the first
Kind prototype with the tainted control-plane node. Measuring newly gained fits
on victim nodes avoids that masking and avoids a full domain scan per target.

The implementation indexes domain nodes once and caches `FutureIdle()` resources
only for nodes with candidate victims. For each bundle it clones the affected
nodes' cached resources. Scores are stored on temporary bundles; cache, session,
victim and target resources are not modified. Removing the configuration resets
the option to false on the next action execution.

In the fixture, the spread bundle scores zero and the packed bundle scores one.
The score is an ordering heuristic: it does not jointly place the gang and does
not account for affinity, volumes or device allocation. Existing predicate
simulation and `JobPipelined` validation remain the final admission checks.

Apply the tie-breaker before `UnifiedEvictable`, because its capacity filter is
order-sensitive. Recompute it after `ApplyAllowedTasks`, which may reduce a SAFE
bundle's approved task set. Continue honoring all existing eligibility,
guarantee, deserved, `allowWholeBundle` and gang-readiness checks.

Keep the first increment restricted to the current domain and current pending
task set. Do not change `gangpreempt`, target selection (#5946), partial SAFE
bundle trimming (#5995), queue fairness, or the relative order of different
bundle types, queues or ROI values.

The initial score may not distinguish improvements requiring several bundles,
and individual task fits can overestimate joint feasibility. Later policies can
address these cases after functional and scheduling-cost measurements.

## Validation before an implementation is ready

- Disabled mode preserves the existing order.
- The four reproduction cases prefer packed placement when the option is on.
- Type, queue and ROI precedence continue to dominate the layout score.
- Check CPU, memory, scalar resources and heterogeneous task requests.
- Partial filtering of SAFE bundles recalculates their scores.
- Failed predicate/placement simulation rolls back all operations.
- Comparator scores are stable, deterministic and do not mutate resources.
- Measure scoring cost with increasing node, bundle and pending-task counts;
  compare scheduler CPU, cycle duration, successful placements and eviction
  counts. Avoid a full predicate simulation for every comparison. Cache the
  resource-only inputs and evaluate affected nodes where possible.

## Proof-of-concept measurements

The scoring/sorting microbenchmark ran on darwin/arm64, Apple M2 Pro. Targets in
these cases fit neither the original nodes nor the spread bundles. Numbers
describe one scoring/sorting call, not an
entire scheduling cycle; `gangreclaim` scores both before and after filtering.

| Nodes / bundles / targets | Option off | Option on | Bytes per enabled call |
| --- | ---: | ---: | ---: |
| 100 / 8 / 1 | 0.53 microseconds | 4.78 microseconds | 5,032 |
| 1,000 / 32 / 16 | 2.77 microseconds | 65.39 microseconds | 62,160 |

These local microbenchmarks quantify added work, not an end-to-end throughput
improvement. Larger domains, many pending targets, heterogeneous requests and
production scheduling latency still need representative measurements before a
default policy change could be considered.

## Reproduce

Action-level baseline characterization:

```sh
go test -race ./pkg/scheduler/actions/gangreclaim \
  -run TestGangReclaimFragmentationReproduction -v -count=3 -timeout=120s
```

Kind reproduction requires Docker, Kind, kubectl, Go and Python 3. Use a new
disposable cluster named `volcano-5958`; the harness pins that context and checks
the node names. It creates and deletes its own test namespaces and queues.

```sh
mkdir -p /tmp/volcano-5958
cat > /tmp/volcano-5958/kind.yaml <<'EOF'
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
EOF
kind create cluster --name volcano-5958 --image kindest/node:v1.36.1 \
  --config /tmp/volcano-5958/kind.yaml \
  --kubeconfig /tmp/volcano-5958/kubeconfig --wait 120s
kubectl --kubeconfig /tmp/volcano-5958/kubeconfig apply --server-side \
  -f installer/helm/chart/volcano/crd/bases
kubectl --kubeconfig /tmp/volcano-5958/kubeconfig wait \
  --for=condition=Established crd --all --timeout=60s
go build -o /tmp/volcano-5958/vc-scheduler ./cmd/scheduler
python3 hack/reproduce-gangreclaim-fragmentation.py \
  --kubeconfig /tmp/volcano-5958/kubeconfig \
  --scheduler /tmp/volcano-5958/vc-scheduler \
  --evidence-dir /tmp/volcano-5958/disabled
python3 hack/reproduce-gangreclaim-fragmentation.py \
  --kubeconfig /tmp/volcano-5958/kubeconfig \
  --scheduler /tmp/volcano-5958/vc-scheduler \
  --evidence-dir /tmp/volcano-5958/enabled --enable-fragmentation-order
kind delete cluster --name volcano-5958
```

The harness writes per-case before/after Pod snapshots, Events, scheduler logs
and `summary.json`. Do not publish the kubeconfig with the evidence.

Run the scoring/sorting microbenchmark with:

```sh
go test ./pkg/scheduler/actions/gangreclaim -run '^$' \
  -bench BenchmarkBundleResourceFitScores -benchmem -benchtime=500ms
```

## Discussion requested

Is an opt-in tie-breaker after the existing ROI key an appropriate first
increment? This preserves the current policy precedence while improving the
demonstrated equal-policy cases. A broader policy that overrides ROI or adapts
to cumulative victim selections would need a separate agreement on semantics.
