# GPU Number User guide

## Important Note

> **Note** GPU Number is deprecated in volcano v1.9, recommended to use the Volcano VGPU feature, which is provided by HAMI project, click [here](https://github.com/Project-HAMi/volcano-vgpu-device-plugin)

## Arguments
| ID  | Name                          | Type           | Default Value | Required | Description                                          | Example                                       |
|-----|-------------------------------|-----------------|---------------|----------|------------------------------------------------------|-----------------------------------------------|
| 1   | `GPUSharingEnable` | bool | `false` | N | whether enable gpu sharing | deviceshare.GPUSharingEnable: true |
| 2   | `NodeLockEnable` | bool | `false` | N | whether enable node lock | deviceshare.NodeLockEnable: true |
| 3   | `GPUNumberEnable` | bool | `false` | N | whether enable gpu number | deviceshare.GPUNumberEnable: true |
| 4   | `VGPUEnable` | bool | `false` | N | whether enable vgpu | deviceshare.VGPUEnable: true |
| 5   | `SchedulePolicy` | string | `` | N | schedule policy | deviceshare.SchedulePolicy: binpack |
| 6   | `ScheduleWeight` | int | 0 | N | schedule weight | deviceshare.ScheduleWeight: 10 |
| 7   | `VGPUNumber` | string | `volcano.sh/vgpu-number` | N | resource name for vgpu number | deviceshare.VGPUNumber: volcano.sh/vgpu-number |
| 8   | `VGPUMemory` | string | `volcano.sh/vgpu-memory` | N | resource name for vgpu memory | deviceshare.VGPUMemory: volcano.sh/vgpu-memory |
| 9   | `VGPUCores` | string | `volcano.sh/vgpu-cores` | N | resource name for vgpu cores | deviceshare.VGPUCores: volcano.sh/vgpu-cores |

## Environment setup

### Install volcano

#### 1. Install from source

Refer to [Install Guide](../../installer/README.md) to install volcano.

After installed, update the scheduler configuration:

```shell script
kubectl edit cm -n volcano-system volcano-scheduler-configmap
```

For volcano v1.8.2+(v1.8.2 excluded), use the following configMap 

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
      - name: deviceshare
        arguments:
          deviceshare.GPUNumberEnable: true # enable gpu number
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
```

For volcano v1.8.2-(v1.8.2 included), use the following configMap 

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
        arguments:
          predicate.GPUNumberEnable: true # enable gpu number
      - name: proportion
      - name: nodeorder
      - name: binpack
```

#### 2. Install from release package.

Same as above, after installed, update the scheduler configuration in `volcano-scheduler-configmap` configmap.

### Install Volcano device plugin

Please refer to [volcano device plugin](https://github.com/volcano-sh/devices/blob/master/README.md#quick-start)

* Remember to config volcano device plugin to support gpu-number, users need to config volcano device plugin --gpu-strategy=number. For more information [volcano device plugin configuration](https://github.com/volcano-sh/devices/blob/master/doc/config.md)

### Verify environment is ready

Check the node status, it is ok  `volcano.sh/gpu-number` is included in the allocatable resources. 

```shell script
$ kubectl get node {node name} -oyaml
...
Capacity:
  attachable-volumes-gce-pd:  127
  cpu:                        2
  ephemeral-storage:          98868448Ki
  hugepages-1Gi:              0
  hugepages-2Mi:              0
  memory:                     7632596Ki
  pods:                       110
  volcano.sh/gpu-memory:      0
  volcano.sh/gpu-number:      1
Allocatable:
  attachable-volumes-gce-pd:  127
  cpu:                        1930m
  ephemeral-storage:          47093746742
  hugepages-1Gi:              0
  hugepages-2Mi:              0
  memory:                     5752532Ki
  pods:                       110
  volcano.sh/gpu-memory:      0
  volcano.sh/gpu-number:      1
```

### Running Jobs With Multiple GPU Cards

Jobs can have multiple exclusive NVIDIA GPUs cards via defining container level resource requirements `volcano.sh/gpu-number`:
```shell script
$ cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod1
spec:
  containers:
    - name: cuda-container
      image: nvidia/cuda:9.0-devel
      command: ["sleep"]
      args: ["100000"]
      resources:
        limits:
          volcano.sh/gpu-number: 1 # requesting 1 gpu cards
EOF
```

If the above pods claim multiple gpu cards, you can see each of them has exclusive gpu cards:

```shell script
$ kubectl exec -ti  gpu-pod1 env
...
NVIDIA_VISIBLE_DEVICES=0
VOLCANO_GPU_ALLOCATED=1
...
```
### Understanding How Multiple GPU Cards Requirement Works 

The main architecture is similar as the previous, but the gpu-index results of each pod will be a list of gpu cards index. 

![gpu_number](../images/gpu-number.png)

1. create a pod with `volcano.sh/gpu-number` resource request,

2. volcano scheduler predicates and allocate gpu cards to the pod. Add the below annotation

```yaml
annotations:
  volcano.sh/gpu-index: “0”
  volcano.sh/predicate-time: “1593764466550835304”
```

3. kubelet watches the pod bound to itself, and calls allocate API to set env before running the container.

```yaml
env:
  NVIDIA_VISIBLE_DEVICES: “0” # GPU card index
  VOLCANO_GPU_ALLOCATED: “1” # GPU number allocated
```