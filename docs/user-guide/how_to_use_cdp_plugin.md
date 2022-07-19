# Cooldown Protection Plugin User Guide

## Background
When we need to enable elastic training or serving, preemptible job's pods can be preempted or back to running repeatedly, if no cooldown protection set, these pods can be preempted again after they just started for a short time, this may cause service stability dropped.
So we add "cdp" plugin to ensure preemptible job's pods can run for at least some time set by user.

In another case, we do not want some tasks always be evicted, which makes it be starved to death, we need a way to protect them.

## Environment setup

### Install volcano

Refer to [Install Guide](../../installer/README.md) to install volcano.

### Update scheduler configmap

After installed, update the scheduler configuration:

```shell
kubectl edit configmap -n volcano-system volcano-scheduler-configmap
```

Register `cdp` plugin in configmap while enable `preempt` action

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, preempt, backfill"
    tiers:
    - plugins:
      - name: priority
      - name: gang
      - name: conformance
      - name: cdp
    - plugins:
      - name: drf
      - name: predicates
      - name: task-topology
        arguments:
          task-topology.weight: 10
      - name: proportion
      - name: nodeorder
      - name: binpack
```

### Running Jobs

Take a simple volcano job as sample.

original job yaml is as below, which has "ps" and "worker" task

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: test-job
spec:
  minAvailable: 3
  schedulerName: volcano
  priorityClassName: high-priority
  plugins:
    ssh: []
    env: []
    svc: []
  maxRetry: 5
  queue: default
  volumes:
    - mountPath: "/myinput"
    - mountPath: "/myoutput"
      volumeClaimName: "testvolumeclaimname"
      volumeClaim:
        accessModes: [ "ReadWriteOnce" ]
        storageClassName: "my-storage-class"
        resources:
          requests:
            storage: 1Gi
  tasks:
    - replicas: 6
      name: "worker"
      template:
        metadata:
          name: worker
        spec:
          containers:
            - image: nginx
              imagePullPolicy: IfNotPresent
              name: nginx
              resources:
                requests:
                  cpu: "1"
          restartPolicy: OnFailure
    - replicas: 2
      name: "ps"
      template:
        metadata:
          name: ps
        spec:
          containers:
            - image: nginx
              imagePullPolicy: IfNotPresent
              name: nginx
              resources:
                requests:
                  cpu: "1"
          restartPolicy: OnFailure

```

#### Edit yaml of vcjob

1. add annotations in volcano job in format below.
   1. `volcano.sh/preemptable` annotation indicates that job or task is preemptable
   2. `volcano.sh/cooldown-time` annotation indicates cooldown time for the entire job or dedicated task. Value for the annotation indicates cooldown time, valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h". 
   3. `volcano.sh/max-cooldown-times` annotation indicates max number of cooldown(eviction times) for entire job or dedicated task. if the task cooldown times are greater than this value, the task cannot be evicted.
        ```yaml
            volcano.sh/preemptable: "true"
            volcano.sh/cooldown-time: "600s"
            volcano.sh/max-cooldown-times: "2"
        ```

**Example 1**

Add annotation to entire job, then "ps" and "worker" task can be preempted and all have cooldown time support.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: test-job
  annotations:
    volcano.sh/preemptable: "true"
    volcano.sh/cooldown-time: "600s"
spec:
  ... # below keep the same
```

**Example 2**

Add annotation to dedicated task, as shown below, only "worker" can be preempted and have cooldown time support.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: test-job
spec:
  minAvailable: 3
  schedulerName: volcano
  priorityClassName: high-priority
  plugins:
    ssh: []
    env: []
    svc: []
  maxRetry: 5
  queue: default
  volumes:
    - mountPath: "/myinput"
    - mountPath: "/myoutput"
      volumeClaimName: "testvolumeclaimname"
      volumeClaim:
        accessModes: [ "ReadWriteOnce" ]
        storageClassName: "my-storage-class"
        resources:
          requests:
            storage: 1Gi
  tasks:
    - replicas: 6
      name: "worker"
      template:
        metadata:
          name: worker
          annotations:     # add annotation in tasks
            volcano.sh/preemptable: "true"
            volcano.sh/cooldown-time: "600s"
        spec:
          containers:
            - image: nginx
              imagePullPolicy: IfNotPresent
              name: nginx
              resources:
                requests:
                  cpu: "1"
          restartPolicy: OnFailure
    - replicas: 2
      name: "ps"
      template:
        metadata:
          name: ps
        spec:
          containers:
            - image: nginx
              imagePullPolicy: IfNotPresent
              name: nginx
              resources:
                requests:
                  cpu: "1"
          restartPolicy: OnFailure

```

**Example 3**

Add annotation to dedicated task, as shown below, the only task in a job named "test-low-job" can be preempted and have the maximum number of times of cooldown support.

In that case, if the cluster only has 6 CPU cores, the first step users apply a job named "test-low-job", then apply a job named "test-middle-job", after the job named "test-middle-job" is finished, the job named "test-low-job" running again.  At this moment, apply the task job named "test-high-job" can not preempted the task job named "test-low-job". because the cooldown is protected, the job named "test-low-job" can just be preempted one time when set annotation `volcano.sh/max-cooldown-times: "1"`
```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
   name: test-low-job
spec:
   minAvailable: 3
   schedulerName: volcano
   priorityClassName: low-priority
   maxRetry: 5
   queue: default
   tasks:
      - replicas: 6
        name: tasks
        template:
           metadata:
              name: worker
              annotations:
                 volcano.sh/preemptable: 'true'
                 volcano.sh/max-cooldown-times: '1'
           spec:
              containers:
                 - image: alpine
                   imagePullPolicy: IfNotPresent
                   name: sleep
                   command:
                      - /bin/ash
                      - '-c'
                      - sleep 3600
                   resources:
                      requests:
                         cpu: '1'
              restartPolicy: OnFailure
---
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
   name: test-middle-job
spec:
   minAvailable: 3
   schedulerName: volcano
   priorityClassName: middle-priority
   maxRetry: 5
   queue: default
   tasks:
      - replicas: 6
        name: tasks
        template:
           metadata:
              name: worker
           spec:
              containers:
                 - image: alpine
                   imagePullPolicy: IfNotPresent
                   name: sleep
                   command:
                      - /bin/ash
                      - '-c'
                      - sleep 60
                   resources:
                      requests:
                         cpu: '1'
              restartPolicy: OnFailure
---
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
   name: test-high-job
spec:
   minAvailable: 3
   schedulerName: volcano
   priorityClassName: high-priority
   maxRetry: 5
   queue: default
   tasks:
      - replicas: 6
        name: tasks
        template:
           metadata:
              name: worker
           spec:
              containers:
                 - image: alpine
                   imagePullPolicy: IfNotPresent
                   name: sleep
                   command:
                      - /bin/ash
                      - '-c'
                      - sleep 3600
                   resources:
                      requests:
                         cpu: '1'
              restartPolicy: OnFailure
```