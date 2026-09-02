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

	coreinformerv1 "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	topologyinformerv1alpha1 "volcano.sh/apis/pkg/client/informers/externalversions/topology/v1alpha1"
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

// DiscovererOptions contains the process-provided dependencies available to discoverers.
type DiscovererOptions struct {
	KubeClient        clientset.Interface
	VolcanoClient     vcclientset.Interface
	NodeInformer      coreinformerv1.NodeInformer
	HyperNodeInformer topologyinformerv1alpha1.HyperNodeInformer
}

// DiscovererConstructor creates a discoverer from configuration and clients.
// It is retained for compatibility with existing out-of-tree discoverers.
type DiscovererConstructor func(cfg DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) Discoverer

// DiscovererOptionsConstructor creates a discoverer from configuration and
// process-provided dependencies, including shared informers.
type DiscovererOptionsConstructor func(cfg DiscoveryConfig, options DiscovererOptions) (Discoverer, error)

var (
	mutex              sync.Mutex
	discovererRegistry = make(map[string]DiscovererOptionsConstructor)
)

// RegisterDiscoverer registers a discoverer constructor for a given source
func RegisterDiscoverer(source string, constructor DiscovererConstructor) {
	RegisterDiscovererWithOptions(source, func(cfg DiscoveryConfig, options DiscovererOptions) (Discoverer, error) {
		return constructor(cfg, options.KubeClient, options.VolcanoClient), nil
	})
}

// RegisterDiscovererWithOptions registers a discoverer that reuses
// process-provided dependencies such as shared informers.
func RegisterDiscovererWithOptions(source string, constructor DiscovererOptionsConstructor) {
	mutex.Lock()
	defer mutex.Unlock()

	discovererRegistry[source] = constructor
}

// NewDiscoverer creates a discoverer using the legacy client-only contract.
func NewDiscoverer(cfg DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) (Discoverer, error) {
	return NewDiscovererWithOptions(cfg, DiscovererOptions{KubeClient: kubeClient, VolcanoClient: vcClient})
}

// NewDiscovererWithOptions creates a discoverer using process-provided dependencies.
func NewDiscovererWithOptions(cfg DiscoveryConfig, options DiscovererOptions) (Discoverer, error) {
	mutex.Lock()
	defer mutex.Unlock()

	constructor, exists := discovererRegistry[cfg.Source]
	if !exists {
		return nil, fmt.Errorf("unsupported discoverer type: %s", cfg.Source)
	}
	return constructor(cfg, options)
}
