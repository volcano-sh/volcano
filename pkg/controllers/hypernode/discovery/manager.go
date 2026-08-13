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
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	coreinformerv1 "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	topologyinformerv1alpha1 "volcano.sh/apis/pkg/client/informers/externalversions/topology/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
	"volcano.sh/volcano/pkg/controllers/hypernode/config"

	_ "volcano.sh/volcano/pkg/controllers/hypernode/discovery/label"
	_ "volcano.sh/volcano/pkg/controllers/hypernode/discovery/ufm"
)

type Result struct {
	// HyperNodes contains the discovered hypernodes
	HyperNodes []*topologyv1alpha1.HyperNode
	// Source indicates the source of the discovery
	Source string
}

type resultAcknowledgement struct {
	once       sync.Once
	discoverer api.Discoverer
	done       chan struct{}
}

func newResultAcknowledgement(discoverer api.Discoverer) *resultAcknowledgement {
	return &resultAcknowledgement{discoverer: discoverer, done: make(chan struct{})}
}

func (a *resultAcknowledgement) mark() {
	a.once.Do(func() {
		a.discoverer.ResultSynced()
		// A hot reload waits for the producing discoverer's acknowledgement
		// callback to finish before stopping and replacing that instance.
		close(a.done)
	})
}

// Manager is the interface for managing network topology discovery
type Manager interface {
	// Start initializes and starts the topology discovery manager
	Start() error
	// Stop halts all discovery processes
	Stop()
	// ResultChannel returns a channel for receiving discovery results
	ResultChannel() <-chan Result

	// ResultSynced every time the Result in ResultChannel are processed, this method must be called to notify network topology discover
	ResultSynced(source string)
}

// manager manages network topology discovery processes
type manager struct {
	mutex           sync.Mutex
	lifecycleMutex  sync.Mutex
	discovererMutex sync.RWMutex
	stopOnce        sync.Once
	startOnce       sync.Once
	startErr        error
	managerWG       sync.WaitGroup
	workerWG        sync.WaitGroup
	ctx             context.Context
	cancel          context.CancelFunc

	configLoader config.Loader
	config       *api.NetworkTopologyConfig

	discoverers map[string]api.Discoverer
	// processorStopCh and processorDone bind each result forwarder to the
	// discoverer instance registered for the same source.
	processorStopCh map[string]chan struct{}
	processorDone   map[string]chan struct{}
	pendingAck      map[string]*resultAcknowledgement
	workQueue       workqueue.TypedRateLimitingInterface[string]
	stopCh          chan struct{}

	kubeClient        clientset.Interface
	vcClient          vcclientset.Interface
	nodeInformer      coreinformerv1.NodeInformer
	hyperNodeInformer topologyinformerv1alpha1.HyperNodeInformer

	resultCh chan Result
}

// NewManager creates a discovery manager using the legacy client-only contract.
// Discoverers retain their original responsibility for any informers they need.
func NewManager(configLoader config.Loader, queue workqueue.TypedRateLimitingInterface[string], kubeClient clientset.Interface, vcClient vcclientset.Interface) Manager {
	return newManager(configLoader, queue, kubeClient, vcClient, nil, nil)
}

// NewManagerWithInformers creates a discovery manager with process-provided shared informers.
func NewManagerWithInformers(configLoader config.Loader, queue workqueue.TypedRateLimitingInterface[string], kubeClient clientset.Interface, vcClient vcclientset.Interface,
	nodeInformer coreinformerv1.NodeInformer, hyperNodeInformer topologyinformerv1alpha1.HyperNodeInformer) Manager {
	return newManager(configLoader, queue, kubeClient, vcClient, nodeInformer, hyperNodeInformer)
}

