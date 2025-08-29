# Dequeue Strategies

Volcano scheduler supports different strategies for dequeuing jobs from queues when resources are limited. This document describes the available dequeue strategies and how to configure them.

## Overview

Dequeue strategies control two aspects of job scheduling:
1. **Job Sorting**: How jobs within a queue are ordered (by creation time or by priority)
2. **Blocking Behavior**: What happens when a job fails to allocate resources (FIFO blocks the queue, Traverse skips and continues)

## Available Strategies

### 1. Creation Time Based FIFO (`creationtime-based-fifo`)

**Behavior**: 
- Jobs are sorted by **creation time** (first-in-first-out)
- When a job fails to allocate resources (`stmt == nil`), the **entire queue is blocked** for the current scheduling cycle
- The scheduler will not attempt to schedule any other jobs from this queue until:
  - The failed job is removed or modified
  - A new scheduling cycle begins, which will retry the failed job
  - Resources become available for the failed job

**Use Case**: When you want strict first-come-first-served ordering and can tolerate temporary resource underutilization.

**Characteristics**:
- Maintains strict job ordering based on creation time
- Blocks the entire queue when any job fails to schedule
- Ensures fairness based on job submission order
- May lead to resource underutilization if blocked jobs require more resources than currently available

### 2. Priority Based FIFO (`priority-based-fifo`)

**Behavior**:
- Jobs are sorted by **priority** (higher priority first)
- When a job fails to allocate resources (`stmt == nil`), the queue is **not blocked** and continues processing
- **Note**: Currently, this strategy behaves the same as `priority-based-traverse` in terms of blocking behavior (continues processing on failure)
- **Priority-based scheduling**: If a higher-priority job arrives in a new scheduling cycle, it will be scheduled before lower-priority jobs that were previously in the queue. This ensures that the highest-priority jobs are always considered first, regardless of when they were submitted.

**Use Case**: When you want priority-based ordering. Note that the current implementation continues processing on failure, similar to traverse behavior.

**Characteristics**:
- Maintains job ordering based on priority
- Currently continues processing even when a job fails to schedule (implementation may differ from naming)
- Ensures high-priority jobs are considered first
- Maximizes resource utilization by continuing to process other jobs
- **Dynamic priority handling**: Higher-priority jobs arriving in new scheduling cycles will be scheduled before previously queued lower-priority jobs

### 3. Creation Time Based Traverse (`creationtime-based-traverse`)

**Behavior**:
- Jobs are sorted by **creation time** (first-in-first-out)
- When a job fails to allocate resources (`stmt == nil`), the **entire queue is blocked** for the current scheduling cycle
- **Note**: Currently, this strategy behaves the same as `creationtime-based-fifo` in terms of blocking behavior (blocks on failure)

**Use Case**: When you want jobs sorted by creation time. Note that the current implementation blocks on failure, similar to FIFO behavior.

**Characteristics**:
- Jobs ordered by creation time
- Current implementation blocks queue on failure (implementation may differ from naming)
- Ensures strict first-come-first-served ordering

### 4. Priority Based Traverse (`priority-based-traverse`) - Default

**Behavior**:
- Jobs are sorted by **priority** (higher priority first)
- When a job fails to allocate resources (`stmt == nil`), the queue is **not blocked** and continues processing
- The scheduler continues to attempt scheduling other jobs in the queue
- Failed jobs remain in the queue and may be retried in subsequent scheduling cycles

**Use Case**: When you want to maximize resource utilization by allowing smaller or lower-priority jobs to be scheduled even when higher-priority jobs are waiting.

**Characteristics**:
- Maximizes cluster resource utilization
- Allows smaller jobs to be scheduled even when larger jobs are blocked
- Continues queue processing regardless of individual job failures
- Uses priority-based job ordering
- May lead to starvation of large/high-priority jobs if many small jobs keep arriving
- Provides better resource utilization in heterogeneous workload environments



## Configuration

### Using Annotations

Configure the dequeue strategy by adding an annotation to your Queue resource:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: my-queue
  annotations:
    volcano.sh/dequeue-strategy: "priority-based-traverse"  # or other strategies
spec:
  weight: 1
```

### Available Values

- `creationtime-based-fifo`: Sort by creation time, block queue on failure
- `priority-based-fifo`: Sort by priority, block queue on failure
- `creationtime-based-traverse`: Sort by creation time, current behavior blocks on failure
- `priority-based-traverse`: Sort by priority, continue processing on failure (default)

If no annotation is provided or an invalid value is specified, the system defaults to `priority-based-traverse` strategy and logs a warning.

## Examples

### Example 1: Priority-Based FIFO for Critical Workloads

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: critical-queue
  annotations:
    volcano.sh/dequeue-strategy: "priority-based-fifo"
spec:
  weight: 100
  capability:
    cpu: "1000"
    memory: "1000Gi"
```

