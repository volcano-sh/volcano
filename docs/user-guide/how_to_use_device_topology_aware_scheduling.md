# Device Topology-Aware Scheduling User Guide

## Introduction

Accelerators on one Node are often split into groups. GPUs in the same group are connected by a fast link, such as NVLink, while GPUs in different groups talk over a slower path, such as PCIe. A training Pod that needs 2 GPUs runs faster when both GPUs are in the same group.

Device topology-aware scheduling lets a workload ask Volcano to place each Pod's devices inside one such group. Volcano calls this group a **local domain**. The workload sets a policy, and the `device-topology-aware` scheduler plugin uses a per-Node device inventory to choose Nodes, and, for a hard policy, the exact devices.

This feature is **Alpha** and is disabled by default.

> **Important:** In this release, a hard policy needs an allocation adapter, and the only adapter is `mock`. The mock adapter records the chosen device IDs in scheduler memory only. It does not tell the kubelet or the device plugin which devices to use, so on a real Node the Pod may still get different devices. Use hard policies for simulation and testing, such as with KWOK. A soft policy needs no adapter.

### Concepts

| Term | Meaning |
|---|---|
| Local domain | A group of devices of one resource on one Node, such as GPUs joined by NVLink. |
| Fabric | A group of local domains on several Nodes that a device interconnect joins, for example a rack-scale NVLink fabric. One Node's inventory declares it (see [Declare a fabric](#declare-a-fabric)). |
| Provider | The source of the device inventory. This release has one: the `annotation` provider, which reads an annotation on each Node. |
| Allocation adapter | The component that reserves the exact devices a hard policy chose. This release has one: `mock`. |
| Hard | The placement is required. A Pod is placed only where its devices fit in one local domain, and Volcano reserves the exact devices. |
| Soft | The placement is preferred. Volcano gives a small score to Nodes where the devices fit in one local domain, but any Node that fits the normal resource request can be used. |

## Environment setup

### Prerequisites

