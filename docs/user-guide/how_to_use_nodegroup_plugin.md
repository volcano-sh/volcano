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

### Nodegroup Plugin Strict Configuration

`strict` is a boolean argument that controls scheduling behavior for queues that do not have node group affinity defined.

*   When `strict: false`, tasks from a queue without affinity can be scheduled on nodes that **do not** have the `volcano.sh/nodegroup-name` label.
*   When `strict: true` (the default), tasks are only allowed to be scheduled on nodes that have a `volcano.sh/nodegroup-name` label.

The default value is `true`.

```yaml
# scheduler configuration
actions: "allocate, backfill, preempt, reclaim"
tiers:
- plugins:
  - name: priority
  - name: gang
  - name: conformance
- plugins:
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodegroup
    arguments:
      strict: false
```


### Hierarchical Queue Configuration

#### Enabling Hierarchical Support

To use hierarchical queues with nodegroup plugin, ensure the plugin configuration includes `enableHierarchy: true`:

```yaml
# scheduler configuration
actions: "allocate, backfill, preempt, reclaim"
tiers:
- plugins:
  - name: priority
  - name: gang
  - name: conformance
- plugins:
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodegroup
    enableHierarchy: true
```

#### Creating Queue Hierarchies

**Basic Hierarchy Setup**

1. **Create root queue with nodegroup affinity:**
```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: root
spec:
  weight: 1
  affinity:
    nodeGroupAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
      - production
```

2. **Create child queues that inherit affinity:**
```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: engineering
spec:
  weight: 1
  parent: root
  # No affinity specified - inherits from root

---
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: backend-team
spec:
  weight: 1
  parent: engineering
  # No affinity specified - inherits from root (through engineering)
```

3. **Create child queues with custom affinity:**
```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: frontend-team
spec:
  weight: 1
  parent: engineering
  affinity:
    nodeGroupAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
      - frontend
    # Overrides inherited affinity from root
```

#### Inheritance Behavior

| Scenario | Behavior |
|----------|----------|
| Child queue without affinity, parent has affinity | Inherits parent's affinity |
| Child queue without affinity, parent also without affinity | Inherits from nearest ancestor with affinity |
| Child queue with affinity | Uses its own affinity (no inheritance) |
| Queue without parent specified | Considered child of root queue |

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

## Troubleshooting Hierarchical Queues

### Issue: Tasks not scheduled despite available nodes

**Symptoms:**
- Tasks remain pending even with available nodes in nodegroups
- Error messages about queue affinity requirements

**Possible Causes:**
1. Child queue inheriting restrictive affinity from parent
2. Nodegroup labels not matching inherited affinity rules
3. Circular queue dependencies

**Solutions:**
1. Check inheritance chain:
```bash
kubectl get queue <queue-name> -o yaml
# Verify parent hierarchy and affinity inheritance
```

2. Override restrictive inheritance:
```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: flexible-team
spec:
  parent: restrictive-parent
  affinity:
    nodeGroupAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
      - flexible-nodes
```

### Issue: Unexpected affinity behavior

**Symptoms:**
- Tasks scheduled on unexpected nodegroups
- Inheritance not working as expected

**Debugging Steps:**
1. Enable detailed logging:
```bash
# Check scheduler logs with higher verbosity
kubectl logs -n volcano-system volcano-scheduler-xxx --tail=100 | grep -i nodegroup
```

2. Verify queue configuration:
```bash
# List all queues with their hierarchy
kubectl get queues -o custom-columns=NAME:.metadata.name,PARENT:.spec.parent,WEIGHT:.spec.weight
```

### Issue: Circular queue dependencies

**Symptoms:**
- Queues not building ancestry correctly
- Warning messages about cycle detection

**Solutions:**
1. Review queue hierarchy:
```bash
# Check for circular references
kubectl get queues -o custom-columns=NAME:.metadata.name,PARENT:.spec.parent
```

2. Fix circular dependencies by updating parent references:
```yaml
# Remove or correct circular parent relationships
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: problematic-queue
spec:
  parent: ""  # Remove circular reference
  weight: 1
```
