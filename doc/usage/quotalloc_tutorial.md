# Tutorial of Kube-quotalloc

This doc will show the tutorial of Kube-quotalloc.

## 1. Pre-condition
To run `kube-quotalloc`, a kubernetes cluster must start up. Here is a document on [Using kubeadm to Create a Cluster](https://kubernetes.io/docs/setup/independent/create-cluster-kubeadm/). Additionally, for common purposes and testing and deploying on local machine, one can use Minikube. This is a document on [Running Kubernetes Locally via Minikube](https://kubernetes.io/docs/getting-started-guides/minikube/).

## 2. Create QuotaAllocator CRD
`QuotaAllocator` is a kubernetes CRD [(custom resources definition)](https://kubernetes.io/docs/tasks/access-kubernetes-api/extend-api-custom-resource-definitions/). Following is the yaml file of QuotaAllocator CRD and it must be created before `kube-quotalloc` startup.

```yaml
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: quotaallocators.arbitrator.incubator.k8s.io
spec:
  group: arbitrator.incubator.k8s.io
  names:
    kind: QuotaAllocator
    listKind: QuotaAllocatorList
    plural: quotaallocators
    singular: quotaallocator
  scope: Namespaced
  version: v1
```

In order to create the CRD, run:

```
# kubectl create -f <filename>
```

Verify quotaallocator CRD after creation

```
# kubectl get crd
NAME                                          AGE
quotaallocators.arbitrator.incubator.k8s.io   7m
```

## 3. Create QuotaAllocator
After creating `QuotaAllocator` CRD, need to create quotaallocators for resource allocation. A quotaallocator is like a tenant and includes resource request information. `kube-quotalloc` will allocate cluster resources between different quotaallocators. Following is a yaml template of `QuotaAllocator`:

```yaml
apiVersion: "arbitrator.incubator.k8s.io/v1"
kind: QuotaAllocator
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

* `name`: name of this quotaallocator.
* `namespace`: namespace of this quotaallocator, resources will be allocated to this namespace by `kube-quotalloc`. The quotaallocator must belong to a namespace and a namespace has one quotaallocator at most. So it is a namespace level resource allocation. Fine-grained scheduling will be supported in future. Refer [Fine-grained scheduling](#future1) for more details.
* `weight`: weight of this quotaallocator, it must be an integer. The higher the weight, the more resources quotaallocator will get.
* `request`: the total resource request of this quotaallocator. It contains CPU and Memory keys. CPU must be an integer for the time being. **This field is not supported by `kube-quotalloc` yet**. It will be enhanced to support DRF. Refer [Resource request](#future2) for more details.

For example, to create two quotaallocators like following, `q01` in namespace `allocator-ns01`, `q02` in namespace `allocator-ns02`:

Create `allocator01.yaml` with the following content and execute `kubectl create -f allocator01.yaml`

```yaml
apiVersion: "arbitrator.incubator.k8s.io/v1"
kind: QuotaAllocator
metadata:
  name: q01
  namespace: allocator-ns01
spec:
  weight: 3
  request:
    resources:
      cpu: "5"
      memory: 6Gi
```

Create `allocator02.yaml` with the following content and execute `kubectl create -f allocator02.yaml`

```yaml
apiVersion: "arbitrator.incubator.k8s.io/v1"
kind: QuotaAllocator
metadata:
  name: q02
  namespace: allocator-ns02
spec:
  weight: 5
  request:
    resources:
      cpu: "6"
      memory: 7Gi
```

Verify that the quotaallocators were created:
```
# kubectl get quotaallocators --all-namespaces
NAMESPACE        NAME      AGE
allocator-ns01   q01       15m
allocator-ns02   q02       15m
```

`kube-quotalloc` will allocate resources to the two quotaallocators `q01` and `q02` according to their weight.

## 4. Create Resource Quota
After creating quotaallocators, one needs to create resource quota for each quotaallocator. The resource quota must be under the same namespace as the quotaallocator. There is one resource quota under a namespace at most.
To support [Fine-grained scheduling](#future1), `kube-quotalloc` needs to use some new quota of QuotaAllocator/Job for resource limitation. Refer [New quota for QuotaAllocator/Job](#future3) for more details.

For example, create two quotas like following:

Create `resource-quota01.yaml` with the following content and run `kubectl create -f resource-quota01.yaml`

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: rq01
  namespace: allocator-ns01
spec:
  hard:
    pods: "100"
    limits.cpu: "0"
    limits.memory: "0"
    requests.cpu: "0"
    requests.memory: "0"
```

Create `resource-quota02.yaml` with the following content and run `kubectl create -f resource-quota02.yaml`

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: rq02
  namespace: allocator-ns02
spec:
  hard:
    pods: "100"
    limits.cpu: "0"
    limits.memory: "0"
    requests.cpu: "0"
    requests.memory: "0"
```

Verify the quotaallocators are created:
```
# kubectl get quota --all-namespaces
NAMESPACE        NAME     AGE
allocator-ns01   rq01     26m
allocator-ns02   rq02     26m
```

We can see that resources limitation (CPU and memory) is 0 now. `kube-quotalloc` will change resource limitations of related quota after allocating resources to each quotaallocator.

## 5. Start kube-quotalloc
Download kube-arbitrator to `$GOPATH/src/github.com/kubernetes-incubator/` from github `https://github.com/kubernetes-incubator/kube-arbitrator`

Build kube-quotalloc:

```
# cd $GOPATH/src/github.com/kubernetes-incubator/kube-arbitrator
# make
```

Start kube-quotalloc:

```
# ./_output/bin/kube-quotalloc --kubeconfig /root/.kube/config
```

`--kubeconfig` must be set to specify the kubernetes configuration file.

Now we can see resource limitation of quota is changed. As mentioned above, `kube-quotalloc` allocates resources to each quotaallocator(namespace) according to the weight in a quotaallocator.

```
# kubectl get quota rq01 -n allocator-ns01 -o yaml
... ...
spec:
  hard:
    limits.cpu: "6"
    limits.memory: "18919466496"
    pods: "100"
    requests.cpu: "6"
    requests.memory: "18919466496"
... ...
```

```
# kubectl get quota rq02 -n allocator-ns02 -o yaml
... ...
spec:
  hard:
    limits.cpu: "10"
    limits.memory: "31532444160"
    pods: "100"
    requests.cpu: "10"
    requests.memory: "31532444160"
... ...
```

## 6. Current structure
![](../images/tutorial.jpg)

# Future work of kube-quotalloc
## <span id="future1">1. Fine-grained scheduling</span>
`kube-quotalloc` only supports allocating resources to quotaallocator(namespace) level now. To do fine-grained scheduling, a quotaallocator can contain multiple jobs, **a job can be a batch job, big data job(Spark, Hadoop, etc), or custom definition job**. All the jobs in the same quotaallocator will share quotaallocator resources and `kube-quotalloc` can allocate those resources to each job by some strategy. In the roadmap, `kube-quotalloc` will support job level resource allocation, [Issue 71: Queue job level resources assignment](https://github.com/kubernetes-incubator/kube-arbitrator/issues/71) is logged to trace the discussion.

## <span id="future2">2. Resource request</span>
A quotaallocator/job just includes the present total resource request. Only CPU and Memory resources are supported. Other types of resources, such as volume, will be included.
In most cases, the customer just wants to define a `resource unit`, and all tasks belonging to the same quotaallocator/job request the same resource unit. `kube-quotalloc` will support to define resource unit and total request number in quotaallocator/job and allocate resources to quotaallocator/job by DRF.

## <span id="future3">3. New quota for QuotaAllocator/Job</span>
`kube-quotalloc` uses ResourceQuota to limit resources usage of each quotaallocator. ResourcesQuota works at namespaces level. In the roadmap, `kube-quotalloc` needs to add new Quota and admission control for QuotaAllocator and Job to limit resource usage. It may be as follows:

* `AllocatorQuota`: resource usage limitation of a QuotaAllocator
* `JobQuota`: resource usage limitation of a Job
* `AllocatorQuotaController`: new admission controller for AllocatorQuota
* `JobQuotaController`: new admission controller for JobQuota

[Issue 91](https://github.com/kubernetes-incubator/kube-arbitrator/issues/91) is logged to trace the discussion.
