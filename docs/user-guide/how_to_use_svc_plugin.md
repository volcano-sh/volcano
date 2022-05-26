# Volcano Job Plugin -- SVC User Guidance

## Background
**SVC Plugin** is designed for the communication for pods within a volcano job, which is essential for workloads such as
[TensorFlow](https://tensorflow.google.cn/) and [MPI](https://www.open-mpi.org/). For example, it is necessary for
`tensorflow job` to contact with each other between `ps` and `worker`. Volcano job plugin `svc` enable pods within a job
to visit each other by domain.

## Key Points
* Once `svc` plugin is configured, value of field `hostname` under `spec` will be filled out to be **the pod name** for
all pods under the job automatically. Namely, `pod.spec.hostname` is `pod.metadata.name`.
* Once `svc` plugin is configured, value of field `subdomain` under `spec` will be filled out to be **the job name** for
all pods under the job automatically. Namely, `pod.spec.subdomain` is `job.metadata.name`.
* Once `svc` plugin is configured, environment variables `VC_%s_NUM` will be registered to all the containers(including
initContainers) under the job automatically. `%s` will be replaced by the **task name** which the pod belongs to. The value
of the environment variable is the **task replicas**. The number of the environment variables depends on the number of tasks,
which is usually `2` for most AI and Big Data jobs contains 2 roles. For example, a Spark job contains `driver` and `executor`.
* Once `svc` plugin is configured, environment variables `VC_%s_HOSTS` will be registered to all the containers(including 
initContainers) under the job automatically. `%s` will be replaced by the **task name** which the pod belongs to. The value
of the environment variable are the domains of all the pods under the task. The number of the environment variables depends
on the number of tasks, which is usually `2` for most AI and Big Data jobs contains 2 roles. For example, a TensorFlow job
contains `ps` and `worker`.
* A configmap whose name joins job-name and `svc` with `-` will be created automatically, which contains replicas of all
tasks and domains of all pods under the task. It will be mounted as a volume for all pods under the job and serves as the
host files under the directory `/etc/volcano/`.
* A headless service whose name is the same with job will be created.
* If `disable-network-policy` is set to be false, a `NetworkPolicy` object with the type `Ingress` will be created for
the job.

## Arguments
| ID  | Name                          | Value           | Default Value | Required | Description                                          | Example                                       |
|-----|-------------------------------|-----------------|---------------|----------|------------------------------------------------------|-----------------------------------------------|
| 1   | `publish-not-ready-addresses` | `true`/`false`  | `false`       | N        | whether publish the pod address when it is not ready | svc: ["--publish-not-ready-addresses=true"]   |
| 2   | `disable-network-policy`      | `true`/`false`  | `false`       | N        | whether disable network policy for the job           | svc: ["--disable-network-policy=true"]        |

## Examples
```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: tensorflow-dist-mnist
spec:
  minAvailable: 3
  schedulerName: volcano
  plugins:
    env: []
    svc: ["--publish-not-ready-addresses=false", "--disable-network-policy=false"]  ## SVC plugin register
  policies:
    - event: PodEvicted
      action: RestartJob
  queue: default
  tasks:
    - replicas: 1
      name: ps
      template:
        spec:
          containers:
            - command:
                - sh
                - -c
                - |
                  PS_HOST=`cat /etc/volcano/ps.host | sed 's/$/&:2222/g' | sed 's/^/"/;s/$/"/' | tr "\n" ","`;    ## Get host domain from host files generated
                  WORKER_HOST=`cat /etc/volcano/worker.host | sed 's/$/&:2222/g' | sed 's/^/"/;s/$/"/' | tr "\n" ","`;
                  export TF_CONFIG={\"cluster\":{\"ps\":[${PS_HOST}],\"worker\":[${WORKER_HOST}]},\"task\":{\"type\":\"ps\",\"index\":${VK_TASK_INDEX}},\"environment\":\"cloud\"};
                  python /var/tf_dist_mnist/dist_mnist.py
              image: volcanosh/dist-mnist-tf-example:0.0.1
              name: tensorflow
              ports:
                - containerPort: 2222
                  name: tfjob-port
              resources: {}
          restartPolicy: Never
    - replicas: 2
      name: worker
      policies:
        - event: TaskCompleted
          action: CompleteJob
      template:
        spec:
          containers:
            - command:
                - sh
                - -c
                - |
                  PS_HOST=`cat /etc/volcano/ps.host | sed 's/$/&:2222/g' | sed 's/^/"/;s/$/"/' | tr "\n" ","`;
                  WORKER_HOST=`cat /etc/volcano/worker.host | sed 's/$/&:2222/g' | sed 's/^/"/;s/$/"/' | tr "\n" ","`;
                  export TF_CONFIG={\"cluster\":{\"ps\":[${PS_HOST}],\"worker\":[${WORKER_HOST}]},\"task\":{\"type\":\"worker\",\"index\":${VK_TASK_INDEX}},\"environment\":\"cloud\"};
                  python /var/tf_dist_mnist/dist_mnist.py
              image: volcanosh/dist-mnist-tf-example:0.0.1
              name: tensorflow
              ports:
                - containerPort: 2222
                  name: tfjob-port
              resources: {}
          restartPolicy: Never
```
Note:
* Fields `hostname` and `subdomain` have been added to the pods under job `tensorflow-dist-mnist`. The following is part
yaml of the `ps` pod. 
```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    scheduling.k8s.io/group-name: tensorflow-dist-mnist
    volcano.sh/job-name: tensorflow-dist-mnist
    volcano.sh/job-version: "0"
    volcano.sh/queue-name: default
    volcano.sh/task-spec: ps
    volcano.sh/template-uid: tensorflow-dist-mnist-ps
  labels:
    volcano.sh/job-name: tensorflow-dist-mnist
    volcano.sh/job-namespace: default
    volcano.sh/queue-name: default
    volcano.sh/task-spec: ps
  name: tensorflow-dist-mnist-ps-0
  namespace: default
  ownerReferences:
  - apiVersion: batch.volcano.sh/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: Job
    name: tensorflow-dist-mnist
    uid: 52c98cc2-4791-490f-8572-22df2c16ef8f
  resourceVersion: "855403"
  uid: 1b9e834b-de7e-4760-9b23-2a673d38e5d9
spec:
  containers:
  - command:
      - sh
        - -c
        - |
        PS_HOST=`cat /etc/volcano/ps.host | sed 's/$/&:2222/g' | sed 's/^/"/;s/$/"/' | tr "\n" ","`;    ## Get host domain from host files generated
        WORKER_HOST=`cat /etc/volcano/worker.host | sed 's/$/&:2222/g' | sed 's/^/"/;s/$/"/' | tr "\n" ","`;
        export TF_CONFIG={\"cluster\":{\"ps\":[${PS_HOST}],\"worker\":[${WORKER_HOST}]},\"task\":{\"type\":\"ps\",\"index\":${VK_TASK_INDEX}},\"environment\":\"cloud\"};
        python /var/tf_dist_mnist/dist_mnist.py
    env:
    - name: VK_TASK_INDEX
      value: "0"
    - name: VC_TASK_INDEX
      value: "0"
    - name: VC_PS_HOSTS     ## Environment variable `VC_PS_HOSTS` contains the domains of all the `ps` hosts. 
      valueFrom:
        configMapKeyRef:
          key: VC_PS_HOSTS
          name: tensorflow-dist-mnist-svc
    - name: VC_PS_NUM       ## Environment variable `VC_PS_NUM` contains the number of `ps` hosts.
      valueFrom:
        configMapKeyRef:
          key: VC_PS_NUM
          name: tensorflow-dist-mnist-svc
    - name: VC_WORKER_HOSTS   ## Environment variable `VC_WORKER_HOSTS` contains the domains of all the `worker` hosts.
      valueFrom:
        configMapKeyRef:
          key: VC_WORKER_HOSTS
          name: tensorflow-dist-mnist-svc
    - name: VC_WORKER_NUM     ## Environment variable `VC_WORKER_NUM` contains the number of `worker` hosts.
      valueFrom:
        configMapKeyRef:
          key: VC_WORKER_NUM
          name: tensorflow-dist-mnist-svc
    image: volcanosh/dist-mnist-tf-example:0.0.1
    name: tensorflow
    ports:
    - containerPort: 2222
      name: tfjob-port
      protocol: TCP
    resources: {}
    volumeMounts:   ## Mount the configmap generated for the job under `/etc/volcano`, which contains all host files. 
    - mountPath: /etc/volcano
      name: tensorflow-dist-mnist-svc
    - mountPath: /var/run/secrets/kubernetes.io/serviceaccount
      name: kube-api-access-wflz5
      readOnly: true
  dnsPolicy: ClusterFirst
  enableServiceLinks: true
  hostname: tensorflow-dist-mnist-ps-0    ## Add `hostname` filed
  nodeName: volcano-control-plane
  restartPolicy: Never
  schedulerName: volcano
  subdomain: tensorflow-dist-mnist        ## Add `subdomain` filed
  tolerations:
  - effect: NoExecute
    key: node.kubernetes.io/not-ready
    operator: Exists
    tolerationSeconds: 300
  - effect: NoExecute
    key: node.kubernetes.io/unreachable
    operator: Exists
    tolerationSeconds: 300
  volumes:
  - configMap:    ## Configmap generated for the job
      defaultMode: 420
      name: tensorflow-dist-mnist-svc
    name: tensorflow-dist-mnist-svc
status:
  conditions:
  - lastProbeTime: null
    lastTransitionTime: "2022-04-13T02:08:17Z"
    status: "True"
    type: Initialized
  - lastProbeTime: null
    lastTransitionTime: "2022-04-13T02:08:18Z"
    status: "True"
    type: Ready
  - lastProbeTime: null
    lastTransitionTime: "2022-04-13T02:08:18Z"
    status: "True"
    type: ContainersReady
  - lastProbeTime: null
    lastTransitionTime: "2022-04-13T02:08:17Z"
    status: "True"
    type: PodScheduled
  hostIP: x.x.x.x
  phase: Running
  podIP: x.x.x.x
  podIPs:
  - ip: x.x.x.x
  qosClass: BestEffort
  startTime: "2022-04-13T02:08:17Z"
```
* Host information is registered to all pods under the job. The following are registered environment variables for `ps` pod.
```
[root@tensorflow-dist-mnist-ps-0 /] env | grep VC
VC_PS_NUM=1
VC_PS_HOSTS=tensorflow-dist-mnist-ps-0.tensorflow-dist-mnist  ## ps pod domain
VC_WORKER_NUM=2
VC_WORKER_HOSTS=tensorflow-dist-mnist-worker-0.tensorflow-dist-mnist,tensorflow-dist-mnist-worker-1.tensorflow-dist-mnist  ## worker pods domains
```
* The host files added under `/etc/volcano` are as follows.
```
[root@tensorflow-dist-mnist-ps-0 /] ls /etc/volcano/
VC_PS_HOSTS  VC_PS_NUM  VC_WORKER_HOSTS  VC_WORKER_NUM  ps.host  worker.host
[root@tensorflow-dist-mnist-ps-0 /]# cat /etc/volcano/ps.host
tensorflow-dist-mnist-ps-0.tensorflow-dist-mnist
[root@tensorflow-dist-mnist-ps-0 /]# cat /etc/volcano/worker.host 
tensorflow-dist-mnist-worker-0.tensorflow-dist-mnist
tensorflow-dist-mnist-worker-1.tensorflow-dist-mnist
```
* The headless service `tensorflow-dist-mnist` generated for the job is as follows.
```yaml
apiVersion: v1
kind: Service
metadata:
  creationTimestamp: "2022-04-13T02:08:15Z"
  name: tensorflow-dist-mnist
  namespace: default
  ownerReferences:
  - apiVersion: batch.volcano.sh/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: Job
    name: tensorflow-dist-mnist
    uid: 52c98cc2-4791-490f-8572-22df2c16ef8f
  resourceVersion: "855341"
  uid: a77cb081-72ae-442f-96da-e36974dfed48
spec:
  clusterIP: None
  clusterIPs:
  - None
  ipFamilies:
  - IPv4
  ipFamilyPolicy: SingleStack
  selector:
    volcano.sh/job-name: tensorflow-dist-mnist
    volcano.sh/job-namespace: default
  sessionAffinity: None
  type: ClusterIP
status:
  loadBalancer: {}
```
* The configmap `tensorflow-dist-mnist-svc` generated for the job is as follows.
```yaml
apiVersion: v1
data:
  VC_PS_HOSTS: tensorflow-dist-mnist-ps-0.tensorflow-dist-mnist
  VC_PS_NUM: "1"
  VC_WORKER_HOSTS: tensorflow-dist-mnist-worker-0.tensorflow-dist-mnist,tensorflow-dist-mnist-worker-1.tensorflow-dist-mnist
  VC_WORKER_NUM: "2"
  ps.host: tensorflow-dist-mnist-ps-0.tensorflow-dist-mnist
  worker.host: |-
    tensorflow-dist-mnist-worker-0.tensorflow-dist-mnist
    tensorflow-dist-mnist-worker-1.tensorflow-dist-mnist
kind: ConfigMap
metadata:
  creationTimestamp: "2022-04-13T02:08:15Z"
  name: tensorflow-dist-mnist-svc
  namespace: default
  ownerReferences:
  - apiVersion: batch.volcano.sh/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: Job
    name: tensorflow-dist-mnist
    uid: 52c98cc2-4791-490f-8572-22df2c16ef8f
  resourceVersion: "855340"
  uid: c4f3db21-6857-451f-b8b8-bbd5aa8b06ec
```
* The networkpolicy `tensorflow-dist-mnist` generated for the job is as follows.
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  creationTimestamp: "2022-04-13T02:08:15Z"
  name: tensorflow-dist-mnist
  namespace: default
  ownerReferences:
  - apiVersion: batch.volcano.sh/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: Job
    name: tensorflow-dist-mnist
    uid: 52c98cc2-4791-490f-8572-22df2c16ef8f
  resourceVersion: "855343"
  uid: ddf8aada-51d7-47c1-99a0-5e0d8a913a4d
spec:
  ingress:
  - from:
    - podSelector:
        matchLabels:
          volcano.sh/job-name: tensorflow-dist-mnist
          volcano.sh/job-namespace: default
  podSelector:
    matchLabels:
      volcano.sh/job-name: tensorflow-dist-mnist
      volcano.sh/job-namespace: default
  policyTypes:
  - Ingress
```
## Note
* DNS plugin is required in your Kubernetes cluster such as `corndns`.
* Kubernetes version >= v1.14