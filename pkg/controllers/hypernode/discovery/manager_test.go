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
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	fakevcclientset "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
	"volcano.sh/volcano/pkg/controllers/hypernode/config"
	fakedisc "volcano.sh/volcano/pkg/controllers/hypernode/discovery/fake"
)

type acknowledgementDiscoverer struct {
	synced atomic.Int32
}

func (*acknowledgementDiscoverer) Start() (chan []*topologyv1alpha1.HyperNode, error) {
	return make(chan []*topologyv1alpha1.HyperNode), nil
}

func (*acknowledgementDiscoverer) Stop() error     { return nil }
func (*acknowledgementDiscoverer) Name() string    { return "acknowledgement" }
func (d *acknowledgementDiscoverer) ResultSynced() { d.synced.Add(1) }

func TestResultSyncedTargetsProducingDiscoverer(t *testing.T) {
	producer := &acknowledgementDiscoverer{}
	topologyCh := make(chan []*topologyv1alpha1.HyperNode, 1)
	m := &manager{
		resultCh:   make(chan Result),
		stopCh:     make(chan struct{}),
		pendingAck: make(map[string]*resultAcknowledgement),
	}
	processorStopCh := make(chan struct{})
	processorDone := make(chan struct{})
	m.workerWG.Add(1)
	go m.processTopology("same-source", producer, topologyCh, processorStopCh, processorDone)
	topologyCh <- []*topologyv1alpha1.HyperNode{{ObjectMeta: metav1.ObjectMeta{Name: "test"}}}
	result := <-m.resultCh

	// The acknowledgement is bound to the in-flight result and remains
	// idempotent even when called more than once.
	m.ResultSynced(result.Source)
	m.ResultSynced(result.Source)

	if got := producer.synced.Load(); got != 1 {
		t.Fatalf("producer acknowledgement count = %d, want 1", got)
	}

	// A subsequent in-flight result is independently bound to the same
	// producing instance.
	topologyCh <- []*topologyv1alpha1.HyperNode{{ObjectMeta: metav1.ObjectMeta{Name: "legacy"}}}
	legacyResult := <-m.resultCh
	m.ResultSynced(legacyResult.Source)
	if got := producer.synced.Load(); got != 2 {
		t.Fatalf("producer acknowledgement count = %d, want 2", got)
	}
	close(processorStopCh)
	<-processorDone
	m.workerWG.Wait()
}

