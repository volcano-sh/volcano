# Sharing devices in volcano

## Introduction

We implement a common interface for shareable devices(GPU,NPU,FPGA,...) called Devices, and use it to reimplement current gpu-share mechanism. The goal is to let device-sharing easy to implement, and better organised. If you wish to grant vc-scheduler the ability to share another device, all you need is to implement these methods in Devices, and place your logic under pkg/scheduler/api/devices. 

## Backguards

We intended to provide volcano the ability to share third-party resources link GPU,NPU,etc in the near future. At first, I tried to implement these logics based on predicate.gpushare, but i sooner realised that these logics scattered in device_info.go, node_info.go, pod_info.go, and whole predicate folder. if i follow the implementation of predicate.gpushare, i will have no choice but hack deeply into vc-scheduler api. Sooner or later vc-scheduler api will be crowded with various device-sharing logic, which is probably not what we wished.

## Implementation

### Interface Devices design

The design of Devices is shown below:

```
type Devices interface {
	//following two functions used in node_info
	//AddResource is to add the corresponding device resource of this 'pod' into current scheduler cache
	AddResource(pod *v1.Pod)
	//SubResoure is to substract the corresponding device resource of this 'pod' from current scheduler cache
	SubResource(pod *v1.Pod)

	//following four functions used in predicate
	//HasDeviceRequest checks if the 'pod' request this device
	HasDeviceRequest(pod *v1.Pod) bool
	//FiltreNode checks if the 'pod' fit in current node
	// The first return value represents the filtering result, and the value range is "0, 1, 2, 3"
	// 0: Success
	// Success means that plugin ran correctly and found pod schedulable.

	// 1: Error
	// Error is used for internal plugin errors, unexpected input, etc.

	// 2: Unschedulable
	// Unschedulable is used when a plugin finds a pod unschedulable. The scheduler might attempt to
	// preempt other pods to get this pod scheduled. Use UnschedulableAndUnresolvable to make the
	// scheduler skip preemption.
	// The accompanying status message should explain why the pod is unschedulable.

	// 3: UnschedulableAndUnresolvable
	// UnschedulableAndUnresolvable is used when a plugin finds a pod unschedulable and
	// preemption would not change anything. Plugins should return Unschedulable if it is possible
	// that the pod can get scheduled with preemption.
	// The accompanying status message should explain why the pod is unschedulable.
	FilterNode(pod *v1.Pod) (int, string, error)
	
	//Allocate action in predicate
	Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error
	//Release action in predicate
	Release(kubeClient kubernetes.Interface, pod *v1.Pod) error

	//used for debug and monitor
	GetStatus() string
}
```

The first two method are used for node_info to update cluster status. The following four methods are used in predicate which allocatation and deallocation actually take place. Finally a monitor mothod for debug.

### Create a seperate package for gpushare related methods, and use Devices method to reimplement it.

There are two steps we need to do, first, we need to create a new package in "pkg/scheduler/api/devices/nvidia/gpushare", and implement Devices methods in it, then we need to seperate gpushare-related logic from "scheduler.api" and "predicate plugin", and convert them to package "pkg/scheduler/api/devices/nvidia/gpushare". The package contains the following files: device.go(which implement SharedDevicePool interface methods), share.go(which contains private methods for device.go), type.go(which contains const values and definations).

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
| HasDeviceRequest(pod *v1.Pod) bool | pkg/scheduler/plugins/predicates/predicate.go | Check whether this 'pod' request a portion of this device |
| FilterNode(pod *v1.Pod)| pkg/scheduler/plugins/predicates/predicate.go | Check whether the portion of device this pod requests can fit in current node |
| Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error | pkg/scheduler/plugins/predicates/predicate.go | Allocate the portion of this device from the current node to this pod |
| Release(kubeClient kubernetes.Interface, pod *v1.Pod) error | pkg/scheduler/plugins/predicates/predicate.go | Dellocate the portion of this device from this pod |
| GetStatus() string | none | Used for debug and monitor | 

### 4. Add your initialization code in /pkg/scheduler/api/node_info.go

This is the *only* place you hack into scheduler.api ,which you have to register your policy during initialization of node_struct.

```

// setNodeOthersResource initialize sharable devices
func (ni *NodeInfo) setNodeOthersResource(node *v1.Node) {
	ni.Others[GPUSharingDevice] = gpushare.NewGPUDevices(ni.Name, node)
	//ni.Others["your device sharing policy name"] = your device sharing package initialization method
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




