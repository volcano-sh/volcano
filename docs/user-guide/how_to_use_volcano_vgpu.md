# Volcano vGPU User guide

## Prerequisites

The list of prerequisites for running the Volcano device plugin is described below:
* NVIDIA drivers > 440
* nvidia-docker version > 2.0 (see how to [install](https://github.com/NVIDIA/nvidia-docker) and it's [prerequisites](https://github.com/nvidia/nvidia-docker/wiki/Installation-\(version-2.0\)#prerequisites))
* docker configured with nvidia as the [default runtime](https://github.com/NVIDIA/nvidia-docker/wiki/Advanced-topics#default-runtime).
* Kubernetes version >= 1.16
* Volcano verison >= 1.9

## Environment setup

### Install volcano

Refer to [Install Guide](../../installer/README.md) to install volcano.

After installed, update the scheduler configuration:

```shell script
kubectl edit cm -n volcano-system volcano-scheduler-configmap
```

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

### Install Volcano device plugin

Please refer to [volcano vgpu device plugin](https://github.com/Project-HAMi/volcano-vgpu-device-plugin?tab=readme-ov-file#enabling-gpu-support-in-kubernetes)

### Verify environment is ready

Check the node status, it is ok if `volcano.sh/vgpu-memory` and `volcano.sh/vgpu-number` are included in the allocatable resources.

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
    volcano.sh/vgpu-memory: "89424"
    volcano.sh/vgpu-number: "8"    # GPU resource
  capacity:
    cpu: "4"
    ephemeral-storage: 123722704Ki
    hugepages-1Gi: "0"
    hugepages-2Mi: "0"
    memory: 8174332Ki
    pods: "110"
    volcano.sh/vgpu-memory: "89424"
    volcano.sh/vgpu-number: "8"   # GPU resource
```

### Running GPU Sharing Jobs

VGPU can be requested by both set "volcano.sh/vgpu-number" , "volcano.sh/vgpu-cores" and "volcano.sh/vgpu-memory" in resource.limit

```shell script
$ cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: gpu-pod1
spec:
  schedulerName: volcano
  containers:
    - name: cuda-container
      image: nvidia/cuda:9.0-devel
      command: ["sleep"]
      args: ["100000"]
      resources:
        limits:
          volcano.sh/vgpu-number: 2 # requesting 2 gpu cards
          volcano.sh/vgpu-memory: 3000 # (optinal)each vGPU uses 3G device memory
          volcano.sh/vgpu-cores: 50 # (optional)each vGPU uses 50% core  
EOF
```

You can validate device memory using nvidia-smi inside container:

![img](https://github.com/Project-HAMi/volcano-vgpu-device-plugin/blob/main/doc/hard_limit.jpg)

> **WARNING:** *if you don't request GPUs when using the device plugin with NVIDIA images all
> the GPUs on the machine will be exposed inside your container.
> The number of vgpu used by a container can not exceed the number of gpus on that node.*

### Monitor

volcano-scheduler-metrics records every GPU usage and limitation, visit the following address to get these metrics.

```
curl {volcano scheduler cluster ip}:8080/metrics
```

You can also collect the **GPU utilization**, **GPU memory usage**, **pods' GPU memory limitations** and **pods' GPU memory usage** metrics on nodes by visiting the following addresses:

```
curl {volcano device plugin pod ip}:9394/metrics
```
![img](https://github.com/Project-HAMi/volcano-vgpu-device-plugin/blob/main/doc/vgpu_device_plugin_metrics.png)

# Issues and Contributing

* You can report a bug by [filing a new issue](https://github.com/volcano-sh/volcano/issues)
* You can contribute by opening a [pull request](https://help.github.com/articles/using-pull-requests/)