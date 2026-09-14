# NUMA Aware User Guide

## Environment setup

### Pre-Condition

- Enable cpu manager and set policy to "static"
- Enable topology manager and set the policy option you want
    <br>
    1. Set the above conditions by editing the kubelet configuration file

   ```
    cat /var/lib/kubelet/config.yaml
   ```

   ```
    {...}
    cpuManagerPolicy: static
    topologyManagerPolicy: best-effort
    kubeReserved:
      cpu: 1000m
   ```

   2. Restart kubelet to take effect <br>
      Run the following:

      ```
      1. systemctl stop kubelet
      2. rm -rf /var/lib/kubelet/cpu_manager_state
      3. systemctl daemon-reload
      4. systemctl start kubelet
      ```

### Install volcano

#### 1. Install from source

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
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
      - name: numa-aware # add it to enable numa-aware plugin
        arguments:
          weight: 10
```

#### 2. Install from release package

Same as above, after installed, update the scheduler configuration in `volcano-scheduler-configmap` configmap.

### Install volcano resource exporter

Please refer to [volcano resource exporter](https://github.com/volcano-sh/resource-exporter/blob/main/README.md)

### Verify environment is ready

Check the CRD **numatopo** whether the data of all nodes exists.

```
kubectl get numatopo 
NAME              AGE
node-1            4h8m
node-2            4h8m
node-3            4h8m
```

## Usage

### Running volcano Job with topology policy

Support the task-level topology policy and edit **spec.tasks.topologyPolicy** to specify whether to perform topology scheduling.<br> The supported options are the same as [topology manager](https://kubernetes.io/docs/tasks/administer-cluster/topology-manager/#topology-manager-policies) on kubelet:

````
   1. single-numa-node
   2. best-effort
   3. restricted
   4. none

````

For example

```
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: vj-test
spec:
  schedulerName: volcano
  minAvailable: 1
  tasks:
    - replicas: 1
      name: "test"
      topologyPolicy: best-effort # set the topology policy for task 
      template:
        spec:
          containers:
            - image: alpine
              command: ["/bin/sh", "-c", "sleep 1000"]
              imagePullPolicy: IfNotPresent
              name: running
              resources:
                limits:
                  cpu: 20
                  memory: "100Mi"
          restartPolicy: OnFailure
```

### Running TFJob with topology policy

Add the annotation **volcano.sh/numa-topology-policy** to specify the topology policy you want.

```
apiVersion: kubeflow.org/v1
kind: TFJob
metadata:
  generateName: tfjob
  name: tfjob-test
spec:
  tfReplicaSpecs:
    PS:
      replicas: 1
      restartPolicy: OnFailure
      template:
        metadata:
          annotations:
            sidecar.istio.io/inject: "false"
            volcano.sh/numa-topology-policy: "best-effort" # set the topology policy for pod
        spec:
          containers:
          - name: tensorflow
            image: alpine:latest
            imagePullPolicy: IfNotPresent
            command: ["/bin/sh", "-c", "sleep 1000"]
            resources:
              limits:
                cpu: 15
                memory: 2Gi
              requests:
                cpu: 15
                memory: 2Gi
    Worker:
      replicas: 1
      restartPolicy: OnFailure
      template:
        metadata:
          annotations:
            sidecar.istio.io/inject: "false"
            volcano.sh/numa-topology-policy: "best-effort"
        spec:
          containers:
          - name: tensorflow
            image: alpine:latest
            imagePullPolicy: IfNotPresent
            command: ["/bin/sh", "-c", "sleep 1000"]
            resources:
              limits:
                cpu: 15
                memory: 2Gi
              requests:
                cpu: 15
                memory: 2Gi
```

### Practice

|worker node|allocatable cpu on NUMA node 0|allocatable cpu on NUMA node 2|
|-----|----|-----|
| node-1| 12 | 12|
| node-2| 20 | 20|

Submit a volcano job as the following:

```
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: vj-test
spec:
  schedulerName: volcano
  minAvailable: 1
  tasks:
    - replicas: 1
      name: "test"
      topologyPolicy: best-effort # set the topology policy for task 
      template:
        spec:
          containers:
            - image: alpine
              command: ["/bin/sh", "-c", "sleep 1000"]
              imagePullPolicy: IfNotPresent
              name: running
              resources:
                limits:
                  cpu: 16
                  memory: "100Mi"
          restartPolicy: OnFailure
