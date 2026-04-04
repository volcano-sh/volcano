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

Support the task-level topology policy and edit **spec.tasks.topologyPolicy** to specify whether to perform topology scheduling.<br> The supported options are the same as [topology manager](https://v1-19.docs.kubernetes.io/docs/tasks/administer-cluster/topology-manager/) on kubelet:

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

## GPU NUMA-Aware Scheduling

The numa-aware plugin supports **GPU topology-aware scheduling**. This enables Volcano to consider GPU-to-NUMA node affinity when placing GPU workloads, reducing cross-NUMA memory traffic and improving performance for AI/ML training and inference.

### How It Works

On multi-socket servers, each GPU is physically attached to a specific NUMA node via PCIe. Accessing memory across NUMA boundaries incurs higher latency. GPU NUMA-aware scheduling ensures that:

1. GPUs are preferentially allocated from the **fewest NUMA nodes** possible.
2. When both CPUs and GPUs are requested, the scheduler favors nodes where they share the **same NUMA node**, minimizing cross-NUMA data transfers.
3. Nodes that cannot satisfy the GPU request within the topology policy are **filtered out** during the predicate phase.

**Data flow:**
1. The **resource-exporter** DaemonSet discovers GPU NUMA affinity by reading `/sys/bus/pci/devices/*/numa_node` and writes it to the `Numatopology` CRD as the `gpuDetail` field.
2. The **Volcano scheduler** watches the CRD and populates `GPUDetail` in its internal cache.
3. During scheduling, the **gpuMng** hint provider generates topology hints based on available GPUs per NUMA node, and the scorer computes a unified NUMA bitmask across CPU and GPU assignments.

### Prerequisites for GPU Scheduling

In addition to the [CPU NUMA prerequisites](#pre-condition) above, GPU NUMA-aware scheduling requires:

1. **NVIDIA GPUs** with the [NVIDIA device plugin](https://github.com/NVIDIA/k8s-device-plugin) installed
2. **Volcano resource-exporter** with GPU topology support (requires the GPU discovery patch from [resource-exporter#12](https://github.com/volcano-sh/resource-exporter/pull/12))

The resource-exporter automatically discovers NVIDIA GPUs via sysfs and populates the `gpuDetail` field in the `Numatopology` CRD.

### Verify GPU Topology Data

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

### Running GPU Jobs with Topology Policy

#### Volcano Job with GPU NUMA Affinity

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

With `topologyPolicy: best-effort`, Volcano will:
1. Prefer nodes where all 4 GPUs can be allocated from the **same NUMA node**.
2. If no single NUMA node has 4 available GPUs, allow cross-NUMA allocation but still pick the node with the fewest NUMA nodes spanned.
3. Co-locate the 16 requested CPUs on the same NUMA node as the GPUs when possible.

#### Single-NUMA-Node Policy for Strict Locality

For latency-sensitive inference workloads, use `single-numa-node` to enforce that all GPUs (and CPUs) come from exactly one NUMA node:

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

With `single-numa-node`, the scheduler will **reject** any node where 2 GPUs cannot be provided from a single NUMA node.

#### PyTorchJob with GPU NUMA Affinity

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

### GPU Scheduling Practice

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

**Scheduling decision:** The pod will be scheduled to **gpu-node-2**, because it can allocate all 4 GPUs from a single NUMA node (NUMA 0 or NUMA 1 both have 4 available). On gpu-node-1, NUMA 0 only has 2 available GPUs, so satisfying 4 GPUs requires spanning both NUMA nodes.

### Scoring with Mixed CPU+GPU Workloads

When a pod requests both CPUs and GPUs, the scorer computes the **union** of NUMA nodes used by the CPU and GPU assignments. This means:

- If CPUs and GPUs are allocated from the **same NUMA node**, the union count is 1 → highest score.
- If CPUs are on NUMA 0 and GPUs are on NUMA 1, the union count is 2 → lower score.

This ensures that the scheduler naturally co-locates CPUs and GPUs on the same NUMA node when possible, which is critical for AI workloads where the CPU feeds data to the GPU over PCIe.

### Limitations

The scheduler determines the best node for GPU NUMA alignment, but the actual GPU device allocation is performed by kubelet's device plugin (e.g., the NVIDIA device plugin). The scheduler does not directly control which specific GPU devices are assigned to a container.

To ensure consistency between the scheduler's NUMA preference and the actual allocation:

- Set kubelet's **Topology Manager policy** to `restricted` or `single-numa-node`. This causes kubelet to reject allocations that violate the NUMA topology constraint, ensuring alignment with the scheduler's decision.
- For full scheduler-controlled allocation, **Dynamic Resource Allocation (DRA)** is the long-term solution. Once DRA is GA, the scheduler can make binding allocation decisions that kubelet must respect.

### Troubleshooting

| Symptom | Cause | Solution |
|---------|-------|----------|
| `gpuDetail` is empty in `numatopo` CRD | Resource-exporter cannot find NVIDIA GPUs | Verify GPUs are visible: `ls /sys/bus/pci/devices/*/vendor` should include `0x10de` (NVIDIA) |
| Pod stays Pending with topology policy | No node satisfies the topology constraint | Relax the policy (e.g., `best-effort` instead of `single-numa-node`) or request fewer GPUs |
| GPUs allocated across NUMA nodes despite `best-effort` | Not enough GPUs available on a single NUMA node | Check available GPU count per NUMA: `kubectl get numatopo <node> -o yaml` |
| Scheduler logs show "no GPU topology info" | `GPUDetail` not populated in scheduler cache | Ensure resource-exporter is running and the `numatopo` CRD has `gpuDetail` data |
