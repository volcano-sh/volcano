# ResourceStrategyFit Plugin

## Summary

The native k8s ResourceStrategyFit plug-in can only adopt one type of strategy for all resources, such as MostRequestedPriority and LeastRequestedPriority. However, in industrial practice, this design is not applicable in some scenarios. For example: in AI scenarios, we usually disperse CPU tasks in CPU machine groups to reduce hot spots. GPU tasks are gathered in GPU machine groups to reduce GPU fragmentation. Therefore, we need to expand a scheduling strategy to meet the needs of this scenario.

## Motivation

- Different resource types can be configured with different aggregation or dispersion strategies, and weights can be used to distinguish priorities

### Goals

- Different types of resources can be configured with different strategies to prioritize them in the form of weights

### Non-Goals

- None.

## Proposal

Extend one plug-ins to meet the above needs

- ResourceStrategyFit

## User Story

### Story1
- Users expect different resource allocation strategies to be applied based on resource types. For example, in PyTorch jobs, the master pod (which uses CPU) should be distributed to avoid node hotspots, while worker pods (which use GPU) should be aggregated to minimize resource fragmentation.

### Story2
- In multi-vendor GPU environments, users need to configure resource strategies for various GPU types (NVIDIA, AMD, Intel) without maintaining separate configurations for each specific GPU model. For example, a cluster with `nvidia.com/gpu-v100`, `nvidia.com/gpu-a100`, and `amd.com/gpu-mi100` should allow users to configure `nvidia.com/gpu/*` and `amd.com/gpu/*` patterns to apply consistent strategies across all GPU variants from each vendor.

## Design Details

### ResourceStrategyFit

config：
```
actions: "enqueue, allocate, backfill, reclaim, preempt"
tiers:
- plugins:
 - name: resource-strategy-fit
    arguments:
      resourceStrategyFitWeight: 10
      resources:
        nvidia.com/gpu:
          type: MostAllocated
          weight: 2
        cpu:
          type: LeastAllocated
          weight: 1
```
config description：

<table>
	<tr>
	    <th>strategy</th>
	    <th>calculation method</th>
	    <th>effect</th>  
	</tr>
	<tr>
	    <td>MostAllocated</td>
	    <td>(used + requested)/allocable</td>
	    <td>Aggregated</td>
	</tr>
	<tr>
	    <td>LeastAllocated</td>
	    <td>(allocable - (used + requested))/allocable</td>
	    <td>Dispersed</td>
	</tr>
</table>

Formula Parameters:
- `used`: Resources currently in use by running pods on the node
- `requested`: Resources requested by pods that are scheduled to the node but not yet running
- `allocable`: Total allocatable resources available on the node

node score:
```
finalScoreNode = [(weight1 * resource1) + (weight2 * resource2) + … + (weightN* resourceN)] /(weight1+weight2+ … +weightN)
```

## Syntax Rules
### Wildcard Syntax Support
- Only suffix wildcard patterns are supported (e.g., `nvidia.com/gpu/*`)
- **Regular expressions are NOT supported** (e.g., `gpu-[a-z]+`, `nvidia.com/gpu-[1-9]*`)
- Invalid patterns are filtered out during configuration parsing:
    - Single asterisk (`*`)
    - Asterisk in the middle (`vendor.*/gpu`)
    - Asterisk at the beginning (`*/gpu`)
    - Multiple asterisks (`vendor.com/**`)

### Matching Priority
1. **Exact match** takes highest priority
2. **Longest prefix match** for wildcard patterns

**Configuration Example:**
```yaml
resources:
  nvidia.com/gpu-v100:     # Exact match - highest priority
    type: MostAllocated
    weight: 3
  nvidia.com/gpu/*:        # Wildcard match - covers other NVIDIA GPUs
    type: MostAllocated
    weight: 2
  amd.com/gpu/*:           # Wildcard match for AMD GPUs
    type: LeastAllocated
    weight: 2
  cpu:
    type: LeastAllocated
    weight: 1
```

**Implementation Details:**
- **Configuration-time validation**: Invalid wildcard patterns are filtered during plugin initialization with warning logs
- **Runtime matching**: Uses O(n) prefix matching algorithm with exact match optimization
- **Backward compatibility**: Existing exact match configurations continue to work unchanged

## Performance Considerations

### Wildcard Matching Performance
- **Configuration parsing**: O(1) validation per resource pattern during plugin initialization
- **Runtime matching**: O(n) complexity where n is the number of configured resource patterns
- **Memory overhead**: Minimal additional memory for storing wildcard patterns
- **Scalability**: Suitable for typical cluster sizes with hundreds of resource types

### Risk Analysis
1. **Performance Impact**: Linear search through resource patterns may affect scheduling latency in clusters with extensive resource configurations
2. **Configuration Complexity**: Wildcard patterns may mask configuration errors, requiring careful validation
3. **Backward Compatibility**: Changes to matching logic must not affect existing exact match configurations
4. **Error Handling**: Invalid patterns are silently filtered with warning logs, which may hide configuration mistakes

### Configuration Guidelines
1. **Use exact matches for critical resources**: `cpu`, `memory`, primary GPU types
2. **Apply wildcards for resource families**: `vendor.com/gpu/*` patterns
3. **Avoid overly broad patterns**: Prefer `nvidia.com/gpu/*` over `nvidia.com/*`
4. **Test configurations**: Verify matching behavior in development environments

## Alternatives

### Binpack VS ResourceStrategyFit
If you want to use the clustering strategy for all resource types, you can choose the Binpack plugin. If you need to configure different clustering or scattering strategies for different resource types, you can choose the ResourceStrategyFit plugin. ResourceStrategyFit can also achieve the same results as Binpack by adjusting configuration parameters.