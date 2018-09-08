# Tutorial of kube-arbitrator

This doc will show how to run `kube-arbitrator` as a kubernetes batch scheduler. It is for [master](https://github.com/kubernetes-incubator/kube-arbitrator/tree/master) branch.

## 1. Pre-condition
To run `kube-arbitrator`, a Kubernetes cluster must start up. Here is a document on [Using kubeadm to Create a Cluster](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/). Additionally, for common purposes and testing and deploying on local machine, one can use Minikube. This is a document on [Running Kubernetes Locally via Minikube](https://kubernetes.io/docs/getting-started-guides/minikube/).

`kube-arbitrator` need to run as a kubernetes scheduler. The next step will show how to run `kube-arbitrator` as kubernetes scheduler quickly. Refer [Configure Multiple Schedulers](https://kubernetes.io/docs/tasks/administer-cluster/configure-multiple-schedulers/) to get more details.

## 2. Config kube-arbitrator for Kubernetes

### (1) kube-arbitrator image

An official kube-arbitrator image is provided and you can download it from [DockerHub](https://hub.docker.com/r/kubesigs/kube-batchd/). The version is `v0.2` now.

### (2) Create a Kubernetes Deployment for kube-arbitrator

#### Download kube-arbitrator

```bash
# mkdir -p $GOPATH/src/github.com/kubernetes-incubator/
# cd $GOPATH/src/github.com/kubernetes-incubator/
# git clone git@github.com:kubernetes-incubator/kube-arbitrator.git
```

#### Deploys `kube-arbitrator` by Helm

Run the `kube-arbitrator` as kubernetes scheduler

```bash
# helm install $GOPATH/src/github.com/kubernetes-incubator/kube-arbitrator/deployment/kube-arbitrator --namespace kube-system
```

Verify the release

```bash
# helm list
NAME        	REVISION	UPDATED                 	STATUS  	CHART                	NAMESPACE
dozing-otter	1       	Thu Jun 14 18:52:15 2018	DEPLOYED	kube-arbitrator-0.2.0	kube-system
```

NOTE: `kube-arbitrator` need to collect cluster information(such as Pod, Node, CRD, etc) for scheduing, so the service account used by the deployment must have permission to access those cluster resources, otherwise, `kube-arbitrator` will fail to startup.

### (3) Create a Job

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
      schedulerName: kube-batchd
---
apiVersion: scheduling.incubator.k8s.io/v1alpha1
kind: PodGroup
metadata:
  name: qj-1
spec:
  numMember: 6
```

The yaml file means a Job named `qj-01` to create 6 pods(it is specified by `parallelism`), these pods will be scheduled by scheudler `kube-batchd` (it is specified by `schedulerName`). `kube-batchd` will watch `PodGroup`, and the annotation `scheduling.k8s.io/group-name` identify which group the pod belongs to. `kube-batchd` will start `.spec.numMember` pods for a Job at the same time; otherwise, such as resources are not sufficient, `kube-batchd` will not start any pods for the Job.

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


## 4. Create PriorityClass for Pod

`kar-scheduler` will start pods by their priority in the same QueueJob, pods with higher priority will start first. Here is sample to show `PriorityClass` usage:

Create a `priority_1000.yaml` with the following contents:

```yaml
apiVersion: scheduling.k8s.io/v1alpha1
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
