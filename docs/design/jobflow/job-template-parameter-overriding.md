# Job Template Parameter Overriding

[@mahdikhashan](https://github.com/mahdikhashan); June 9, 2025

## Summary

This is an enhancement on top of the original design for JobFlow, it will introduce a new field to the FlowSpec that users can reference a JobTemplate by its name and patch one or more parameters.

## Table of Contents

- [Job Template Parameter Overriding](#job-template-parameter-overriding)
  * [Motivation](#motivation)
  * [User Stories](#user-stories)
    + [Story 1](#story-1)
  * [Design Details](#design-details)
    + [JobFlow API changes](#jobflow-api-changes)
    + [Merge Strategy for fields](#merge-strategy-for-fields)
        - [Job level fields](#job-level-fields)
        - [Pod level fields](#pod-level-fields)
        - [Container level fields](#container-level-fields)
    + [Examples](#examples)
        - [Targeting a specific Container name](#targeting-a-specific-container-name)
        - [Adding a Volume](#adding-a-volume)
  * [Test Plan](#test-plan)
    + [Unit Tests for Controller Updates](#unit-tests-for-controller-updates)

## Motivation

Currently, when user reference a JobTemplate inside a Flow, they don't have the ability to patch or override a parameter in place (e.g. change the version of the image or resources allocated to the related pod). This Forces them to define multiple JobTemplates, resulting in reduced reusability and added frustrations.

## User Stories

### Story 1

> As a machine learning engineer, I want to run tasks using different versions of my experiments, each encapsulated in a container. The ability to override the job template within the flow, rather than defining multiple separate job templates, reduces duplication and improves the readability of my task definitions.

## Design Details

To implement this feature, We introduce `patch` field to the flow spec, with it, user can address the spec for the JobTemplate.

### JobFlow API changes

`Flow` gains a new field named `patch`:

```yaml
type Flow struct {
  ...

  // +optional
  Patch *Patch `json:"patch,omitempty"`
}
```

```yaml
type Patch struct {
  // +optional
  Spec v1alpha1.JobSpec  `json:"spec,omitempty"`
}
```

### Merge Strategy for fields

Following tables outlines the merge strategy for updating job, pod and container level specifications. Resources are matched primarily by `name` to ensure precise patching, with unmatched entries added when appropriate.

#### Job level fields

| Field                     | Merge Strategy  | Behavior on Match            | If Not Present in Target |
| ------------------------- | --------------- | ---------------------------- | ------------------------ |
| `schedulerName`           | Replace         | Override with new value      | Add new value            |
| `minAvailable`            | Replace         | Override with new value      | Add new value            |
| `volumes`                 | Merge by `name` | Update matching volumes      | Add new volumes          |
| `tasks`                   | Merge by `name` | Merge `TaskSpec` recursively | Add new tasks            |
| `policies`                | Replace         | Override entire list         | Add new list             |
| `plugins`                 | Merge by key    | Update existing plugin args  | Add new plugin entries   |
| `runningEstimate`         | Replace         | Override with new value      | Add new value            |
| `queue`                   | Replace         | Override with new value      | Add new value            |
| `maxRetry`                | Replace         | Override with new value      | Add new value            |
| `ttlSecondsAfterFinished` | Replace         | Override with new value      | Add new value            |
| `priorityClassName`       | Replace         | Override with new value      | Add new value            |
| `minSuccess`              | Replace         | Override with new value      | Add new value            |
| `networkTopology`         | Replace         | Override with new value      | Add new value            |


#### Pod level fields


| Field        | Merge By         | Behavior on Match      | If Not Matched   |
| -------------- | ---------------- | ---------------------- | ---------------- |
| `containers`   | `name`           | Patch only matched one | Add new container |
| `volumes`      | `name`           | Replace                | Add new volume   |
| `nodeSelector`    | Replace        | Overwrite key-value pairs  | Add new keys          |
| `tolerations`     | By `key`       | Update matching toleration | Add new toleration    |
| `affinity`        | Replace        | Overwrite entire section   | Add if missing        |
| `securityContext` | Replace        | Overwrite entire section   | Add if missing        |
| `restartPolicy`   | Replace        | Overwrite value            | Set if not present    |
| `hostAliases`     | By `ip`        | Update entries by IP       | Add new alias entries |


#### Container level fields

| Field                            | Merge By              | Behavior on Match                | If Not Matched     |
| ---------------------------------- | --------------------- | -------------------------------- | ------------------ |
| `env`                              | `name`                | Replace value                    | Add new variable   |
| `volumeMounts`                     | `mountPath` or `name` | Update or replace                | Add new mount      |
| `resources`                        | —                     | Merge CPU/memory requests/limits | Set or override    |
| `ports`                            | `containerPort`       | Replace or update                | Add new port       |
| `args` / `command`                 | —                     | Replace entirely                 | Set if not present |
| `image`                            | —                     | Replace image                    | Set if not present |
| `livenessProbe` / `readinessProbe` | —                     | Replace entire probe config      | Add if missing     |

### Examples

#### Targeting a specific Container name

Whether there is one container or multiple, we can identify and update a specific container by its `name`. Alternatively, we can add a new container(e.g. such as a sidecar) if needed.

Such example could be:

```yaml
apiVersion: flow.volcano.sh/v1alpha1
kind: JobTemplate
metadata:
  name: ml-task
spec:
  minAvailable: 1
  schedulerName: volcano
  queue: default
  tasks:
    - replicas: 1
      name: "ml-task"
      template:
        metadata:
          name: train-job
        spec:
          containers:
            - name: cnn-mnist-torch
              image: cnn-mnist-torch:latest
              command:
                - python
                - train.py
                - --epoch
                - "3"
              imagePullPolicy: IfNotPresent
              resources:
                requests:
                  cpu: "1"
          restartPolicy: OnFailure
---
apiVersion: flow.volcano.sh/v1alpha1
kind: JobFlow
metadata:
  name: mnist-experiments
  namespace: training
spec:
  jobRetainPolicy: delete  
  flows:
  - name: ml-task
    patch:
      spec:
        tasks:
        - template:
            spec:
              containers:
              - name: cnn-mnist-torch
              - image: "myregistry/cnn-mnist-torch-cuda:v1"
```

#### Adding a Volume

Add the volume if it doesn't already exist by `name`, since the `name` uniquely identifies the volume and is required to correctly reference it in `volumeMounts` within containers.

Such example could be:

```yaml
apiVersion: flow.volcano.sh/v1alpha1
kind: JobTemplate
metadata:
  name: ml-task
spec:
  minAvailable: 1
  schedulerName: volcano
  queue: default
  tasks:
    - replicas: 1
      name: "ml-task"
      template:
        metadata:
          name: train-job
        spec:
          containers:
            - name: cnn-mnist-torch
              image: cnn-mnist-torch:latest
              command:
                - python
                - train.py
                - --epoch
                - "3"
              imagePullPolicy: IfNotPresent
              resources:
                requests:
                  cpu: "1"
          restartPolicy: OnFailure
---
apiVersion: flow.volcano.sh/v1alpha1
kind: JobFlow
metadata:
  name: mnist-experiments
  namespace: training
spec:
  jobRetainPolicy: delete  
  flows:
    - name: ml-task
      patch:
        spec:
          tasks:
            - template:
                spec:
                  volumes:
                    - name: data-volume
                      emptyDir: {}
                  containers:
                    - name: cnn-mnist-torch
                      volumeMounts:
                        - name: data-volume
                          mountPath: /app/data
```

## Test Plan

### Unit Tests for Controller Updates

- Verify that container patches correctly update existing containers by matching their `name` (e.g., image, environment variables).
- Confirm that new containers are added when no matching container `name` is found.
- Ensure environment variables are merged by `name`, with existing variables updated and new ones added seamlessly.
- Validate that `volumes` and `volumeMounts` are properly added or updated by matching name or mountPath.
- Test the controller’s handling of patches referencing nonexistent containers or malformed specifications, ensuring graceful error handling.
- Confirm that the controller manages concurrent patches correctly without causing conflicts or data inconsistencies.
