# Ray Plugin User Guide

## Introduction

**Ray plugin** is designed to optimize the user experience when deploying a ray cluster, it not only allows users to write less yaml, but also supports users to deploy a ray cluster.

## How the Ray Plugin Works

The Ray Plugin will do three things:

* Configure the commands of head and worker nodes in a ray cluster.
* Open three ports used by ray head node (GCS, Ray dashboard and Client server.
* Create a service mapped to the ray head node container ports. (ex, submit a ray job, Access a ray dashboard and client server)

> *Note*
> - This plugin is based on the ray cli (Command Line Interface) and this guide use the [official ray docker image](https://hub.docker.com/r/rayproject/ray).
> - **svc plugin is necessary** when you use the ray plugin.

## Parameters of the Ray Plugin

### Arguments

| ID  | Name             | Type   | Default Value | Required | Description                             | Example                  |
| --- | ---------------- | ------ | ------------- | -------- | --------------------------------------- | ------------------------ |
| 1   | head             | string | head          | No       | Name of Head Task in Volcano Job        | --head=head              |
| 2   | worker           | string | worker        | No       | Name of Worker Task in Volcano Job      | --worker=worker          |
| 3   | headContainer    | string | head          | No       | Name of Main Container in a head task   | --headContainer=head     |
| 4   | workerContainer  | string | worker        | No       | Name of Main Container in a worker task | --workerContainer=worker |
| 5   | port             | string | 6379          | No       | The port to open for the GCS            | --port=6379              |
| 6   | dashboardPort    | string | 8265          | No       | The port to open for the Ray dashboard  | --dashboardPort=8265     |
| 7   | clientServerPort | string | 10001         | No       | The port to open for the client server  | --clientServerPort=10001 |

## Examples

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: ray-cluster-job
spec:
  minAvailable: 3
  schedulerName: volcano
  plugins:
    ray: []
    svc: []
  policies:
    - event: PodEvicted
      action: RestartJob
  queue: default
  tasks:
    - replicas: 1
      name: head
      template:
        spec:
          containers:
            - name: head
              image: rayproject/ray:latest-py311-cpu
              resources: {}
          restartPolicy: OnFailure
    - replicas: 2
      name: worker
      template:
        spec:
          containers:
            - name: worker
              image: rayproject/ray:latest-py311-cpu
              resources: {}
          restartPolicy: OnFailure 

```