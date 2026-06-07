# Pytorch Plugin User Guide

## Introduction

**Pytorch plugin** is designed to optimize the user experience when running pytorch jobs, it not only allows users to write less yaml, but also ensures the normal operation of Pytorch jobs.

## How the Pytorch Plugin Works

The Pytorch Plugin will do the following:

* Open ports used by Pytorch for all containers of the job
* Force open `svc` plugins
* Add some envs such like `MASTER_ADDR`, `MASTER_PORT`, `WORLD_SIZE`, `RANK` which pytorch distributed training needed to containers automatically
* Add an init container to worker pods to wait for the master node to be ready before starting (ensures master starts first)

## Parameters of the Pytorch Plugin

### Arguments

| ID   | Name                 | Type   | Default Value      | Required | Description                                                                          | Example                                |
| ---- | -------------------- | ------ | ------------------ | -------- | ------------------------------------------------------------------------------------ | -------------------------------------- |
| 1    | master               | string | master             | No       | Name of Pytorch master                                                               | --master=master                        |
| 2    | worker               | string | worker             | No       | Name of Pytorch worker                                                               | --worker=worker                        |
| 3    | port                 | int    | 23456              | No       | The port to open for the container                                                   | --port=23456                           |
| 4    | wait-master-enabled  | bool   | false              | No       | Enable init container to wait for master                                             | --wait-master-enabled=true             |
| 5    | wait-master-timeout  | int    | 300                | No       | Timeout in seconds for waiting master (only effective when wait-master-enabled=true) | --wait-master-timeout=600              |
| 6    | wait-master-image    | string | busybox:1.36.1     | No       | Image for wait-for-master init container (only effective when wait-master-enabled=true) | --wait-master-image=busybox:latest  |

## Examples

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: pytorch-job
spec:
  minAvailable: 1
  schedulerName: volcano
  plugins:
    pytorch: [
      "--master=master",
      "--worker=worker",
      "--port=23456",
      "--wait-master-enabled=true",          # Enable init container to wait for master (optional, default: false)
      "--wait-master-timeout=600",           # Timeout in seconds (optional, default: 300)
      "--wait-master-image=busybox:1.36.1"   # Init container image (optional, default: busybox:1.36.1)
    ]
  tasks:
    - replicas: 1
      name: master
      policies:
        - event: TaskCompleted
          action: CompleteJob
      template:
        spec:
          containers:
            - image: gcr.io/kubeflow-ci/pytorch-dist-sendrecv-test:1.0
              imagePullPolicy: IfNotPresent
              name: master
          restartPolicy: OnFailure
    - replicas: 2
      name: worker
      template:
        spec:
          containers:
            - image: gcr.io/kubeflow-ci/pytorch-dist-sendrecv-test:1.0
              imagePullPolicy: IfNotPresent
              name: worker
              workingDir: /home
          restartPolicy: OnFailure
```

## Notes

* The `wait-for-master` init container feature is **disabled by default**. Enable it by setting `--wait-master-enabled=true`
* When enabled, an init container will be added to worker pods to ensure the master is ready before starting workers
* Default init container image is `busybox:1.36.1`, can be customized via `--wait-master-image`
* Workers will wait for the master to become ready with a configurable timeout (default 300 seconds / 5 minutes)
* If the master doesn't become ready within the timeout, the worker pod will fail with an error message
* The init container checks the master's port connectivity using multiple fallback methods:
  1. `nc -z` (netcat) if available
  2. `/dev/tcp` with timeout command if available
  3. `/dev/tcp` direct connection as fallback
* **Note**: The parameters `--wait-master-timeout` and `--wait-master-image` are only effective when `--wait-master-enabled=true`
* **Image Requirements**: The custom image should have at least one of the following:
  * `nc` (netcat) command - recommended, available in busybox, alpine
  * `/dev/tcp` support in shell - available in bash/sh
  * Recommended images: `busybox:1.36.1`, `alpine:latest`, `bash:latest`
* Customization examples:
  * Enable feature: `--wait-master-enabled=true`
  * Custom timeout: `--wait-master-enabled=true --wait-master-timeout=600` (10 minutes)
  * Custom image: `--wait-master-enabled=true --wait-master-image=busybox:latest`
