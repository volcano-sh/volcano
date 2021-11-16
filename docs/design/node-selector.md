## Introduction

for example, my k8s cluster has 10 nodes (10 gpu per node), 5 for training, 5 for serving, I want to use volcano schedule tfjob/pytorchjob/vcjob on training nodes. use default-scheduler schedule online pod on serving nodes. it is work by setting nodeSelector or nodeAffinity to pod, but it is not properly when considering volcano queue mechanism.

if there are two queue: queue1(weight=1) and queue2 (weight=1), it is overused if queue1 used 40 gpu, but it's weight resource is 50 (100 * 1/2), so if queue2 want use 20 gpu, reclaimable mechanism will not work.

so it is necessary to limit volcano scheduler that it can only work on training nodes, not all nodes in cluster.

so I add nodeSelector for volcano scheduler.

## Usage

in following example, volcano can only work on the node which has `node-role:training` or `node-role:dev` or `gpu-model: tesla` label. you can use any label name you like.

```yaml
    actions: "enqueue, allocate,reclaim, backfill"
    tiers:
    - plugins:
      - name: priority
      - name: gang
      - name: conformance
    - plugins:
      - name: overcommit
      - name: drf
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
    nodeSelector:
      node-role: training,dev
      gpu-model: tesla
```

## Design

volcano use nodeInformer to get all node list in k8s cluster. so we can filter the node list by nodeSelector label. 

