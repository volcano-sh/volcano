# CrossQuota Plugin User Guide

## Introduction

The CrossQuota plugin is a powerful Volcano scheduler plugin designed for heterogeneous clusters with mixed GPU and CPU workloads. It provides two key capabilities:

1. **Multi-Resource Quota Management**: Limits resource usage of non-GPU tasks on GPU nodes to ensure GPU tasks have sufficient companion resources (CPU, memory, etc.).

2. **Intelligent Node Ordering**: Scores and ranks nodes based on resource utilization patterns, optimizing task placement according to configurable strategies.

This guide will walk you through setting up and using the CrossQuota plugin effectively.

## Features

- ✅ Multi-resource quota support (CPU, memory, ephemeral storage, hugepages, custom resources)
- ✅ Flexible quota configuration (absolute values and percentages)
- ✅ Node-specific quota overrides via annotations
- ✅ Two scoring strategies: most-allocated and least-allocated
- ✅ Configurable resource weights for fine-grained control
- ✅ Pod-level strategy selection via annotations
- ✅ Automatic GPU node and CPU pod detection
- ✅ Label-based pod filtering for selective quota enforcement
- ✅ Backward compatible with CPU-only configurations

## Prerequisites

- Volcano v1.14.0 or later
- Kubernetes cluster with GPU nodes
- Basic understanding of Kubernetes resource management

## Environment Setup

### Install Volcano

If you haven't installed Volcano yet, follow the [installation guide](https://github.com/volcano-sh/volcano/blob/master/installer/README.md).

### Enable CrossQuota Plugin

Edit the Volcano scheduler configuration:

```bash
kubectl edit configmap volcano-scheduler-configmap -n volcano-system
```

Add the `crossquota` plugin to your scheduler configuration:

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
          # Configure GPU resource patterns (supports regex)
          gpu-resource-names: "nvidia.com/gpu,amd.com/gpu"
          
          # Specify resources to be quota-controlled
          quota-resources: "cpu,memory"
          
          # Set quota limits (absolute values)
          quota.cpu: "32"
          quota.memory: "64Gi"
          
          # Node ordering configuration
          crossQuotaWeight: 10
          weight.cpu: 10
          weight.memory: 1
      - name: nodeorder
      - name: binpack
```

### Restart Volcano Scheduler

After updating the configuration, restart the scheduler:

```bash
kubectl rollout restart deployment volcano-scheduler -n volcano-system
```

## Configuration Guide

### Basic Configuration

#### GPU Resource Detection

Configure GPU resource patterns to identify GPU nodes:

```yaml
gpu-resource-names: "nvidia.com/gpu,amd.com/gpu"
```

Supports regex patterns for flexibility:

```yaml
gpu-resource-names: ".*\\.com/gpu"  # Matches nvidia.com/gpu, amd.com/gpu, etc.
```

#### Quota Resources

Specify which resources to control:

```yaml
quota-resources: "cpu,memory"
```

Supports standard and custom resources:

```yaml
quota-resources: "cpu,memory,ephemeral-storage,hugepages-1Gi"
```

#### Quota Limits

Set absolute quotas:

```yaml
quota.cpu: "32"           # 32 CPU cores
quota.memory: "64Gi"      # 64 GiB memory
```

Or percentage-based quotas:

```yaml
quota-percentage.cpu: "50"      # 50% of node's CPU
quota-percentage.memory: "75"   # 75% of node's memory
```

### Node Ordering Configuration

#### Plugin Weight

Controls the overall influence of the plugin in scheduling decisions:

```yaml
crossQuotaWeight: 10  # Default: 10, set to 0 to disable node ordering
```

#### Resource Weights

Assign importance to different resources:

```yaml
weight.cpu: 10      # High importance for CPU
weight.memory: 1    # Lower importance for memory
```

Higher weights mean the resource has more influence on the final score.

### Node-Specific Configuration

Override global quotas for specific nodes using annotations:

```bash
kubectl annotate node gpu-node-1 \
  volcano.sh/crossquota-cpu="48" \
  volcano.sh/crossquota-memory="96Gi"
```

Or use percentage overrides:

```bash
kubectl annotate node gpu-node-1 \
  volcano.sh/crossquota-percentage-cpu="75" \
  volcano.sh/crossquota-percentage-memory="80"
