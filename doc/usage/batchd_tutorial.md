# Tutorial of kube-batchd

This doc will show how to run kube-batchd as a kubernetes scheduler. It is for [master](https://github.com/kubernetes-incubator/kube-arbitrator/tree/master) branch.

## 1. Pre-condition
To run kube-batchd, a kubernetes cluster must start up. Here is a document on [Using kubeadm to Create a Cluster](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/). Additionally, for common purposes and testing and deploying on local machine, one can use Minikube. This is a document on [Running Kubernetes Locally via Minikube](https://kubernetes.io/docs/getting-started-guides/minikube/).

kube-batchd need to run as a kubernetes scheduler. The next step will show how to run kube-batchd as kubernetes scheduler quickly. Refer [Configure Multiple Schedulers](https://kubernetes.io/docs/tasks/administer-cluster/configure-multiple-schedulers/) to get more details.

## 2. Config Kube-batchd as Kubernetes scheduler

### (1) Kube-batchd image

An official kube-batchd images is provided and you can download it from [DockerHub](https://hub.docker.com/r/kubearbitrator/batchd/). The version is `v0.1` now.

### (2) Create a Kubernetes Deployment for kube-batchd

#### Download kube-batchd

```
# mkdir -p $GOPATH/src/github.com/kubernetes-incubator/
# cd $GOPATH/src/github.com/kubernetes-incubator/
# git clone git@github.com:kubernetes-incubator/kube-arbitrator.git
```

#### Create a deployment for kube-batchd

Run the kube-batchd as kubernetes scheduler

```
# kubectl create -f $GOPATH/src/github.com/kubernetes-incubator/kube-arbitrator/deployment/kube-batchd.yaml
```

Verify kube-batchd deployment

```
# kubectl get deploy kube-batchd -n kube-system
NAME           DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
kube-batchd    1         1         1            1           1m

# kubectl get pods --all-namespaces
NAMESPACE     NAME                             READY     STATUS       RESTARTS   AGE
... ...
kube-system   kube-batchd-2521827519-khmgx     1/1       Running      0          2m
... ...
```

NOTE: kube-batchd need to collect cluster information(such as Pod, Node, CRD, etc) for scheduing, so the service account used by the deployment must have permission to access those cluster resources, otherwise, kube-batchd will fail to startup.

### (3) Create a QueueJob

Create a file named `queuejob-01.yaml` with the following content:

```yaml
apiVersion: "arbitrator.incubator.k8s.io/v1"
kind: QueueJob
metadata:
  name: qj-01
spec:
  replicas: 3
  minavailable: 3
  template:
    spec:
      schedulerName: kube-batchd
      containers:
      - name: key-value-store
        image: redis
        resources:
          limits:
            memory: "3Gi"
            cpu: "7"
          requests:
            memory: "3Gi"
            cpu: "7"
        ports:
        - containerPort: 80
```

The yaml file means a QueueJob named `qj-01` contains 3 pods(it is specified by `replicas`), these pods will be scheduled by scheudler `kube-batchd`(it is specified by `schedulerName`). `kube-batchd` will start `replicas` pods for a QueueJob at the same time, otherwise, such as resources are not sufficient, `kube-batchd` will not start any pods for the QueueJob.

Create the QueueJob

```
# kubectl create -f queuejob-01.yaml
```

Verify that the QueueJob named `qj-01` is present in the output of

```
# kubectl get queuejob
```

Check the pods status

`# kubectl get pod --all-namespaces`

NOTE: `minavailable` is for a future work, not used yet. We can ignore it now.

## 3. Create PodDisruptionBudget for Application

NOTE: This step is not necessary when using QueueJob because `kube-batchd` will create PDB for a QueueJob automatically. It is needed when you only want to use gang-scheduling of kube-batchd, not QueueJob.

kube-batchd will reuse the number of `PodDisruptionBudget.spec.minAvailable` and change nothing of `PodDisruptionBudget` feature. Here is a sample:

Create a file named `pdb.yaml` with the following content:

```yaml
apiVersion: policy/v1beta1
kind: PodDisruptionBudget
metadata:
  name: pdb-01
spec:
  minAvailable: 4
  selector:
    matchLabels:
      app: redis
```

Create the PodDisruptionBudget

```
# kubectl create -f pdb.yaml
```

For kube-batchd, above yaml file means

* It is for application with label `app: redis`, assuming all pods under the same application (such as Deployment/RS/Job) have same `app` label
* Each application with label `app:redis` needs `minAvailable` (which is 4 in the above case) pods, at the least, to start.
* When resources are sufficient, kube-batchd will start `minAvailable` pods of each application one by one, and then start the remaining pods of each application by DRF policy.
* When resources are not sufficient(`minAvailable` can not be met), kube-batchd will not start any pod for the PodSet.

If there is no PodDisruptionBudget, `minAvailable` of each application will be 0.


## 4. Create PriorityClass for Pod

kube-batchd will start pods by their priority in the same PodSet, pods with higher priority will start first. Here is sample to show `PriorityClass` usage:

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
* The pod in same Deployment/RS/Job share the same pod template, so they have the same `PriorityClass`. To specify a different `PriorityClass` for pods in same PodSet, users need to create controllers by themselves.
