# Volcano vGPU User Guide

## Background Knowledge of GPU Sharing Modes in Volcano

Volcano supports **two GPU sharing modes** for virtual GPU (vGPU) scheduling:

### 1. HAMI-core (Software-based vGPU)

**Description**:
Leverages **VCUDA**, a CUDA API hijacking technique to enforce GPU core and memory usage limits, enabling **software-level virtual GPU slicing**.

**Use case**:
Ideal for environments requiring **fine-grained GPU sharing**. Compatible with all GPU types.

---

### 2. Dynamic MIG (Hardware-level GPU Slicing)

**Description**:
Utilizes **NVIDIA's MIG (Multi-Instance GPU)** technology to partition a physical GPU into isolated instances with **hardware-level performance guarantees**.

**Use case**:
Best for **performance-sensitive** workloads. Requires **MIG-capable GPUs** (e.g., A100, H100).

---

GPU Sharing mode is a node configuration. Volcano supports heterogeneous cluster(i.e a part of node uses HAMi-core while another part uses dynamic MIG), See [volcano-vgpu-device-plugin](https://github.com/Project-HAMi/volcano-vgpu-device-plugin) for configuration and details.

## Installation

To enable vGPU scheduling, the following components must be set up based on the selected mode:

### Common Requirements

* **Prerequisites**:

  * NVIDIA driver > 440
  * nvidia-docker > 2.0
  * Docker configured with `nvidia` as the default runtime
  * Kubernetes >= 1.16
  * Volcano >= 1.9

* **Install Volcano**:

  * Follow instructions in [Volcano Installer Guide](https://github.com/volcano-sh/volcano?tab=readme-ov-file#quick-start-guide)

* **Install Device Plugin**:

  * Deploy [`volcano-vgpu-device-plugin`](https://github.com/Project-HAMi/volcano-vgpu-device-plugin)

  **Note:** the [vgpu device plugin yaml](https://github.com/Project-HAMi/volcano-vgpu-device-plugin/blob/main/volcano-vgpu-device-plugin.yml) also includes the ***Node GPU mode*** and the ***MIG geometry*** configuration. Please refer to the [vgpu device plugin config](https://github.com/Project-HAMi/volcano-vgpu-device-plugin/blob/main/doc/config.md).

* **Validate Setup**:
  Ensure node allocatable resources include:

```yaml
volcano.sh/vgpu-memory: "89424"
volcano.sh/vgpu-number: "8"
```
* **Scheduler Config Update**:

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
      - name: predicates
      - name: deviceshare
        arguments:
          deviceshare.VGPUEnable: true   # enable vgpu plugin
          deviceshare.SchedulePolicy: binpack  # scheduling policy. binpack / spread
```

Check with:

```bash
kubectl get node {node-name} -o yaml
```

---

### HAMI-core Usage

* **Pod Spec**:

```yaml
metadata:
  name: hami-pod
  annotations:
    volcano.sh/vgpu-mode: "hami-core"
spec:
  schedulerName: volcano
  containers:
  - name: cuda-container
    image: nvidia/cuda:9.0-devel
    resources:
      limits:
        volcano.sh/vgpu-number: 1    # requesting 1 gpu cards
        volcano.sh/vgpu-cores: 50    # (optional)each vGPU uses 50%
        volcano.sh/vgpu-memory: 3000 # (optional)each vGPU uses 3G GPU memory
```

---

### Dynamic MIG Usage

* **Enable MIG Mode**:

If you need to use MIG (Multi-Instance GPU), you must run the following command on the GPU node.

```bash
sudo nvidia-smi -mig 1
```

* **Geometry Config (Optional)**:
  The volcano-vgpu-device-plugin automatically generates an initial MIG configuration, which is stored in the `volcano-vgpu-device-config` ConfigMap under the `kube-system` namespace. You can customize this configuration as needed. For more details, refer to the [vgpu device plugin yaml](https://github.com/Project-HAMi/volcano-vgpu-device-plugin/blob/main/volcano-vgpu-device-plugin.yml).

* **Pod Spec with MIG Annotation**:

```yaml
metadata:
  name: mig-pod
  annotations:
    volcano.sh/vgpu-mode: "mig"
spec:
  schedulerName: volcano
  containers:
  - name: cuda-container
    image: nvidia/cuda:9.0-devel
    resources:
      limits:
        volcano.sh/vgpu-number: 1
        volcano.sh/vgpu-memory: 3000
```

Note: Actual memory allocated depends on best-fit MIG slice (e.g., request 3GB → 5GB slice used).

---

## Scheduler Mode Selection

* **Explicit Mode**:

  * Use annotation `volcano.sh/vgpu-mode` to force hami-core or MIG mode.
  * If annotation is absent, scheduler selects mode based on resource fit and policy.

* **Scheduling Policy**:

  * Modes like `binpack` or `spread` influence node selection.

---

## Summary Table

| Mode        | Isolation        | MIG GPU Required | Annotation | Core/Memory Control | Recommended For            |
| ----------- | ---------------- | ---------------- | ---------- | ------------------- | -------------------------- |
| HAMI-core   | Software (VCUDA) | No               | No         | Yes                 | General workloads          |
| Dynamic MIG | Hardware         | Yes              | Yes        | MIG-controlled      | Performance-sensitive jobs |

---

## Monitoring

* **Scheduler Metrics**:

```bash
curl http://<volcano-scheduler-ip>:8080/metrics
```

* **Device Plugin Metrics**:

```bash
curl http://<plugin-pod-ip>:9394/metrics
```

Metrics include GPU utilization, pod memory usage, and limits.

---

## Issues and Contributions

* File bugs: [Volcano Issues](https://github.com/volcano-sh/volcano/issues)
* Contribute: [Pull Requests Guide](https://help.github.com/articles/using-pull-requests/)