```

### Pod-Level Configuration

Specify scoring strategy for individual pods:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-cpu-pod
  annotations:
    volcano.sh/crossquota-scoring-strategy: "most-allocated"
spec:
  schedulerName: volcano
  containers:
  - name: app
    image: nginx
    resources:
      requests:
        cpu: "4"
        memory: "8Gi"
```

Available strategies:
- `most-allocated`: Prefer nodes with higher utilization (default)
- `least-allocated`: Prefer nodes with lower utilization

### Label Selector Configuration

Use label selectors to control which pods are subject to quota enforcement:

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota.cpu: "32"
    quota.memory: "64Gi"
    # Only apply quota to pods with these labels
    label-selector:
      matchLabels:
        quota-controlled: "true"
        team: "data-science"
      matchExpressions:
        - key: environment
          operator: In
          values:
            - production
            - staging
```

**Supported Operators**:
- `In`: Label value must be in the specified list
- `NotIn`: Label value must not be in the specified list
- `Exists`: Label key must exist (value doesn't matter)
- `DoesNotExist`: Label key must not exist

**Pod Labels Example**:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: quota-controlled-pod
  labels:
    quota-controlled: "true"
    team: "data-science"
    environment: "production"
spec:
  schedulerName: volcano
  containers:
  - name: app
    image: my-app:latest
    resources:
      requests:
        cpu: "4"
        memory: "8Gi"
```

## Usage Examples

### Example 1: Basic Multi-Resource Quota

**Scenario**: Limit CPU and memory usage of CPU pods on GPU nodes.

**Configuration**:

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota.cpu: "32"
    quota.memory: "64Gi"
```

**Result**:
- CPU pods on GPU nodes cannot exceed 32 CPU cores
- CPU pods on GPU nodes cannot exceed 64 GiB memory
- GPU pods can use full node resources

### Example 2: Percentage-Based Quotas

**Scenario**: Reserve 50% of resources for GPU tasks.

**Configuration**:

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota-percentage.cpu: "50"
    quota-percentage.memory: "50"
```

**Result**:
- CPU pods can use up to 50% of each GPU node's CPU
- CPU pods can use up to 50% of each GPU node's memory
- Configuration adapts automatically to different node sizes

### Example 3: Resource Consolidation Strategy

**Scenario**: Consolidate batch jobs on fewer nodes to preserve capacity for large GPU tasks.

**Configuration**:

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota.cpu: "32"
    quota.memory: "64Gi"
    crossQuotaWeight: 10
    weight.cpu: 10
    weight.memory: 1
```

**Pod Annotation**:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: batch-job
spec:
  template:
    metadata:
      annotations:
        volcano.sh/crossquota-scoring-strategy: "most-allocated"
    spec:
      schedulerName: volcano
      containers:
      - name: worker
        image: batch-processor:latest
        resources:
          requests:
            cpu: "4"
            memory: "8Gi"
```

**Result**:
- Batch jobs prefer nodes with higher CPU/memory utilization
- Jobs consolidate on fewer nodes
- More nodes remain available for large GPU tasks

### Example 4: Resource Distribution Strategy

**Scenario**: Distribute long-running services evenly to avoid hotspots.

**Configuration**:

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota-percentage.cpu: "60"
    quota-percentage.memory: "60"
    crossQuotaWeight: 10
    weight.cpu: 10
    weight.memory: 1
```

**Pod Annotation**:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: web-service
  annotations:
    volcano.sh/crossquota-scoring-strategy: "least-allocated"
spec:
  schedulerName: volcano
  containers:
  - name: nginx
    image: nginx:latest
    resources:
      requests:
        cpu: "2"
        memory: "4Gi"
```

**Result**:
- Services prefer nodes with lower utilization
- Even distribution across GPU nodes
- Avoids resource hotspots

### Example 5: Node-Specific Quotas

**Scenario**: Different GPU node types require different quotas.

**Node Configuration**:

```bash
# High-end GPU node - reserve more for GPU tasks
kubectl annotate node gpu-node-a100 \
  volcano.sh/crossquota-cpu="16" \
  volcano.sh/crossquota-memory="32Gi"

# Standard GPU node - allow more CPU task allocation
kubectl annotate node gpu-node-v100 \
  volcano.sh/crossquota-cpu="48" \
  volcano.sh/crossquota-memory="96Gi"
```

