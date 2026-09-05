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

package label

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
	"volcano.sh/volcano/pkg/controllers/hypernode/utils"
)

func TestNewLabelDiscoverer_start(t *testing.T) {
	tests := []struct {
		name                 string
		config               api.DiscoveryConfig
		nodes                map[string]*corev1.Node
		existHyperNode       map[string]*topologyv1alpha1.HyperNode
		expectedHyperNodeMap map[string]*topologyv1alpha1.HyperNode
	}{
		{
			name:                 "test1",
			config:               getCfg(),
			nodes:                expectedNodeForTest1(),
			expectedHyperNodeMap: expectedHyperNodesForTest1(),
		},
		// Some nodes only have tier1 labels and do not have tier2 labels.
		{
			name:                 "test2",
			config:               getCfg(),
			nodes:                expectedNodeForTest2(),
			expectedHyperNodeMap: expectedHyperNodesForTest2(),
		},
		// Some nodes only have tier2 labels and do not have tier1 labels.
		{
			name:                 "test3",
			config:               getCfg(),
			nodes:                expectedNodeForTest3(),
			expectedHyperNodeMap: expectedHyperNodesForTest3(),
		},
		{
			name:                 "test4",
			config:               getCfg(),
			nodes:                expectedNodeForTest4(),
			existHyperNode:       getExistHyperNodesForTest4(),
			expectedHyperNodeMap: expectedHyperNodesForTest4(),
		},
		{
			name:                 "test5",
			config:               getCfg(),
			nodes:                expectedNodeForTest5(),
			existHyperNode:       getExistHyperNodesForTest5(),
			expectedHyperNodeMap: expectedHyperNodesForTest5(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := tc.config
			kubeClient := fake.NewSimpleClientset()
			fakeVcClient := vcclientset.NewSimpleClientset()
			createNode(kubeClient, tc.nodes)
			createHyperNode(fakeVcClient, tc.existHyperNode)
			informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
			vcInformerFactory := vcinformer.NewSharedInformerFactory(fakeVcClient, 0)
			nodeInformer := informerFactory.Core().V1().Nodes()
			hyperNodeInformer := vcInformerFactory.Topology().V1alpha1().HyperNodes()
			nodeInformer.Informer()
			hyperNodeInformer.Informer()
			d, err := NewLabelDiscovererWithOptions(cfg, api.DiscovererOptions{
				KubeClient: kubeClient, VolcanoClient: fakeVcClient,
				NodeInformer: nodeInformer, HyperNodeInformer: hyperNodeInformer,
			})
			assert.NoError(t, err)
			stopCh := make(chan struct{})
			informerFactory.Start(stopCh)
			vcInformerFactory.Start(stopCh)
			informerFactory.WaitForCacheSync(stopCh)
			vcInformerFactory.WaitForCacheSync(stopCh)
			outputCh, err := d.Start()
			assert.NoError(t, err)
			var hyperNodes []*topologyv1alpha1.HyperNode
			select {
			case hyperNodes = <-outputCh:
			case <-time.After(time.Second):
				t.Fatal("Timeout waiting for output")
			}
			expectedHyperNodeMap := tc.expectedHyperNodeMap
			assert.Equal(t, len(expectedHyperNodeMap), len(hyperNodes), "Hypernode count should match")
			for _, hn := range hyperNodes {
				klog.Infof("target hyperNode name is %s\n", hn.Name)
			}
			d.Stop()
			close(stopCh)
		})
	}
}

func TestNewLabelDiscovererWithOptionsFallsBackToDedicatedInformers(t *testing.T) {
	discoverer, err := NewLabelDiscovererWithOptions(getCfg(), api.DiscovererOptions{
		KubeClient:    fake.NewSimpleClientset(),
		VolcanoClient: vcclientset.NewSimpleClientset(),
	})
	assert.NoError(t, err)

	labelDiscoverer, ok := discoverer.(*labelDiscoverer)
	assert.True(t, ok)
	assert.NotNil(t, labelDiscoverer.informerFactory)
	assert.NotNil(t, labelDiscoverer.vcInformerFactory)
}

