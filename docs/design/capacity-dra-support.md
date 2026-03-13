# DRA Resource Quota Support in Capacity Plugin

## Motivation

The current Volcano capacity plugin manages CPU, Memory, and ScalarResources quotas. To support Kubernetes Dynamic Resource Allocation (DRA), we need to extend the capacity plugin to manage DRA resource quotas (e.g., GPU, vGPU) for queues.

DRA is a Kubernetes feature that provides a flexible way for requesting, configuring, and sharing specialized devices. It introduces new resource types (DeviceClass, ResourceClaim) that differ from traditional resources.

## Goals

1. Enable queue-level quota management for DRA resources
2. Track total device counts and optional capacity dimensions
3. Maintain consistency with existing queue quota semantics
4. Support hierarchical queues quota enforcement

### Non-Goals

- Managing physical device allocation (handled by DRA drivers and predicates plugin)
- Understanding DeviceClass semantics (capacity plugin only does numeric quota tracking)

## Design Principles

1. **Per-DeviceClass quota tracking**: Track quotas separately for each DeviceClass (e.g., nvidia-h100, hami-vgpu)
2. **Generic resource tracking**: Handle device count and optional capacity dimensions
3. **Follow existing Queue patterns**: Use similar structure to traditional resource quotas
4. **Support hierarchical queues** as an advanced feature. *(Note: Elastic preemption traits like `deserved` and `guarantee` for DRA resources are not supported by the capacity plugin directly and must be implemented in other plugins.)*

---

## Proposal

### Feature Scenarios

| Scenario | Capability | Prerequisites |
|----------|------------|---------------|
| **Basic** | DRA quota enforcement (`dra.capability`) | - |
| **Advanced** | Hierarchical queues (inheritance & constraints) | Basic |

### Queue CRD Extension

Add a new `dra` field to `QueueSpec`:

```go
type QueueSpec struct {
    // ... existing fields (capability, deserved, guarantee) ...
    
    // DRA defines DRA resource quotas for the queue
    // +optional
    DRA *DRAQuota `json:"dra,omitempty"`
}

// DRAQuota defines DRA resource quotas per DeviceClass
type DRAQuota struct {
    // Capability defines upper limit per DeviceClass (basic scenario)
    // Key: DeviceClass name (e.g., "nvidia-h100", "hami-core-gpu.project-hami.io")
    // +optional
    Capability map[string]DRAResourceQuota `json:"capability,omitempty"`
    // 
    // Note: Deserved and Guarantee fields are defined for future extensibility but are NOT 
    // actively used by the capacity plugin for elastic preemption today.
    
    // Deserved defines deserved quota per DeviceClass
    // +optional
    Deserved map[string]DRAResourceQuota `json:"deserved,omitempty"`
    
    // Guarantee defines minimum guaranteed quota per DeviceClass 
    // +optional
    Guarantee map[string]DRAResourceQuota `json:"guarantee,omitempty"`
}

// DRAResourceQuota defines quota values for a specific DeviceClass
type DRAResourceQuota struct {
    // Count is the device count limit for this DeviceClass (required)
    Count int64 `json:"count"`
    
    // Capacity defines consumable capacity limits (optional)
    // Key: capacity dimension name (e.g., "cores", "memory")
    // This is used when DRA resources support consumable capacity
    // +optional
    Capacity map[string]resource.Quantity `json:"capacity,omitempty"`
}
```

### Usage Example

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: ml-team
spec:
  # Traditional resource quota
  capability:
    cpu: "100"
    memory: "200Gi"
  
  # DRA resource quota (new)
  dra:
    # Basic: device count limit per DeviceClass
    capability:
      # Physical GPU quota
      nvidia-h100:
        count: 8
      # vGPU quota with capacity dimensions
      hami-core-gpu.project-hami.io:
        count: 80
        capacity:
          cores: "800"
          memory: "80Gi"
    # Note: Using deserved and guarantee currently has no effect on elasticity in the capacity plugin.
    # They may be utilized by other plugins in the future for DRA elasticity support.
    deserved:
      hami-core-gpu.project-hami.io:
        count: 40
    guarantee:
      hami-core-gpu.project-hami.io:
        count: 10
```

### Quota Semantics

| Field | Semantics | Enforcement |
|-------|-----------|-------------|
| `dra.capability` | Hard limit, cannot exceed | Allocation rejected if quota would be exceeded |
| `dra.deserved` | Future extensibility | Currenly NOT supported for elasticity/preemption in capacity plugin |
| `dra.guarantee` | Future extensibility | Currently NOT supported for elasticity/preemption in capacity plugin |

---

## Implementation Overview

### Resource Aggregation

The capacity plugin aggregates DRA requests from all ResourceClaims in a Pod, grouped by DeviceClass:
- Sum up device counts per DeviceClass across all claims
- Sum up capacity dimensions per DeviceClass across all claims

Example:
```
Pod has 2 claims:
  Claim 1: DeviceClass=nvidia-h100, count=2
  Claim 2: DeviceClass=hami-core-gpu, count=1, cores=30, memory=4Gi
  Claim 3: DeviceClass=hami-core-gpu, count=1, cores=20, memory=2Gi
  
Aggregated request:
  nvidia-h100: {count=2}
  hami-core-gpu: {count=2, cores=50, memory=6Gi}
```

### Quota Checking

When scheduling a task:
1. For each DeviceClass in the task's DRA request:
   - Check if `allocated[deviceClass].count + request[deviceClass].count <= capability[deviceClass].count`
   - For each capacity dimension, check if `allocated[deviceClass][dim] + request[deviceClass][dim] <= capability[deviceClass][dim]`
2. If hierarchical queues enabled, recursively check parent queues

### Relationship with Predicates Plugin

| Component | Responsibility |
|-----------|----------------|
| **Capacity Plugin** | Queue quota management (logical limit) |
| **Predicates Plugin** | Node filtering, physical resource availability check |

The two are decoupled:
- Capacity plugin ensures queue quota is not exceeded
- Predicates plugin (via K8s DRA) ensures nodes have sufficient physical resources

---

## Upgrade Strategy

1. Add `dra` field to Queue CRD with `+optional` tag
2. Existing queues continue to work without modification
3. Scheduler gracefully handles queues without `dra` field (no DRA quota enforcement)
4. Users can gradually enable DRA quotas per queue

## References

- [Kubernetes DRA Documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
- [DRA Consumable Capacity KEP](https://github.com/kubernetes/enhancements/tree/master/keps/sig-scheduling/5075-dra-consumable-capacity)
- [Volcano Capacity Plugin](./capacity-scheduling.md)
- [Volcano Hierarchical Queue](./hierarchical-queue-on-capacity-plugin.md)
- [Detailed Development Design](./capacity-dra-dev-design.md)