**Scheduler Configuration**:

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    # Default quotas (overridden by node annotations)
    quota.cpu: "32"
    quota.memory: "64Gi"
```

**Result**:
- A100 nodes preserve more resources for GPU tasks
- V100 nodes allow more CPU task allocation
- Flexible per-node configuration

### Example 6: Custom Resources

**Scenario**: Control hugepages usage on GPU nodes.

**Configuration**:

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory,hugepages-1Gi"
    quota.cpu: "32"
    quota.memory: "64Gi"
    quota.hugepages-1Gi: "8Gi"
    weight.cpu: 10
    weight.memory: 1
    weight.hugepages-1Gi: 5
```

**Result**:
- CPU pods limited to 8 GiB of 1Gi hugepages
- Hugepages usage considered in node scoring
- Comprehensive resource control

### Example 7: Label-Based Selective Quota Enforcement

**Scenario**: Apply quotas only to development and testing workloads, allow production workloads to use full resources.

**Configuration**:

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota.cpu: "16"
    quota.memory: "32Gi"
    label-selector:
      matchExpressions:
        - key: environment
          operator: In
          values:
            - development
            - testing
        - key: priority
          operator: NotIn
          values:
            - high
            - critical
```

**Development Pod** (controlled by quota):

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: dev-pod
  labels:
    environment: "development"
    priority: "normal"
spec:
  schedulerName: volcano
  containers:
  - name: app
    image: dev-app:latest
    resources:
      requests:
        cpu: "4"
        memory: "8Gi"
```

**Production Pod** (not controlled by quota):

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: prod-pod
  labels:
    environment: "production"
    priority: "high"
spec:
  schedulerName: volcano
  containers:
  - name: app
    image: prod-app:latest
    resources:
      requests:
        cpu: "8"
        memory: "16Gi"
```

**Result**:
- Development and testing pods limited to 16 CPU cores and 32 GiB memory
- Production pods can use full node resources
- Flexible policy enforcement based on workload characteristics

### Example 8: Multi-Tenancy with Team-Based Quotas

**Scenario**: Different teams have different quota limits on shared GPU nodes.

**Configuration**:

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota.cpu: "24"
    quota.memory: "48Gi"
    label-selector:
      matchLabels:
        quota-enabled: "true"
```

**Node-Specific Override for Team A**:

```bash
kubectl annotate node gpu-node-team-a \
  volcano.sh/crossquota-cpu="32" \
  volcano.sh/crossquota-memory="64Gi"
```

**Team A Pod**:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: team-a-pod
  labels:
    quota-enabled: "true"
    team: "team-a"
spec:
  schedulerName: volcano
  nodeSelector:
    team: "team-a"
  containers:
  - name: app
    image: team-a-app:latest
    resources:
      requests:
        cpu: "8"
        memory: "16Gi"
```

**Team B Pod** (without quota-enabled label):

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: team-b-pod
  labels:
    team: "team-b"
spec:
  schedulerName: volcano
  containers:
  - name: app
    image: team-b-app:latest
    resources:
      requests:
        cpu: "4"
        memory: "8Gi"
```

**Result**:
- Team A pods are quota-controlled (24 CPU cores, 48 GiB memory by default)
- Team B pods without quota-enabled label can use full resources
- Per-node quota overrides allow team-specific configurations

## Scoring Strategies Explained

### Most-Allocated Strategy

**When to Use**:
- Batch processing jobs
- Short-lived tasks
- When consolidating workloads is desired
- When preserving empty nodes for large tasks

**Formula**:
```
score = (used + requested) / quota × resourceWeight
```

**Example**:
```
Node A: CPU usage 80%, Memory usage 70% → Higher score
Node B: CPU usage 30%, Memory usage 40% → Lower score
Result: Node A is preferred
```

### Least-Allocated Strategy

**When to Use**:
- Long-running services
- High-availability workloads
- When load balancing is desired
- When avoiding resource hotspots

**Formula**:
```
score = (quota - used - requested) / quota × resourceWeight
```

**Example**:
```
Node A: CPU usage 80%, Memory usage 70% → Lower score
Node B: CPU usage 30%, Memory usage 40% → Higher score
Result: Node B is preferred
```

## Best Practices

