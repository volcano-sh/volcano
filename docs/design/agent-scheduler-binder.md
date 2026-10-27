# Agent Scheduler Binder visibility and capacity

## Conflict-avoidance hint lifecycle

`NodesInBinder` records pending-node hints, not resource reservations.
`SelectBestNodes` downgrades these nodes only when the number of scored nodes
exceeds the requested candidate count, and falls back to pending nodes if there
are too few alternatives. A pending hint never prohibits selecting a node.

An allocation result contains one Pod and up to `K` candidate nodes. The worker
registers those candidates before sending the result to `BindCheckChannel`.
The single conflict-checking Binder then:

1. Selects a candidate using the existing consumed-generation check.
2. Calls `AddBindTask`, which checks current node resources and assumes the Pod.
3. Records the generation consumed by this result in `nodeBindRecords`.
4. Releases the result's candidate-hint references.

Previously the references were released before conflict checking and assume.
A concurrent worker could miss the pending-node downgrade hint while its node
snapshot still reflected the pre-assume state. That can produce extra generation
conflicts and requeues. The existing generation and resource checks still reject
stale or unfit results; this window has not been shown to cause an incorrect
binding or oversubscription. Extra retries can recur, but their cost depends on
the workload and queueing behavior.

The new ordering retains the heuristic hint through assume and generation
recording. It does not make snapshots atomic, guarantee zero conflicts, or
establish a new binding-safety guarantee. A conflict or synchronous assume error
releases this result's references before requeueing. Each result releases its own
references exactly once; other results' references to the same node remain.

On a synchronous assume error, the Binder also restores the task and Pod node
names to their pre-attempt values before copying the Pod into the retry queue.
For an unscheduled Pod these are empty. Real-cache tests cover a node removed
after the worker snapshot and resources consumed by an informer-observed Pod.
This rollback does not change the asynchronous pre-bind/API-failure protocol.

## Capacity contract and derivation

`--binder-channel-capacity=C` sets the number of **Pod scheduling results** that
may wait in `BindCheckChannel`, not the number of candidate nodes. The accepted
range is a non-negative integer:

- `C > 0`: a send blocks when all `C` slots are occupied, until a consumer frees
  a slot.
- `C = 0`: the channel is unbuffered; a send waits for a receiving Binder. This
  removes the queued backlog but does not eliminate worker-held results.

Assume the current execution model: `W` synchronous worker loops, one
conflict-checking Binder consumer, and at most `K` candidates in each result.
Between registering and releasing candidates, results can be in three places:

| Location | Maximum results retaining candidate references |
| --- | ---: |
| Channel buffer | `C` |
| Workers that registered a result but have not completed its send | `W` |
| Binder currently checking or assuming a result | `1` |

Therefore the maximum pending results and candidate references are:

```text
pending results     <= C + W + 1
candidate references <= K * (C + W + 1)
```

The bound is preserved by the result lifecycle, not by the release ordering
alone. Initially there are no registered results. Registration adds at most
`K` references to a worker-held result, and a synchronous worker cannot register
another result until its current send completes. Enqueue/dequeue transfers that
result between the worker, queue and consumer categories without registering it
again. The channel contains at most `C` results and the consumer handles one at
a time. Successful processing, conflict and assume failure each release the
result's own references exactly once before that result leaves these categories
or is requeued. Thus completed results cannot leave references accumulating
outside the `C + W + 1` population.

Holding references through assume and generation recording is a separate
heuristic-visibility requirement. Earlier release can still satisfy the upper
bound while losing the downgrade hint and increasing conflicting selections.
Conversely, leaked references can accumulate after their results leave the
pipeline and invalidate the population bound. The generation/resource checks,
hint lifetime, and exact-once reference accounting serve different purposes.

Here candidate references mean the sum of counts in `NodesInBinder`, not every
NodeInfo pointer held by snapshots. References are counted with multiplicity:
several results may refer to the same node. `K` is a bound on the **actual** candidate count, including a configured
`candidateNodeCount`, rather than a copy of the default value 3.