- Volcano is installed.
- Each Node with accelerators reports them as an extended resource, such as `nvidia.com/gpu`, in its allocatable resources.
- A trusted component can write the device inventory annotation on those Nodes (see [Publish the device inventory](#publish-the-device-inventory)).

### Enable the feature gate

The `DeviceTopologyAwareScheduling` feature gate must be enabled on the **scheduler** and on the **admission webhook**. The webhook rejects every device topology policy while the gate is off.

With Helm:

```bash
helm install volcano volcano/volcano --namespace volcano-system --create-namespace \
  --set custom.scheduler_feature_gates="DeviceTopologyAwareScheduling=true" \
  --set custom.admission_feature_gates="DeviceTopologyAwareScheduling=true"
```

For an existing deployment, add this argument to both the `volcano-scheduler` and `volcano-admission` containers:

```yaml
--feature-gates=DeviceTopologyAwareScheduling=true
```

### Configure the scheduler

Add the `device-topology-aware` plugin to a tier, and add a top-level `deviceTopology` section to the scheduler configuration (the `volcano-scheduler-configmap` ConfigMap):

```yaml
actions: "enqueue, allocate, backfill"
tiers:
- plugins:
  - name: priority
  - name: gang
  - name: conformance
- plugins:
  - name: drf
  - name: predicates
  - name: proportion
  - name: nodeorder
  - name: device-topology-aware
deviceTopology:
  providers:
  - id: node-annotation
    type: annotation
    identityNamespace: gpu-v1
    resources:
    - nvidia.com/gpu
  adapters:
  - type: mock
    provider: node-annotation
    resource: nvidia.com/gpu
```

| Field | Required | Meaning |
|---|---|---|
| `providers[].id` | Yes | A unique name for the provider. |
| `providers[].type` | Yes | Must be `annotation`. |
| `providers[].identityNamespace` | Yes | A stable name that Volcano adds to every device ID from this provider. Changing it makes every device look new. |
| `providers[].resources` | Yes | The extended resources this provider describes. One resource can belong to only one provider. |
| `providers[].maxAge` | No | How long an inventory stays fresh after its `observedAt`, such as `2m` or `10m`. The default is `2m`. The publisher must refresh the annotation within this time. |
| `adapters[].type` | Yes | Must be `mock`. |
| `adapters[].provider` | Yes | The `id` of the provider that publishes the resource. |
| `adapters[].resource` | Yes | One resource from that provider's `resources`. |

The `adapters` section is optional. Without an adapter, soft policies work, and every hard policy is rejected.

The scheduler reads `deviceTopology` only when it starts. After you change this section, restart the scheduler. The scheduler fails to start when the section is invalid, for example when a resource belongs to two providers or an adapter names an unknown provider.

### Publish the device inventory

The annotation provider reads the `topology.volcano.sh/device-inventory` annotation on each Node. The value is a JSON document:

```yaml
apiVersion: v1
kind: Node
metadata:
  name: gpu-node-0
  annotations:
    topology.volcano.sh/device-inventory: |
      {
        "apiVersion": "topology.volcano.sh/v1alpha1",
        "generation": 1,
        "observedAt": "2026-11-12T08:00:00Z",
        "freshUntil": "2026-11-12T08:02:00Z",
        "devices": [
          {"id": "gpu-0", "resourceName": "nvidia.com/gpu", "localDomain": "nvlink-0", "health": "Healthy"},
          {"id": "gpu-1", "resourceName": "nvidia.com/gpu", "localDomain": "nvlink-0", "health": "Healthy"},
          {"id": "gpu-2", "resourceName": "nvidia.com/gpu", "localDomain": "nvlink-1", "health": "Healthy"},
          {"id": "gpu-3", "resourceName": "nvidia.com/gpu", "localDomain": "nvlink-1", "health": "Healthy"}
        ],
        "localDomains": [
          {"id": "nvlink-0", "devices": ["gpu-0", "gpu-1"]},
          {"id": "nvlink-1", "devices": ["gpu-2", "gpu-3"]}
        ]
      }
```

| Field | Meaning |
|---|---|
| `apiVersion` | Must be `topology.volcano.sh/v1alpha1`. |
| `generation` | A positive number. Increase it every time the content changes. |
| `observedAt` | When the publisher read the inventory, in RFC 3339 format. |
| `freshUntil` | Optional. When the publisher stops trusting the inventory. It must not be earlier than `observedAt`. |
| `devices[]` | One entry per device: a device `id` unique on the Node, the `resourceName`, the `localDomain` it belongs to, and `health` (`Healthy` or `Unhealthy`). |
| `localDomains[]` | One entry per local domain: an `id` unique on the Node, and the IDs of its devices. A local domain holds devices of only one resource. |

Volcano rejects an inventory, and keeps the last valid one, when any of these rules is broken:

- Every device appears in exactly one local domain, and that domain matches the device's `localDomain`.
- The Node's allocatable count of each resource equals the number of its `Healthy` devices in the inventory.
- A new inventory has a higher `generation`, or the same `generation` with the same content.
- `observedAt` is at most one minute ahead of the scheduler's clock.
- Every fabric follows the rules in [Declare a fabric](#declare-a-fabric).

**Refresh the annotation regularly.** An inventory stays fresh until its `observedAt` plus the provider's `maxAge`, or until its `freshUntil` when that is earlier. After that, Volcano treats it as stale. A hard policy never uses stale data, so its Pods stay pending with `DeviceTopologyStale`. A soft policy just loses its preference. Refresh at least every half `maxAge`, for example every minute with the default `2m`. To refresh, write the same inventory with a new `observedAt`, and a later `freshUntil` if you set one, and keep the same `generation`. Increase `generation` only when the devices, domains, health, or fabrics change. A higher `generation` on a fabric member keeps the fabric only when the member's local domains in the fabric still hold the same devices (see [Declare a fabric](#declare-a-fabric)).

The inventory is trusted input, because it decides where Pods run. Only the component that publishes it should be able to update Nodes. Do not give workload users or application ServiceAccounts permission to patch Nodes.

### Declare a fabric

A fabric joins local domains on several Nodes, for example the NVLink domains of the Nodes in one rack. One Node, the **owner**, declares the fabric in its inventory. The other **member** Nodes publish their local domains as usual. This example adds a fabric to the inventory of `gpu-node-0`:

```json
"fabrics": [
  {
    "id": "rack-1/nvlink-fabric-0",
    "ownerNode": "gpu-node-0",
    "members": [
      {"node": "gpu-node-0", "localDomains": ["nvlink-0", "nvlink-1"]},
      {"node": "gpu-node-1", "localDomains": ["nvlink-0"]}
    ],
    "interconnect": "NVLink"
  }
]
```

| Field | Meaning |
|---|---|
| `id` | The fabric ID, unique in the cluster. |
| `ownerNode` | The Node that declares the fabric. It must be the Node that carries this annotation. |
| `members[]` | At least two Nodes, each with the IDs of its local domains in the fabric. The owner must be one of them. |
| `interconnect` | Optional. A label such as `NVLink`, for people only. |

Volcano uses a fabric only when it is valid:

- Every member Node has a valid inventory that contains the listed local domains. Until then, Volcano waits, so the Nodes can publish their inventories in any order.
- Only one Node declares a fabric ID. If a second Node declares the same ID, Volcano rejects that Node's whole inventory and keeps its last valid one.
- After the fabric is valid, it stays tied to the member Nodes and to the devices in their listed local domains at that time. A member Node that is deleted or replaced, or whose listed local domain is removed or holds different devices, makes the fabric unavailable. The owner must publish its inventory again with a higher `generation` to declare the fabric again. Other member changes, such as a device's health, keep the fabric, and Volcano just skips the unhealthy device.
- Do not declare a fabric for Nodes that only share an Ethernet, InfiniBand, or RoCE network. A fabric means the devices are joined by a device interconnect.

### Enable the Pod admission webhook (recommended)

If workloads use the Pod annotation form (see [Plain Pods, Deployments, and StatefulSets](#plain-pods-deployments-and-statefulsets)), enable the `/pods/validate` admission webhook. Volcano does not enable it by default. With it, a Pod with an invalid annotation is rejected when it is created, with a clear error. Without it, such a Pod is created but stays Pending (see [Troubleshooting](#troubleshooting)).

With Helm, add `/pods/validate` to `custom.enabled_admissions`. Keep the webhooks you already use in the list:

```bash
--set custom.enabled_admissions="/jobs/mutate,/jobs/validate,/podgroups/validate,/pods/validate,/queues/mutate,/queues/validate,/hypernodes/validate,/cronjobs/validate"
```

## Usage

A policy has one or more **requirements** and an optional **fabric** rule:

```yaml
deviceTopology:
  requirements:
  - resource:
      extendedResourceName: nvidia.com/gpu
    mode: hard
```

| Field | Meaning |
|---|---|
| `requirements[].resource.extendedResourceName` | Required. The device resource the requirement applies to. CPU and memory are not allowed. |
| `requirements[].mode` | `hard` or `soft`. |
| `fabric.mode` | `hard`: all Pods of the policy unit get local domains in one fabric, so a gang can span Nodes. `soft`: Volcano prefers such a placement. A policy with `fabric` must have exactly one requirement. |

A requirement applies to every Pod in the policy unit that requests its resource. Pods that do not request the resource are scheduled as usual. Each such Pod must request a whole number of devices from exactly one regular container. Requests from init containers are not supported.

### Volcano Job

Set `spec.deviceTopology`. The policy covers the whole Job:

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: local-domain-training
spec:
  schedulerName: volcano
  minAvailable: 2
  deviceTopology:
    requirements:
    - resource:
        extendedResourceName: nvidia.com/gpu
      mode: hard
  tasks:
  - name: worker
    replicas: 2
    template:
      spec:
        containers:
        - name: trainer
          image: example/trainer:latest
          resources:
            requests:
              nvidia.com/gpu: 2
            limits:
              nvidia.com/gpu: 2
```

Each worker gets 2 GPUs from one local domain. Because the Job is a gang (`minAvailable: 2`), Volcano places both workers only when there is a complete plan for both.

### Task partition policy

To give each partition of a task its own policy, set `deviceTopology` in the task's `partitionPolicy` instead. A Job cannot set both a Job-level policy and a partition policy.

```yaml
  tasks:
  - name: worker
    replicas: 4
    partitionPolicy:
      totalPartitions: 2
      partitionSize: 2
      deviceTopology:
        requirements:
        - resource:
            extendedResourceName: nvidia.com/gpu
          mode: hard
```

### PodGroup

When you create a PodGroup yourself, set the same field in `spec.deviceTopology`:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: PodGroup
metadata:
  name: training-group
spec:
  minMember: 2
  deviceTopology:
    requirements:
    - resource:
        extendedResourceName: nvidia.com/gpu
      mode: hard
```

### Plain Pods, Deployments, and StatefulSets

For workloads that are not Volcano Jobs, put the policy as JSON in the `volcano.sh/device-topology` annotation of the Pod, or of the Pod template. The Pod must use `schedulerName: volcano`. The Volcano controller copies the policy into the PodGroup it creates for the workload.

To make a Deployment or StatefulSet a gang, put the `scheduling.volcano.sh/group-min-member` annotation on the Deployment or StatefulSet itself, not on its Pod template. The controller reads it from the Pod's owner, and a Deployment's annotations are copied to its ReplicaSets. This needs the controller option `--inherit-owner-annotations`, which is on by default.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: local-domain-serving
  annotations:
    scheduling.volcano.sh/group-min-member: "2"
spec:
  replicas: 2
  selector:
    matchLabels:
      app: local-domain-serving
  template:
    metadata:
      labels:
        app: local-domain-serving
      annotations:
        volcano.sh/device-topology: '{"requirements":[{"resource":{"extendedResourceName":"nvidia.com/gpu"},"mode":"hard"}]}'
    spec:
      schedulerName: volcano
      containers:
      - name: server
        image: example/server:latest
        resources:
          requests:
            nvidia.com/gpu: 1
          limits:
            nvidia.com/gpu: 1
```

### Changing a policy

A policy cannot be changed after it is created. Volcano rejects an update that changes the policy of a Job or a PodGroup. To change it:

| Workload | How to change the policy |
|---|---|
| Volcano Job | Create a new Job. |
| PodGroup | Create a new PodGroup. |
| Deployment | Update the annotation in the Pod template. The rollout creates a new ReplicaSet, which gets a new PodGroup with the new policy. |
| StatefulSet | Recreate the StatefulSet. A StatefulSet keeps one PodGroup for its whole life, so an updated Pod template keeps the old policy. |

### Verify the scheduling result

Check the policy on the PodGroup:

```bash
kubectl get podgroup <name> -o jsonpath='{.spec.deviceTopology}'
```

When a Pod stays Pending, read the reason from its `PodScheduled` condition:

```bash
kubectl get pod <name> -o jsonpath='{.status.conditions[?(@.type=="PodScheduled")].message}'
```

The message counts the reasons across Nodes, for example `Node: 0/2 nodes are unavailable: 1 DeviceTopologyDataNotReady, 1 DeviceTopologyReservationConflict.` See [Troubleshooting](#troubleshooting) for what each reason means.

## Supported scope and limitations

| Area | Supported in this release | Not supported in this release |
|---|---|---|
| Devices | Whole devices of extended resources | Shared or fractional devices, CPU and memory topology |
| Inventory source | The Node annotation provider | Topology CRDs, device plugin, and DRA `ResourceSlice` sources |
| Allocation adapter | `mock`, for simulation only | Adapters that enforce the chosen devices on the Node |
| Fabric | Fabrics that one Node's inventory declares | Fabrics found automatically from HyperNodes or the network |
| Pod requests | One regular container per Pod requests the resource | Init container requests, more than one container per Pod |
| Policy scope | Job, task partition, PodGroup, Pod annotation | A Job-level policy together with a partition policy |
| Updates | Create-only policies | Changing a policy in place |
| DRA | — | Selecting devices through DRA resource claims |

Other behavior to know:

- A soft policy only adds a small score. Other scoring plugins can outweigh it.
- After the scheduler restarts, hard placement on a resource waits until the adapter reports what it holds, and Pods may briefly show `DeviceTopologyDataNotReady`. The mock adapter forgets its reservations when the scheduler restarts.
- A hard fabric rule needs all Pods of the policy unit to be placed together. While some Pods of the unit run, a new Pod of the same unit, such as a replacement for a failed Pod, stays Pending with `DeviceTopologyFabricUnavailable`. For a Job with a hard fabric rule, set `minAvailable` to the number of replicas.

## Troubleshooting

| Reason in the Pod message | Meaning | What to check |
|---|---|---|
| `DeviceTopologyDataNotReady` | The Node has no valid inventory for the resource, or the scheduler is still recovering after a restart. | That the Node has the annotation, and that the scheduler log shows no inventory error for the Node. |
| `DeviceTopologyStale` | The inventory is older than the provider's `maxAge`, or its `freshUntil` has passed. | That the publisher refreshes the annotation more often than `maxAge`, and that the clocks of the publisher and the scheduler agree. |
| `DeviceTopologyDeviceUnhealthy` | The Node has no healthy device of the resource. | The `health` values in the inventory. |
| `DeviceTopologyDomainFragmented` | No single local domain has enough healthy devices for the Pod, the other Pods of the same gang need the domains that would fit, or no complete plan exists for the whole gang. | The Pod's device count compared with the domain sizes. For a gang, whether every Pod can get its own domain at the same time. |
| `DeviceTopologyReservationConflict` | A local domain is big enough, but some of its devices are held by other Pods. | Whether other Pods release devices soon. |
| `DeviceTopologyFabricUnavailable` | The policy has a hard fabric rule, and no valid fabric has a fitting local domain on the Node. It also appears when other Pods of the same policy unit already run (see [limitations](#supported-scope-and-limitations)). | That the owner Node's inventory declares the fabric, and that every member Node publishes the listed local domains. If a member Node was replaced or changed the devices in a listed local domain, publish the owner's inventory again with a higher `generation`. |
| `DeviceTopologyInvalidPolicy` | The policy cannot be used, for example a requirement that no Pod requests. | The policy and the Pods' resource requests. |
| `DeviceTopologyUnsupportedPodRequest` | A Pod requests the resource in a way this release does not support. | That the resource is requested as a whole number from one regular container, not from an init container. |
| `DeviceTopologyPlanningBudgetExceeded` | The gang is too large for Volcano to search all placements. | Use a smaller gang, or a partition policy to split it. |
| `Node: prepare transactional batch bind: ...` | Every Pod fits on its own, but the exact devices could not be reserved. The rest of the message gives the cause. For example, `no device topology allocation adapter registry is configured` means a hard policy is used without an adapter. | The `adapters` section of the scheduler configuration. |

If a Pod that uses the `volcano.sh/device-topology` annotation stays Pending and has no `scheduling.k8s.io/group-name` annotation, its policy annotation is invalid, and the Pod webhook is not enabled. Volcano creates no PodGroup for such a Pod, so the scheduler never places it without its policy. The `volcano-controllers` log shows the error. Fix the annotation and recreate the Pod.
