# Capacity Plugin: Check Queue Dimensions Only Feature

## Overview

This feature adds a configuration switch to the capacity plugin that allows users to control which resource dimensions are checked when determining if a queue can accommodate a task or job.

## Background

By default, the capacity plugin checks all resource dimensions (CPU, memory, and all scalar resources) when determining if a task can be allocated to a queue or if a job can be enqueued. This can be overly restrictive in scenarios where:

1. A queue is primarily intended for specific resource types (e.g., GPU-only workloads)
2. Users want to only enforce limits on certain resources while being flexible with others
3. The cluster has heterogeneous resources and different queues focus on different resource types

## Feature Design

### Configuration

The feature is controlled by a plugin argument `checkQueueDimensionsOnly` in the scheduler configuration:

- **`checkQueueDimensionsOnly: true`**: Only check resource dimensions that are explicitly defined in the queue's `capability` field
- **`checkQueueDimensionsOnly: false`** (default): Check all resource dimensions requested by the task/job

### Behavior

#### When Enabled (`checkQueueDimensionsOnly: true`)

The plugin will:
1. Extract the resource dimensions defined in the queue's `capability` specification
2. Only check those specific dimensions when validating resource availability
3. Ignore other resource dimensions not specified in the capability

**Example**: If a queue's capability only defines GPU resources:
```yaml
capability:
  nvidia.com/gpu: "8"
```

Then only GPU resources will be checked. CPU and memory requests will not be validated against the queue's capacity, allowing flexible allocation based on GPU availability.

#### When Disabled (`checkQueueDimensionsOnly: false`) - Default Behavior

The plugin will:
1. Check all resource dimensions requested by the task/job
2. Validate that the queue has sufficient capacity for every requested resource type
3. Reject allocation if any resource dimension exceeds capacity

This is the original behavior that ensures all resources are within queue limits.

## Configuration Example

### Scheduler ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, backfill"
    tiers:
    - plugins:
      - name: capacity
        arguments:
          checkQueueDimensionsOnly: true  # Enable the feature
```

### Queue Definition

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: gpu-queue
spec:
  weight: 1
  capability:
    nvidia.com/gpu: "16"  # Only GPU capacity is defined
  reclaimable: true
```

With the above configuration:
- The scheduler will only check if the GPU resources fit within the queue's capacity
- CPU and memory resources will not be validated against this queue's capacity
- Tasks can be scheduled as long as GPU requirements are met

## Use Cases

### 1. GPU-Only Queue

For a queue dedicated to GPU workloads where CPU/memory are abundant:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: gpu-training
spec:
  capability:
    nvidia.com/gpu: "32"
```

With `checkQueueDimensionsOnly: true`, this queue will only enforce GPU limits, allowing flexible CPU/memory allocation.

### 2. Mixed Resource Queue with Specific Limits

For scenarios where you want to limit specific resources while being flexible with others:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: ml-workloads
spec:
  capability:
    nvidia.com/gpu: "16"
    hostdev: "8"
```

This queue will only check GPU and RDMA nics, ignoring CPU and memory limits.

### 3. Default Behavior for General Purpose Queue

For general-purpose queues, keep the feature disabled (default):

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: volcano-scheduler-configmap
data:
  volcano-scheduler.conf: |
    tiers:
    - plugins:
      - name: capacity
        # checkQueueDimensionsOnly not set, defaults to false
```

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: default-queue
spec:
  capability:
    cpu: "100"
    memory: "200Gi"
    nvidia.com/gpu: "8"
```

All resources (CPU, memory, GPU) will be checked against capacity.

## Implementation Details

### New Resource Comparison Function

Added `LessEqualWithSpecifiedDimensions` to `pkg/scheduler/api/resource_info.go`:

```go
func (r *Resource) LessEqualWithSpecifiedDimensions(rr *Resource, specifiedDimensions *Resource) (bool, []string)
```

This function:
- Compares two resources only for dimensions specified in `specifiedDimensions`
- Returns true if all specified dimensions are within limits
- Returns false and a list of insufficient resources if any specified dimension exceeds limits

### Plugin Modifications

The capacity plugin (`pkg/scheduler/plugins/capacity/capacity.go`) was modified to:

1. Accept the `checkQueueDimensionsOnly` configuration parameter
2. Use the new comparison function when the feature is enabled
3. Apply the selective checking in two key areas:
   - Task allocation (`queueAllocatableWithCheck`)
   - Job enqueueing (`jobEnqueueable`)

## Migration Guide

### Enabling the Feature

1. Update the scheduler ConfigMap to include the plugin argument:
   ```yaml
   arguments:
     checkQueueDimensionsOnly: true
   ```

2. Ensure your queue definitions only include the resource dimensions you want to enforce in the `capability` field

3. Restart the volcano-scheduler to apply the configuration

### Disabling the Feature

Simply remove the argument or set it to `false`:
```yaml
arguments:
  checkQueueDimensionsOnly: false
```

## Considerations

### Advantages
- More flexible resource management for specialized workloads
- Better utilization of heterogeneous resources
- Simplified queue configuration for single-resource-type workloads

### Limitations
- When enabled globally, all queues will use selective checking
- Requires careful queue capacity configuration to avoid unexpected behavior
- May allow over-subscription of non-checked resources

### Best Practices
1. Use this feature for specialized queues (GPU, RDMA, etc.)
2. Keep detailed documentation of which resources are checked per queue
3. Monitor resource usage to ensure no unintended over-subscription
4. Consider using different scheduler configurations for different queue types if needed

## Testing

### Test Scenario 1: GPU-Only Checking

```yaml
# Queue with only GPU capacity
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: test-gpu-queue
spec:
  capability:
    nvidia.com/gpu: "2"
```

```yaml
# Job requesting GPU, CPU, and memory
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: test-job
spec:
  minAvailable: 1
  queue: test-gpu-queue
  tasks:
  - replicas: 1
    template:
      spec:
        containers:
        - name: main
          resources:
            requests:
              cpu: "100"        # Will not be checked
              memory: "100Gi"   # Will not be checked
              nvidia.com/gpu: "1"  # Will be checked
```

**Expected**: Job is enqueued and scheduled even if CPU/memory exceed normal limits, as long as GPU is available.

### Test Scenario 2: Default Behavior

With `checkQueueDimensionsOnly: false`, the same job would be rejected if CPU or memory exceed queue capacity.

## Conclusion

The `checkQueueDimensionsOnly` feature provides fine-grained control over resource checking in the capacity plugin, enabling more flexible and efficient resource management for specialized workloads. Enable it when you need to enforce limits only on specific resource types while maintaining flexibility for others.
