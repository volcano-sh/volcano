# GPU Sharing User guide

## Contact And Bug Report

Thanks staff of 4paradigm.com for contributing this feature to volcano, if you encountered any issues, feel free to submit an issue, or sending an email to <limengxuan@4paradigm.com>

## Main features

***GPU sharing***: Each task can allocate a portion of GPU instead of a whole GPU card, thus GPU can be shared among multiple tasks.

***Device Memory Control***: GPUs can be allocated with certain device memory size (i.e 3000M) or device memory percentage of whole GPU(i.e 50%) and have made it that it does not exceed the boundary.

***Easy to use***: See the examples below.

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
          deviceshare.VGPUEnable: true # enable vgpu
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
          predicate.VGPUEnable: true # enable vgpu
      - name: proportion
      - name: nodeorder
      - name: binpack
```

#### 2. Install from release package

Same as above, after installed, update the scheduler configuration in `volcano-scheduler-configmap` configmap.

### Install Volcano device plugin

Please refer to [volcano device plugin](https://github.com/volcano-sh/devices/blob/master/README.md#quick-start)

### Verify environment is ready

Check the node status, it is ok if `volcano.sh/vgpu-number` is included in the allocatable resources.

```shell script
$ kubectl get node {node name} -oyaml
...
status:
  addresses:
  - address: 172.17.0.3
    type: InternalIP
  - address: volcano-control-plane
    type: Hostname
  allocatable:
    cpu: "4"
    ephemeral-storage: 123722704Ki
    hugepages-1Gi: "0"
    hugepages-2Mi: "0"
    memory: 8174332Ki
    pods: "110"
    volcano.sh/gpu-number: "10"    # vGPU resource
  capacity:
    cpu: "4"
    ephemeral-storage: 123722704Ki
    hugepages-1Gi: "0"
    hugepages-2Mi: "0"
    memory: 8174332Ki
    pods: "110"
    volcano.sh/gpu-memory: "89424"
    volcano.sh/gpu-number: "10"   # vGPU resource
```

### Running GPU Sharing Jobs

NVIDIA GPUs can now be shared via container level resource requirements using the resource name `volcano.sh/vgpu-memory` and `volcano.sh/vgpu-number`:

```shell script
$ cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod12
spec:
  schedulerName: volcano
  containers:
    - name: ubuntu-container
      image: ubuntu:18.04
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          volcano.sh/vgpu-number: 2 # requesting 2 vGPUs
          volcano.sh/vgpu-memory: 2000
          #volcano.sh/vgpu-memory-percentage: 50 #Each vGPU containers 50% device memory of that GPU. Can not be used with nvidia.com/gpumem
    - name: ubuntu-container0
      image: ubuntu:18.04
      command: ["bash", "-c", "sleep 86400"]
    - name: ubuntu-container1
      image: ubuntu:18.04
      command: ["bash", "-c", "sleep 86400"]
      resources:
        limits:
          volcano.sh/vgpu-number: 2 # requesting 2 vGPUs
          volcano.sh/vgpu-memory: 3000 
EOF
```

If only the above pods are claiming gpu resource in a cluster, you can see the pods sharing one gpu card:

```shell script
$ kubectl exec -ti  gpu-pod12 -c ubuntu-container nvidia-smi
...
[4pdvGPU Warn(30207:139917515929408:util.c:149)]: new_uuid=GPU-a88b5d0e-eb85-924b-b3cd-c6cad732f745 1
[4pdvGPU Warn(30207:139917515929408:util.c:149)]: new_uuid=GPU-d2407b50-70b1-f427-d712-801233c47b67 1
[4pdvGPU Msg(30207:139917515929408:libvgpu.c:871)]: Initializing.....
[4pdvGPU Msg(30207:139917515929408:device.c:249)]: driver version=11020
Tue Mar  7 12:19:03 2023       
+-----------------------------------------------------------------------------+
| NVIDIA-SMI 460.73.01    Driver Version: 460.73.01    CUDA Version: 11.2     |
|-------------------------------+----------------------+----------------------+
| GPU  Name        Persistence-M| Bus-Id        Disp.A | Volatile Uncorr. ECC |
| Fan  Temp  Perf  Pwr:Usage/Cap|         Memory-Usage | GPU-Util  Compute M. |
|                               |                      |               MIG M. |
|===============================+======================+======================|
|   0  Tesla V100-PCIE...  On   | 00000000:1B:00.0 Off |                    0 |
| N/A   47C    P0    27W / 250W |      0MiB /  2000MiB |      0%      Default |
|                               |                      |                  N/A |
+-------------------------------+----------------------+----------------------+
|   1  Tesla V100-PCIE...  On   | 00000000:88:00.0 Off |                    0 |
| N/A   51C    P0    31W / 250W |      0MiB /  2000MiB |      0%      Default |
|                               |                      |                  N/A |
+-------------------------------+----------------------+----------------------+
                                                                               
+-----------------------------------------------------------------------------+
| Processes:                                                                  |
|  GPU   GI   CI        PID   Type   Process name                  GPU Memory |
|        ID   ID                                                   Usage      |
|=============================================================================|
|  No running processes found                                                 |
+-----------------------------------------------------------------------------+
[4pdvGPU Msg(30207:139917515929408:multiprocess_memory_limit.c:457)]: Calling exit handler 30207
...

$ kubectl exec -ti  gpu-pod12 -c ubuntu-container1 nvidia-smi
...
[4pdvGPU Warn(31336:140369108928320:util.c:149)]: new_uuid=GPU-a88b5d0e-eb85-924b-b3cd-c6cad732f745 1
[4pdvGPU Warn(31336:140369108928320:util.c:149)]: new_uuid=GPU-d2407b50-70b1-f427-d712-801233c47b67 1
[4pdvGPU Msg(31336:140369108928320:libvgpu.c:871)]: Initializing.....
[4pdvGPU Msg(31336:140369108928320:device.c:249)]: driver version=11020
Tue Mar  7 12:19:59 2023       
+-----------------------------------------------------------------------------+
| NVIDIA-SMI 460.73.01    Driver Version: 460.73.01    CUDA Version: 11.2     |
|-------------------------------+----------------------+----------------------+
| GPU  Name        Persistence-M| Bus-Id        Disp.A | Volatile Uncorr. ECC |
| Fan  Temp  Perf  Pwr:Usage/Cap|         Memory-Usage | GPU-Util  Compute M. |
|                               |                      |               MIG M. |
|===============================+======================+======================|
|   0  Tesla V100-PCIE...  On   | 00000000:1B:00.0 Off |                    0 |
| N/A   47C    P0    27W / 250W |      0MiB /  3000MiB |      0%      Default |
|                               |                      |                  N/A |
+-------------------------------+----------------------+----------------------+
|   1  Tesla V100-PCIE...  On   | 00000000:88:00.0 Off |                    0 |
| N/A   51C    P0    31W / 250W |      0MiB /  3000MiB |      0%      Default |
|                               |                      |                  N/A |
+-------------------------------+----------------------+----------------------+
                                                                               
+-----------------------------------------------------------------------------+
| Processes:                                                                  |
|  GPU   GI   CI        PID   Type   Process name                  GPU Memory |
|        ID   ID                                                   Usage      |
|=============================================================================|
|  No running processes found                                                 |
+-----------------------------------------------------------------------------+
[4pdvGPU Msg(31336:140369108928320:multiprocess_memory_limit.c:457)]: Calling exit handler 31336
...
```
