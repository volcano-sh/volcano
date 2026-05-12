# CrossQuota Plugin

## Summary

The CrossQuota plugin is a Volcano scheduler plugin that provides flexible resource quota management and intelligent node ordering for non-GPU tasks on GPU nodes. It extends the CPU Quota Plugin by supporting multiple resource types (CPU, memory, ephemeral storage, hugepages, and custom resources) and introducing advanced node scoring strategies to optimize resource utilization in heterogeneous clusters.

## Motivation

In modern Kubernetes clusters, especially those with mixed GPU and CPU workloads, several challenges arise:

1. **Resource Inefficiency**: Non-GPU tasks can occupy valuable resources on GPU nodes, potentially starving GPU tasks of necessary companion resources (CPU, memory).

2. **Limited Resource Control**: Existing solutions only control single resource types (typically CPU), lacking comprehensive multi-resource quota management.

3. **Suboptimal Node Selection**: Without intelligent node scoring, non-GPU tasks may be distributed randomly across GPU nodes, leading to:
   - Resource fragmentation
   - Inefficient resource utilization
   - Increased operational complexity

4. **Lack of Flexibility**: Different workload types require different scheduling strategies:
   - Batch jobs benefit from resource consolidation (most-allocated)
   - Long-running services benefit from resource distribution (least-allocated)

### Goals

- Support multi-resource quota management for non-GPU tasks on GPU nodes
- Provide intelligent node ordering with configurable scoring strategies
- Support both absolute and percentage-based quota configurations
- Enable node-specific quota overrides through annotations
- Support pod-level scoring strategy selection
- Maintain backward compatibility with CPU-only configurations
- Provide flexible resource weight configuration for fine-grained control

### Non-Goals

- Managing GPU resource allocation (handled by other plugins)
- Implementing complex resource reservation mechanisms
- Replacing existing DRF or binpack plugins
- Managing quotas on non-GPU nodes

## Proposal

Extend the crossquota plugin with two main capabilities:

1. **Multi-Resource Quota Management (Predicate)**:
   - Enforce resource quotas for multiple resource types on GPU nodes
   - Support both absolute quotas and percentage-based quotas
   - Allow node-specific overrides via annotations
   - Track resource usage in real-time

2. **Intelligent Node Ordering (Scoring)**:
   - Implement two scoring strategies: most-allocated and least-allocated
   - Support configurable resource weights for multi-dimensional scoring
   - Enable pod-level strategy selection via annotations
   - Calculate scores based on quota-limited resources rather than total allocatable

## User Stories

### Story 1: Resource Consolidation for Batch Jobs

As a data scientist running short-lived batch jobs, I want my CPU tasks to be consolidated on fewer GPU nodes so that:
- GPU nodes remain available for large GPU tasks
- Resource fragmentation is minimized
- Cluster efficiency is improved

**Solution**: Use `most-allocated` scoring strategy with appropriate resource weights.

### Story 2: Resource Distribution for Long-Running Services

As a platform engineer managing long-running services, I want CPU tasks evenly distributed across GPU nodes so that:
- No single node becomes a hotspot
- System stability is improved
- Resource usage is balanced

**Solution**: Use `least-allocated` scoring strategy with balanced resource weights.

### Story 3: Multi-Resource Quota Management

As a cluster administrator, I want to limit both CPU and memory usage of non-GPU tasks on GPU nodes so that:
- GPU tasks always have sufficient companion resources
- Resource allocation is predictable
- Cluster utilization is optimized

**Solution**: Configure multi-resource quotas with both CPU and memory limits.

### Story 4: Node-Specific Configuration

As a cluster administrator managing heterogeneous GPU nodes, I want to set different quotas for different node types so that:
- High-end GPU nodes reserve more resources for GPU tasks
- Low-end GPU nodes allow more non-GPU task allocation
- Configuration is flexible and manageable

**Solution**: Use node annotations to override global quota configurations.

## Design Details

### Architecture

```
┌────────────────────────────────────────────────────────────┐
│                    CrossQuota Plugin                       │
├────────────────────────────────────────────────────────────┤
│                                                            │
│  ┌───────────────────────┐  ┌──────────────────────────┐   │
│  │   Configuration       │  │   Node/Pod Detection     │   │
│  │   Parser              │  │   - isGPUNode()          │   │
│  │   - parseArguments()  │  │   - isCPUPod()           │   │
│  │   - parseQuotas()     │  │                          │   │
│  │   - parseWeights()    │  │                          │   │
│  └───────────────────────┘  └──────────────────────────┘   │
│                                                            │
│  ┌───────────────────────┐  ┌──────────────────────────┐   │
│  │   Predicate Function  │  │   Node Ordering          │   │
│  │   - Quota Enforcement │  │   - Score Calculation    │   │
│  │   - Resource Tracking │  │   - Strategy Selection   │   │
│  │   - Error Handling    │  │   - Weight Application   │   │
│  └───────────────────────┘  └──────────────────────────┘   │
│                                                            │
└────────────────────────────────────────────────────────────┘
```

