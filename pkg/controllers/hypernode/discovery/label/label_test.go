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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
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
			time.Sleep(300 * time.Millisecond)
			d := NewLabelDiscoverer(cfg, kubeClient, fakeVcClient)
			outputCh, _ := d.Start()
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
		})
	}
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

func buildHyperNode(name string, tier int, members []topologyv1alpha1.MemberSpec, labels map[string]string) *topologyv1alpha1.HyperNode {
	return &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: topologyv1alpha1.HyperNodeSpec{
			Tier:    tier,
			Members: members,
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
		"hypernode-topologya2-tier1-jcdfg": buildHyperNode("hypernode-topologya2-tier1-jcdfg", 1,
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
		"hypernode-topologya2-tier1-jxcdr": buildHyperNode("hypernode-topologya2-tier1-jxcdr", 1,
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
		"hypernode-topologya2-tier1-quksd": buildHyperNode("hypernode-topologya2-tier1-quksd", 1,
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
		"hypernode-topologya2-tier1-akdhg": buildHyperNode("hypernode-topologya2-tier1-akdhg", 1,
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
		"hypernode-topologya2-tier2-7hslk": buildHyperNode("hypernode-topologya2-tier2-7hslk", 2,
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
		"hypernode-topologya2-tier2-zmonf": buildHyperNode("hypernode-topologya2-tier2-zmonf", 2,
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
		"hypernode-topologya2-tier1-jcdfg": buildHyperNode("hypernode-topologya2-tier1-jcdfg", 1,
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
		"hypernode-topologya2-tier1-cjain": buildHyperNode("hypernode-topologya2-tier1-cjain", 1,
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
		"hypernode-topologya2-tier2-fanfn": buildHyperNode("hypernode-topologya2-tier2-fanfn", 2,
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
		"hypernode-topologya2-tier1-fanfn": buildHyperNode("hypernode-topologya2-tier1-fanfn", 1,
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
		"hypernode-topologya2-tier1-mnsx6": buildHyperNode("hypernode-topologya2-tier1-mnsx6", 1,
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
		"hn-topologya3-s0": buildHyperNode("hn-topologya3-s0", 1,
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
		"hypernode-topologya2-tier1-mnsx6": buildHyperNode("hypernode-topologya2-tier1-mnsx6", 1,
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
		"hn-topologya3-s0": buildHyperNode("hn-topologya3-s0", 1,
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
