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
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
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
				"topologydeep": []interface{}{
					map[interface{}]interface{}{
						"nodeLabel": "volcano.sh/tordeep-2",
					},
					map[interface{}]interface{}{
						"nodeLabel": "volcano.sh/tordeep-1",
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

func TestGenerateHyperNodesWithMixedTopologyProfiles(t *testing.T) {
	const (
		profileLabel    = "example.com/topology-profile"
		hyperCluster    = "volcano.sh/hypercluster"
		hyperNode       = "volcano.sh/hypernode"
		superPod        = "volcano.sh/superpod"
		hyperNodeKey    = "example.com/hypernode-domain"
		hyperClusterKey = "example.com/hypercluster-domain"
	)

	cfg := api.DiscoveryConfig{
		Source: "label",
		Config: map[string]interface{}{
			"networkTopologyTypes": map[string]interface{}{
				"topologyshallow": map[string]interface{}{
					"nodeSelector": map[string]interface{}{
						"matchLabels": map[string]interface{}{profileLabel: "shallow"},
					},
					"levels": []interface{}{
						map[string]interface{}{"nodeLabel": hyperClusterKey, "tierName": hyperCluster},
						map[string]interface{}{"nodeLabel": hyperNodeKey, "tierName": hyperNode},
						map[string]interface{}{"nodeLabel": "kubernetes.io/hostname"},
					},
				},
				"topologydeep": map[string]interface{}{
					"nodeSelector": map[string]interface{}{
						"matchLabels": map[string]interface{}{profileLabel: "deep"},
					},
					"levels": []interface{}{
						map[string]interface{}{"nodeLabel": hyperClusterKey, "tierName": hyperCluster},
						map[string]interface{}{"nodeLabel": hyperNodeKey, "tierName": hyperNode},
						map[string]interface{}{"nodeLabel": "example.com/superpod-domain", "tierName": superPod},
						map[string]interface{}{"nodeLabel": "kubernetes.io/hostname"},
					},
				},
			},
		},
	}
	nodes := []*corev1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "shallow-node", Labels: map[string]string{
				profileLabel: "shallow", hyperNodeKey: "hn-0", hyperClusterKey: "hc-0",
			}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "deep-node", Labels: map[string]string{
				profileLabel: "deep", hyperNodeKey: "hn-0", hyperClusterKey: "hc-0",
				"example.com/superpod-domain": "sp-0",
			}},
		},
	}

	discoverer := newDiscovererForGenerationTest(t, cfg, nodes)
	infoMap, err := discoverer.generateHyperNodeInfo()
	require.NoError(t, err)
	require.Len(t, infoMap, 5)

	// Rebuilding from the same desired state must produce stable names and
	// graph content even though networkTopologyTypes is backed by a Go map.
	secondInfoMap, err := discoverer.generateHyperNodeInfo()
	require.NoError(t, err)
	assert.Equal(t, infoMap, secondInfoMap)

	byProfileTier := make(map[string]map[int]*topologyv1alpha1.HyperNode)
	for _, hn := range discoverer.buildHyperNodes(infoMap) {
		profile := hn.Labels[api.NetworkTopologyProfileLabelKey]
		if byProfileTier[profile] == nil {
			byProfileTier[profile] = make(map[int]*topologyv1alpha1.HyperNode)
		}
		require.Nil(t, byProfileTier[profile][hn.Spec.Tier], "fixture expects one domain per profile tier")
		byProfileTier[profile][hn.Spec.Tier] = hn
	}

	require.Len(t, byProfileTier["topologyshallow"], 2)
	require.Len(t, byProfileTier["topologydeep"], 3)
	assert.Equal(t, hyperNode, byProfileTier["topologyshallow"][1].Spec.TierName)
	assert.Equal(t, hyperCluster, byProfileTier["topologyshallow"][2].Spec.TierName)
	assert.Equal(t, superPod, byProfileTier["topologydeep"][1].Spec.TierName)
	assert.Equal(t, hyperNode, byProfileTier["topologydeep"][2].Spec.TierName)
	assert.Equal(t, hyperCluster, byProfileTier["topologydeep"][3].Spec.TierName)

	// shallow and deep intentionally reuse the same domain key/value. Profile-scoped
	// identity must still create independent HyperNodes at different tiers.
	shallowHyperNode := byProfileTier["topologyshallow"][1]
	deepHyperNode := byProfileTier["topologydeep"][2]
	assert.Equal(t, "hn-0", shallowHyperNode.Labels[hyperNodeKey])
	assert.Equal(t, "hn-0", deepHyperNode.Labels[hyperNodeKey])
	assert.NotEqual(t, shallowHyperNode.Name, deepHyperNode.Name)
	assert.Equal(t, []string{"shallow-node"}, exactMemberNames(shallowHyperNode))
	assert.Equal(t, []string{"deep-node"}, exactMemberNames(byProfileTier["topologydeep"][1]))
	assert.Equal(t, []string{deepHyperNode.Name}, exactMemberNames(byProfileTier["topologydeep"][3]))
}