### Component Details

#### 1. Configuration Parser

**Purpose**: Parse and validate plugin configuration arguments.

**Components**:
- `parseArguments()`: Parse GPU patterns and quota resources
- `parseQuotaConfigurations()`: Parse quota values (absolute and percentage)
- `parseWeightArguments()`: Parse plugin weight and resource weights

**Configuration Format**:
```yaml
arguments:
  # GPU node detection
  gpu-resource-names: "nvidia.com/gpu,amd.com/gpu"
  
  # Quota resources
  quota-resources: "cpu,memory"
  
  # Quota configuration
  quota.cpu: "32"
  quota-percentage.memory: "50"
  
  # Node ordering configuration
  crossQuotaWeight: 10
  weight.cpu: 10
  weight.memory: 1
```

#### 2. Node and Pod Detection

**GPU Node Detection**:
```go
func (cq *crossQuotaPlugin) isGPUNode(node *api.NodeInfo) bool {
    // Check if node has any GPU resources matching configured patterns
    for resName := range node.Node.Status.Allocatable {
        for _, pattern := range cq.gpuResourcePatterns {
            if pattern.MatchString(string(resName)) {
                return true
            }
        }
    }
    return false
}
```

**CPU Pod Detection**:
```go
func (cq *crossQuotaPlugin) isCPUPod(task *api.TaskInfo) bool {
    // Check if task requests any GPU resources
    for resName := range task.Resreq.ScalarResources {
        for _, pattern := range cq.gpuResourcePatterns {
            if pattern.MatchString(string(resName)) {
                return false // Has GPU resource, not a CPU pod
            }
        }
    }
    return true // No GPU resource, is a CPU pod
}
```

#### 3. Predicate Function (Quota Enforcement)

**Purpose**: Enforce resource quotas by rejecting tasks that would exceed limits.

**Algorithm**:
```
For each task T and node N:
  1. If T is not a CPU pod, skip (allow)
  2. If N is not a GPU node, skip (allow)
  3. Get quota resource Q for node N
  4. Calculate current usage U of CPU pods on N
  5. For each quota resource R:
     a. Calculate: required = T.request[R]
     b. Calculate: used = U[R]
     c. Calculate: quota = Q[R]
     d. If (used + required) > quota:
        - Return error: quota exceeded
  6. Return success (allow)
```

**Example**:
```
Node: gpu-node-1
Quota: {cpu: 32000m, memory: 64Gi}
Current Usage: {cpu: 28000m, memory: 50Gi}
New Task Request: {cpu: 6000m, memory: 10Gi}

Check CPU: 28000 + 6000 = 34000 > 32000 ❌ REJECT
Check Memory: 50 + 10 = 60 < 64 ✓ PASS

Result: Task rejected due to CPU quota exceeded
```

#### 4. Node Ordering Function (Scoring)

**Purpose**: Score nodes based on resource utilization to optimize placement.

**Scoring Strategies**:

##### Most-Allocated Strategy (Default)

Prioritizes nodes with higher resource utilization.

**Formula**:
```
resourceScore = (used + requested) / quota × resourceWeight
finalScore = (Σ resourceScore) / (Σ resourceWeight) × pluginWeight
```

**Example**:
```
Node: gpu-node-1
Quota: {cpu: 32000m, memory: 64Gi}
Current Usage: {cpu: 24000m, memory: 40Gi}
Task Request: {cpu: 4000m, memory: 8Gi}
Resource Weights: {cpu: 10, memory: 1}

CPU Score: (24000 + 4000) / 32000 × 10 = 8.75
Memory Score: (40 + 8) / 64 × 1 = 0.75
Normalized Score: (8.75 + 0.75) / (10 + 1) = 0.864
Final Score: 0.864 × 10 = 8.64
```

##### Least-Allocated Strategy

Prioritizes nodes with lower resource utilization.

**Formula**:
```
resourceScore = (quota - used - requested) / quota × resourceWeight
finalScore = (Σ resourceScore) / (Σ resourceWeight) × pluginWeight
```

**Example**:
```
Node: gpu-node-1
Quota: {cpu: 32000m, memory: 64Gi}
Current Usage: {cpu: 8000m, memory: 16Gi}
Task Request: {cpu: 4000m, memory: 8Gi}
Resource Weights: {cpu: 10, memory: 1}

CPU Score: (32000 - 8000 - 4000) / 32000 × 10 = 6.25
Memory Score: (64 - 16 - 8) / 64 × 1 = 0.625
Normalized Score: (6.25 + 0.625) / (10 + 1) = 0.625
Final Score: 0.625 × 10 = 6.25
```

