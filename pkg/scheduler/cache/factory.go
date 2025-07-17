/*
Copyright 2021 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cache

import (
	"sync"

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

// BinderRegistry is used to hold the registered binders, such as pre-binders, post-binders
type BinderRegistry struct {
	mu         sync.RWMutex
	preBinders map[string]PreBinder
	// Can add postBinders in the future
}

func NewBinderRegistry() *BinderRegistry {
	return &BinderRegistry{
		preBinders: make(map[string]PreBinder),
	}
}

// Register registers or updates a binder for the given plugin name. The plugin can be such as preBinder or postBinder.
// It always overwrites the existing binder map to support plugin configuration updates
// during runtime, as plugins may be reconfigured without restarting the scheduler.
func (r *BinderRegistry) Register(name string, binder interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if pb, ok := binder.(PreBinder); ok {
		klog.V(5).Infof("Register preBinder %s successfully", name)
		r.preBinders[name] = pb
	}
}
