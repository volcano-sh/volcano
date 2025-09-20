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
	"testing"
	"time"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	fakevcclientset "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
	"volcano.sh/volcano/pkg/controllers/hypernode/config"
	fakedisc "volcano.sh/volcano/pkg/controllers/hypernode/discovery/fake"
)

func TestManager_StartMultipleDiscoverers(t *testing.T) {
	// Prepare test data
	hyperNodesA := []*topologyv1alpha1.HyperNode{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ha1"}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ha2"}},
	}

	hyperNodesB := []*topologyv1alpha1.HyperNode{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "hb"}},
	}

	constructorA := api.DiscovererConstructor(func(cfg api.DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) api.Discoverer {
		return fakedisc.NewFakeDiscoverer(hyperNodesA, cfg)
	})
	constructorB := api.DiscovererConstructor(func(cfg api.DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) api.Discoverer {
		return fakedisc.NewFakeDiscoverer(hyperNodesB, cfg)
	})

	api.RegisterDiscoverer("sourceA", constructorA)
	api.RegisterDiscoverer("sourceB", constructorB)

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

	constructor := api.DiscovererConstructor(func(cfg api.DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) api.Discoverer {
		return fakedisc.NewFakeDiscoverer(hyperNodes, cfg)
	})

	api.RegisterDiscoverer("testSource", constructor)
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
	m := NewManager(loader, queue, fakeClient, fakeVcClient)

	// Start the manager
	err := m.Start()
	assert.NoError(t, err)

	// Enqueue a dummy key to trigger the sync handler
	queue.Add("test-namespace/test-config")

	// Give the manager some time to process the initial config
	time.Sleep(100 * time.Millisecond)

	//// Update the config with V2 version that disables the discoverer
	loader.SetConfig(discoveryConfigV2)
	// Enqueue the key again to trigger the sync handler with the updated config
	queue.Add("test-namespace/test-config")

	// Give the manager some time to process the updated config
	time.Sleep(100 * time.Millisecond)

	// Assert that the discoverer has been stopped
	mgr := m.(*manager)
	mgr.mutex.Lock()
	_, exists := mgr.discoverers["testSource"]
	mgr.mutex.Unlock()
	assert.False(t, exists, "Discoverer should be stopped")

	// Stop the manager
	m.Stop()
}
