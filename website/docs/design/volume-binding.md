# Volume Binding

## Backgrounds
Currently, Volcano does not support the `WaitForFirstConsumer` mode for volume binding. 
The current operations for binding volumes are intrusively modified into the scheduler cache, becoming part of the cache's interface.
The `VolumeBinding` plugin, as a plugin also introduced from kube-scheduler, should be introduced into the `predicates` or `nodeorder` plugins like other plugins, 
volume binding functionality should not be intrusively modified into the cache.

## Design Details
`BindVolumes`, `AllocateVolumes`, and other intrusive changes to the scheduler cache as interfaces need to be removed, 
which are originally part of the binder in the `VolumeBinding` plugin and should not be exposed to the cache. 
The extension points involved in the `VolumeBinding` plugin are `PreFilter`, `Filter`, `PreScore`, `Score`, `Reserve`, `Unreserve`, and `PreBind`, 
so it is most appropriate to introduce `VolumeBinding` into the `predicates` plugin, and `predicates` is the core plugin. 
`VolumeBinding` should be integrated into `predicates` plugin as a default enabled feature. However, 
predicates did not implement extension points such as `Score`, `Reserve`, `PreBind`, etc., so the predicates plugin can implement these interfaces as follows:
1. `Score`: For scoring, `predicates` can refer to and reuse the `AddBatchNodeOrderFn` logic of the 
[nodeorder](https://github.com/volcano-sh/volcano/blob/7103c18de19821cd278f949fa24c13da350a8c5d/pkg/scheduler/plugins/nodeorder/nodeorder.go#L301-L335) plugin
2. `Reserve` and `Unreserve`: For `Reserve` and `Unreserve`, `predicates` plugin can implement them in event handler's `AllocateFunc` and `DeallocateFunc`, for example,
add following codes for `Reserve`:
```go
func (pp *predicatesPlugin) runReservePlugins(ssn *framework.Session, event *framework.Event) {
	// Volume Binding Reserve
	if pp.volumeBindingPlugin != nil {
		status := pp.volumeBindingPlugin.Reserve(context.TODO(), state, event.Task.Pod, event.Task.Pod.Spec.NodeName)
		if !status.IsSuccess() {
			event.Err = status.AsError()
			return
		}
	}
	
	// other plugins' Reserve
}
```
3. `PreBind`: For `PreBind`, a new interface called `PreBinder` should be added and the predicates plugin should implement it, contains
two methods `PreBind` and `PreBindRollback`, and `PreBind` method directly calls the `PreBind` extension points of each plugin. 
Because the plugin lifecycle is generally in the session, that is, to pre-allocate nodes to pods, while `PreBind` and `Bind` are in the cache, 
which are two different goroutines, so if the plugin needs to pass additional information to the `PreBind` method, 
it can be set through the `BindContext` parameter of the `PreBind` method, which is a new struct containing a map that can carry the information that the plugin needs to pass to `PreBind`. 
The specific implementation details of `PreBind` can be referred to this doc: [PreBind](prebind.md).