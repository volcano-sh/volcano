# Task Launch Order Within Job

@[hwdef](https://github.com/hwdef), @[Thor-wl](https://github.com/Thor-wl); July 19th, 2021

## Introduction

This feature provides the ability to customize the order in which tasks are launched.

## Scope

### In Scope

* Reasons why this feature is needed
* Define the API

### Out of Scope

* Start order of job
* Task start sequence between multiple jobs
* Dependency completion state of the task start sequence

## Scenarios

* MPI Job. The worker pods need to be started first and then the master pod can run. Elsewise, the master pod can't setup the ssh tunnel and thus failed, this will add unnecessary waste of resources. In this case, MPI worker pods need to be in the running state, and then the master pod can start. Very similary, for the TensorFlow job, the TF master pod must be started first and then the TF worker pods.

## Scene Comparison

| example   | number of tasks | task dependencies | concurrent execution | Solution Status   | Disadvantages of the current solution |
| ---------- | -------- | ------------ | -------- | -------------------------------------------------- | -------------------------- |
| MPI        | 2        | linear dependencies | yes    | 1. Add initcontainer to the task<br/>2.Using user-written check scripts in initcontainer | 1.Increase the cost of use for users<br />2.Still using resources while waiting |
| matlab     | 2        | linear dependencies | yes    | 1. Add initcontainer to the task<br/>2.Using user-written check scripts in initcontainer |1.Increase the cost of use for users<br />2.Still using resources while waiting|
| tensorflow | n>=2   | linear dependencies | yes    | 1. Add initcontainer to the task<br/>2.Using user-written check scripts in initcontainer |1.Increase the cost of use for users<br />2.Still using resources while waiting|

## Requirement

Based on the scenarios listed above, the dependencies can be abstracted as:

* Task-A depends on task-B, which means A must be started first and then B.
* Triggering policy, in our cases, there may be only one trigger policy, which is the running state

For the ease of end user's experince, we need to unify the way of composing a complicated jobs with lots of tasks, instead of letting user handle the complexcity themselvs using init-containers or other workflow tools. So we need to  have an more advanced VCJob that has below abilities:

## Design

### Field

Add a field in `vcjob.spec.task.dependsOn`, it represents the name of the tasks that this task needs to depend on

### API
vcjob example
```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: lm-mpi-job
spec:
  ......
  tasks:
    - replicas: 2
      name: mpiworker
      template:
      ......
    - replicas: 1
      name: mpimaster
      dependsOn: 
        name: mpiworker
      template:
      ......
```

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: example-job
spec:
  ......
  tasks:
    - replicas: 2
      name: task1
      template:
      ......
    - replicas: 1
      name: task2
      template:
      ......
    - replicas: 2
      name: task3
      dependsOn: 
        name: task1, task2
        iteration: any
      template:
      ......
```

### Usage
* create a job that contains at least two tasks, fill in the task name in the `vcjob.spec.task.dependsOn` field, this name indicates the task that this task wants to rely on.
* If there are multiple dependent tasks, you need to fill in the `iteration` field, the value can be `any` or `all`, `any` means that one of the multiple tasks reach the running state then run this task, `all` means that all tasks reach the runnig state before running this task.
* get task status to check if it is correct order

### Implementaion
* create a new field in vcjob
* create admission for this field

### Notice
* deal with the conflict with gang-scheduling, you need to disable the gang plugin when using it.
