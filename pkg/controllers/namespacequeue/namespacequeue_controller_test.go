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

package namespacequeue

import (
	"context"
	"testing"

	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informerfactory "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/framework"
)

func newFakeNamespaceQueueController(t *testing.T) *namespaceQueueController {
	t.Helper()

	vcClient := vcclient.NewSimpleClientset()
	vcInformerFactory := informerfactory.NewSharedInformerFactory(vcClient, 0)
	controller := &namespaceQueueController{}

	err := controller.Initialize(&framework.ControllerOption{
		VolcanoClient:           vcClient,
		VCSharedInformerFactory: vcInformerFactory,
		WorkerThreadsForQueue:   1,
	})
	if err != nil {
		t.Fatalf("failed to initialize controller: %v", err)
	}

	return controller
}

func TestAddNamespaceQueue(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	nq := newNamespaceQueue("team-a", "cluster/research")
	nq.Name = "training"

	controller.addNamespaceQueue(nq)
	expectWorkQueueKey(t, controller, "team-a/training")
}

func TestUpdateNamespaceQueue(t *testing.T) {
	tests := []struct {
		name       string
		oldParent  string
		newParent  string
		wantQueued bool
	}{
		{
			name:       "status-only update is ignored",
			oldParent:  "cluster/research",
			newParent:  "cluster/research",
			wantQueued: false,
		},
		{
			name:       "spec update is queued",
			oldParent:  "cluster/research",
			newParent:  "department",
			wantQueued: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := newFakeNamespaceQueueController(t)
			oldNQ := newNamespaceQueue("team-a", tt.oldParent)
			oldNQ.Name = "training"
			oldNQ.ResourceVersion = "1"
			newNQ := oldNQ.DeepCopy()
			newNQ.Spec.Parent = tt.newParent
			newNQ.ResourceVersion = "2"

			if !tt.wantQueued {
				newNQ.Status.State = schedulingv1beta1.QueueStateOpen
			}

			controller.updateNamespaceQueue(oldNQ, newNQ)
			if tt.wantQueued {
				expectWorkQueueKey(t, controller, "team-a/training")
				return
			}

			if controller.workQueue.Len() != 0 {
				t.Fatalf("status-only update queued %d item(s)", controller.workQueue.Len())
			}
		})
	}
}

func TestDeleteNamespaceQueue(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
	}{
		{
			name: "direct object",
			obj: func() interface{} {
				nq := newNamespaceQueue("team-a", "cluster/research")
				nq.Name = "training"
				return nq
			}(),
		},
		{
			name: "tombstone",
			obj: cache.DeletedFinalStateUnknown{
				Key: "team-a/training",
				Obj: func() *schedulingv1beta1.NamespaceQueue {
					nq := newNamespaceQueue("team-a", "cluster/research")
					nq.Name = "training"
					return nq
				}(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := newFakeNamespaceQueueController(t)
			controller.deleteNamespaceQueue(tt.obj)
			expectWorkQueueKey(t, controller, "team-a/training")
		})
	}
}

func TestAddQueue(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	addNamespaceQueuesForQueueEvent(t, controller)

	controller.addQueue(&schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
	})

	// Only the NamespaceQueue whose parent is cluster/research should be queued.
	expectWorkQueueKey(t, controller, "team-a/training")
}

func TestUpdateQueue(t *testing.T) {
	tests := []struct {
		name       string
		newVersion string
		wantQueued bool
	}{
		{
			name:       "resource version is unchanged",
			newVersion: "1",
			wantQueued: false,
		},
		{
			name:       "resource version changed",
			newVersion: "2",
			wantQueued: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := newFakeNamespaceQueueController(t)
			addNamespaceQueuesForQueueEvent(t, controller)

			oldQueue := &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "research",
					ResourceVersion: "1",
				},
			}
			newQueue := oldQueue.DeepCopy()
			newQueue.ResourceVersion = tt.newVersion

			controller.updateQueue(oldQueue, newQueue)
			if tt.wantQueued {
				expectWorkQueueKey(t, controller, "team-a/training")
				return
			}

			if controller.workQueue.Len() != 0 {
				t.Fatalf("workQueue length = %d, want 0", controller.workQueue.Len())
			}
		})
	}
}

func TestDeleteQueue(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
	}{
		{
			name: "direct object",
			obj: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "research"},
			},
		},
		{
			name: "tombstone",
			obj: cache.DeletedFinalStateUnknown{
				Key: "research",
				Obj: &schedulingv1beta1.Queue{
					ObjectMeta: metav1.ObjectMeta{Name: "research"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := newFakeNamespaceQueueController(t)
			addNamespaceQueuesForQueueEvent(t, controller)

			controller.deleteQueue(tt.obj)
			expectWorkQueueKey(t, controller, "team-a/training")
		})
	}
}

func addNamespaceQueuesForQueueEvent(
	t *testing.T,
	controller *namespaceQueueController,
) {
	t.Helper()

	matching := newNamespaceQueue("team-a", "cluster/research")
	matching.Name = "training"

	otherClusterQueue := newNamespaceQueue("team-b", "cluster/production")
	otherClusterQueue.Name = "training"

	localParent := newNamespaceQueue("team-a", "research")
	localParent.Name = "local"

	for _, nq := range []*schedulingv1beta1.NamespaceQueue{
		matching,
		otherClusterQueue,
		localParent,
	} {
		if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(nq); err != nil {
			t.Fatalf("failed to add NamespaceQueue to indexer: %v", err)
		}
	}
}