func TestGenerateHyperNodesWithSingleTopologyLevelProfile(t *testing.T) {
	const (
		profileLabel = "example.com/topology-profile"
		domainLabel  = "example.com/compute-domain"
		tierName     = "volcano.sh/compute-domain"
	)
	cfg := api.DiscoveryConfig{Source: "label", Config: map[string]interface{}{
		"networkTopologyTypes": map[string]interface{}{
			"topologyflat": map[string]interface{}{
				"nodeSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{profileLabel: "flat"},
				},
				"levels": []interface{}{
					map[string]interface{}{"nodeLabel": domainLabel, "tierName": tierName},
					map[string]interface{}{"nodeLabel": corev1.LabelHostname},
				},
			},
		},
	}}
	node := func(name, domain string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			profileLabel: "flat", domainLabel: domain,
		}}}
	}
	discoverer := newDiscovererForGenerationTest(t, cfg, []*corev1.Node{
		node("flat-node-0", "domain-a"),
		node("flat-node-1", "domain-a"),
		node("flat-node-2", "domain-b"),
	})

	infoMap, err := discoverer.generateHyperNodeInfo()
	require.NoError(t, err)
	assert.Len(t, infoMap, 2, "one real topology level should produce one HyperNode per domain")

	byDomain := make(map[string]*topologyv1alpha1.HyperNode)
	for _, hyperNode := range discoverer.buildHyperNodes(infoMap) {
		assert.Equal(t, 1, hyperNode.Spec.Tier)
		assert.Equal(t, tierName, hyperNode.Spec.TierName)
		assert.Equal(t, "topologyflat", hyperNode.Labels[api.NetworkTopologyProfileLabelKey])
		byDomain[hyperNode.Labels[domainLabel]] = hyperNode
	}
	assert.Equal(t, []string{"flat-node-0", "flat-node-1"}, exactMemberNames(byDomain["domain-a"]))
	assert.Equal(t, []string{"flat-node-2"}, exactMemberNames(byDomain["domain-b"]))
}