This configuration ensures high-priority jobs are always scheduled first. If a higher-priority job arrives in a new scheduling cycle, it will be scheduled before lower-priority jobs that were previously in the queue. This ensures that the highest-priority jobs are always considered first, regardless of when they were submitted.

### Example 2: Priority-Based Traverse for Development

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: dev-queue
  annotations:
    volcano.sh/dequeue-strategy: "priority-based-traverse"
spec:
  weight: 10
  capability:
    cpu: "100"
    memory: "200Gi"
```

This configuration maximizes resource utilization by continuing to process jobs even when some fail to allocate.

### Example 3: Creation Time Based FIFO

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: first-come-queue
  annotations:
    volcano.sh/dequeue-strategy: "creationtime-based-fifo"
spec:
  weight: 50
  capability:
    cpu: "500"
    memory: "500Gi"
```

This configuration processes jobs strictly in the order they were created, blocking the queue if the first job cannot be scheduled.

### Example 4: Creation Time Based Traverse

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: time-ordered-queue
  annotations:
    volcano.sh/dequeue-strategy: "creationtime-based-traverse"
spec:
  weight: 30
  capability:
    cpu: "300"
    memory: "300Gi"
```

This configuration orders jobs by creation time. Note: Currently, this strategy also blocks the queue on failure.



## Monitoring and Troubleshooting

### Checking Queue Configuration

You can verify the dequeue strategy of a queue using kubectl:

```bash
kubectl get queue my-queue -o yaml
```

Look for the `volcano.sh/dequeue-strategy` annotation.

### Logs

The scheduler logs will indicate which strategy is being used for each queue:

```
Try to allocate resource to Jobs in Queue <my-queue> with strategy <priority-based-traverse>
```

#### FIFO Strategy Logs
When using FIFO strategies (`*-fifo`), you may see logs indicating queue blocking:
```
Try to allocate resource to Jobs in Queue <critical-queue> with strategy <priority-based-fifo>
Try to allocate resource to 1 tasks of Job <namespace/job-name>
# No further logs for this queue until job succeeds or is removed
```

#### Traverse Strategy Logs  
When using `priority-based-traverse` strategy, you'll see continuous queue processing:
```
Try to allocate resource to Jobs in Queue <dev-queue> with strategy <priority-based-traverse>
Try to allocate resource to 1 tasks of Job <namespace/job1>
Try to allocate resource to Jobs in Queue <dev-queue> with strategy <priority-based-traverse>
Try to allocate resource to 1 tasks of Job <namespace/job2>
```

### Common Issues

1. **Queue blocked in FIFO strategies**: If a job cannot be scheduled due to insufficient resources, the entire queue becomes blocked (applies to `*-fifo` strategies). Check resource requirements and availability. Consider:
   - Increasing cluster resources
   - Switching to `priority-based-traverse` strategy for better utilization
   - Reviewing job resource requests for optimization

2. **Large jobs never scheduled in traverse queue**: When using `priority-based-traverse`, smaller jobs may continuously consume available resources, causing starvation of large jobs. Consider:
   - Using `priority-based-fifo` strategy to ensure large jobs get priority
   - Implementing resource quotas or limits
   - Adjusting queue weights to balance resource allocation

3. **Invalid strategy annotation**: The system will fall back to `priority-based-traverse` (default) strategy and log a warning:
   ```
   Invalid dequeue strategy 'invalid-strategy' for queue 'my-queue', using default strategy 'priority-based-traverse'
   ```
   Check annotation syntax and supported values.

4. **No strategy annotation**: If no annotation is provided, the system uses the default strategy and logs:
   ```
   No dequeue strategy annotation found for queue 'my-queue', using default strategy 'priority-based-traverse'
   ```

5. **Queue not processing jobs**: Verify that:
   - The queue has the correct dequeue strategy annotation
   - Jobs are in the correct queue
   - Cluster has sufficient resources for at least some jobs
   - Queue is not overused (check `Overused` condition)

## Best Practices

1. **Use Priority-Based FIFO** (`priority-based-fifo`) for queues where:
   - Job execution order based on priority is critical (e.g., production pipelines)
   - You can tolerate temporary resource underutilization
   - High-priority jobs must be scheduled before lower-priority ones
   - Preventing resource fragmentation is important

2. **Use Creation Time Based FIFO** (`creationtime-based-fifo`) for queues where:
   - First-come-first-served ordering is required
   - Fairness based on submission time is important
   - You can tolerate temporary resource underutilization

3. **Use Priority-Based Traverse** (`priority-based-traverse`) for queues where:
   - Maximum resource utilization is the priority
   - Jobs have varying resource requirements
   - Development or testing environments with mixed workloads
   - You want to avoid blocking the entire queue due to single job failures
   - Priority-based ordering is still desired

4. **Use Creation Time Based Traverse** (`creationtime-based-traverse`) for queues where:
   - Jobs should be ordered by creation time
   - Note: Currently behaves similarly to FIFO in blocking behavior

5. **Monitor queue behavior** regularly to ensure the chosen strategy meets your requirements:
   - Check job completion times and queue throughput
   - Monitor resource utilization patterns
   - Watch for signs of job starvation or queue blocking
   - Review scheduler logs to verify strategy is being applied correctly

6. **Consider queue weights** in combination with dequeue strategies to achieve desired resource allocation across multiple queues.

7. **Resource planning**: Design your cluster and job resource requests considering the dequeue strategy:
   - For FIFO queues, ensure adequate resources for the largest expected jobs
   - For Traverse queues, consider the impact of resource fragmentation
   - Balance between strict ordering and resource utilization

8. **Job priority configuration**: When using priority-based strategies, ensure jobs have appropriate priority values set to achieve desired scheduling behavior.

## Implementation Details

### Architecture

- Dequeue strategy is configured per queue through the `volcano.sh/dequeue-strategy` annotation
- Strategy selection happens during queue processing in the allocate action
- All strategies use the same unified scheduling loop with conditional logic
- Job sorting function is selected based on the strategy (creation time vs priority)

### Job Sorting Logic

The scheduler uses different sorting functions based on the strategy:

```go
// In allocateResources function
if ssn.Queues[job.Queue].DequeueStrategy == api.DequeueStrategyCreationtimeBasedFIFO ||
   ssn.Queues[job.Queue].DequeueStrategy == api.DequeueStrategyCreationTimeBasedTraverse {
    // Sort jobs by creation time
    jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobCreationTimeBasedOrderFn)
} else {
    // Sort jobs by priority (for priority-based strategies)
    jobsMap[job.Queue] = util.NewPriorityQueue(ssn.JobOrderFn)
}
```

### Blocking Behavior Logic

When a job fails to allocate resources (`stmt == nil`), the queue blocking behavior depends on the strategy:

```go
// In allocateResources function after job allocation attempt
if (queue.DequeueStrategy == api.DequeueStrategyCreationtimeBasedFIFO ||
    queue.DequeueStrategy == api.DequeueStrategyCreationTimeBasedTraverse) && stmt == nil {
    continue  // Don't add queue back - blocks further processing
}
// For priority-based strategies or successful allocation, add queue back
queues.Push(queue)
```

**Current Implementation Notes**:
- **Creation time based strategies** (`creationtime-based-fifo` and `creationtime-based-traverse`): Both block the queue when a job fails to allocate (the current implementation does not distinguish between FIFO and Traverse for creation-time-based strategies)
- **Priority based strategies** (`priority-based-fifo` and `priority-based-traverse`): Both continue processing by re-queuing the queue even when a job fails (the current implementation does not distinguish between FIFO and Traverse for priority-based strategies)

**Note**: The current implementation groups strategies by sorting method (creation time vs priority) rather than by blocking behavior (FIFO vs Traverse). This means:
- All `creationtime-based-*` strategies currently block on failure
- All `priority-based-*` strategies currently continue processing on failure

### Strategy Constants

The available strategies are defined as constants in `pkg/scheduler/api/queue_info.go`:

```go
const (
    DequeueStrategyCreationtimeBasedFIFO      = "creationtime-based-fifo"
    DequeueStrategyPriorityBasedFIFO          = "priority-based-fifo"
    DequeueStrategyCreationTimeBasedTraverse  = "creationtime-based-traverse"
    DequeueStrategyPriorityBasedTraverse      = "priority-based-traverse"
    DefaultDequeueStrategy                     = DequeueStrategyPriorityBasedTraverse
)
```

### Default Behavior

- If no annotation is provided, the system uses `priority-based-traverse`
- If an invalid strategy value is provided, the system falls back to `priority-based-traverse` and logs a warning