func TestStopDiscovererWaitsForDeliveredResultAndDropsBufferedResults(t *testing.T) {
	producer := &acknowledgementDiscoverer{}
	topologyCh := make(chan []*topologyv1alpha1.HyperNode, 2)
	processorStopCh := make(chan struct{})
	processorDone := make(chan struct{})
	m := &manager{
		discoverers:     map[string]api.Discoverer{"same-source": producer},
		processorStopCh: map[string]chan struct{}{"same-source": processorStopCh},
		processorDone:   map[string]chan struct{}{"same-source": processorDone},
		pendingAck:      make(map[string]*resultAcknowledgement),
		resultCh:        make(chan Result),
		stopCh:          make(chan struct{}),
	}
	m.workerWG.Add(1)
	go m.processTopology("same-source", producer, topologyCh, processorStopCh, processorDone)
	topologyCh <- []*topologyv1alpha1.HyperNode{{ObjectMeta: metav1.ObjectMeta{Name: "delivered"}}}
	topologyCh <- []*topologyv1alpha1.HyperNode{{ObjectMeta: metav1.ObjectMeta{Name: "buffered"}}}
	result := <-m.resultCh

	stopDone := make(chan error, 1)
	go func() { stopDone <- m.stopSingleDiscoverer(result.Source) }()
	select {
	case err := <-stopDone:
		t.Fatalf("stop completed before delivered result was acknowledged: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	m.ResultSynced(result.Source)
	select {
	case err := <-stopDone:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("stop did not complete after result acknowledgement")
	}
	select {
	case result := <-m.resultCh:
		t.Fatalf("old discoverer forwarded buffered result after stop: %s", result.HyperNodes[0].Name)
	case <-time.After(50 * time.Millisecond):
	}
	m.workerWG.Wait()
}

func TestManager_StartMultipleDiscoverers(t *testing.T) {
	// Prepare test data
	hyperNodesA := []*topologyv1alpha1.HyperNode{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ha1"}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ha2"}},
	}

	hyperNodesB := []*topologyv1alpha1.HyperNode{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "hb"}},
	}

	constructorA := api.DiscovererOptionsConstructor(func(cfg api.DiscoveryConfig, options api.DiscovererOptions) (api.Discoverer, error) {
		return fakedisc.NewFakeDiscoverer(hyperNodesA, cfg), nil
	})
	constructorB := api.DiscovererOptionsConstructor(func(cfg api.DiscoveryConfig, options api.DiscovererOptions) (api.Discoverer, error) {
		return fakedisc.NewFakeDiscoverer(hyperNodesB, cfg), nil
	})

	api.RegisterDiscovererWithOptions("sourceA", constructorA)
	api.RegisterDiscovererWithOptions("sourceB", constructorB)

	discoveryConfig := &api.NetworkTopologyConfig{
		NetworkTopologyDiscovery: []api.DiscoveryConfig{
			{
				Source:  "sourceA",
				Enabled: true,
			},
			{
				Source:  "sourceB",
				Enabled: true,
			},
		},
	}
	loader := config.NewFakeLoader(discoveryConfig)

	// Create manager
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	queue.Add("test-namespace/test-config")
	fakeClient := fake.NewSimpleClientset()
	fakeVcClient := fakevcclientset.NewSimpleClientset()
	m := NewManager(loader, queue, fakeClient, fakeVcClient)
	err := m.Start()
	assert.NoError(t, err)

	timeout := time.After(time.Second)

	for i := 0; i < 2; i++ {
		select {
		case result := <-m.ResultChannel():
			if result.Source == "sourceA" {
				assert.Equal(t, 2, len(result.HyperNodes))
				assert.Equal(t, "ha1", result.HyperNodes[0].Name)
				assert.Equal(t, "ha2", result.HyperNodes[1].Name)
			} else if result.Source == "sourceB" {
				assert.Equal(t, 1, len(result.HyperNodes))
				assert.Equal(t, "hb", result.HyperNodes[0].Name)
			}
			m.ResultSynced(result.Source)
		case <-timeout:
			t.Fatal("Test timed out waiting for results")
		}
	}
	mgr := m.(*manager)
	mgr.mutex.Lock()
	assert.Equal(t, discoveryConfig, mgr.config)
	mgr.mutex.Unlock()
	// Stop manager
	m.Stop()
}

func TestManager_syncHandler(t *testing.T) {
	// Prepare test data
	hyperNodes := []*topologyv1alpha1.HyperNode{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ha1"}},
	}

	constructor := api.DiscovererOptionsConstructor(func(cfg api.DiscoveryConfig, options api.DiscovererOptions) (api.Discoverer, error) {
		return fakedisc.NewFakeDiscoverer(hyperNodes, cfg), nil
	})

	api.RegisterDiscovererWithOptions("testSource", constructor)
	discoveryConfigV1 := &api.NetworkTopologyConfig{
		NetworkTopologyDiscovery: []api.DiscoveryConfig{
			{
				Source:  "testSource",
				Enabled: true,
				Config: map[string]interface{}{
					"key": "value",
				},
			},
		},
	}
	discoveryConfigV2 := &api.NetworkTopologyConfig{
		NetworkTopologyDiscovery: []api.DiscoveryConfig{
			{
				Source:  "testSource",
				Enabled: false,
				Config: map[string]interface{}{
					"key": "value",
				},
			},
		},
	}

	loader := config.NewFakeLoader(discoveryConfigV1)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	fakeClient := fake.NewSimpleClientset()
	fakeVcClient := fakevcclientset.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(fakeClient, 0)
	vcInformerFactory := vcinformer.NewSharedInformerFactory(fakeVcClient, 0)
	m := NewManagerWithInformers(loader, queue, fakeClient, fakeVcClient, informerFactory.Core().V1().Nodes(), vcInformerFactory.Topology().V1alpha1().HyperNodes())

	// Start the manager
	err := m.Start()
	assert.NoError(t, err)

	// Enqueue a dummy key to trigger the sync handler
	queue.Add("test-namespace/test-config")

	var result Result
	select {
	case result = <-m.ResultChannel():
		m.ResultSynced(result.Source)
	case <-time.After(time.Second):
		t.Fatal("Test timed out waiting for initial discovery result")
	}

	//// Update the config with V2 version that disables the discoverer
	loader.SetConfig(discoveryConfigV2)
	// Enqueue the key again to trigger the sync handler with the updated config
	queue.Add("test-namespace/test-config")

	// Assert that the discoverer has been stopped
	mgr := m.(*manager)
	assert.Eventually(t, func() bool {
		mgr.discovererMutex.RLock()
		defer mgr.discovererMutex.RUnlock()
		_, exists := mgr.discoverers["testSource"]
		return !exists
	}, time.Second, 10*time.Millisecond, "Discoverer should be stopped")

	// Stop the manager
	m.Stop()
}

