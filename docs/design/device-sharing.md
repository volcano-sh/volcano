# Sharing devices in volcano

## Introduction

We implement a common interface for shareable devices(GPU,NPU,FPGA,...) called sharedDevicePool, and use it to reimplement current gpu-share mechanism. The goal is to let device-sharing easy to implement, and better organised. If you wish to grant vc-scheduler the ability to share another device, all you need to do is to implement these methods in sharedDevicePool, and place your logic under pkg/scheduler/api/devices. No other hackings are needed.

## Backguards

We intended to provide volcano the ability to share third-party resources link GPU,NPU,etc in the near future. At fitst, I tried to implement these logic based on predicate.gpushare, but i sooner realised that logics scattered in device_info.go, node_info.go, pod_info.go, and whole predicate folder. if i follow predicate.gpushare implementation, i have no choice but hack deeply into vc-scheduler api. Sooner or later vc-scheduler api will be crowded with various device-sharing logic, that is probably not what we wished.

## Implementation

### Interface SharedDevicePool design

The design of shareddevicePool is shown below:

```
type SharedDevicePool interface {
	//following two functions used in node_info
	AddResource(pod *v1.Pod)
	SubResource(pod *v1.Pod)

	//following four functions used in predicate
	RequestInPod(pod *v1.Pod) bool // check whether this pod uses this device
	FitInPod(pod *v1.Pod) (bool, error) // check whether this pod can fit in current node
	AllocateToPod(kubeClient kubernetes.Interface, pod *v1.Pod) error
	ReleaseFromPod(kubeClient kubernetes.Interface, pod *v1.Pod) error

	//used for debug and monitor
	MonitorStatus() string
}
```

The first two method are used for node_info to update cluster status. The following four methods are used in predicate which allocatation and deallocation actually take place. Finally a monitor mothod for debug.

### Create a seperate package for gpushare related methods, and use SharedDevicePool method to reimplement it.

There are two steps we need to do, first, we need to create a new package in "pkg/scheduler/api/devices/nvidia/gpushare", and implement SharedDevicePool methods in it, then we need to seperate gpushare-related logic from "scheduler.api" and "predicate plugin", and convert them to package "pkg/scheduler/api/devices/nvidia/gpushare". The package contains the following files: device.go(which implement SharedDevicePool interface methods), share.go(which contains private methods for device.go), type.go(which contains const values and definations).

Details of methods mapping is shown in the table below:

| origin file | corresponding file(s) in new package |
| ------------- | ------------- |
| pkg/scheduler/api/node_info.go | pkg/scheduler/api/devices/nvidia/gpushare/device_info.go, pkg/scheduler/api/devices/nvidia/gpushare/share.go |
| pkg/scheduler/api/device_info.go | pkg/scheduler/api/devices/nvidia/gpushare/device_info.go, pkg/scheduler/api/devices/nvidia/gpushare/share.go |
| pkg/scheduler/api/pod_info.go | pkg/scheduler/api/devices/nvidia/gpushare/share.go |
| pkg/scheduler/plugins/predicates/predicates.go | pkg/scheduler/api/devices/nvidia/gpushare/device_info.go |
| pkg/scheduler/plugins/predicates/gpu.go | pkg/scheduler/api/devices/nvidia/gpushare/share.go |

## How to add a new device-share policy

### 1. Define your device in /pkg/scheduler/api/shared_device_pool.go

Name your policy and put it in shared_device_pool.go as follows:

```
const (
	GPUSharingDevice = "GpuShare"
	Your_new_sharing_policy = "xxxxx"
)
```

### 2. Create a new package in /pkg/scheduler/api/devices/"your device name"/"your policy name"

For example, if you try to implement a NPU share policy, then you are recommended to create a package in /pkg/scheduler/api/device/ascend/npushare

### 3. Implement methods of interface shared_device_pool, and put them in your new package

Note that, you can't to refer to any struct of methods in scheduler.api to avoid cycle importing. If there is anything in scheduler.api you *must* need, then you should modify the SharedDevicePool interface to pass it.
The methods defined in SharedDevicePool interface and its information is shown in table below:

| interface | invoker file | information |
| ------------- | ------------ | ------------- |
| AddResource(pod *v1.Pod) | pkg/scheduler/api/node_info.go | Add the 'pod' and its resources into scheduler cache |
| SubResource(pod *v1.Pod) | pkg/scheduler/api/node_info.go | Delete the 'pod' and substract its resources from scheduler cache |
| RequestInPod(pod *v1.Pod) bool | pkg/scheduler/plugins/predicates/predicate.go | Check whether this 'pod' request a portion of this device |
| FitInPod(pod *v1.Pod)| pkg/scheduler/plugins/predicates/predicate.go | Check whether the portion of device this pod requests can fit in current node |
| AllocateToPod(kubeClient kubernetes.Interface, pod *v1.Pod) error | pkg/scheduler/plugins/predicates/predicate.go | Allocate the portion of this device from the current node to this pod |
| ReleaseFromPod(kubeClient kubernetes.Interface, pod *v1.Pod) error | pkg/scheduler/plugins/predicates/predicate.go | Dellocate the portion of this device from this pod |
| MonitorStatus() string | none | Used for debug and monitor | 

### 4. Add your initialization code in /pkg/scheduler/api/node_info.go

This is the *only* place you hack into scheduler.api ,which you have to register your policy during initialization of node_struct.

```
// NewNodeInfo is used to create new nodeInfo object
{

	func NewNodeInfo(node *v1.Node) *NodeInfo {
		nodeInfo := &NodeInfo{
		...

		nodeInfo.SharedDevices[GPUSharingDevice] = gpushare.NewGPUDevices(nodeInfo.Name, node)
		#nodeInfo.SharedDevices[you policy name] = "your policy package Initialization method"
		...
		}
		return nodeInfo
	}
}
```

### 5. Check if your policy is enabled in /pkg/scheduler/plugins/predicate/predicates.go

This is the *only* plae you hack into predicates.go, when the scheduler checks if your policy is enabled in scheduler configuration.

predicates.go:

```
...
// Checks whether predicate.GPUSharingEnable is provided or not, if given, modifies the value in predicateEnable struct.
args.GetBool(&gpushare.GpuSharingEnable, GPUSharingPredicate)
args.GetBool(&gpushare.GpuNumberEnable, GPUNumberPredicate)
args.GetBool(&gpushare.NodeLockEnable, NodeLockEnable)
args.GetBool("your policy enable variable","your policy enable parameter")
...
```




