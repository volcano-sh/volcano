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

package hypernode

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/framework"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
	"volcano.sh/volcano/pkg/controllers/hypernode/config"
	"volcano.sh/volcano/pkg/controllers/hypernode/discovery"
)

type mockDiscoveryManager struct {
	startCalled bool
	stopCalled  bool
	resultCh    chan discovery.Result
	syncedCh    chan string
	syncedCount int

	mu sync.Mutex
}

func (m *mockDiscoveryManager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.startCalled = true
	return nil
}

func (m *mockDiscoveryManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.stopCalled = true
	close(m.resultCh)
}

func (m *mockDiscoveryManager) ResultSynced(source string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncedCount++
	if m.syncedCh != nil {
		m.syncedCh <- source
	}
}

func (m *mockDiscoveryManager) ResultChannel() <-chan discovery.Result {
	return m.resultCh
}

func TestWatchDiscoveryResultsRetriesBeforeAcknowledgement(t *testing.T) {
	existing := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rack-a",
			Labels: map[string]string{
				api.NetworkTopologySourceLabelKey: "label",
			},
		},
	}
	fakeVcClient := vcclientset.NewSimpleClientset(existing.DeepCopy())
	vcInformerFactory := vcinformer.NewSharedInformerFactory(fakeVcClient, 0)
	hyperNodeInformer := vcInformerFactory.Topology().V1alpha1().HyperNodes()
	assert.NoError(t, hyperNodeInformer.Informer().GetIndexer().Add(existing.DeepCopy()))

	var updateAttempts atomic.Int32
	fakeVcClient.Fake.PrependReactor("update", "hypernodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if updateAttempts.Add(1) == 1 {
			return true, nil, errors.New("transient update failure")
		}
		return false, nil, nil
	})

	mockManager := &mockDiscoveryManager{
		resultCh: make(chan discovery.Result),
		syncedCh: make(chan string, 1),
	}
	controller := &hyperNodeController{
		vcClient:         fakeVcClient,
		hyperNodeLister:  hyperNodeInformer.Lister(),
		discoveryManager: mockManager,
	}
	stopCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		controller.watchDiscoveryResults(stopCh)
	}()

	mockManager.resultCh <- discovery.Result{
		Source: "label",
		HyperNodes: []*topologyv1alpha1.HyperNode{{
			ObjectMeta: metav1.ObjectMeta{Name: "rack-a"},
			Spec: topologyv1alpha1.HyperNodeSpec{
				Tier: 1,
			},
		}},
	}

	select {
	case source := <-mockManager.syncedCh:
		assert.Equal(t, "label", source)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for discovery result acknowledgement")
	}
	assert.GreaterOrEqual(t, updateAttempts.Load(), int32(2))
	mockManager.mu.Lock()
	assert.Equal(t, 1, mockManager.syncedCount)
	mockManager.mu.Unlock()

	close(mockManager.resultCh)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for discovery result watcher to stop")
	}
}

func TestWatchDiscoveryResultsDoesNotBlockOtherSources(t *testing.T) {
	existing := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rack-a",
			Labels: map[string]string{
				api.NetworkTopologySourceLabelKey: "label",
			},
		},
	}
	fakeVcClient := vcclientset.NewSimpleClientset(existing.DeepCopy())
	vcInformerFactory := vcinformer.NewSharedInformerFactory(fakeVcClient, 0)
	hyperNodeInformer := vcInformerFactory.Topology().V1alpha1().HyperNodes()
	assert.NoError(t, hyperNodeInformer.Informer().GetIndexer().Add(existing.DeepCopy()))
	fakeVcClient.Fake.PrependReactor("update", "hypernodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("persistent update failure")
	})

	mockManager := &mockDiscoveryManager{
		resultCh: make(chan discovery.Result),
		syncedCh: make(chan string, 1),
	}
	controller := &hyperNodeController{
		vcClient:         fakeVcClient,
		hyperNodeLister:  hyperNodeInformer.Lister(),
		discoveryManager: mockManager,
	}
	stopCh := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		controller.watchDiscoveryResults(stopCh)
	}()

	mockManager.resultCh <- discovery.Result{
		Source:     "label",
		HyperNodes: []*topologyv1alpha1.HyperNode{{ObjectMeta: metav1.ObjectMeta{Name: "rack-a"}}},
	}
	mockManager.resultCh <- discovery.Result{Source: "ufm", HyperNodes: nil}

	select {
	case source := <-mockManager.syncedCh:
		assert.Equal(t, "ufm", source)
	case <-time.After(time.Second):
		t.Fatal("a failing discovery source blocked acknowledgement of another source")
	}

	close(stopCh)
	close(mockManager.resultCh)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for discovery result watcher to stop")
	}
}

