# NUMA Aware User Guide

## Environment setup

### Pre-Condition

- Enable cpu manager and set policy to "static"
- Enable topology manager and set the policy option you want
    <br><br>
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
