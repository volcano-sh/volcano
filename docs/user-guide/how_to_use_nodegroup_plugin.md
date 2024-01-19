# Nodegroup Plugin User Guide

## Introduction

**Nodegroup plugin** is designed to isolate resources by assigning labels to nodes and set node label affinty on Queue.

## Usage

### assign label to node
Assign label to node, label key is `volcano.sh/nodegroup-name`.
```shell script
kubectl label nodes <nodename> volcano.sh/nodegroup-name=<groupname>
```

### configure queue
Create queue and bind nodegroup to it.

   ```yaml
   apiVersion: scheduling.volcano.sh/v1beta1
   kind: Queue
   metadata:
     name: default
     spec:
       reclaimable: true
       weight: 1
       affinity:            # added field
         nodeGroupAffinity:
           requiredDuringSchedulingIgnoredDuringExecution:
           - <groupname>
           preferredDuringSchedulingIgnoredDuringExecution:
           - <groupname>
         nodeGroupAntiAffinity:
           requiredDuringSchedulingIgnoredDuringExecution:
           - <groupname>
           preferredDuringSchedulingIgnoredDuringExecution:
           - <groupname>
   ```
### submit a vcjob

submit vcjob job-1 to default queue.

```shell script
$ cat <<EOF | kubectl apply -f -
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: job-1
spec:
  minAvailable: 1
  schedulerName: volcano
  queue: default
  policies:
    - event: PodEvicted
      action: RestartJob
  tasks:
    - replicas: 1
      name: nginx
      policies:
      - event: TaskCompleted
        action: CompleteJob
      template:
        spec:
          containers:
            - command:
              - sleep
              - 10m
              image: nginx:latest
              name: nginx
              resources:
                requests:
                  cpu: 1
                limits:
                  cpu: 1
          restartPolicy: Never
EOF
```

### validate queue affinity and antiAffinity rules is effected

Query pod information and verify whether the pod has been scheduled on the correct node. The pod should be scheduled on nodes with
label `nodeGroupAffinity.requiredDuringSchedulingIgnoredDuringExecution` or `nodeGroupAffinity.preferredDuringSchedulingIgnoredDuringExecution`. If not, the pod should be scheduled on nodes with label of `nodeGroupAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution`. Specifically, the pod must not be scheduled on nodes with the label `nodeGroupAntiAffinity.requiredDuringSchedulingIgnoredDuringExecution`.

```shell script
kubectl get po job-1-nginx-0 -o wide 
```

## How the Nodegroup Plugin Works

The nodegroup design document provides the most detailed information about the node group. There are some tips to help avoid certain issues.These tips are based on a four-nodes cluster and vcjob called job-1:

| Node | Label |
| ------ | ------ |
| node1 | groupname1 |
| node2 | groupname2 |
| node3 | groupname3 |
| node4 | groupname4 |

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: job-1
spec:
  minAvailable: 1
  schedulerName: volcano
  queue: default
  policies:
    - event: PodEvicted
      action: RestartJob
  tasks:
    - replicas: 1
      name: nginx
      policies:
      - event: TaskCompleted
        action: CompleteJob
      template:
        spec:
          containers:
            - command:
              - sleep
              - 10m
              image: nginx:latest
              name: nginx
              resources:
                requests:
                  cpu: 1
                limits:
                  cpu: 1
          restartPolicy: Never
```

1. Soft constraints are a subset of hard constraints, including both affinity and anti-affinity. Consider a queue setup as follows:
   ```yaml
   apiVersion: scheduling.volcano.sh/v1beta1
   kind: Queue
   metadata:
     name: default
     spec:
       reclaimable: true
       weight: 1
       affinity:            # added field
         nodeGroupAffinity:
           requiredDuringSchedulingIgnoredDuringExecution:
           - groupname1
           - gropuname2
           preferredDuringSchedulingIgnoredDuringExecution:
           - groupname1
         nodeGroupAntiAffinity:
           requiredDuringSchedulingIgnoredDuringExecution:
           - groupname3
           - gropuname4
           preferredDuringSchedulingIgnoredDuringExecution:
           - groupname3
   ```
   This implies that tasks in the "default" queue will be scheduled on "groupname1" and "groupname2", with a preference for "groupname1" to run first. Tasks are restricted from running on "groupname3" and "groupname4". However, if the resources in other node groups are insufficient, the task can run on "nodegroup3".

2. If soft constraints do not form a subset of hard constraints, the queue configuration is incorrect, leading to tasks running on "groupname2":
    ```yaml
   apiVersion: scheduling.volcano.sh/v1beta1
   kind: Queue
   metadata:
     name: default
     spec:
       reclaimable: true
       weight: 1
       affinity:            # added field
         nodeGroupAffinity:
           requiredDuringSchedulingIgnoredDuringExecution:
           - gropuname2
           preferredDuringSchedulingIgnoredDuringExecution:
           - groupname1
         nodeGroupAntiAffinity:
           requiredDuringSchedulingIgnoredDuringExecution:
           - gropuname4
           preferredDuringSchedulingIgnoredDuringExecution:
           - groupname3
   ```

3. If there is a conflict between nodeGroupAffinity and nodeGroupAntiAffinity, nodeGroupAntiAffinity takes higher priority.
   ```yaml
   apiVersion: scheduling.volcano.sh/v1beta1
   kind: Queue
   metadata:
     name: default
     spec:
       reclaimable: true
       weight: 1
       affinity:            # added field
         nodeGroupAffinity:
           requiredDuringSchedulingIgnoredDuringExecution:
           - groupname1
           - gropuname2
           preferredDuringSchedulingIgnoredDuringExecution:
           - groupname1
         nodeGroupAntiAffinity:
           requiredDuringSchedulingIgnoredDuringExecution:
           - groupname1
           preferredDuringSchedulingIgnoredDuringExecution:
           - gropuname2
   ```
   This implies that tasks in the "default" queue can only run on "groupname2".

4. Generally, tasks run on "groupname1" first because it is a soft constraint. However, the scoring function comprises several plugins, so the task may sometimes run on "groupname2".
   ```yaml
   apiVersion: scheduling.volcano.sh/v1beta1
   kind: Queue
   metadata:
     name: default
     spec:
       reclaimable: true
       weight: 1
       affinity:            # added field
         nodeGroupAffinity:
           requiredDuringSchedulingIgnoredDuringExecution:
           - groupname1
           - gropuname2
           preferredDuringSchedulingIgnoredDuringExecution:
           - groupname1
         nodeGroupAntiAffinity:
           requiredDuringSchedulingIgnoredDuringExecution:
           - groupname3
           - gropuname4
           preferredDuringSchedulingIgnoredDuringExecution:
           - groupname3
   ```
