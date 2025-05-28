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

package discovery

import (
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
	"volcano.sh/volcano/pkg/controllers/hypernode/config"

	_ "volcano.sh/volcano/pkg/controllers/hypernode/discovery/ufm"
)

type Result struct {
	// HyperNodes contains the discovered hypernodes
	HyperNodes []*topologyv1alpha1.HyperNode
	// Source indicates the source of the discovery
	Source string
}

// Manager is the interface for managing network topology discovery
type Manager interface {
	// Start initializes and starts the topology discovery manager
	Start() error
	// Stop halts all discovery processes
	Stop()
	// ResultChannel returns a channel for receiving discovery results
	ResultChannel() <-chan Result
}

// manager manages network topology discovery processes
type manager struct {
	mutex sync.Mutex

	configLoader config.Loader
	config       *api.NetworkTopologyConfig

	discoverers map[string]api.Discoverer
	workQueue   workqueue.TypedRateLimitingInterface[string]
	stopCh      chan struct{}

	kubeClient clientset.Interface

	resultCh chan Result
}

// NewManager create a new network topology discovery manager
func NewManager(configLoader config.Loader, queue workqueue.TypedRateLimitingInterface[string], kubeClient clientset.Interface) Manager {
	return &manager{
		configLoader: configLoader,
		discoverers:  make(map[string]api.Discoverer),
		resultCh:     make(chan Result),
		stopCh:       make(chan struct{}),
		workQueue:    queue,
		kubeClient:   kubeClient,
	}
}

// Start initializes and starts the topology discovery manager
func (m *manager) Start() error {
	var err error
	cfg, err := m.configLoader.LoadConfig()
	if err != nil {
		klog.ErrorS(err, "Failed to load config")
		// Initialize with an empty config to avoid nil pointer dereference.
		m.config = &api.NetworkTopologyConfig{}
		// Do not return an error here, in case of configMap is updated correctly later.
	} else {
		m.config = cfg
	}

	go m.worker()

	klog.InfoS("Network topology discovery manager started")
	return nil
}

// Stop halts all discovery processes
func (m *manager) Stop() {
	close(m.stopCh)
	m.stopAllDiscoverers()
	klog.InfoS("Network topology discovery manager stopped")
}

func (m *manager) ResultChannel() <-chan Result {
	return m.resultCh
}

// startSingleDiscoverer start a single network topology discoverer.
func (m *manager) startSingleDiscoverer(source string) error {
	cfg, err := m.configLoader.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}
	discoveryCfg := cfg.GetDiscoveryConfig(source)
	if discoveryCfg == nil {
		return fmt.Errorf("configuration not found for network topology discovery source: %s", source)
	}

	discoverer, err := api.NewDiscoverer(*discoveryCfg, m.kubeClient)
	if err != nil {
		return fmt.Errorf("failed to create discoverer: %v", err)
	}

	m.discoverers[source] = discoverer

	outputCh, err := discoverer.Start()
	if err != nil {
		return fmt.Errorf("failed to start discoverer: %v", err)
	}

	go m.processTopology(source, outputCh)

	klog.InfoS("Started network topology discoverer", "source", source)
	return nil
}

func (m *manager) stopAllDiscoverers() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for source := range m.discoverers {
		if err := m.stopSingleDiscoverer(source); err != nil {
			klog.ErrorS(err, "Failed to stop discoverer", "source", source)
		}
	}
	m.discoverers = make(map[string]api.Discoverer)
}

func (m *manager) stopSingleDiscoverer(source string) error {
	discoverer, exists := m.discoverers[source]
	if !exists {
		klog.InfoS("No need to stop discoverer as it may not start yet", "source", source)
		return nil
	}

	if err := discoverer.Stop(); err != nil {
		return err
	}

	delete(m.discoverers, source)
	return nil
}

func (m *manager) worker() {
	for m.processNext() {
	}
}

// processNext handles a single workQueue item
func (m *manager) processNext() bool {
	key, shutdown := m.workQueue.Get()
	if shutdown {
		return false
	}
	defer m.workQueue.Done(key)

	if err := m.syncHandler(key); err != nil {
		m.workQueue.AddRateLimited(key)
		klog.ErrorS(err, "Failed to process network topology discoverer", "key", key)
		return true
	}
	m.workQueue.Forget(key)
	return true
}

// parseConfig loads and parses the configuration from ConfigMap
func (m *manager) parseConfig(key string) (*api.NetworkTopologyConfig, error) {
	_, _, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return nil, err
	}
	newConfig, err := m.configLoader.LoadConfig()
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, err
		}
		// set an empty config and should not return err because we should handle configMap deletion event.
		newConfig = &api.NetworkTopologyConfig{}
	}
	return newConfig, nil
}

// syncHandler handles the configuration update event.
func (m *manager) syncHandler(key string) error {
	klog.InfoS("Received configuration update")
	newConfig, err := m.parseConfig(key)
	if err != nil {
		return err
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	err = m.handleRemovedSources(newConfig)
	if err != nil {
		return err
	}

	// TODO: Only restart changed discoverers.
	for _, source := range newConfig.GetEnabledDiscoverySources() {
		klog.InfoS("Restarting network discovery", "source", source)
		if err = m.stopSingleDiscoverer(source); err != nil {
			return err
		}
		if err = m.startSingleDiscoverer(source); err != nil {
			return err
		}
	}

	// update the config for next compare.
	m.config = newConfig
	return nil
}

// handleRemovedSources stops discoverers sources that are no longer enabled
func (m *manager) handleRemovedSources(config *api.NetworkTopologyConfig) error {
	oldConfig := m.config

	oldSources := sets.Set[string]{}
	for _, source := range oldConfig.GetEnabledDiscoverySources() {
		oldSources.Insert(source)
	}

	newSources := sets.Set[string]{}
	for _, source := range config.GetEnabledDiscoverySources() {
		newSources.Insert(source)
	}

	for source := range oldSources.Difference(newSources) {
		klog.InfoS("Stopping network discovery", "source", source)
		if err := m.stopSingleDiscoverer(source); err != nil {
			return err
		}
	}
	return nil
}

// processTopology processes the topology data received from the discoverer
func (m *manager) processTopology(source string, topologyCh <-chan []*topologyv1alpha1.HyperNode) {
	for {
		select {
		case hyperNodes, ok := <-topologyCh:
			if !ok {
				klog.InfoS("Topology channel closed, stopping processor", "source", source)
				return
			}

			m.resultCh <- Result{
				HyperNodes: hyperNodes,
				Source:     source,
			}
			klog.V(3).InfoS("Forwarded discovery results to unified channel",
				"source", source,
				"nodeCount", len(hyperNodes))

		case <-m.stopCh:
			return
		}
	}
}
