# Dynamic Resources Allocation(DRA) plugin
## Motivation
The kubelet device plugin interface allows extended devices to connect to the node, but for many newer devices, 
this approach for requesting these extended devices is too limited. For example:

- Device initialization: When starting a workload that uses an accelerator like an FPGA, the accelerator may need to be reconfigured 
  or reprogrammed. Currently, it’s impossible to specify the desired device properties that are required for reconfiguring devices. 
  For the FPGA example, a file containing the desired configuration of the FPGA has to be referenced.
- Device cleanup: Sometimes when the workload is finished it may need to clean up of the device. For example, an FPGA might have 
  to be reset because its configuration for the workload was confidential. Currently, there is no interface that device plugin provided 
  to clean up the devices.
- Partial allocation: When workloads use only a portion of the device capabilities, devices can be partitioned (e.g. using Nvidia MIG or SR-IOV) to better match workload needs. 
  Sharing the devices in this way can greatly increase HW utilization / reduce costs. Currently, there's no API to request partial device allocation, devices need to be 
  pre-partitioned and advertised in the same way a full / static devices are. User must then select a pre-partitioned device instead of having one created for them on the fly 
  based on their particular resource constraints. Without the ability to create devices dynamically (i.e. at the time they are requested) the set of pre-defined devices must 
  be carefully tuned to ensure that device resources do not go unused because some of the pre-partioned devices are in low-demand.
- Support Over the Fabric devices: Some devices may be available over the Fabric (network, special links, etc). Currently, the device plugin API is designed for node-local resources 
  that get discovered by a plugin running on the node. There is no universal design for this kind of cross-fabric scheduling. 
- Currently, `Requests` or `Limits` can only declare a number and can not pass many parameters. It relies too much on annotation to pass parameters.

Therefore, kubernetes introduces the DRA(Dynamic Resource Allocation), which also called Device Plugin 2.0. Volcano also follows up and introduces the DRA mechanism to 
enhance the extended device access capabilities. DRA has two architectures, in k8s versions 1.26~1.29, it is called classic DRA, 
resource controller and scheduler need to collaborate to schedule, which may lead to poor scheduling efficiency. Currently, this architecture has been withdrawn in k8s in latest branch, 
all the related codes has been removed. Structured parameters DRA is a new architecture introduced after v1.30, this will also be the only DRA architecture in the future. 
Therefore, volcano introduces structured paramaters DRA in k8s version 1.31, and will only maintain this DRA architecture and related APIs in the future.

The architecture of structured parameters DRA is as follows:
https://github.com/kubernetes/enhancements/raw/87dac43838c73966c05da2cb1a14c0ac0b66ceab/keps/sig-node/4381-dra-structured-parameters/components.png
`ResourceClaim` is the Pod's demand for extended device, just like the demand for PVC. Users need to customize the kubelet DRA driver to carry the attributes of the extended device in the `ResourceSlice` 
and report it to the scheduler, and then the scheduler will use the information in the `ResourceSlice` to schedule. 

> See more details you may refer to structured parameters DRA KEP: https://github.com/kubernetes/enhancements/tree/87dac43838c73966c05da2cb1a14c0ac0b66ceab/keps/sig-node/4381-dra-structured-parameters

