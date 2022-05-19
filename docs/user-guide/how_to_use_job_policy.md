# How to Configure Volcano Job Policy
## Background
`Policy` provides an API of volcano job and task lifecycle management for users. For example, in some scenarios, especially
in AI, big data and HPC field, it is required to restart a job if any `master` or `worker` fails. Users can easily achieve that
by configuring `policy` for the volcano job under `job.spec`.

## Key Points
* Volcano allows users to configure a pair of `Event`(`Events`) and `Action` for a volcano job or a task. If the specified
event(events) happens, the target action will be triggered.
* If the policy is configured under `job.spec` only, it will work for all tasks by default. If the policy is configured
under `task.spec` only, it will only work for the task. If the policy is configured in both job and task level, it will obey
the task policy.
* Users can set multiple policy for a job or a task.
* Currently, Volcano provides **5 build-in events** for users. The details are as follows.

| ID  | Event           | Description                                                                                                                                                                                                       |
|-----|-----------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 1   | `PodFailed`     | Check whether there is any pod' status is `Failed`.                                                                                                                                                               |
| 2   | `PodEvicted`    | Check whether there is any pod is evicted.                                                                                                                                                                        |
| 3   | `Unknown`       | Check whether the status of a volcano job is `Unknown`. The most possible factor is task unschedulable. It is triggered when part pods can't be scheduled while some are already running in gang-scheduling case. |
| 4   | `TaskCompleted` | Check whether there is a task whose all pods are succeed. If `minsuccess` is configured for a task, it will also be regarded as task completes.                                                                   |
| 5   | `*`             | It means all the events, which is not so common used.                                                                                                                                                             |

* Currently, Volcano provides **5 build-in actions** for users. The details are as follows.

| ID  | Action            | Description                                                                                                      |
|-----|-------------------|------------------------------------------------------------------------------------------------------------------|
| 1   | `AbortJob`        | Abort the whole job, but it can be resumed. All pods will be evicted and no pod will be recreated.               |
| 2   | `RestartJob`      | Restart the whole job.                                                                                           |
| 3   | `RestartTask`     | Default action. The task will be restarted. This action **cannot** work with job level events such as `Unknown`. |
| 4   | `TerminateJob`    | Terminate the whole job and it **cannot** be resumed. All pods will be evicted and no pod will be recreated.     |
| 5   | `CompleteJob`     | Regard the job as completed. The unfinished pods will be killed.                                                 |

## Examples
1. Set a pair of `event` and `action`.
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
    svc: []
  policies:
    - event: PodEvicted   # Job level policy. If any pod is evicted, restart the job. It will only work on `ps` task.
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
        - event: TaskCompleted    # Task level policy. If this task completes, complete the job.
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
2. Set a pair of `events` and `action`.
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
    svc: []
  queue: default
  tasks:
    - replicas: 1
      name: ps
      policies:
        - events: [PodEvicted, PodFailed]   # Task level policy. If any pod is evicted or fails in this task, restart the job.
          action: RestartJob
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
        - event: TaskCompleted  # Task level policy. If this task completes, complete the job.
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