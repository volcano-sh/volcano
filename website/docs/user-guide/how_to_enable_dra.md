# How to Enable Dynamic Resource Allocation (DRA) in Volcano Scheduler

This document describes the steps required to enable Dynamic Resource Allocation (DRA) support in the Volcano scheduler.

## Prerequisites
Before proceeding with the configuration steps, ensure your cluster meets the following prerequisites:

### Configure Cluster Nodes (Containerd)
For nodes running containerd as the container runtime, you must enable the Container Device Interface (CDI) feature. 
This is crucial for containerd to properly interact with DRA drivers and inject dynamic resources into Pods.

Modify the containerd configuration file on each node (typically /etc/containerd/config.toml) to ensure the following setting is present:
```toml
# Enable CDI as described in
# https://tags.cncf.io/container-device-interface#containerd-configuration
[plugins."io.containerd.grpc.v1.cri"]
  enable_cdi = true
  cdi_spec_dirs = ["/etc/cdi", "/var/run/cdi"]
```
After modifying the configuration, restart the containerd service on each node for the changes to take effect. For example: `sudo systemctl restart containerd`

> If you are using other container runtimes, please refer to: [how-to-configure-cdi](https://github.com/cncf-tags/container-device-interface?tab=readme-ov-file#how-to-configure-cdi)

## 1. Configure Kube-apiserver
DRA-related APIs are k8s built-in resources instead of CRD resources, and these resources are not registered by default in v1.32, 
so you need to set the startup parameters of kube-apiserver to manually register DRA-related APIs, add or ensure the following flag is present in your kube-apiserver manifest or configuration:
```yaml
--runtime-config=resource.k8s.io/v1beta1=true
```

## 2. Install Volcano With DRA feature gates enabled
When installing Volcano, you need to enable the DRA related feature gates, e.g., `DynamicResourceAllocation` must be enabled when you need to use DRA, 
you can also choose to enable the `DRAAdminAccess` feature gate to manage devices as your need.

When you are using helm to install Volcano, you can use following command to install Volcano with DRA feature gates enabled:
```bash
helm install volcano volcano/volcano --namespace volcano-system --create-namespace \
  --set custom.scheduler_feature_gates="DynamicResourceAllocation=true" \
  # Add other necessary Helm values for your installation
```

When you directly use `kubectl apply -f` to install Volcano, you need to add or ensure the following flag is present in your volcano-scheduler manifest:
```yaml
--feature-gates=DynamicResourceAllocation=true
```

## 3. Configure Volcano Scheduler Plugins
After installing Volcano, you need to configure the Volcano scheduler's plugin configuration to enable the DRA plugin within the predicates plugin arguments.

Locate your Volcano scheduler configuration (A ConfigMap contains the configuration). Find the predicates plugin configuration and add or modify its arguments to enable DRA plugin.

An example snippet of the scheduler configuration (within the volcano-scheduler.conf key of the ConfigMap) might look like this:
```yaml
actions: "enqueue, allocate, backfill"
tiers:
- plugins:
  - name: priority
  - name: gang
- plugins:
  - name: drf
  - name: predicates
    arguments:
      predicate.DynamicResourceAllocationEnable: true
  - name: proportion
  - name: nodeorder
  - name: binpack
```

## 4. Deploy a DRA Driver
To utilize Dynamic Resource Allocation, you need to deploy a DRA driver in your cluster. The driver is responsible for managing the lifecycle of dynamic resources.
For example, you can refer to the [kubernetes-sigs/dra-example-driver](https://github.com/kubernetes-sigs/dra-example-driver) to deploy a example DRA driver for testing.

For some DRA Drivers which have already been used in actual production, you can refer to:
- [NVIDIA/k8s-dra-driver-gpu](https://github.com/NVIDIA/k8s-dra-driver-gpu)
- [intel/intel-resource-drivers-for-kubernetes](https://github.com/intel/intel-resource-drivers-for-kubernetes)

