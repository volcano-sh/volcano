# GPU Exclusivity User Guide

## Overview

GPU exclusivity ensures that pods matching specific label rules get **dedicated physical GPUs** that are not shared with other rule-matching pods. This is useful for workloads like distributed training that require isolated GPU access for performance or correctness.

GPU exclusivity is built into the `deviceshare` plugin and works alongside vGPU scheduling. Non-matching pods continue to share GPUs normally.

## Prerequisites

* Volcano >= 1.11
* vGPU scheduling enabled (`deviceshare.VGPUEnable: true`)
* [volcano-vgpu-device-plugin](https://github.com/Project-HAMi/volcano-vgpu-device-plugin) deployed

## Configuration

### Scheduler ConfigMap

Add `deviceshare.GPUExclusiveRules` to the deviceshare plugin arguments in the scheduler configmap:

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
      - name: proportion
      - name: nodeorder
      - name: binpack
      - name: deviceshare
        arguments:
          deviceshare.VGPUEnable: true
          deviceshare.GPUExclusiveRules:
            - workloadType: training
```

After updating the configmap, restart the scheduler:

```bash
kubectl delete pod -n volcano-system -l app=volcano-scheduler
```

### Rule Format

Each rule is a map of **label key-value pairs**. A pod matches a rule if it carries **ALL** specified labels with matching values.

**Single-label rule** — match pods with `workloadType=training`:

```yaml
deviceshare.GPUExclusiveRules:
  - workloadType: training
```

**Multi-label rule** — match pods with both `workloadType=batch` AND `priority=high`:

```yaml
deviceshare.GPUExclusiveRules:
  - workloadType: batch
    priority: high
```

**Multiple rules** — each rule is evaluated independently:

```yaml
deviceshare.GPUExclusiveRules:
  - workloadType: training
  - workloadType: batch
    priority: high
  - workloadType: serving
    tier: gpu-exclusive
```

## Usage

### Creating Pods with GPU Exclusivity

Add the matching labels to your pod spec. No additional annotations are required.

**Example: Training pod with exclusive GPUs**

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: training-job
  labels:
    workloadType: training
spec:
  schedulerName: volcano
  containers:
  - name: trainer
    image: nvidia/cuda:12.0-base
    command: ["sleep", "3600"]
    resources:
      limits:
        volcano.sh/vgpu-number: "4"
        volcano.sh/vgpu-memory: "16384"
```

**Example: Inference pod without exclusivity (no matching labels)**

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: inference-service
spec:
  schedulerName: volcano
  containers:
  - name: server
    image: nvidia/cuda:12.0-base
    command: ["sleep", "3600"]
    resources:
      limits:
        volcano.sh/vgpu-number: "1"
        volcano.sh/vgpu-memory: "4096"
```

### Behavior

| Scenario | Result |
|----------|--------|
| Pod matches a rule | Gets exclusive physical GPUs — no other rule-matching pod can use them |
| Pod does not match any rule | Scheduled normally, can share GPUs with any pod |
| Pod partially matches a multi-label rule | Not matched — all labels in the rule must match |
| All exclusive GPUs on a node are reserved | Rule-matching pod stays Pending until GPUs are freed |
| No rules configured | Plugin is a no-op — all pods share GPUs normally |

### Cross-Rule Exclusivity

When multiple rules are configured, pods from **different rules also get exclusive GPUs from each other**. For example, with rules for `workloadType=training` and `workloadType=batch, priority=high`:

* Training pods get GPUs exclusive from all other rule-matching pods
* High-priority batch pods get GPUs exclusive from all other rule-matching pods
* Inference pods (no matching labels) can share any GPU

### Verifying GPU Assignments

Check the GPU assignment annotation on a scheduled pod:

```bash
kubectl get pod <pod-name> -o jsonpath='{.metadata.annotations.volcano\.sh/vgpu-ids-new}'
```

Example output:

```
GPU-0,NVIDIA,1000,0:GPU-1,NVIDIA,1000,0:GPU-2,NVIDIA,1000,0:GPU-3,NVIDIA,1000,0:
```

To verify no overlap between two pods:

```bash
# Get GPU IDs for each pod
kubectl get pod pod-a -o jsonpath='{.metadata.annotations.volcano\.sh/vgpu-ids-new}' | tr ':' '\n' | grep GPU | cut -d, -f1 | sort
kubectl get pod pod-b -o jsonpath='{.metadata.annotations.volcano\.sh/vgpu-ids-new}' | tr ':' '\n' | grep GPU | cut -d, -f1 | sort
```

## Example: Full Workflow

1. Configure two exclusivity rules (training and high-priority batch):

```yaml
deviceshare.GPUExclusiveRules:
  - workloadType: training
  - workloadType: batch
    priority: high
```

2. Submit a training pod (4 GPUs) and a high-priority batch pod (3 GPUs) on a node with 8 physical GPUs:

```bash
# Training pod gets 4 exclusive GPUs (e.g., GPU-0 to GPU-3)
# Batch pod gets 3 different exclusive GPUs (e.g., GPU-4 to GPU-6)
# GPU-7 remains available for non-exclusive pods
```

3. Submit an inference pod (1 GPU) — it can share any GPU, including those used by training or batch pods.

4. Submit another training pod requesting 5 GPUs — it stays Pending because only 4 exclusive GPUs remain (GPU-4 to GPU-6 are reserved by the batch rule, GPU-7 is the only free one).

## Troubleshooting

### Plugin Not Loading Rules

Check scheduler logs for the configuration message:

```bash
kubectl logs -n volcano-system -l app=volcano-scheduler | grep gpuexclusive
```

Expected output:

```
gpuexclusive config: vgpuResourceName=volcano.sh/vgpu-number, rules=[{map[workloadType:training]}]
```

If you see `no rules configured`, verify that the rules are under `deviceshare.GPUExclusiveRules` in the deviceshare plugin arguments (not as a separate plugin).

### Pod Stuck in Pending

If a rule-matching pod is Pending, all exclusive GPUs on the node may be reserved. Check:

```bash
# View current GPU assignments
kubectl get pods -o custom-columns='NAME:.metadata.name,GPUS:.metadata.annotations.volcano\.sh/vgpu-ids-new'
```

Either wait for existing pods to complete, or scale down other exclusive workloads to free GPUs.
