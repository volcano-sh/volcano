# GPU Request with Nvidia GPU Operator User guide

## Environment setup

### Install Nvidia GPU-Operator

Install Nvidia's GPU Operator following their [instructions](https://docs.nvidia.com/datacenter/cloud-native/gpu-operator/getting-started.html#install-kubernetes) choosing your appropriate environmental setup (This has been tested with v1.8 without preinstalled drivers and with containerD as CRI).

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
        arguments:
          predicate.GPUNumberEnable: true # enables GPU requests
      - name: proportion
      - name: nodeorder
      - name: binpack
```

#### 2. Install from release package.

Same as above, after installed, update the scheduler configuration in `volcano-scheduler-configmap` configmap.

### Verify environment is ready

After installing gpu-operator, your nodes should be annotated verbosely with information such as `nvidia.com/gpu.product` and allocatable/capacity annotations with `nvidia.com/gpu`. These are the main annotations that are used for scheduling.

### Running GPU Sharing Jobs

NVIDIA GPUs can now be allocated at the container level resource requirements using the resource name `nvidia.com/gpu` and scheduled on particular nodes with nodeSelector `nvidia.com/gpu.product` as shown below:

```shell script
$ cat << EOF | kubectl apply -f -
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: vc-gpu-job
spec:
  minAvailable: 1
  schedulerName: volcano
  tasks:
    - replicas: 1
      name: cuda-test
      template:
        spec:
          nodeSelector:
            nvidia.com/gpu.product: NVIDIA-GeForce-RTX-3090 # Run on node with RTX3090
          containers:
            - name: cuda-container
              command:
              - sleep
              - 5m
              image: nvidia/cuda:11.4.2-base-ubuntu20.04
              resources:
               limits:
                 nvidia.com/gpu: 1 # Request a single GPU for the Pod
EOF
```

You should then be able to see the pods scheduled on nodes and can only view the number of cards they have requested by attaching a terminal and running `nvidia-smi` and use as any normal GPU.

### Understanding How Multiple GPU Cards Requirement Works

The scheduling happens the exact way with previous implementations, but uses annotations provided by gpu-operator and does not add extra annotations such as environment variables to pods, gpu exposure is handled by nvidia's drivers.