### Configuration Best Practices

1. **Start Conservative**: Begin with restrictive quotas and gradually increase based on observed usage patterns.

   ```yaml
   quota.cpu: "16"  # Start low
   quota.memory: "32Gi"
   ```

2. **Use Percentages for Heterogeneous Clusters**: When nodes have varying capacities, percentage-based quotas adapt automatically.

   ```yaml
   quota-percentage.cpu: "50"
   quota-percentage.memory: "50"
   ```

3. **Set Appropriate Resource Weights**: Prioritize critical resources by assigning higher weights.

   ```yaml
   weight.cpu: 10      # CPU is critical
   weight.memory: 3    # Memory is somewhat important
   ```

4. **Test in Non-Production First**: Validate configuration in a test environment before deploying to production.

### Operational Best Practices

1. **Monitor Resource Utilization**: Regularly check quota usage to optimize settings.

   ```bash
   # Enable detailed logging
   kubectl logs -n volcano-system volcano-scheduler-xxx --tail=100 | grep crossquota
   ```

2. **Use Node Annotations for Special Cases**: Override global settings for specific node types.

   ```bash
   kubectl annotate node special-gpu-node \
     volcano.sh/crossquota-cpu="8"
   ```

3. **Apply Strategies Per Workload Type**: Different workloads benefit from different strategies.

   ```yaml
   # Batch jobs
   volcano.sh/crossquota-scoring-strategy: "most-allocated"
   
   # Services
   volcano.sh/crossquota-scoring-strategy: "least-allocated"
   ```

4. **Document Your Configuration**: Maintain clear documentation of quota settings and rationale.

### Strategy Selection Guidelines

| Workload Type | Recommended Strategy | Reasoning |
|---------------|---------------------|-----------|
| Batch Jobs | `most-allocated` | Consolidate on fewer nodes |
| Web Services | `least-allocated` | Distribute for reliability |
| Data Processing | `most-allocated` | Maximize node availability |
| Monitoring/Logging | `least-allocated` | Avoid concentration |
| CI/CD Jobs | `most-allocated` | Optimize resource packing |

### Label Selector Best Practices

1. **Use Opt-in Approach**: Apply quotas only to pods with specific labels to avoid surprises.

   ```yaml
   label-selector:
     matchExpressions:
       - key: quota-controlled
         operator: Exists
   ```

2. **Combine Multiple Criteria**: Layer multiple conditions for precise control.

   ```yaml
   label-selector:
     matchLabels:
       team: "data-science"
     matchExpressions:
       - key: environment
         operator: In
         values:
           - development
           - testing
   ```

3. **Exempt Critical Workloads**: Use `DoesNotExist` or `NotIn` to exclude high-priority pods.

   ```yaml
   label-selector:
     matchExpressions:
       - key: priority
         operator: NotIn
         values:
           - critical
           - high
   ```

4. **Document Label Requirements**: Maintain clear documentation of required labels for quota control.

5. **Use Admission Webhooks**: Validate that pods have required labels before admission to avoid scheduling issues.

## Monitoring and Debugging

### Enable Detailed Logging

Set log level to see detailed plugin activity:

```bash
# Edit scheduler deployment
kubectl edit deployment volcano-scheduler -n volcano-system

# Add flag: --v=4 for detailed logs, --v=5 for very detailed logs
```

### Log Messages

**Plugin Initialization** (V=3):
```
crossquota initialized. GPUPatterns=[nvidia.com/gpu], quotaResources=[cpu memory], labelSelector=quota-controlled=true
crossquota: plugin weight=10, resource weights=map[cpu:10 memory:1]
```

**Label Selector Parsing** (V=4):
```
crossquota: parsed label selector: quota-controlled=true,environment in (production,staging)
```

**Quota Calculation** (V=4):
```
crossquota: node gpu-node-1 quota cpu set to 32000 (original 64000)
```

**Scoring** (V=4):
```
Crossquota score for Task default/my-pod on node gpu-node-1 is: 75.5
```

**Detailed Resource Scoring** (V=5):
```
Task default/my-pod on node gpu-node-1 resource cpu, strategy: most-allocated, 
weight: 10, need 4000.000000, used 20000.000000, total 32000.000000, score 7.500000
```

### Common Issues and Solutions

#### Issue 1: Pods Not Scheduled