func TestGenerateHyperNodesWithMultipleDomains(t *testing.T) {
	const (
		profileKey      = "example.com/topology-profile"
		hyperNodeKey    = "example.com/hypernode-domain"
		hyperClusterKey = "example.com/hypercluster-domain"
	)
	cfg := api.DiscoveryConfig{Source: "label", Config: map[string]interface{}{
		"networkTopologyTypes": map[string]interface{}{
			"topologyshallow": map[string]interface{}{
				"nodeSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{profileKey: "shallow"},
				},
				"levels": []interface{}{
					map[string]interface{}{"nodeLabel": hyperClusterKey, "tierName": "volcano.sh/hypercluster"},
					map[string]interface{}{"nodeLabel": hyperNodeKey, "tierName": "volcano.sh/hypernode"},
					map[string]interface{}{"nodeLabel": corev1.LabelHostname},
				},
			},
		},
	}}
	node := func(name, hyperNodeDomain, hyperClusterDomain string) *corev1.Node {
		return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{
			profileKey: "shallow", hyperNodeKey: hyperNodeDomain, hyperClusterKey: hyperClusterDomain,
		}}}
	}
	discoverer := newDiscovererForGenerationTest(t, cfg, []*corev1.Node{
		node("node-a1", "hn-a", "hc-a"),
		node("node-a2", "hn-a", "hc-a"),
		node("node-b1", "hn-b", "hc-a"),
		node("node-c1", "hn-c", "hc-b"),
	})

	infoMap, err := discoverer.generateHyperNodeInfo()
	require.NoError(t, err)
	require.Len(t, infoMap, 5)

	hyperNodesByDomain := make(map[string]*topologyv1alpha1.HyperNode)
	hyperClustersByDomain := make(map[string]*topologyv1alpha1.HyperNode)
	for _, hyperNode := range discoverer.buildHyperNodes(infoMap) {
		switch hyperNode.Spec.Tier {
		case 1:
			hyperNodesByDomain[hyperNode.Labels[hyperNodeKey]] = hyperNode
		case 2:
			hyperClustersByDomain[hyperNode.Labels[hyperClusterKey]] = hyperNode
		default:
			t.Fatalf("unexpected tier %d", hyperNode.Spec.Tier)
		}
	}

	require.Len(t, hyperNodesByDomain, 3)
	require.Len(t, hyperClustersByDomain, 2)
	assert.Equal(t, []string{"node-a1", "node-a2"}, exactMemberNames(hyperNodesByDomain["hn-a"]))
	assert.Equal(t, []string{"node-b1"}, exactMemberNames(hyperNodesByDomain["hn-b"]))
	assert.Equal(t, []string{"node-c1"}, exactMemberNames(hyperNodesByDomain["hn-c"]))
	assert.ElementsMatch(t, []string{hyperNodesByDomain["hn-a"].Name, hyperNodesByDomain["hn-b"].Name}, exactMemberNames(hyperClustersByDomain["hc-a"]))
	assert.Equal(t, []string{hyperNodesByDomain["hn-c"].Name}, exactMemberNames(hyperClustersByDomain["hc-b"]))
}

func TestBuildHyperNodeNameRejectsDeterministicNameConflict(t *testing.T) {
	const domainKey = "example.com/hypernode-domain"
	cfg := api.DiscoveryConfig{Source: "label", Config: map[string]interface{}{
		"networkTopologyTypes": map[string]interface{}{
			"topologyshallow": map[string]interface{}{
				"levels": []interface{}{
					map[string]interface{}{"nodeLabel": domainKey, "tierName": "volcano.sh/hypernode"},
					map[string]interface{}{"nodeLabel": corev1.LabelHostname},
				},
			},
		},
	}}
	discoverer := newDiscovererForGenerationTest(t, cfg, nil)
	profile := discoverer.topologyProfiles[0]

	deterministicName, err := discoverer.buildHyperNodeName(profile, domainKey, "hn-0", 1, nil)
	require.NoError(t, err)
	require.NoError(t, discoverer.hyperNodeInformer.Informer().GetIndexer().Add(&topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{Name: deterministicName, Labels: map[string]string{
			api.NetworkTopologySourceLabelKey:  "label",
			api.NetworkTopologyProfileLabelKey: "different-profile",
			domainKey:                          "different-domain",
		}},
	}))

	_, err = discoverer.buildHyperNodeName(profile, domainKey, "hn-0", 1, nil)
	require.ErrorContains(t, err, "is already used by a different topology domain")
}

