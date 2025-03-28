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

package topoautodiscovery

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	fakevcclientset "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/framework"
)

type FakeCache struct {
	sync.RWMutex
	hyperNodeToGC     sets.Set[string]
	desiredHyperNodes sets.Set[*topologyv1alpha1.HyperNode]
}

func (c *FakeCache) OpenSession()  {}
func (c *FakeCache) CloseSession() {}
func (c *FakeCache) GetHyperNodeToSync() (hyperNode *topologyv1alpha1.HyperNode, done bool) {
	c.Lock()
	defer c.Unlock()
	node, ok := c.desiredHyperNodes.PopAny()
	return node, !ok

}

func (c *FakeCache) GetHyperNodeToGC() (hyperNode string, done bool) {
	c.Lock()
	defer c.Unlock()
	node, ok := c.hyperNodeToGC.PopAny()
	return node, !ok
}

var defaultNamespace = "volcano-system"

func newAutoDiscoveryController(vcClient vcclientset.Interface) *networkTopologyAutoDiscoveryController {
	kubeClientSet := fakekubeclientset.NewSimpleClientset()
	sharedInformerFactory := informers.NewSharedInformerFactory(kubeClientSet, 0)
	vcSharedInformerFactory := vcinformer.NewSharedInformerFactory(vcClient, 0)

	opt := &framework.ControllerOption{
		KubePodNamespace:        defaultNamespace,
		KubeClient:              kubeClientSet,
		VolcanoClient:           vcClient,
		SharedInformerFactory:   sharedInformerFactory,
		VCSharedInformerFactory: vcSharedInformerFactory,
		NetworkTopologyAutoDiscoveryWorkerThreads:   2,
		NetworkTopologyAutoDiscoveryGCWorkerThreads: 2,
		NetworkTopologyAutoDiscoverySyncPeriod:      time.Second,
	}

	controller := &networkTopologyAutoDiscoveryController{}
	controller.Initialize(opt)
	return controller
}

func newTypeHyperNode(name string, tier int) *topologyv1alpha1.HyperNode {
	hyperNode := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: topologyv1alpha1.HyperNodeSpec{
			Tier: tier,
		},
	}
	return hyperNode
}

func TestControllerSync(t *testing.T) {
	existedHyperNodes := []*topologyv1alpha1.HyperNode{
		newTypeHyperNode("hypernode1", 1),
		newTypeHyperNode("hypernode2", 1),
		newTypeHyperNode("hypernode3", 1),
	}

	testCases := []struct {
		name string

		hyperNodeToGCInCache      sets.Set[string]
		desiredHyperNodesCInCache sets.Set[*topologyv1alpha1.HyperNode]

		expectedNodeDeleteCount int
		expectedNodeCreateCount int
		expectedNodeUpdateCount int
	}{
		{
			// hypernodes in k8s:
			// hypernode1(tier 1), hypernode2(tier 1), hypernode3(tier 1)
			name:                 "case1 : update hypernode",
			hyperNodeToGCInCache: sets.Set[string]{},
			desiredHyperNodesCInCache: sets.New(
				newTypeHyperNode("hypernode1", 1),
				// update tier 1 to tier 2 for hypernode2
				newTypeHyperNode("hypernode2", 2),
				// update tier 1 to tier 2 for hypernode3
				newTypeHyperNode("hypernode3", 2)),
			expectedNodeUpdateCount: 2,
		},
		{
			// hypernodes in k8s:
			// hypernode1(tier 1), hypernode2(tier 2), hypernode3(tier 2)
			name: "case2: delete hypernode",
			// gc: hypernode1
			hyperNodeToGCInCache: sets.New("hypernode1"),
			desiredHyperNodesCInCache: sets.New(
				newTypeHyperNode("hypernode2", 2),
				newTypeHyperNode("hypernode3", 2)),
			expectedNodeDeleteCount: 1,
		},
		{
			// hypernodes in k8s:
			// hypernode2(tier 2), hypernode3(tier 2)
			name:                 "case3: create hypernode",
			hyperNodeToGCInCache: sets.Set[string]{},
			desiredHyperNodesCInCache: sets.New(
				newTypeHyperNode("hypernode2", 2),
				newTypeHyperNode("hypernode3", 2),
				// new hypernodes: hypernode4, hypernode5, hypernode6
				newTypeHyperNode("hypernode4", 1),
				newTypeHyperNode("hypernode5", 1),
				newTypeHyperNode("hypernode6", 1)),
			expectedNodeCreateCount: 3,
		},
		{
			// hypernodes in k8s:
			// hypernode2(tier 2), hypernode3(tier 2), hypernode4(tier 1), hypernode5(tier 1), hypernode6(tier 1)
			name: "case4: gc && sync",
			// gc: hypernode2, hypernode3
			hyperNodeToGCInCache: sets.New("hypernode2", "hypernode3"),
			desiredHyperNodesCInCache: sets.New(
				// update hypernodes: hypernode4, hypernode5, hypernode6
				// change tier from 1 to 2
				newTypeHyperNode("hypernode4", 2),
				newTypeHyperNode("hypernode5", 2),
				newTypeHyperNode("hypernode6", 2),
				// new hypernodes: hypernode7, hypernode8, hypernode9
				newTypeHyperNode("hypernode7", 1),
				newTypeHyperNode("hypernode8", 1),
				newTypeHyperNode("hypernode9", 1)),
			expectedNodeCreateCount: 3,
			expectedNodeDeleteCount: 2,
			expectedNodeUpdateCount: 3,
		},
	}

	stopCh := make(chan struct{})
	defer close(stopCh)
	vcClient := fakevcclientset.NewSimpleClientset()
	c := newAutoDiscoveryController(vcClient)
	c.sharedInformerFactory.Start(stopCh)
	c.vcSharedInformerFactory.Start(stopCh)
	c.sharedInformerFactory.WaitForCacheSync(stopCh)
	c.vcSharedInformerFactory.WaitForCacheSync(stopCh)

	hyperNodeStore := c.vcSharedInformerFactory.Topology().V1alpha1().HyperNodes().Informer().GetStore()

	for _, hyperNode := range existedHyperNodes {
		c.vcClient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
	}
	err := wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, 5*time.Second, false, func(ctx context.Context) (bool, error) {
		return len(hyperNodeStore.List()) == len(existedHyperNodes), nil
	})
	if err != nil {
		t.Fatal("failed to wait informers sync")
	}

	for _, tc := range testCases {
		vcClient.ClearActions()
		c.cache = &FakeCache{
			hyperNodeToGC:     tc.hyperNodeToGCInCache,
			desiredHyperNodes: tc.desiredHyperNodesCInCache,
		}
		c.Sync()
		actualCreateCount, actualDeleteCount, actualUpdateCount := 0, 0, 0
		for _, action := range vcClient.Actions() {
			if action.GetVerb() == "create" {
				actualCreateCount++
			}
			if action.GetVerb() == "delete" {
				actualDeleteCount++
			}
			if action.GetVerb() == "update" {
				actualUpdateCount++
			}
		}
		assert.Equal(t, tc.expectedNodeCreateCount, actualCreateCount, tc.name, "create action")
		assert.Equal(t, tc.expectedNodeDeleteCount, actualDeleteCount, tc.name, "delete action")
		assert.Equal(t, tc.expectedNodeUpdateCount, actualUpdateCount, tc.name, "update action")
	}
}