func TestSyncNamespaceQueue(t *testing.T) {
	tests := []struct {
		name        string
		nq          *schedulingv1beta1.NamespaceQueue
		parentNQ    *schedulingv1beta1.NamespaceQueue
		parentQueue *schedulingv1beta1.Queue
		wantError   bool
	}{
		{
			name: "cluster parent exists",
			nq: func() *schedulingv1beta1.NamespaceQueue {
				nq := newNamespaceQueue("team-a", "cluster/research")
				nq.Name = "training"
				return nq
			}(),
			parentQueue: &schedulingv1beta1.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "research"},
			},
		},
		{
			name: "namespace parent exists",
			nq: func() *schedulingv1beta1.NamespaceQueue {
				nq := newNamespaceQueue("team-a", "department")
				nq.Name = "training"
				return nq
			}(),
			parentNQ: func() *schedulingv1beta1.NamespaceQueue {
				nq := newNamespaceQueue("team-a", "cluster/default")
				nq.Name = "department"
				nq.Status.State = schedulingv1beta1.QueueStateOpen
				nq.Status.Conditions = []metav1.Condition{{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
				}}
				return nq
			}(),
		},
		{
			name: "missing parent does not return an error",
			nq: func() *schedulingv1beta1.NamespaceQueue {
				nq := newNamespaceQueue("team-a", "cluster/missing")
				nq.Name = "training"
				return nq
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := newFakeNamespaceQueueController(t)
			if _, err := controller.vcClient.
				SchedulingV1beta1().
				NamespaceQueues(tt.nq.Namespace).
				Create(context.Background(), tt.nq.DeepCopy(), metav1.CreateOptions{}); err != nil {
				t.Fatalf("failed to create NamespaceQueue in fake client: %v", err)
			}
			if tt.parentQueue != nil {
				if err := controller.queueInformer.Informer().GetIndexer().Add(tt.parentQueue); err != nil {
					t.Fatalf("failed to add parent Queue to indexer: %v", err)
				}
			}
			if tt.parentNQ != nil {
				if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(tt.parentNQ); err != nil {
					t.Fatalf("failed to add parent NamespaceQueue to indexer: %v", err)
				}
			}
			if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(tt.nq); err != nil {
				t.Fatalf("failed to add NamespaceQueue to indexer: %v", err)
			}

			err := controller.syncNamespaceQueue("team-a/training")
			if (err != nil) != tt.wantError {
				t.Fatalf("syncNamespaceQueue() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestNamespaceQueueParentReadiness(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	parent := newNamespaceQueue("team-a", "cluster/default")
	parent.Name = "department"
	parent.Status.State = schedulingv1beta1.QueueStateOpen
	parent.Status.Conditions = []metav1.Condition{{
		Type:   "Ready",
		Status: metav1.ConditionFalse,
	}}
	child := newNamespaceQueue("team-a", "department")
	child.Name = "training"

	if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(parent); err != nil {
		t.Fatalf("failed to add parent NamespaceQueue: %v", err)
	}
	if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(child); err != nil {
		t.Fatalf("failed to add child NamespaceQueue: %v", err)
	}
	if _, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues("team-a").Create(
		context.Background(), child.DeepCopy(), metav1.CreateOptions{},
	); err != nil {
		t.Fatalf("failed to create child NamespaceQueue: %v", err)
	}

	if err := controller.syncNamespaceQueue("team-a/training"); err != nil {
		t.Fatalf("syncNamespaceQueue() error = %v", err)
	}
	updated, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues("team-a").Get(
		context.Background(), "training", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("failed to get updated NamespaceQueue: %v", err)
	}
	condition := apiMeta.FindStatusCondition(updated.Status.Conditions, "Ready")
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "ParentNotReady" {
		t.Fatalf("Ready condition = %#v, want False/ParentNotReady", condition)
	}
}

func TestNamespaceQueueParentStatusUpdateRequeuesChildren(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	child := newNamespaceQueue("team-a", "department")
	child.Name = "training"
	if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(child); err != nil {
		t.Fatalf("failed to add child NamespaceQueue: %v", err)
	}

	oldParent := newNamespaceQueue("team-a", "cluster/default")
	oldParent.Name = "department"
	oldParent.ResourceVersion = "1"
	newParent := oldParent.DeepCopy()
	newParent.ResourceVersion = "2"
	newParent.Status.State = schedulingv1beta1.QueueStateOpen
	newParent.Status.Conditions = []metav1.Condition{{
		Type:   "Ready",
		Status: metav1.ConditionTrue,
	}}

	controller.updateNamespaceQueue(oldParent, newParent)
	expectWorkQueueKey(t, controller, "team-a/training")
}

func TestProcessNextWorkItem(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	called := false
	controller.syncHandler = func(key string) error {
		called = true
		if key != "team-a/training" {
			t.Errorf("syncHandler() key = %q, want %q", key, "team-a/training")
		}
		return nil
	}

	controller.workQueue.Add("team-a/training")
	if !controller.processNextWorkItem() {
		t.Fatal("processNextWorkItem() returned false")
	}
	if !called {
		t.Fatal("syncHandler was not called")
	}
	if controller.workQueue.Len() != 0 {
		t.Fatalf("workQueue has %d item(s) after successful processing", controller.workQueue.Len())
	}
}

func expectWorkQueueKey(t *testing.T, controller *namespaceQueueController, want string) {
	t.Helper()
	if controller.workQueue.Len() != 1 {
		t.Fatalf("workQueue length = %d, want 1", controller.workQueue.Len())
	}

	got, shutdown := controller.workQueue.Get()
	if shutdown {
		t.Fatal("workQueue unexpectedly shut down")
	}
	defer controller.workQueue.Done(got)
	controller.workQueue.Forget(got)

	if got != want {
		t.Fatalf("workQueue key = %q, want %q", got, want)
	}
}
