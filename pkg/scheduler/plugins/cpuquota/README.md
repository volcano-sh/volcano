# CPU Quota Plugin

The CPU Quota Plugin is a Volcano scheduler plugin that limits CPU task usage on GPU nodes. This plugin helps prevent CPU-intensive workloads from consuming all available CPU resources on GPU nodes, ensuring that GPU workloads have sufficient CPU resources for optimal performance.

## Overview

The plugin automatically identifies GPU nodes and CPU tasks, then enforces CPU quotas to ensure fair resource allocation. It supports both global configuration and node-specific overrides through annotations.

## Features

- **GPU Node Detection**: Automatically identifies GPU nodes based on configurable resource patterns
- **CPU Task Identification**: Distinguishes CPU tasks from GPU tasks based on resource requests
- **Flexible Quota Configuration**: Supports both absolute CPU quotas and percentage-based quotas
- **Node-Specific Overrides**: Allows per-node quota configuration through annotations
- **Real-time Tracking**: Monitors CPU usage on GPU nodes in real-time
- **Regex Support**: Supports regular expressions for GPU resource name patterns

## Configuration

### Plugin Arguments

The plugin accepts the following configuration arguments:

- `cpuquota.gpu-resource-names`: Comma-separated list of GPU resource names (supports regex)
- `cpuquota.cpu-quota`: Global CPU quota in cores (e.g., 4.0 for 4 cores)
- `cpuquota.cpu-quota-percentage`: Global CPU quota as percentage of node's allocatable CPU (e.g., 50.0 for 50%)

### Node Annotations

Nodes can override the global configuration using annotations:

- `volcano.sh/cpu-quota`: Node-specific CPU quota in cores
- `volcano.sh/cpu-quota-percentage`: Node-specific CPU quota as percentage

### Priority Order

1. Node annotation `volcano.sh/cpu-quota` (highest priority)
2. Node annotation `volcano.sh/cpu-quota-percentage`
3. Plugin argument `cpuquota.cpu-quota`
4. Plugin argument `cpuquota.cpu-quota-percentage`
5. Full allocatable CPU (no quota)

**Note**: If both quota and percentage are specified at the same level, quota takes precedence.

## Usage

### 1. Enable Plugin in Scheduler Configuration

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, backfill"
    tiers:
    - plugins:
      - name: priority
      - name: gang
      - name: conformance
    - plugins:
      - name: drf
      - name: predicates
      - name: cpuquota
        arguments:
          cpuquota.gpu-resource-names: "nvidia.com/gpu,amd.com/gpu"
          cpuquota.cpu-quota: 4.0
          cpuquota.cpu-quota-percentage: 50.0
      - name: nodeorder
      - name: binpack
```

### 2. Node-Specific Configuration

```yaml
apiVersion: v1
kind: Node
metadata:
  name: gpu-node-1
  annotations:
    volcano.sh/cpu-quota: "6"  # Override to 6 cores (string format for annotations)
spec:
  # ... node specification
```

Or using percentage:

```yaml
apiVersion: v1
kind: Node
metadata:
  name: gpu-node-2
  annotations:
    volcano.sh/cpu-quota-percentage: "75"  # 75% of allocatable CPU
spec:
  # ... node specification
```

## Examples

### Example 1: Basic Configuration

```yaml
# Scheduler configuration
cpuquota.gpu-resource-names: "nvidia.com/gpu"
cpuquota.cpu-quota: 4.0
```

This configuration:
- Identifies nodes with `nvidia.com/gpu` resources as GPU nodes
- Limits CPU tasks to use maximum 4 cores on GPU nodes
- Allows GPU tasks to use full CPU resources

### Example 2: Percentage-Based Quota

```yaml
# Scheduler configuration
cpuquota.gpu-resource-names: "nvidia.com/gpu,amd.com/gpu"
cpuquota.cpu-quota-percentage: 50.0
```

This configuration:
- Identifies nodes with `nvidia.com/gpu` or `amd.com/gpu` resources as GPU nodes
- Limits CPU tasks to use maximum 50% of allocatable CPU on GPU nodes

### Example 3: Regex Pattern Support

```yaml
# Scheduler configuration
cpuquota.gpu-resource-names: ".*\\.com/gpu"
cpuquota.cpu-quota: 2.0
```

This configuration:
- Identifies nodes with any resource ending in `.com/gpu` as GPU nodes
- Limits CPU tasks to use maximum 2 cores on GPU nodes

## How It Works

1. **Session Initialization**: During `OnSessionOpen`, the plugin:
   - Identifies GPU nodes based on configured resource patterns
   - Calculates CPU quotas for each GPU node
   - Creates tracking structures for CPU usage
   - Adds existing CPU tasks to tracking

2. **Predicate Function**: During scheduling, the plugin:
   - Skips non-CPU tasks and non-GPU nodes
   - Checks if adding the task would exceed the CPU quota
   - Returns error if quota would be exceeded

3. **Event Handling**: The plugin tracks task allocation/deallocation:
   - Updates CPU usage when CPU tasks are allocated
   - Updates CPU usage when CPU tasks are deallocated
   - Maintains accurate resource tracking

4. **Session Cleanup**: During `OnSessionClose`, the plugin:
   - Clears all tracking data
   - Prepares for the next scheduling session

## Error Handling

The plugin handles various error conditions:

- **Invalid GPU Patterns**: Invalid regex patterns are logged and ignored
- **Invalid Quota Values**: Invalid quota values result in errors during quota calculation
- **Missing Configuration**: If no quota is configured, full allocatable CPU is used
- **Resource Tracking**: Ensures accurate resource tracking even with task failures

## Monitoring

The plugin provides detailed logging at level 4:

- GPU node identification
- CPU quota calculations
- Task allocation/deallocation events
- Resource usage tracking

Example log messages:
```
Node gpu-node-1 CPU quota set to 4.00 (original: 8.00)
CPU task cpu-task-1 allocated to node gpu-node-1, CPU usage: 2.00/4.00
CPU task cpu-task-2 deallocated from node gpu-node-1, CPU usage: 1.00/4.00
```

## Best Practices

1. **Configure GPU Resource Names**: Ensure GPU resource names are correctly specified
2. **Set Appropriate Quotas**: Balance between CPU task needs and GPU task requirements
3. **Use Node Annotations**: Override global settings for specific nodes when needed
4. **Monitor Usage**: Regularly check CPU usage patterns to optimize quota settings
5. **Test Configuration**: Validate configuration in a test environment before production

## Limitations

- Only tracks CPU resources (memory and other resources are not limited)
- Requires GPU resource patterns to be correctly configured
- Node annotations take precedence over global configuration
- Quota enforcement is based on resource requests, not actual usage