If an operator has a maximum pending-result budget `P`, a sufficient condition
under these assumptions is `C <= P - W - 1`. For a candidate-reference budget
`R` and `K > 0`, it is `C <= floor(R / K) - W - 1`. A negative right-hand side
means that a channel-capacity setting alone cannot guarantee that budget: even
an unbuffered channel permits worker-held and consumer-held results.

These inequalities describe bounded state. They do not determine a unique
capacity or bound waiting time. They also do not guarantee an unused candidate:
eligible node sets may overlap, and candidate selection can fall back to nodes
already present in `NodesInBinder`. The subsequent `BindFlowChannel`, pre-bind
work, and API binding are outside this result-buffer budget.

`AddBindTask` currently holds the cache mutex while sending to `BindFlowChannel`.
If that channel is full, the send can hold up cache operations such as
`UpdateSnapshot`. However, `BindTask` starts pre-bind/API work asynchronously, so
slow API calls do not necessarily fill the channel: downstream goroutines can
accumulate instead. This capacity setting does not bound that work or change
the downstream locking/consumption protocol.

In particular, neither a second buffer nor two rounds of results per candidate
are required by this control flow. The previous proposed `W * 3 * 2` formula
and `[10, 100]` clamp did not follow from these constraints. Adding a test that
repeats that arithmetic does not establish the missing policy.

## Default and configuration

The default remains **5000**, the pre-existing channel capacity. Compatibility
here means only retaining that default number of queue slots for deployments
that do not configure the option. It does not guarantee unchanged throughput,
latency or conflict rates, and it does not imply that 5000 is optimal.
Different worker counts and eligible-node pools may require explicit tuning.
The pending-hint lifetime change applies at every supported capacity. There is no worker multiplier, candidate multiplier,
or implicit clamp on an explicitly supplied capacity.

The CLI and the programmatic Binder, cache and scheduler constructors validate
negative capacities. Constructors return errors rather than allowing a negative
value to reach `make(chan, ...)`; cache/scheduler validation precedes client,
workqueue and file-watcher setup. A shared validation rule lives in the
lightweight `pkg/agentscheduler/config` package, below the CLI and runtime
packages. Cache initialization captures the validated capacity, then uses the
private allocation path; it does not contain an unreachable late validation
failure/cleanup branch.

For the binary, set `--binder-channel-capacity=<non-negative integer>`. For
Helm, set `custom.agent_scheduler_binder_channel_capacity` to the same value.
For example, `--set custom.agent_scheduler_binder_channel_capacity=0` selects
an unbuffered handoff. It is a valid boundary setting, not a general performance
recommendation.

Choose and validate a capacity against a workload's budget, worker concurrency,
eligible node sets, and Binder service rate. Evaluate successful bindings per
second and scheduling latency as well as generation conflicts. Fewer conflicts
alone do not establish better throughput.

## Evidence and validation boundary

The experiments below are separate datasets tied to different commits. They
must not be combined or relabelled as measurements of a later revision.

### Dataset A: superseded automatic formula (`b887171b6`)

Experiments on the earlier PR head `b887171b67a936b8473a6ed7b2c0b0e26179f4f0`
compared the original Binder, visibility-only correction with capacity 5000,
and the proposed automatic capacity. The [per-run data](agent-scheduler-binder-results.csv)
includes the configured capacities, conflict counts, successful bindings and
elapsed times for all 84 stable runs. `baseline` uses the Binder ordering from
`0854d3a4018c9304f8a25ded7a6f136c539f753c`; `fixed` is the experiment label for post-assume hint release.
In a 200-node scheduling workload, the
automatic capacity improved throughput at 6 and 12 workers but reduced it by
25.6% at 4 workers. In a separate 20-eligible-node capacity sweep at 6 workers,
capacities 10, 36, and 100 produced median rates of 431.1, 309.0, and 271.8
bindings/s, respectively. That sweep did not include an original-Binder
baseline, so it establishes capacity sensitivity rather than a before/after
comparison of the entire PR.

