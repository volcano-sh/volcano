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
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientsetfake "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func newFakeHyperNodeController() (*hyperNodeController, *vcclientsetfake.Clientset, *fake.Clientset) {
	vcClient := vcclientsetfake.NewSimpleClientset()
	kubeClient := fake.NewSimpleClientset()

	vcInformerFactory := vcinformer.NewSharedInformerFactory(vcClient, 0)
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)

	controller := &hyperNodeController{
		vcClient:          vcClient,
		kubeClient:        kubeClient,
		vcInformerFactory: vcInformerFactory,
		informerFactory:   informerFactory,
		hyperNodeInformer: vcInformerFactory.Topology().V1alpha1().HyperNodes(),
		hyperNodeLister:   vcInformerFactory.Topology().V1alpha1().HyperNodes().Lister(),
		hyperNodeQueue:    workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]()),
		nodeLister:        informerFactory.Core().V1().Nodes().Lister(),
	}

	return controller, vcClient, kubeClient
}

func TestAddHyperNode(t *testing.T) {
	controller, _, _ := newFakeHyperNodeController()

	// Test normal case
	hyperNode := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-hypernode",
		},
	}

	controller.addHyperNode(hyperNode)

	// Verify that there is one element in the queue
	assert.Equal(t, 1, controller.hyperNodeQueue.Len())

	// Test error case (non-HyperNode object)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	controller.addHyperNode(pod)

	// Verify that the queue length is still 1 (error object not added)
	assert.Equal(t, 1, controller.hyperNodeQueue.Len())
}

func TestUpdateHyperNode(t *testing.T) {
	controller, _, _ := newFakeHyperNodeController()

	oldHyperNode := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-hypernode",
		},
		Spec: topologyv1alpha1.HyperNodeSpec{
			Members: []topologyv1alpha1.MemberSpec{
				{
					Type: topologyv1alpha1.MemberTypeNode,
					Selector: topologyv1alpha1.MemberSelector{
						ExactMatch: &topologyv1alpha1.ExactMatch{
							Name: "node1",
						},
					},
				},
			},
		},
	}

	newHyperNode := oldHyperNode.DeepCopy()
	newHyperNode.Spec.Members = append(newHyperNode.Spec.Members, topologyv1alpha1.MemberSpec{
		Type: topologyv1alpha1.MemberTypeNode,
		Selector: topologyv1alpha1.MemberSelector{
			ExactMatch: &topologyv1alpha1.ExactMatch{
				Name: "node2",
			},
		},
	})

	controller.updateHyperNode(oldHyperNode, newHyperNode)

	// Verify that there is one element in the queue
	assert.Equal(t, 1, controller.hyperNodeQueue.Len())

	// Test error case (non-HyperNode object)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
	}

	controller.updateHyperNode(oldHyperNode, pod)

	// Verify that the queue length is still 1 (error object not added)
	assert.Equal(t, 1, controller.hyperNodeQueue.Len())
}

func TestEnqueueHyperNode(t *testing.T) {
	controller, _, _ := newFakeHyperNodeController()

	hyperNode := &topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-hypernode",
		},
	}

	controller.enqueueHyperNode(hyperNode)

	// Verify that there is one element in the queue
	assert.Equal(t, 1, controller.hyperNodeQueue.Len())

	// Check if the element in the queue is the correct key
	key, shutdown := controller.hyperNodeQueue.Get()
	assert.False(t, shutdown)
	assert.Equal(t, "test-hypernode", key)
}

