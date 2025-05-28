# HyperNode Auto Discovery Design Document

## Introduction

This design document describes the design and implementation of the HyperNode network topology discovery feature in Volcano. This feature automatically discovers the network topology structure within the cluster and creates and maintains HyperNode custom resources (CRs) based on the discovered topology information. Consequently, the Volcano Scheduler will leverage these HyperNode CRs for scheduling decisions, eliminating the need for users to manually maintain HyperNode information.

## Design Goals

*   **Automated Discovery**: Automatically discover network topology information from different data sources (such as UFM, RoCE, etc.).
*   **Scalability**: Support multiple network topology discovery sources and easily extend new discovery methods.
*   **Real-time**: Be able to reflect changes in network topology in a timely manner.
*   **Fault Tolerance**: When a discovery source fails, it does not affect the normal operation of the system.
*   **Security**: Securely manage authentication credentials using Kubernetes Secrets.

## Overall Design

### Component Architecture

The entire network topology discovery function consists of the following core components:

*   **Config Loader**: Responsible for loading network topology discovery configuration information from ConfigMap.
*   **Discovery Manager**: Responsible for managing and coordinating various network topology discoverers.
*   **Discoverer**: A specific network topology discoverer, responsible for obtaining network topology information from a specific data source and converting it into `HyperNode` resources.
*   **HyperNode Controller**: Responsible for listening to changes in `HyperNode` resources and creating, updating, or deleting `HyperNode` resources based on the discovered topology information.

### Process Flow

```
ConfigMap -> Config Loader -> Discovery Manager -> Discoverer -> HyperNode Controller -> HyperNode
```

1.  **Configuration Loading**: `Config Loader` loads network topology discovery configuration information from ConfigMap, including enabled discovery sources, discovery intervals, data source addresses, etc.
2.  **Discovery Management**: `Discovery Manager` creates and starts the corresponding `Discoverer` based on the configuration information.
3.  **Topology Discovery**: `Discoverer` obtains network topology information from the specified data source and converts the topology information into `HyperNode` resources.
4.  **Resource Synchronization**: `HyperNode Controller` receives the `HyperNode` resources discovered by `Discoverer`, compares them with the existing `HyperNode` resources, and then creates, updates, or deletes `HyperNode` resources to keep the `HyperNode` resources in the cluster consistent with the actual network topology.

## Detailed Design

### Config Loader

`Config Loader` is responsible for loading network topology discovery configuration information from ConfigMap.

*   **Function**:
    *   Read configuration information from the specified ConfigMap.
    *   Parse configuration information and convert it into a `NetworkTopologyConfig` object.
*   **Configuration Format**:

```yaml
# volcano-controller.conf file in ConfigMap
networkTopologyDiscovery:
  - source: ufm
    enabled: true
    interval: 10m
    credentials:
      secretRef:
        name: ufm-credentials
        namespace: volcano-system
    config:
      [...]
  - source: roce
    enabled: false
    interval: 15m
    config:
      [...]
  - source: label
    enabled: false
    config:
      # Configuration of the label discovery source
```

*   **Code Example**:

```go
// NetworkTopologyConfig represents the configuration of the network topology
type NetworkTopologyConfig struct {
    // NetworkTopologyDiscovery specifies the network topology to discover,
    // Each discovery source has its own specific configuration
	NetworkTopologyDiscovery []DiscoveryConfig `json:"networkTopologyDiscovery" yaml:"networkTopologyDiscovery"`
}

// SecretRef refers to a secret containing sensitive information
type SecretRef struct {
    Name      string `json:"name" yaml:"name"`
    Namespace string `json:"namespace" yaml:"namespace"`
}

// Credentials specifies how to retrieve credentials
type Credentials struct {
    SecretRef *SecretRef `json:"secretRef" yaml:"secretRef"`
}

// DiscoveryConfig contains configuration for a specific discovery source
type DiscoveryConfig struct {
    // Source specifies the discover source (e.g., "ufm", "roce", etc.)
    Source string `json:"source" yaml:"source"`

    // Enabled determines if discovery for this source is active
    Enabled bool `json:"enabled" yaml:"enabled"`

    // Interval is the period between topology discovery operations
    // If not specified, DefaultDiscoveryInterval will be used
    Interval time.Duration `json:"interval" yaml:"interval"`
	
    // Credentials specifies the username/password to access the discovery source
    Credentials *Credentials `json:"credentials" yaml:"credentials"`

    // Config contains specific configuration parameters for each discovery source
    Config map[string]interface{} `json:"config" yaml:"config"`
}
```

