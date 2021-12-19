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
	"io"
	"os"
	"sync"

	"k8s.io/client-go/rest"
	"k8s.io/klog"
)

// Factory is a function that returns a cache.Interface.
// The cache parameter provides an SchedulerCache object for the factory,
// allowing the eventHandler to get and modify the global schedulerCache.
// The config parameter provides an io.Reader handler to the factory in
// order to load specific configurations. If no configuration is provided
// the parameter is nil.
// restConfig holds the common attributes that can be passed to a
// kubernetes client on initialization. Allow the eventHandler to initialize
// a new client to interact with the kubernetes cluster.
type Factory func(cache *SchedulerCache, config io.Reader, restConfig *rest.Config) (Interface, error)

// eventHandlers is a mapping from <eventHandler name> to factory that
// processing the object events <object event handler suits>.
var eventHandlers = make(map[string]Factory)

// eventHandlerMutex protects the cache.eventHandlers and is
// used to synchronize eventHandler registry.
var eventHandlerMutex sync.Mutex

// RegisterEventHandler registers a cache.Factory by name.
// Register when the component starts.
func RegisterEventHandler(name string, eventHandler Factory) {
	eventHandlerMutex.Lock()
	defer eventHandlerMutex.Unlock()

	if _, found := eventHandlers[name]; found {
		klog.Fatalf("EventHandler %q was registered twice.", name)
	}

	klog.Infof("Registered eventHandler %q.", name)
	eventHandlers[name] = eventHandler
}

// GetEventHandler creates an instance of the event handler, or nil if the name is unknown.
// The error return if failed to read the configuration or the named handler was known but failed to initialize.
// The name parameter is the provider event handler name.
// The configFilePath parameter specifies the path of the configuration file for the event handler,
// If the configFilePath is null, that is there is no configuration for the event handler.
// The cache parameter provides an SchedulerCache object for the event handler,
// allowing the eventHandler to get and modify the global schedulerCache.
// The restConfig holds the common attributes that can be passed to a
// kubernetes client on initialization. Allow the eventHandler to initialize
// a new client to interact with the kubernetes cluster.
func GetEventHandler(name, configFilePath string, cache *SchedulerCache, restConfig *rest.Config) (Interface, error) {
	eventHandlerMutex.Lock()
	defer eventHandlerMutex.Unlock()

	f, found := eventHandlers[name]
	if !found {
		return nil, nil
	}

	var config *os.File
	if len(configFilePath) != 0 {
		var err error
		config, err = os.Open(configFilePath)
		if err != nil {
			klog.Fatalf("Couldn't open the event handler configuration %q: %#v.", configFilePath, err)
			return nil, err
		}

		defer func() {
			if err := config.Close(); err != nil {
				klog.Errorf("Close file %q failed for %v.", configFilePath, err)
			}
		}()
	}

	return f(cache, config, restConfig)
}
