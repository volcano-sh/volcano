# Volcano Shard Policy Framework

This directory contains the pluggable policy framework for Volcano's NodeShard controller, which enables different node assignment strategies for schedulers.

## Background

This framework was implemented to address two key issues:

**Issue #4722 - Fast Scheduling for AI Agent Workloads**
- Agent tasks require ultra-fast scheduling at scale with minimal latency
- Proposed dedicated agent scheduler operating alongside Volcano's main scheduler
- Node-level sharding with dynamic assignment based on cluster state

**Issue #4944 - Pluggable Shard Policies**
- Original NodeShard implementation only supported allocation-rate strategy
- Code was tightly coupled, making it difficult to extend
- Required: refactor to plugin architecture + add capability and warmup strategies

## Overview

The policy framework allows administrators to configure different sharding strategies (policies) for schedulers without modifying core code. Each policy implements the `ShardPolicy` interface and determines how nodes should be assigned to schedulers.

This enables key use cases like:
- ✅ Fast Agent scheduling with guaranteed capacity headroom
- ✅ Warmup pool prioritization for minimal task startup latency
- ✅ Traditional batch workload optimization
- ✅ Multiple schedulers with different strategies coexisting

## Architecture

### Core Components

1. **ShardPolicy Interface** (`interface.go`)
   - Defines the contract that all policies must implement
   - Methods: `Name()`, `Initialize()`, `Calculate()`, `Cleanup()`

2. **Policy Registry** (`registry.go`)
   - Thread-safe registration and lookup mechanism
   - Policies are registered at initialization via `init()` functions

3. **Arguments System** (`arguments.go`)
   - Type-safe parameter parsing (int, float64, bool, string)
   - Follows Volcano's scheduler plugin pattern

4. **Policy Factory** (`factory.go`)
   - Registers all built-in policies at startup
   - Extensible for custom policies

## Built-in Policies

### 1. Allocation-Rate Policy (`allocationrate/`)

**Purpose**: Assigns nodes based on CPU utilization ranges (default behavior from original implementation).

**Configuration**:
```bash
--scheduler-configs="volcano:volcano:allocation-rate:2:100:minCPUUtil=0.0,maxCPUUtil=0.6,preferWarmupNodes=false"
```

**Parameters**:
- `minCPUUtil` (float64): Minimum CPU utilization threshold (0.0-1.0)
- `maxCPUUtil` (float64): Maximum CPU utilization threshold (0.0-1.0)
- `preferWarmupNodes` (bool): Prioritize warmup nodes
- `minNodes` (int): Minimum nodes to assign
- `maxNodes` (int): Maximum nodes to assign

**Behavior**:
- Filters nodes within CPU utilization range [minCPUUtil, maxCPUUtil]
- Sorts by utilization (higher first) within warmup preference groups
- Selects nodes within min/max constraints

### 2. Capability Policy (`capability/`)

**Purpose**: Assigns nodes with sufficient capacity headroom (e.g., current utilization + 30% ≤ 100%).

**Configuration**:
```bash
--scheduler-configs="agent:agent:capability:2:50:maxCapacityPercent=0.30"
```

**Parameters**:
- `maxCapacityPercent` (float64): Maximum additional capacity percentage (0.0-1.0)
- `preferWarmupNodes` (bool): Prioritize warmup nodes
- `minNodes` (int): Minimum nodes to assign
- `maxNodes` (int): Maximum nodes to assign

**Behavior**:
- Filters nodes where `CPUUtilization ≤ (1.0 - maxCapacityPercent)`
- Sorts by lowest utilization (most headroom)
- Ensures scheduler has room to allocate resources

### 3. Warmup Policy (`warmup/`)

**Purpose**: Prioritizes nodes with warmup labels for faster scheduling.

**Configuration**:
```bash
--scheduler-configs="warmup-sched:warmup:warmup:5:100:allowNonWarmup=true"
```

**Parameters**:
- `warmupLabel` (string): Label key to identify warmup nodes (default: "node.volcano.sh/warmup")
- `warmupLabelValue` (string): Label value for warmup nodes (default: "true")
- `allowNonWarmup` (bool): Fallback to non-warmup nodes if needed
- `minNodes` (int): Minimum nodes to assign
- `maxNodes` (int): Maximum nodes to assign

**Behavior**:
- Prioritizes warmup nodes first
- Sorts by lowest utilization within each group
- Falls back to non-warmup nodes if `allowNonWarmup=true` and minNodes not met

## Configuration Format

### Old Format (Backwards Compatible)
```bash
--scheduler-configs="name:type:minUtil:maxUtil:preferWarmup:minNodes:maxNodes"
```
**Example**: `volcano:volcano:0.0:0.6:false:2:100`

Automatically converted to `allocation-rate` policy.

### New Format
```bash
--scheduler-configs="name:type:policy:minNodes:maxNodes[:key=val,key=val]"
```
**Examples**:
- `volcano:volcano:allocation-rate:2:100:minCPUUtil=0.0,maxCPUUtil=0.6`
- `agent:agent:capability:2:50:maxCapacityPercent=0.30`
- `warmup-sched:warmup:warmup:5:100:allowNonWarmup=true`

### Mixed Configuration
Both formats can be used simultaneously:
```bash
--scheduler-configs="volcano:volcano:0.0:0.6:false:2:100,agent:agent:capability:2:50:maxCapacityPercent=0.30"
```

