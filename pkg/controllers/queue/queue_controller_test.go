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

	busv1alpha1 "volcano.sh/apis/pkg/apis/bus/v1alpha1"
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

		c.podGroups[testcase.podGroup.Spec.Queue][key] = struct{}{}
		c.deletePodGroup(cache.DeletedFinalStateUnknown{Key: key, Obj: testcase.podGroup})
		if _, ok := c.podGroups[testcase.podGroup.Spec.Queue][key]; ok != testcase.ExpectValue {
			t.Errorf("case %d (%s) tombstone: expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, ok)
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

func TestCloseQueueAfterQueueRecreate(t *testing.T) {
	testCases := []struct {
		Name            string
		PodGroups       []*schedulingv1beta1.PodGroup
		ExpectState     schedulingv1beta1.QueueState
		ExpectPodGroups int
	}{
		{
			Name: "close after recreate observes the running PodGroup",
			PodGroups: []*schedulingv1beta1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "ns1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "c1",
					},
					Status: schedulingv1beta1.PodGroupStatus{
						Phase: schedulingv1beta1.PodGroupRunning,
					},
				},
			},
			ExpectState:     schedulingv1beta1.QueueStateClosing,
			ExpectPodGroups: 1,
		},
		{
			Name:            "close after recreate with no PodGroup reports Closed",
			ExpectState:     schedulingv1beta1.QueueStateClosed,
			ExpectPodGroups: 0,
		},
	}

	for _, testcase := range testCases {
		c := newFakeController()
		queue := &schedulingv1beta1.Queue{
			ObjectMeta: metav1.ObjectMeta{
				Name: "c1",
			},
			Spec: schedulingv1beta1.QueueSpec{
				Weight: 1,
			},
		}

		for _, pg := range testcase.PodGroups {
			assert.NoError(t, c.pgInformer.Informer().GetStore().Add(pg))
			c.addPodGroup(pg)
		}

		// The queue is deleted while its PodGroups are still running and then
		// recreated under the same name. The recreated queue receives no
		// PodGroup event for the PodGroups that kept running.
		c.deleteQueue(queue)
		assert.Empty(t, c.getPodGroups(queue.Name))

		assert.NoError(t, c.queueInformer.Informer().GetStore().Add(queue))
		_, err := c.vcClient.SchedulingV1beta1().Queues().Create(context.TODO(), queue, metav1.CreateOptions{})
		assert.NoError(t, err)

		err = c.handleQueue(&apis.Request{
			QueueName: queue.Name,
			Event:     busv1alpha1.OutOfSyncEvent,
			Action:    busv1alpha1.CloseQueueAction,
		})
		assert.NoError(t, err)

		item, err := c.vcClient.SchedulingV1beta1().Queues().Get(context.TODO(), queue.Name, metav1.GetOptions{})
		assert.NoError(t, err)
		assert.Equal(t, testcase.ExpectState, item.Status.State)
		assert.Len(t, c.getPodGroups(queue.Name), testcase.ExpectPodGroups)
	}
}

func TestReconcilePodGroups(t *testing.T) {
	testCases := []struct {
		Name            string
		PodGroups       []*schedulingv1beta1.PodGroup
		Cache           map[string]struct{}
		ExpectPodGroups []string
		ExpectCache     map[string]struct{}
	}{
		{
			Name: "adds PodGroups that are missing from the cache",
			PodGroups: []*schedulingv1beta1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "ns1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "c1",
					},
				},
			},
			ExpectPodGroups: []string{"ns1/pg1"},
			ExpectCache:     map[string]struct{}{"ns1/pg1": {}},
		},
		{
			Name:            "drops cache entries that no longer exist",
			Cache:           map[string]struct{}{"ns1/stale": {}},
			ExpectPodGroups: []string{},
			ExpectCache:     map[string]struct{}{},
		},
		{
			Name: "matches the cache to the informer in both directions",
			PodGroups: []*schedulingv1beta1.PodGroup{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pg1",
						Namespace: "ns1",
					},
					Spec: schedulingv1beta1.PodGroupSpec{
						Queue: "c1",
					},
				},
			},
			Cache:           map[string]struct{}{"ns1/stale": {}},
			ExpectPodGroups: []string{"ns1/pg1"},
			ExpectCache:     map[string]struct{}{"ns1/pg1": {}},
		},
	}

	for _, testcase := range testCases {
		c := newFakeController()
		for _, pg := range testcase.PodGroups {
			assert.NoError(t, c.pgInformer.Informer().GetStore().Add(pg))
		}

		if testcase.Cache != nil {
			current := make(map[string]struct{}, len(testcase.Cache))
			for key := range testcase.Cache {
				current[key] = struct{}{}
			}
			c.podGroups["c1"] = current
		}

		podGroups := c.reconcilePodGroups("c1")

		assert.ElementsMatch(t, testcase.ExpectPodGroups, podGroups)
		assert.Equal(t, testcase.ExpectCache, c.podGroups["c1"])
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