func TestManager_UnchangedSourceConfigSkipsRestart(t *testing.T) {
	var startCount int32

	constructor := api.DiscovererConstructor(func(cfg api.DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) api.Discoverer {
		atomic.AddInt32(&startCount, 1)
		return fakedisc.NewFakeDiscoverer([]*topologyv1alpha1.HyperNode{}, cfg)
	})
	api.RegisterDiscoverer("unchangedSrc", constructor)

	discoveryConfig := &api.NetworkTopologyConfig{
		NetworkTopologyDiscovery: []api.DiscoveryConfig{
			{
				Source:  "unchangedSrc",
				Enabled: true,
				Config:  map[string]interface{}{"key": "value"},
			},
		},
	}
	loader := config.NewFakeLoader(discoveryConfig)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	fakeClient := fake.NewSimpleClientset()
	fakeVcClient := fakevcclientset.NewSimpleClientset()
	m := NewManager(loader, queue, fakeClient, fakeVcClient)

	err := m.Start()
	assert.NoError(t, err)

	// First sync: new source not yet running, should start the discoverer
	queue.Add("test-namespace/test-config")
	timeout := time.After(time.Second)
	select {
	case <-m.ResultChannel():
	case <-timeout:
		t.Fatal("timed out waiting for first result")
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&startCount), "discoverer should start once on first sync")

	// Second sync with same config: discoverer is running and config unchanged, should skip restart
	queue.Add("test-namespace/test-config")
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&startCount), "discoverer should not restart when config is unchanged")

	m.Stop()
}

func TestManager_ChangedSourceConfigRestartsDiscoverer(t *testing.T) {
	var startCount int32

	constructor := api.DiscovererConstructor(func(cfg api.DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) api.Discoverer {
		atomic.AddInt32(&startCount, 1)
		return fakedisc.NewFakeDiscoverer([]*topologyv1alpha1.HyperNode{}, cfg)
	})
	api.RegisterDiscoverer("changedSrc", constructor)

	discoveryConfigV1 := &api.NetworkTopologyConfig{
		NetworkTopologyDiscovery: []api.DiscoveryConfig{
			{
				Source:  "changedSrc",
				Enabled: true,
				Config:  map[string]interface{}{"key": "value1"},
			},
		},
	}
	discoveryConfigV2 := &api.NetworkTopologyConfig{
		NetworkTopologyDiscovery: []api.DiscoveryConfig{
			{
				Source:  "changedSrc",
				Enabled: true,
				Config:  map[string]interface{}{"key": "value2"},
			},
		},
	}

	loader := config.NewFakeLoader(discoveryConfigV1)
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())
	fakeClient := fake.NewSimpleClientset()
	fakeVcClient := fakevcclientset.NewSimpleClientset()
	m := NewManager(loader, queue, fakeClient, fakeVcClient)

	err := m.Start()
	assert.NoError(t, err)

	// First sync: should start the discoverer
	queue.Add("test-namespace/test-config")
	timeout := time.After(time.Second)
	select {
	case <-m.ResultChannel():
	case <-timeout:
		t.Fatal("timed out waiting for first result")
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&startCount), "discoverer should start once on first sync")

	// Second sync with updated config: should restart the discoverer
	loader.SetConfig(discoveryConfigV2)
	queue.Add("test-namespace/test-config")
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(2), atomic.LoadInt32(&startCount), "discoverer should restart when config changes")

	m.Stop()
}
