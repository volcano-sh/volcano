# How to use dynamicresources plugin 
## Introduction
Dynamicresources plugin introduces the DRA(Dynamic Resource Allocation) into volcano enabling user to use extended device 
such as GPU/NPU, FPGA, etc. Users can declare `ResourceClaim` in the Pod and carry some parameters to enhance the scheduling 
of nodes with extended devices. For more detail, please see [dynamicresources-plugin.md](../design/dynamicresources-plugin.md)

## Environment setup

### Install volcano

Refer to [Install Guide](https://github.com/volcano-sh/volcano/blob/master/installer/README.md) to install volcano.

After installed, update the scheduler configuration:

```shell
kubectl edit cm -n volcano-system volcano-scheduler-configmap
```

Please make sure

- allocate or backfill action is enabled.
- dynamicresources plugin is enabled.
```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, backfill, reclaim, preempt"
    tiers:
    tiers:
    - plugins:
      - name: dynamicresources # dynamicresources plugin should be enabled
      - name: priority
      - name: gang
        enablePreemptable: false
      - name: conformance
      - name: sla
    - plugins:
      - name: overcommit
      - name: drf
        enablePreemptable: false
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
```
- the featuregate called DynamicResourceAllocation is set to true in scheduler args:
```yaml
- args:
  ...
  - --feature-gates=DynamicResourceAllocation=true
  ...
```

## Usage
### Deploy kubelet DRA driver
kubelet DRA driver runs as a daemonset to prepare extended devices and report `ResourceSlice` API. Here are some sample 
opensource projects for reference:
- https://github.com/NVIDIA/k8s-dra-driver: If you has Nvidia GPU on nodes, you can take this to test. 
- https://github.com/intel/intel-resource-drivers-for-kubernetes.git
- https://github.com/kubernetes-sigs/dra-example-driver:
It's the simplest repo to deploy a dra driver, it provides access to a set of mock GPU devices, it will print logs in container if the 
container allocated a mock GPU. You can refer to its #Demo part in README.md to deploy a simple dra driver, after deploying
the driver succeeded, you can apply the demo pods requesting `ResourceClaim`s to running:
```shell
kubectl apply --filename=demo/gpu-test{1,2,3,4,5}.yaml
```
After all the pods are running, you can dump the logs of each pods to verify that GPUs were allocated:
```shell
for example in $(seq 1 5); do \
  echo "gpu-test${example}:"
  for pod in $(kubectl get pod -n gpu-test${example} --output=jsonpath='{.items[*].metadata.name}'); do \
    for ctr in $(kubectl get pod -n gpu-test${example} ${pod} -o jsonpath='{.spec.containers[*].name}'); do \
      echo "${pod} ${ctr}:"
      if [ "${example}" -lt 3 ]; then
        kubectl logs -n gpu-test${example} ${pod} -c ${ctr}| grep -E "GPU_DEVICE_[0-9]+=" | grep -v "RESOURCE_CLAIM"
      else
        kubectl logs -n gpu-test${example} ${pod} -c ${ctr}| grep -E "GPU_DEVICE_[0-9]+" | grep -v "RESOURCE_CLAIM"
      fi
    done
  done
  echo ""
done
```
This should produce output similar to the following:
```shell
gpu-test1:
pod0 ctr0:
declare -x GPU_DEVICE_6="gpu-6"
pod1 ctr0:
declare -x GPU_DEVICE_7="gpu-7"

gpu-test2:
pod0 ctr0:
declare -x GPU_DEVICE_0="gpu-0"
declare -x GPU_DEVICE_1="gpu-1"

gpu-test3:
pod0 ctr0:
declare -x GPU_DEVICE_2="gpu-2"
declare -x GPU_DEVICE_2_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_2_TIMESLICE_INTERVAL="Default"
pod0 ctr1:
declare -x GPU_DEVICE_2="gpu-2"
declare -x GPU_DEVICE_2_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_2_TIMESLICE_INTERVAL="Default"

gpu-test4:
pod0 ctr0:
declare -x GPU_DEVICE_3="gpu-3"
declare -x GPU_DEVICE_3_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_3_TIMESLICE_INTERVAL="Default"
pod1 ctr0:
declare -x GPU_DEVICE_3="gpu-3"
declare -x GPU_DEVICE_3_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_3_TIMESLICE_INTERVAL="Default"

gpu-test5:
pod0 ts-ctr0:
declare -x GPU_DEVICE_4="gpu-4"
declare -x GPU_DEVICE_4_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_4_TIMESLICE_INTERVAL="Long"
pod0 ts-ctr1:
declare -x GPU_DEVICE_4="gpu-4"
declare -x GPU_DEVICE_4_SHARING_STRATEGY="TimeSlicing"
declare -x GPU_DEVICE_4_TIMESLICE_INTERVAL="Long"
pod0 sp-ctr0:
declare -x GPU_DEVICE_5="gpu-5"
declare -x GPU_DEVICE_5_PARTITION_COUNT="10"
declare -x GPU_DEVICE_5_SHARING_STRATEGY="SpacePartitioning"
pod0 sp-ctr1:
declare -x GPU_DEVICE_5="gpu-5"
declare -x GPU_DEVICE_5_PARTITION_COUNT="10"
declare -x GPU_DEVICE_5_SHARING_STRATEGY="SpacePartitioning"
```