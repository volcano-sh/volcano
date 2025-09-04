# Dequeue Strategies

Volcano scheduler supports different strategies for dequeuing jobs from queues when resources are limited. This document describes the available dequeue strategies and how to configure them.

## Available Strategies

### 1. FIFO Strategy (`fifo`)

**Behavior**: When a job in the queue fails to schedule due to insufficient resources, the entire queue is blocked for the current scheduling cycle. The scheduler will not attempt to schedule any other jobs from this queue until one of the following occurs:
- The failed job is removed or modified.
- A new scheduling cycle begins, which will retry the failed job.
- Another higher-priority job has been added to the queue.

The system maintains strict job ordering and does not skip failed jobs to try subsequent ones.

**Use Case**: When you want strict ordering and prefer to wait for resources to become available for higher-priority jobs rather than scheduling lower-priority ones.

**Characteristics**:
- Maintains strict job ordering within the queue
- Blocks the entire queue when any job fails to schedule
- May lead to resource underutilization if blocked jobs require more resources than currently available
- Ensures fairness based on job submission order and priority

### 2. Traverse Strategy (`traverse`) - Default

**Behavior**: If a job in the queue cannot be scheduled (resource allocation fails), the system continues processing the queue and attempts to schedule other jobs. Failed jobs remain in the queue and may be retried in subsequent scheduling cycles.

**Use Case**: When you want to maximize resource utilization by allowing smaller jobs to be scheduled even when larger jobs are waiting.

**Characteristics**:
- Maximizes cluster resource utilization
- Allows smaller jobs to be scheduled even when larger jobs are blocked
- Continues queue processing regardless of individual job failures
- May lead to starvation of large jobs if many small jobs keep arriving
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
    volcano.sh/dequeue-strategy: "fifo"  # or "traverse"
spec:
  weight: 1
```

### Available Values

- `fifo`: Use FIFO strategy
- `traverse`: Use traverse strategy (default)

If no annotation is provided or an invalid value is specified, the system defaults to `traverse` strategy.

## Examples

### Example 1: FIFO Queue for Critical Workloads

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: critical-queue
  annotations:
    volcano.sh/dequeue-strategy: "fifo"
spec:
  weight: 100
  capability:
    cpu: "1000"
    memory: "1000Gi"
```

### Example 2: Traverse Queue for Development

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: dev-queue
  annotations:
    volcano.sh/dequeue-strategy: "traverse"
spec:
  weight: 10
  capability:
    cpu: "100"
    memory: "200Gi"
```



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
Try to allocate resource to Jobs in Queue <my-queue> with strategy <fifo>
```

#### FIFO Strategy Logs
When using FIFO strategy, you may see logs indicating queue blocking:
```
Try to allocate resource to Jobs in Queue <critical-queue> with strategy <fifo>
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

2. **Large jobs never scheduled in traverse queue**: This indicates resource starvation where smaller jobs continuously consume available resources. Consider:
   - Using `fifo` strategy to ensure large jobs get priority
   - Implementing resource quotas or limits
   - Adjusting queue weights to balance resource allocation

3. **Invalid strategy annotation**: The system will fall back to `traverse` strategy and log a warning. Check annotation syntax and supported values.

4. **Queue not processing jobs**: Verify that:
   - The queue has the correct dequeue strategy annotation
   - Jobs are in the correct queue
   - Cluster has sufficient resources for at least some jobs

## Best Practices

1. **Use FIFO** for queues where:
   - Job execution order is critical (e.g., production pipelines)
   - You can tolerate temporary resource underutilization
   - Jobs have similar resource requirements
   - Preventing resource fragmentation is important

2. **Use Traverse** for queues where:
   - Maximum resource utilization is the priority
   - Jobs have varying resource requirements
   - Development or testing environments with mixed workloads
   - You want to avoid blocking the entire queue due to single job failures

3. **Monitor queue behavior** regularly to ensure the chosen strategy meets your requirements:
   - Check job completion times and queue throughput
   - Monitor resource utilization patterns
   - Watch for signs of job starvation or queue blocking

4. **Consider queue weights** in combination with dequeue strategies to achieve desired resource allocation across multiple queues.

5. **Resource planning**: Design your cluster and job resource requests considering the dequeue strategy:
   - For FIFO queues, ensure adequate resources for the largest expected jobs
   - For Traverse queues, consider the impact of resource fragmentation

## Implementation Details

### Architecture

- Dequeue strategy is configured per queue through annotations
- Strategy selection happens during queue processing in the allocate action
- Both strategies use the same unified scheduling loop with conditional logic

### FIFO Strategy Implementation

- When a job fails to schedule (`stmt == nil`), the queue is **not** added back to the scheduling loop
- This effectively blocks the entire queue until:
  - Resources become available for the failed job
  - The job is removed or modified
  - The queue strategy is changed

### Traverse Strategy Implementation  

- When a job fails to schedule, the queue is always added back to the scheduling loop
- The scheduler continues to process other jobs in the queue regardless of individual failures
- This ensures continuous queue processing and maximum resource utilization

### Key Code Logic

```go
// Core logic in allocateResources function
if queue.DequeueStrategy == api.DequeueStrategyFIFO && stmt == nil {
    continue  // Don't add queue back - blocks further processing
}
// Always add queue back for traverse strategy or successful scheduling
queues.Push(queue)
```
