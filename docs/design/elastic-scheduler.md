## Introduction

This feature allows volcano to schedule workloads based on the `[min,max]` config to improve resource utilization rate and shorten the execution time of training job.

for example, k8s cluster has 10 GPUs, I want to use volcano schedule training job(tfjob/pytorchjob/vcjob) in two queues: queue1 and queue2

||weight|  reclaimable| deserved GPUs|
|---|---|---|---|
|queue1|  1|   false|    5|
|queue2|  1|   false|    5|

![](images/elastic-scheduler-job1-1.png)

if there is job1-1 running in queue1, we see pod6~pod10 as elastic pods/resources because they can be preempted if queue resource is shortage. elastic pods is the lowest priority, **it should be created last and be preempted first**.
1. elastic pods can be created only when there are free resources.
2. elastic pods can be preempted if there are not enough resources for running minAvailable pods.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: job1-1
spec:
  minAvailable: 5     #min
  queue: queue1
  tasks:
    - replicas: 10    #max
      name: job1-1
      template:
        metadata:
          name: job1-1
        spec:
          containers:
            - image: train_script
              name: xx
              resources:
                limits:
                  cpu: 1
                  nvidia.com/gpu: 1
```



in detail, there are some principles for elastic schedule
1. if submit job1-1 and job1-2 at the same time, it should create job1.minAvailable pods and job2.minAvailable pods first, and then create job1/job2.elastic pods if there are extra resource.
   ![](images/elastic-scheduler-job1-1-2.png)
2. if submit job1-1 and then submit job1-2 in queue1, elastic pods in job1-1 will be preempted
   ![](images/elastic-scheduler-job1-2.png)
3. if submit job1-1 and then submit job2-1 in queue2, elastic pods in job1-1 will be preempted
   ![](images/elastic-scheduler-job2-1.png)

## Design

1. enqueue action. relax control of job enqueue logic. because elastic pods can be preempted at any time, we can see elastic resources as free resources in queue, so we need fix overcommit.jobEnqueueableFns and proportion.jobEnqueueableFns. it should be noticed that  if total elastic resources can not be satisfied with new-job's minReq, the new-job also should be pending.
   ![](images/elastic-scheduler-job1-3.png)
2. allocate action(already implemented), create minAvailable pods(all job) first, and then create elastic pods if there are free resources.
3. preempt action. in queue scope. 
   1. (already implemented) preempt elastic pods if there are starving job in the same queue. 
   2. it should be noticed that  if total elastic resources can not be satisfied with starving job's minReq, it is not necessary to preempt elastic pods.
4. reclaim action. in cluster scope. if a queue is overused, reclaim its elastic resources whether queue.reclaimable is true or not
