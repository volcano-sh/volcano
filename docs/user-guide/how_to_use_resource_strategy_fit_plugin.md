# Resource Strategy Fit Plugin User Guide

## Introduction

The **Resource Strategy Fit Plugin** is a Volcano scheduler plugin that provides intelligent resource allocation strategies for pod scheduling. It supports both global configuration and pod-level annotations to optimize resource utilization across different workloads.

## Key Features

- **Multiple Scoring Strategies**: Supports `LeastAllocated` and `MostAllocated` strategies
- **Resource-Specific Configuration**: Configure different strategies for different resource types (CPU, Memory, GPU, etc.)
- **Pod-Level Override**: Allow individual pods to override global configuration via annotations
- **Weighted Scoring**: Fine-tune resource importance with configurable weights
- **Wildcard Support**: Use wildcard patterns for resource matching

## Installation

### 1. Install Volcano

Refer to [Install Guide](../../installer/README.md) to install Volcano.

### 2. Configure the Plugin

Update the Volcano scheduler configuration:

```bash
kubectl edit cm -n volcano-system volcano-scheduler-configmap
```

Add the `resource-strategy-fit` plugin to your configuration:

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "reclaim, allocate, backfill, preempt"
    tiers:
    - plugins:
      - name: priority
      - name: gang
      - name: conformance
    - plugins:
      - name: drf
      - name: predicates
      - name: resource-strategy-fit
        arguments:
          resourceStrategyFitWeight: 10
          resources:
            cpu:
              type: "LeastAllocated"
              weight: 1
            memory:
              type: "LeastAllocated"
              weight: 1
            nvidia.com/gpu:
              type: "MostAllocated"
              weight: 2
```

## Global Configuration

### Basic Configuration

The plugin supports two main scoring strategies:

| Strategy | Description | Use Case |
|----------|-------------|----------|
| `LeastAllocated` | Prefers nodes with more available resources | General workloads, load balancing |
| `MostAllocated` | Prefers nodes with higher resource utilization | GPU workloads, resource consolidation |

### Configuration Parameters

```yaml
arguments:
  resourceStrategyFitWeight: 10          # Plugin weight (default: 10)
  resources:                              # Resource-specific configuration
    cpu:                                 # Resource name
      type: "LeastAllocated"             # Scoring strategy
      weight: 1                          # Resource weight
    memory:
      type: "LeastAllocated"
      weight: 1
    nvidia.com/gpu:
      type: "MostAllocated"
      weight: 2