```

The pod will be scheduled to node-2, because it can allocate the cpu request of the pod on a single NUMA node and the node-1 needs to do this on two NUMA nodes.

## GPU NUMA-aware scheduling

The numa-aware plugin can also schedule GPU workloads by NUMA affinity. On multi-socket servers each GPU is attached to a specific NUMA node over PCIe, and accessing memory across NUMA boundaries costs more latency, so keeping the GPUs (and the CPUs feeding them) on the same NUMA node helps for training and inference.

### How it works

The plugin tries to allocate GPUs from as few NUMA nodes as possible. When a pod asks for both CPUs and GPUs, it prefers nodes where both come from the same NUMA node. Nodes that can't meet the request under the topology policy are filtered out in the predicate phase.

The data comes from a few places:

1. The resource-exporter DaemonSet reads `/sys/bus/pci/devices/*/numa_node` and writes the GPU affinity into the `gpuDetail` field of the `Numatopology` CRD.
2. The scheduler watches the CRD and fills `GPUDetail` in its cache.
3. At schedule time the gpuMng hint provider builds topology hints from the available GPUs per NUMA node, and the scorer unions the NUMA bitmask across the CPU and GPU assignments.

### Prerequisites for GPU scheduling

On top of the [CPU NUMA prerequisites](#pre-condition) above, you also need:

1. NVIDIA GPUs with the [NVIDIA device plugin](https://github.com/NVIDIA/k8s-device-plugin) installed.
2. A resource-exporter build with GPU topology support (the discovery patch from [resource-exporter#12](https://github.com/volcano-sh/resource-exporter/pull/12)).

The resource-exporter finds the GPUs via sysfs and fills the `gpuDetail` field in the `Numatopology` CRD.

### Verify GPU topology data

After the resource-exporter is running on GPU nodes, verify that GPU topology data is present:

```bash
kubectl get numatopo <node-name> -o yaml
```

You should see a `gpuDetail` section in the spec:

```yaml
apiVersion: nodeinfo.volcano.sh/v1alpha1
kind: Numatopology
metadata:
  name: gpu-node-1
spec:
  policies:
    CPUManagerPolicy: static
    TopologyManagerPolicy: best-effort
  cpuDetail:
    "0": {"numa": 0, "socket": 0, "core": 0}
    "1": {"numa": 0, "socket": 0, "core": 1}
    # ... more CPUs
  gpuDetail:
    "0": {"numa": 0, "busID": "0000:3b:00.0", "deviceModel": "NVIDIA A100"}
    "1": {"numa": 0, "busID": "0000:86:00.0", "deviceModel": "NVIDIA A100"}
    "2": {"numa": 0, "busID": "0000:af:00.0", "deviceModel": "NVIDIA A100"}
    "3": {"numa": 0, "busID": "0000:d8:00.0", "deviceModel": "NVIDIA A100"}
    "4": {"numa": 1, "busID": "0000:3c:00.0", "deviceModel": "NVIDIA A100"}
    "5": {"numa": 1, "busID": "0000:87:00.0", "deviceModel": "NVIDIA A100"}
    "6": {"numa": 1, "busID": "0000:b0:00.0", "deviceModel": "NVIDIA A100"}
    "7": {"numa": 1, "busID": "0000:d9:00.0", "deviceModel": "NVIDIA A100"}
  numares:
    nvidia.com/gpu:
      allocatable: "0-7"
      capacity: 8
```

If `gpuDetail` is missing or empty, check the resource-exporter logs:

```bash
kubectl logs -n volcano-system -l app=resource-exporter
```

### Running GPU jobs with a topology policy

#### Volcano job with GPU NUMA affinity

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: gpu-training-job
spec:
  schedulerName: volcano
  minAvailable: 1
  tasks:
    - replicas: 1
      name: "trainer"
      topologyPolicy: best-effort
      template:
        spec:
          containers:
            - image: nvcr.io/nvidia/pytorch:24.01-py3
              name: training
              command: ["python", "train.py"]
              resources:
                limits:
                  cpu: 16
                  memory: "64Gi"
                  nvidia.com/gpu: 4
                requests:
                  cpu: 16
                  memory: "64Gi"
                  nvidia.com/gpu: 4
          restartPolicy: OnFailure
```

With `topologyPolicy: best-effort` the scheduler prefers a node where all 4 GPUs fit on one NUMA node. If no single node has 4 free GPUs it still allows a cross-NUMA placement, but picks the node spanning the fewest NUMA nodes, and keeps the 16 CPUs on the same NUMA node as the GPUs when it can.

#### single-numa-node policy for strict locality

For latency-sensitive inference, use `single-numa-node` to force all GPUs (and CPUs) onto one NUMA node:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: gpu-inference-job
spec:
  schedulerName: volcano
  minAvailable: 1
  tasks:
    - replicas: 1
      name: "inference"
      topologyPolicy: single-numa-node
      template:
        spec:
          containers:
            - image: nvcr.io/nvidia/tritonserver:24.01-py3
              name: inference
              resources:
                limits:
                  cpu: 8
                  memory: "32Gi"
                  nvidia.com/gpu: 2
                requests:
                  cpu: 8
                  memory: "32Gi"
                  nvidia.com/gpu: 2
          restartPolicy: OnFailure
```

With `single-numa-node` the scheduler rejects any node that can't provide both GPUs from one NUMA node.

#### PyTorchJob with GPU NUMA affinity

For distributed training with Kubeflow PyTorchJob:

```yaml
apiVersion: kubeflow.org/v1
kind: PyTorchJob
metadata:
  name: distributed-training
spec:
  pytorchReplicaSpecs:
    Master:
      replicas: 1
      restartPolicy: OnFailure
      template:
        metadata:
          annotations:
            volcano.sh/numa-topology-policy: "best-effort"
        spec:
          schedulerName: volcano
          containers:
          - name: pytorch
            image: nvcr.io/nvidia/pytorch:24.01-py3
            resources:
              limits:
                cpu: 16
                memory: 64Gi
                nvidia.com/gpu: 4
              requests:
                cpu: 16
                memory: 64Gi
                nvidia.com/gpu: 4
    Worker:
      replicas: 3
      restartPolicy: OnFailure
      template:
        metadata:
          annotations:
            volcano.sh/numa-topology-policy: "best-effort"
        spec:
          schedulerName: volcano
          containers:
          - name: pytorch
            image: nvcr.io/nvidia/pytorch:24.01-py3
            resources:
              limits:
                cpu: 16
                memory: 64Gi
                nvidia.com/gpu: 4
              requests:
                cpu: 16
                memory: 64Gi
                nvidia.com/gpu: 4
```

### GPU scheduling example

Consider a cluster with two GPU nodes, each having 8 NVIDIA A100 GPUs across 2 NUMA nodes:

| Node | GPUs on NUMA 0 | GPUs on NUMA 1 | Available GPUs on NUMA 0 | Available GPUs on NUMA 1 |
|------|---------------|----------------|--------------------------|--------------------------|
| gpu-node-1 | 4 | 4 | 2 | 4 |
| gpu-node-2 | 4 | 4 | 4 | 4 |

Submit a job requesting 4 GPUs with `best-effort` topology policy:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: gpu-numa-test
spec:
  schedulerName: volcano
  minAvailable: 1
  tasks:
    - replicas: 1
      name: "test"
      topologyPolicy: best-effort
      template:
        spec:
          containers:
            - image: nvcr.io/nvidia/pytorch:24.01-py3
              command: ["nvidia-smi"]
              name: gpu-test
              resources:
                limits:
                  nvidia.com/gpu: 4
          restartPolicy: OnFailure
```

This lands on gpu-node-2, since it can give all 4 GPUs from one NUMA node (both NUMA 0 and NUMA 1 have 4 free). gpu-node-1 only has 2 free on NUMA 0, so 4 GPUs there would span both nodes.

### Scoring with mixed CPU+GPU workloads

When a pod asks for CPUs and GPUs together, the scorer unions the NUMA nodes used by the two assignments. If both land on the same NUMA node the union count is 1 and the node scores highest; if the CPUs are on NUMA 0 and the GPUs on NUMA 1 the count is 2 and the score drops. So the scheduler ends up co-locating CPUs and GPUs on the same NUMA node when it can, which is what you want when the CPU is feeding the GPU over PCIe.

### Limitations

The scheduler picks the best node for NUMA alignment, but the actual device assignment is still done by kubelet's device plugin (the NVIDIA device plugin, for example). The scheduler doesn't pick the exact GPU devices handed to a container.

To keep the scheduler's preference and the real allocation in sync:

- Set kubelet's Topology Manager policy to `restricted` or `single-numa-node` so kubelet rejects allocations that break the NUMA constraint.
- Longer term, Dynamic Resource Allocation (DRA) is the real fix. Once DRA is GA the scheduler can make the binding allocation decision that kubelet has to honour.

### Troubleshooting

| Symptom | Cause | Solution |
|---------|-------|----------|
| `gpuDetail` is empty in `numatopo` CRD | Resource-exporter cannot find NVIDIA GPUs | Verify GPUs are visible: `ls /sys/bus/pci/devices/*/vendor` should include `0x10de` (NVIDIA) |
| Pod stays Pending with topology policy | No node satisfies the topology constraint | Relax the policy (e.g., `best-effort` instead of `single-numa-node`) or request fewer GPUs |
| GPUs allocated across NUMA nodes despite `best-effort` | Not enough GPUs available on a single NUMA node | Check available GPU count per NUMA: `kubectl get numatopo <node> -o yaml` |
| Scheduler logs show "no GPU topology info" | `GPUDetail` not populated in scheduler cache | Ensure resource-exporter is running and the `numatopo` CRD has `gpuDetail` data |
