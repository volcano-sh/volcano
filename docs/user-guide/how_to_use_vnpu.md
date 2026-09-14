# Ascend vNPU User Guide

## Introduction

Volcano supports **two vNPU modes** for sharing Ascend devices:

| Mode | Maintained by | Supported chips | Use case |
|------|---------------|------------------|----------|
| [HAMi Mode](#hami-mode) | Third-party community [HAMi](https://github.com/Project-HAMi) | Ascend 310 and Ascend 910 series | NPU/vNPU cluster for 910 or 310 series; heterogeneous Ascend cluster (mixed chip types, e.g. 910A/910B2/910B3/310P) |
| [MindCluster Mode](#mindcluster-mode) | [MindCluster](https://gitcode.com/Ascend/mind-cluster) (official Ascend cluster scheduling add-on) | Ascend 310 series | vNPU cluster for Ascend 310 series, with support for more chip types to come |

Pick the section below that matches the mode you want to use; each section is self-contained and covers installation and usage for that mode.

---

## Common Prerequisites

- Kubernetes >= 1.16
- Volcano >= 1.14 (1.16 for `hami-core` mode)

### Install Volcano

Follow instructions in the [Volcano Installer Guide](https://github.com/volcano-sh/volcano?tab=readme-ov-file#quick-start-guide).

---

## HAMi Mode

### Description

This mode is developed by a third-party community 'HAMi', which is also the developer of the [volcano-vgpu](./how_to_use_volcano_vgpu.md) feature. It supports vNPU for both Ascend 310 and Ascend 910, and can manage a heterogeneous Ascend cluster (a cluster with multiple Ascend chip types, e.g. 910A/910B2/910B3/310P).

1. HAMi provides a distinct `ResourceName` for each supported Ascend chip model.
2. Supports template-based hard partitioning of vNPU devices.
3. Starting with version 1.16, supports soft partitioning based on [`hami-core`](https://github.com/Project-HAMi/hami-vnpu-core), known as the `hami-core` mode.

### Installation

**Prerequisites**:

[ascend-docker-runtime](https://gitcode.com/Ascend/mind-cluster/tree/master/component/ascend-docker-runtime) is required for HAMi mode.

Make sure the [Common Prerequisites](#common-prerequisites) above are satisfied first.

#### Label the Node with `ascend=on`

```
kubectl label node {ascend-node} ascend=on
```

#### Deploy `hami-scheduler-device` ConfigMap

1. Download file
```
curl -O https://raw.githubusercontent.com/Project-HAMi/ascend-device-plugin/main/ascend-device-configmap.yaml
```
2. (Optional) Set `hamiVnpuCore` to `true` if you want to enable `hami-vnpu-core`
3. Deploy the yaml

```
kubectl apply -f ascend-device-configmap.yaml
```

#### Deploy ascend-device-plugin

```
kubectl apply -f https://raw.githubusercontent.com/Project-HAMi/ascend-device-plugin/main/ascend-device-plugin.yaml
```

For more information, refer to the [ascend-device-plugin documentation](https://github.com/Project-HAMi/ascend-device-plugin).

#### Update Scheduler Config
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
          deviceshare.AscendHAMiVNPUEnable: true   # enable ascend vnpu
          deviceshare.SchedulePolicy: binpack  # scheduling policy. binpack / spread
          deviceshare.KnownGeometriesCMNamespace: kube-system
          deviceshare.KnownGeometriesCMName: hami-scheduler-device
```

  **Note:** You may notice that, 'volcano-vgpu' has its own GeometriesCMName and GeometriesCMNamespace, which means if you want to use both vNPU and vGPU in a same volcano cluster, you need to merge the configMap from both sides and set it here.

### Usage

The supported Ascend chips and their `ResourceNames` are shown in the following table (`ResourceCoreName` only applies to `hami-core` mode; it can be ignored for template vNPU mode):

| ChipName | ResourceName | ResourceMemoryName | ResourceCoreName |
|-------|-------|-------|-------|
| 910A | huawei.com/Ascend910A | huawei.com/Ascend910A-memory | huawei.com/Ascend910A-core |
| 910B2 | huawei.com/Ascend910B2 | huawei.com/Ascend910B2-memory | huawei.com/Ascend910B2-core |
| 910B3 | huawei.com/Ascend910B3 | huawei.com/Ascend910B3-memory | huawei.com/Ascend910B3-core |
| 910B4 | huawei.com/Ascend910B4 | huawei.com/Ascend910B4-memory | huawei.com/Ascend910B4-core |
| 910B4-1 | huawei.com/Ascend910B4-1 | huawei.com/Ascend910B4-1-memory | huawei.com/Ascend910B4-1-core |
| 910C | huawei.com/Ascend910C | huawei.com/Ascend910C-memory | huawei.com/Ascend910C-core |
| 310P3 | huawei.com/Ascend310P | huawei.com/Ascend310P-memory | huawei.com/Ascend310P-core |

#### Template vNPU mode

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: ascend-pod
spec:
  schedulerName: volcano
  containers:
    - name: ubuntu-container
      image: swr.cn-south-1.myhuaweicloud.com/ascendhub/ascend-pytorch:24.0.RC1-A2-1.11.0-ubuntu20.04
      command: ["sleep"]
      args: ["100000"]
      resources:
        limits:
          huawei.com/Ascend310P: "1"
          huawei.com/Ascend310P-memory: "4096"
```

#### Hami vNPU scene memory allocation restrictions

- When a container requests a single vNPU device, the memory can be configured to any value, and the memory request will automatically align with the closest sharding strategy
  - Example: Single card memory is 65536, virtualization templates are 1/4 (16384), 1/2 (32768)
    - A container requests 1 vNPU device and requests 1024 memory, so the actual allocated memory is 16384
    - A container requests 1 vNPU device with a requested memory of 20480, resulting in an actual allocated memory of 32768

- When a container requests multiple vNPU devices, the memory resource request can be left unspecified or filled with the maximum value; the memory allocated to that container is the actual memory of the entire card

#### `hami-core` mode

```
apiVersion: v1
kind: Pod
metadata:
  name: ascend-pod
  annotations:
    huawei.com/vnpu-mode: hami-core
spec:
  schedulerName: volcano
  containers:
    - name: ubuntu-container
      image: quay.io/ascend/vllm-ascend:v0.18.0-310p
      command: ["sleep"]
      args: ["100000"]
      resources:
        limits:
          huawei.com/Ascend310P: "1"
          huawei.com/Ascend310P-memory: "4096"
          huawei.com/Ascend310P-core: "90"
```

See the `ResourceName` table in the [Usage](#usage) section above for the supported Ascend chips.

**Note**: If the pod's annotations do not specify `hami-core`, the device will be allocated in the template vNPU mode even if the `hami-core` feature is enabled in the configuration file.

### Monitoring

When a node runs in **`hami-core` (soft slicing) mode**, `ascend-device-plugin` starts an **embedded Prometheus exporter** on **`:9395/metrics`** that reports physical-device and per-container vNPU usage. It is **not** started for the template vNPU (or whole-card) path. 

#### Exposed metrics

| Metric | Labels | Description |
| :--- | :--- | :--- |
| `hami_host_gpu_memory_used_bytes` | `device_index`, `device_uuid`, `device_type` | Physical NPU memory used (bytes) |
| `hami_host_gpu_utilization_ratio` | `device_index`, `device_uuid`, `device_type` | Physical NPU AICore utilization (0-100) |
| `hami_vgpu_memory_used_bytes` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid` | Per-container vNPU memory used (bytes) |
| `hami_vgpu_memory_limit_bytes` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid` | Per-container vNPU memory limit (bytes) |
| `hami_container_device_utilization_ratio` | `namespace`, `pod`, `container`, `vdevice_index`, `device_uuid` | AICore utilization of the device the container runs on (0-100) |


---

## MindCluster Mode

### Description

The initial version of [MindCluster](https://gitcode.com/Ascend/mind-cluster)—the official Ascend cluster scheduling add-on—required custom modifications and recompilation of Volcano. Furthermore, it was limited to Volcano release1.7 and release1.9, which complicated its use and restricted access to newer Volcano features.

To address this, we have integrated its core scheduling logic for Ascend vNPU into Volcano's native device-share plugin, which is designed specifically for scheduling and sharing heterogeneous resources like GPUs and NPUs. This integration provides seamless access to vNPU capabilities through the procedure below, while maintaining full compatibility with the latest Volcano features.

### Use case

- vNPU cluster for Ascend 310 series
- with support for more chip types to come

### Installation

Make sure the [Common Prerequisites](#common-prerequisites) above are satisfied first.

#### Install Third-Party Components

Follow the official [Ascend documentation](https://www.hiascend.com/document/detail/zh/mindcluster/72rc1/clustersched/dlug/mxdlug_start_006.html#ZH-CN_TOPIC_0000002470358262__section1837511531098) to install the following components:
- NodeD
- Ascend Device Plugin
- Ascend Docker Runtime
- ClusterD
- Ascend Operator

> **Note:** Skip the installation of `ascend-volcano` mentioned in the document above, as we have already installed the native Volcano from the Volcano community in the **Common Prerequisites** part.

**Configuration Adjustment for Ascend Device Plugin:**

When installing `ascend-device-plugin`, you must set the `presetVirtualDevice` parameter to `"false"` in the `device-plugin-310P-volcano-v{version}.yaml` file to enable dynamic virtualization of 310P:

```yaml
...
args: [
  "device-plugin",
  "-useAscendDocker=true",
  "-volcanoType=true",
  "-presetVirtualDevice=false",
  "-logFile=/var/log/mindx-dl/devicePlugin/devicePlugin.log",
  "-logLevel=0"
]
...
```
For detailed information, please consult the official [Ascend MindCluster documentation.](https://www.hiascend.com/document/detail/zh/mindcluster/72rc1/clustersched/dlug/cpaug_0020.html)

#### Scheduler Config Update
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
          deviceshare.AscendMindClusterVNPUEnable: true   # enable ascend vnpu
    configurations:
    ...
    - name: init-params
      arguments: {"grace-over-time":"900","presetVirtualDevice":"false"}  # to enable dynamic virtualization, presetVirtualDevice need to be set false
```

### Usage

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: mindx-dls
  namespace: vnpu
  labels:
    ring-controller.atlas: ascend-310P
spec:
  minAvailable: 1
  schedulerName: volcano
  policies:
    - event: PodEvicted
      action: RestartJob
  plugins:
    ssh: []
    env: []
    svc: []
  maxRetry: 3
  queue: default
  tasks:
    - name: "default-test"
      replicas: 1
      template:
        metadata:
          labels:
            app: infers
            ring-controller.atlas: ascend-310P
            vnpu-dvpp: "null"
            vnpu-level: low
        spec:
          schedulerName: volcano
          containers:
            - name: resnet50infer
              image: swr.cn-south-1.myhuaweicloud.com/ascendhub/mindie:2.1.RC1-300I-Duo-py311-openeuler24.03-lts
              imagePullPolicy: IfNotPresent
              securityContext:
                privileged: false
              command: ["/bin/bash", "-c", "tail -f /dev/null"]
              resources:
                requests:
                  huawei.com/npu-core: 8
                limits:
                  huawei.com/npu-core: 8
          nodeSelector:
            host-arch: huawei-arm

```

The supported Ascend chips and their `ResourceNames` are shown in the following table:

| ChipName | JobLabel and TaskLabel             | ResourceName |
|-------|------------------------------------|-------|
| 310P3 | ring-controller.atlas: ascend-310P | huawei.com/npu-core |

**Description of Labels in the Virtualization Task YAML**

| **Key**                   | **Value**       | **Description**                                                                                                                                                                                                                                                                                                                                                                                                             |
| ------------------------- | --------------- |-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| **vnpu-level**            | **low**         | Low configuration (default). Selects the lowest-configuration "virtualized instance template."                                                                                                                                                                                                                                                                                                                              |
|                           | **high**        | Performance-first. When cluster resources are sufficient, the scheduler will choose the highest-configured virtualized instance template possible. When most physical NPUs in the cluster are already in use and only a few AI Cores remain on each device, the scheduler will allocate templates that match the remaining AI Core count rather than forcing high-profile templates. For details, refer to the table below. |
| **vnpu-dvpp**             | **yes**         | The Pod uses DVPP.                                                                                                                                                                                                                                                                                                                                                                                                          |
|                           | **no**          | The Pod does not use DVPP.                                                                                                                                                                                                                                                                                                                                                                                                  |
|                           | **null**        | Default value. DVPP usage is not considered.                                                                                                                                                                                                                                                                                                                                                                                |
| **ring-controller.atlas** | **ascend-310P** | Indicates that the task uses products from the Atlas inference series.                                                                                                                                                                                                                                                                                                                                                      |

**Effect of DVPP and Level Configurations**

| **Product Model**                       | **Requested AI Core Count** | **vnpu-dvpp** | **vnpu-level**       | **Downgrade** | **Selected Template** |
| --------------------------------------- | --------------------------- |---------------| -------------------- | ------------- | --------------------- |
| **Atlas Inference Series (8 AI Cores)** | **1**                       | `null`        | Any value            | –             | `vir01`               |
|                                         | **2**                       | `null`        | `low` / other values | –             | `vir02_1c`            |
|                                         | **2**                       | `null`        | `high`               | No            | `vir02`               |
|                                         | **2**                       | `null`        | `high`               | Yes           | `vir02_1c`            |
|                                         | **4**                       | `yes`         | `low` / other values | –             | `vir04_4c_dvpp`       |
|                                         | **4**                       | `no`          | `low` / other values | –             | `vir04_3c_ndvpp`      |
|                                         | **4**                       | `null`        | `low` / other values | –             | `vir04_3c`            |
|                                         | **4**                       | `yes`         | `high`               | –             | `vir04_4c_dvpp`       |
|                                         | **4**                       | `no`          | `high`               | –             | `vir04_3c_ndvpp`      |
|                                         | **4**                       | `null`        | `high`               | No            | `vir04`               |
|                                         | **4**                       | `null`        | `high`               | Yes           | `vir04_3c`            |
|                                         | **8 or multiples of 8**     | Any value     | Any value            | –             | –                     |

**Notice**

For **chip virtualization (non-full card usage)**, the value of `vnpu-dvpp` must strictly match the corresponding value listed in the above table.
Any other values will cause the task to fail to be dispatched.

For detailed information, please consult the official [Ascend MindCluster documentation.](https://www.hiascend.com/document/detail/zh/mindcluster/72rc1/clustersched/dlug/cpaug_0020.html)