func TestNodeLabelChangesTriggerDiscovery(t *testing.T) {
	const (
		profileKey = "example.com/topology-profile"
		domainKey  = "example.com/hypernode-domain"
	)
	cfg := api.DiscoveryConfig{Source: "label", Config: map[string]interface{}{
		"networkTopologyTypes": map[string]interface{}{
			"topologyshallow": map[string]interface{}{
				"nodeSelector": map[string]interface{}{
					"matchExpressions": []interface{}{
						map[string]interface{}{"key": profileKey, "operator": "In", "values": []interface{}{"shallow"}},
					},
				},
				"levels": []interface{}{
					map[string]interface{}{"nodeLabel": domainKey, "tierName": "volcano.sh/hypernode"},
					map[string]interface{}{"nodeLabel": corev1.LabelHostname},
				},
			},
		},
	}}
	discoverer, ok := NewLabelDiscoverer(cfg, fake.NewSimpleClientset(), vcclientset.NewSimpleClientset()).(*labelDiscoverer)
	require.True(t, ok)
	require.NoError(t, discoverer.configErr)
	t.Cleanup(func() {
		discoverer.queue.ShutDown()
	})

	oldNode := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{
		profileKey: "shallow",
		domainKey:  "hn-0",
	}}}
	unrelatedUpdate := oldNode.DeepCopy()
	unrelatedUpdate.Labels["example.com/unrelated"] = "changed"
	discoverer.UpdateNode(oldNode, unrelatedUpdate)
	time.Sleep(20 * time.Millisecond)
	assert.Zero(t, discoverer.queue.Len(), "unwatched labels must not trigger a full discovery")

	selectorUpdate := oldNode.DeepCopy()
	selectorUpdate.Labels[profileKey] = "deep"
	discoverer.UpdateNode(oldNode, selectorUpdate)
	require.Eventually(t, func() bool {
		return discoverer.queue.Len() == 1
	}, time.Second, 10*time.Millisecond)
	key, shutdown := discoverer.queue.Get()
	require.False(t, shutdown)
	discoverer.queue.Done(key)
	discoverer.queue.Forget(key)

	domainUpdate := oldNode.DeepCopy()
	domainUpdate.Labels[domainKey] = "hn-1"
	discoverer.UpdateNode(oldNode, domainUpdate)
	require.Eventually(t, func() bool {
		return discoverer.queue.Len() == 1
	}, time.Second, 10*time.Millisecond)
}

func TestGenerateHyperNodesRejectsMultipleProfileMatches(t *testing.T) {
	profile := func(levelLabel string) map[string]interface{} {
		return map[string]interface{}{
			"nodeSelector": map[string]interface{}{
				"matchLabels": map[string]interface{}{"example.com/role": "compute"},
			},
			"levels": []interface{}{
				map[string]interface{}{"nodeLabel": levelLabel, "tierName": "volcano.sh/hypernode"},
				map[string]interface{}{"nodeLabel": "kubernetes.io/hostname"},
			},
		}
	}
	cfg := api.DiscoveryConfig{
		Source: "label",
		Config: map[string]interface{}{
			"networkTopologyTypes": map[string]interface{}{
				"topologyshallow": profile("example.com/shallow-hypernode"),
				"topologydeep":    profile("example.com/deep-hypernode"),
			},
		},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "ambiguous-node", Labels: map[string]string{
		"example.com/role":              "compute",
		"example.com/shallow-hypernode": "shallow-hn",
		"example.com/deep-hypernode":    "deep-hn",
	}}}

	discoverer := newDiscovererForGenerationTest(t, cfg, []*corev1.Node{node})
	infoMap, err := discoverer.generateHyperNodeInfo()
	require.ErrorContains(t, err, "matches multiple topology profiles")
	assert.Empty(t, infoMap, "an ambiguous node must not publish a partial topology")
}

func TestGenerateHyperNodesPreservesLegacyMultiTopologyTraversal(t *testing.T) {
	const (
		fabricDomain  = "example.com/fabric-domain"
		storageDomain = "example.com/storage-domain"
	)
	cfg := api.DiscoveryConfig{Source: "label", Config: map[string]interface{}{
		"networkTopologyTypes": map[string]interface{}{
			"topologyfabric": []interface{}{
				map[string]interface{}{"nodeLabel": fabricDomain, "tierName": "volcano.sh/fabric"},
				map[string]interface{}{"nodeLabel": corev1.LabelHostname},
			},
			"topologystorage": []interface{}{
				map[string]interface{}{"nodeLabel": storageDomain, "tierName": "volcano.sh/storage"},
				map[string]interface{}{"nodeLabel": corev1.LabelHostname},
			},
		},
	}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "multi-network-node", Labels: map[string]string{
		fabricDomain:  "fabric-a",
		storageDomain: "storage-a",
	}}}

	discoverer := newDiscovererForGenerationTest(t, cfg, []*corev1.Node{node})
	infoMap, err := discoverer.generateHyperNodeInfo()
	require.NoError(t, err)
	require.Len(t, infoMap, 2)

	byProfile := make(map[string]*topologyv1alpha1.HyperNode)
	for _, hyperNode := range discoverer.buildHyperNodes(infoMap) {
		byProfile[hyperNode.Labels[api.NetworkTopologyProfileLabelKey]] = hyperNode
		assert.Equal(t, []string{node.Name}, exactMemberNames(hyperNode))
	}
	require.Contains(t, byProfile, "topologyfabric")
	require.Contains(t, byProfile, "topologystorage")
	assert.Equal(t, "volcano.sh/fabric", byProfile["topologyfabric"].Spec.TierName)
	assert.Equal(t, "volcano.sh/storage", byProfile["topologystorage"].Spec.TierName)
}

