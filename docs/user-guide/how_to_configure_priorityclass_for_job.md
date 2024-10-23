# How to configure priorityclass for job

## Background
When a user creates a job, if there is no `PriorityClassName` specified in the template of the task, the task will use the `PriorityClassName` specified by the job. The user can specify `PriorityClassName` in each task's template to override the configuration of job, so that priority can be configured separately for each task.

## Key Points
- If the task does not specify `PriorityClassName` but the job does, the task will use the job's PriorityClass, and the `PreemptionPolicy` and priority value will also be the same as the job. 
- When user needs to allow task in job to preempt other tasks if the resources are insufficient, a separate `PriorityClassName` must be set in the task's template, and **it is important to note that if there are multiple tasks that need to be set to different priorities, then user need to set the `PriorityClassName` for all of them**, otherwise the task that does not specify `PriorityClassName` will use the PriorityClass of the job it belongs to.

## Example
```yaml
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: job-priority
value: 1
preemptionPolicy: Never
---
apiVersion: scheduling.k8s.io/v1
kind: PriorityClass
metadata:
  name: task-priority
value: 10
---
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: job
spec:
  schedulerName: volcano
  priorityClassName: job-priority
  minAvailable: 1
  tasks:
    - replicas: 1
      name: preempt-task
      template:
        spec:
          priorityClassName: task-priority # The priorityclass is specified individually for this task
          containers:
            - image: alpine
              command: ["/bin/sh", "-c", "sleep 1000"]
              name: preempt
              resources:
                requests:
                  cpu: 1
    - replicas: 1
      name: non-preempt-task # This task does not specify PriorityClassName, so it will use the "job-priority" priorityclass specified by job
      template:
        spec:
          containers:
            - image: alpine
              command: ["/bin/sh", "-c", "sleep 1000"]
              name: non-preempt
              resources:
                requests:
                  cpu: 1
```