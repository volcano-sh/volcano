# Tutorial of kube-batchd

This doc will show how to run kube-batchd as a kubernetes scheduler. It is for [master](https://github.com/kubernetes-incubator/kube-arbitrator/tree/master) branch.

## 1. Pre-condition
To run kube-batchd, a kubernetes cluster must start up. Here is a document on [Using kubeadm to Create a Cluster](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/). Additionally, for common purposes and testing and deploying on local machine, one can use Minikube. This is a document on [Running Kubernetes Locally via Minikube](https://kubernetes.io/docs/getting-started-guides/minikube/).

kube-batchd need to run as a kubernetes scheduler. The next step will show how to run kube-batchd as kubernetes scheduler quickly. Refer [Configure Multiple Schedulers](https://kubernetes.io/docs/tasks/administer-cluster/configure-multiple-schedulers/) to get more details.

## 2. Config Kube-batchd as Kubernetes scheduler

### (1) Package Kube-batchd

#### Download and Build kube-batchd

```
# mkdir -p $GOPATH/src/github.com/kubernetes-incubator/
# cd $GOPATH/src/github.com/kubernetes-incubator/
# git clone git@github.com:kubernetes-incubator/kube-arbitrator.git
# cd kube-arbitrator
# make
```

#### Build docker image

```
# mkdir -p /tmp/kube-image
# cd /tmp/kube-image
# cp $GOPATH/src/github.com/kubernetes-incubator/kube-arbitrator/_output/bin/kube-batchd ./
# cp /root/.kube/config ./
```

Create `Dockerfile` in `/tmp/kube-image`

```
# cat /tmp/kube-image/Dockerfile
From ubuntu

ADD kube-batchd /opt
ADD config /opt

# cd /tmp/kube-image/
# docker build -t kube-batchd:v1 .
```

Verify kube-batchd images

```
# docker images kube-batchd
REPOSITORY        TAG       IMAGE ID          CREATED              SIZE
kube-batchd       v1        bc4cec05da6a      About a minute ago   149.3 MB
```

### (2) Define a Kubernetes Deployment for kube-batchd

Create a `kube-batchd.yaml` with the following content:

```yaml
apiVersion: apps/v1beta1
kind: Deployment
metadata:
  labels:
    component: scheduler
    tier: control-plane
  name: kube-batchd
  namespace: kube-system
spec:
  replicas: 1
  template:
    metadata:
      labels:
        component: scheduler
        tier: control-plane
        version: second
    spec:
      containers:
      - command:
        - /opt/kube-batchd
        - --kubeconfig=/opt/config
        image: kube-batchd:v1
        name: kube-second-scheduler
        resources:
          requests:
            cpu: '0.1'
        securityContext:
          privileged: false
        volumeMounts: []
      hostNetwork: false
      hostPID: false
      volumes: []
```

Run the kube-batchd as kubernetes scheduler

```
# kubectl create -f kube-batchd.yaml
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

### (3) Specify kube-batchd scheduler for deployment

Create a file named `deployment-drf01.yaml` with the following content:

```yaml
apiVersion: apps/v1beta1
kind: Deployment
metadata:
  name: drf-01
  namespace: ns01
spec:
  replicas: 4
  template:
    metadata:
      labels:
        app: redis
    spec:
      schedulerName: kube-batchd
      containers:
      - name: key-value-store
        image: redis
        resources:
          limits:
            memory: "3Gi"
            cpu: "3"
          requests:
            memory: "3Gi"
            cpu: "3"
        ports:
        - containerPort: 80
```

Run the deployment

```
# kubectl create -f deployment-drf01.yaml
```

Verify that the deployment named `drf-01` is present in the output of
```
# kubectl get deployments
```

## 3. Create PodDisruptionBudget for Application

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
