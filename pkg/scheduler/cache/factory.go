package cache

import (
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// bindMethodMap Binder management
var bindMethodMap Binder

// RegisterBindMethod register Bind Method
func RegisterBindMethod(binder Binder) {
	bindMethodMap = binder
}

func GetBindMethod() Binder {
	return bindMethodMap
}

func init() {
	RegisterBindMethod(NewBinder())
}

// CustomResourceEventHandlerCreator the constructor function of the EventHandler object
type CustomResourceEventHandlerCreator func(sc *SchedulerCache, config *rest.Config) (CustomResourceEventHandler, error)

type eventHandlersType struct {
	creators map[string]CustomResourceEventHandlerCreator
	handlers map[string]CustomResourceEventHandler
}

var eventHandlers = eventHandlersType{
	creators: map[string]CustomResourceEventHandlerCreator{},
	handlers: map[string]CustomResourceEventHandler{},
}

// RegisterCustomResourceEventHandler register the creator function to the factory
func RegisterCustomResourceEventHandler(name string, creator CustomResourceEventHandlerCreator) {
	if _, found := eventHandlers.creators[name]; found {
		klog.Fatalf("EventHandler %q was registered twice.", name)
	}
	eventHandlers.creators[name] = creator
	klog.V(3).Infof("register %s event handler success", name)
}

// AddCustomResourceEventHandlers iterate all registered event handlers to register their methods to k8s
func AddCustomResourceEventHandlers(sc *SchedulerCache, config *rest.Config) {
	for name, ehc := range eventHandlers.creators {
		handler, err := ehc(sc, config)
		if err != nil {
			klog.Errorf("register %s event handlers failed: %v", name, err)
			continue
		}
		eventHandlers.handlers[name] = handler
		handler.AddEventHandlers()
		klog.V(3).Infof("add %s event handlers success", name)
	}
}

// StartCustomResourceInformers iterate to start all informers of the registered event handlers
func StartCustomResourceInformers(stop <-chan struct{}) {
	for _, ih := range eventHandlers.handlers {
		ih.StartInformer(stop)
	}
}

// WaitForCustomResourceCacheSync iterate to wait all cache synced
func WaitForCustomResourceCacheSync(stop <-chan struct{}) {
	for _, ih := range eventHandlers.handlers {
		ih.WaitForCacheSync(stop)
	}
}

// CustomResourceClient get the client set of the custom event handler
func CustomResourceClient(name string) interface{} {
	return eventHandlers.handlers[name].Client()
}
