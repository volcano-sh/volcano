# Volcano support GPU and KunLun chip brief design doc

## Background

Currently Volcano don't support GPU sharing and KunLun chip support, we need to implement it for Baidu to use Volcano in production.

Our company has purchased different types of GPU and KunLun chip machines in various historical periods, which mainly include k40, p40, v100, etc. However, when running deep learning tasks, there are relatively large problems in the normalized GPU/KunLun scheduling performance of heterogeneous machines. We hope to find a model that can solve the problem of heterogeneous GPU machines being mixed in a large pool and capable of training a single The task provides the ability to unify the GPU cards.

## Solution Proposal

### Upper layer API changes

We will use extended resources for customized resource declaration. extended-resources is a stable feature in k8s1.9. By sending a a patch node request to the apiserver, a custom resource type is added for this node, which is used to count the customized resources and configure the corresponding QoS. 

A custom resource expansion method, which reports the name and total number of resources to the API server, and the Scheduler adds and subtracts the resource availability based on the creation and deletion of the resource pod, and then determines whether there is Nodes that meet the resource conditions.

The advantage of this abstraction is that we can arbitrarily combine the resources required for the task. From the upper layer, it is more intuitive and better understood than the machine-based resource model.

ex. for adding/deleting a customized resource for node 10.10.10.10 with resource GPU_P40:

```
// add
curl --header "Content-Type: application/json-patch+json" \
--request PATCH \
--data '[{"op": "add", "path": "/status/capacity/volcano~1gpu_p40_8", "value": "40"}]' \
http://localhost:8080/api/v1/nodes/10.10.10.10/status
//remove
curl --header "Content-Type: application/json-patch+json" \
--request PATCH \
--data '[{"op": "remove", "path": "/status/capacity/volcano~1gpu_p40_8"}]' \
http://localhost:8080/api/v1/nodes/10.10.10.10/status

```

Our pod configuration should look like this: 

```
apiVersion: v1
kind: Pod
metadata:
  name: extended-resource-demo
spec:
  containers:
  - name: extended-resource-demo-ctr
    image: volcano-sh/volcano-gpu-demo-image
    resources:
      requests:
        volcano/gpu_p40_8: 3
        nvidia.com/gpu: 3
        cpu: xxx
        memory: xxx
      limits:
        volcano/gpu_p40_8: 3
        nvidia.com/gpu: 3
        cpu: xxx
        memory: xxx

```


#### Notes:

Volcano adds startup parameters—gpu-type = ”volcano/gpu_k40_4, volcano/gpu_k40_16, volcano/gpu_p40_8, volcano/gpu_v100_8”

Specify the gpu request and limit of the above model in the submission job. At the same time, user need to specify the same number of nvidia.com/gpu resources (corresponding to the actual gpu setting)


### Device Plugin changes

Nvidia already support Device Plugin without GPU sharing mechanism. By providing a universal device plugin mechanism and a standard device API interface, it is opened through the feature gate, that is, configured --feature-gates = DevicePlugins = true. So we need to support KunLun Device Plugin and GPU sharing Device Plugin to meet our needs.

Device plugins are actually simple grpc servers. They need to implement the following two methods ListAndWatch and Allocate, and listen to Unix sockets in the /var/lib/kubelet/device-plugins/ directory, such as /var/lib/kubelet/device-plugins/xpu.sock.


* ListAndWatch: Kubelet will call this API for device discovery and status update (such as the device becoming unhealthy)

* Allocate: When Kubelet creates a container to use the device, Kubelet will call the API to perform the corresponding operation of the device and notify Kubelet to configure the device, volume, and environment variables required to initialize the container.

For achieving the goal of GPU memory sharing, we need to implement additional API called GpuMemoryToAllocate.

Based on the differences between Kunlun chips and ordinary GPU chips, NVIDIA's runtime configuration is removed, and the path of Kunlun chips is directly specified in the resource allocation of the plugin.



