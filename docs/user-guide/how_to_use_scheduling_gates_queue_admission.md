# How to Use Scheduling Gates for Queue Admission

This document describes how to enable and use the `SchedulingGatesQueueAdmission` feature to prevent cluster autoscalers (such as Cluster Autoscaler or Karpenter) from triggering unnecessary scale-ups when pods are waiting for Volcano queue capacity.

## Problem

Volcano marks pods as `Unschedulable` for any allocation failure, whether it's due to insufficient cluster resources (where autoscaling is appropriate) or queue capacity limits (where autoscaling is not needed). Cluster autoscalers cannot distinguish between these scenarios, causing unnecessary node scale-ups.

## Solution

This feature uses Kubernetes [`schedulingGates`](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-scheduling-readiness/) to hold pods until the queue has capacity. While gated, pods are invisible to autoscalers and the gate is removed only after the queue capacity check passes. If the pod then can't schedule due to missing nodes, it gets marked `Unschedulable`, and autoscalers can react to add more nodes.

## Prerequisites

> TODO: Validate which release will hold this feature and if proportion also includes this.

- Volcano v1.15+ with the `SchedulingGatesQueueAdmission` feature gate enabled
- The `capacity` plugin configured in the scheduler (the feature's reserved resource tracking is currently implemented in the capacity plugin).

## 1. Enable the Feature Gate

The feature is Alpha and disabled by default. Enable it on both the **scheduler** and **webhook-manager**.

### Using Helm

```bash
helm install volcano volcano/volcano --namespace volcano-system --create-namespace \
  --set custom.scheduler_feature_gates="SchedulingGatesQueueAdmission=true"
```

### Using kubectl apply

Add the following flag to both the `volcano-scheduler` and `volcano-admission` deployments:

```yaml
--feature-gates=SchedulingGatesQueueAdmission=true
```

## 2. Configure the Capacity Plugin

Ensure the `capacity` plugin is enabled in your scheduler configuration. The reserved resource tracking that prevents race conditions between gate removal and pod allocation is implemented in this plugin.

Example scheduler configuration:

```yaml
actions: "enqueue, allocate, backfill"
tiers:
- plugins:
  - name: priority
  - name: gang
- plugins:
  - name: predicates
  - name: capacity
  - name: nodeorder
```

## 3. Opt-in Pods

The feature is opt-in per pod, and you can start using it by adding the following annotation to pods that should use gate-controlled queue admission:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  annotations:
    # Opt-in annotation
    scheduling.volcano.sh/queue-allocation-gate: "true"
spec:
  schedulerName: volcano
  containers:
  - name: worker
    image: nginx
    resources:
      requests:
        cpu: "1"
        memory: "1Gi"
```

When this pod is created:

1. The Volcano webhook injects a `scheduling.volcano.sh/queue-allocation-gate` scheduling gate.
2. The pod stays gated (invisible to autoscalers) until the queue has capacity.
3. Once capacity is available, the scheduler removes the gate.
4. If the pod can be placed on a node, it gets scheduled normally.
5. If no node matches (e.g., needs a specific node type), it gets marked `Unschedulable` to trigger autoscaling.

## 4. Verify the Feature is Working

After creating an opted-in pod, verify the gate was injected by the mutation webhook:

```bash
kubectl get pod my-pod -o jsonpath='{.spec.schedulingGates}'
```

Expected output (while waiting for queue capacity):

```json
[{"name":"scheduling.volcano.sh/queue-allocation-gate"}]
```

Once the queue has capacity and the scheduler removes the gate, the field will be empty:

```bash
kubectl get pod my-pod -o jsonpath='{.spec.schedulingGates}'
# empty output
```

## Interaction with Other Scheduling Gates

If a pod has additional scheduling gates from other controllers (e.g., `example.com/my-gate`), Volcano will not remove its gate until the pod has **only** the Volcano-controlled gate remaining.

## Limitations

- Once a pod's gate is removed, it reserves queue capacity until it is scheduled or deleted. If the pod remains unschedulable (*e.g.*, waiting for the autoscaler to add nodes), it continues to hold queue capacity, potentially blocking other pods. Additionally, the feature currently **does not implement a timeout** for reserved capacity. Operators should be aware that *ungated-but-unschedulable* pods can hold queue capacity indefinitely.

## Related

- [Design document](../../docs/design/scheduling-gates-queue-admission.md)
- [Kubernetes Pod Scheduling Readiness](https://kubernetes.io/docs/concepts/scheduling-eviction/pod-scheduling-readiness/)