func TestBuildHyperNodeNameFallsBackToAPIWhenInformerCacheIsStale(t *testing.T) {
	const (
		topologyType  = "e2e-rack-topology"
		topologyLabel = "volcano.sh/e2e-hypernode-rack"
		topologyValue = "rack-a"
		existingName  = "hypernode-e2e-rack-topology-tier1-abcde"
	)
	existing := utils.BuildHyperNodeWithTierName(existingName, 1, topologyLabel, nil, map[string]string{
		api.NetworkTopologySourceLabelKey: "label",
		topologyLabel:                     topologyValue,
	})
	kubeClient := fake.NewSimpleClientset()
	fakeVcClient := vcclientset.NewSimpleClientset(existing)
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	vcInformerFactory := vcinformer.NewSharedInformerFactory(fakeVcClient, 0)
	hyperNodeInformer := vcInformerFactory.Topology().V1alpha1().HyperNodes()

	discoverer, err := NewLabelDiscovererWithOptions(getCfg(), api.DiscovererOptions{
		KubeClient:        kubeClient,
		VolcanoClient:     fakeVcClient,
		NodeInformer:      informerFactory.Core().V1().Nodes(),
		HyperNodeInformer: hyperNodeInformer,
	})
	assert.NoError(t, err)
	labelDiscoverer := discoverer.(*labelDiscoverer)
	resolver := newHyperNodeNameResolver(labelDiscoverer.hyperNodeLister, labelDiscoverer.vcClient)

	// Keep the informer stopped so its cache does not contain the API object.
	cached, err := hyperNodeInformer.Lister().List(labels.Everything())
	assert.NoError(t, err)
	assert.Empty(t, cached)

	name, err := resolver.buildHyperNodeName(topologyType, topologyLabel, topologyValue, 1, map[string]HyperNodeInfo{})
	assert.NoError(t, err)
	assert.Equal(t, existingName, name)
}

func TestHyperNodeNameResolverLoadsLiveSnapshotOnceWithoutPerNameGets(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	fakeVcClient := vcclientset.NewSimpleClientset()
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	vcInformerFactory := vcinformer.NewSharedInformerFactory(fakeVcClient, 0)
	discoverer, err := NewLabelDiscovererWithOptions(getCfg(), api.DiscovererOptions{
		KubeClient:        kubeClient,
		VolcanoClient:     fakeVcClient,
		NodeInformer:      informerFactory.Core().V1().Nodes(),
		HyperNodeInformer: vcInformerFactory.Topology().V1alpha1().HyperNodes(),
	})
	assert.NoError(t, err)
	labelDiscoverer := discoverer.(*labelDiscoverer)
	resolver := newHyperNodeNameResolver(labelDiscoverer.hyperNodeLister, labelDiscoverer.vcClient)

	_, err = resolver.buildHyperNodeName("topology-a", "volcano.sh/rack", "rack-a", 1, map[string]HyperNodeInfo{})
	assert.NoError(t, err)
	_, err = resolver.buildHyperNodeName("topology-a", "volcano.sh/rack", "rack-b", 1, map[string]HyperNodeInfo{})
	assert.NoError(t, err)

	listCount := 0
	getCount := 0
	for _, action := range fakeVcClient.Actions() {
		if action.GetVerb() == "list" && action.GetResource().Resource == "hypernodes" {
			listCount++
		}
		if action.GetVerb() == "get" && action.GetResource().Resource == "hypernodes" {
			getCount++
		}
	}
	assert.Equal(t, 1, listCount)
	assert.Equal(t, 0, getCount)
}

func TestBuildHyperNodeNameReturnsLiveLookupError(t *testing.T) {
	kubeClient := fake.NewSimpleClientset()
	fakeVcClient := vcclientset.NewSimpleClientset()
	fakeVcClient.PrependReactor("list", "hypernodes", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("API unavailable")
	})
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	vcInformerFactory := vcinformer.NewSharedInformerFactory(fakeVcClient, 0)
	discoverer, err := NewLabelDiscovererWithOptions(getCfg(), api.DiscovererOptions{
		KubeClient:        kubeClient,
		VolcanoClient:     fakeVcClient,
		NodeInformer:      informerFactory.Core().V1().Nodes(),
		HyperNodeInformer: vcInformerFactory.Topology().V1alpha1().HyperNodes(),
	})
	assert.NoError(t, err)

	labelDiscoverer := discoverer.(*labelDiscoverer)
	resolver := newHyperNodeNameResolver(labelDiscoverer.hyperNodeLister, labelDiscoverer.vcClient)
	_, err = resolver.buildHyperNodeName(
		"topology-a", "volcano.sh/rack", "rack-a", 1, map[string]HyperNodeInfo{})
	assert.ErrorContains(t, err, "failed to confirm existing HyperNodes from API")
}