### Discovery Manager

`Discovery Manager` is responsible for managing and coordinating various network topology discoverers.

*   **Function**:
    *   Create and start the corresponding `Discoverer` based on the configuration information.
    *   Periodically obtain network topology information from `Discoverer`.
    *   Send network topology information to `HyperNode Controller`.
*   **Key Code**:

```go
// RegisterDiscoverer registers the discoverer constructor for the specified source
func RegisterDiscoverer(source string, constructor DiscovererConstructor, kubeClient clientset.Interface) {
    discovererRegistry[source] = constructor
}
```

### Discoverer

`Discoverer` is a specific network topology discoverer, responsible for obtaining network topology information from a specific data source and converting it into `HyperNode` resources.

*   **Function**:
    *   Retrieve authentication credentials from Kubernetes Secrets.
    *   Obtain network topology information from the specified data source.
    *   Convert network topology information into `HyperNode` resources.
*   **Interface Definition**:

```go
// Discoverer is the interface for network topology discovery
type Discoverer interface {
    // Start begins the discovery process, sending discovered nodes through the provided channel
    Start(outputCh chan<- []*topologyv1alpha1.HyperNode) error

    // Stop halts the discovery process
    Stop() error

    // Name returns the discoverer identifier, this is used for labeling discovered hyperNodes for distinction.
    Name() string
}
```

*   **Implementation**:
    *   **UFM Discoverer**: Obtain network topology information from UFM (Unified Fabric Manager).
    *   **RoCE Discoverer**: Obtain network topology information from RoCE (RDMA over Converged Ethernet) devices.
    *   **Label Discoverer**: Discover network topology information based on the Label on the Node.

*   **Credential Management**:
    *  Credentials are retrieved from Kubernetes Secrets specified in the configuration.
    *  The Secret reference includes both name and namespace parameters.

### HyperNode Controller

`HyperNode Controller` is responsible for listening to changes in `HyperNode` resources and creating, updating, or deleting `HyperNode` resources based on the discovered topology information.

*   **Function**:
    *   Listen to change events of `HyperNode` resources.
    *   Create, update, or delete `HyperNode` resources based on the network topology information provided by `Discoverer`.
*   **Key Code**:

```go
func (hn *hyperNodeController) reconcileTopology(source string, discoveredNodes []*topologyv1alpha1.HyperNode) {
 klog.InfoS("Starting topology reconciliation", "source", source, "discoveredNodeCount", len(discoveredNodes))

 existingNodes, err := hn.hyperNodeLister.List(labels.SelectorFromSet(labels.Set{
  api.NetworkTopologySourceLabelKey: source,
 }))
 if err != nil {
  klog.ErrorS(err, "Failed to list existing HyperNode resources")
  return
 }

 existingNodeMap := make(map[string]*topologyv1alpha1.HyperNode)
 for _, node := range existingNodes {
  existingNodeMap[node.Name] = node
 }

 discoveredNodeMap := make(map[string]*topologyv1alpha1.HyperNode)
 for _, node := range discoveredNodes {
  if node.Labels == nil {
   node.Labels = make(map[string]string)
  }
  node.Labels[api.NetworkTopologySourceLabelKey] = source
  discoveredNodeMap[node.Name] = node
 }

 for name, node := range discoveredNodeMap {
  if _, exists := existingNodeMap[name]; !exists {
   [...]
  } else {
   [...]
  }

  delete(existingNodeMap, name)
 }

 for name := range existingNodeMap {
  klog.InfoS("Deleting HyperNode", "name", name, "source", source)
  if err := utils.DeleteHyperNode(hn.vcClient, name); err != nil {
   klog.ErrorS(err, "Failed to delete HyperNode", "name", name)
  }
 }

 klog.InfoS("Topology reconciliation completed",
  "source", source,
  "discovered", len(discoveredNodes),
  "created/updated", len(discoveredNodeMap),
  "deleted", len(existingNodeMap))
}
```
