# Volcano Job Plugin -- Env User Guidance

## Background
**Env Plugin** is designed for business that a pod should be aware of its index in the task such as [MPI](https://www.open-mpi.org/)
and [TensorFlow](https://tensorflow.google.cn/). The indices will be registered as **environment variables** automatically
when the Volcano job is created. For example, a tensorflow job consists of *1* ps and *2* workers. And each worker maps 
to a slice of raw data. In order to make the workers be aware of its target slice, they get their index in the environment
variables.

## Key Points
* The index keys of the environment variables are `VK_TASK_INDEX` and `VC_TASK_INDEX`, they have the same value.
* The value of the indices is a number which ranges from `0` to `length - 1`. The `length` equals to the number of replicas 
of the task. It is also the index of the pod in the task. 

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
    env: []   ## Env plugin register, note that no values are needed in the array.
    svc: []
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
                  PS_HOST=`cat /etc/volcano/ps.host | sed 's/$/&:2222/g' | sed 's/^/"/;s/$/"/' | tr "\n" ","`;
                  WORKER_HOST=`cat /etc/volcano/worker.host | sed 's/$/&:2222/g' | sed 's/^/"/;s/$/"/' | tr "\n" ","`;
                  export TF_CONFIG={\"cluster\":{\"ps\":[${PS_HOST}],\"worker\":[${WORKER_HOST}]},\"task\":{\"type\":\"ps\",\"index\":${VK_TASK_INDEX}},\"environment\":\"cloud\"};   ## Get the index from the environment variable and configure it in the TF job.
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
* When config env plugin in the tensorflow job above, all the pods will have 2 environment variables `VK_TASK_INDEX` and 
`VC_TASK_INDEX`. The environment variables registered in the `ps` pod are as follows.
```
[root@tensorflow-dist-mnist-ps-0 /] env | grep TASK_INDEX
VK_TASK_INDEX=0
VC_TASK_INDEX=0
```
* Considering the 2 workers, you will get that their names are `tensorflow-dist-mnist-worker-0` and
`tensorflow-dist-mnist-worker-1`. And the corresponding values of the index environment variables are `0` and `1`.
```
[root@tensorflow-dist-mnist-worker-0 /] env | grep TASK_INDEX
VK_TASK_INDEX=0
VC_TASK_INDEX=0
```
```
[root@tensorflow-dist-mnist-worker-1 /] env | grep TASK_INDEX
VK_TASK_INDEX=1
VC_TASK_INDEX=1
```
## Note
* Because of historical reasons, environment variables `VK_TASK_INDEX` and `VC_TASK_INDEX` both exist, `VK_TASK_INDEX` will
be **deprecated** in the future releases.
* No value are needed when register env plugin in the volcano job.