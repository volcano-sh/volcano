/*
Copyright 2014 The Kubernetes Authors.

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

package queuejobresources

import (
	"sync"

	"github.com/golang/glog"
	qjobv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	"k8s.io/client-go/rest"
)

// Factory is a function that returns an Interface for queue job resources.
type Factory func(config *rest.Config) Interface

//RegisteredResources : registered resources
type RegisteredResources struct {
	lock     sync.Mutex
	registry map[qjobv1.ResourceType]Factory
}

// Registered enumerates the names of all registered plugins.
func (rres *RegisteredResources) Registered() []qjobv1.ResourceType {
	rres.lock.Lock()
	defer rres.lock.Unlock()
	keys := []qjobv1.ResourceType{}
	for k := range rres.registry {
		keys = append(keys, k)
	}
	return keys
}

// Register registers a Factory by type. This
// is expected to happen during app startup.
func (rres *RegisteredResources) Register(t qjobv1.ResourceType, factory Factory) {
	rres.lock.Lock()
	defer rres.lock.Unlock()
	if rres.registry != nil {
		_, found := rres.registry[t]
		if found {
			glog.Fatalf("Queue job resource type %s was registered twice", t)
		}
	} else {
		rres.registry = map[qjobv1.ResourceType]Factory{}
	}

	glog.V(1).Infof("Registered queue job resource type %s", t)
	rres.registry[t] = factory

}

// InitQueueJobResource creates an instance of the type queue job resource.  It returns
//`false` if the type is not known.
func (rres *RegisteredResources) InitQueueJobResource(t qjobv1.ResourceType,
	config *rest.Config) (Interface, bool, error) {
	rres.lock.Lock()
	defer rres.lock.Unlock()
	f, found := rres.registry[t]
	if !found {
		return nil, false, nil
	}

	ret := f(config)
	return ret, true, nil
}
