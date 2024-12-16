package hypernode

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	volcanoclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/framework"

	hncache "volcano.sh/volcano/pkg/controllers/hypernode/cache"
)

func newFakeHyperNodeController() *hyperNodeController {
	vcClient := volcanoclient.NewSimpleClientset()
	vcSharedInformers := vcinformer.NewSharedInformerFactory(vcClient, 0)
	kubeClient := kubeclient.NewClientset()
	kubeSharedInformers := informers.NewSharedInformerFactory(kubeClient, 0)

	hnc := &hyperNodeController{}

	opt := &framework.ControllerOption{
		KubeClient:              kubeClient,
		VolcanoClient:           vcClient,
		SharedInformerFactory:   kubeSharedInformers,
		VCSharedInformerFactory: vcSharedInformers,
	}

	hnc.Initialize(opt)

	return hnc
}

type member struct {
	name     string
	nodeType string
}

func newFakeNode(name, hyperNodeList string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				hyperNodesLabelKey: hyperNodeList,
			},
		}}
}

func newFakeHyperNode(tier, hyperNodeName, resourceVersion string, memberList []member) *topologyv1alpha1.HyperNode {
	hn := newHyperNodeWithEmptyMembers(tier, hyperNodeName)
	for _, m := range memberList {
		memberSpec := newHyperNodeMember(m.nodeType, m.name)
		hn.Spec.Members = append(hn.Spec.Members, memberSpec)
	}
	hn.ResourceVersion = resourceVersion
	return hn
}

func compareHyperNodeActions(actionsI []*hyperNodeAction, actionsJ []*hyperNodeAction) bool {
	if len(actionsI) != len(actionsJ) {
		return false
	}

	for index := range actionsI {
		if !equality.Semantic.DeepEqual(actionsI[index].hyperNode, actionsJ[index].hyperNode) ||
			actionsI[index].action != actionsJ[index].action {
			return false
		}
	}

	return true
}

func CloneHyperNodesCache(originalCache *hncache.HyperNodesCache) (*hncache.HyperNodesCache, error) {
	newCache := hncache.NewHyperNodesCache(cache.NewIndexer(cache.MetaNamespaceKeyFunc,
		cache.Indexers{
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		}))

	for _, obj := range originalCache.HyperNodes.List() {
		hn, _ := obj.(*topologyv1alpha1.HyperNode)
		copiedHN := hn.DeepCopy()
		err := newCache.UpdateHyperNode(copiedHN)
		if err != nil {
			return nil, err
		}
	}

	originalCache.MembersToHyperNode.Range(func(key, value interface{}) bool {
		hn, _ := value.(*topologyv1alpha1.HyperNode)
		copiedHN := hn.DeepCopy()
		newCache.MembersToHyperNode.Store(key, copiedHN)
		return true
	})

	return newCache, nil
}

// TestHyperNodesList tests according to the following tree structure
//	          s6
//	       /       \
//	     s4          s5
//	   /   \       /    \
//	 s0     s1    s2     s3
//	/  \   /  \  /  \   /  \
// n0  n1 n2  n3 n4 n5 n6  n7