func TestHyperNodeController_Run(t *testing.T) {
	stopCh := make(chan struct{})

	fakeVcClient := vcclientset.NewSimpleClientset()
	fakeKubeClient := k8sfake.NewSimpleClientset()

	existingHyperNodes := []*topologyv1alpha1.HyperNode{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "existing-node-1",
				Labels: map[string]string{
					api.NetworkTopologySourceLabelKey: "ufm",
				},
			},
			Spec: topologyv1alpha1.HyperNodeSpec{
				Members: []topologyv1alpha1.MemberSpec{
					{
						Type: topologyv1alpha1.MemberTypeNode,
						Selector: topologyv1alpha1.MemberSelector{
							ExactMatch: &topologyv1alpha1.ExactMatch{Name: "existing-node-1"},
						},
					},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "existing-node-2",
				Labels: map[string]string{
					api.NetworkTopologySourceLabelKey: "ufm",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "existing-node-3",
				Labels: map[string]string{
					api.NetworkTopologySourceLabelKey: "roce",
				},
			},
		},
	}

	for _, node := range existingHyperNodes {
		_, err := fakeVcClient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), node, metav1.CreateOptions{})
		assert.NoError(t, err, "Should be able to create the existing HyperNode")
	}

	vcInformerFactory := vcinformer.NewSharedInformerFactory(fakeVcClient, 0)
	kubeInformerFactory := informers.NewSharedInformerFactory(fakeKubeClient, 0)

	mockManager := &mockDiscoveryManager{
		resultCh: make(chan discovery.Result),
	}

	controller := &hyperNodeController{
		vcClient:           fakeVcClient,
		kubeClient:         fakeKubeClient,
		vcInformerFactory:  vcInformerFactory,
		informerFactory:    kubeInformerFactory,
		hyperNodeInformer:  vcInformerFactory.Topology().V1alpha1().HyperNodes(),
		hyperNodeLister:    vcInformerFactory.Topology().V1alpha1().HyperNodes().Lister(),
		configMapInformer:  kubeInformerFactory.Core().V1().ConfigMaps(),
		configMapLister:    kubeInformerFactory.Core().V1().ConfigMaps().Lister(),
		discoveryManager:   mockManager,
		configMapNamespace: "test-namespace",
		configMapName:      "test-release-controller-configmap",
		hyperNodeQueue:     workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
	}

	go controller.Run(stopCh)

	time.Sleep(time.Second)
	assert.True(t, func() bool { mockManager.mu.Lock(); defer mockManager.mu.Unlock(); return mockManager.startCalled }(), "Discovery manager should be started")

	// phase1: update and create hypernode
	go func() {
		updatedHyperNode := &topologyv1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: "existing-node-1",
			},
			Spec: topologyv1alpha1.HyperNodeSpec{
				Members: []topologyv1alpha1.MemberSpec{
					{
						Type: topologyv1alpha1.MemberTypeNode,
						Selector: topologyv1alpha1.MemberSelector{
							ExactMatch: &topologyv1alpha1.ExactMatch{Name: "updated-node-1"},
						},
					},
				},
			},
		}

		newHyperNode := &topologyv1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: "new-hypernode",
			},
			Spec: topologyv1alpha1.HyperNodeSpec{},
		}

		mockManager.resultCh <- discovery.Result{
			Source:     "ufm",
			HyperNodes: []*topologyv1alpha1.HyperNode{updatedHyperNode, newHyperNode},
		}
	}()

	time.Sleep(300 * time.Millisecond)

	// verify if the existing HyperNode is updated
	updatedNode, err := fakeVcClient.TopologyV1alpha1().HyperNodes().Get(context.TODO(), "existing-node-1", metav1.GetOptions{})
	assert.NoError(t, err, "Should be able to get the updated HyperNode")
	assert.Equal(t, "updated-node-1", updatedNode.Spec.Members[0].Selector.ExactMatch.Name)

	// verify if the new HyperNode is created
	_, err = fakeVcClient.TopologyV1alpha1().HyperNodes().Get(context.TODO(), "new-hypernode", metav1.GetOptions{})
	assert.NoError(t, err, "Should be able to get the created HyperNode")

	// phase2: delete hypernode
	go func() {
		mockManager.resultCh <- discovery.Result{
			Source:     "ufm",
			HyperNodes: []*topologyv1alpha1.HyperNode{},
		}
	}()

	time.Sleep(300 * time.Millisecond)

	// verify if the existing HyperNode with source match is deleted
	nodeList, err := fakeVcClient.TopologyV1alpha1().HyperNodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			api.NetworkTopologySourceLabelKey: "ufm",
		}).String(),
	})
	assert.NoError(t, err)
	assert.Equal(t, 0, len(nodeList.Items), "All HyperNodes should have been deleted")

	// verify if the existing HyperNode with source match is deleted
	nodeList, err = fakeVcClient.TopologyV1alpha1().HyperNodes().List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			api.NetworkTopologySourceLabelKey: "roce",
		}).String(),
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(nodeList.Items), "HyperNodes from different discovery sources should not be deleted")

	close(stopCh)
	time.Sleep(100 * time.Millisecond)
	assert.True(t, func() bool { mockManager.mu.Lock(); defer mockManager.mu.Unlock(); return mockManager.stopCalled }(), "Discovery manager should be stopped")
}