func TestGenerateHyperNodesExplicitProfileTakesOwnershipOverLegacyFallback(t *testing.T) {
	const (
		profileLabel = "example.com/profile"
		explicitTier = "example.com/explicit-domain"
		legacyTier   = "example.com/legacy-domain"
	)
	cfg := api.DiscoveryConfig{Source: "label", Config: map[string]interface{}{
		"networkTopologyTypes": map[string]interface{}{
			"topologyexplicit": map[string]interface{}{
				"nodeSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{profileLabel: "explicit"},
				},
				"levels": []interface{}{
					map[string]interface{}{"nodeLabel": explicitTier, "tierName": "volcano.sh/explicit"},
					map[string]interface{}{"nodeLabel": corev1.LabelHostname},
				},
			},
			"topologylegacy": []interface{}{
				map[string]interface{}{"nodeLabel": legacyTier, "tierName": "volcano.sh/legacy"},
				map[string]interface{}{"nodeLabel": corev1.LabelHostname},
			},
		},
	}}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "explicit-node", Labels: map[string]string{
		profileLabel: "explicit",
		explicitTier: "explicit-a",
		legacyTier:   "legacy-a",
	}}}

	discoverer := newDiscovererForGenerationTest(t, cfg, []*corev1.Node{node})
	infoMap, err := discoverer.generateHyperNodeInfo()
	require.NoError(t, err)
	require.Len(t, infoMap, 1)
	hyperNodes := discoverer.buildHyperNodes(infoMap)
	require.Len(t, hyperNodes, 1)
	assert.Equal(t, "topologyexplicit", hyperNodes[0].Labels[api.NetworkTopologyProfileLabelKey])
}

func TestGenerateHyperNodesRejectsMultipleParents(t *testing.T) {
	cfg := api.DiscoveryConfig{Config: map[string]interface{}{
		"networkTopologyTypes": map[string]interface{}{
			"topologyshallow": map[string]interface{}{
				"nodeSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{"example.com/profile": "shallow"},
				},
				"levels": []interface{}{
					map[string]interface{}{"nodeLabel": "example.com/hypercluster", "tierName": "volcano.sh/hypercluster"},
					map[string]interface{}{"nodeLabel": "example.com/hypernode", "tierName": "volcano.sh/hypernode"},
					map[string]interface{}{"nodeLabel": "kubernetes.io/hostname"},
				},
			},
		},
	}}
	nodes := []*corev1.Node{
		{ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: map[string]string{
			"example.com/profile": "shallow", "example.com/hypernode": "hn-0", "example.com/hypercluster": "hc-0",
		}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "node-b", Labels: map[string]string{
			"example.com/profile": "shallow", "example.com/hypernode": "hn-0", "example.com/hypercluster": "hc-1",
		}}},
	}

	discoverer := newDiscovererForGenerationTest(t, cfg, nodes)
	_, err := discoverer.generateHyperNodeInfo()
	require.ErrorContains(t, err, "belongs to multiple parents")
}

func TestParseCfgRejectsDuplicateSemanticTierName(t *testing.T) {
	cfg := api.DiscoveryConfig{Config: map[string]interface{}{
		"networkTopologyTypes": map[string]interface{}{
			"topologydeep": map[string]interface{}{
				"levels": []interface{}{
					map[string]interface{}{"nodeLabel": "example.com/cluster", "tierName": "volcano.sh/hypernode"},
					map[string]interface{}{"nodeLabel": "example.com/hypernode", "tierName": "volcano.sh/hypernode"},
					map[string]interface{}{"nodeLabel": "kubernetes.io/hostname"},
				},
			},
		},
	}}

	_, _, err := parseCfg(cfg)
	require.ErrorContains(t, err, "duplicate tierName")
}