func newManager(configLoader config.Loader, queue workqueue.TypedRateLimitingInterface[string], kubeClient clientset.Interface, vcClient vcclientset.Interface,
	nodeInformer coreinformerv1.NodeInformer, hyperNodeInformer topologyinformerv1alpha1.HyperNodeInformer) *manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &manager{
		ctx:               ctx,
		cancel:            cancel,
		configLoader:      configLoader,
		discoverers:       make(map[string]api.Discoverer),
		processorStopCh:   make(map[string]chan struct{}),
		processorDone:     make(map[string]chan struct{}),
		pendingAck:        make(map[string]*resultAcknowledgement),
		resultCh:          make(chan Result),
		stopCh:            make(chan struct{}),
		workQueue:         queue,
		kubeClient:        kubeClient,
		vcClient:          vcClient,
		nodeInformer:      nodeInformer,
		hyperNodeInformer: hyperNodeInformer,
	}
}

// Start initializes and starts the topology discovery manager
func (m *manager) Start() error {
	m.startOnce.Do(func() {
		m.lifecycleMutex.Lock()
		defer m.lifecycleMutex.Unlock()
		cfg, err := m.configLoader.LoadConfig()
		if err != nil {
			klog.ErrorS(err, "Failed to load config")
			// Initialize with an empty config to avoid nil pointer dereference.
			m.config = &api.NetworkTopologyConfig{}
			// Do not return an error here, in case of configMap is updated correctly later.
		} else if cfg == nil {
			klog.ErrorS(nil, "Config loader returned an empty config")
			m.config = &api.NetworkTopologyConfig{}
		} else {
			m.config = cfg
		}
		select {
		case <-m.stopCh:
			m.startErr = fmt.Errorf("network topology discovery manager has already been stopped")
			return
		default:
		}
		m.managerWG.Add(1)
		go func() {
			defer m.managerWG.Done()
			m.worker()
		}()
		klog.InfoS("Network topology discovery manager started")
	})
	return m.startErr
}

// Stop halts all discovery processes
func (m *manager) Stop() {
	m.stopOnce.Do(func() {
		m.lifecycleMutex.Lock()
		m.cancel()
		close(m.stopCh)
		m.workQueue.ShutDown()
		m.lifecycleMutex.Unlock()
		m.managerWG.Wait()
		m.stopAllDiscoverers()
		m.workerWG.Wait()
		close(m.resultCh)
		klog.InfoS("Network topology discovery manager stopped")
	})
}

// ResultSynced acknowledges the in-flight result for a source. The manager
// binds the acknowledgement to the discoverer instance that produced it.
func (m *manager) ResultSynced(source string) {
	m.discovererMutex.RLock()
	acknowledgement := m.pendingAck[source]
	m.discovererMutex.RUnlock()
	if acknowledgement != nil {
		acknowledgement.mark()
		return
	}
	klog.InfoS("No in-flight discovery result to acknowledge", "source", source)
}

func (m *manager) ResultChannel() <-chan Result {
	return m.resultCh
}

// startSingleDiscoverer start a single network topology discoverer.
func (m *manager) startSingleDiscoverer(source string) error {
	select {
	case <-m.stopCh:
		return fmt.Errorf("network topology discovery manager has been stopped")
	default:
	}
	cfg, err := m.configLoader.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}
	if cfg == nil {
		return fmt.Errorf("failed to load config: loader returned nil")
	}
	discoveryCfg := cfg.GetDiscoveryConfig(source)
	if discoveryCfg == nil {
		return fmt.Errorf("configuration not found for network topology discovery source: %s", source)
	}

	discoverer, err := api.NewDiscovererWithOptions(*discoveryCfg, api.DiscovererOptions{
		Context:    m.ctx,
		KubeClient: m.kubeClient, VolcanoClient: m.vcClient,
		NodeInformer: m.nodeInformer, HyperNodeInformer: m.hyperNodeInformer,
	})
	if err != nil {
		return fmt.Errorf("failed to create discoverer: %v", err)
	}
	if discoverer == nil {
		return fmt.Errorf("failed to create discoverer: constructor for source %s returned nil", source)
	}

	outputCh, err := discoverer.Start()
	if err != nil {
		if stopErr := discoverer.Stop(); stopErr != nil {
			klog.ErrorS(stopErr, "Failed to clean up discoverer after start failure", "source", source)
		}
		return fmt.Errorf("failed to start discoverer: %v", err)
	}

	processorStopCh := make(chan struct{})
	processorDone := make(chan struct{})
	m.discovererMutex.Lock()
	m.discoverers[source] = discoverer
	m.processorStopCh[source] = processorStopCh
	m.processorDone[source] = processorDone
	m.discovererMutex.Unlock()
	m.workerWG.Add(1)
	go m.processTopology(source, discoverer, outputCh, processorStopCh, processorDone)

	klog.InfoS("Started network topology discoverer", "source", source)
	return nil
}

