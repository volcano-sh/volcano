# Tutorial of kube-batch

This doc will show how to run `kube-batch` as a kubernetes batch scheduler. It is for [master](https://github.com/kubernetes-sigs/kube-batch/tree/master) branch.

## Prerequisite
To run `kube-batch`, a Kubernetes cluster must start up. Here is a document on [Using kubeadm to Create a Cluster](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/). Additionally, for common purposes and testing and deploying on local machine, one can use Minikube. This is a document on [Running Kubernetes Locally via Minikube](https://kubernetes.io/docs/getting-started-guides/minikube/). Besides, you can also use [kind](https://github.com/kubernetes-sigs/kind) which is a tool for running local Kubernetes clusters using Docker container "nodes".

`kube-batch` need to run as a kubernetes scheduler. The next step will show how to run `kube-batch` as kubernetes scheduler quickly. Refer [Configure Multiple Schedulers](https://kubernetes.io/docs/tasks/administer-cluster/configure-multiple-schedulers/) to get more details.

## Install kube-batch for Kubernetes

### Download kube-batch

```bash
# mkdir -p $GOPATH/src/github.com/kubernetes-sigs/
# cd $GOPATH/src/github.com/kubernetes-sigs/
# git clone http://github.com/kubernetes-sigs/kube-batch -b v0.4.2
```

### Deploy kube-batch by Helm

Run `kube-batch` as kubernetes scheduler.

```bash
# helm install $GOPATH/src/github.com/kubernetes-sigs/kube-batch/deployment/kube-batch --namespace kube-system
```

Verify the release

```bash
# helm list
NAME        	REVISION	UPDATED                 	STATUS  	CHART                	NAMESPACE
dozing-otter	1       	Thu Mar 29 18:52:15 2019	DEPLOYED	kube-batch-0.4.2   	 kube-system
```

## Customize kube-batch

Please check following configurable fields and customize kube-batch based on your needs.

### kube-batch configuraions

| configs | default value | Descriptions |
| ----------- | ----------- | ------ |
| master | "" | The address of the Kubernetes API server (overrides any value in kubeconfig) |
| kubeconfig | "" | Path to kubeconfig file with authorization and master location information. If you run kube-batch outside cluster or for debug purpose, this is very useful |
| scheduler-name | kube-batch | kube-batch will handle pods whose `.spec.SchedulerName` is same as scheduler-name |
| scheduler-conf | "" | The absolute path of scheduler configuration file |
| default-queue | default | The default queue name of the job |
| schedule-period | Seconds | The period between each scheduling cycle |
| leader-elect | false | Start a leader election client and gain leadership before executing the main loop. Enable this when running replicated kube-batch for high availability |
| lock-object-namespace | "" | Define the namespace of the lock object that is used for leader election |
| listen-address | :8080 | The address to listen on for HTTP requests. Metrics will be exposed at this port |


### scheduler configuration

By default, scheduler will read following default configuraion as `scheduler-conf`.

```yaml
actions: "allocate, backfill"
tiers:
- plugins:
  - name: priority
  - name: gang
- plugins:
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodeorder
```

The `actions` is a list of actions that will be executed by kube-batch in order, although the "order" maybe incorrect; the kube-batch do not enforce that. In above example, `allocate, backfill` will be executed in order by kube-batch.

The `plugins` is a list of plugins that will be used by related actions, e.g. allocate. It includes several tiers of plugin list by names; if it fit plugins in high priority tier, the action will not go through the plugins in lower priority tiers. In each tier, it's considered passed if all plugins are fitted in `plugins.names`.

multiple tiers for plugins is introduced into kube-batch, high priority jobs will take all resources it need; if priority equal, shares resource by DRF.

Currently, kube-batch supports
- actions
  - allocate
  - backfill
  - preempt
  - reclaim
- plugins
  - conformance
  - drf
  - gang
  - nodeorder
  - predicates
  - priority
  - proportion


## kube-batch Features


### Gang-Scheduling
Create a file named `job-01.yaml` with the following content:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: qj-1
spec:
  backoffLimit: 6
  completions: 6
  parallelism: 6
  template:
    metadata:
      annotations:
        scheduling.k8s.io/group-name: qj-1
    spec:
      containers:
      - image: busybox
        imagePullPolicy: IfNotPresent
        name: busybox
        resources:
          requests:
            cpu: "1"
      restartPolicy: Never
      schedulerName: kube-batch
---
apiVersion: scheduling.incubator.k8s.io/v1alpha1
kind: PodGroup
metadata:
  name: qj-1
spec:
  minMember: 6
```

The yaml file means a Job named `qj-01` to create 6 pods(it is specified by `parallelism`), these pods will be scheduled by scheduler `kube-batch` (it is specified by `schedulerName`). `kube-batch` will watch `PodGroup`, and the annotation `scheduling.k8s.io/group-name` identify which group the pod belongs to. `kube-batch` will start `.spec.minMember` pods for a Job at the same time; otherwise, such as resources are not sufficient, `kube-batch` will not start any pods for the Job.

Create the Job

```bash
# kubectl create -f job-01.yaml
```

Check job status

```bash
# kubectl get jobs
NAME      DESIRED   SUCCESSFUL   AGE
qj-1      6         6            2h
```

Check the pods status

```bash
# kubectl get pod --all-namespaces
```


### Job PriorityClass

`kube-batch` scheduler will start pods by their priority in the same QueueJob, pods with higher priority will start first. Here is sample to show `PriorityClass` usage:

Create a `priority_1000.yaml` with the following contents:

```yaml
apiVersion: scheduling.k8s.io/v1beta1
kind: PriorityClass
metadata:
  name: high-priority
  namespace: batch-ns01
value: 1000
```

Create the PriorityClass with priority 1000.

```
# kubectl create -f priority_1000.yaml
```

Create a Pod configuration file (say `pod-config-ns01-r01.yaml`):

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-ns01-r01
spec:
  containers:
    - name: key-value-store
      image: redis
      resources:
        limits:
          memory: "1Gi"
          cpu: "1"
        requests:
          memory: "1Gi"
          cpu: "1"
      ports:
        - containerPort: 6379
  priorityClassName: high-priority
```

Create the Pod with priority 1000.

```
# kubectl create -f pod-config-ns01-r01.yaml
```


NOTE:

* `PriorityClass` is supported in kubernetes 1.9 or later.
* The pod in same Deployment/RS/Job share the same pod template, so they have the same `PriorityClass`.
  To specify a different `PriorityClass` for pods in same QueueJob, users need to create controllers by themselves.



## FAQ

### How to fix apiVersion unavailable error when I install kube-batch using helm?
`Error: apiVersion "scheduling.incubator.k8s.io/v1alpha1" in kube-batch/templates/default.yaml is not available`
please update your helm to latest version and try it again.


### What permissions should I grant to kube-batch? It failed to list resources at the cluster scope.

```
E0327 06:30:45.494416       1 reflector.go:134] github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache/cache.go:302: Failed to list *v1.Pod: pods is forbidden: User "system:serviceaccount:kube-system:default" cannot list pods at the cluster scope
E0327 06:30:45.495362       1 reflector.go:134] github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache/cache.go:306: Failed to list *v1.PersistentVolumeClaim: persistentvolumeclaims is forbidden: User "system:serviceaccount:kube-system:default" cannot list persistentvolumeclaims at the cluster scope
E0327 06:30:45.496355       1 reflector.go:134] github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache/cache.go:308: Failed to list *v1alpha1.Queue: queues.scheduling.incubator.k8s.io is forbidden: User "system:serviceaccount:kube-system:default" cannot list queues.scheduling.incubator.k8s.io at the cluster scope
E0327 06:30:45.497695       1 reflector.go:134] github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache/cache.go:301: Failed to list *v1beta1.PodDisruptionBudget: poddisruptionbudgets.policy is forbidden: User "system:serviceaccount:kube-system:default" cannot list poddisruptionbudgets.policy at the cluster scope
E0327 06:30:45.498641       1 reflector.go:134] github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache/cache.go:303: Failed to list *v1.Node: nodes is forbidden: User "system:serviceaccount:kube-system:default" cannot list nodes at the cluster scope
E0327 06:30:45.499895       1 reflector.go:134] github.com/kubernetes-sigs/kube-batch/pkg/scheduler/cache/cache.go:305: Failed to list *v1.PersistentVolume: persistentvolumes is forbidden: User "system:serviceaccount:kube-system:default" cannot list persistentvolumes at the cluster scope

```

`kube-batch` need to collect cluster information(such as Pod, Node, CRD, etc) for scheduling, so the service account used by the deployment must have permission to access those cluster resources, otherwise, `kube-batch` will fail to startup. For users who are not familiar with Kubernetes RBAC, please copy the example/role.yaml into `$GOPATH/src/github.com/kubernetes-sigs/kube-batch/deployment/kube-batch/templates/` and reinstall batch.


### How to enable gang-scheduling on kubeflow/tf-operator for kube-batch?
Please follow tf-operator [quick start](https://www.kubeflow.org/docs/components/tftraining/#quick-start) to install tf-operator. In `tf-operator` v0.5, `tf-operator` will create `PodGroup` and use it for gang-scheduling rather than PDB which will be deprecated.

1. Add following rules to `tf-job-operator` clusterrole to grant tf-job-operator permission to create `PodGroup`.

```yaml
- apiGroups:
  - scheduling.incubator.k8s.io
  resources:
  - podgroups
  verbs:
  - '*'

```

2. Enable gang-scheduling in `tf-operator` by add flag `--enable-gang-scheduling=true`
```
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: tf-job-operator
  ....
spec:
.....
    spec:
      containers:
      - command:
        - /opt/kubeflow/tf-operator.v1beta2
        - --alsologtostderr
        - -v=1
        - --enable-gang-scheduling=true
```

3. Try to submit following training jobs. If you have less than 2 gpus available in your cluster, none of workers will be scheduled.
In order to guide `tf-operator` to use `PodGroup`, you need to annotate `scheduling.k8s.io/group-name: ${PodGroupName}` to your job. In this case, `tf-operator` will create a `PodGroup` named `gang-scheduling-podgroup`.

```yaml
apiVersion: "kubeflow.org/v1beta1"
kind: "TFJob"
metadata:
  name: "gang-scheduling-podgroup"
spec:
  cleanPodPolicy: None
  tfReplicaSpecs:
    Worker:
      replicas: 2
      restartPolicy: Never
      template:
        metadata:
          annotations:
            scheduling.k8s.io/group-name: "gang-scheduling-podgroup"
        spec:
          schedulerName: kube-batch
          containers:
            - name: tensorflow
              image: gcr.io/kubeflow-examples/distributed_worker:v20181031-513e107c
              resources:
                limits:
                  nvidia.com/gpu: 1
```

### How does resources allocate to different queues?
Proportion policy calculates usable resources for all nodes and allocate them to each Queue by `Weight` and requests according to max-min weighted faireness algorithm. Here's an example.

```
SchedulerCache Snapshot information:
------------------    ------------------
| Node-1         |    | Node-2         |
|   cpu: 6       |    |   cpu: 3       |
|   memory: 15Gi |    |   memory: 12Gi |
------------------    ------------------
--------------------------    --------------------------
| Queue-1                |    | Queue-2                |
|   Weight: 2            |    |   Weight: 4            |
--------------------------    --------------------------

--------------------------    --------------------------       --------------------------
| PodGroup-1             |    | PodGroup-2             |       | PodGroup-3             |
|   cpu: 5               |    |   cpu: 4               |       |   cpu: 6               |
|   memory: 10           |    |   memory: 12           |       |   memory: 8            |
|   queue: Queue-1       |    |   queue: Queue-2       |       |   queue: QUeue-2       |
--------------------------    --------------------------       --------------------------

 After policy scheduling:
---------------------------    ---------------------------
| Queue-1                 |    | Queue-2                 |
|    Weight: 2            |    |    Weight: 4            |
|    Request: cpu=5       |    |    Request: cpu=10      |
|             memory=10Gi |    |             memory=20Gi |
|                         |    |                         |
|    Deserved:            |    |    Deserved:            |
|      cpu: 3             |    |      cpu: 6             |
|      memory: 9Gi        |    |      memory: 18Gi       |
---------------------------    ---------------------------
```
