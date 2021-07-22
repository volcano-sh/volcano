# Task-level DAG scheduling policy

@[hwdef](https://github.com/hwdef); July 19th, 2021

## Introduction

This feature provides the ability to customize the order in which tasks are launched.

## Scenarios

* mpi job. the master needs to wait for the worker to start before starting, If the master is already started, but the worker is not yet started, the master will restart, which will add unnecessary waste of resources ,in this case, mpiworker needs to be in the running state, and then create the pod for mpimaster..

* Suppose there are two tasks and task 2 needs to use the calculation results of task 1,in this case, task1 needs to be in the complete state, and then create the pod for task2.

* some ETL applications

## Requirement

Each task can specify which one or more tasks it depends on, and it can specify the time point or interface for triggering.

Examples of time points:

* running
* has been running for 10s
* complete

Examples of interfaces:

* Check if a file named raw exists in the /data directory.
* Check if the specified port is open, such as 2222.
* Check if the HTTP GET request to the specified path was successfully accessed, such as /health.

## Design

1. Add a field that is an array named `dependOn` in task spec

```go
type TaskSpec struct {
    ...
	dependOn []dependOn
}
```
2. It contains three fields, `taskName`, `trigger`, `probe`

```go
type dependOn struct {
	taskName string
	Trigger  TriggerSpec
	Probe    *v1.Probe
}
```
2.1 `taskName` is a string indicating the name of the task that the current task depends on

2.2 `trigger` indicates the point in time when the trigger, it has two fields, `status` and `time`, `status` indicates the status of the task, `time` indicates the duration of time since the state was reached,it can be empty, indicating 0 seconds.

```go
type TriggerSpec struct {
	Event string
	Time   time.Duration
}
```

2.3 `probe` is used to detect the status of the task in dependOn. This field can be used with the `probe` struct in kubernetes

eg:

```yaml
dependOn:
  - taskName: taskA, taskB
    probe:
        exec:
            command:
            - cat
            - /data/raw
      initialDelaySeconds: 5
      periodSeconds: 5

  - taskName: taskC
    trigger: 
      event: running
      time: 10s
  - taskName: taskD
    trigger:
      event: running
  - taskName: taskE
    trigger:
      event: complete
```

## Reference

[configure-liveness-readiness-startup-probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)