**Strategy Selection**:
```go
func (cq *crossQuotaPlugin) getScoringStrategy(task *api.TaskInfo) ScoringStrategy {
    // Read from pod annotation
    strategy, exists := task.Pod.Annotations[ScoringStrategyAnnotation]
    if !exists {
        return ScoringStrategyMostAllocated // Default
    }
    return ScoringStrategy(strategy)
}
```

### Configuration Priority

The plugin follows a priority order for quota configuration:

```
1. Node Annotation (volcano.sh/crossquota-<resource>)         [Highest]
2. Node Annotation (volcano.sh/crossquota-percentage-<resource>)
3. Plugin Argument (quota.<resource>)
4. Plugin Argument (quota-percentage.<resource>)
5. Full Allocatable (no limit)                                [Lowest]
```

### Session Lifecycle

```
OnSessionOpen:
  1. Parse configuration
  2. Identify GPU nodes
  3. Calculate quotas for each GPU node
  4. Register predicate function
  5. Register node ordering function (if weight > 0)

OnSessionClose:
  1. Clear node quotas
  2. Reset tracking data
```

## Implementation

### Key Data Structures

```go
type crossQuotaPlugin struct {
    pluginArguments        framework.Arguments
    gpuResourcePatterns    []*regexp.Regexp
    quotaResourceNames     []corev1.ResourceName
    parsedQuotas           map[corev1.ResourceName]float64
    parsedQuotaPercentages map[corev1.ResourceName]float64
    NodeQuotas             map[string]*api.Resource
    pluginWeight           int
    resourceWeights        map[corev1.ResourceName]int
}
```

### Constants

```go
const (
    PluginName = "crossquota"
    
    // Configuration keys
    GPUResourceNames      = "gpu-resource-names"
    ResourcesList         = "quota-resources"
    QuotaPrefix          = "quota."
    QuotaPercentagePrefix = "quota-percentage."
    WeightPrefix         = "weight."
    CrossQuotaWeight     = "crossQuotaWeight"
    
    // Annotation keys
    AnnQuotaPrefix           = "volcano.sh/crossquota-"
    AnnQuotaPercentagePrefix = "volcano.sh/crossquota-percentage-"
    ScoringStrategyAnnotation = "volcano.sh/crossquota-scoring-strategy"
    
    // Scoring strategies
    ScoringStrategyMostAllocated  = "most-allocated"
    ScoringStrategyLeastAllocated = "least-allocated"
    
    // Defaults
    DefaultCrossQuotaWeight = 10
    DefaultCPUWeight        = 10
    DefaultMemoryWeight     = 1
)
```

### Main Functions

#### predicateFn

```go
func (cq *crossQuotaPlugin) predicateFn(task *api.TaskInfo, node *api.NodeInfo) error {
    // Filter: only CPU pods on GPU nodes
    if !cq.isCPUPod(task) || !cq.isGPUNode(node) {
        return nil
    }
    
    // Get quota for this node
    quotaResource := cq.NodeQuotas[node.Name]
    
    // Calculate current usage by CPU pods
    currentUsage := calculateCurrentUsage(node, cq)
    
    // Check each quota resource
    for _, rn := range cq.quotaResourceNames {
        required := getTaskRequest(task, rn)
        used := getNodeUsed(currentUsage, rn)
        quota := getNodeAllocatable(quotaResource, rn)
        
        if used + required > quota {
            return api.NewFitError(task, node, 
                fmt.Sprintf("%s quota exceeded", rn))
        }
    }
    
    return nil
}
```

#### nodeOrderFn

```go
func nodeOrderFn(task *api.TaskInfo, node *api.NodeInfo) (float64, error) {
    // Filter: only CPU pods on GPU nodes
    if !cq.isCPUPod(task) || !cq.isGPUNode(node) {
        return 0, nil
    }
    
    score := cq.calculateScore(task, node)
    return score, nil
}
```

#### calculateScore

```go
func (cq *crossQuotaPlugin) calculateScore(task *api.TaskInfo, node *api.NodeInfo) float64 {
    strategy := cq.getScoringStrategy(task)
    quotaResource := cq.NodeQuotas[node.Name]
    currentUsage := calculateCurrentUsage(node, cq)
    
    score := 0.0
    totalWeight := 0
    
    for _, resource := range cq.quotaResourceNames {
        request := getTaskRequest(task, resource)
        used := getNodeUsed(currentUsage, resource)
        total := getNodeAllocatable(quotaResource, resource)
        weight := cq.resourceWeights[resource]
        
        resourceScore := calculateResourceScore(strategy, request, used, total)
        score += resourceScore × weight
        totalWeight += weight
    }
    
    return (score / totalWeight) × cq.pluginWeight
}
```
