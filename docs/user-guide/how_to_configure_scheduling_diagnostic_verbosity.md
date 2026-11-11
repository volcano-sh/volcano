# How to Configure Scheduling Diagnostic Verbosity

When a Pod or PodGroup cannot be scheduled, the scheduler records the reason in the Pod condition message, the PodGroup condition message, and the scheduling events. By default these messages are detailed: they include how many nodes were rejected, how many nodes failed for each reason, and the candidate node of a Pod that is waiting for its gang to be satisfied.

Those counts change whenever node resources change, even when a Job stays unschedulable for the same reasons. Each changed message is a new status update sent to the API server. In large clusters with many pending Pods, this adds avoidable API server load.

The `--scheduling-diagnostic-verbosity` scheduler flag lets you trade detail for stable messages.

## Modes

| Value | Behavior |
| --- | --- |
| `detailed` (default) | Messages are unchanged: node counts, per-reason counts, and candidate node names are included. |
| `summary` | Node counts, per-reason counts, and candidate node names are omitted. Each message lists only the distinct failure reasons, sorted. |

For example, the same unschedulable Pod produces:

```text
# detailed
0/3 nodes are unavailable: 1 Insufficient memory, 2 Insufficient cpu.

# summary
nodes are unavailable: Insufficient cpu, Insufficient memory.
```

In `summary` mode, a change that only affects the counts, such as a different number of nodes failing for the same reason, produces an identical message, so no status update is sent. A change in the set of failure reasons still changes the message and is still reported.

## Enable Summary Mode

Add the flag to the `volcano-scheduler` arguments and restart the scheduler:

```yaml
--scheduling-diagnostic-verbosity=summary
```

Any value other than `detailed` or `summary` prevents the scheduler from starting.

## What Is Kept and What Is Omitted

In `summary` mode, the following are kept: the distinct failure reasons, the task state counts and `minAvailable` in PodGroup messages, and the scheduling stage and gang information. Pod condition reasons and nominated node handling are unchanged.

The following are omitted: the `0/N` node count, the count in front of each reason, and the node name in messages such as `Pod ns/name can possibly be assigned to <node>`.

The HyperNode-level summary used for network topology scheduling is not changed by this flag and may still contain counts.

## Inspect the Omitted Details

The omitted details are written to the scheduler log at verbosity level 4. Run the scheduler with `-v=4` (with Helm, `--set custom.scheduler_log_level=4`) and search for this message:

```text
Scheduling diagnostics omitted from summary messages
```

Each entry contains the `task`, its scheduling `status`, the `candidateNode`, and the full per-node failure text in `nodeFitErrors`.