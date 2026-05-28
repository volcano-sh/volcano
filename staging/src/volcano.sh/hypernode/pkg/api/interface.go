/*
Copyright 2025 The Volcano Authors.

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

package api

import (
	"fmt"
	"sync"

	clientset "k8s.io/client-go/kubernetes"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
)

// Discoverer is the interface for network topology discovery,
// every discoverer should implement this interface and return the discovered hyperNodes
type Discoverer interface {
	// Start begins the discovery process, sending discovered nodes through the provided channel
	Start() (chan []*topologyv1alpha1.HyperNode, error)

	// Stop halts the discovery process
	Stop() error

	// Name returns the discoverer identifier, this is used for labeling discovered hyperNodes for distinction.
	Name() string

	// ResultSynced the manager must call this method to notice the topology discovery results have been processed
	ResultSynced()
}

// DiscovererConstructor is a function type used to create instances of specific discoverer source
type DiscovererConstructor func(cfg DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) Discoverer

var (
	mutex              sync.Mutex
	discovererRegistry = make(map[string]DiscovererConstructor)
)

// RegisterDiscoverer registers a discoverer constructor for a given source
func RegisterDiscoverer(source string, constructor DiscovererConstructor) {
	mutex.Lock()
	defer mutex.Unlock()

	discovererRegistry[source] = constructor
}

// NewDiscoverer creates a new discoverer instance based on source
func NewDiscoverer(cfg DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) (Discoverer, error) {
	mutex.Lock()
	defer mutex.Unlock()

	constructor, exists := discovererRegistry[cfg.Source]
	if !exists {
		return nil, fmt.Errorf("unsupported discoverer type: %s", cfg.Source)
	}
	return constructor(cfg, kubeClient, vcClient), nil
}
