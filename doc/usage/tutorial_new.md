# Tutorial of kube-arbitrator

This doc will show how to run Kube-arbitrator as a kubernetes scheduler. It is for [master](https://github.com/kubernetes-incubator/kube-arbitrator/tree/master) branch.

## 1. Pre-condition
To run Kube-arbitrator, a kubernetes cluster must start up. Here is a document [Using kubeadm to Create a Cluster](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/)

Kube-arbitrator need to run as a kubernetes scheduler. The next step will show how to run kube-arbitrator as kubernetes scheduler quickly. Refer [Configure Multiple Schedulers](https://kubernetes.io/docs/tasks/administer-cluster/configure-multiple-schedulers/) to get more details.

## 2. Config Kube-arbitrator as Kubernetes scheduler

### (1) Package Kube-arbitrator

#### Download and Build Kube-arbitrator

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
# cp $GOPATH/src/github.com/kubernetes-incubator/kube-arbitrator/_output/bin/kube-arbitrator ./
# cp /root/.kube/config ./
```

Create `Dockerfile` in `/tmp/kube-image`

```
# cat /tmp/kube-image/Dockerfile 
From ubuntu

ADD kube-arbitrator /opt
ADD config /opt

# cd /tmp/kube-image/
# docker build -t kube-arbitrator:v1 .
```

Verify kube-arbitrator images

```
# docker images kube-arbitrator
REPOSITORY          TAG                 IMAGE ID            CREATED              SIZE
kube-arbitrator     v1                  bc4cec05da6a        About a minute ago   149.3 MB
```

### (2) Define a Kubernetes Deployment for kube-arbitartor

```
# cat kube-arbitrator.yaml 
apiVersion: apps/v1beta1
kind: Deployment
metadata:
  labels:
    component: scheduler
    tier: control-plane
  name: kube-arbitrator
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
        - /opt/kube-arbitrator
        - --kubeconfig=/opt/config
        image: kube-arbitrator:v1 
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

Run the kube-arbitrator as kubernetes scheduler

```
# kubectl create -f kube-arbitrator.yaml 
```

Verify kube-arbitrator deployment

```
# kubectl get deploy kube-arbitrator -n kube-system
NAME              DESIRED   CURRENT   UP-TO-DATE   AVAILABLE   AGE
kube-arbitrator   1         1         1            1           1m

# kubectl get pods --all-namespaces
NAMESPACE     NAME                                 READY     STATUS       RESTARTS   AGE
... ...
kube-system   kube-arbitrator-2521827519-khmgx     1/1       Running      0          2m
... ...
```

### (3) Specify kube-arbitrator scheduler for deployment

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
      schedulerName: kube-arbitrator
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