func getCfg() api.DiscoveryConfig {
	return api.DiscoveryConfig{
		Source: "label",
		Config: map[string]interface{}{
			"networkTopologyTypes": map[interface{}]interface{}{
				"topologyA2": []interface{}{
					map[interface{}]interface{}{
						"nodeLabel": "volcano.sh/torA2-2",
					},
					map[interface{}]interface{}{
						"nodeLabel": "volcano.sh_torA2-1",
					},
					map[interface{}]interface{}{
						"nodeLabel": "kubernetes.io/hostname",
					},
				},
				"topologyA3": []interface{}{
					map[interface{}]interface{}{
						"nodeLabel": "volcano.sh/torA3-2",
					},
					map[interface{}]interface{}{
						"nodeLabel": "volcano.sh/torA3-1",
					},
					map[interface{}]interface{}{
						"nodeLabel": "kubernetes.io/hostname",
					},
				},
				"topologyA5": []interface{}{
					map[interface{}]interface{}{
						"nodeLabel": "volcano.sh/torA5-2",
					},
					map[interface{}]interface{}{
						"nodeLabel": "volcano.sh/torA5-1",
					},
					map[interface{}]interface{}{
						"nodeLabel": "kubernetes.io/hostname",
					},
				},
			},
		},
	}
}

func createNode(kubeClient clientset.Interface, nodes map[string]*corev1.Node) {
	for _, node := range nodes {
		kubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
	}
}

func expectedHyperNodesForTest1() map[string]*topologyv1alpha1.HyperNode {
	return map[string]*topologyv1alpha1.HyperNode{
		"hypernode-topologya2-tier1-jcdfg": utils.BuildHyperNodeWithTierName("hypernode-topologya2-tier1-jcdfg", 1, "volcano.sh_torA2-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node0"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node1"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh_torA2-1": "s0"}),
		"hypernode-topologya2-tier1-jxcdr": utils.BuildHyperNodeWithTierName("hypernode-topologya2-tier1-jxcdr", 1, "volcano.sh_torA2-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node2"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node3"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh_torA2-1": "s1"}),
		"hypernode-topologya2-tier1-quksd": utils.BuildHyperNodeWithTierName("hypernode-topologya2-tier1-quksd", 1, "volcano.sh_torA2-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node4"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node5"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh_torA2-1": "s2"}),
		"hypernode-topologya2-tier1-akdhg": utils.BuildHyperNodeWithTierName("hypernode-topologya2-tier1-akdhg", 1, "volcano.sh_torA2-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node6"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node7"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh_torA2-1": "s3"}),
		"hypernode-topologya2-tier2-7hslk": utils.BuildHyperNodeWithTierName("hypernode-topologya2-tier2-7hslk", 2, "volcano.sh_torA2-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeHyperNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "hypernode-topologya2-tier1-jcdfg"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeHyperNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "hypernode-topologya2-tier1-jxcdr"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh/torA2-2": "s4"}),
		"hypernode-topologya2-tier2-zmonf": utils.BuildHyperNodeWithTierName("hypernode-topologya2-tier2-zmonf", 2, "volcano.sh_torA2-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeHyperNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "hypernode-topologya2-tier1-quksd"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeHyperNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "hypernode-topologya2-tier1-akdhg"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh/torA2-2": "s5"}),
	}
}

func expectedNodeForTest1() map[string]*corev1.Node {
	return map[string]*corev1.Node{
		"node0": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node0",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s0",
					"volcano.sh/torA2-2": "s4",
				},
			},
		},
		"node1": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s0",
					"volcano.sh/torA2-2": "s4",
				},
			},
		},
		"node2": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node2",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s1",
					"volcano.sh/torA2-2": "s4",
				},
			},
		},
		"node3": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node3",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s1",
					"volcano.sh/torA2-2": "s4",
				},
			},
		},
		"node4": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node4",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s2",
					"volcano.sh/torA2-2": "s5",
				},
			},
		},
		"node5": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node5",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s2",
					"volcano.sh/torA2-2": "s5",
				},
			},
		},
		"node6": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node6",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s3",
					"volcano.sh/torA2-2": "s5",
				},
			},
		},
		"node7": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node7",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s3",
					"volcano.sh/torA2-2": "s5",
				},
			},
		},
	}
}

