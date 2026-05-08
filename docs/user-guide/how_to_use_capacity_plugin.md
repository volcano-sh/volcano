# Capacity  Plugin User Guide

## Introduction

Capacity plugin is a replacement of proportion plugin, but instead of dividing the queue's deserved resources by weight, it realizes elastic queue capacity management i.e., queue's resource borrowing and lending mechanism by specifying the amount of deserved resources for each dimension resource of the queue. 

A queue can use the idle resources of other queues, and when other queues submit jobs, they can reclaim the resources that have been lent, and the amount of reclaimed resources is the amount of queue's deserved resources. For more detail,  please see [Capacity scheduling design](../design/capacity-scheduling.md)

## Environment setup

### Install volcano

Refer to [Install Guide](https://github.com/volcano-sh/volcano/blob/master/installer/README.md) to install volcano.

After installed, update the scheduler configuration:

```shell
kubectl edit cm -n volcano-system volcano-scheduler-configmap
```

Please make sure

- reclaim action is enabled.
- capacity plugin is enabled and proportion plugin is removed.

Note:  capacity and proportion plugin are in conflict, the two plugins cannot be used together.

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, backfill, reclaim" # add reclaim action.
    tiers:
    - plugins:
      - name: priority
      - name: gang
        enablePreemptable: false
      - name: conformance
    - plugins:
      - name: drf
        enablePreemptable: false
      - name: predicates
      - name: capacity # add this field and remove proportion plugin.
      - name: nodeorder
      - name: binpack
```

## Config queue's deserved resources

Assume there are two nodes and two queues named queue1 and queue2 in your kubernetes cluster, and each node has 4 CPU and 16Gi memory, then there will be total 8 CPU and 32Gi memory in your cluster.

```yaml
allocatable:
  cpu: "4"
  memory: 16Gi
  pods: "110"
```

config queue1's deserved field with 2 cpu and 8Gi memory.

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: queue1
spec:
  reclaimable: true
  deserved: # set the deserved field.
    cpu: 2
    memeory: 8Gi
```

config queue2's deserved field with 6 cpu and 24Gi memory.

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: queue2
spec:
  reclaimable: true
  deserved: # set the deserved field.
    cpu: 6
    memory: 24Gi
```

## Submit pods to each queue

First, submit a deployment named demo-1 to queue1 with replicas=8 and each pod requests 1 cpu and 4Gi memory, because queue2 is idle, so queue1 can use the whole clusters' resources, and you can see that 8 pods are in Running state.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-1
spec:
  selector:
    matchLabels:
      app: demo-1
  replicas: 8
  template:
    metadata:
      labels:
        app: demo-1
      annotations:
        scheduling.volcano.sh/queue-name: "queue1" # set the queue
    spec:
      schedulerName: volcano
      containers:
      - name: nginx
        image: nginx:1.14.2
        resources:
          requests:
            cpu: 1
            memory: 4Gi
        ports:
        - containerPort: 80
```

Expected result:

```shell
$ kubectl get po                                                                                             
NAME                      READY   STATUS    RESTARTS   AGE
demo-1-7bc649f544-2wjg7   1/1     Running   0          5s
demo-1-7bc649f544-cvsmr   1/1     Running   0          5s
demo-1-7bc649f544-j5lzp   1/1     Running   0          5s
demo-1-7bc649f544-jvlbx   1/1     Running   0          5s
demo-1-7bc649f544-mzgg2   1/1     Running   0          5s
demo-1-7bc649f544-ntrs2   1/1     Running   0          5s
demo-1-7bc649f544-nv424   1/1     Running   0          5s
demo-1-7bc649f544-zd6d9   1/1     Running   0          5s
```

Then submit a deployment named demo-2 to queue2 with replicas=8 and each pod requests 1 cpu and 4Gi memory.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo-2
spec:
  selector:
    matchLabels:
      app: demo-2
  replicas: 8
  template:
    metadata:
      labels:
        app: demo-2
      annotations:
        scheduling.volcano.sh/queue-name: "queue2" # set the queue
    spec:
      schedulerName: volcano
      containers:
      - name: nginx
        image: nginx:1.14.2
        resources:
          requests:
            cpu: 1
            memory: 4Gi
        ports:
        - containerPort: 80
```

Because queue1 occupied queue2's resources, so queue2 will reclaim its deserved resources with 6 cpu and 24Gi memory. And each pod of demo-2 request 1 cpu and 4Gi memory, so there will be 6 Pods in Running state of demo-2,  and demo-1's pods will be evicted. 

Finally, you can see that there are 2 Running pods in demo-1(belongs to queue1), and 6 Running pods in demo-2(belongs to queue2), which meets queue's deserved resources respectively.

```shell
$ kubectl get po                                                                                             
NAME                      READY   STATUS    RESTARTS   AGE
demo-1-7bc649f544-4vvdv   0/1     Pending   0          37s
demo-1-7bc649f544-c6mds   0/1     Pending   0          37s
demo-1-7bc649f544-j5lzp   1/1     Running   0          14m
demo-1-7bc649f544-mzgg2   1/1     Running   0          14m
demo-1-7bc649f544-pqdgk   0/1     Pending   0          37s
demo-1-7bc649f544-tx6wp   0/1     Pending   0          37s
demo-1-7bc649f544-wmshq   0/1     Pending   0          37s
demo-1-7bc649f544-wrhrr   0/1     Pending   0          37s
demo-2-6dfb86c49b-2jvgm   0/1     Pending   0          37s
demo-2-6dfb86c49b-dnjzv   1/1     Running   0          37s
demo-2-6dfb86c49b-fzvmp   1/1     Running   0          37s
demo-2-6dfb86c49b-jlf69   1/1     Running   0          37s
demo-2-6dfb86c49b-k62f7   1/1     Running   0          37s
demo-2-6dfb86c49b-k9b9v   1/1     Running   0          37s
demo-2-6dfb86c49b-rpzvg   0/1     Pending   0          37s
demo-2-6dfb86c49b-zch7w   1/1     Running   0          37s
```

