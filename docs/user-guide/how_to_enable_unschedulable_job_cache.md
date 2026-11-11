# How to Enable the Unschedulable Job Cache

The unschedulable Job cache avoids repeating the same scheduling checks for a Job whose scheduling conditions have not changed. It is an Alpha scheduler feature and is disabled by default.

## Enable the Feature

When installing Volcano with Helm, enable the scheduler feature gate:

```bash
helm install volcano volcano/volcano --namespace volcano-system --create-namespace \
  --set custom.scheduler_feature_gates="UnschedulableJobCache=true"
```

For an existing deployment, add the feature gate to the `volcano-scheduler` arguments and restart the scheduler:

```yaml
--feature-gates=UnschedulableJobCache=true
```

No Job, PodGroup, or Queue configuration is required. When the gate is disabled, Jobs continue to be evaluated in every scheduling session as before.

## Expected Behavior

After a Job is rejected, Volcano caches the scheduling result only when every rejecting plugin provides a safe wake-up hint. Later sessions skip the checks covered by that result until a relevant cluster event arrives. For example, a Pod deletion can wake a resource-blocked Job, a Node label update can wake a Job blocked by node affinity, and a Queue update can wake a Job blocked by queue capacity.

Wake-up only requests a normal scheduling retry. All predicates and quota checks run again before Volcano allocates any task. A background watchdog also retries cached Jobs that receive no matching event. The default maximum skip duration is five minutes and can be changed with:

```yaml
--unschedulable-job-cache-max-skip-duration=5m
```

The cache is in memory. Restarting the scheduler clears it, and pending Jobs are evaluated normally after restart.

## Plugin Coverage and Current Constraints

The Alpha implementation supports the upstream QueueingHint providers wrapped by the `predicates` plugin, Volcano's built-in Resource Fit check, `proportion`, and non-hierarchical `capacity`.

Capacity rejections are not cached when `enableHierarchy: true`. A Job can be rejected by an ancestor Queue while a sibling Queue later releases that ancestor's resources. Precisely matching those events requires both the Queue that caused the rejection and the releasing Queue's ancestor path. Until that information is available, Volcano evaluates hierarchical Capacity rejections in every scheduling session.

If any rejecting plugin has no HintProvider, Volcano does not cache the Job. This preserves the existing scheduling behavior at the cost of forgoing the optimization for that Job.

Resource Fit handles both Node allocatable changes and the legacy `volcano.sh/oversubscription-cpu` and `volcano.sh/oversubscription-memory` annotations. The current Volcano agent's default extended-resource oversubscription updates `Node.Status.Allocatable` and follows the normal allocatable-change path.

## Observability

Per-Job cache metrics are disabled by default because they include Job identity labels. Enable the scheduler metrics endpoint and these debug counters with Helm:

```bash
helm upgrade volcano volcano/volcano --namespace volcano-system \
  --set custom.scheduler_feature_gates="UnschedulableJobCache=true" \
  --set custom.scheduler_metrics_enable=true \
  --set custom.scheduler_unschedulable_job_cache_debug_metrics=true
```

The debug counters report cache skips, event wake-ups, and watchdog expirations. They are intended for short-term validation and troubleshooting.

For implementation details and performance results, see the [design document](../design/unschedulable-job-cache.md).