func expectedHyperNodesForTest2() map[string]*topologyv1alpha1.HyperNode {
	return map[string]*topologyv1alpha1.HyperNode{
		"hypernode-topologya2-tier1-jcdfg": utils.BuildHyperNodeWithTierName("hypernode-topologya2-tier1-jcdfg", 1, "volcano.sh_torA2-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node0"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node1"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh_torA2-1": "s0"}),
		"hypernode-topologya2-tier1-cjain": utils.BuildHyperNodeWithTierName("hypernode-topologya2-tier1-cjain", 1, "volcano.sh_torA2-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node2"}},
				},
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node3"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh_torA2-1": "s1"}),
		"hypernode-topologya2-tier2-fanfn": utils.BuildHyperNodeWithTierName("hypernode-topologya2-tier2-fanfn", 2, "volcano.sh_torA2-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeHyperNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "hypernode-topologya2-tier1-cjain"}},
				}}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh_torA2-2": "s2"}),
	}
}

func expectedNodeForTest2() map[string]*corev1.Node {
	return map[string]*corev1.Node{
		"node0": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node0",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s0",
				},
			},
		},
		"node1": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s0",
					"volcano.sh/torA2-2": "s2",
				},
			},
		},
		"node2": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node2",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s1",
				},
			},
		},
		"node3": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node3",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s1",
				},
			},
		},
	}
}

func expectedHyperNodesForTest3() map[string]*topologyv1alpha1.HyperNode {
	return map[string]*topologyv1alpha1.HyperNode{
		"hypernode-topologya2-tier1-fanfn": utils.BuildHyperNodeWithTierName("hypernode-topologya2-tier1-fanfn", 1, "volcano.sh_torA2-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node1"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh_torA2-1": "s0"}),
	}
}

func expectedNodeForTest3() map[string]*corev1.Node {
	return map[string]*corev1.Node{
		"node0": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node0",
				Labels: map[string]string{
					"volcano.sh/torA2-2": "s2",
				},
			},
		},
		"node1": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
				Labels: map[string]string{
					"volcano.sh_torA2-1": "s0",
				},
			},
		},
		"node2": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node2",
				Labels: map[string]string{
					"volcano.sh/torA2-2": "s2",
				},
			},
		},
		"node3": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node3",
				Labels: map[string]string{
					"volcano.sh/torA2-2": "s2",
				},
			},
		},
	}
}

func getExistHyperNodesForTest4() map[string]*topologyv1alpha1.HyperNode {
	return map[string]*topologyv1alpha1.HyperNode{
		"hypernode-topologya2-tier1-mnsx6": utils.BuildHyperNode("hypernode-topologya2-tier1-mnsx6", 1,
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node0"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh/torA3-1": "s0"}),
	}
}

func expectedHyperNodesForTest4() map[string]*topologyv1alpha1.HyperNode {
	return map[string]*topologyv1alpha1.HyperNode{
		"hn-topologya3-s0": utils.BuildHyperNodeWithTierName("hn-topologya3-s0", 1, "volcano.sh/torA3-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node0"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh/torA3-1": "s0"}),
	}
}

func expectedNodeForTest4() map[string]*corev1.Node {
	return map[string]*corev1.Node{
		"node0": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node0",
				Labels: map[string]string{
					"volcano.sh/torA3-1": "s0",
				},
			},
		},
	}
}

func getExistHyperNodesForTest5() map[string]*topologyv1alpha1.HyperNode {
	return map[string]*topologyv1alpha1.HyperNode{
		"hypernode-topologya2-tier1-mnsx6": utils.BuildHyperNodeWithTierName("hypernode-topologya2-tier1-mnsx6", 1, "volcano.sh/torA3-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node0"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh/torA3-1": "s0"}),
	}
}

func expectedHyperNodesForTest5() map[string]*topologyv1alpha1.HyperNode {
	return map[string]*topologyv1alpha1.HyperNode{
		"hn-topologya3-s0": utils.BuildHyperNodeWithTierName("hn-topologya3-s0", 1, "volcano.sh/torA3-1",
			[]topologyv1alpha1.MemberSpec{
				{
					Type:     topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{ExactMatch: &topologyv1alpha1.ExactMatch{Name: "node0"}},
				},
			}, map[string]string{api.NetworkTopologySourceLabelKey: "label",
				"volcano.sh/torA3-1": "s0"}),
	}
}

func expectedNodeForTest5() map[string]*corev1.Node {
	return map[string]*corev1.Node{
		"node0": {
			ObjectMeta: metav1.ObjectMeta{
				Name: "node0",
				Labels: map[string]string{
					"volcano.sh/torA3-1": "s0",
				},
			},
		},
	}
}

func createHyperNode(vcClient vcclient.Interface, nodeMap map[string]*topologyv1alpha1.HyperNode) {
	for _, node := range nodeMap {
		vcClient.TopologyV1alpha1().HyperNodes().Create(
			context.Background(),
			node,
			metav1.CreateOptions{},
		)
	}
}
