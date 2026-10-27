# Agent Scheduler Binder visibility and capacity

## Correctness contract

An allocation result contains one Pod and up to `K` candidate nodes. The worker
registers those candidates in `NodesInBinder` before sending the result to
`BindCheckChannel`. The single conflict-checking Binder then:

1. Selects a candidate whose snapshot generation has not already been consumed.
2. Calls `AddBindTask` to assume the Pod and advance the cache generation.
3. Records the generation consumed by this result in `nodeBindRecords`.
4. Releases all candidate references for this result.

Releasing candidates before step 2 exposes a window in which a worker snapshot
can treat a node as neither pending nor assumed. On success, candidate release
must follow both the cache assume and the Binder generation record. A conflict
or an `AddBindTask` error instead releases the references before requeueing.
Each result must release its own references exactly once; references held by
other results on the same node must remain visible.

This ordering protects the transition. It does not make worker snapshots atomic
with the Binder, nor does it guarantee zero generation conflicts. The existing
generation check remains necessary.

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

References are counted with multiplicity: several results may refer to the same
node. `K` is a bound on the **actual** candidate count, including a configured
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

In particular, neither a second buffer nor two rounds of results per candidate
are required by this control flow. The previous proposed `W * 3 * 2` formula
and `[10, 100]` clamp did not follow from these constraints. Adding a test that
repeats that arithmetic does not establish the missing policy.

## Default and configuration

The default remains **5000**, the pre-existing channel capacity. This preserves
the queue-size behavior of deployments that do not configure the new option;
it is not a claim that 5000 is optimal. The visibility correction applies at
every supported capacity. There is no worker multiplier, candidate multiplier,
or implicit clamp on an explicitly supplied capacity.

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

Experiments on the earlier PR head `b887171b67a936b8473a6ed7b2c0b0e26179f4f0`
compared the original Binder, visibility-only correction with capacity 5000,
and the proposed automatic capacity. The [per-run data](agent-scheduler-binder-results.csv)
includes the configured capacities, conflict counts, successful bindings and
elapsed times for all 84 stable runs. `baseline` uses the Binder ordering from
`0854d3a4018c9304f8a25ded7a6f136c539f753c`; `fixed` uses the corrected ordering.
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

The experiments reject a universal tuning claim for the proposed formula;
they do not supply a replacement universal constant. This change exposes the
capacity budget explicitly while retaining the existing default. Automatic
capacity selection remains follow-up performance work.

Regression coverage checks the intermediate pending state before assume,
success/conflict/error cleanup, exact configured capacity including zero,
blocking and FIFO handoff, and candidate visibility while workers are blocked.
The saturated-state test varies the capacity and candidate count independently
and observes the `K * (C + W + 1)` bound. Concurrent allocation tests drain the
result channel while workers run, including an unbuffered channel, so the test
consumer does not introduce a producer/consumer deadlock.
