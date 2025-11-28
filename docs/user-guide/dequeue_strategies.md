# Dequeue Strategies

Volcano scheduler supports different strategies for dequeuing jobs from queues when resources are limited. This document describes the available dequeue strategies and how to configure them.

## Overview

Dequeue strategies control the **blocking behavior** when a job fails to allocate resources:
- **FIFO**: Blocks the queue when a job fails to allocate, preventing other jobs in the queue from being scheduled in the current cycle
- **Traverse**: Continues processing other jobs in the queue even when one job fails to allocate

**Note**: Job sorting (by creation time or priority) is controlled by scheduler plugins (e.g., priority plugin) and is independent of the dequeue strategy.

## Available Strategies

### 1. FIFO Strategy (`fifo`)

**Behavior**: 
- When a job fails to allocate resources (`stmt == nil`), the **entire queue is blocked** for the current scheduling cycle
- The scheduler will not attempt to schedule any other jobs from this queue until:
  - The failed job is removed or modified
  - A new scheduling cycle begins, which will retry the failed job
  - Resources become available for the failed job

**Use Case**: When you want strict ordering and can tolerate temporary resource underutilization. This ensures that jobs are processed in order, and if one job cannot be scheduled, subsequent jobs in the queue must wait.

**Characteristics**:
- Blocks the entire queue when any job fails to schedule
- Ensures strict job ordering (order depends on job sorting plugins)
- May lead to resource underutilization if blocked jobs require more resources than currently available

### 2. Traverse Strategy (`traverse`) - Default

**Behavior**:
- When a job fails to allocate resources (`stmt == nil`), the queue is **not blocked** and continues processing
- The scheduler continues to attempt scheduling other jobs in the queue
- Failed jobs remain in the queue and may be retried in subsequent scheduling cycles

**Use Case**: When you want to maximize resource utilization by allowing jobs to be scheduled even when other jobs in the queue are waiting for resources.

**Characteristics**:
- Maximizes cluster resource utilization
- Allows jobs to be scheduled even when other jobs in the queue are blocked
- Continues queue processing regardless of individual job failures
- May lead to starvation of large jobs if many small jobs keep arriving
- Provides better resource utilization in heterogeneous workload environments

## Configuration

### Using Queue Spec

Configure the dequeue strategy by setting the `dequeueStrategy` field in the Queue spec:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: my-queue
spec:
  weight: 1
  dequeueStrategy: "traverse"  # or "fifo"
```

### Available Values

- `fifo`: Block queue when a job fails to allocate resources
- `traverse`: Continue processing other jobs when a job fails to allocate resources (default)

If no `dequeueStrategy` is specified or an invalid value is provided, the system defaults to `traverse` strategy.

## Examples

### Example 1: FIFO Strategy for Strict Ordering

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: strict-order-queue
spec:
  weight: 100
  dequeueStrategy: "fifo"
  capability:
    cpu: "1000"
    memory: "1000Gi"
```

This configuration ensures that if a job cannot be scheduled, the entire queue is blocked until that job can be scheduled or is removed.

### Example 2: Traverse Strategy for Maximum Utilization

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: dev-queue
spec:
  weight: 10
  dequeueStrategy: "traverse"
  capability:
    cpu: "100"
    memory: "200Gi"
```

This configuration maximizes resource utilization by continuing to process jobs even when some fail to allocate.

### Example 3: Default Strategy (Traverse)

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: default-queue
spec:
  weight: 50
  capability:
    cpu: "500"
    memory: "500Gi"
```

If `dequeueStrategy` is not specified, the queue defaults to `traverse` strategy.

## Job Sorting

**Important**: The dequeue strategy only controls blocking behavior, not job sorting. Job sorting is determined by scheduler plugins:

- **By Priority**: If the priority plugin is enabled, jobs are sorted by priority (higher priority first)
- **By Creation Time**: If no priority plugin is enabled, jobs are sorted by creation timestamp (older jobs first)

The dequeue strategy works independently of job sorting:
- A queue with `fifo` strategy will block on failure regardless of whether jobs are sorted by priority or creation time
- A queue with `traverse` strategy will continue processing regardless of sorting method

## Monitoring and Troubleshooting

### Checking Queue Configuration

You can verify the dequeue strategy of a queue using kubectl:

```bash
kubectl get queue my-queue -o yaml
```

Look for the `dequeueStrategy` field in the `spec` section.

### Logs

The scheduler logs will indicate which strategy is being used for each queue:

```
Try to allocate resource to Jobs in Queue <my-queue> with strategy <traverse>
```

#### FIFO Strategy Logs
When using FIFO strategy, you may see logs indicating queue blocking:
```
Try to allocate resource to Jobs in Queue <strict-order-queue> with strategy <fifo>
Try to allocate resource to 1 tasks of Job <namespace/job-name>
# No further logs for this queue until job succeeds or is removed
```

