# Resource Quota Plugin (crossquota)

The Resource Quota Plugin (`crossquota`) is a Volcano scheduler plugin that limits resource usage of non-GPU tasks on GPU nodes. This plugin is a more general version of the CPU Quota Plugin, supporting multiple resource types including CPU, memory, ephemeral storage, hugepages, and other custom resources.

## Overview

The plugin automatically identifies GPU nodes and non-GPU tasks, then enforces resource quotas to ensure fair resource allocation. It supports both global configuration and node-specific overrides through annotations.

## Features

- **GPU Node Detection**: Automatically identifies GPU nodes based on configurable resource patterns
- **Non-GPU Task Identification**: Distinguishes non-GPU tasks from GPU tasks based on resource requests
- **Multi-Resource Support**: Supports quotas for CPU, memory, ephemeral storage, hugepages, and custom resources
- **Flexible Quota Configuration**: Supports both absolute resource quotas and percentage-based quotas
- **Node-Specific Overrides**: Allows per-node quota configuration through annotations
- **Real-time Tracking**: Monitors resource usage on GPU nodes in real-time
- **Regex Support**: Supports regular expressions for GPU resource name patterns
- **Backward Compatibility**: Defaults to CPU-only quotas for backward compatibility

## Configuration

### Plugin Arguments

The plugin accepts the following configuration arguments:

- `gpu-resource-names`: Comma-separated list of GPU resource names (supports regex)
- `quota-resources`: Comma-separated list of resource names to be quota-controlled
- `quota.<resource-name>`: Absolute quota for a specific resource (e.g., `quota.cpu: "4"`)
- `quota-percentage.<resource-name>`: Percentage quota for a specific resource (e.g., `quota-percentage.memory: "50"`)

### Node Annotations

Nodes can override the global configuration using annotations:

- `volcano.sh/crossquota-<resource-name>`: Node-specific absolute quota for a resource
- `volcano.sh/crossquota-percentage-<resource-name>`: Node-specific percentage quota for a resource

### Priority Order

1. Node annotation `volcano.sh/crossquota-<resource-name>` (highest priority)
2. Node annotation `volcano.sh/crossquota-percentage-<resource-name>`
3. Plugin argument `quota.<resource-name>`
4. Plugin argument `quota-percentage.<resource-name>`
5. Full allocatable resource (no quota)

**Note**: If both quota and percentage are specified for the same resource at the same level, quota takes precedence.

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
      - name: crossquota
        arguments:
          gpu-resource-names: "nvidia.com/gpu,amd.com/gpu"
          quota-resources: "cpu,memory,ephemeral-storage"
          quota.cpu: "4"
          quota.memory: "8Gi"
          quota-percentage.ephemeral-storage: "50"
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
    volcano.sh/crossquota-cpu: "6"
    volcano.sh/crossquota-memory: "12Gi"
    volcano.sh/crossquota-percentage-ephemeral-storage: "75"
spec:
  # ... node specification
