package cache

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// bindMethodMap Binder management
var bindMethodMap Binder

// RegisterBindMethod register Bind Method
func RegisterBindMethod(binder Binder) {
	bindMethodMap = binder
}

// GetBindMethod get the registered Binder
func GetBindMethod() Binder {
	return bindMethodMap
}

// CustomResourceEventHandlerCreator the constructor function of the EventHandler object.
type CustomResourceEventHandlerCreator func(sc *SchedulerCache) (CustomResourceEventHandler, error)

type eventHandlersType struct {
	creators            map[string]CustomResourceEventHandlerCreator
	initializedHandlers map[string]CustomResourceEventHandler
}

var eventHandlers = eventHandlersType{
	creators:            make(map[string]CustomResourceEventHandlerCreator),
	initializedHandlers: make(map[string]CustomResourceEventHandler),
}

// RegisterCustomResourceEventHandler register the creator function to the factory
func RegisterCustomResourceEventHandler(name string, creator CustomResourceEventHandlerCreator) {
	if _, found := eventHandlers.creators[name]; found {
		klog.ErrorS(nil, "Event handler is already registered", "name", name)
		return
	}
	eventHandlers.creators[name] = creator
	klog.InfoS("Successfully register event handler", "name", name)
}

// GetEventHandlerByGVR return a custom handler func corresponds to a gvr.
func GetEventHandlerByGVR(gvr schema.GroupVersionResource, sc *SchedulerCache) cache.ResourceEventHandler {
	var handler CustomResourceEventHandler
	var ok bool
	var err error
	for name, ehc := range eventHandlers.creators {
		handler, ok = eventHandlers.initializedHandlers[name]
		if !ok {
			handler, err = ehc(sc)
			if err != nil {
				klog.ErrorS(err, "Failed to create event handler", "name", name)
				continue
			}
			eventHandlers.initializedHandlers[name] = handler
		}

		if f := handler.EventHandlerFuncs(gvr); f != nil {
			klog.InfoS("Find matched handler", "name", name, "gvr", gvr)
			return f
		}
	}
	return cache.ResourceEventHandlerFuncs{}
}
