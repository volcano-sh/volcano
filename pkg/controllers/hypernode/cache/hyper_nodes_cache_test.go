package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

func newSimpleFakeHyperNode(name, resourceVersion string) *topologyv1alpha1.HyperNode {
	return &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name:            name,
			ResourceVersion: resourceVersion,
		},
	}
}

func TestUpdateHyperNode(t *testing.T) {
	testCases := []struct {
		name            string
		updateHyperNode *topologyv1alpha1.HyperNode
		initCacheFunc   func(cache *HyperNodesCache)
		expectErr       bool
		expectHyperNode *topologyv1alpha1.HyperNode
	}{
		{
			name:            "hypernode does not exist in local cache, add it",
			updateHyperNode: newSimpleFakeHyperNode("s1", "1"),
			expectErr:       false,
			expectHyperNode: newSimpleFakeHyperNode("s1", "1"),
		},
		{
			name:            "failed to parse resource version",
			updateHyperNode: newSimpleFakeHyperNode("s1", "invalid"),
			initCacheFunc: func(cache *HyperNodesCache) {
				initHyperNode := newSimpleFakeHyperNode("s1", "1")
				cache.HyperNodes.Add(initHyperNode)
			},
			expectErr: true,
		},
		{
			name:            "resource version of hypernode need to be updated is smaller than the hypernode in local cache",
			updateHyperNode: newSimpleFakeHyperNode("s1", "1"),
			initCacheFunc: func(cache *HyperNodesCache) {
				initHyperNode := newSimpleFakeHyperNode("s1", "2")
				cache.HyperNodes.Add(initHyperNode)
			},
			expectErr: false,
		},
		{
			name:            "successfully updated hypernode",
			updateHyperNode: newSimpleFakeHyperNode("s1", "2"),
			initCacheFunc: func(cache *HyperNodesCache) {
				initHyperNode := newSimpleFakeHyperNode("s1", "1")
				cache.HyperNodes.Add(initHyperNode)
			},
			expectErr:       false,
			expectHyperNode: newSimpleFakeHyperNode("s1", "2"),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hnc := NewHyperNodesCache(cache.NewIndexer(
				cache.MetaNamespaceKeyFunc,
				cache.Indexers{
					cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
				},
			))
			if tc.initCacheFunc != nil {
				tc.initCacheFunc(hnc)
			}

			err := hnc.UpdateHyperNode(tc.updateHyperNode)
			assert.Equal(t, tc.expectErr, err != nil)

			if tc.expectHyperNode != nil {
				gotHyperNode, err := hnc.GetHyperNode(tc.expectHyperNode.Name)
				assert.NoError(t, err)
				if !equality.Semantic.DeepEqual(gotHyperNode, tc.expectHyperNode) {
					t.Errorf("hypernode is not equal, got %v, expect %v", gotHyperNode, tc.expectHyperNode)
				}
			}
		})
	}
}