func (m *manager) stopAllDiscoverers() {
	m.discovererMutex.RLock()
	sources := make([]string, 0, len(m.discoverers))
	for source := range m.discoverers {
		sources = append(sources, source)
	}
	m.discovererMutex.RUnlock()

	for _, source := range sources {
		if err := m.stopSingleDiscoverer(source); err != nil {
			klog.ErrorS(err, "Failed to stop discoverer", "source", source)
		}
	}
}

func (m *manager) stopSingleDiscoverer(source string) error {
	m.discovererMutex.Lock()
	discoverer, exists := m.discoverers[source]
	if !exists {
		m.discovererMutex.Unlock()
		klog.InfoS("No need to stop discoverer as it may not start yet", "source", source)
		return nil
	}
	processorStopCh := m.processorStopCh[source]
	processorDone := m.processorDone[source]
	// Stop accepting results from this instance before asking the producer to
	// stop. Any result already delivered remains bound to this instance and is
	// acknowledged before the processor exits during a configuration reload.
	close(processorStopCh)
	m.discovererMutex.Unlock()

	stopErr := discoverer.Stop()
	// Do not start a replacement discoverer until the old forwarder has either
	// received acknowledgement for its delivered result or discarded it during
	// process shutdown.
	<-processorDone

	m.discovererMutex.Lock()
	delete(m.discoverers, source)
	delete(m.processorStopCh, source)
	delete(m.processorDone, source)
	delete(m.pendingAck, source)
	m.discovererMutex.Unlock()
	return stopErr
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
	if newConfig == nil {
		return nil, fmt.Errorf("config loader returned nil")
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
		select {
		case <-m.stopCh:
			return nil
		default:
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
	if oldConfig == nil {
		oldConfig = &api.NetworkTopologyConfig{}
	}

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
func (m *manager) processTopology(source string, discoverer api.Discoverer, topologyCh <-chan []*topologyv1alpha1.HyperNode,
	processorStopCh <-chan struct{}, processorDone chan<- struct{}) {
	defer m.workerWG.Done()
	defer close(processorDone)
	for {
		select {
		case hyperNodes, ok := <-topologyCh:
			if !ok {
				klog.InfoS("Topology channel closed, stopping processor", "source", source)
				return
			}

			acknowledgement := newResultAcknowledgement(discoverer)
			m.discovererMutex.Lock()
			m.pendingAck[source] = acknowledgement
			m.discovererMutex.Unlock()
			clearPendingAck := func() {
				m.discovererMutex.Lock()
				if m.pendingAck[source] == acknowledgement {
					delete(m.pendingAck, source)
				}
				m.discovererMutex.Unlock()
			}
			select {
			case m.resultCh <- Result{
				HyperNodes: hyperNodes,
				Source:     source,
			}:
			case <-processorStopCh:
				clearPendingAck()
				return
			case <-m.stopCh:
				clearPendingAck()
				return
			}
			klog.V(3).InfoS("Forwarded discovery results to unified channel",
				"source", source,
				"nodeCount", len(hyperNodes))
			// Preserve result ordering and ensure a hot-reload cannot start a
			// replacement instance while an old result is still reconciling.
			select {
			case <-acknowledgement.done:
			case <-m.stopCh:
				clearPendingAck()
				return
			}
			clearPendingAck()
			// A configuration reload closes the per-instance processor channel.
			// Exit after the delivered result is fully acknowledged instead of
			// consuming buffered output from the old discoverer generation.
			select {
			case <-processorStopCh:
				return
			default:
			}

		case <-processorStopCh:
			return
		case <-m.stopCh:
			return
		}
	}
}
