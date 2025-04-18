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

package cache

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes"
	kubeclientset "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
)

var defaultNamespace = "volcano-system"

type hyperNodeCache struct {
	Cache
	nodeStore               cache.Store
	hyperNodeStore          cache.Store
	configMapStore          cache.Store
	sharedInformerFactory   informers.SharedInformerFactory
	vcSharedInformerFactory vcinformer.SharedInformerFactory
	//volcanoInformer.
	kubeClient kubeclient.Interface
	vcClient   vcclient.Interface
}

func (h *hyperNodeCache) getInterCache() *HyperNodeCache {
	c := reflect.ValueOf(h.Cache)
	hc, ok := c.Interface().(*HyperNodeCache)
	if !ok {
		return nil
	}
	return hc
}

func newHyperNodeCache() *hyperNodeCache {
	kubeClientSet := kubeclientset.NewSimpleClientset()
	vcclient := vcclientset.NewSimpleClientset()

	sharedInformerFactory := informers.NewSharedInformerFactory(kubeClientSet, 0)
	vcSharedInformerFactory := vcinformer.NewSharedInformerFactory(vcclient, 0)

	return &hyperNodeCache{
		Cache: NewHyperNodeCache(defaultNamespace, sharedInformerFactory.Core().V1().Nodes(), sharedInformerFactory.Core().V1().ConfigMaps(),
			vcSharedInformerFactory.Topology().V1alpha1().HyperNodes().Lister()),
		nodeStore:               sharedInformerFactory.Core().V1().Nodes().Informer().GetStore(),
		hyperNodeStore:          vcSharedInformerFactory.Topology().V1alpha1().HyperNodes().Informer().GetStore(),
		configMapStore:          sharedInformerFactory.Core().V1().ConfigMaps().Informer().GetStore(),
		sharedInformerFactory:   sharedInformerFactory,
		vcSharedInformerFactory: vcSharedInformerFactory,
		kubeClient:              kubeClientSet,
		vcClient:                vcclient,
	}
}

func newNodeLabelBasedTopologyConfigMap(data map[string]string) *v1.ConfigMap {
	c := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      NetworkTopologyAwareConfigMapName,
			Namespace: defaultNamespace,
		},
		Data: map[string]string{
			"node-label-based-topologies": `
topologies:
- name: topology1
  topo-level-keys:
  - topology1-tier1
  - topology1-tier2
- name: topology2
  topo-level-keys:
  - topology2-tier1
  - topology2-tier2`,
		},
	}
	if data != nil {
		c.Data = data
	}
	return c
}

func newNode(name string, labels map[string]string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func newSingleMemberTypeHyperNode(name string, labels map[string]string, tier int, membersType topologyv1alpha1.MemberType, memberNames []string) *topologyv1alpha1.HyperNode {
	hyperNode := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Spec: topologyv1alpha1.HyperNodeSpec{
			Tier: tier,
		},
	}
	for _, name := range memberNames {
		hyperNode.Spec.Members = append(hyperNode.Spec.Members, topologyv1alpha1.MemberSpec{
			Type: membersType,
			Selector: topologyv1alpha1.MemberSelector{
				ExactMatch: &topologyv1alpha1.ExactMatch{
					Name: name,
				},
			},
		})
	}
	return hyperNode
}

