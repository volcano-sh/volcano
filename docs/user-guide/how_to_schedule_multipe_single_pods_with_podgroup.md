# Manage & Schedule multiple single pods with podgroup

## Background
Volcano can manage single pod when the scheduler of pod is volcano, volcano controller will help to create a podgroup for this pod.
But it can not manage and schedule multiple single pods in one pg now. And this missing ability is very useful in spark client mode and so on.
It will also play a role in some scenarios that require multiple single pods to work together (vj cannot be used).

## Key Points
* Single pods need add two annotations, one is "scheduling.k8s.io/group-name", another is "scheduling.volcano.sh/group-min-resources". Please see the following example.
* The controller will select the largest  minresource among single pods of the same "scheduling.k8s.io/group-name" annotation to add or update pg.
* All single pods will be pg's OwnerReference.

## Examples
1. Create three single pods.
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: busybox1
  annotations:
    "scheduling.k8s.io/group-name": "busybox-pg"
    "scheduling.volcano.sh/group-min-resources": "{\"cpu\":\"100m\",\"memory\":\"128Mi\"}"
spec:
  schedulerName: volcano
  containers:
    - name: busybox
      image: busybox:1.28
      command: ['sh', '-c', 'echo "Hello, busybox1!" && sleep 3600']
---
apiVersion: v1
kind: Pod
metadata:
  name: busybox2
  annotations:
    "scheduling.k8s.io/group-name": "busybox-pg"
    "scheduling.volcano.sh/group-min-resources": "{\"cpu\":\"200m\",\"memory\":\"256Mi\"}"
spec:
  schedulerName: volcano
  containers:
    - name: busybox
      image: busybox:1.28
      command: ['sh', '-c', 'echo "Hello, busybox2!" && sleep 3600']
---
apiVersion: v1
kind: Pod
metadata:
  name: busybox3
  annotations:
    "scheduling.k8s.io/group-name": "busybox-pg"
spec:
  schedulerName: volcano
  containers:
    - name: busybox
      image: busybox:1.28
      command: ['sh', '-c', 'echo "Hello, busybox3!" && sleep 3600']
```
2. Kubectl describe pg.
```
Name:         busybox-pg
Namespace:    default
Labels:       <none>
Annotations:  <none>
API Version:  scheduling.volcano.sh/v1beta1
Kind:         PodGroup
Metadata:
....
  Owner References:
    API Version:           v1
    Block Owner Deletion:  true
    Controller:            true
    Kind:                  Pod
    Name:                  busybox1
    UID:                   aebfe6ed-3a4c-48f0-a38b-bc542ed262b4
    API Version:           v1
    Block Owner Deletion:  true
    Controller:            false
    Kind:                  Pod
    Name:                  busybox2
    UID:                   6262515d-1885-442a-b6e1-3878aa26f96a
    API Version:           v1
    Block Owner Deletion:  true
    Controller:            false
    Kind:                  Pod
    Name:                  busybox3
    UID:                   26c217d6-2d30-4282-8959-df0bfca9e07f
....
Spec:
  Min Member:  1
  Min Resources:
    Cpu:     200m
    Memory:  256Mi
  Queue:     default
Status:
....
Events:
  Type    Reason     Age                 From     Message
  ----    ------     ----                ----     -------
  Normal  Scheduled  25s (x25 over 49s)  volcano  pod group is ready
```

## Limitations
* Currently only "scheduling.k8s.io/group-name", "scheduling.volcano.sh/group-min-resources" and "scheduling.volcano.sh/queue-name" are supported.
* Other properties under PodGroup's Spec cannot be injected through pod annotations.