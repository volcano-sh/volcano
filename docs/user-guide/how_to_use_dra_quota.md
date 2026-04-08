# DeviceClass Quota User Guide

## Overview

Volcano supports queue-level quota control for Kubernetes Dynamic Resource Allocation (DRA) resources through the capacity plugin.

You can use it to:

- limit how many DRA devices a queue may consume
- limit consumable DRA capacity such as virtual GPU cores or memory
- combine DRA quota with existing `capability`, `deserved`, and `guarantee` semantics

This feature does not introduce a new queue API. DRA quota is configured through the same queue resource fields already used by the capacity plugin.

For design background, see [DeviceClass Quota Design](../design/capacity-dra-support.md).

## Prerequisites

1. Kubernetes with DRA enabled
2. A DRA-capable driver installed in the cluster
3. Volcano installed with the `capacity` plugin enabled

## Enable Capacity Plugin

Update the scheduler configuration:

```shell
kubectl edit cm -n volcano-system volcano-scheduler-configmap
```

Make sure:

- `capacity` plugin is enabled
- `reclaim` action is enabled if you want queue sharing and reclaim behavior

Example:

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
      - name: capacity
      - name: nodeorder
```

## Queue Key Formats

Use the following keys in `spec.capability`, `spec.deserved`, and `spec.guarantee`.

| Key Format | Example | Meaning |
|------------|---------|---------|
| `deviceclass/<DeviceClass>` | `deviceclass/gpu.nvidia.com: "8"` | device count quota |
| `<dim>.deviceclass/<DeviceClass>` | `cores.deviceclass/hami-core-gpu.project-hami.io: "800"` | consumable-capacity quota |

These keys can coexist with normal resources such as `cpu`, `memory`, and `nvidia.com/gpu`.

## Queue Configuration Examples

### Whole-Card GPU Queue

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: gpu-team
spec:
  reclaimable: true
  capability:
    cpu: "64"
    memory: "256Gi"
    "deviceclass/gpu.nvidia.com": "8"
  deserved:
    "deviceclass/gpu.nvidia.com": "4"
  guarantee:
    "deviceclass/gpu.nvidia.com": "1"
```

### vGPU Queue by Consumable Capacity

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: vgpu-team
spec:
  reclaimable: true
  capability:
    "cores.deviceclass/hami-core-gpu.project-hami.io": "800"
    "memory.deviceclass/hami-core-gpu.project-hami.io": "320Gi"
  deserved:
    "cores.deviceclass/hami-core-gpu.project-hami.io": "400"
  guarantee:
    "cores.deviceclass/hami-core-gpu.project-hami.io": "100"
```

### Mixed Queue

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: ml-team
spec:
  reclaimable: true
  capability:
    cpu: "100"
    memory: "200Gi"
    "nvidia.com/gpu": "4"
    "deviceclass/gpu.nvidia.com": "8"
    "cores.deviceclass/hami-core-gpu.project-hami.io": "800"
    "memory.deviceclass/hami-core-gpu.project-hami.io": "320Gi"
```

## Quota Semantics

| Field | Meaning |
|-------|---------|
| `capability` | hard limit |
| `deserved` | soft share that can be borrowed or reclaimed around |
| `guarantee` | minimum protected share |

These semantics are the same for DRA and non-DRA resources.

## Submit Jobs with DRA Resources

### Option 1: ResourceClaimTemplate

This is the preferred mode when each Pod should get its own claim.

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: gpu-template
  namespace: default
spec:
  spec:
    devices:
      requests:
      - name: gpu-request
        exactly:
          deviceClassName: gpu.nvidia.com
          count: 2
          allocationMode: ExactCount
---
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: gpu-job
spec:
  schedulerName: volcano
  queue: gpu-team
  minAvailable: 1
  tasks:
  - replicas: 1
    name: worker
    template:
      spec:
        resourceClaims:
        - name: gpu
          resourceClaimTemplateName: gpu-template
        containers:
        - name: main
          image: nvidia/cuda:11.0-base
          command: ["nvidia-smi"]
          resources:
            requests:
              cpu: "2"
              memory: "4Gi"
            claims:
            - name: gpu
```

### Option 2: Direct ResourceClaim

This is useful when multiple Pods need to reference the same shareable claim.

```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaim
metadata:
  name: shared-gpu
  namespace: default
spec:
  devices:
    requests:
    - name: gpu-request
      exactly:
        deviceClassName: gpu.nvidia.com
        count: 2
        allocationMode: ExactCount
  shareable: true
---
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: shared-gpu-job
spec:
  schedulerName: volcano
  queue: gpu-team
  minAvailable: 2
  tasks:
  - replicas: 2
    name: worker
    template:
      spec:
        resourceClaims:
        - name: gpu
          resourceClaimName: shared-gpu
        containers:
        - name: main
          image: nvidia/cuda:11.0-base
          command: ["nvidia-smi"]
          resources:
            requests:
              cpu: "2"
              memory: "4Gi"
            claims:
            - name: gpu
```

When multiple Pods reference the same shareable `ResourceClaim`, quota is counted once per claim.

## Queue Sharing Example

The same `capability`, `deserved`, and `guarantee` model applies to DRA resources.

Example:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: queue-a
spec:
  reclaimable: true
  capability:
    "deviceclass/gpu.nvidia.com": "6"
  deserved:
    "deviceclass/gpu.nvidia.com": "3"
  guarantee:
    "deviceclass/gpu.nvidia.com": "1"
---
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: queue-b
spec:
  reclaimable: true
  capability:
    "deviceclass/gpu.nvidia.com": "6"
  deserved:
    "deviceclass/gpu.nvidia.com": "5"
  guarantee:
    "deviceclass/gpu.nvidia.com": "2"
```

This configuration allows the two queues to share the same DRA resource pool using normal capacity-plugin semantics.

## Observe Queue Usage

You can inspect queue allocation state with:

```shell
kubectl get queue -o yaml
```

DRA usage appears in `status.allocated`, for example:

```yaml
status:
  allocated:
    "deviceclass/gpu.nvidia.com": "4"
    "cores.deviceclass/hami-core-gpu.project-hami.io": "400"
```

## Current Scope and Limitations

This user guide focuses on the main supported quota model:

- exact device-count quota
- exact consumable-capacity quota
- direct `ResourceClaim`
- `ResourceClaimTemplate`

The following request styles are not the main quota-accounting target:

- `allocationMode: All`
- `FirstAvailable`

If your environment relies heavily on those modes, evaluate behavior carefully before treating them as strict queue quota signals.

## Best Practices

1. Use `ResourceClaimTemplate` for per-Pod allocation lifecycle
2. Use direct `ResourceClaim` only when you intentionally want sharing
3. Keep queue `capability` aligned with your actual cluster resource strategy
4. Use `deserved` and `guarantee` to express team sharing rules, not just hard caps
5. For mixed environments, configure DRA keys alongside normal resource keys in the same queue

## References

- [DeviceClass Quota Design](../design/capacity-dra-support.md)
- [Capacity Plugin User Guide](./how_to_use_capacity_plugin.md)
- [Kubernetes DRA Documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