```

## Examples

### Example 1: Basic Multi-Resource Configuration

```yaml
# Scheduler configuration
gpu-resource-names: "nvidia.com/gpu"
quota-resources: "cpu,memory,ephemeral-storage"
quota.cpu: "4"
quota.memory: "8Gi"
quota.ephemeral-storage: "10Gi"
```

This configuration:
- Identifies nodes with `nvidia.com/gpu` resources as GPU nodes
- Limits non-GPU tasks to use maximum 4 CPU cores, 8Gi memory, and 10Gi ephemeral storage on GPU nodes
- Allows GPU tasks to use full resources

### Example 2: Percentage-Based Quotas

```yaml
# Scheduler configuration
gpu-resource-names: "nvidia.com/gpu,amd.com/gpu"
quota-resources: "cpu,memory,ephemeral-storage"
quota-percentage.cpu: "50"
quota-percentage.memory: "75"
quota-percentage.ephemeral-storage: "60"
```

This configuration:
- Identifies nodes with `nvidia.com/gpu` or `amd.com/gpu` resources as GPU nodes
- Limits non-GPU tasks to use maximum 50% CPU, 75% memory, and 60% ephemeral storage on GPU nodes

### Example 3: Mixed Configuration

```yaml
# Scheduler configuration
gpu-resource-names: ".*\\.com/gpu"
quota-resources: "cpu,memory,ephemeral-storage"
quota.cpu: "2"
quota.memory: "4Gi"
quota-percentage.ephemeral-storage: "50"
```

This configuration:
- Identifies nodes with any resource ending in `.com/gpu` as GPU nodes
- Limits non-GPU tasks to use maximum 2 CPU cores and 4Gi memory (absolute quotas)
- Limits non-GPU tasks to use maximum 50% ephemeral storage (percentage quota)

### Example 4: Custom Resources

```yaml
# Scheduler configuration
gpu-resource-names: "nvidia.com/gpu"
quota-resources: "cpu,memory,hugepages-1Gi"
quota.cpu: "4"
quota.memory: "8Gi"
quota.hugepages-1Gi: "2"
```

This configuration:
- Supports custom resources beyond the standard Kubernetes resources
- Limits non-GPU tasks to use maximum 2 hugepages of 1Gi each

### Example 5: Backward Compatibility (CPU Only)

```yaml
# Scheduler configuration
gpu-resource-names: "nvidia.com/gpu"
# No quota-resources specified - defaults to CPU only
quota.cpu: "4"
```

This configuration:
- Maintains backward compatibility with CPU-only configurations
- Defaults to controlling only CPU resources when `quota-resources` is not specified

## Resource Format Support

### Standard Resources

- **CPU**: Supports cores (e.g., "4") or millicores (e.g., "4000m")
- **Memory**: Supports various units (e.g., "8Gi", "8192Mi", "8G")
- **Ephemeral Storage**: Supports various units (e.g., "10Gi", "10240Mi")

### Custom Resources

- **Hugepages**: Supports hugepage resources (e.g., "hugepages-1Gi=2", "hugepages-2Mi=1000")
- **Other Custom Resources**: Supports any custom resource defined in the cluster

### Percentage Values

- Must be between 0 and 100
- Applied to the node's allocatable resource amount
- Example: `quota-percentage.cpu: "50"` means 50% of the node's allocatable CPU

## How It Works

1. **Session Initialization**: During `OnSessionOpen`, the plugin:
   - Identifies GPU nodes based on configured resource patterns
   - Calculates resource quotas for each GPU node
   - Creates tracking structures for resource usage
   - Adds existing non-GPU tasks to tracking

2. **Predicate Function**: During scheduling, the plugin:
   - Skips non-GPU tasks and non-GPU nodes
   - Checks if adding the task would exceed any resource quota
   - Returns error if any quota would be exceeded

3. **Event Handling**: The plugin tracks task allocation/deallocation:
   - Updates resource usage when non-GPU tasks are allocated
   - Updates resource usage when non-GPU tasks are deallocated
   - Maintains accurate resource tracking

4. **Session Cleanup**: During `OnSessionClose`, the plugin:
   - Clears all tracking data
   - Prepares for the next scheduling session

## Error Handling

The plugin handles various error conditions:

- **Invalid GPU Patterns**: Invalid regex patterns are logged and ignored
- **Invalid Quota Values**: Invalid quota values result in errors during quota calculation
- **Invalid Percentage Values**: Percentages outside 0-100 range are rejected
- **Missing Configuration**: If no quota is configured for a resource, full allocatable amount is used
- **Resource Tracking**: Ensures accurate resource tracking even with task failures

## Monitoring

The plugin provides detailed logging:

- GPU node identification (level 4)
- Resource quota calculations (level 4)
- Task allocation/deallocation events (level 5)
- Resource usage tracking (level 5)

Example log messages:
```
crossquota: node gpu-node-1 quota cpu set to 4000 (original 8000)
crossquota: task cpu-task-1 allocated on gpu-node-1, usage {cpu=1000/4000 memory=1073741824/8589934592}
crossquota: task cpu-task-2 deallocated on gpu-node-1, usage {cpu=500/4000 memory=536870912/8589934592}
```

## Best Practices

1. **Configure GPU Resource Names**: Ensure GPU resource names are correctly specified
2. **Set Appropriate Quotas**: Balance between non-GPU task needs and GPU task requirements
3. **Use Node Annotations**: Override global settings for specific nodes when needed
4. **Monitor Usage**: Regularly check resource usage patterns to optimize quota settings
5. **Test Configuration**: Validate configuration in a test environment before production
6. **Resource Units**: Use consistent and appropriate units for different resource types
7. **Backward Compatibility**: Start with CPU-only configuration and gradually add other resources

## Limitations

- Only tracks configured resources (other resources are not limited)
- Requires GPU resource patterns to be correctly configured
- Node annotations take precedence over global configuration
- Quota enforcement is based on resource requests, not actual usage
- Custom resource tracking may require additional implementation for complex scenarios