## Non-goals
- Writing kubelet DRA driver. Volcano only provide the capabilities of scheduling pods that specify resource claim. 
- Replace the device plugin API. At present, DRA is not mature yet. If the application resource requirement is simple, it can still be implemented through using device plugin. 
However, if it involves more complex parameter passing and requires some more advanced scenarios, you can use DRA, which support sharing of extended resources such as GPU natively.
## Implementation
A new plugin called `dynamicresources` will be added. The struct of the `dynamicresources` plugin embeds the dynamicResources plugin 
in the native kube-scheduler, and directly calls the extension points such as `PreFilter`, `Filter`, `PreBidn` and others in the kube-scheduler:
```go
type dynamicResourcesPlugin struct {
	*dynamicresources.DynamicResources
}
```
The `dynamicresources` plugin will register following fns:
- PrePredicateFn:
It will call `PreEnqueue` firstly, the `PreEnqueue` is used to verify whether a Pod can be added to scheduler queue, in DRA plugin, 
it's used to check whether the resourceClaims specified by pod exist, so it's more appropriate to place the `PreEnqueue` in PrePredicateFn.
Secondly it will call `PreFilter`, and a new cycleState which will save the pod scheduling context will be initialized to pass between different extension points,
after `PreFilter` succeeded, the cycleState need to be stored into session, otherwise, it will not be able to pass between different extension points, 
and the extension points will be scattered in different places. For example, `PreBind` is executed in SchedulerCache. A new attribute will be added to 
session called `CycleStatesMap` to store those cycleStates that need to pass between extension points scattered in different places:
```go
type Session struct {
  ...
  // The key is task's UID, value is the CycleState.
  CycleStatesMap sync.Map
  ...
}
```
- PredicateFn:
It will get the cycleState stored in `PrePredicateFn` from session and then call `Filter` to get the resource claim allocation result.
- PreBindFn:
It's a new fn added into volcano, it's necessary to bind some resources such as `ResourceClaim` and `PVC` before binding the pod onto the node. 
It will be called in `Bind` stage in `SchedulerCache`, but currently `Bind` only take TaskInfo as the input parameter, 
`PreBindFn` is an attribute registered by the plugin to the session and can not be carried to the `SchedulerCache` for execution. 
Therefore, `Bind` needs to carry more information as input parameters. A new structure called `BindContext` is added:
```go
type BindContext struct {
    TaskInfo *schedulingapi.TaskInfo
    
    // Before the Bind task, we need to execute PreBind. UnPreBindFns need to be executed when prebind fails.
    NodeInfo     *schedulingapi.NodeInfo
    PreBindFns   []schedulingapi.PreBindFn
    UnPreBindFns []schedulingapi.UnPreBindFn
}
```
And `BindContext` will be took as the input parameter to execute the `Bind`, before the pod binding, it will call `PreBindFn` to do prebind,
if prebind failed, UnPreBindFn will called to rollback prebind:
```go
func (sc *SchedulerCache) BindTask() {
	...
	go func(bindContexts []*BindContext) {
		for _, bindContext := range bindContexts {
			for _, preBindFn := range bindContext.PreBindFns {
				err := preBindFn(bindContext.TaskInfo, bindContext.NodeInfo)
				if err != nil {
					klog.Errorf("task %s/%s execute prebind failed: %v", bindContext.TaskInfo.Namespace, bindContext.TaskInfo.Name, err)
					for i := len(bindContext.UnPreBindFns) - 1; i >= 0; i-- {
						unPreBindFn := bindContext.UnPreBindFns[i]
						unPreBindFn(bindContext.TaskInfo, bindContext.NodeInfo)
					}
					sc.resyncTask(bindContext.TaskInfo)
					return
				}
			}
                ...
		}

		sc.Bind(bindContexts)
	}(tmpBindCache)
	...
```
- EventHandler: 
  - AllocateFunc: It will call `Reserve` to reserve the resource claim for the pod, this is useful when multiple pods specify the same `ResourceClaim`.
  - DeallocateFunc: It will call `Unreserve` to rollback if `Reserve` failed.

- UnPreBindFn: It will call `Unreserve` to rollback if `PreBind` failed.

In `OnSessionOpen`, the `DynamicResource` of kube-scheduler will be initialized and the above Fns will be registered in the Session.
Besides, a new string slice attribute called BindContextEnabledPlugins will be added to session, it records the plugin name that need to
carry out the `NodeInfo`, `PreBindFns`, `EventHandlers` together with the `TaskInfo` in `BindContext`.  