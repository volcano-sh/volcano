# MPI Plugin User Guide

## Introduction

**MPI plugin** is designed to optimize the user experience when running MPI jobs, it not only allows users to write less yaml, but also ensures the normal operation of MPI jobs.

## How the MPI Plugin Works

The MPI plugin will do three things:

* Open ports used by MPI for all containers of the job
* Force open `ssh` and `svc` plugins
* add `MPI_HOST` environment variable for master pod, this environment variable includes the worker's domain name, It is used by the `--host` parameter of `mpiexec`

## Parameters of the MPI Plugin

### Key Points

* If `master` or `worker` is configured, please ensure that the tasks corresponding to their values exist, and the roles of these tasks correspond to the meaning of the parameters
* If `port` is configured, make the port value of `sshd` the same as the value of the parameter.
* If the `gang` plugin is enabled, then make sure that the value of `minAvailable` is **equal** to the number of `replicas of the worker`.

### Arguments

| ID   | Name   | Type   | Default Value | Required | Description                        | Example            |
| ---- | ------ | ------ | ------------- | -------- | ---------------------------------- | ------------------ |
| 1    | master | string | master        | No       | Name of MPI master                 | --master=mpimaster |
| 2    | worker | string | worker        | No       | Name of MPI worker                 | --worker=mpiworker |
| 3    | port   | string | 22            | No       | The port to open for the container | --port=5000        |

## Examples

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: lm-mpi-job
spec:
  minAvailable: 1
  schedulerName: volcano
  plugins:
    mpi: ["--master=mpimaster","--worker=mpiworker","--port=22"]  ## MPI plugin register
  tasks:
    - replicas: 1
      name: mpimaster
      policies:
        - event: TaskCompleted
          action: CompleteJob
      template:
        spec:
          containers:
            - command:
                - /bin/sh
                - -c
                - |
                  mkdir -p /var/run/sshd; /usr/sbin/sshd;
                  mpiexec --allow-run-as-root --host ${MPI_HOST} -np 2 mpi_hello_world;
              image: volcanosh/example-mpi:0.0.1
              name: mpimaster
              workingDir: /home
          restartPolicy: OnFailure
    - replicas: 2
      name: mpiworker
      template:
        spec:
          containers:
            - command:
                - /bin/sh
                - -c
                - |
                  mkdir -p /var/run/sshd; /usr/sbin/sshd -D;
              image: volcanosh/example-mpi:0.0.1
              name: mpiworker
              workingDir: /home
          restartPolicy: OnFailure
```