## Creating Custom Policies

### 1. Implement the ShardPolicy Interface

```go
package mypolicy

import "volcano.sh/volcano/pkg/controllers/sharding/policy"

const PolicyName = "my-policy"

type myPolicy struct {
    // Configuration fields
    param1 float64
    param2 int
}

func New() policy.ShardPolicy {
    return &myPolicy{
        // Default values
        param1: 0.5,
        param2: 10,
    }
}

func (p *myPolicy) Name() string {
    return PolicyName
}

func (p *myPolicy) Initialize(args policy.Arguments) error {
    args.GetFloat64(&p.param1, "param1")
    args.GetInt(&p.param2, "param2")

    // Validate parameters
    if p.param1 < 0 || p.param1 > 1 {
        return fmt.Errorf("invalid param1: must be in [0, 1]")
    }

    return nil
}

func (p *myPolicy) Calculate(ctx *policy.PolicyContext) (*policy.PolicyResult, error) {
    // Implement your node selection logic
    selectedNodes := []string{}

    for _, node := range ctx.AllNodes {
        // Skip assigned nodes
        if _, assigned := ctx.AssignedNodes[node.Name]; assigned {
            continue
        }

        // Your selection logic here
        metrics := ctx.NodeMetrics[node.Name]
        if metrics != nil && /* your condition */ {
            selectedNodes = append(selectedNodes, node.Name)
        }
    }

    return &policy.PolicyResult{
        SelectedNodes: selectedNodes,
        Reason:        "Your policy description",
        Metadata:      map[string]interface{}{"key": "value"},
    }, nil
}

func (p *myPolicy) Cleanup() {
    // Release resources if needed
}
```

### 2. Register Your Policy

```go
// In factory.go or your plugin's init function
func init() {
    policy.RegisterPolicy(mypolicy.PolicyName, mypolicy.New)
}
```

### 3. Configure and Use

```bash
--scheduler-configs="my-scheduler:volcano:my-policy:2:100:param1=0.7,param2=15"
```

## Testing

Each policy has comprehensive unit tests:
- `allocationrate/allocationrate_test.go`
- `capability/capability_test.go`
- `warmup/warmup_test.go`
- `registry_test.go`

Run tests:
```bash
go test ./pkg/controllers/sharding/policy/...
```

## Benefits

1. **Extensibility**: Add new policies without modifying core code
2. **Testability**: Each policy tested in isolation
3. **Maintainability**: Clear separation of concerns
4. **Backwards Compatibility**: Old configs still work
5. **Flexibility**: Different schedulers can use different policies
6. **Type Safety**: Strong interfaces prevent runtime errors

## Migration Notes

- No breaking changes - old config format still works
- Default policy is "allocation-rate" if not specified
- Deprecation warnings logged for old format
- All three policies (allocation-rate, capability, warmup) available immediately

## Example Configurations

### Single Scheduler with Allocation-Rate
```bash
--scheduler-configs="volcano:volcano:allocation-rate:2:100:minCPUUtil=0.0,maxCPUUtil=0.6"
```

### Multiple Schedulers with Different Policies
```bash
--scheduler-configs="volcano:volcano:allocation-rate:2:100:minCPUUtil=0.0,maxCPUUtil=0.6,agent:agent:capability:2:50:maxCapacityPercent=0.30"
```

### Warmup Pool Scheduler
```bash
--scheduler-configs="warmup-pool:volcano:warmup:5:100:allowNonWarmup=false"
```

### AI Agent Workload Configuration (Issue #4722 Use Case)

For fast Agent scheduling with minimal latency:

```bash
# Traditional batch scheduler (Volcano) - allocation rate
# Handles nodes with 0-60% utilization for traditional batch workloads
--scheduler-configs="volcano:volcano:allocation-rate:2:100:minCPUUtil=0.0,maxCPUUtil=0.6"

# Agent scheduler - capability-based
# Restricted to 30% of node capacity to ensure fast response times
# Only selects nodes with sufficient headroom (utilization ≤ 70%)
--scheduler-configs="agent-scheduler:agent:capability:2:50:maxCapacityPercent=0.30"

# Warmup pool scheduler - warmup-based
# Only schedule on pre-warmed nodes for ultra-low latency
# No fallback to non-warmup nodes (allowNonWarmup=false)
--scheduler-configs="warmup-agent:agent:warmup:5:20:allowNonWarmup=false"
```

**This configuration achieves:**
- Fast scheduling via capability policy ensuring headroom for Agent tasks
- Warmup pools for minimal task startup latency
- Dynamic shard assignment via pluggable policies
- Coexistence of multiple schedulers with different strategies
- Resource isolation between batch and Agent workloads

## References

- **Primary Issue**: [#4944 - Make NodeShard sharding policies pluggable](https://github.com/volcano-sh/volcano/issues/4944)
- **Parent Issue**: [#4722 - Fast scheduling for AI Agent workloads](https://github.com/volcano-sh/volcano/issues/4722)
- **Related PRs**: #4777 (initial NodeShard), #4804 (agent scheduler), #4848, #4855 (node sharding implementations)
- Design follows Volcano's scheduler plugin framework pattern
- Inspired by Kubernetes scheduler framework