```

### Advanced Configuration Examples

#### 1. GPU-Optimized Configuration

```yaml
arguments:
  resourceStrategyFitWeight: 20
  resources:
    cpu:
      type: "LeastAllocated"
      weight: 1
    memory:
      type: "LeastAllocated"
      weight: 1
    nvidia.com/gpu:
      type: "MostAllocated"
      weight: 5
    nvidia.com/gpu/*:                     # Wildcard for all GPU types
      type: "MostAllocated"
      weight: 3
```

#### 2. Mixed Strategy Configuration

```yaml
arguments:
  resourceStrategyFitWeight: 15
  resources:
    cpu:
      type: "LeastAllocated"
      weight: 3
    memory:
      type: "MostAllocated"
      weight: 1
    example.com/custom-resource:
      type: "LeastAllocated"
      weight: 2
```

## Pod-Level Configuration

### Pod Annotations

Individual pods can override the global configuration using annotations:

| Annotation Key | Description | Example |
|----------------|-------------|---------|
| `volcano.sh/resource-strategy-scoring-type` | Override scoring strategy | `"LeastAllocated"` or `"MostAllocated"` |
| `volcano.sh/resource-strategy-weight` | Override resource weights | `{"cpu": 2, "memory": 1, "nvidia.com/gpu": 3}` |

### Pod-Level Examples

#### 1. Override Strategy for Specific Pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: gpu-workload
  annotations:
    volcano.sh/resource-strategy-scoring-type: "MostAllocated"
    volcano.sh/resource-strategy-weight: '{"nvidia.com/gpu": 5, "cpu": 1}'
spec:
  containers:
  - name: gpu-container
    image: nvidia/cuda:11.0-runtime
    resources:
      requests:
        nvidia.com/gpu: 1
        cpu: "2"
        memory: "4Gi"
      limits:
        nvidia.com/gpu: 1
        cpu: "2"
        memory: "4Gi"
  schedulerName: volcano
```

#### 2. Custom Resource Weights

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: custom-resource-pod
  annotations:
    volcano.sh/resource-strategy-scoring-type: "LeastAllocated"
    volcano.sh/resource-strategy-weight: '{"cpu": 3, "memory": 2, "example.com/custom": 5}'
spec:
  containers:
  - name: app
    image: my-app:latest
    resources:
      requests:
        cpu: "1"
        memory: "2Gi"
        example.com/custom: "1"
  schedulerName: volcano
```

## Volcano Job Integration

### Basic Volcano Job

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: resource-strategy-job
spec:
  minAvailable: 2
  schedulerName: volcano
  plugins:
    env: []
    svc: []
  tasks:
  - replicas: 2
    name: worker
    template:
      metadata:
        annotations:
          volcano.sh/resource-strategy-scoring-type: "LeastAllocated"
          volcano.sh/resource-strategy-weight: '{"cpu": 2, "memory": 1}'
      spec:
        containers:
        - name: worker
          image: my-worker:latest
          resources:
            requests:
              cpu: "2"
              memory: "4Gi"
            limits:
              cpu: "2"
              memory: "4Gi"
        restartPolicy: Never
```

### Multi-Task Job with Different Strategies

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: mixed-strategy-job
spec:
  minAvailable: 3
  schedulerName: volcano
  plugins:
    env: []
    svc: []
  tasks:
  - replicas: 1
    name: gpu-task
    template:
      metadata:
        annotations:
          volcano.sh/resource-strategy-scoring-type: "MostAllocated"
          volcano.sh/resource-strategy-weight: '{"nvidia.com/gpu": 5, "cpu": 1}'
      spec:
        containers:
        - name: gpu-worker
          image: gpu-app:latest
          resources:
            requests:
              nvidia.com/gpu: 1
              cpu: "1"
              memory: "2Gi"
  - replicas: 2
    name: cpu-task
    template:
      metadata:
        annotations:
          volcano.sh/resource-strategy-scoring-type: "LeastAllocated"
          volcano.sh/resource-strategy-weight: '{"cpu": 3, "memory": 2}'
      spec:
        containers:
        - name: cpu-worker
          image: cpu-app:latest
          resources:
            requests:
              cpu: "2"
              memory: "4Gi"
```

## Use Cases

### 1. GPU Workload Optimization

For GPU-intensive workloads, use `MostAllocated` strategy to consolidate GPU usage:

```yaml
# Global configuration
arguments:
  resourceStrategyFitWeight: 20
  resources:
    nvidia.com/gpu:
      type: "MostAllocated"
      weight: 5
    cpu:
      type: "LeastAllocated"
      weight: 1
```

### 2. Load Balancing

For general workloads, use `LeastAllocated` strategy to distribute load evenly:

```yaml
# Global configuration
arguments:
  resourceStrategyFitWeight: 10
  resources:
    cpu:
      type: "LeastAllocated"
      weight: 2
    memory:
      type: "LeastAllocated"
      weight: 1
```

### 3. Mixed Workloads

Combine different strategies for different resource types:

```yaml
# Global configuration
arguments:
  resourceStrategyFitWeight: 15
  resources:
    cpu:
      type: "LeastAllocated"
      weight: 3
    memory:
      type: "LeastAllocated"
      weight: 2
    nvidia.com/gpu:
      type: "MostAllocated"
      weight: 5
```

## Troubleshooting

### Verify Plugin Configuration

Check the scheduler logs to ensure the plugin is loaded correctly:

```bash
kubectl logs -n volcano-system deployment/volcano-scheduler | grep "resource-strategy-fit"
```

Expected output:
```
Initialize resource-strategy-fit plugin with configuration: {resourceStrategyFitWeight: 10, resources: {...}}
```

### Common Issues

1. **Plugin not loaded**: Ensure the plugin is included in the scheduler configuration
2. **Invalid annotations**: Check JSON format for pod-level weight annotations
3. **Resource not found**: Verify resource names match exactly (case-sensitive)
4. **Scoring not working**: Check plugin weight and resource weights are properly configured

### Debug Information

Enable debug logging to see scoring decisions:

```yaml
# Add to scheduler configuration
arguments:
  resourceStrategyFitWeight: 10
  # ... other configuration
  logLevel: 4  # Enable debug logging
```

## Best Practices

1. **Start with global configuration** for consistent behavior across all workloads
2. **Use pod-level annotations sparingly** for specific workload requirements
3. **Test different strategies** to find optimal configuration for your use case
4. **Monitor resource utilization** after applying the plugin
5. **Use appropriate weights** to balance different resource types
6. **Consider workload characteristics** when choosing between `LeastAllocated` and `MostAllocated`
