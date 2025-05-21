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
	testing2 "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientsetfake "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
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
		name           string
		hyperNodeKey   string
		setupFunc      func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode
		expectedError  bool
		expectedUpdate bool
		expectedCount  int64
	}{
		{
			name:         "Normal case: NodeCount needs to be updated",
			hyperNodeKey: "test-hypernode-1",
			setupFunc: func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode {
				hyperNode := &topologyv1alpha1.HyperNode{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-hypernode-1",
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
							{
								Type: topologyv1alpha1.MemberTypeNode,
								Selector: topologyv1alpha1.MemberSelector{
									ExactMatch: &topologyv1alpha1.ExactMatch{
										Name: "node2",
									},
								},
							},
							{
								Type: topologyv1alpha1.MemberTypeNode,
								Selector: topologyv1alpha1.MemberSelector{
									ExactMatch: &topologyv1alpha1.ExactMatch{
										Name: "node3",
									},
								},
							},
						},
					},
					Status: topologyv1alpha1.HyperNodeStatus{
						NodeCount: 0, // Initial count does not match actual count
					},
				}

				_, err := vcClient.TopologyV1alpha1().HyperNodes().Create(context.Background(), hyperNode, metav1.CreateOptions{})
				assert.NoError(t, err)

				return hyperNode
			},
			expectedError:  false,
			expectedUpdate: true,
			expectedCount:  3,
		},
		{
			name:         "Node does not exist",
			hyperNodeKey: "non-existent",
			setupFunc: func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode {
				return nil
			},
			expectedError:  false,
			expectedUpdate: false,
			expectedCount:  0,
		},
		{
			name:         "NodeCount already matches, no update needed",
			hyperNodeKey: "test-hypernode-2",
			setupFunc: func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode {
				hyperNode := &topologyv1alpha1.HyperNode{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-hypernode-2",
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
							{
								Type: topologyv1alpha1.MemberTypeNode,
								Selector: topologyv1alpha1.MemberSelector{
									ExactMatch: &topologyv1alpha1.ExactMatch{
										Name: "node2",
									},
								},
							},
							{
								Type: topologyv1alpha1.MemberTypeNode,
								Selector: topologyv1alpha1.MemberSelector{
									ExactMatch: &topologyv1alpha1.ExactMatch{
										Name: "node3",
									},
								},
							},
							{
								Type: topologyv1alpha1.MemberTypeNode,
								Selector: topologyv1alpha1.MemberSelector{
									ExactMatch: &topologyv1alpha1.ExactMatch{
										Name: "node4",
									},
								},
							},
						},
					},
					Status: topologyv1alpha1.HyperNodeStatus{
						NodeCount: 4, // Already matches, no update needed
					},
				}

				_, err := vcClient.TopologyV1alpha1().HyperNodes().Create(context.Background(), hyperNode, metav1.CreateOptions{})
				assert.NoError(t, err)

				return hyperNode
			},
			expectedError:  false,
			expectedUpdate: false,
			expectedCount:  4,
		},
		{
			name:         "API error case",
			hyperNodeKey: "test-hypernode-3",
			setupFunc: func(controller *hyperNodeController, vcClient *vcclientsetfake.Clientset) *topologyv1alpha1.HyperNode {
				hyperNode := &topologyv1alpha1.HyperNode{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-hypernode-3",
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
							{
								Type: topologyv1alpha1.MemberTypeNode,
								Selector: topologyv1alpha1.MemberSelector{
									ExactMatch: &topologyv1alpha1.ExactMatch{
										Name: "node2",
									},
								},
							},
							{
								Type: topologyv1alpha1.MemberTypeNode,
								Selector: topologyv1alpha1.MemberSelector{
									ExactMatch: &topologyv1alpha1.ExactMatch{
										Name: "node3",
									},
								},
							},
							{
								Type: topologyv1alpha1.MemberTypeNode,
								Selector: topologyv1alpha1.MemberSelector{
									ExactMatch: &topologyv1alpha1.ExactMatch{
										Name: "node4",
									},
								},
							},
							{
								Type: topologyv1alpha1.MemberTypeNode,
								Selector: topologyv1alpha1.MemberSelector{
									ExactMatch: &topologyv1alpha1.ExactMatch{
										Name: "node5",
									},
								},
							},
						},
					},
					Status: topologyv1alpha1.HyperNodeStatus{
						NodeCount: 0, // Needs update
					},
				}

				_, err := vcClient.TopologyV1alpha1().HyperNodes().Create(context.Background(), hyperNode, metav1.CreateOptions{})
				assert.NoError(t, err)

				// Add error when updating status
				vcClient.PrependReactor("update", "hypernodes", func(action testing2.Action) (handled bool, ret runtime.Object, err error) {
					updateAction := action.(testing2.UpdateAction)
					obj := updateAction.GetObject()
					hn, ok := obj.(*topologyv1alpha1.HyperNode)
					if ok && hn.Name == "test-hypernode-3" {
						return true, nil, errors.NewInternalError(assert.AnError)
					}
					return false, nil, nil
				})

				return hyperNode
			},
			expectedError:  true,
			expectedUpdate: true,
			expectedCount:  5,
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

			err := controller.syncHyperNodeStatus(tc.hyperNodeKey)

			// Verify results
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// If update is expected and no error, verify status
			if tc.expectedUpdate && !tc.expectedError && hyperNode != nil {
				updatedHyperNode, err := vcClient.TopologyV1alpha1().HyperNodes().Get(context.Background(), hyperNode.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				assert.Equal(t, tc.expectedCount, updatedHyperNode.Status.NodeCount)
			}
		})
	}
}