These were three-run, pre-created bursts of 2000 Pods through a Kind v1.36.1
API server and virtual Node objects without kubelets. Both Binder variants had
the same diagnostic instrumentation, used Go 1.27.1, and ran with
`GOMAXPROCS=8` on the same eight CPU affinities of a shared host. The small pool
restricted eligibility to 20 nodes while retaining 200 virtual Node objects in
the scheduler cache. A separate 100-Pod smoke test reached Running on real Kind
workers. Preliminary runs with CNI restarts were excluded and rerun after
stabilizing the test infrastructure. These results are not production throughput
guarantees or measurements of a newly rebased commit.

The dataset motivated rejecting the automatic default. It does not establish
performance of the later explicit-capacity implementation or supply a universal
replacement constant.

### Dataset B: explicit capacity (`f27b585a3`)

Commit `f27b585a3cb63959ad9442d848641ea0f404cf9b` removed the automatic formula,
retained default capacity 5000 and exposed the explicit CLI option. The
[per-run explicit-capacity data](agent-scheduler-binder-explicit-results.csv)
contains 18 pressure runs: 2000 pre-created Pods per run, three repeats per
configuration, and 36,000 successful API bindings. Its original-ordering
baseline uses `77236588da03eefcb18b5744d60f7a305b6949ef` on the same code tree.
Capacity came from the production CLI, without a diagnostic capacity override.

| Eligible nodes / workers | Original ordering, C=5000 | Explicit implementation default, C=5000 | Explicit tuning |
| --- | ---: | ---: | ---: |
| 200 / 4 | 1387.5 bindings/s | 1320.2 bindings/s | C=100: 1403.3 bindings/s |
| 20 / 6 | 275.4 bindings/s | 276.9 bindings/s | C=10: 431.7 bindings/s |

Values are medians. The 200-node default comparison changed by -4.9%; individual
ranges were 1310.4–1410.7/s and 1283.9–1416.4/s. The overlapping ranges do not
prove performance equivalence. The 20-node default median changed by +0.6%.
These observations support keeping default-value compatibility distinct from
performance guarantees and show why workload-specific tuning may be needed.

This dataset used a separate stable Kind v1.36.1 cluster, Go 1.27.1,
`GOMAXPROCS=8`, the same eight shared-host CPU affinities and identical diagnostic
instrumentation on both variants. It retained 200 virtual Node objects, with
eligibility restricted to 20 in the smaller pool. A separate 20-Pod smoke test
with CLI capacity 0 reached Running on real Kind workers. All pressure runs
cross-checked counters, watch events and final API assignments; CNI restart
counts remained zero. These are burst scheduling/API-binding results, not a
200-node container-runtime E2E or production throughput guarantee.

### Behavioral coverage of the current revision

The measured datasets above remain attributable to their named commits. Later
constructor validation and test hardening do not turn those measurements into
current-head performance results. Automatic capacity selection remains
follow-up work, and this PR does not close the general scalability issue.

Regression coverage checks the intermediate pending state before assume,
success/conflict/error cleanup, exact configured capacity including zero,
blocking and FIFO handoff, and programmatic rejection of negative capacities.
The saturated-state test crosses `W` in `{1, 4}`, `C` in `{0, 1, W+1}`, and
`K` in `{1, 3, 5}`. These represent serial/concurrent workers, no buffer/one
slot/a queue larger than the worker pool, and single/default/custom candidate
counts. Every combination checks total references at saturation against
`K * (C + W + 1)`, then checks that completing the paused consumer removes
exactly `K` references while the others remain visible.

The paused-assume test isolates a worker snapshot during one Binder operation;
its constructor argument of 1 is channel capacity, not a worker count. The
saturation matrix provides the concurrent sender coverage. Concurrent allocation
tests separately cross serial, multi-worker and stress profiles with unbuffered,
single-slot and whole-workload buffers, checking progress and the final result
count while a consumer drains the channel.
