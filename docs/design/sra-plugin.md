# Scarce Resource Avoidance (SRA) Plugin

## Introduction
In a cluster where GPU nodes and CPU nodes are deployed together, there is a risk that a task submitted to the cluster, which does not require GPU resources, may be scheduled to a GPU node. This can lead to inefficient resource utilization, as a job requiring both CPU and GPU resources might be pending due to insufficient CPU resources on the GPU node. This scenario is not ideal, as it can result in underutilization of resources and potential delays in job execution. Therefore, sra plugin is proposed to avoid nodes with critical resources (such as GPUs) for certain CPU jobs and to improve overall resource utilization.

## Solution

1. In sra plugin, arguments `sra.resources` is provided to configure important resources in the cluster.
2. Based on the significance of different resources, `sra.resources.[ResourceName]` can assign varying weights for resource allocation. A higher weight signifies greater importance, and tasks that do not require this resource will, to the extent possible, be scheduled away from nodes utilizing such resources
3. For all tasks, user can set `sra.resources` and `sra.resources.[ResourceName]` field in `sra` plugin arguments via `volcano-scheduler-configmap` in following format:
      ```yaml
       actions: "enqueue, reclaim, allocate, backfill, preempt"
       tiers:
       - plugins:
           - name: sra
             arguments:
               sra.weight: 2
               sra.resources: nvidia.com/t4, nvidia.com/a10
               sra.resources.nvidia.com/t4: 1
               sra.resources.nvidia.com/a10: 1
      ```

4. `sra` plugin return 1 callback functions: `AddNodeOrderFn`:
   1. Now, we have tasks:

      | Task Name  | Task request resource                                        |
      |------------|--------------------------------------------------------------|
      | cpu-task-0 | `{cpu: 2, memory: 4Gi}`                                      |
      | gpu-task-0 | `{cpu: 2, memory: 4Gi, nvidia.com/t4: 2}`                    |
      | gpu-task-1 | `{cpu: 2, memory: 4Gi, nvidia.com/t4: 1, nvidia.com/a10: 2}` | 
   
   2. Suppose there are 3 nodes available in cluster:

      | Node Name | Resource capacity on node                                       |
      |-----------|-----------------------------------------------------------------|
      | node1     | `{cpu: 32, memory: 64Gi}`                                       | 
      | node2     | `{cpu: 16, memory: 32Gi, nvidia.com/t4: 10}`                    | 
      | node3     | `{cpu: 16, memory: 32Gi, nvidia.com/t4: 5, nvidia.com/a10: 10}` |

    Through the sra plug-in we will get the following results:
   
      | Task       | Node  | Score result (sra) | Reason                                                       |
      |------------|-------|--------------------|--------------------------------------------------------------|
      | cpu-task-0 | node1 | 200                | node resources meet the task request and no scarce resources |
      | cpu-task-0 | node2 | 100                | node resources meet the task request and have t4             |
      | cpu-task-0 | node3 | 0                  | node resources meet the task request and have t4, a10        |
      | gpu-task-0 | node1 | 0                  | node resources don't meet the task requirements              |
      | gpu-task-0 | node2 | 100                | node resources meet the task request and have t4             |
      | gpu-task-0 | node3 | 0                  | node resources meet the task request and have t4, a10        |
      | gpu-task-1 | node1 | 0                  | node resources don't meet the task requirements              |
      | gpu-task-1 | node2 | 0                  | node resources don't meet the task requirements              |
      | gpu-task-1 | node3 | 0                  | node resources meet the task request and have t4, a10        |