#### Traverse Strategy Logs  
When using traverse strategy, you'll see continuous queue processing:
```
Try to allocate resource to Jobs in Queue <dev-queue> with strategy <traverse>
Try to allocate resource to 1 tasks of Job <namespace/job1>
Try to allocate resource to Jobs in Queue <dev-queue> with strategy <traverse>
Try to allocate resource to 1 tasks of Job <namespace/job2>
```

### Common Issues

1. **Queue blocked in FIFO strategy**: If a job cannot be scheduled due to insufficient resources, the entire queue becomes blocked. Check resource requirements and availability. Consider:
   - Increasing cluster resources
   - Switching to `traverse` strategy for better utilization
   - Reviewing job resource requests for optimization

2. **Large jobs never scheduled in traverse queue**: When using `traverse`, smaller jobs may continuously consume available resources, causing starvation of large jobs. Consider:
   - Using `fifo` strategy to ensure strict ordering
   - Implementing resource quotas or limits
   - Adjusting queue weights to balance resource allocation

3. **Invalid strategy value**: If an invalid `dequeueStrategy` value is provided, the system defaults to `traverse` strategy. Check the spec field for correct values (`fifo` or `traverse`).

4. **Queue not processing jobs**: Verify that:
   - The queue has the correct `dequeueStrategy` in the spec
   - Jobs are in the correct queue
   - Cluster has sufficient resources for at least some jobs
   - Queue is not overused (check `Overused` condition)

## Best Practices

1. **Use FIFO Strategy** (`fifo`) for queues where:
   - Strict job ordering is critical
   - You can tolerate temporary resource underutilization
   - You want to ensure that if one job cannot be scheduled, subsequent jobs must wait
   - Preventing resource fragmentation is important

2. **Use Traverse Strategy** (`traverse`) for queues where:
   - Maximum resource utilization is the priority
   - Jobs have varying resource requirements
   - Development or testing environments with mixed workloads
   - You want to avoid blocking the entire queue due to single job failures

3. **Monitor queue behavior** regularly to ensure the chosen strategy meets your requirements:
   - Check job completion times and queue throughput
   - Monitor resource utilization patterns
   - Watch for signs of job starvation or queue blocking
   - Review scheduler logs to verify strategy is being applied correctly

4. **Consider queue weights** in combination with dequeue strategies to achieve desired resource allocation across multiple queues.

5. **Resource planning**: Design your cluster and job resource requests considering the dequeue strategy:
   - For FIFO queues, ensure adequate resources for the largest expected jobs
   - For Traverse queues, consider the impact of resource fragmentation
   - Balance between strict ordering and resource utilization

6. **Job priority configuration**: When using priority-based job sorting (via priority plugin), ensure jobs have appropriate priority values set to achieve desired scheduling behavior.

## Implementation Details

### Architecture

- Dequeue strategy is configured per queue through the `dequeueStrategy` field in the Queue spec
- Strategy selection happens during queue processing in the allocate action
- All strategies use the same unified scheduling loop with conditional logic
- Job sorting is independent of dequeue strategy and is controlled by scheduler plugins

### Blocking Behavior Logic

When a job fails to allocate resources (`stmt == nil`), the queue blocking behavior depends on the strategy:

```go
// In allocateResources function after job allocation attempt
if queue.DequeueStrategy == api.DequeueStrategyFIFO && stmt == nil {
    continue  // Don't add queue back - blocks further processing
}
// For traverse strategy or successful allocation, add queue back
queues.Push(queue)
```

### Strategy Constants

The available strategies are defined as constants in `pkg/scheduler/api/queue_info.go`:

```go
const (
    DequeueStrategyFIFO     = scheduling.DequeueStrategyFIFO     // "fifo"
    DequeueStrategyTraverse  = scheduling.DequeueStrategyTraverse // "traverse"
    DefaultDequeueStrategy   = scheduling.DefaultDequeueStrategy // "traverse"
)
```

These constants map to the string values defined in the scheduling API:
- `DequeueStrategyFIFO` = `"fifo"`
- `DequeueStrategyTraverse` = `"traverse"`
- `DefaultDequeueStrategy` = `"traverse"`

### Default Behavior

- If no `dequeueStrategy` is specified in the Queue spec, the system uses `traverse` (default)
- If an invalid strategy value is provided, the system falls back to `traverse` and uses the default behavior

### Job Sorting Implementation

Job sorting is handled by the scheduler's `JobOrderFn`, which is composed of order functions from enabled plugins:

- If priority plugin is enabled, jobs are sorted by priority (higher priority first)
- If no priority plugin is enabled, jobs are sorted by creation timestamp (older jobs first)

The dequeue strategy does not affect job sorting - it only controls whether the queue continues processing when a job fails to allocate resources.