func TestSyncHyperNodeStatus(t *testing.T) {
	testCases := []struct {
		name          string
		hyperNodeKey  string
		setupFunc     func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode
		nodes         []*v1.Node
		expectedError bool
		expectedCount int64
	}{
		{
			name:         "Normal case: NodeCount needs to be updated",
			hyperNodeKey: "test-hypernode-1",
			setupFunc: func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode {
				hyperNode := api.BuildHyperNode("test-hypernode-1", 0, []api.MemberConfig{
					{Name: "node1", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
					{Name: "node2", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
					{Name: "node3", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
				})

				_, err := vcClient.TopologyV1alpha1().HyperNodes().Create(context.Background(), hyperNode, metav1.CreateOptions{})
				assert.NoError(t, err)

				return hyperNode
			},
			expectedError: false,
			expectedCount: 3,
		},
		{
			name:         "Node does not exist",
			hyperNodeKey: "non-existent",
			setupFunc: func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode {
				return nil
			},
			expectedError: false,
			expectedCount: 0,
		},
		{
			name:         "NodeCount already matches, no update needed",
			hyperNodeKey: "test-hypernode-2",
			setupFunc: func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode {
				hyperNode := api.BuildHyperNode("test-hypernode-2", 0, []api.MemberConfig{
					{Name: "node1", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
					{Name: "node2", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
					{Name: "node3", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
					{Name: "node4", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
				})
				hyperNode.Status.NodeCount = 4

				vcClient.PrependReactor("update", "hypernodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					updateAction, ok := action.(k8stesting.UpdateAction)
					if !ok || updateAction.GetSubresource() != "status" {
						return false, nil, errors.NewBadRequest("should update hyperNode status")
					}

					return false, nil, errors.NewBadRequest("should not update hyperNode status")
				})

				_, err := vcClient.TopologyV1alpha1().HyperNodes().Create(context.Background(), hyperNode, metav1.CreateOptions{})
				assert.NoError(t, err)

				return hyperNode
			},
			expectedError: false,
			expectedCount: 4,
		},
		{
			name:         "API error case",
			hyperNodeKey: "test-hypernode-3",
			setupFunc: func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode {
				hyperNode := api.BuildHyperNode("test-hypernode-3", 0, []api.MemberConfig{
					{Name: "node1", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
					{Name: "node2", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
					{Name: "node3", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
					{Name: "node4", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
					{Name: "node5", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
				})

				_, err := vcClient.TopologyV1alpha1().HyperNodes().Create(context.Background(), hyperNode, metav1.CreateOptions{})
				assert.NoError(t, err)

				// Add error when updating status
				vcClient.PrependReactor("update", "hypernodes", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
					updateAction := action.(k8stesting.UpdateAction)
					obj := updateAction.GetObject()
					hn, ok := obj.(*topologyv1alpha1.HyperNode)
					if ok && hn.Name == "test-hypernode-3" {
						return true, nil, errors.NewInternalError(assert.AnError)
					}
					return false, nil, nil
				})

				return hyperNode
			},
			expectedError: true,
			expectedCount: 5,
		},
		{
			name:         "Exact & Label & Regex Match",
			hyperNodeKey: "test-hypernode-4",
			setupFunc: func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode {
				hyperNode := api.BuildHyperNode("test-hypernode-4", 0, []api.MemberConfig{
					{Name: "master", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
					{Name: "", Type: topologyv1alpha1.MemberTypeNode, Selector: "label", LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"zone": "east"}}},
					{Name: "node[1-3]", Type: topologyv1alpha1.MemberTypeNode, Selector: "regex"},
				})
				_, err := vcClient.TopologyV1alpha1().HyperNodes().Create(context.Background(), hyperNode, metav1.CreateOptions{})
				assert.NoError(t, err)
				return hyperNode
			},
			nodes:         getNodes(),
			expectedError: false,
			expectedCount: 4,
		},
		{
			name:         "Exact & Nil Label & Regex Match",
			hyperNodeKey: "test-hypernode-5",
			setupFunc: func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode {
				hyperNode := api.BuildHyperNode("test-hypernode-5", 0, []api.MemberConfig{
					{Name: "master", Type: topologyv1alpha1.MemberTypeNode, Selector: "exact"},
					{Name: "", Type: topologyv1alpha1.MemberTypeNode, Selector: "label"},
					{Name: "node[2-4]", Type: topologyv1alpha1.MemberTypeHyperNode, Selector: "regex"},
				})
				_, err := vcClient.TopologyV1alpha1().HyperNodes().Create(context.Background(), hyperNode, metav1.CreateOptions{})
				assert.NoError(t, err)
				return hyperNode
			},
			nodes:         getNodes(),
			expectedError: false,
			expectedCount: 4,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			controller, vcClient, _ := newFakeHyperNodeController()

			// Start informer
			controller.vcInformerFactory.Start(nil)
			controller.vcInformerFactory.WaitForCacheSync(nil)

			// Set up test environment
			hyperNode := tc.setupFunc(controller, vcClient)
			if hyperNode != nil {
				// Wait for informer to update
				controller.vcInformerFactory.Topology().V1alpha1().HyperNodes().Informer().GetIndexer().Add(hyperNode)
			}

			for _, node := range tc.nodes {
				err := controller.informerFactory.Core().V1().Nodes().Informer().GetIndexer().Add(node)
				assert.NoError(t, err)
			}
			err := controller.syncHyperNodeStatus(tc.hyperNodeKey)

			// Verify results
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Verify status
			if !tc.expectedError && hyperNode != nil {
				updatedHyperNode, err := vcClient.TopologyV1alpha1().HyperNodes().Get(context.Background(), hyperNode.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedCount, updatedHyperNode.Status.NodeCount)
			}
		})
	}
}

func getNodes() []*v1.Node {
	nodes := []*v1.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
				Labels: map[string]string{
					"zone": "east",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node2",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node3",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node4",
				Labels: map[string]string{
					"zone": "west",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "master",
			},
		},
	}
	return nodes
}
