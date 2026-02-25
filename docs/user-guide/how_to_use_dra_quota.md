# DRA Quota User Guide

## Introduction

DRA (Dynamic Resource Allocation) Quota support allows you to manage specialized device resources (such as GPUs, vGPUs, or other DRA-enabled devices) at the queue level using the capacity plugin. This feature enables you to:

- Set hard limits on DRA resources per DeviceClass for each queue
- Track device counts and optional capacity dimensions
- Support elastic resource sharing and reclamation between queues

For design details, see [DRA Quota Design](../design/capacity-dra-support.md).

## Prerequisites

1. **Kubernetes Version**: v1.26 or later
2. **Feature Gates**: `DynamicResourceAllocation` must be enabled
3. **DRA Driver**: A DRA driver must be installed in your cluster (e.g., GPU driver with DRA support)
4. **Volcano**: Installed with capacity plugin enabled

## Environment Setup

### Install Volcano

Refer to [Install Guide](https://github.com/volcano-sh/volcano/blob/master/installer/README.md) to install Volcano.

### Configure Scheduler

Update the scheduler configuration to enable the capacity plugin:

```shell
kubectl edit cm -n volcano-system volcano-scheduler-configmap
```

Ensure:
- `reclaim` action is enabled
- `capacity` plugin is enabled (remove `proportion` plugin if present)

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, backfill, reclaim"
    tiers:
    - plugins:
      - name: priority
      - name: gang
      - name: conformance
    - plugins:
      - name: drf
      - name: predicates
      - name: capacity  # Use capacity plugin
      - name: nodeorder
```

### Install DRA Driver

Install a DRA driver in your cluster. For example, if using GPU resources:

```shell
# Example: Install a DRA-enabled GPU driver
kubectl apply -f https://example.com/gpu-dra-driver.yaml
```

Verify that DeviceClass resources are available:

```shell
kubectl get deviceclass
```

## Configure Queue DRA Quotas

### Basic Configuration

Create a queue with DRA quota limits:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: ml-team
spec:
  reclaimable: true
  # Traditional resource quota
  deserved:
    cpu: "100"
    memory: "200Gi"
  
  # DRA resource quota
  dra:
    capability:
      # GPU quota: maximum 8 GPUs
      gpu.example.com:
        count: 8
```

### Advanced Configuration with Capacity Dimensions

For DRA resources that support consumable capacity (e.g., vGPU with cores and memory):

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: ai-team
spec:
  reclaimable: true
  dra:
    capability:
      # vGPU quota with capacity dimensions
      hami-core-gpu.project-hami.io:
        count: 80
        capacity:
          cores: "800"
          memory: "80Gi"
    
    # Deserved quota (for elastic preemption)
    deserved:
      hami-core-gpu.project-hami.io:
        count: 40
        capacity:
          cores: "400"
          memory: "40Gi"
    
    # Guaranteed minimum (cannot be preempted)
    guarantee:
      hami-core-gpu.project-hami.io:
        count: 10
        capacity:
          cores: "100"
          memory: "10Gi"
```

### Quota Semantics

| Field | Meaning | Behavior |
|-------|---------|----------|
| `dra.capability` | Hard limit | Jobs exceeding this limit will remain pending |
| `dra.deserved` | Soft limit | Resources beyond deserved can be borrowed but may be reclaimed |
| `dra.guarantee` | Minimum guarantee | Resources within guarantee cannot be preempted |

## Submit Jobs with DRA Resources

### Create ResourceClaim

First, create a ResourceClaim requesting DRA devices:

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  name: gpu-claim
  namespace: default
spec:
  devices:
    requests:
    - name: gpu-request
      exactly:
        deviceClassName: gpu.example.com
        count: 2
        allocationMode: ExactCount
```

### Create Job Referencing ResourceClaim

Create a Volcano Job that references the ResourceClaim:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: gpu-job
spec:
  schedulerName: volcano
  queue: ml-team
  minAvailable: 1
  tasks:
  - replicas: 1
    name: worker
    template:
      spec:
        containers:
        - name: main
          image: nvidia/cuda:11.0-base
          command: ["nvidia-smi"]
          resources:
            requests:
              cpu: "2"
              memory: "4Gi"
        resourceClaims:
        - name: gpu
          resourceClaimName: gpu-claim
```

## Example Scenario: Multi-Queue Resource Sharing

### Setup

Assume you have:
- 2 queues: `queue-a` and `queue-b`
- Total cluster capacity: 8 GPUs (DeviceClass: `gpu.example.com`)

Configure queues:

```yaml
# Queue A: capability=4, deserved=2
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: queue-a
spec:
  reclaimable: true
  dra:
    capability:
      gpu.example.com:
        count: 4
    deserved:
      gpu.example.com:
        count: 2
---
# Queue B: capability=6, deserved=4
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: queue-b
spec:
  reclaimable: true
  dra:
    capability:
      gpu.example.com:
        count: 6
    deserved:
      gpu.example.com:
        count: 4
```

### Scenario Steps

1. **Submit job to queue-a requesting 4 GPUs**
   - Result: Job runs successfully (queue-a can use up to 4 GPUs)

2. **Submit job to queue-b requesting 4 GPUs**
   - Result: Queue-b reclaims its deserved resources (4 GPUs)
   - Queue-a's job is preempted down to its deserved quota (2 GPUs)
   - Queue-b's job gets 4 GPUs

3. **Final state**:
   - Queue-a: 2 GPUs (deserved quota)
   - Queue-b: 4 GPUs (deserved quota)
   - 2 GPUs remain idle

## Troubleshooting

### Job Stays Pending

**Symptom**: Job remains in Pending state

**Possible Causes**:
1. DRA quota exceeded for the queue
2. No physical devices available on nodes
3. DeviceClass not configured in queue

**Solution**:
```shell
# Check queue DRA quota
kubectl get queue <queue-name> -o yaml

# Check job events
kubectl describe job <job-name>

# Check ResourceClaim status
kubectl get resourceclaim <claim-name> -o yaml
```

### Quota Not Enforced

**Symptom**: Jobs exceed configured DRA quota

**Possible Causes**:
1. Capacity plugin not enabled
2. Queue `dra` field not configured

**Solution**:
```shell
# Verify capacity plugin is enabled
kubectl get cm -n volcano-system volcano-scheduler-configmap -o yaml

# Verify queue configuration
kubectl get queue <queue-name> -o yaml
```

## Best Practices

1. **Set Realistic Quotas**: Ensure `capability` matches or is less than physical cluster capacity
2. **Use Deserved for Sharing**: Configure `deserved` to enable elastic resource sharing
3. **Guarantee Critical Workloads**: Use `guarantee` for production workloads that should not be preempted
4. **Monitor Usage**: Regularly check queue resource usage and adjust quotas as needed

## References

- [DRA Quota Design Document](../design/capacity-dra-support.md)
- [Capacity Plugin User Guide](./how_to_use_capacity_plugin.md)
- [Kubernetes DRA Documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
