# Tutorial of kube-batchd

This doc will show how to run Kube-batchd as a kubernetes scheduler. It is for [master](https://github.com/kubernetes-incubator/kube-arbitrator/tree/master) branch.

## 1. Pre-condition
To run Kube-batchd, a kubernetes cluster must start up. Here is a document [Using kubeadm to Create a Cluster](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/)

Kube-batchd need to run as a kubernetes scheduler. The next step will show how to run kube-batchd as kubernetes scheduler quickly. Refer [Configure Multiple Schedulers](https://kubernetes.io/docs/tasks/administer-cluster/configure-multiple-schedulers/) to get more details.

## 2. Config Kube-batchd as Kubernetes scheduler

### (1) Package Kube-batchd

#### Download and Build Kube-batchd

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
# cd /tmp/kube-images
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

```
# cat kube-batchd.yaml 
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

```
# cat deployment-drf01.yaml 
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

## 3. Create PodDisruptionBudget for Application

Kube-batchd will reuse the number of `PodDisruptionBudget.spec.minAvailable` and change nothing of `PodDisruptionBudget` feature. Here is a sample:

```
# cat pdb.yaml 
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

For kube-batchd, above yaml file means

* It is for the applications with label `app: redis`. Assume all pods under the same application (such as Deployment/RS/Job) have same `app` label
* Each application with label `app:redis` need start `minAvailable` pods at least, `minAvailable` is 4.
* When resources are sufficient, Kube-batchd will start `minAvailable` pods of each application one by one, and then start left pods of each application by DRF policy.
* When resources are not sufficient, Kube-batchd just tries to start `minAvailable` pods of each application as much as possible.

If there is no PodDisruptionBudget, `minAvailable` of each application will be 0.