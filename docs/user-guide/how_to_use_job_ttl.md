# How to Configure Volcano Job Time to Live
## Background
Similar to a standard `Job` resource, VolcanoJobs can be configured to be automatically garbage 
collected after they finish execution (either Complete or Failed). This is configured by setting 
`spec.ttlSecondsAfterFinished` which limits the lifetime of a Job.

## Key Points
`ttlSecondsAfterFinished` is an optional parameter that can be configured on VolcanoJobs which 
defaults to `nil`. The value of `ttlSecondsAfterFinished` must be a positive integer and indicates 
the number of seconds after a job finishes executing (either Complete or Failed) before it becomes 
eligible for garbage collection.

If `ttlSecondsAfterFinished` is unset or set to `nil`, the job will remain indefinitely. If set to 
zero (`0`), the job will become eligible for garbage collection immediately upon completion. If set 
to a positive integer, `N`, the job will become eligible for garbage collection `N` seconds after 
the job has completed.

## Other Reading
While this uses a custom garbage collector, this operates nearly identically to 
`ttlSecondsAfterFinished` from a standard `batch.v1.job` resource. The [official Kubernetes 
documentation](https://kubernetes.io/docs/concepts/workloads/controllers/ttlafterfinished/) has some 
useful tips describing how mutating webhooks can be used to take greater advantage of 
`ttlSecondsAfterFinished`.

## Example
The manifest below creates a job that will be eligible for garbage collection 10 minutes after it 
either completes or fails.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: job
metadata:
  generateName: test-job-
spec:
  minAvailable: 1
  schedulerName: volcano
  queue: testing
  ttlSecondsAfterFinished: 600
  policies:
    - event: PodEvicted
      action: RestartJob
  tasks:
    - replicas: 1
      name: sleeper
      policies:
        - event: TaskCompleted
          action: CompleteJob
      template:
        spec:
          restartPolicy: Never
          imagePullPolicy: IfNotPresent
          containers:
            - name: sleeper
              image: debian:buster
              command:
                - /bin/bash
                - -c
                - |
                  for i in {0..5}; do
                      echo "sleeping"
                      sleep 1
                  done
```
