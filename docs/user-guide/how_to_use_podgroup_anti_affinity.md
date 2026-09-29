# PodGroup Anti-Affinity User Guide

## Introduction

PodGroup anti-affinity places different PodGroups in separate HyperNode domains, such as racks or supernodes. It can be used to isolate workload instances across failure domains.

The `group-topology-affinity` plugin supports two types of rules:

- `required`: matching PodGroups must be placed in different domains.
- `preferred`: matching PodGroups are preferably placed in different domains, but may share a domain when needed.

## Environment setup

### Prerequisites

1. Install Volcano in your Kubernetes cluster. See the [Volcano Installation Guide](../../installer/README.md).
2. Configure the HyperNode topology for the Nodes running your workloads. See [Building Network Topology](how_to_use_network_topology_aware_scheduling.md#32-building-network-topology) or the [HyperNode Auto-Discovery Guide](how_to_use_hypernode_auto_discovery.md).

### Update scheduler ConfigMap

Update the Volcano scheduler configuration:

```shell
kubectl edit configmap -n volcano-system volcano-scheduler-configmap
```

Add `group-topology-affinity` to an existing `tiers[].plugins` list in `volcano-scheduler.conf`. Add `network-topology-aware` as well to use the combined network topology example below. Retain the existing actions and other plugins.

```yaml
- name: group-topology-affinity
  arguments:
    weight: 10
- name: network-topology-aware
  arguments:
    weight: 10
```

The `group-topology-affinity` plugin is not enabled by default. Its gradient and order hooks default to enabled and do not need explicit configuration. The optional `arguments.weight` controls the plugin's contribution to scoring and defaults to `1` for both plugins.

## Usage

### Configure required PodGroup anti-affinity

The following example separates PodGroups labeled `app: model-a` in the `default` namespace at the `supernode` tier. The HyperNode topology must contain that tier and enough domains to satisfy the rules.

Save the following configuration as `podgroup-a.yaml`:

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: PodGroup
metadata:
  name: inference-a
  namespace: default
  labels:
    app: model-a
spec:
  minMember: 2
  queue: default
  topologyAffinity:
    podGroupAntiAffinity:
      required:
      - podGroupSelector:
          matchLabels:
            app: model-a
        topologyTierName: supernode
```

Create `podgroup-b.yaml` with the same configuration, changing only `metadata.name` to `inference-b`. Apply both PodGroups:

```shell
kubectl apply -f podgroup-a.yaml -f podgroup-b.yaml
```

Configure the rule on each PodGroup that needs mutual isolation. `podGroupSelector` matches PodGroup labels, not Pod labels, and excludes the current PodGroup.

Each term must specify exactly one of `topologyTierName` and `topologyTier`, corresponding to HyperNode `spec.tierName` or `spec.tier`. All `required` terms must be satisfied.

### Create member Pods

Set `schedulerName: volcano` and use the `scheduling.k8s.io/group-name` annotation to associate each Pod with its PodGroup. For example, save the following as `pod-a-0.yaml`:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: inference-a-0
  namespace: default
  labels:
    app: model-a
  annotations:
    scheduling.k8s.io/group-name: inference-a
spec:
  schedulerName: volcano
  containers:
  - name: worker
    image: registry.k8s.io/pause:3.9
    resources:
      requests:
        cpu: "100m"
        memory: "64Mi"
```

Create a second Pod named `inference-a-1` in `pod-a-1.yaml`, using the same PodGroup annotation. Create `pod-b-0.yaml` and `pod-b-1.yaml` with Pod names `inference-b-0` and `inference-b-1`, and set their PodGroup annotation to `inference-b`.

```shell
kubectl apply -f pod-a-0.yaml -f pod-a-1.yaml -f pod-b-0.yaml -f pod-b-1.yaml
```

When using a workload controller, configure the policy on its PodGroup and ensure the controller preserves it. The `topologyAffinity` field shown here belongs to the PodGroup API.

### Verify the scheduling result

Check the Nodes selected for the Pods and their HyperNode membership:

```shell
kubectl get pods -n default -l app=model-a -o wide
kubectl get hypernodes
kubectl describe hypernode <hypernode-name>
```

Pods from `inference-a` and `inference-b` should not share a supernode. If there are no domains satisfying the required rules and resource requests, the affected Pods remain Pending.

### Combine anti-affinity with network topology affinity

To also keep all members of each PodGroup within one supernode, add `networkTopology` to the PodGroup specification before creating the workloads:

```yaml
spec:
  networkTopology:
    mode: hard
    highestTierName: supernode
```

Keep the `topologyAffinity` configuration from the required anti-affinity example. Each PodGroup will stay within one supernode, and the two PodGroups will use different supernodes.

### Configure preferred PodGroup anti-affinity

Use `preferred` instead of `required` when separation is desirable but not mandatory:

```yaml
topologyAffinity:
  podGroupAntiAffinity:
    preferred:
    - podGroupSelector:
        matchLabels:
          app: model-a
      topologyTierName: supernode
      weight: 80
```

Each preferred term must set `weight` to a value from `1` to `100`. A larger weight gives the preference more influence, but does not guarantee separation. Required terms must omit `weight`.

### Select PodGroups across namespaces

By default, `podGroupSelector` selects PodGroups in the current namespace. Use `namespaceSelector` to change the scope:

- Omit it to select only the current namespace.
- Set it to `{}` to select all namespaces.
- Provide a label selector to select namespaces by their labels.

The following rule selects PodGroups labeled `app: model-a` in namespaces labeled `environment: production`:

```yaml
topologyAffinity:
  podGroupAntiAffinity:
    required:
    - podGroupSelector:
        matchLabels:
          app: model-a
      namespaceSelector:
        matchLabels:
          environment: production
      topologyTierName: supernode
```

An empty `podGroupSelector: {}` matches all other PodGroups in the selected namespaces.

## Supported scope and limitations

The `allocate` action supports required and preferred PodGroup anti-affinity, namespace selection, and composition with `networkTopology`.

The following scenarios are not supported:

- Required or preferred PodGroup anti-affinity for BestEffort Pods scheduled by backfill. Whole-PodGroup isolation is not guaranteed if any members are placed through backfill.
- SubGroup affinity and anti-affinity (`subGroupAffinity` and `subGroupAntiAffinity`).
- Affinity between different PodGroups.
- Automatic relocation of running Pods after label, policy, or topology changes.
- Evicting another PodGroup solely to clear an anti-affinity conflict.

## Troubleshooting

Check PodGroup and Pod events for scheduling failures:

```shell
kubectl describe podgroup -n default inference-a
kubectl describe pod -n default inference-a-0
```

- If rules have no effect, check the scheduler name, plugin configuration, PodGroup labels, and the limitations above.
- If Pods remain Pending, check the HyperNode tier, Node membership, available resources, and whether matching PodGroups already occupy all eligible domains.
- For cross-namespace rules, check the Namespace labels used by `namespaceSelector`.
