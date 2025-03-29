# How to use kwok to create fake nodes to simulate scheduling
## Background
- In production, we may meet bugs related with GPU/NPU scheduling, we have to reproduce to find out where the bugs are. But in fact, we may not have a GPU/NPU testing environment to reproduce, so create fake nodes can help us to simulate scheduling not only to do features validation about GPU/NPU/[other extended resources] scheduling, but also help us to reproduce to locate scheduler bugs.
- Simulate scheduling can help us to do performance testing of scheduler, expecially for large-scale clusters, and kwok can create many fake nodes with very few resources.
## Usage
There are to two scripts in kwok dir to install kwok and create specific number of fake nodes. But first you need to have a kubernetes cluster.
### Install kwok
Execute the script `install-kwok.sh` under `hack/kwok` to install related CRDs of kwok. 
### Create fake nodes
There is a script `create-fake-node.sh` under `hack/kwok` to help create example fake nodes. By default, directly execute the script `./create-fake-node.sh` will help us to create a fake node at Ready stage with 32 core CPUs, 256Gi memories and 110 pods for allocation. You can pass command line parameters to set specfic number of CPUs, memories, pods and even extended resources such as GPU/NPU etc, or create multiple fake nodes. Using like:
```shell
# create 10 fake nodes with 4 CPUs, 8Gi memories and extended resources with volcano.sh/gpu-number=4,volcano.sh/gpu-memory=20
./create-fake-node.sh -n 10 -c 4 -m 8Gi -e volcano.sh/gpu-number=4,volcano.sh/gpu-memory=20
```
You can use `./create-fake-node.sh -h` to see more details of command line parameters:
```shell
Usage: ./create-fake-node.sh [options]
   -n NODE_COUNT   Number of nodes to create (default: 1)
   -b BASE_NODE_NAME Base name for nodes (default: kwok-node)
   -c CPU          Amount of CPU resources that can be allocated (default: 32)
   -m MEMORY       Amount of memory resources that can be allocated (default: 256Gi)
   -p PODS         Number of pods can be allocated (default: 110)
   -e EXTENDED_RESOURCES   Pairs of amount of extended resources that can be allocated, e.g., 'gpu=1,npu=2'
   -h              Display this help message
```
### Deploy fake pods
Under `hack/kwok/examples`, there is a example deployment yaml to create a fake pod, requests 2 CPU cores and 4Gi memories, you can follow this yaml to deploy your workload in writing on your own.
