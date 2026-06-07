---
title: Dedicated Volumes
authors:
  - "@hzxuzhonghu"
reviewers:
  - "@k82cn"
  - "@zrss"
approvers:
  - "@k82cn"
creation-date: 2019-11-21
last-updated: 2019-11-21
status: review
---

# Dedicated Volumes

## Summary

The purpose of this is to allow mount dedicated volumes per pod of a Volcano Job.

## Motivation

Volume mount was supported from begin. But there are few limitations:

1. We can specify volumes of Job scope, all the volumes are shared by all pods within a Job.

2. We specify volumes by setting `TaskSpec.PodTemplateSpec.Volumes`, but similarly they are shared by pods within a task.

But in real world, scenarios like DL/BigData, etc requires high performance. Shared storage has some performance issue,
like io limit, read/write conflicts.

Also some cloud vendors do not support volumes mounting to multiple nodes, it is to prevent data inconsistent.

Given the above issues, we should support pods within volcano mounting volumes exclusively.

## Goals

Job TaskSpec will contain an additional structure called `VolumeClaimTemplates` to control exclusive volumes.

## Proposal

### Implementation Details

#### API Changes

Following changes will be made to the TaskSpec and VolumeSpec for volcano Job.

```go
// TaskSpec specifies the task specification of Job
type TaskSpec struct {
    ...

	// The volumes mount on pods of the Task
    // Depends on the `VolumeSpec.GenerateName`, they can be dedicated or shared.
    // If `GenerateName` is specified and `VolumeClaimName` is not, the Job controller will generate a dedicated PVC for each pod.
	Volumes []VolumeSpec `json:"volumes,omitempty" protobuf:"bytes,3,opt,name=volumes"`
    ...
}
```

```go
// VolumeSpec defines the specification of Volume, e.g. PVC
type VolumeSpec struct {
	// Path within the container at which the volume should be mounted.  Must
	// not contain ':'.
	MountPath string `json:"mountPath" protobuf:"bytes,1,opt,name=mountPath"`

	// defined the PVC name
	VolumeClaimName string `json:"volumeClaimName,omitempty" protobuf:"bytes,2,opt,name=volumeClaimName"`

    // If `VolumeClaimName` is empty, then the job controller will generate a name with `{task_index}` suffixed for each task instance.
    // Note: it can be set for task scoped only.
	GenerateName string `json:"generateName,omitempty" protobuf:"bytes,4,opt,name=generateName"`

	// VolumeClaim defines the PVC used by the VolumeMount.
	VolumeClaim *v1.PersistentVolumeClaimSpec `json:"volumeClaim,omitempty" protobuf:"bytes,3,opt,name=volumeClaim"`
}
```

- By default, this is empty. The task instance will use volumes defined in `JobSpec.Volumes` and `TaskSpec.Template`.

- If `Volumes` are specified, these pvcs are referenced by all the pods of the task.
  If the VolumeSpec specifies the `GenerateName` while the `VolumeClaimName` left empty,  the pvc name is generated with task index suffixed by job controller.
  Otherwise, the explicitly declared pvc will be shared by all pods of a task.

- If the pvcs does not exist, job controller will create them.


#### Implementation

Consider the following demo job:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: demo
spec:
  tasks:
  - name: task
    replicas: 2
    template:
      spec:
        containers:
        - name: ps
          image: ps-img
    volumes:
    - mountPath: /data
      generateName: train-pvc-
      volumeClaim:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 8Gi
```
Job controller creates persistent volume claim from `TaskSpec.Volumes`, the total number of pvcs built here is equal to `TaskSpec.Replicas` * `len(TaskSpec.Volumes)`.

As is known the two pods created by job controller is: `demo-task-0` and `demo-task-1`. The name is combined with job name, task name and pod ordinal.
For the above example, it will create 2 persistent volume claims, one for each pod.
We should make use of the pod ordinal here to generate the pvc name as well, with the format: `<TaskSpec.Volumes[i].GenerateName><task_index>`.
For the above example, the pvc created for pod `demo-task-0` is `train-pvc-0`, and the pvc for `demo-task-1` is `train-pvc-1`.