func TestHyperNodesList(t *testing.T) {
	hnc := newFakeHyperNodeController()

	// create hypernodes tree according to the node's label
	addTestCases := []struct {
		name          string
		node          *v1.Node
		hyperNodeList []string
		expectActions []*hyperNodeAction
	}{
		{
			name:          "add n0",
			node:          newFakeNode("n0", "s0,s4,s6"),
			hyperNodeList: []string{"s0", "s4", "s6"},
			expectActions: []*hyperNodeAction{
				{
					hyperNode: newFakeHyperNode("1", "s0", "", []member{{name: "n0", nodeType: TypeNode}}),
					action:    createAction,
				},
				{
					hyperNode: newFakeHyperNode("2", "s4", "", []member{{name: "s0", nodeType: TypeHyperNode}}),
					action:    createAction,
				},
				{
					hyperNode: newFakeHyperNode("3", "s6", "", []member{{name: "s4", nodeType: TypeHyperNode}}),
					action:    createAction,
				},
			},
		},
		{
			name:          "add n1",
			node:          newFakeNode("n1", "s0,s4,s6"),
			hyperNodeList: []string{"s0", "s4", "s6"},
			expectActions: []*hyperNodeAction{
				{
					hyperNode: newFakeHyperNode("1", "s0", "1", []member{
						{name: "n0", nodeType: TypeNode},
						{name: "n1", nodeType: TypeNode},
					}),
					action: updateAction,
				},
			},
		},
		{
			name:          "add n2",
			node:          newFakeNode("n2", "s1,s4,s6"),
			hyperNodeList: []string{"s1", "s4", "s6"},
			expectActions: []*hyperNodeAction{
				{
					hyperNode: newFakeHyperNode("1", "s1", "", []member{{name: "n2", nodeType: TypeNode}}),
					action:    createAction,
				},
				{
					hyperNode: newFakeHyperNode("2", "s4", "1", []member{
						{name: "s0", nodeType: TypeHyperNode},
						{name: "s1", nodeType: TypeHyperNode},
					}),
					action: updateAction,
				},
			},
		},
		{
			name:          "add n3",
			node:          newFakeNode("n3", "s1,s4,s6"),
			hyperNodeList: []string{"s1", "s4", "s6"},
			expectActions: []*hyperNodeAction{
				{
					hyperNode: newFakeHyperNode("1", "s1", "1", []member{
						{name: "n2", nodeType: TypeNode},
						{name: "n3", nodeType: TypeNode},
					}),
					action: updateAction,
				},
			},
		},
		{
			name:          "add n4",
			node:          newFakeNode("n4", "s2,s5,s6"),
			hyperNodeList: []string{"s2", "s5", "s6"},
			expectActions: []*hyperNodeAction{
				{
					hyperNode: newFakeHyperNode("1", "s2", "", []member{{name: "n4", nodeType: TypeNode}}),
					action:    createAction,
				},
				{
					hyperNode: newFakeHyperNode("2", "s5", "", []member{{name: "s2", nodeType: TypeHyperNode}}),
					action:    createAction,
				},
				{
					hyperNode: newFakeHyperNode("3", "s6", "1", []member{
						{name: "s4", nodeType: TypeHyperNode},
						{name: "s5", nodeType: TypeHyperNode},
					}),
					action: updateAction,
				},
			},
		},
		{
			name:          "add n5",
			node:          newFakeNode("n5", "s2,s5,s6"),
			hyperNodeList: []string{"s2", "s5", "s6"},
			expectActions: []*hyperNodeAction{
				{
					hyperNode: newFakeHyperNode("1", "s2", "1", []member{
						{name: "n4", nodeType: TypeNode},
						{name: "n5", nodeType: TypeNode},
					}),
					action: updateAction,
				},
			},
		},
		{
			name:          "add n6",
			node:          newFakeNode("n6", "s3,s5,s6"),
			hyperNodeList: []string{"s3", "s5", "s6"},
			expectActions: []*hyperNodeAction{
				{
					hyperNode: newFakeHyperNode("1", "s3", "", []member{{name: "n6", nodeType: TypeNode}}),
					action:    createAction,
				},
				{
					hyperNode: newFakeHyperNode("2", "s5", "1", []member{
						{name: "s2", nodeType: TypeHyperNode},
						{name: "s3", nodeType: TypeHyperNode},
					}),
					action: updateAction,
				},
			},
		},
		{
			name:          "add n7",
			node:          newFakeNode("n7", "s3,s5,s6"),
			hyperNodeList: []string{"s3", "s5", "s6"},
			expectActions: []*hyperNodeAction{
				{
					hyperNode: newFakeHyperNode("1", "s3", "1", []member{
						{name: "n6", nodeType: TypeNode},
						{name: "n7", nodeType: TypeNode},
					}),
					action: updateAction,
				},
			},
		},
	}

	for _, tc := range addTestCases {
		actions, err := hnc.addHyperNodesList(tc.node, tc.hyperNodeList)
		assert.NoError(t, err)
		if !compareHyperNodeActions(actions, tc.expectActions) {
			t.Errorf("test case %s: hypernode actions list are not equal, got %v, expect %v", tc.name, actions, tc.expectActions)
		}

		// update local hyperNodesCache
		for index := range actions {
			copiedHN := actions[index].hyperNode.DeepCopy()
			if len(copiedHN.ResourceVersion) == 0 {
				// init resource version if resource version is empty
				copiedHN.ResourceVersion = "1"
			} else {
				currentRV, err := strconv.Atoi(copiedHN.ResourceVersion)
				assert.NoError(t, err)
				copiedHN.ResourceVersion = strconv.Itoa(currentRV + 1)
			}
			err = hnc.hyperNodesCache.UpdateHyperNode(copiedHN)
			assert.NoError(t, err)
			for _, m := range actions[index].hyperNode.Spec.Members {
				hnc.hyperNodesCache.MembersToHyperNode.Store(m.Selector.ExactMatch.Name, actions[index].hyperNode)
			}
		}
	}

	type updateNode struct {
		node             *v1.Node
		oldHyperNodeList []string
		newHyperNodeList []string
	}

	// update node's label
	updateTestCases := []struct {
		name           string
		updateNodeList []*updateNode
		expectActions  []*hyperNodeAction
	}{
		{
			name: "node0: s0,s4,s6 -> s1,s4,s6",
			updateNodeList: []*updateNode{
				{
					node:             newFakeNode("n0", "s1,s4,s6"),
					oldHyperNodeList: []string{"s0", "s4", "s6"},
					newHyperNodeList: []string{"s1", "s4", "s6"},
				},
			},
			expectActions: []*hyperNodeAction{
				{
					hyperNode: newFakeHyperNode("1", "s0", "2", []member{
						{name: "n1", nodeType: TypeNode},
					}),
					action: updateAction,
				},
				{
					hyperNode: newFakeHyperNode("1", "s1", "2", []member{
						{name: "n2", nodeType: TypeNode},
						{name: "n3", nodeType: TypeNode},
						{name: "n0", nodeType: TypeNode},
					}),
					action: updateAction,
				},
			},
		},
		{
			name: "node0,node1: s0,s4,s6 -> s1,s4,s6, remove hypernode s0",
			updateNodeList: []*updateNode{
				{
					node:             newFakeNode("n0", "s1,s4,s6"),
					oldHyperNodeList: []string{"s0", "s4", "s6"},
					newHyperNodeList: []string{"s1", "s4", "s6"},
				},
				{
					node:             newFakeNode("n1", "s1,s4,s6"),
					oldHyperNodeList: []string{"s0", "s4", "s6"},
					newHyperNodeList: []string{"s1", "s4", "s6"},
				},
			},
			expectActions: []*hyperNodeAction{
				{
					hyperNode: newFakeHyperNode("1", "s0", "2", []member{
						{name: "n1", nodeType: TypeNode},
					}),
					action: updateAction,
				},
				{
					hyperNode: newFakeHyperNode("1", "s1", "2", []member{
						{name: "n2", nodeType: TypeNode},
						{name: "n3", nodeType: TypeNode},
						{name: "n0", nodeType: TypeNode},
					}),
					action: updateAction,
				},
				{
					hyperNode: newFakeHyperNode("1", "s0", "3", []member{
						{name: "n1", nodeType: TypeNode},
					}),
					action: deleteAction,
				},
				{
					hyperNode: newFakeHyperNode("2", "s4", "2", []member{
						{name: "s1", nodeType: TypeHyperNode},
					}),
					action: updateAction,
				},
				{
					hyperNode: newFakeHyperNode("1", "s1", "3", []member{
						{name: "n2", nodeType: TypeNode},
						{name: "n3", nodeType: TypeNode},
						{name: "n0", nodeType: TypeNode},
						{name: "n1", nodeType: TypeNode},
					}),
					action: updateAction,
				},
			},
		},
	}

	originalHNTree, err := CloneHyperNodesCache(hnc.hyperNodesCache)
	assert.NoError(t, err)

	for _, tc := range updateTestCases {
		t.Run(tc.name, func(t *testing.T) {
			// we use the same hypernodes cache updated every time
			clonedCache, err := CloneHyperNodesCache(originalHNTree)
			assert.NoError(t, err)
			hnc.hyperNodesCache = clonedCache

			actions := make([]*hyperNodeAction, 0)
			for _, update := range tc.updateNodeList {
				newActions, err := hnc.updateHyperNodeList(update.node, update.oldHyperNodeList, update.newHyperNodeList)
				assert.NoError(t, err)
				actions = append(actions, newActions...)
				// update local hyperNodesCache
				for index := range actions {
					copiedHN := actions[index].hyperNode.DeepCopy()
					currentRV, err := strconv.Atoi(copiedHN.ResourceVersion)
					assert.NoError(t, err)

					copiedHN.ResourceVersion = strconv.Itoa(currentRV + 1)
					err = hnc.hyperNodesCache.UpdateHyperNode(copiedHN)
					assert.NoError(t, err)

					for _, m := range actions[index].hyperNode.Spec.Members {
						if cachedObj, exists := hnc.hyperNodesCache.MembersToHyperNode.Load(m.Selector.ExactMatch.Name); exists {
							cachedHN := cachedObj.(*topologyv1alpha1.HyperNode)
							// The current member already exists, check whether the parent hypernode is the same
							if cachedHN.Name == actions[index].hyperNode.Name {
								continue
							}
							hnc.hyperNodesCache.MembersToHyperNode.Store(m.Selector.ExactMatch.Name, actions[index].hyperNode)
						}
					}
				}
			}

			if !compareHyperNodeActions(actions, tc.expectActions) {
				t.Errorf("test case %s: hypernode actions list are not equal, got %v, expect %v", tc.name, actions, tc.expectActions)
			}
		})
	}
}
