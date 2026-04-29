# Volcano Pod Status/Event Performance Optimization Design

## 1. Background and Problem Statement

In large-scale AI and big-data clusters, many Pods can remain in `Pending/Unschedulable` for long periods. The scheduler then updates Pod status and emits scheduling events frequently. The previous behavior had two major issues:

1. **Oversized status write payloads**
   - `UpdateStatus` sends the full Pod object.
   - With large Pod YAML definitions (for example, `100KB+`), serialization, network transfer, and etcd storage costs increase significantly.

2. **High-frequency repeated failure writes**
   - Identical or near-identical unschedulable outcomes are repeatedly written.
   - Under high pressure (many Pending Pods), this can create status-write and event-report storms.

## 2. Design Goals

This optimization targets the following goals:

- **Reduce per-write cost**: switch Pod status updates from full-object updates to `status` subresource patch.
- **Reduce repeated writes**: apply adaptive backoff to repeated `Unschedulable` status updates and events.
- **Keep critical signals timely**: still sync immediately for important state changes and required scenarios.
- **Provide user configurability**: expose thresholds and backoff intervals via CLI flags for cluster-specific tuning.
- **Control long-running memory growth**: clean up status-sync metadata by lifecycle to avoid unbounded cache growth.

## 3. Implementation

### 3.1 Switch Pod status updates to Patch

- The default status write path is changed from `UpdateStatus` to `Patch(..., "status")`.
- Patch payload includes only the `status` field to reduce request size.
- Applied in the default `StatusUpdater.UpdatePodStatus` path in scheduler cache.

### 3.2 Adaptive backoff strategy (Status/Event decoupled)

The optimization uses cluster `Pending` task count as the pressure indicator, with three levels: low, medium, and high pressure.

> Note: pressure thresholds are based on **Pending Pod count**, not node count.

- **Status backoff**: controls repeated `PodCondition` writes.
- **Event backoff**: independently controls `FailedScheduling` emission frequency (decoupled from status writes).

This reduces etcd write pressure while preserving higher-frequency observability signals.

### 3.3 Key decision rules

The following logic is executed in `taskUnschedulable`:

1. **Get pressure value by scenario**
   - In the `CloseSession -> RecordJobStatusEvent` path, `pendingTaskCount` is computed at job scope only when unschedulable updates are actually needed in this round, then reused within that job.
   - In the current implementation, calculation is triggered only when an `Allocated/Pending/Pipelined` bucket is non-empty.
   - In low-frequency failure paths such as `Bind/executePreBinds`, callers compute and pass `pendingTaskCount` on demand.

2. **Short-circuit repeated status**
   - If the new condition has no meaningful change from current status, skip status write by default.

3. **Prioritize critical changes**
   - `nominatedNodeName` changes are not throttled for status updates (to preserve autoscaler-related semantics).
   - Status tuple changes (such as `phase/reason`) are synced with higher priority.

4. **Independently rate-limit events**
   - Repeated events with the same `reason/message` are suppressed within interval windows.
   - Event emission can be immediate when `reason/message` changes.

### 3.4 Status-sync metadata and concurrency

- `podStatusSyncCache` is maintained in scheduler cache to track:
  - latest status-sync time and status tuple
  - latest event emission time and event content
- A dedicated lock protects this cache to avoid tight coupling with the primary scheduler-cache lock.
- Cache entries are cleaned on Pod deletion paths to prevent long-term memory growth.

## 4. User Configuration (CLI)

### 4.1 Status-related flags

- `--pod-status-low-pressure-threshold` (default `500`)
- `--pod-status-high-pressure-threshold` (default `2000`)
- `--pod-status-low-pressure-interval` (default `0s`)
- `--pod-status-mid-pressure-interval` (default `120s`)
- `--pod-status-high-pressure-interval` (default `300s`)

Threshold semantics:
- `pod-status-*-pressure-threshold` values represent Pending Pod count thresholds, not node-count thresholds.
- Example: `--pod-status-low-pressure-threshold=500` means low pressure when Pending Pod count is below `500`.

### 4.2 Event-related flags

- `--pod-event-low-pressure-interval` (default `0s`)
- `--pod-event-mid-pressure-interval` (default `60s`)
- `--pod-event-high-pressure-interval` (default `120s`)

### 4.3 Validation rules

The scheduler validates the following constraints at startup (invalid values fail fast):

- `lowPressureThreshold >= 0`
- `highPressureThreshold > 0`
- `lowPressureThreshold <= highPressureThreshold`
- `status low/mid interval >= 0`
- `status high interval > 0`
- `event low/mid interval >= 0`
- `event high interval > 0`

## 5. Recommended Tuning Approach

For different cluster sizes, use these guidelines:

- **Small clusters (low Pending count)**: keep defaults or reduce mid/high intervals for better status freshness.
- **Large high-pressure clusters**: increase status mid/high intervals first to suppress write storms.
- **Troubleshooting-first observability**: decrease event intervals for more timely signals.
- **Control-plane stability first**: increase high-pressure event interval to further reduce alert/event storms.

## 6. Upgrade and Compatibility Notes

- Default values are built in. **Without new flags, the feature remains usable**, while behavior changes from high-frequency full updates to patch + adaptive backoff.
- All parameters are CLI-overridable and support progressive rollout without code changes.
- If freshness is more important, reduce intervals; if control-plane stability is more important, increase mid/high intervals.
- To temporarily approximate legacy behavior, reduce status/event intervals close to `0s`.

## 7. Parameter Templates

### 7.1 Small/medium clusters (freshness-oriented)

```bash
--pod-status-low-pressure-threshold=200 \
--pod-status-high-pressure-threshold=800 \
--pod-status-low-pressure-interval=0s \
--pod-status-mid-pressure-interval=30s \
--pod-status-high-pressure-interval=60s \
--pod-event-low-pressure-interval=0s \
--pod-event-mid-pressure-interval=20s \
--pod-event-high-pressure-interval=40s
```

### 7.2 Large high-pressure clusters (stability-oriented)

```bash
--pod-status-low-pressure-threshold=500 \
--pod-status-high-pressure-threshold=2000 \
--pod-status-low-pressure-interval=0s \
--pod-status-mid-pressure-interval=120s \
--pod-status-high-pressure-interval=300s \
--pod-event-low-pressure-interval=0s \
--pod-event-mid-pressure-interval=60s \
--pod-event-high-pressure-interval=120s
```

## 8. Key Code Paths

- `pkg/scheduler/cache/cache.go`
  - status patch write path
  - adaptive status/event backoff logic
  - parameter application and decision functions
- `pkg/scheduler/cache/event_handlers.go`
  - status-sync metadata cleanup on Pod deletion
- `cmd/scheduler/app/options/options.go`
  - CLI flag exposure and validation rules
- `cmd/scheduler/app/options/options_test.go`
  - default-value and validation test coverage
