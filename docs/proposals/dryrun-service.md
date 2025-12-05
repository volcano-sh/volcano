# Volcano dryrun service
@weapons97 2025/11/19

## Introduction

Target issue: [Feature Request: Allow dry-run scheduling for workloads](https://github.com/volcano-sh/volcano/issues/4738)

Sometimes, when scheduling workloads, we are not certain whether the requested resources can be successfully scheduled. If not, we may need to reduce the resource requirements.
With the dry-run scheduling feature, we can avoid repeated trial and error and enhance the interpretability of scheduling.

This proposal proposes an independent service with scheduling dry-run APIs to address the aforementioned needs.

### Goals

- An independent service for dry-run scheduling

## Proposal

We can implement a service that provides dry-run scheduling APIs.

The API are as follows:

#### Dry-run scheduling
- `POST /api/v1/scheduling/dryrun`

request body like:

- replicas: The number of replicas of the workload.
- queue: The name of the queue where the workload is scheduled.
- template: The template of the pod resources.
  - spec: The spec of the pod resources.
  - affinity: The affinity of the pod resources.
  - containers: The containers of the pod resources.
    - resources: The resources of the container.

example:
```json
{
  "replicas": 1,
  "queue": "queue1",
  "template": {
    "spec": {
      "affinity": {
        "nodeAffinity": {
          "requiredDuringSchedulingIgnoredDuringExecution": {
            "nodeSelectorTerms": [
              {
                "matchExpressions": [
                  {
                    "key": "machine.cluster.vke.volcengine.com/gpu-name2",
                    "operator": "In",
                    "values": [
                      "Tesla-V100"
                    ]
                  }
                ]
              }
            ]
          }
        }
      },
      "containers": [
        {
          "resources": {
            "limits": {
              "nvidia.com/gpu": "1"
            },
            "requests": {
              "nvidia.com/gpu": "1"
            }
          }
        },
        {
          "resources": {
            "limits": {
              "cpu": "1",
              "memory": "1Gi"
            },
            "requests": {
              "cpu": "1",
              "memory": "1Gi"
            }
          }
        }
      ]
    }
  }
}
```

response body like:

- code: Code of the response. 0 means success.
- message: Message of the response.
- data: The scheduling result, containing:
    - nodes: A list of node names where the workload can be scheduled.
    - bad_nodes: An object mapping node names that cannot be scheduled to the reason for the failure.

example:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "nodes": [
      "vgpu-node01",
      "vgpu-node02"
    ],
    "bad_nodes": {
      "k8s-master01": "Insufficient with vcore",
      "k8s-master02": "Insufficient with vmemory",
      "k8s-master03": "Insufficient with cpu"
    }
  }
}
```

#### Scheduling reason

The API is used to query the scheduling reason for a workload.

request path like:

- `GET /api/v1/scheduling/reason/{namespace}/{workload-type}/{name}`

request parameters like:
    - namespace: The namespace of the workload.
    - workload-type: The type of the workload.
    - name: The name of the workload.
response body like:
- code: Code of the response. 0 means success.
- message: Message of the response.
- data: The scheduling result, containing:
    - nodes: A list of node names where the workload can be scheduled.
    - bad_nodes: An object mapping node names that cannot be scheduled to the reason for the failure.

example:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "nodes": [
      "vgpu-node01",
      "vgpu-node02"
    ],
    "bad_nodes": {
      "k8s-master01": "Insufficient with vcore",
      "k8s-master02": "Insufficient with vmemory",
      "k8s-master03": "Insufficient with cpu"
    }
  }
}
```
example of failed response:
```json
{
  "code": 1,
  "message": "can not find workload job1 in namespace ns1"
}
```


### Advantages

- It shall not interfere with the scheduler's existing logic.
- It can be used independently.

### Disadvantages

- The scheduling results may not be consistent with those of the scheduler.
