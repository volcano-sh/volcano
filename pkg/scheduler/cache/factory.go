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

// PreBinderRegistry is used to hold the registered preBinders
type PreBinderRegistry struct {
	mu         sync.RWMutex
	preBinders map[string]PreBinder
}

func NewPreBinderRegistry() *PreBinderRegistry {
	return &PreBinderRegistry{
		preBinders: make(map[string]PreBinder),
	}
}

// Register registers or updates a preBinder for the given plugin name.
// It always overwrites the existing handler to support plugin configuration updates
// during runtime, as plugins may be reconfigured without restarting the scheduler.
func (r *PreBinderRegistry) Register(name string, preBinder interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if pb, ok := preBinder.(PreBinder); ok {
		r.preBinders[name] = pb
	}
}