func TestParseCfgRejectsUnsafeConfiguration(t *testing.T) {
	validProfile := func() map[string]interface{} {
		return map[string]interface{}{
			"levels": []interface{}{
				map[string]interface{}{"nodeLabel": "example.com/hypernode", "tierName": "volcano.sh/hypernode"},
				map[string]interface{}{"nodeLabel": corev1.LabelHostname},
			},
		}
	}

	tests := []struct {
		name          string
		config        map[string]interface{}
		errorContains string
	}{
		{
			name: "missing network topology types",
			config: map[string]interface{}{
				"networkTopologyType": map[string]interface{}{"topologyshallow": validProfile()},
			},
			errorContains: "networkTopologyType",
		},
		{
			name: "empty network topology types",
			config: map[string]interface{}{
				"networkTopologyTypes": map[string]interface{}{},
			},
			errorContains: "at least one topology profile",
		},
		{
			name: "unknown profile field",
			config: map[string]interface{}{
				"networkTopologyTypes": map[string]interface{}{
					"topologyshallow": map[string]interface{}{
						"nodeSelecter": map[string]interface{}{},
						"levels":       validProfile()["levels"],
					},
				},
			},
			errorContains: "nodeSelecter",
		},
		{
			name: "unknown level field",
			config: map[string]interface{}{
				"networkTopologyTypes": map[string]interface{}{
					"topologyshallow": map[string]interface{}{
						"levels": []interface{}{
							map[string]interface{}{"nodeLabel": "example.com/hypernode", "tierNmae": "volcano.sh/hypernode"},
							map[string]interface{}{"nodeLabel": corev1.LabelHostname},
						},
					},
				},
			},
			errorContains: "tierNmae",
		},
		{
			name: "leaf level is not hostname",
			config: map[string]interface{}{
				"networkTopologyTypes": map[string]interface{}{
					"topologyshallow": map[string]interface{}{
						"levels": []interface{}{
							map[string]interface{}{"nodeLabel": "example.com/hypernode", "tierName": "volcano.sh/hypernode"},
							map[string]interface{}{"nodeLabel": "example.com/node-id"},
						},
					},
				},
			},
			errorContains: "last level must use nodeLabel",
		},
		{
			name: "leaf level has tier name",
			config: map[string]interface{}{
				"networkTopologyTypes": map[string]interface{}{
					"topologyshallow": map[string]interface{}{
						"levels": []interface{}{
							map[string]interface{}{"nodeLabel": "example.com/hypernode", "tierName": "volcano.sh/hypernode"},
							map[string]interface{}{"nodeLabel": corev1.LabelHostname, "tierName": "volcano.sh/node"},
						},
					},
				},
			},
			errorContains: "node leaf level must not set tierName",
		},
		{
			name: "invalid qualified node label",
			config: map[string]interface{}{
				"networkTopologyTypes": map[string]interface{}{
					"topologyshallow": map[string]interface{}{
						"levels": []interface{}{
							map[string]interface{}{"nodeLabel": "bad/key/extra", "tierName": "volcano.sh/hypernode"},
							map[string]interface{}{"nodeLabel": corev1.LabelHostname},
						},
					},
				},
			},
			errorContains: "not a valid qualified name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseCfg(api.DiscoveryConfig{Source: "label", Config: tc.config})
			require.ErrorContains(t, err, tc.errorContains)
		})
	}
}

