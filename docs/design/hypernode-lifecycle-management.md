# HyperNode Lifecycle Management

## Background

Currently, Volcano has implemented the capability to schedule workloads based on HyperNodes. A HyperNode represents a network topology performance domain, and the Volcano scheduler selects the optimal HyperNode for a workload based on its hierarchical structure. Workloads within a HyperNode benefit from the highest network communication performance, significantly reducing inter-task distributed communication latency for large-scale distributed AI training and inference jobs, thereby enhancing training and inference efficiency.

HyperNode is an abstraction of network topology domains, capable of representing various hardware and network types, such as GPU, NPU, InfiniBand (IB), RoCE, and NVSwitch networks. Currently, HyperNodes rely on manual creation by cluster administrators. However, this manual maintenance approach is prone to errors and requires deep familiarity with the data center network topology. It also lacks automation. Therefore, an automatic discovery mechanism is needed to manage the entire lifecycle of HyperNodes.

## Solution

### Intree

One option is to provide an automatic HyperNode discovery mechanism directly within the Volcano controller. This would require integration with different hardware vendors and network types. For instance, for InfiniBand (IB) networks, tools like UFM (Unified Fabric Manager) and ibnetdiscover can be used for network topology discovery. For RoCE networks, LLDP (Link Layer Discovery Protocol) can be used to discovery network topology. These tools would automatically construct and maintain HyperNodes. However, this approach has several issues:

- Data centers have diverse hardware and interface types, and network protocols vary widely, making unified abstraction complex.
- Maintenance costs on the Volcano side would be high, and debugging would be challenging.
- Data center hardware often involves sensitive information and security concerns, making most vendors reluctant to expose their code and implementation details to Volcano.

### OutofTree

Given the issues with the Intree approach, we can consider an OutofTree solution. This involves delegating the implementation to data center vendors, with Volcano abstracting away the implementation details. Vendors would implement their own network topology discovery mechanisms based on their hardware and network types, effectively managing the HyperNode lifecycle themselves. Volcano would only handle resource management and scheduling based on HyperNodes. If users require automatic HyperNode lifecycle management, they would need to use the vendor-provided controller alongside Volcano.

For the OutofTree approach, there are two options:

#### Plugin Registration

The Volcano controller provides a basic framework, encapsulating operations like HyperNode creation, update, and deletion. Vendors can mount their implementations to the Volcano controller via a provider model. Each vendor would implement a plugin interface defined by the Volcano controller, maintaining their own plugin independently without invasive changes to the Volcano controller. The plugin would be compiled into a .so file and mounted to the Volcano controller.

The interface is defined as follows:

```go
// Plugin is the interface for the hyperNode provider, vendors should implement this
// and hyperNode controller call the plugin to populate hyperNodes.
type Plugin interface {
	// Name is the name of the plugin.
	Name() string
	// Start starts the plugin.
	Start(eventCh chan<- Event, replyCh <-chan Reply, informer topologyinformerv1alpha1.HyperNodeInformer) error
	// Stop stops the plugin.
	Stop() error
}
```

Key parameters include:

`eventCh chan<- Event`: Vendors should send HyperNode create/update/delete events to this channel, and the Volcano controller will communicate with the API Server to store them.

`replyCh <-chan Reply`: Volcano will reply with errors to vendor providers through this channel if unexpected errors occur during communication with the API Server and retries fail. Providers should handle these errors by resending the event or performing fault-tolerant processing.

The overall workflow is as follows:

![hypernode-lifecycle](images/hypernode-lifecycle.png)

For examples and usage instructions, please refer to: https://github.com/volcano-sh/volcano/pull/4014. This approach has the following pros and cons:

Pros: Non-invasive modifications, easy maintenance; no need to deploy additional components; abstracts away vendor implementation details.

Cons: May lack flexibility; providers must develop and maintain within the defined interface rules.

#### Bypass Volcano Controller

While the plugin approach avoids invasive changes to the Volcano controller, it requires development within the defined interface, which may not meet all provider needs. Providers may find it challenging to implement flexibly based on their actual hardware and network types. Therefore, another option is to fully delegate HyperNode lifecycle management to a vendor-implemented controller. This would require deploying a vendor-provided controller, effectively creating a separate controller dedicated to HyperNode lifecycle management.

Pros: Completely decoupled from Volcano.

Cons: Not constrained by Volcano's interface standards, leading to inconsistent implementations; vendors may not be aware of API changes; requires deploying an additional component.