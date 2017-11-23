# Tutorial of Kube-arbitrator

This doc will show the tutorial of Kube-arbitrator.

## 1. Pre-condition
To run Kube-arbitrator, a kubernetes cluster must start up. Here is a document [Using kubeadm to Create a Cluster](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/)

## 2. Create Queue CRD
`Queue` is a kubernetes CRD [(custom resources definition)](https://kubernetes.io/docs/tasks/access-kubernetes-api/extend-api-custom-resource-definitions/). Following is the yaml file of Queue CRD and it must be created before Kube-arbitrator startup.

```yaml
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: queues.arbitrator.incubator.k8s.io
spec:
  group: arbitrator.incubator.k8s.io
  names:
    kind: Queue
    listKind: QueueList
    plural: queues
    singular: queue
  scope: Namespaced
  version: v1
```

Verify queue crd after creation 

```
# kubectl get crd
NAME                                    KIND
queues.arbitrator.incubator.k8s.io      CustomResourceDefinition.v1beta1.apiextensions.k8s.io
```

## 3. Create Queue
After creating Queue CRD, need to create queues for resource allocation. A queue is like a tenant and includes resource request information. Kube-arbitrator will allocate cluster resources between different queues. Following is yaml template of `Queue`:

```
apiVersion: "arbitrator.incubator.k8s.io/v1"
kind: Queue
metadata:
  name: xxx
  namespace: xxx
spec:
  weight: xxx
  request:
    resources:
      cpu: "xxx"
      memory: xxxGi
```

* `name`: name of this queue.
* `namespace`: namespace of this queue, resources will be allocated to this namespace by Kube-arbitrator. The queue must belong to a namespace and a namespace has one queue at most. So it is namespace level resource allocation. Fine-grained scheduling will be support in future. Refer [Fine-grained scheduling](#future1) for more details. 
* `weight`: weight of this queue, it must be an integer. The high weight queue will get more resources.
* `request`: the total resource request of this queue, it contains CPU and Memory. CPU must be integer now. The request will be enhanced to support DRF. Refer [Resource request](#future2) for more details.

For example, create two queues like following, `q01` in namespace `ns01`, `q02` in namespace `ns02`

```
# kubectl get queue --all-namespaces
NAMESPACE   NAME      KIND
ns01        q01       Queue.v1.arbitrator.incubator.k8s.io
ns02        q02       Queue.v1.arbitrator.incubator.k8s.io
```
```
# cat queue01.yaml
apiVersion: "arbitrator.incubator.k8s.io/v1"
kind: Queue
metadata:
  name: q01
  namespace: ns01
spec:
  weight: 6
  request:
    resources:
      cpu: "5"
      memory: 6Gi
```
```
# cat queue02.yaml
apiVersion: "arbitrator.incubator.k8s.io/v1"
kind: Queue
metadata:
  name: q02
  namespace: ns02
spec:
  weight: 1
  request:
    resources:
      cpu: "6"
      memory: 7Gi
```

Kube-arbitrator will allocate resources to the two queues `q01` and `q02` according to their weight and resource requirement after it starts up. Now it uses max-min weighted fairness algorithm.

## 4. Create Resource Quota
After creating queues, then need to create resource quota for each queue. The resource quota must be under the same namespace as the queue. There is one resource quota under a namespace at most.
To support [Fine-grained scheduling](#future1), Kube-arbitrator needs to some new quota of Queue/QueueJob for resource limitation. Refer [New quota for Queue/QueueJob](#future3) for more details.

For example, create two quotas like following:

```
# kubectl get quota --all-namespaces
NAMESPACE   NAME      AGE
ns01        rq01      1d
ns02        rq02      1d
```
```
# cat resource-quota01.yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: rq01
  namespace: ns01
spec:
  hard:
    pods: "100"
    limits.cpu: "0"
    limits.memory: "0"
    requests.cpu: "0"
    requests.memory: "0"
```
```
# cat resource-quota02.yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: rq02
  namespace: ns02
spec:
  hard:
    pods: "100"
    limits.cpu: "0"
    limits.memory: "0"
    requests.cpu: "0"
    requests.memory: "0"
```

We can see that resources limitation(CPU and memory) is 0 now. Kube-arbitrator will change resource limitation of related quota after allocating resource to each queue.

## 5. Start kube-arbitrator
Download kube-arbitrator to `$GOPATH/src/github.com/kubernetes-incubator/` from github `https://github.com/kubernetes-incubator/kube-arbitrator`

Build kube-arbitrator:

```
# cd $GOPATH/src/github.com/kubernetes-incubator/kube-arbitrator
# make
```

Start kube-arbitartor:

```
# ./_output/bin/kube-arbitrator --kubeconfig /root/.kube/config
```

`--kubeconfig` must be set to specify the kubernetes configuration file.

Now we can see resource limitation of quota is changed. As mentioned above, Kube-arbitrator allocates resources to each queue(namespace) according to the weight and resource request in a queue.

```
# kubectl get quota rq01 --namespace=ns01 -o yaml
... ...
spec:
  hard:
    limits.cpu: "5"
    limits.memory: "6442450944"
    pods: "100"
    requests.cpu: "5"
    requests.memory: "6442450944"
... ...
```
```
# kubectl get quota rq02 --namespace=ns02 -o yaml
... ...
spec:
  hard:
    limits.cpu: "6"
    limits.memory: "7516192768"
    pods: "100"
    requests.cpu: "6"
    requests.memory: "7516192768"
... ...
```

## 6. Current structure
![](../images/tutorial.jpg)

# Future work of kube-arbitrator
## <span id="future1">1. Fine-grained scheduling</span>
Kube-arbitrator only supports allocating resources to queue(namespace) level now. To do fine-grained scheduling, a queue can contain multiple queue jobs, a queue job can be a batch job, big data job(Spark, Hadoop, etc), or custom definition job. All the queue jobs in the same queue will share queue resources and Kube-arbitrator can allocate those resources to each queue job by some strategy. In the roadmap, Kube-arbitrator will support QueueJob level resource allocation, [Issue 71: Queue job level resources assignment](https://github.com/kubernetes-incubator/kube-arbitrator/issues/71) is logged to trace the discussion.

## <span id="future2">2. Resource request</span>
A queue/queuejob just include the total resource request now. Only CPU and Memory are supported. Other types of resources will be included, such as volume, etc.
In most cases, customer just want to define a `resource unit`, all tasks belongs to the same queue/queuejob request the same resource unit. Kube-arbitrator will support to define resource unit and total request number in queue/queuejob and allocate resource to queue/queuejob by DRF.

## <span id="future3">3. New quota for Queue/QueueJob</span>
Kube-arbitrator uses ResourceQuota to limit resource usage of each queue, it is namespaces level. In the roadmap, Kube-arbitrator needs to add new Quota and admission control for Queue and QueueJob to limit resource usage. It may be as follows:

* `QueueQuota`: resource usage limitation of a Queue
* `QueueJobQuota`: resource usage limitation of a QueueJob
* `QueueQuotaController`: new admission controller for QueueQuota
* `QueueJobQuotaController`: new admission controller for QueueJobQuota

[Issue 91](https://github.com/kubernetes-incubator/kube-arbitrator/issues/91) is logged to trace the discussion.