func TestParseCfgAcceptsLabelSelectorExpressions(t *testing.T) {
	cfg := api.DiscoveryConfig{Source: "label", Config: map[string]interface{}{
		"networkTopologyTypes": map[string]interface{}{
			"topologydeep": map[string]interface{}{
				"nodeSelector": map[string]interface{}{
					"matchExpressions": []interface{}{
						map[string]interface{}{"key": "example.com/region", "operator": "In", "values": []interface{}{"east", "west"}},
						map[string]interface{}{"key": "example.com/environment", "operator": "NotIn", "values": []interface{}{"development"}},
						map[string]interface{}{"key": "example.com/accelerator", "operator": "Exists"},
						map[string]interface{}{"key": "example.com/retired", "operator": "DoesNotExist"},
					},
				},
				"levels": []interface{}{
					map[string]interface{}{"nodeLabel": "example.com/hypernode", "tierName": "volcano.sh/hypernode"},
					map[string]interface{}{"nodeLabel": corev1.LabelHostname},
				},
			},
		},
	}}

	profiles, watchedKeys, err := parseCfg(cfg)
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	assert.True(t, profiles[0].nodeSelector.Matches(labels.Set{
		"example.com/region":      "east",
		"example.com/environment": "production",
		"example.com/accelerator": "gpu",
	}))
	assert.False(t, profiles[0].nodeSelector.Matches(labels.Set{
		"example.com/region":      "east",
		"example.com/environment": "development",
		"example.com/accelerator": "gpu",
	}))
	for _, key := range []string{
		"example.com/region",
		"example.com/environment",
		"example.com/accelerator",
		"example.com/retired",
	} {
		assert.Contains(t, watchedKeys, key)
	}
}

func TestParseMixedTopologyProfilesFromYAML(t *testing.T) {
	const configYAML = `
networkTopologyDiscovery:
  - source: label
    enabled: true
    config:
      networkTopologyTypes:
        topologyshallow:
          nodeSelector:
            matchLabels:
              volcano.sh/network-topology-profile: shallow
          levels:
            - nodeLabel: volcano.sh/shallow-hypercluster
              tierName: volcano.sh/hypercluster
            - nodeLabel: volcano.sh/shallow-hypernode
              tierName: volcano.sh/hypernode
            - nodeLabel: kubernetes.io/hostname
        topologydeep:
          nodeSelector:
            matchLabels:
              volcano.sh/network-topology-profile: deep
          levels:
            - nodeLabel: volcano.sh/deep-hypercluster
              tierName: volcano.sh/hypercluster
            - nodeLabel: volcano.sh/deep-hypernode
              tierName: volcano.sh/hypernode
            - nodeLabel: volcano.sh/deep-superpod
              tierName: volcano.sh/superpod
            - nodeLabel: kubernetes.io/hostname
`
	config := &api.NetworkTopologyConfig{}
	require.NoError(t, yaml.Unmarshal([]byte(configYAML), config))
	require.Len(t, config.NetworkTopologyDiscovery, 1)

	profiles, watchedKeys, err := parseCfg(config.NetworkTopologyDiscovery[0])
	require.NoError(t, err)
	require.Len(t, profiles, 2)
	assert.Equal(t, "topologydeep", profiles[0].name)
	assert.Equal(t, []NodeLabel{
		{NodeLabel: "volcano.sh/deep-superpod", TierName: "volcano.sh/superpod"},
		{NodeLabel: "volcano.sh/deep-hypernode", TierName: "volcano.sh/hypernode"},
		{NodeLabel: "volcano.sh/deep-hypercluster", TierName: "volcano.sh/hypercluster"},
	}, profiles[0].levels)
	assert.Contains(t, watchedKeys, "volcano.sh/network-topology-profile")
	assert.Contains(t, watchedKeys, "volcano.sh/deep-superpod")
}

func newDiscovererForGenerationTest(t *testing.T, cfg api.DiscoveryConfig, nodes []*corev1.Node) *labelDiscoverer {
	t.Helper()
	discoverer, ok := NewLabelDiscoverer(cfg, fake.NewSimpleClientset(), vcclientset.NewSimpleClientset()).(*labelDiscoverer)
	require.True(t, ok)
	require.NoError(t, discoverer.configErr)
	for _, node := range nodes {
		require.NoError(t, discoverer.nodeInformer.Informer().GetIndexer().Add(node))
	}
	return discoverer
}

func exactMemberNames(hyperNode *topologyv1alpha1.HyperNode) []string {
	names := make([]string, 0, len(hyperNode.Spec.Members))
	for _, member := range hyperNode.Spec.Members {
		if member.Selector.ExactMatch != nil {
			names = append(names, member.Selector.ExactMatch.Name)
		}
	}
	sort.Strings(names)
	return names
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
