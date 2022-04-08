# Job API

[@k82cn](http://github.com/k82cn);  Dec 27, 2018

## Motivation

`Job` is the fundamental object of high performance workload; this document provides the definition of `Job` in Volcano.

## Scope

### In Scope

* Define the API of Job
* Define the behaviour of Job
* Clarify the interaction with other features

### Out of Scope

* Volumes: volume management is out of scope for job management related features
* Network: the addressing between tasks will be described in other project

## Function Detail

The definition of `Job` follow Kuberentes's style, e.g. Status, Spec; the follow sections will only describe
the major functions of `Job`, refer to [Appendix](#appendix) section for the whole definition of `Job`.

### Multiple Pod Template

As most jobs of high performance workload include different type of tasks, e.g. TensorFlow (ps/worker), Spark (driver/executor);
`Job` introduces `taskSpecs` to support multiple pod template, defined as follow.  The `Policies` will describe in
 [Error Handling](#error-handling) section.

 ```go
// JobSpec describes how the job execution will look like and when it will actually run
type JobSpec struct {
    ...

    // Tasks specifies the task specification of Job
    // +optional
    Tasks []TaskSpec `json:"tasks,omitempty" protobuf:"bytes,5,opt,name=tasks"`
}

// TaskSpec specifies the task specification of Job
type TaskSpec struct {
    // Name specifies the name of task
    Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

    // Replicas specifies the replicas of this TaskSpec in Job
    Replicas int32 `json:"replicas,omitempty" protobuf:"bytes,2,opt,name=replicas"`

    // Specifies the pod that will be created for this TaskSpec
    // when executing a Job
    Template v1.PodTemplateSpec `json:"template,omitempty" protobuf:"bytes,3,opt,name=template"`

    // Specifies the lifecycle of tasks
    // +optional
    Policies []LifecyclePolicy `json:"policies,omitempty" protobuf:"bytes,4,opt,name=policies"`
}
```

`JobController` will create Pods based on the templates and replicas in `spec.tasks`;
the controlled `OwnerReference` of Pod will be set to the `Job`. The following is
an example YAML with multiple pod template.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: tf-job
spec:
  tasks:
  - name: "ps"
    replicas: 2
    template:
      spec:
        containers:
        - name: ps
          image: ps-img
  - name: "worker"
    replicas: 5
    template:
      spec:
        containers:
        - name: worker
          image: worker-img
```

### Job Input/Output

Most of high performance workload will handle data which is considering as input/output of a Job.
The following types are introduced for Job's input/output.

```go
type VolumeSpec struct {
	MountPath string `json:"mountPath" protobuf:"bytes,1,opt,name=mountPath"`

	// defined the PVC name
	// + optional
	VolumeClaimName string `json:"volumeClaimName,omitempty" protobuf:"bytes,2,opt,name=volumeClaimName"`

	// VolumeClaim defines the PVC used by the VolumeSpec.
	// + optional
	VolumeClaim *PersistentVolumeClaim `json:"claim,omitempty" protobuf:"bytes,3,opt,name=claim"`
}

type JobSpec struct{
    ...

    // The volumes mount on Job
    // +optional
    Volumes []VolumeSpec `json:"volumes,omitempty" protobuf:"bytes,1,opt,name=volumes"`
}
```

The `Volumes` of Job can be `nil` which means user will manage data themselves. If `*VolumeSpec.volumeClaim` is `nil` and `*VolumeSpec.volumeClaimName` is `nil` or not exist in PersistentVolumeClaim，`emptyDir` volume will be used for each Task/Pod.

### Conditions and Phases

The following phases are introduced to give a simple, high-level summary of where the Job is in its lifecycle; and the conditions array,
the reason and message field contain more detail about the job's status.

```go
type JobPhase string

const (
    // Pending is the phase that job is pending in the queue, waiting for scheduling decision
    Pending JobPhase = "Pending"
    // Aborting is the phase that job is aborted, waiting for releasing pods
    Aborting JobPhase = "Aborting"
    // Aborted is the phase that job is aborted by user or error handling
    Aborted JobPhase = "Aborted"
    // Running is the phase that minimal available tasks of Job are running
    Running JobPhase = "Running"
    // Restarting is the phase that the Job is restarted, waiting for pod releasing and recreating
    Restarting JobPhase = "Restarting"
    // Completed is the phase that all tasks of Job are completed successfully
    Completed JobPhase = "Completed"
    // Terminating is the phase that the Job is terminated, waiting for releasing pods
    Terminating JobPhase = "Terminating"
    // Teriminated is the phase that the job is finished unexpected, e.g. events
    Teriminated JobPhase = "Terminated"
)

// JobState contains details for the current state of the job.
type JobState struct {
    // The phase of Job.
    // +optional
    Phase JobPhase `json:"phase,omitempty" protobuf:"bytes,1,opt,name=phase"`

    // Unique, one-word, CamelCase reason for the phase's last transition.
    // +optional
    Reason string `json:"reason,omitempty" protobuf:"bytes,2,opt,name=reason"`

    // Human-readable message indicating details about last transition.
    // +optional
    Message string `json:"message,omitempty" protobuf:"bytes,3,opt,name=message"`
}

// JobStatus represents the current state of a Job
type JobStatus struct {
    // Current state of Job.
    State JobState `json:"state,omitempty" protobuf:"bytes,1,opt,name=state"`

    ......
}
```

The following table shows available transactions between different phases. The phase can not transfer to the target
phase if the cell is empty.

| From \ To     | Pending | Aborted | Running | Completed | Terminated |
| ------------- | ------- | ------- | ------- | --------- | ---------- |
| Pending       | *       | *       | *       |           |            |
| Aborted       | *       | *       |         |           |            |
| Running       |         | *       | *       | *         | *          |
| Completed     |         |         |         | *         |            |
| Terminated    |         |         |         |           | *          |

`Restarting`, `Aborting` and `Terminating` are temporary states to avoid race condition, e.g. there'll be several
`PodeEvictedEvent`s because of `TerminateJobAction` which should not be handled again.

### Error Handling

After Job was created in system, there'll be several events related to the Job, e.g. Pod succeeded, Pod failed;
and some events are critical to the Job, e.g. Pod of MPIJob failed. So `LifecyclePolicy` is introduced to handle different
events based on user's configuration.

```go
// Event is the type of Event related to the Job
type Event string

const (
    // AllEvents means all event
    AllEvents             Event = "*"
    // PodFailedEvent is triggered if Pod was failed
    PodFailedEvent        Event = "PodFailed"
    // PodEvictedEvent is triggered if Pod was deleted
    PodEvictedEvent       Event = "PodEvicted"
    // These below are several events can lead to job 'Unknown'
    // 1. Task Unschedulable, this is triggered when part of
    //    pods can't be scheduled while some are already running in gang-scheduling case.
    JobUnknownEvent Event = "Unknown"

    // OutOfSyncEvent is triggered if Pod/Job were updated
    OutOfSyncEvent Event = "OutOfSync"
    // CommandIssuedEvent is triggered if a command is raised by user
    CommandIssuedEvent Event = "CommandIssued"
    // TaskCompletedEvent is triggered if the 'Replicas' amount of pods in one task are succeed
    TaskCompletedEvent Event = "TaskCompleted"
)

// Action is the type of event handling
type Action string

const (
    // AbortJobAction if this action is set, the whole job will be aborted:
    // all Pod of Job will be evicted, and no Pod will be recreated
    AbortJobAction Action = "AbortJob"
    // RestartJobAction if this action is set, the whole job will be restarted
    RestartJobAction Action = "RestartJob"
    // TerminateJobAction if this action is set, the whole job wil be terminated
    // and can not be resumed: all Pod of Job will be evicted, and no Pod will be recreated.
    TerminateJobAction Action = "TerminateJob"
    // CompleteJobAction if this action is set, the unfinished pods will be killed, job completed.
    CompleteJobAction Action = "CompleteJob"

    // ResumeJobAction is the action to resume an aborted job.
    ResumeJobAction Action = "ResumeJob"
    // SyncJobAction is the action to sync Job/Pod status.
    SyncJobAction Action = "SyncJob"
)

// LifecyclePolicy specifies the lifecycle and error handling of task and job.
type LifecyclePolicy struct {
    Event  Event  `json:"event,omitempty" protobuf:"bytes,1,opt,name=event"`
    Action Action `json:"action,omitempty" protobuf:"bytes,2,opt,name=action"`
    Timeout *metav1.Duration `json:"timeout,omitempty" protobuf:"bytes,3,opt,name=timeout"`
}
```

Both `JobSpec` and `TaskSpec` include lifecycle policy: the policies in `JobSpec` are the default policy if no policies
in `TaskSpec`; the policies in `TaskSpec` will overwrite defaults.

```go
// JobSpec describes how the job execution will look like and when it will actually run
type JobSpec struct {
    ...

    // Specifies the default lifecycle of tasks
    // +optional
    Policies []LifecyclePolicy `json:"policies,omitempty" protobuf:"bytes,5,opt,name=policies"`

    // Tasks specifies the task specification of Job
    // +optional
    Tasks []TaskSpec `json:"tasks,omitempty" protobuf:"bytes,6,opt,name=tasks"`
}

// TaskSpec specifies the task specification of Job
type TaskSpec struct {
    ...

    // Specifies the lifecycle of tasks
    // +optional
    Policies []LifecyclePolicy `json:"policies,omitempty" protobuf:"bytes,4,opt,name=policies"`
}
```

The following examples demonstrate the usage of `LifecyclePolicy` for job and task.

For the training job of machine learning framework, the whole job should be restarted if any task was failed or evicted.
To simplify the configuration, a job level `LifecyclePolicy` is set as follows.  As no `LifecyclePolicy` is set for any
task, all tasks will use the policies in `spec.policies`.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: tf-job
spec:
  # If any event here, restart the whole job.
  policies:
  - event: *
    action: RestartJob
  tasks:
  - name: "ps"
    replicas: 1
    template:
      spec:
        containers:
        - name: ps
          image: ps-img
  - name: "worker"
    replicas: 5
    template:
      spec:
        containers:
        - name: worker
          image: worker-img
  ...
```

Some BigData framework (e.g. Spark) may have different requirements. Take Spark as example, the whole job will be restarted
if 'driver' tasks failed and only restart the task if 'executor' tasks failed. `OnFailure` restartPolicy is set for executor
and `RestartJob` is set for driver `spec.tasks.policies` as follow.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: spark-job
spec:
  tasks:
  - name: "driver"
    replicas: 1
    policies:
    - event: *
      action: RestartJob
    template:
      spec:
        containers:
        - name: driver
          image: driver-img
  - name: "executor"
    replicas: 5
    template:
      spec:
        containers:
        - name: executor
          image: executor-img
        restartPolicy: OnFailure
```

## Features Interaction

### Admission Controller

The following validations must be included to make sure expected behaviours:

* `spec.minAvailable` <= sum(`spec.taskSpecs.replicas`)
* no duplicated name in `spec.taskSpecs` array
* no duplicated event handler in `LifecyclePolicy` array, both job policies and task policies

### CoScheduling

CoScheduling (or Gang-scheduling) is required by most of high performance workload, e.g. TF training job, MPI job.
The `spec.minAvailable` is used to identify how many pods will be scheduled together. The default value of `spec.minAvailable`
is summary of `spec.tasks.replicas`. The admission controller web hook will check `spec.minAvailable` against
the summary of `spec.tasks.replicas`; the job creation will be rejected if `spec.minAvailable` > sum(`spec.tasks.replicas`).
If `spec.minAvailable` < sum(`spec.tasks.replicas`), the pod of `spec.tasks` will be created randomly;
refer to [Task Priority with Job](#task-priority-within-job) section on how to create tasks in order.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: tf-job
spec:
  # minAvailable to run job
  minAvailable: 6
  tasks:
  - name: "ps"
    replicas: 1
    template:
      spec:
        containers:
        - name: "ps"
          image: "ps-img"
  - name: "worker"
    replicas: 5
    template:
      spec:
        containers:
        - name: "worker"
          image: "worker-img"
```

### Task Priority within Job

In addition to multiple pod template, the priority of each task maybe different. `PriorityClass` of `PodTemplate` is reused
to define the priority of task within a job. This's an example to run spark job: 1 driver with 5 executors, the driver's
priority is `master-pri` which is higher than normal pods; as `spec.minAvailable` is 3, the scheduler will make sure one driver
with 2 executors will be scheduled if not enough resources.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: spark-job
spec:
  minAvailable: 3
  tasks:
  - name: "driver"
    replicas: 1
    template:
      spec:
        priorityClass: "master-pri"
        containers:
        - name: driver
          image: driver-img
  - name: "executor"
    replicas: 5
    template:
      spec:
        containers:
        - name: executor
          image: executor-img
```

**NOTE**: although scheduler will make sure high priority pods with job will be scheduled firstly, there's still a race
condition between different kubelets that low priority pod maybe launched early; the job/task dependency will be introduced
later to handle such kind of race condition.

### Resource sharing between Job

By default, the `spec.minAvailable` is set to the summary of `spec.tasks.replicas`; if it's set to a smaller value,
the pod beyond `spec.minAvailable` will share resource between jobs.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: spark-job
spec:
  minAvailable: 3
  tasks:
  - name: "driver"
    replicas: 1
    template:
      spec:
        priorityClass: "master-pri"
        containers:
        - name: driver
          image: driver-img
  - name: "executor"
    replicas: 5
    template:
      spec:
        containers:
        - name: executor
          image: executor-img
```

### Plugins for Job

As many jobs of AI frame, e.g. TensorFlow, MPI, Mxnet, need set env, pods communicate, ssh sign in without password.
We provide Job api plugins to give users a better focus on core business.
Now we have three plugins, every plugin has parameters, if not provided, we use default.

* env: set VK_TASK_INDEX to each container, is a index for giving the identity to container.
* svc: create Service and *.host to enable pods communicate.
* ssh: sign in ssh without password, e.g. use command mpirun or mpiexec.

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: mpi-job
spec:
  minAvailable: 2
  schedulerName: volcano
  policies:
  - event: PodEvicted
    action: RestartJob
  plugins:
    ssh: []
    env: []
    svc: []
  tasks:
  - replicas: 1
    name: mpimaster
    template:
      spec:
        containers:
          image: mpi-image
          name: mpimaster
  - replicas: 2
    name: mpiworker
    template:
      spec:
        containers:
          image: mpi-image
          name: mpiworker
```

## Appendix

```go
type Job struct {
    metav1.TypeMeta `json:",inline"`

    metav1.ObjectMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

    // Specification of the desired behavior of a cron job, including the minAvailable
    // +optional
    Spec JobSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

    // Current status of Job
    // +optional
    Status JobStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// JobSpec describes how the job execution will look like and when it will actually run
type JobSpec struct {
    // SchedulerName is the default value of `taskSpecs.template.spec.schedulerName`.
    // +optional
    SchedulerName string `json:"schedulerName,omitempty" protobuf:"bytes,1,opt,name=schedulerName"`

    // The minimal available pods to run for this Job
    // +optional
    MinAvailable int32 `json:"minAvailable,omitempty" protobuf:"bytes,2,opt,name=minAvailable"`

    // The volumes mount on Job
    Volumes []VolumeSpec `json:"volumes,omitempty" protobuf:"bytes,3,opt,name=volumes"`

    // Tasks specifies the task specification of Job
    // +optional
    Tasks []TaskSpec `json:"taskSpecs,omitempty" protobuf:"bytes,4,opt,name=taskSpecs"`

    // Specifies the default lifecycle of tasks
    // +optional
    Policies []LifecyclePolicy `json:"policies,omitempty" protobuf:"bytes,5,opt,name=policies"`

    // Specifies the plugin of job
    // Key is plugin name, value is the arguments of the plugin
    // +optional
    Plugins map[string][]string `json:"plugins,omitempty" protobuf:"bytes,6,opt,name=plugins"`

    //Specifies the queue that will be used in the scheduler, "default" queue is used this leaves empty.
    Queue string `json:"queue,omitempty" protobuf:"bytes,7,opt,name=queue"`

    // Specifies the maximum number of retries before marking this Job failed.
    // Defaults to 3.
    // +optional
    MaxRetry int32 `json:"maxRetry,omitempty" protobuf:"bytes,8,opt,name=maxRetry"`
}

// VolumeSpec defines the specification of Volume, e.g. PVC
type VolumeSpec struct {
    MountPath string `json:"mountPath" protobuf:"bytes,1,opt,name=mountPath"`

    // defined the PVC name
    VolumeClaimName string `json:"volumeClaimName,omitempty" protobuf:"bytes,2,opt,name=volumeClaimName"`

    // VolumeClaim defines the PVC used by the VolumeMount.
    VolumeClaim *v1.PersistentVolumeClaimSpec `json:"volumeClaim,omitempty" protobuf:"bytes,3,opt,name=volumeClaim"`
}

// Event represent the phase of Job, e.g. pod-failed.
type Event string

const (
    // AllEvent means all event
    AllEvents Event = "*"
    // PodFailedEvent is triggered if Pod was failed
    PodFailedEvent Event = "PodFailed"
    // PodEvictedEvent is triggered if Pod was deleted
    PodEvictedEvent Event = "PodEvicted"
    // These below are several events can lead to job 'Unknown'
    // 1. Task Unschedulable, this is triggered when part of
    //    pods can't be scheduled while some are already running in gang-scheduling case.
    JobUnknownEvent Event = "Unknown"

    // OutOfSyncEvent is triggered if Pod/Job were updated
    OutOfSyncEvent Event = "OutOfSync"
    // CommandIssuedEvent is triggered if a command is raised by user
    CommandIssuedEvent Event = "CommandIssued"
    // TaskCompletedEvent is triggered if the 'Replicas' amount of pods in one task are succeed
    TaskCompletedEvent Event = "TaskCompleted"
)

// Action is the action that Job controller will take according to the event.
type Action string

const (
    // AbortJobAction if this action is set, the whole job will be aborted:
    // all Pod of Job will be evicted, and no Pod will be recreated
    AbortJobAction Action = "AbortJob"
    // RestartJobAction if this action is set, the whole job will be restarted
    RestartJobAction Action = "RestartJob"
    // TerminateJobAction if this action is set, the whole job wil be terminated
    // and can not be resumed: all Pod of Job will be evicted, and no Pod will be recreated.
    TerminateJobAction Action = "TerminateJob"
    // CompleteJobAction if this action is set, the unfinished pods will be killed, job completed.
    CompleteJobAction Action = "CompleteJob"

    // ResumeJobAction is the action to resume an aborted job.
    ResumeJobAction Action = "ResumeJob"
    // SyncJobAction is the action to sync Job/Pod status.
    SyncJobAction Action = "SyncJob"
)

// LifecyclePolicy specifies the lifecycle and error handling of task and job.
type LifecyclePolicy struct {
    // The action that will be taken to the PodGroup according to Event.
    // One of "Restart", "None".
    // Default to None.
    // +optional
    Action Action `json:"action,omitempty" protobuf:"bytes,1,opt,name=action"`

    // The Event recorded by scheduler; the controller takes actions
    // according to this Event.
    // +optional
    Event Event `json:"event,omitempty" protobuf:"bytes,2,opt,name=event"`

    // Timeout is the grace period for controller to take actions.
    // Default to nil (take action immediately).
    // +optional
    Timeout *metav1.Duration `json:"timeout,omitempty" protobuf:"bytes,3,opt,name=timeout"`
}

// TaskSpec specifies the task specification of Job
type TaskSpec struct {
    // Name specifies the name of tasks
    Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

    // Replicas specifies the replicas of this TaskSpec in Job
    Replicas int32 `json:"replicas,omitempty" protobuf:"bytes,2,opt,name=replicas"`

    // Specifies the pod that will be created for this TaskSpec
    // when executing a Job
    Template v1.PodTemplateSpec `json:"template,omitempty" protobuf:"bytes,3,opt,name=template"`

    // Specifies the lifecycle of task
    // +optional
    Policies []LifecyclePolicy `json:"policies,omitempty" protobuf:"bytes,4,opt,name=policies"`
}

type JobPhase string

const (
    // Pending is the phase that job is pending in the queue, waiting for scheduling decision
    Pending JobPhase = "Pending"
    // Aborting is the phase that job is aborted, waiting for releasing pods
    Aborting JobPhase = "Aborting"
    // Aborted is the phase that job is aborted by user or error handling
    Aborted JobPhase = "Aborted"
    // Running is the phase that minimal available tasks of Job are running
    Running JobPhase = "Running"
    // Restarting is the phase that the Job is restarted, waiting for pod releasing and recreating
    Restarting JobPhase = "Restarting"
    // Completing is the phase that required tasks of job are completed, job starts to clean up
    Completing JobPhase = "Completing"
    // Completed is the phase that all tasks of Job are completed successfully
    Completed JobPhase = "Completed"
    // Terminating is the phase that the Job is terminated, waiting for releasing pods
    Terminating JobPhase = "Terminating"
    // Terminated is the phase that the job is finished unexpected, e.g. events
    Terminated JobPhase = "Terminated"
    // Failed is the phase that the job is restarted failed reached the maximum number of retries.
    Failed JobPhase = "Failed"
)

// JobState contains details for the current state of the job.
type JobState struct {
    // The phase of Job.
    // +optional
    Phase JobPhase `json:"phase,omitempty" protobuf:"bytes,1,opt,name=phase"`

    // Unique, one-word, CamelCase reason for the phase's last transition.
    // +optional
    Reason string `json:"reason,omitempty" protobuf:"bytes,2,opt,name=reason"`

    // Human-readable message indicating details about last transition.
    // +optional
    Message string `json:"message,omitempty" protobuf:"bytes,3,opt,name=message"`
}

// JobStatus represents the current status of a Job
type JobStatus struct {
    // Current state of Job.
    State JobState `json:"state,omitempty" protobuf:"bytes,1,opt,name=state"`

    // The number of pending pods.
    // +optional
    Pending int32 `json:"pending,omitempty" protobuf:"bytes,2,opt,name=pending"`

    // The number of running pods.
    // +optional
    Running int32 `json:"running,omitempty" protobuf:"bytes,3,opt,name=running"`

    // The number of pods which reached phase Succeeded.
    // +optional
    Succeeded int32 `json:"Succeeded,omitempty" protobuf:"bytes,4,opt,name=succeeded"`

    // The number of pods which reached phase Failed.
    // +optional
    Failed int32 `json:"failed,omitempty" protobuf:"bytes,5,opt,name=failed"`

    // The minimal available pods to run for this Job
    // +optional
    MinAvailable int32 `json:"minAvailable,omitempty" protobuf:"bytes,6,opt,name=minAvailable"`

    // The number of pods which reached phase Terminating.
    // +optional
    Terminating int32 `json:"terminating,omitempty" protobuf:"bytes,7,opt,name=terminating"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type JobList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

    Items []Job `json:"items" protobuf:"bytes,2,rep,name=items"`
}

```
