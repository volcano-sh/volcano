# Volcano Sharding Examples

This directory contains example configurations for using the Volcano Sharding Controller feature.

## Examples Overview

| Example | Description | Use Case |
|---------|-------------|----------|
| [basic-nodeshard.yaml](#basic-nodeshard) | Simple NodeShard configuration | Single scheduler with node subset |
| [multi-scheduler-deployment.yaml](#multi-scheduler-deployment) | Two schedulers with separate shards | Mixed workload types |
| [agent-volcano-sharding.yaml](#agent-volcano-sharding) | Agent + Volcano scheduler setup | Agentic AI + batch workloads |
| [soft-mode-example.yaml](#soft-mode-example) | Soft sharding mode configuration | Flexible scheduling with fallback |

## Prerequisites

- Volcano v1.14 or later installed
- Kubernetes cluster with multiple nodes
- NodeShard CRD installed (included with Volcano)

## Basic NodeShard

**File**: `basic-nodeshard.yaml`

Simple example showing how to create a NodeShard for a single scheduler:

```bash
kubectl apply -f basic-nodeshard.yaml
```

This creates a NodeShard named "volcano" with three nodes. The Volcano scheduler configured with `--scheduler-sharding-name=volcano` will use these nodes for scheduling.

**Verify**:
```bash
kubectl get nodeshard volcano
kubectl describe nodeshard volcano
```

## Multi-Scheduler Deployment

**File**: `multi-scheduler-deployment.yaml`

Demonstrates running two schedulers (Volcano and Agent) with separate node shards in hard isolation mode.

**Deploy**:
```bash
kubectl apply -f multi-scheduler-deployment.yaml
```

This creates:
- Two NodeShards (volcano, agent-scheduler)
- Two scheduler deployments with hard sharding mode
- Separate node assignments for each scheduler

**Use Case**: Isolate batch workloads from latency-sensitive Agentic AI workloads.

**Verify**:
```bash
kubectl get nodeshards
kubectl get pods -n volcano-system
```

## Agent + Volcano Sharding

**File**: `agent-volcano-sharding.yaml`

Complete setup for running Agent scheduler alongside Volcano scheduler with dynamic node assignment.

**Deploy**:
```bash
kubectl apply -f agent-volcano-sharding.yaml
```

**Features**:
- Agent scheduler for high-utilization nodes (Agentic AI workloads)
- Volcano scheduler for low-utilization nodes (batch workloads)
- Hard isolation to prevent scheduling conflicts

**Test with sample workloads**:
```bash
# Deploy a batch job (uses Volcano scheduler)
kubectl apply -f sample-batch-job.yaml

# Deploy an agent workload (uses Agent scheduler)
kubectl apply -f sample-agent-pod.yaml
```

## Soft Mode Example

**File**: `soft-mode-example.yaml`

Shows soft sharding mode where schedulers prefer their shard but can use other nodes if needed.

**Deploy**:
```bash
kubectl apply -f soft-mode-example.yaml
```

**Use Case**: Flexible scheduling when strict isolation isn't required, with graceful fallback when shard nodes are full.

## Customizing Examples

### Changing Node Names

Update the `nodesDesired` field in NodeShard specs:

```yaml
spec:
  nodesDesired:
    - your-node-1
    - your-node-2
    - your-node-3
```

Get your cluster's node names:
```bash
kubectl get nodes -o name
```

### Adjusting Sharding Mode

Change the scheduler deployment args:

```yaml
args:
  - --scheduler-sharding-mode=soft  # or 'hard' or 'none'
  - --scheduler-sharding-name=volcano
```

### Scaling Schedulers

Adjust replica count for higher throughput:

```yaml
spec:
  replicas: 2  # Run 2 scheduler instances
```

> **Note**: Multiple replicas require leader election. Ensure `--leader-elect=true` is set.

## Troubleshooting

### Pods Not Scheduling

Check if the scheduler is using the correct shard:

```bash
kubectl logs -n volcano-system deployment/volcano-scheduler | grep shard
```

Verify NodeShard exists and has nodes:

```bash
kubectl get nodeshard volcano -o yaml
```

### NodeShard Status Not Updating

Check scheduler permissions:

```bash
kubectl auth can-i update nodeshards --as=system:serviceaccount:volcano-system:volcano-scheduler
```

View scheduler logs for errors:

```bash
kubectl logs -n volcano-system deployment/volcano-scheduler --tail=50
```

### Scheduling Conflicts

If using soft mode and experiencing conflicts, switch to hard mode:

```yaml
args:
  - --scheduler-sharding-mode=hard
```

## Next Steps

- Read the [Sharding Controller User Guide](../docs/user-guide/how_to_use_sharding_controller.md)
- Review the [Sharding Controller Design](../docs/design/sharding_controller.md)
- Explore [Agent Scheduler Documentation](../docs/design/agent-scheduler.md)

## Contributing

Found an issue or have an improvement? Please open an issue or pull request in the [Volcano repository](https://github.com/volcano-sh/volcano).
