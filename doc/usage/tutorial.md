# Tutorial of kube-arbitrator

This doc will show how to run `kube-arbitrator` as a kubernetes batch system. It is for [master](https://github.com/kubernetes-incubator/kube-arbitrator/tree/master) branch.

## 1. Pre-condition
To run `kube-arbitrator`, a Kubernetes cluster must start up. Here is a document on [Using kubeadm to Create a Cluster](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/). Additionally, for common purposes and testing and deploying on local machine, one can use Minikube. This is a document on [Running Kubernetes Locally via Minikube](https://kubernetes.io/docs/getting-started-guides/minikube/).

`kube-arbitrator/kar-scheduler` need to run as a kubernetes scheduler. The next step will show how to run `kar-scheduler` as kubernetes scheduler quickly. Refer [Configure Multiple Schedulers](https://kubernetes.io/docs/tasks/administer-cluster/configure-multiple-schedulers/) to get more details.

## 2. Config kube-arbitrator for Kubernetes

### (1) kube-arbitrator image

An official kube-arbitrator image is provided and you can download it from [DockerHub](https://hub.docker.com/r/kubearbitrator/kar-scheduler/). The version is `0.1` now.

### (2) Create a Kubernetes Deployment for kube-arbitrator

#### Download kube-arbitrator

```
# mkdir -p $GOPATH/src/github.com/kubernetes-incubator/
# cd $GOPATH/src/github.com/kubernetes-incubator/
# git clone git@github.com:kubernetes-incubator/kube-arbitrator.git
```

#### Deploys `kube-arbitrator` by Helm

Run the `kar-scheduler` as kubernetes scheduler

```
# helm install $GOPATH/src/github.com/kubernetes-incubator/kube-arbitrator/deployment/kube-arbitrator --namespace kube-system
```

Verify the release

```
# helm list
NAME        	REVISION	UPDATED                 	STATUS  	CHART                	NAMESPACE
dozing-otter	1       	Thu Jun 14 18:52:15 2018	DEPLOYED	kube-arbitrator-0.1.0	kube-system
```

NOTE: `kube-arbitrator` need to collect cluster information(such as Pod, Node, CRD, etc) for scheduing, so the service account used by the deployment must have permission to access those cluster resources, otherwise, `kube-arbitrator` will fail to startup.

### (3) Create a QueueJob

Create a file named `queuejob-01.yaml` with the following content:

```yaml
apiVersion: arbitrator.incubator.k8s.io/v1alpha1
kind: QueueJob
metadata:
  namespace: default
  name: qj-01
spec:
  schedulingSpec:
    minAvailable: 1
  taskSpecs:
  - replicas: 3
    selector:
      matchLabels:
        queuejob.arbitrator.k8s.io: test
    template:
      metadata:
        labels:
          queuejob.arbitrator.k8s.io: test
        name: test
      spec:
        containers:
        - image: busybox
          imagePullPolicy: IfNotPresent
          name: test
          resources:
            requests:
              cpu: "1"
              memory: 100Mi
        schedulerName: kar-scheduler
```

The yaml file means a QueueJob named `qj-01` contains 3 pods(it is specified by `replicas`), these pods will be scheduled by scheudler `kar-scheduler` (it is specified by `schedulerName`). `kar-scheduler` will start `.schedSpec.minAvailable` pods for a QueueJob at the same time, otherwise, such as resources are not sufficient, `kar-scheduler` will not start any pods for the QueueJob.

Create the QueueJob

```
# kubectl create -f queuejob-01.yaml
```

Check job status by `karcli` (the command line of `kube-arbitrator`)

```
# karcli job list
Name                          Creation                 Replicas    Min     Pending     Running     Succeeded   Failed
qj-01                         2018-06-14 18:57:25      3           2       3           0           0           0
```

Check the pods status

`# kubectl get pod --all-namespaces`



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
