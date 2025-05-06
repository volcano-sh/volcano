# PreBind

## Backgrounds
Inspired by [#3618](https://github.com/volcano-sh/volcano/issues/3618). Volcano has introduced a lot of plugins from kube-scheduler,
such as `NodeAffinity`, `TaintToleration`, `PodTopologySpread`, `ImageLocality`, etc. These plugins implement the `PreFilter`, `Filter`, 
`PreScore`, `Score` in the predicates and nodeorder plugins. However, plugins such as `VolumeBinding` and `DynamicResourceAllocation` 
require the `PreBind` extension point. This extension point is executed before binding the Pod to a node and is used to bind additional resources, 
such as `PVC` and `ResourceClaim`. If the `PreBind` operation fails, the pod cannot be bound to a node. Currently, volcano lacks the `PreBind` extension point.

## Motivation
Add `PreBind` extension point to volcano, enables the introduction of plugins such as `VolumeBinding` and `DynamicResourceAllocation`, to bind additional resources

## Design Details
A scheduling session is responsible for filtering nodes, scoring nodes, and pre-selecting a node for a pod. 
While the cache is responsible for binding the Pod to a node. The session and cache execute in two different goroutines. 
Since `Bind` is executed in the cache, `PreBind` also needs to be executed in the cache. 
However, the current extension points are all registered in the session, such as `PrePredicateFn`, `PredicateFn`, `NodeOrderFn`, etc.
Adding an extension point like called `PreBindFn` in the session is meaningless because `PreBind` needs to be executed in the cache,
If we try to add `PreBindFn` to the session like adding other extension points, when the session dispatches the pod bind task to the cache, 
it still needs to copy the plugins' `PreBind` execution func to the cache. If some plugins' `PreBind` logic contains references to the session, 
this might also lead to the session not being released even after `CloseSession` has been called.
Therefore, it is best for the plugin to register `PreBind` directly to the cache. 

### PreBinder interface
A new interface needs to be added to the cache, called the `PreBinder` interface:
```go
type PreBinder interface {
	PreBind(ctx context.Context, bindCtx *BindContext) error

	// PreBindRollBack is called when the pre-bind or bind fails.
	PreBindRollBack(ctx context.Context, bindCtx *BindContext)
}
```
It contains two methods, one is `PreBind`, which is the method for the plugin to execute the main `PreBind` logic, and the other is `PreBindRollBack`. 
When `PreBind` fails or `Bind` fails, the executed `PreBind` needs to be rolled back to unbind the additional bound resources. If the plugin wants to execute `PreBind`, 
it needs to implement the `PreBinder` interface and call the newly added `RegisterPreBinder` method in session to register the plugin into the cache:
```go
// RegisterPreBinder registers the passed bind handler to the cache
func (ssn *Session) RegisterPreBinder(name string, preBinder interface{}) {
	ssn.cache.RegisterPreBinder(name, preBinder)
}
```
### BindContext
For the parameters of `PreBind` and `PreBindRollBack`, `BindContext` is a newly defined structure:
```go
type BindContextExtension interface{}

type BindContext struct {
	TaskInfo *schedulingapi.TaskInfo
	// Extensions stores extra bind context information of each plugin
	Extensions map[string]BindContextExtension
}
```
This structure contains the `TaskInfo` that needs to be bound, and a new field called `Extensions`. It's a map contains the bind context information of each plugin.
The `BindContextExtension` carries the information that the plugin needs to pass into the `PreBind` extension point, the `Bind` extension point, 
and even the `PostBind` extension point that may need to be added in the future. The `BindContextExtension` is just a empty `interface{}` type.
```go
type BindContextHandler interface {
	// SetupBindContextExtension allows the plugin to set up extension information in the bind context
	SetupBindContextExtension(ssn *Session, bindCtx *cache.BindContext)
}
```
`BindContextHandler` is a new interface added to session. Plugin can choose whether to implement `BindContextHandler` interface. For example, 
`VolumeBinding` needs to carry cycleState to `PreBind`, it can wrap cycleState and then set the additional information to be passed through `SetUpBindContextExtension` method.
```go
// bind context extension information
type bindContextExtension struct {
	State *k8sframework.CycleState
}

func (pp *predicatesPlugin) SetupBindContextExtension(ssn *framework.Session, bindCtx *cache.BindContext) {
	// ...
	// set up bind context extension
	bindCtx.Extensions[pp.Name()] = &bindContextExtension{State: ssn.GetCycleState(bindCtx.TaskInfo.UID)}
}
```
So to summarize, if a new plugin needs to call `PreBind`, the process involves:
1. Implement the `PreBinder` interface
2. Call `RegisterPreBinder` to register the plugin when in `OpenSession`

(Optional) If the plugin needs to pass additional information to `PreBind`, the process involves:
1. Implement the `SetupBindContextExtension` method of the `BindContextHandler` interface
2. In the `SetupBindContextExtension` implementation, set the additional information that the plugin needs to carry in the `Extensions` map of the `BindContext` parameter

