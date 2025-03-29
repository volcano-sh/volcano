package cache

import "sync"

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

type bindHandler struct {
	preBinder PreBinder
}

// BindHandlerRegistry is used to hold the registered bindHandlers, which include calls to PreBind,
// error handling for bind failures, etc.
type BindHandlerRegistry struct {
	mu       sync.RWMutex
	handlers map[string]*bindHandler
}

func NewBindHandlerRegistry() *BindHandlerRegistry {
	return &BindHandlerRegistry{
		handlers: make(map[string]*bindHandler),
	}
}

// Register registers or updates a bind handler for the given plugin name.
// It always overwrites the existing handler to support plugin configuration updates
// during runtime, as plugins may be reconfigured without restarting the scheduler.
func (r *BindHandlerRegistry) Register(name string, handler interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	h := &bindHandler{}
	if pb, ok := handler.(PreBinder); ok {
		h.preBinder = pb
	}

	r.handlers[name] = h
}