**Symptom**: Pods remain in Pending state with quota exceeded error.

**Solution**:
1. Check current quota usage:
   ```bash
   kubectl logs -n volcano-system volcano-scheduler-xxx | grep "quota exceeded"
   ```

2. Increase quota or adjust pod requests:
   ```yaml
   quota.cpu: "48"  # Increased from 32
   ```

#### Issue 2: Uneven Node Distribution

**Symptom**: All pods scheduled to same nodes.

**Solution**:
1. Verify strategy is set correctly:
   ```yaml
   volcano.sh/crossquota-scoring-strategy: "least-allocated"
   ```

2. Increase plugin weight:
   ```yaml
   crossQuotaWeight: 20  # Increased from 10
   ```

#### Issue 3: Node Ordering Not Working

**Symptom**: Plugin weight is zero or pod strategy not applied.

**Solution**:
1. Verify plugin weight is non-zero:
   ```yaml
   crossQuotaWeight: 10  # Must be > 0
   ```

2. Check pod annotation syntax:
   ```yaml
   volcano.sh/crossquota-scoring-strategy: "most-allocated"  # Correct
   ```

#### Issue 4: Pods Not Matching Label Selector

**Symptom**: Pods are not being quota-controlled despite configuration.

**Solution**:
1. Verify pod has required labels:
   ```bash
   kubectl get pod my-pod -o jsonpath='{.metadata.labels}'
   ```

2. Test label selector matching:
   ```bash
   kubectl get pods -l quota-controlled=true
   ```

3. Check label selector syntax in configuration:
   ```yaml
   label-selector:
     matchLabels:
       quota-controlled: "true"  # Must be a string
   ```

4. Review scheduler logs for label selector warnings:
   ```bash
   kubectl logs -n volcano-system volcano-scheduler-xxx | grep "label selector"
   ```

## Advanced Usage

### Multi-Tier Quotas

Configure different quotas for different GPU types:

```bash
# NVIDIA A100 nodes - strict limits
kubectl annotate node gpu-a100-* \
  volcano.sh/crossquota-cpu="16" \
  volcano.sh/crossquota-memory="32Gi"

# NVIDIA V100 nodes - moderate limits
kubectl annotate node gpu-v100-* \
  volcano.sh/crossquota-cpu="32" \
  volcano.sh/crossquota-memory="64Gi"

# NVIDIA T4 nodes - relaxed limits
kubectl annotate node gpu-t4-* \
  volcano.sh/crossquota-cpu="48" \
  volcano.sh/crossquota-memory="96Gi"
```

### Adding Multi-Resource Support

Gradually expand from CPU-only to multi-resource:

```yaml
# Step 1: Start with CPU only (backward compatible)
quota-resources: "cpu"
quota.cpu: "32"

# Step 2: Add memory
quota-resources: "cpu,memory"
quota.cpu: "32"
quota.memory: "64Gi"

# Step 3: Add more resources as needed
quota-resources: "cpu,memory,ephemeral-storage"
quota.cpu: "32"
quota.memory: "64Gi"
quota.ephemeral-storage: "100Gi"
```

## Reference

### Configuration Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `gpu-resource-names` | string | - | Comma-separated GPU resource patterns (supports regex) |
| `quota-resources` | string | `"cpu"` | Comma-separated list of resources to quota |
| `quota.<resource>` | string | - | Absolute quota for resource |
| `quota-percentage.<resource>` | string | - | Percentage quota for resource (0-100) |
| `crossQuotaWeight` | int | 10 | Plugin weight for node ordering |
| `weight.<resource>` | int | varies | Weight for resource in scoring |
| `label-selector` | object | - | Label selector for filtering pods (matchLabels and matchExpressions) |

### Annotations

| Annotation | Scope | Values | Description |
|------------|-------|--------|-------------|
| `volcano.sh/crossquota-<resource>` | Node | quantity | Override quota for specific node |
| `volcano.sh/crossquota-percentage-<resource>` | Node | 0-100 | Override quota percentage for node |
| `volcano.sh/crossquota-scoring-strategy` | Pod | `most-allocated`, `least-allocated` | Scoring strategy selection |

### Resource Weight Defaults

| Resource | Default Weight |
|----------|---------------|
| `cpu` | 10 |
| `memory` | 1 |
| Others | 1 |