func TestHyperNodeController_Initialize(t *testing.T) {
	os.Setenv(config.NamespaceEnvKey, "test-namespace")
	os.Setenv(config.ReleaseNameEnvKey, "test-release")
	defer func() {
		os.Unsetenv(config.NamespaceEnvKey)
		os.Unsetenv(config.ReleaseNameEnvKey)
	}()

	fakeVcClient := vcclientset.NewSimpleClientset()
	fakeKubeClient := k8sfake.NewSimpleClientset()
	vcInformerFactory := vcinformer.NewSharedInformerFactory(fakeVcClient, 0)
	kubeInformerFactory := informers.NewSharedInformerFactory(fakeKubeClient, 0)

	controller := &hyperNodeController{
		informerFactory: kubeInformerFactory,
	}

	err := controller.Initialize(&framework.ControllerOption{
		VolcanoClient:           fakeVcClient,
		KubeClient:              fakeKubeClient,
		VCSharedInformerFactory: vcInformerFactory,
		SharedInformerFactory:   kubeInformerFactory,
	})

	assert.NoError(t, err)
	assert.Equal(t, fakeVcClient, controller.vcClient)
	assert.Equal(t, fakeKubeClient, controller.kubeClient)
	assert.Equal(t, vcInformerFactory, controller.vcInformerFactory)
	assert.NotNil(t, controller.hyperNodeInformer)
	assert.NotNil(t, controller.hyperNodeLister)
	assert.NotNil(t, controller.discoveryManager)
	assert.NotNil(t, controller.configMapQueue)
	assert.Equal(t, "test-namespace", controller.configMapNamespace)
	assert.Equal(t, "test-release-controller-configmap", controller.configMapName)
}
