/*
Copyright 2019 The Volcano Authors.

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

package queue

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informerfactory "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/controllers/framework"
	"volcano.sh/volcano/pkg/controllers/queue/state"
)

func newFakeController() *queuecontroller {
	KubeBatchClientSet := vcclient.NewSimpleClientset()
	KubeClientSet := kubeclient.NewSimpleClientset()

	vcSharedInformers := informerfactory.NewSharedInformerFactory(KubeBatchClientSet, 0)

	controller := &queuecontroller{}
	opt := framework.ControllerOption{
		VolcanoClient:           KubeBatchClientSet,
		KubeClient:              KubeClientSet,
		VCSharedInformerFactory: vcSharedInformers,
	}

	controller.Initialize(&opt)

	return controller
}

// addQueueToStore adds a Queue to the controller's informer cache so that
// queueLister.Get / queueLister.List return it without needing a running informer.
func addQueueToStore(c *queuecontroller, q *schedulingv1beta1.Queue) {
	_ = c.queueInformer.Informer().GetStore().Add(q)
}

func TestAddQueue(t *testing.T) {
	testCases := []struct {
		Name        string
		queue       *schedulingv1beta1.Queue
		ExpectValue int
	}{
		{
			Name: "AddQueue",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "c1",
				},
				Spec: schedulingv1beta1.QueueSpec{
					Weight: 1,
				},
			},
			ExpectValue: 1,
		},
	}

	for i, testcase := range testCases {
		c := newFakeController()

		c.addQueue(testcase.queue)

		if testcase.ExpectValue != c.queue.Len() {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, c.queue.Len())
		}
	}
}

func TestDeleteQueue(t *testing.T) {
	testCases := []struct {
		Name        string
		queue       *schedulingv1beta1.Queue
		ExpectValue bool
	}{
		{
			Name: "DeleteQueue",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "c1",
				},
				Spec: schedulingv1beta1.QueueSpec{
					Weight: 1,
				},
			},
			ExpectValue: false,
		},
	}

	for i, testcase := range testCases {
		c := newFakeController()
		c.podGroups[testcase.queue.Name] = make(map[string]struct{})

		c.deleteQueue(testcase.queue)

		if _, ok := c.podGroups[testcase.queue.Name]; ok != testcase.ExpectValue {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, ok)
		}
	}

}

func TestAddPodGroup(t *testing.T) {
	namespace := "c1"

	testCases := []struct {
		Name        string
		podGroup    *schedulingv1beta1.PodGroup
		ExpectValue int
	}{
		{
			Name: "addpodgroup",
			podGroup: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pg1",
					Namespace: namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: "c1",
				},
			},
			ExpectValue: 1,
		},
	}

	for i, testcase := range testCases {
		c := newFakeController()

		c.addPodGroup(testcase.podGroup)

		if testcase.ExpectValue != c.queue.Len() {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, c.queue.Len())
		}
		if testcase.ExpectValue != len(c.podGroups[testcase.podGroup.Spec.Queue]) {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, len(c.podGroups[testcase.podGroup.Spec.Queue]))
		}
	}

}

func TestDeletePodGroup(t *testing.T) {
	namespace := "c1"

	testCases := []struct {
		Name        string
		podGroup    *schedulingv1beta1.PodGroup
		ExpectValue bool
	}{
		{
			Name: "deletepodgroup",
			podGroup: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pg1",
					Namespace: namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: "c1",
				},
			},
			ExpectValue: false,
		},
	}

	for i, testcase := range testCases {
		c := newFakeController()

		key, _ := cache.MetaNamespaceKeyFunc(testcase.podGroup)
		c.podGroups[testcase.podGroup.Spec.Queue] = make(map[string]struct{})
		c.podGroups[testcase.podGroup.Spec.Queue][key] = struct{}{}

		c.deletePodGroup(testcase.podGroup)
		if _, ok := c.podGroups[testcase.podGroup.Spec.Queue][key]; ok != testcase.ExpectValue {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, ok)
		}
	}
}

func TestUpdatePodGroup(t *testing.T) {
	namespace := "c1"

	testCases := []struct {
		Name        string
		podGroupold *schedulingv1beta1.PodGroup
		podGroupnew *schedulingv1beta1.PodGroup
		ExpectValue int
	}{
		{
			Name: "updatepodgroup",
			podGroupold: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pg1",
					Namespace: namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: "c1",
				},
				Status: schedulingv1beta1.PodGroupStatus{
					Phase: schedulingv1beta1.PodGroupPending,
				},
			},
			podGroupnew: &schedulingv1beta1.PodGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pg1",
					Namespace: namespace,
				},
				Spec: schedulingv1beta1.PodGroupSpec{
					Queue: "c1",
				},
				Status: schedulingv1beta1.PodGroupStatus{
					Phase: schedulingv1beta1.PodGroupRunning,
				},
			},
			ExpectValue: 1,
		},
	}

	for i, testcase := range testCases {
		c := newFakeController()

		c.updatePodGroup(testcase.podGroupold, testcase.podGroupnew)

		if testcase.ExpectValue != c.queue.Len() {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, c.queue.Len())
		}
	}
}

func TestSyncQueue(t *testing.T) {
	testCases := []struct {
		Name                  string
		queue                 *schedulingv1beta1.Queue
		updateStatusFnFactory func(queue *schedulingv1beta1.Queue) state.UpdateQueueStatusFn
		ExpectState           schedulingv1beta1.QueueState
	}{
		{
			Name: "From empty state to open",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "root",
				},
				Status: schedulingv1beta1.QueueStatus{
					State: "",
				},
			},
			ExpectState: schedulingv1beta1.QueueStateOpen,
			updateStatusFnFactory: func(queue *schedulingv1beta1.Queue) state.UpdateQueueStatusFn {
				return func(status *schedulingv1beta1.QueueStatus, podGroupList []string) {
					if len(queue.Status.State) == 0 {
						status.State = schedulingv1beta1.QueueStateOpen
					}
				}
			},
		},
		{
			Name: "From open to close",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "root",
				},
				Status: schedulingv1beta1.QueueStatus{
					State: schedulingv1beta1.QueueStateOpen,
				},
			},
			ExpectState: schedulingv1beta1.QueueStateClosed,
			updateStatusFnFactory: func(queue *schedulingv1beta1.Queue) state.UpdateQueueStatusFn {
				return func(status *schedulingv1beta1.QueueStatus, podGroupList []string) {
					status.State = schedulingv1beta1.QueueStateClosed
				}
			},
		},
	}

	for _, testcase := range testCases {
		c := newFakeController()

		_, err := c.vcClient.SchedulingV1beta1().Queues().Create(context.TODO(), testcase.queue, metav1.CreateOptions{})
		assert.NoError(t, err)

		updateStatusFn := testcase.updateStatusFnFactory(testcase.queue)
		err = c.syncQueue(testcase.queue, updateStatusFn)
		assert.NoError(t, err)

		item, err := c.vcClient.SchedulingV1beta1().Queues().Get(context.TODO(), testcase.queue.Name, metav1.GetOptions{})
		assert.NoError(t, err)
		assert.Equal(t, testcase.ExpectState, item.Status.State)
	}
}

func TestProcessNextWorkItem(t *testing.T) {
	testCases := []struct {
		Name        string
		ExpectValue int32
	}{
		{
			Name:        "processNextWorkItem",
			ExpectValue: 0,
		},
	}

	for i, testcase := range testCases {
		c := newFakeController()
		c.queue.Add(&apis.Request{JobName: "test"})
		bVal := c.processNextWorkItem()
		fmt.Println("The value of boolean is ", bVal)
		if c.queue.Len() != 0 {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, c.queue.Len())
		}
	}
}

func TestUpdateQueueParent(t *testing.T) {
	testCases := []struct {
		Name         string
		queue        *schedulingv1beta1.Queue
		needsCreate  bool
		expectParent string
	}{
		{
			Name: "root queue is not patched",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "root"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1},
			},
			needsCreate:  false,
			expectParent: "",
		},
		{
			Name: "queue with parent already set is not patched",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Parent: "custom-parent"},
			},
			needsCreate:  false,
			expectParent: "custom-parent",
		},
		{
			Name: "queue without parent gets parent set to root",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "orphan"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1},
			},
			needsCreate:  true,
			expectParent: "root",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			c := newFakeController()
			if tc.needsCreate {
				_, err := c.vcClient.SchedulingV1beta1().Queues().Create(context.TODO(), tc.queue, metav1.CreateOptions{})
				assert.NoError(t, err)
			}
			result, err := c.updateQueueParent(tc.queue)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectParent, result.Spec.Parent)
		})
	}
}

func TestUpdateQueueAnnotation(t *testing.T) {
	testCases := []struct {
		Name            string
		queue           *schedulingv1beta1.Queue
		annotationKey   string
		annotationValue string
		expectValue     string
		// needsCreate controls whether the queue must pre-exist in vcClient (i.e. a patch will be issued).
		needsCreate bool
	}{
		{
			Name: "annotation already set to same value is a no-op",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "q1",
					Annotations: map[string]string{
						ClosedByParentAnnotationKey: ClosedByParentAnnotationTrueValue,
					},
				},
			},
			annotationKey:   ClosedByParentAnnotationKey,
			annotationValue: ClosedByParentAnnotationTrueValue,
			expectValue:     ClosedByParentAnnotationTrueValue,
			needsCreate:     false,
		},
		{
			Name: "nil annotations map gets created with the new key",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "q2"},
			},
			annotationKey:   ClosedByParentAnnotationKey,
			annotationValue: ClosedByParentAnnotationTrueValue,
			expectValue:     ClosedByParentAnnotationTrueValue,
			needsCreate:     true,
		},
		{
			Name: "existing annotations get new key appended",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "q3",
					Annotations: map[string]string{"unrelated-key": "unrelated-value"},
				},
			},
			annotationKey:   ClosedByParentAnnotationKey,
			annotationValue: ClosedByParentAnnotationFalseValue,
			expectValue:     ClosedByParentAnnotationFalseValue,
			needsCreate:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			c := newFakeController()
			if tc.needsCreate {
				_, err := c.vcClient.SchedulingV1beta1().Queues().Create(context.TODO(), tc.queue, metav1.CreateOptions{})
				assert.NoError(t, err)
			}
			result, err := c.updateQueueAnnotation(tc.queue, tc.annotationKey, tc.annotationValue)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectValue, result.Annotations[tc.annotationKey])
		})
	}
}

func TestUpdateQueueHandler(t *testing.T) {
	testCases := []struct {
		Name               string
		oldQueue           *schedulingv1beta1.Queue
		newQueue           *schedulingv1beta1.Queue
		expectEnqueueCount int
	}{
		{
			Name: "weight change with same parent does not enqueue",
			oldQueue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Parent: "parent"},
			},
			newQueue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 5, Parent: "parent"},
			},
			expectEnqueueCount: 0,
		},
		{
			Name: "parent change triggers a sync work item",
			oldQueue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Parent: "old-parent"},
			},
			newQueue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Parent: "new-parent"},
			},
			expectEnqueueCount: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			c := newFakeController()
			c.updateQueue(tc.oldQueue, tc.newQueue)
			assert.Equal(t, tc.expectEnqueueCount, c.queue.Len())
		})
	}
}

func TestSyncHierarchicalQueue(t *testing.T) {
	testCases := []struct {
		Name               string
		child              *schedulingv1beta1.Queue
		parent             *schedulingv1beta1.Queue
		// childInVCClient indicates the child must pre-exist in vcClient so that the
		// annotation patch inside syncHierarchicalQueue can succeed.
		childInVCClient    bool
		expectEnqueueCount int
		expectAnnotation   string
	}{
		{
			Name: "root queue returns immediately without action",
			child: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "root"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateOpen},
			},
			parent:             nil,
			expectEnqueueCount: 0,
		},
		{
			Name: "parent closed - open child gets close action enqueued and ClosedByParent set",
			child: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child-a"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Parent: "par-a"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateOpen},
			},
			parent: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "par-a"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosed},
			},
			childInVCClient:    true,
			expectEnqueueCount: 1,
			expectAnnotation:   ClosedByParentAnnotationTrueValue,
		},
		{
			Name: "parent closing - open child gets close action enqueued and ClosedByParent set",
			child: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child-b"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Parent: "par-b"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateOpen},
			},
			parent: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "par-b"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosing},
			},
			childInVCClient:    true,
			expectEnqueueCount: 1,
			expectAnnotation:   ClosedByParentAnnotationTrueValue,
		},
		{
			Name: "parent closed - child already closed - no action taken",
			child: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child-c"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Parent: "par-c"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosed},
			},
			parent: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "par-c"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosed},
			},
			expectEnqueueCount: 0,
		},
		{
			Name: "parent open - child closed-by-parent - open action enqueued",
			child: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "child-d",
					Annotations: map[string]string{
						ClosedByParentAnnotationKey: ClosedByParentAnnotationTrueValue,
					},
				},
				Spec:   schedulingv1beta1.QueueSpec{Weight: 1, Parent: "par-d"},
				Status: schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosed},
			},
			parent: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "par-d"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateOpen},
			},
			expectEnqueueCount: 1,
		},
		{
			Name: "parent open - child closed but NOT by parent - no action taken",
			child: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "child-e",
					Annotations: map[string]string{
						ClosedByParentAnnotationKey: ClosedByParentAnnotationFalseValue,
					},
				},
				Spec:   schedulingv1beta1.QueueSpec{Weight: 1, Parent: "par-e"},
				Status: schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosed},
			},
			parent: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "par-e"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateOpen},
			},
			expectEnqueueCount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			c := newFakeController()
			if tc.parent != nil {
				addQueueToStore(c, tc.parent)
			}
			if tc.childInVCClient {
				_, err := c.vcClient.SchedulingV1beta1().Queues().Create(context.TODO(), tc.child, metav1.CreateOptions{})
				assert.NoError(t, err)
			}

			err := c.syncHierarchicalQueue(tc.child)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectEnqueueCount, c.queue.Len())

			if tc.expectAnnotation != "" {
				updated, err := c.vcClient.SchedulingV1beta1().Queues().Get(context.TODO(), tc.child.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				assert.Equal(t, tc.expectAnnotation, updated.Annotations[ClosedByParentAnnotationKey])
			}
		})
	}
}

func TestOpenHierarchicalQueue(t *testing.T) {
	testCases := []struct {
		Name               string
		queue              *schedulingv1beta1.Queue
		parent             *schedulingv1beta1.Queue
		children           []*schedulingv1beta1.Queue
		expectError        bool
		expectEnqueueCount int
	}{
		{
			Name: "root-parented queue with no children enqueues nothing",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "q1"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Parent: "root"},
			},
			expectError:        false,
			expectEnqueueCount: 0,
		},
		{
			Name: "parent is closing - returns error and does not open",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Parent: "par"},
			},
			parent: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "par"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosing},
			},
			expectError:        true,
			expectEnqueueCount: 0,
		},
		{
			Name: "parent is closed - returns error and does not open",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "child2"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Parent: "par2"},
			},
			parent: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "par2"},
				Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosed},
			},
			expectError:        true,
			expectEnqueueCount: 0,
		},
		{
			Name: "children with ClosedByParent annotation get open action enqueued",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "parent"},
				Spec:       schedulingv1beta1.QueueSpec{Weight: 1, Parent: "root"},
			},
			children: []*schedulingv1beta1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "c1",
						Annotations: map[string]string{
							ClosedByParentAnnotationKey: ClosedByParentAnnotationTrueValue,
						},
					},
					Spec: schedulingv1beta1.QueueSpec{Parent: "parent"},
				},
				{
					// no ClosedByParent annotation — should not be re-opened
					ObjectMeta: metav1.ObjectMeta{Name: "c2"},
					Spec:       schedulingv1beta1.QueueSpec{Parent: "parent"},
				},
			},
			expectError:        false,
			expectEnqueueCount: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			c := newFakeController()
			if tc.parent != nil {
				addQueueToStore(c, tc.parent)
			}
			for _, child := range tc.children {
				addQueueToStore(c, child)
			}

			err := c.openHierarchicalQueue(tc.queue)
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expectEnqueueCount, c.queue.Len())
		})
	}
}

func TestCloseHierarchicalQueue(t *testing.T) {
	testCases := []struct {
		Name                    string
		queue                   *schedulingv1beta1.Queue
		children                []*schedulingv1beta1.Queue
		expectContinue          bool
		expectEnqueueCount      int
		expectAnnotatedChildren []string
	}{
		{
			Name: "root queue cannot be closed - returns false",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "root"},
			},
			expectContinue:     false,
			expectEnqueueCount: 0,
		},
		{
			Name: "leaf queue with no children returns true with no work",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "leaf"},
				Spec:       schedulingv1beta1.QueueSpec{Parent: "root"},
			},
			expectContinue:     true,
			expectEnqueueCount: 0,
		},
		{
			Name: "open children get ClosedByParent annotation and close action enqueued",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "par"},
				Spec:       schedulingv1beta1.QueueSpec{Parent: "root"},
			},
			children: []*schedulingv1beta1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "open-child"},
					Spec:       schedulingv1beta1.QueueSpec{Parent: "par"},
					Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateOpen},
				},
			},
			expectContinue:          true,
			expectEnqueueCount:      1,
			expectAnnotatedChildren: []string{"open-child"},
		},
		{
			Name: "already closed and closing children are skipped entirely",
			queue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "par2"},
				Spec:       schedulingv1beta1.QueueSpec{Parent: "root"},
			},
			children: []*schedulingv1beta1.Queue{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "closed-child"},
					Spec:       schedulingv1beta1.QueueSpec{Parent: "par2"},
					Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosed},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "closing-child"},
					Spec:       schedulingv1beta1.QueueSpec{Parent: "par2"},
					Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosing},
				},
			},
			expectContinue:     true,
			expectEnqueueCount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			c := newFakeController()
			for _, child := range tc.children {
				addQueueToStore(c, child)
				// Children that will be annotated must also exist in vcClient so the patch succeeds.
				if child.Status.State != schedulingv1beta1.QueueStateClosed &&
					child.Status.State != schedulingv1beta1.QueueStateClosing {
					_, err := c.vcClient.SchedulingV1beta1().Queues().Create(context.TODO(), child, metav1.CreateOptions{})
					assert.NoError(t, err)
				}
			}

			continued, err := c.closeHierarchicalQueue(tc.queue)
			assert.NoError(t, err)
			assert.Equal(t, tc.expectContinue, continued)
			assert.Equal(t, tc.expectEnqueueCount, c.queue.Len())

			for _, childName := range tc.expectAnnotatedChildren {
				updated, err := c.vcClient.SchedulingV1beta1().Queues().Get(context.TODO(), childName, metav1.GetOptions{})
				assert.NoError(t, err)
				assert.Equal(t, ClosedByParentAnnotationTrueValue, updated.Annotations[ClosedByParentAnnotationKey])
			}
		})
	}
}