func TestCacheSession(t *testing.T) {
	existedNodes := []*v1.Node{
		newNode("node1", map[string]string{"topology1-tier1": "n1s1", "topology1-tier2": "n1"}),
		newNode("node2", map[string]string{"topology1-tier1": "n1s2", "topology1-tier2": "n1"}),
		newNode("node3", map[string]string{"topology1-tier1": "n2s1", "topology1-tier2": "n2"}),
		newNode("node4", map[string]string{"topology1-tier1": "n2s2", "topology1-tier2": "n2"}),
	}
	existedHyperNodes := []*topologyv1alpha1.HyperNode{
		newSingleMemberTypeHyperNode("topology1-t3-n3s3", map[string]string{"topology1-tier1": "n3s3", "volcano.io/label-based-topology-name": "topology1"}, 1,
			topologyv1alpha1.MemberTypeNode, []string{"node5"}),
		newSingleMemberTypeHyperNode("topology1-t1-n1s1-59f8b9996d", map[string]string{"topology1-tier1": "n1s1", "volcano.io/label-based-topology-name": "topology1"}, 1,
			topologyv1alpha1.MemberTypeNode, []string{"node1"}),
		newSingleMemberTypeHyperNode("topology1-t1-n1s2-5b88fdd7d9", map[string]string{"topology1-tier1": "n1s2", "volcano.io/label-based-topology-name": "topology1"}, 1,
			topologyv1alpha1.MemberTypeNode, []string{"node2"}),
	}

	testCases := []struct {
		name string

		deletedNodes      []string
		updatedNodes      []*v1.Node
		updatedConfigMaps []*v1.ConfigMap

		expectedDirtyHyperNodes []string
		expectedHyperNodeToSync []*topologyv1alpha1.HyperNode
		expectedHyperNodeToGC   []string
	}{
		{
			name: "case1: sync HyperNode",
			//       n1            n2
			//     /    \        /    \
			//   n1s1  n1s2    n2s1  n2s2
			//    |      |      |      |
			//   node1 node2  node3  node4

			expectedHyperNodeToSync: []*topologyv1alpha1.HyperNode{
				newSingleMemberTypeHyperNode("topology1-t1-n1s1-59f8b9996d", map[string]string{"topology1-tier1": "n1s1", "volcano.io/label-based-topology-name": "topology1"}, 1,
					topologyv1alpha1.MemberTypeNode, []string{"node1"}),
				newSingleMemberTypeHyperNode("topology1-t1-n1s2-5b88fdd7d9", map[string]string{"topology1-tier1": "n1s2", "volcano.io/label-based-topology-name": "topology1"}, 1,
					topologyv1alpha1.MemberTypeNode, []string{"node2"}),
				newSingleMemberTypeHyperNode("topology1-t1-n2s1-d5dc96c77", map[string]string{"topology1-tier1": "n2s1", "volcano.io/label-based-topology-name": "topology1"}, 1,
					topologyv1alpha1.MemberTypeNode, []string{"node3"}),
				newSingleMemberTypeHyperNode("topology1-t1-n2s2-cbd85fdcb", map[string]string{"topology1-tier1": "n2s2", "volcano.io/label-based-topology-name": "topology1"}, 1,
					topologyv1alpha1.MemberTypeNode, []string{"node4"}),
				newSingleMemberTypeHyperNode("topology1-t2-n1-c4b7d9f6", map[string]string{"topology1-tier2": "n1", "volcano.io/label-based-topology-name": "topology1"}, 2,
					topologyv1alpha1.MemberTypeHyperNode, []string{"topology1-t1-n1s1-59f8b9996d", "topology1-t1-n1s2-5b88fdd7d9"}),
				newSingleMemberTypeHyperNode("topology1-t2-n2-564fc588f", map[string]string{"topology1-tier2": "n2", "volcano.io/label-based-topology-name": "topology1"}, 2,
					topologyv1alpha1.MemberTypeHyperNode, []string{"topology1-t1-n2s1-d5dc96c77", "topology1-t1-n2s2-cbd85fdcb"}),
			},
			expectedHyperNodeToGC: []string{"topology1-t3-n3s3"},
		},
		{
			name: "case2: delete node event",
			//      n1            n2
			//       \        /    \
			//     n1s2    n2s1  n2s2
			//       |      |      |
			//     node2  node3  node4
			deletedNodes: []string{"node1"},
			// For node1 deleted event, n1 and n1s1 hyperNode resource will be dirty, remove from HyperNodeToSync cache
			expectedDirtyHyperNodes: []string{"topology1-t1-n1s1-59f8b9996d", "topology1-t2-n1-c4b7d9f6"},
			expectedHyperNodeToSync: []*topologyv1alpha1.HyperNode{
				newSingleMemberTypeHyperNode("topology1-t1-n1s2-5b88fdd7d9", map[string]string{"topology1-tier1": "n1s2", "volcano.io/label-based-topology-name": "topology1"}, 1,
					topologyv1alpha1.MemberTypeNode, []string{"node2"}),
				newSingleMemberTypeHyperNode("topology1-t1-n2s1-d5dc96c77", map[string]string{"topology1-tier1": "n2s1", "volcano.io/label-based-topology-name": "topology1"}, 1,
					topologyv1alpha1.MemberTypeNode, []string{"node3"}),
				newSingleMemberTypeHyperNode("topology1-t1-n2s2-cbd85fdcb", map[string]string{"topology1-tier1": "n2s2", "volcano.io/label-based-topology-name": "topology1"}, 1,
					topologyv1alpha1.MemberTypeNode, []string{"node4"}),
				newSingleMemberTypeHyperNode("topology1-t2-n2-564fc588f", map[string]string{"topology1-tier2": "n2", "volcano.io/label-based-topology-name": "topology1"}, 2,
					topologyv1alpha1.MemberTypeHyperNode, []string{"topology1-t1-n2s1-d5dc96c77", "topology1-t1-n2s2-cbd85fdcb"}),
			},
			expectedHyperNodeToGC: []string{"topology1-t3-n3s3"},
		},
		{
			name: "case3: update node event",
			//      n1           n2             n1            n2
			//       \        /    \              \          /   \
			//     n1s2    n2s1  n2s2      -->    n1s1    n2s1  n2s2
			//       |      |      |                |       |     |
			//     node2  node3  node4            node2   node3  node4
			updatedNodes: []*v1.Node{
				newNode("node2", map[string]string{"topology1-tier1": "n1s1", "topology1-tier2": "n1"}),
			},
			// For node2 updated event, n1, n1s1, n1s2(n1s1 update to n1s2) hyperNode resource will be dirty, remove from HyperNodeToSync cache
			expectedDirtyHyperNodes: []string{"topology1-t1-n1s1-59f8b9996d", "topology1-t1-n1s2-5b88fdd7d9", "topology1-t2-n1-c4b7d9f6"},
			expectedHyperNodeToSync: []*topologyv1alpha1.HyperNode{
				newSingleMemberTypeHyperNode("topology1-t1-n2s1-d5dc96c77", map[string]string{"topology1-tier1": "n2s1", "volcano.io/label-based-topology-name": "topology1"}, 1,
					topologyv1alpha1.MemberTypeNode, []string{"node3"}),
				newSingleMemberTypeHyperNode("topology1-t1-n2s2-cbd85fdcb", map[string]string{"topology1-tier1": "n2s2", "volcano.io/label-based-topology-name": "topology1"}, 1,
					topologyv1alpha1.MemberTypeNode, []string{"node4"}),
				newSingleMemberTypeHyperNode("topology1-t2-n2-564fc588f", map[string]string{"topology1-tier2": "n2", "volcano.io/label-based-topology-name": "topology1"}, 2,
					topologyv1alpha1.MemberTypeHyperNode, []string{"topology1-t1-n2s1-d5dc96c77", "topology1-t1-n2s2-cbd85fdcb"}),
			},
			expectedHyperNodeToGC: []string{"topology1-t3-n3s3"},
		},
		{
			name: "case4: update configMap",
			updatedConfigMaps: []*v1.ConfigMap{
				newNodeLabelBasedTopologyConfigMap(map[string]string{
					"node-label-based-topologies": `
topologies:
- name: topology1
  topo-level-keys:
  - topology1-tier1`,
				}),
			},
		},
	}

	syncTimeout := 5 * time.Second
	stopCh := make(chan struct{})
	defer close(stopCh)
	c := newHyperNodeCache()
	interCache := c.getInterCache()
	if interCache == nil {
		t.Fatal("failed to get internal cache")
	}

	c.sharedInformerFactory.Start(stopCh)
	c.vcSharedInformerFactory.Start(stopCh)
	c.sharedInformerFactory.WaitForCacheSync(stopCh)
	c.vcSharedInformerFactory.WaitForCacheSync(stopCh)

	for _, node := range existedNodes {
		c.kubeClient.CoreV1().Nodes().Create(context.TODO(), node, metav1.CreateOptions{})
	}
	for _, hyperNode := range existedHyperNodes {
		c.vcClient.TopologyV1alpha1().HyperNodes().Create(context.TODO(), hyperNode, metav1.CreateOptions{})
	}
	c.kubeClient.CoreV1().ConfigMaps(defaultNamespace).Create(context.TODO(), newNodeLabelBasedTopologyConfigMap(nil), metav1.CreateOptions{})

	err := wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, syncTimeout, false, func(ctx context.Context) (bool, error) {
		return len(c.nodeStore.List()) == len(existedNodes) && len(c.hyperNodeStore.List()) == len(existedHyperNodes) && len(c.configMapStore.List()) == 1, nil
	})
	if err != nil {
		t.Fatal("failed to wait informers sync")
	}

	for _, tc := range testCases {
		c.OpenSession()

		for _, nodeName := range tc.deletedNodes {
			c.kubeClient.CoreV1().Nodes().Delete(context.TODO(), nodeName, metav1.DeleteOptions{})
		}
		for _, node := range tc.updatedNodes {
			c.kubeClient.CoreV1().Nodes().Update(context.TODO(), node, metav1.UpdateOptions{})
		}
		for _, configMap := range tc.updatedConfigMaps {
			c.kubeClient.CoreV1().ConfigMaps(defaultNamespace).Update(context.TODO(), configMap, metav1.UpdateOptions{})
		}

		err = wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, syncTimeout, false, func(ctx context.Context) (bool, error) {
			interCache.RLock()
			defer interCache.RUnlock()
			dirtyHyperNodesInCache := interCache.dirtyHyperNodes.UnsortedList()
			if len(tc.expectedDirtyHyperNodes) == 0 {
				return len(dirtyHyperNodesInCache) == 0, nil
			}
			sort.Strings(tc.expectedDirtyHyperNodes)
			sort.Strings(dirtyHyperNodesInCache)
			return reflect.DeepEqual(tc.expectedDirtyHyperNodes, dirtyHyperNodesInCache), nil
		})
		if err != nil {
			t.Fatal("failed to wait hyperNodeDirty cache ready", tc.name, tc.expectedDirtyHyperNodes)
		}

		var actualHyperNodeToSync []*topologyv1alpha1.HyperNode
		err = wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, syncTimeout, false, func(ctx context.Context) (bool, error) {
			for {
				hyperNode, done := c.GetHyperNodeToSync()
				if done {
					break
				}
				actualHyperNodeToSync = append(actualHyperNodeToSync, hyperNode)
			}
			return len(actualHyperNodeToSync) == len(tc.expectedHyperNodeToSync), nil
		})
		if err != nil {
			t.Fatal("failed to wait hyperNodeToSync cache ready", tc.name, actualHyperNodeToSync, tc.expectedHyperNodeToSync)
		}

		var actualHyperNodeToGC []string
		err = wait.PollUntilContextTimeout(context.TODO(), 100*time.Millisecond, syncTimeout, false, func(ctx context.Context) (bool, error) {
			for {
				hyperNode, done := c.GetHyperNodeToGC()
				if done {
					break
				}
				actualHyperNodeToGC = append(actualHyperNodeToGC, hyperNode)
			}
			return len(actualHyperNodeToGC) == len(tc.expectedHyperNodeToGC), nil
		})
		if err != nil {
			t.Fatal("failed to wait hyperNodeToGC cache ready", tc.name, actualHyperNodeToGC, tc.expectedHyperNodeToGC)
		}

		sort.Slice(actualHyperNodeToSync, func(i, j int) bool {
			return actualHyperNodeToSync[i].Name < actualHyperNodeToSync[j].Name
		})
		sort.Slice(tc.expectedHyperNodeToSync, func(i, j int) bool {
			return tc.expectedHyperNodeToSync[i].Name < tc.expectedHyperNodeToSync[j].Name
		})
		assert.Equal(t, tc.expectedHyperNodeToSync, actualHyperNodeToSync, tc.name)

		c.CloseSession()
	}
}
