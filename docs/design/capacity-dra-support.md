# DeviceClass Quota Support in Capacity Plugin

## Background

Volcano capacity plugin already manages queue quotas for CPU, memory, and other standard resources. Kubernetes Dynamic Resource Allocation (DRA) introduces a different resource model based on `DeviceClass`, `ResourceClaim`, and `ResourceSlice`.

For users, the problem is still the same: a queue needs a clear logical quota boundary. A team should be able to say:

- this queue can use at most N DRA devices
- this queue can borrow up to a soft limit
- this queue keeps a minimum guaranteed share

The design goal is to make DRA resources participate in the same queue quota model as existing resources, instead of introducing a parallel quota API just for DRA.

## Design Goals

1. Reuse the existing queue fields `capability`, `deserved`, and `guarantee`
2. Support both whole-device quota and consumable-capacity quota
3. Keep DRA quota semantics aligned with existing capacity plugin behavior
4. Support both direct `ResourceClaim` and `ResourceClaimTemplate` usage
5. Make shared `ResourceClaim` accounting intuitive for users

## Non-Goals

- Managing physical device placement or allocation details
- Replacing Kubernetes DRA scheduling logic
- Defining a new DRA-specific queue CRD structure

## Core Idea

DRA quota is expressed directly in the queue `ResourceList` using reserved key formats.

### Key Formats

| Key Format | Example | Meaning |
|------------|---------|---------|
| `deviceclass/<DeviceClass>` | `deviceclass/gpu.nvidia.com: "8"` | limit by device count |
| `<dim>.deviceclass/<DeviceClass>` | `cores.deviceclass/hami-core-gpu.project-hami.io: "800"` | limit by consumable capacity dimension |

This keeps DRA resources in the same configuration model as `cpu`, `memory`, and device-plugin resources.

## Queue Semantics

The meaning of queue fields does not change for DRA:

| Field | Meaning |
|-------|---------|
| `capability` | hard upper bound |
| `deserved` | soft share that can be borrowed or reclaimed around |
| `guarantee` | minimum protected share |

This means users do not need to learn a second quota model for DRA. They use the same capacity-plugin semantics they already know.

## Supported Usage Patterns

### 1. Whole-Device Quota

Use `deviceclass/<name>` when the queue should be limited by device count.

Example:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: gpu-team
spec:
  reclaimable: true
  capability:
    cpu: "64"
    memory: "256Gi"
    "deviceclass/gpu.nvidia.com": "8"
  deserved:
    "deviceclass/gpu.nvidia.com": "4"
  guarantee:
    "deviceclass/gpu.nvidia.com": "1"
```

### 2. Consumable-Capacity Quota

Use `<dim>.deviceclass/<name>` when the queue should be limited by a consumable dimension such as virtual GPU cores or memory.

Example:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: vgpu-team
spec:
  reclaimable: true
  capability:
    "cores.deviceclass/hami-core-gpu.project-hami.io": "800"
    "memory.deviceclass/hami-core-gpu.project-hami.io": "320Gi"
  deserved:
    "cores.deviceclass/hami-core-gpu.project-hami.io": "400"
  guarantee:
    "cores.deviceclass/hami-core-gpu.project-hami.io": "100"
```

### 3. Mixed Resource Queues

A queue may contain traditional resources, device-plugin resources, whole-device DRA quota, and DRA capacity dimensions at the same time.

Example:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: ml-team
spec:
  reclaimable: true
  capability:
    cpu: "100"
    memory: "200Gi"
    "nvidia.com/gpu": "4"
    "deviceclass/gpu.nvidia.com": "8"
    "cores.deviceclass/hami-core-gpu.project-hami.io": "800"
    "memory.deviceclass/hami-core-gpu.project-hami.io": "320Gi"
```

## How Requests Are Interpreted

From the queue point of view, DRA quota is consumed by `ResourceClaim` requests.

Two user-facing patterns are supported:

- a Pod references a direct `ResourceClaim`
- a Pod references a `ResourceClaimTemplate`, and Kubernetes creates the actual claim for that Pod

In both cases, the queue quota view is the same: Volcano accounts the request against the corresponding `DeviceClass`.

## Shared ResourceClaim Semantics

When multiple Pods reference the same shareable `ResourceClaim`, the quota should be counted once per claim, not once per Pod.

This matches user expectation:

- one logical shared claim should consume one logical share of queue quota
- queue usage should not be inflated just because multiple Pods mount the same shareable claim

## Relationship with Cluster Capacity

Queue quota is a logical scheduling boundary. Physical device availability is still determined by Kubernetes DRA and the installed DRA driver.

In practice:

- Volcano checks whether a queue is allowed to consume more of a given `DeviceClass`
- Kubernetes DRA checks whether a matching physical allocation is possible

These two responsibilities are complementary rather than overlapping.

## Root Queue and Total Resource View

For DRA resources, the cluster-wide total can come from actual cluster state or from an administrator’s explicit queue configuration.

The important user-facing rule is:

- administrators may explicitly configure the root queue for DRA totals when they need a stronger logical boundary or when automatic totals are not sufficient for their environment

## Unsupported or Deferred Scenarios

The first version intentionally keeps a narrow scope. The following scenarios are not the main target of this design:

- `allocationMode: All`
- `FirstAvailable` style prioritized requests
- partitionable-device total accounting that depends on more specialized device semantics

These scenarios may still exist in Kubernetes DRA, but they are not the primary quota-accounting model described here.

## Why This Design

This design keeps the feature understandable and operationally simple:

- no new queue API surface for DRA
- same quota semantics as existing capacity plugin resources
- same user workflow for hard limit, soft sharing, and guarantee
- one configuration model that works for CPU, memory, device plugins, and DRA

That is the main tradeoff: keep the DRA quota experience integrated with existing Volcano queue management rather than building a separate DRA-only quota system.

## References

- [Capacity Plugin Design](./capacity-scheduling.md)
- [Hierarchical Queue on Capacity Plugin](./hierarchical-queue-on-capacity-plugin.md)
- [Kubernetes DRA Documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
- [DeviceClass Quota User Guide](../user-guide/how_to_use_dra_quota.md)
