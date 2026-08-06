/*
Copyright 2026 The Volcano Authors.

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
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informerfactory "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/framework"
	controllerutil "volcano.sh/volcano/pkg/util"
)

func TestNamespaceQueueControllerFlags(t *testing.T) {
	controller := &namespaceQueueController{}
	flags := pflag.NewFlagSet("namespacequeue-controller", pflag.ContinueOnError)
	controller.AddFlags(flags)
	if err := flags.Parse([]string{"--max-namespacequeue-depth=7"}); err != nil {
		t.Fatalf("failed to parse controller flags: %v", err)
	}
	if controller.maxNamespaceQueueDepth != 7 {
		t.Fatalf("maxNamespaceQueueDepth = %d, want 7", controller.maxNamespaceQueueDepth)
	}
}

func TestNamespaceQueueControllerRejectsInvalidDepth(t *testing.T) {
	controller := &namespaceQueueController{}
	err := controller.Initialize(&framework.ControllerOption{
		KubeClient:              kubefake.NewSimpleClientset(),
		VolcanoClient:           vcclient.NewSimpleClientset(),
		VCSharedInformerFactory: informerfactory.NewSharedInformerFactory(vcclient.NewSimpleClientset(), 0),
	})
	if err == nil {
		t.Fatal("Initialize() succeeded with an invalid NamespaceQueue depth")
	}
}

func newFakeNamespaceQueueController(t *testing.T) *namespaceQueueController {
	t.Helper()

	vcClient := vcclient.NewSimpleClientset()
	vcInformerFactory := informerfactory.NewSharedInformerFactory(vcClient, 0)
	controller := &namespaceQueueController{
		maxNamespaceQueueDepth: controllerutil.DefaultMaxNamespaceQueueDepth,
	}

	err := controller.Initialize(&framework.ControllerOption{
		KubeClient:              kubefake.NewSimpleClientset(),
		VolcanoClient:           vcClient,
		VCSharedInformerFactory: vcInformerFactory,
		WorkerThreadsForQueue:   1,
	})
	if err != nil {
		t.Fatalf("failed to initialize controller: %v", err)
	}
	t.Cleanup(controller.eventBroadcaster.Shutdown)

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

func TestUpdateNamespaceQueueRequeuesOldAndNewParentSubtrees(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	oldParent := newTestNamespaceQueue("team-a", "old-parent", "cluster/research")
	newParent := newTestNamespaceQueue("team-a", "new-parent", "cluster/research")
	oldChild := newTestNamespaceQueue("team-a", "old-child", "old-parent")
	newChild := newTestNamespaceQueue("team-a", "new-child", "new-parent")
	for _, queue := range []*schedulingv1beta1.NamespaceQueue{oldParent, newParent, oldChild, newChild} {
		if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(queue); err != nil {
			t.Fatalf("failed to add NamespaceQueue %s/%s: %v", queue.Namespace, queue.Name, err)
		}
	}

	oldQueue := newTestNamespaceQueue("team-a", "target", "old-parent")
	oldQueue.ResourceVersion = "1"
	newQueue := oldQueue.DeepCopy()
	newQueue.Spec.Parent = "new-parent"
	newQueue.ResourceVersion = "2"

	controller.updateNamespaceQueue(oldQueue, newQueue)
	expectWorkQueueKeys(t, controller, []string{
		"team-a/target",
		"team-a/old-child",
		"team-a/new-child",
	})
}

func TestUpdateNamespaceQueueRequeuesDeletion(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	oldQueue := newTestNamespaceQueue("team-a", "training", "cluster/research")
	oldQueue.ResourceVersion = "1"
	newQueue := oldQueue.DeepCopy()
	newQueue.ResourceVersion = "2"
	now := metav1.Now()
	newQueue.DeletionTimestamp = &now

	controller.updateNamespaceQueue(oldQueue, newQueue)
	expectWorkQueueKey(t, controller, "team-a/training")
}

func TestNamespaceQueueCleanupOnlySkipsNonDeletingObjects(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	controller.cleanupOnly = true
	queue := newNamespaceQueue("team-a", "cluster/research")
	queue.Name = "training"

	if err := controller.reconcileNamespaceQueue(queue); err != nil {
		t.Fatalf("reconcileNamespaceQueue() error = %v", err)
	}
}

func TestRecordConditionEvents(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	recorder := record.NewFakeRecorder(10)
	controller.recorder = recorder
	oldQueue := newNamespaceQueue("team-a", "cluster/research")
	oldQueue.Name = "training"
	newQueue := oldQueue.DeepCopy()
	newQueue.Status.Conditions = []metav1.Condition{
		{
			Type:   "Authorized",
			Status: metav1.ConditionFalse,
			Reason: "NamespaceNotAllowed",
		},
		{
			Type:   "Ready",
			Status: metav1.ConditionFalse,
			Reason: "NamespaceNotAllowed",
		},
	}

	controller.recordConditionEvents(oldQueue, newQueue)
	for range 2 {
		select {
		case <-recorder.Events:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for condition event")
		}
	}
	select {
	case event := <-recorder.Events:
		t.Fatalf("unexpected extra event: %s", event)
	default:
	}

	controller.recordConditionEvents(newQueue, newQueue)
	select {
	case event := <-recorder.Events:
		t.Fatalf("unchanged condition emitted event: %s", event)
	default:
	}

	updatedGeneration := newQueue.DeepCopy()
	updatedGeneration.Status.Conditions[0].ObservedGeneration++
	updatedGeneration.Status.Conditions[1].ObservedGeneration++
	controller.recordConditionEvents(newQueue, updatedGeneration)
	select {
	case event := <-recorder.Events:
		t.Fatalf("generation-only condition update emitted event: %s", event)
	default:
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
			if tt.wantQueued {
				newQueue.Spec.AllowedNamespaces = []string{"team-a"}
			}

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

func TestNamespaceQueueParentIndex(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	children := []*schedulingv1beta1.NamespaceQueue{
		func() *schedulingv1beta1.NamespaceQueue {
			nq := newNamespaceQueue("team-a", "cluster/default")
			nq.Name = "training"
			return nq
		}(),
		func() *schedulingv1beta1.NamespaceQueue {
			nq := newNamespaceQueue("team-a", "training")
			nq.Name = "job"
			return nq
		}(),
		func() *schedulingv1beta1.NamespaceQueue {
			nq := newNamespaceQueue("team-b", "cluster/default")
			nq.Name = "inference"
			return nq
		}(),
	}
	for _, child := range children {
		if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(child); err != nil {
			t.Fatalf("failed to add NamespaceQueue %s/%s: %v", child.Namespace, child.Name, err)
		}
	}

	got, err := controller.getDirectChildNamespaceQueues(
		controllerutil.ResolvedQueueReference{
			Scope: controllerutil.ClusterQueueReferenceScope,
			Name:  "default",
		},
	)
	if err != nil {
		t.Fatalf("getDirectChildNamespaceQueues() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("direct child count = %d, want 2", len(got))
	}
}

func TestEnqueueDescendantNamespaceQueues(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	queues := []*schedulingv1beta1.NamespaceQueue{
		newTestNamespaceQueue("team-a", "training", "cluster/default"),
		newTestNamespaceQueue("team-a", "job", "training"),
		newTestNamespaceQueue("team-a", "task", "job"),
		newTestNamespaceQueue("team-b", "inference", "cluster/default"),
		newTestNamespaceQueue("team-b", "other", "cluster/other"),
	}
	for _, queue := range queues {
		if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(queue); err != nil {
			t.Fatalf("failed to add NamespaceQueue %s/%s: %v", queue.Namespace, queue.Name, err)
		}
	}

	controller.enqueueDescendantNamespaceQueues(controllerutil.ResolvedQueueReference{
		Scope: controllerutil.ClusterQueueReferenceScope,
		Name:  "default",
	})
	expectWorkQueueKeys(t, controller, []string{
		"team-a/training",
		"team-a/job",
		"team-a/task",
		"team-b/inference",
	})
}

func TestEnqueueDescendantNamespaceQueuesStopsAtCycle(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	for _, queue := range []*schedulingv1beta1.NamespaceQueue{
		newTestNamespaceQueue("team-a", "a", "b"),
		newTestNamespaceQueue("team-a", "b", "a"),
	} {
		if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(queue); err != nil {
			t.Fatalf("failed to add NamespaceQueue %s/%s: %v", queue.Namespace, queue.Name, err)
		}
	}

	controller.enqueueDescendantNamespaceQueues(controllerutil.ResolvedQueueReference{
		Scope:     controllerutil.NamespaceQueueReferenceScope,
		Namespace: "team-a",
		Name:      "a",
	})
	expectWorkQueueKeys(t, controller, []string{"team-a/b"})
}

func newTestNamespaceQueue(namespace, name, parent string) *schedulingv1beta1.NamespaceQueue {
	nq := newNamespaceQueue(namespace, parent)
	nq.Name = name
	return nq
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
	parent.Status.Conditions = []metav1.Condition{
		{Type: "Authorized", Status: metav1.ConditionTrue},
		{Type: "Ready", Status: metav1.ConditionFalse},
	}
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

func TestNamespaceQueueClusterParentReadiness(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	queue := newNamespaceQueue("team-a", "cluster/research")
	queue.Name = "training"
	if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(queue); err != nil {
		t.Fatalf("failed to add NamespaceQueue: %v", err)
	}
	if _, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues("team-a").Create(
		context.Background(), queue.DeepCopy(), metav1.CreateOptions{},
	); err != nil {
		t.Fatalf("failed to create NamespaceQueue: %v", err)
	}

	parent := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Spec: schedulingv1beta1.QueueSpec{
			AllowedNamespaces: []string{"team-a"},
		},
		Status: schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateClosed},
	}
	if err := controller.queueInformer.Informer().GetIndexer().Add(parent); err != nil {
		t.Fatalf("failed to add parent Queue: %v", err)
	}

	if err := controller.syncNamespaceQueue("team-a/training"); err != nil {
		t.Fatalf("syncNamespaceQueue() error = %v", err)
	}
	updated, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues("team-a").Get(
		context.Background(), "training", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("failed to get NamespaceQueue: %v", err)
	}
	ready := apiMeta.FindStatusCondition(updated.Status.Conditions, "Ready")
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != "ParentNotReady" {
		t.Fatalf("Ready condition = %#v, want False/ParentNotReady", ready)
	}
	authorized := apiMeta.FindStatusCondition(updated.Status.Conditions, "Authorized")
	if authorized == nil || authorized.Status != metav1.ConditionTrue {
		t.Fatalf("Authorized condition = %#v, want True", authorized)
	}
}

func TestNamespaceQueueHierarchyCycleCondition(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	queues := []*schedulingv1beta1.NamespaceQueue{
		newTestNamespaceQueue("team-a", "a", "b"),
		newTestNamespaceQueue("team-a", "b", "a"),
	}
	for _, queue := range queues {
		if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(queue); err != nil {
			t.Fatalf("failed to add NamespaceQueue %s/%s: %v", queue.Namespace, queue.Name, err)
		}
		if _, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues(queue.Namespace).Create(
			context.Background(), queue.DeepCopy(), metav1.CreateOptions{},
		); err != nil {
			t.Fatalf("failed to create NamespaceQueue %s/%s: %v", queue.Namespace, queue.Name, err)
		}
	}

	if err := controller.syncNamespaceQueue("team-a/a"); err != nil {
		t.Fatalf("syncNamespaceQueue() error = %v", err)
	}
	updated, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues("team-a").Get(
		context.Background(), "a", metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("failed to get NamespaceQueue: %v", err)
	}
	ready := apiMeta.FindStatusCondition(updated.Status.Conditions, "Ready")
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != "HierarchyCycle" {
		t.Fatalf("Ready condition = %#v, want False/HierarchyCycle", ready)
	}
}

func TestIsNamespaceQueueReadyRequiresCurrentGeneration(t *testing.T) {
	queue := newNamespaceQueue("team-a", "cluster/default")
	queue.Generation = 2
	queue.Status.State = schedulingv1beta1.QueueStateOpen
	queue.Status.Conditions = []metav1.Condition{{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 1,
	}}

	if isNamespaceQueueReady(queue) {
		t.Fatal("isNamespaceQueueReady() = true for a stale Ready condition")
	}

	queue.Status.Conditions[0].ObservedGeneration = queue.Generation
	if !isNamespaceQueueReady(queue) {
		t.Fatal("isNamespaceQueueReady() = false for a current Ready condition")
	}
}

func TestNamespaceQueuePodGroupHandlers(t *testing.T) {
	t.Run("add namespace queue PodGroup", func(t *testing.T) {
		controller := newFakeNamespaceQueueController(t)
		controller.addPodGroup(newPodGroup("team-a", "pg", "namespace/training", schedulingv1beta1.PodGroupPending))
		expectWorkQueueKey(t, controller, "team-a/training")
	})

	t.Run("ignore cluster queue PodGroup", func(t *testing.T) {
		controller := newFakeNamespaceQueueController(t)
		controller.addPodGroup(newPodGroup("team-a", "pg", "research", schedulingv1beta1.PodGroupPending))
		if controller.workQueue.Len() != 0 {
			t.Fatalf("workQueue length = %d, want 0", controller.workQueue.Len())
		}
	})

	t.Run("phase update requeues namespace queue", func(t *testing.T) {
		controller := newFakeNamespaceQueueController(t)
		oldPG := newPodGroup("team-a", "pg", "namespace/training", schedulingv1beta1.PodGroupPending)
		oldPG.ResourceVersion = "1"
		newPG := oldPG.DeepCopy()
		newPG.ResourceVersion = "2"
		newPG.Status.Phase = schedulingv1beta1.PodGroupRunning

		controller.updatePodGroup(oldPG, newPG)
		expectWorkQueueKey(t, controller, "team-a/training")
	})

	t.Run("queue reference update requeues old and new namespace queues", func(t *testing.T) {
		controller := newFakeNamespaceQueueController(t)
		oldPG := newPodGroup("team-a", "pg", "namespace/training", schedulingv1beta1.PodGroupPending)
		oldPG.ResourceVersion = "1"
		newPG := oldPG.DeepCopy()
		newPG.ResourceVersion = "2"
		newPG.Spec.Queue = "namespace/inference"

		controller.updatePodGroup(oldPG, newPG)
		expectWorkQueueKeys(t, controller, []string{"team-a/inference", "team-a/training"})
	})

	t.Run("delete tombstone requeues namespace queue", func(t *testing.T) {
		controller := newFakeNamespaceQueueController(t)
		pg := newPodGroup("team-a", "pg", "namespace/training", schedulingv1beta1.PodGroupCompleted)

		controller.deletePodGroup(cache.DeletedFinalStateUnknown{Key: "team-a/pg", Obj: pg})
		expectWorkQueueKey(t, controller, "team-a/training")
	})
}

func TestReconcileNamespaceQueuePodGroupCounters(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	nq := newNamespaceQueue("team-a", "cluster/research")
	nq.Name = "training"
	parent := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Spec: schedulingv1beta1.QueueSpec{
			AllowedNamespaces: []string{"team-a"},
		},
	}

	if _, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues(nq.Namespace).Create(
		context.Background(), nq.DeepCopy(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create NamespaceQueue: %v", err)
	}
	if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(nq); err != nil {
		t.Fatalf("failed to add NamespaceQueue to indexer: %v", err)
	}
	if err := controller.queueInformer.Informer().GetIndexer().Add(parent); err != nil {
		t.Fatalf("failed to add parent Queue to indexer: %v", err)
	}

	podGroups := []*schedulingv1beta1.PodGroup{
		newPodGroup("team-a", "pending", "namespace/training", schedulingv1beta1.PodGroupPending),
		newPodGroup("team-a", "running", "namespace/training", schedulingv1beta1.PodGroupRunning),
		newPodGroup("team-a", "inqueue", "namespace/training", schedulingv1beta1.PodGroupInqueue),
		newPodGroup("team-a", "completed", "namespace/training", schedulingv1beta1.PodGroupCompleted),
		newPodGroup("team-a", "unknown", "namespace/training", schedulingv1beta1.PodGroupUnknown),
		newPodGroup("team-a", "unset", "namespace/training", ""),
		newPodGroup("team-a", "other-nq", "namespace/inference", schedulingv1beta1.PodGroupRunning),
		newPodGroup("team-a", "cluster", "research", schedulingv1beta1.PodGroupRunning),
	}
	for _, pg := range podGroups {
		if err := controller.podGroupInformer.Informer().GetIndexer().Add(pg); err != nil {
			t.Fatalf("failed to add PodGroup %s to indexer: %v", pg.Name, err)
		}
	}

	if err := controller.syncNamespaceQueue("team-a/training"); err != nil {
		t.Fatalf("syncNamespaceQueue() error = %v", err)
	}
	assertNamespaceQueueCounters(t, controller, "team-a", "training", 2, 1, 1, 1, 1)

	updatedPending := podGroups[0].DeepCopy()
	updatedPending.Status.Phase = schedulingv1beta1.PodGroupCompleted
	if err := controller.podGroupInformer.Informer().GetIndexer().Update(updatedPending); err != nil {
		t.Fatalf("failed to update PodGroup in indexer: %v", err)
	}
	if err := controller.syncNamespaceQueue("team-a/training"); err != nil {
		t.Fatalf("second syncNamespaceQueue() error = %v", err)
	}
	assertNamespaceQueueCounters(t, controller, "team-a", "training", 2, 0, 1, 1, 2)
}

func TestUpdateNamespaceQueueStatusPreservesSchedulerOwnedFields(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	nq := newNamespaceQueue("team-a", "cluster/research")
	nq.Name = "training"
	nq.Status.Allocated = v1.ResourceList{"cpu": resource.MustParse("2")}
	nq.Status.Reservation = schedulingv1beta1.Reservation{
		Nodes:    []string{"node-1"},
		Resource: v1.ResourceList{"memory": resource.MustParse("4Gi")},
	}

	if _, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues(nq.Namespace).Create(
		context.Background(), nq.DeepCopy(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create NamespaceQueue: %v", err)
	}
	if err := controller.queueInformer.Informer().GetIndexer().Add(&schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Spec: schedulingv1beta1.QueueSpec{
			AllowedNamespaces: []string{"team-a"},
		},
	}); err != nil {
		t.Fatalf("failed to add parent Queue: %v", err)
	}
	if err := controller.podGroupInformer.Informer().GetIndexer().Add(
		newPodGroup("team-a", "pending", "namespace/training", schedulingv1beta1.PodGroupPending),
	); err != nil {
		t.Fatalf("failed to add PodGroup: %v", err)
	}

	if err := controller.updateNamespaceQueueStatus(nq); err != nil {
		t.Fatalf("updateNamespaceQueueStatus() error = %v", err)
	}

	updated, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues(nq.Namespace).Get(
		context.Background(), nq.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get NamespaceQueue: %v", err)
	}
	if updated.Status.State != schedulingv1beta1.QueueStateOpen || updated.Status.Pending != 1 {
		t.Fatalf("controller-owned status = %#v, want Open with Pending=1", updated.Status)
	}
	if !updated.Status.Allocated.Cpu().Equal(resource.MustParse("2")) {
		t.Fatalf("allocated CPU = %v, want 2", updated.Status.Allocated.Cpu())
	}
	if len(updated.Status.Reservation.Nodes) != 1 || updated.Status.Reservation.Nodes[0] != "node-1" {
		t.Fatalf("reservation nodes = %v, want [node-1]", updated.Status.Reservation.Nodes)
	}
	if !updated.Status.Reservation.Resource.Memory().Equal(resource.MustParse("4Gi")) {
		t.Fatalf("reservation memory = %v, want 4Gi", updated.Status.Reservation.Resource.Memory())
	}
}

func TestNamespaceQueueLifecycleState(t *testing.T) {
	tests := []struct {
		name            string
		desired         schedulingv1beta1.QueueState
		workloadDrained bool
		runtimeDrained  bool
		want            schedulingv1beta1.QueueState
	}{
		{name: "open", desired: schedulingv1beta1.QueueStateOpen, want: schedulingv1beta1.QueueStateOpen},
		{
			name:            "closed and drained",
			desired:         schedulingv1beta1.QueueStateClosed,
			workloadDrained: true,
			runtimeDrained:  true,
			want:            schedulingv1beta1.QueueStateClosed,
		},
		{
			name:            "closed with pending workload",
			desired:         schedulingv1beta1.QueueStateClosed,
			workloadDrained: false,
			runtimeDrained:  true,
			want:            schedulingv1beta1.QueueStateClosing,
		},
		{
			name:            "closed with runtime resource",
			desired:         schedulingv1beta1.QueueStateClosed,
			workloadDrained: true,
			runtimeDrained:  false,
			want:            schedulingv1beta1.QueueStateClosing,
		},
		{name: "invalid desired state", desired: schedulingv1beta1.QueueStateClosing, want: schedulingv1beta1.QueueStateUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := controllerutil.ResolveNamespaceQueueLifecycleState(
				tt.desired,
				tt.workloadDrained,
				tt.runtimeDrained,
			)
			if got != tt.want {
				t.Fatalf("namespaceQueueLifecycleState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpdateNamespaceQueueStatusUsesLatestRuntimeState(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	current := newNamespaceQueue("team-a", "cluster/research")
	current.Name = "training"
	current.Spec.State = schedulingv1beta1.QueueStateClosed
	current.Status.Allocated = v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}
	if _, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues(current.Namespace).Create(
		context.Background(), current, metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create NamespaceQueue: %v", err)
	}
	if err := controller.queueInformer.Informer().GetIndexer().Add(&schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Spec: schedulingv1beta1.QueueSpec{
			AllowedNamespaces: []string{"team-a"},
		},
	}); err != nil {
		t.Fatalf("failed to add parent Queue: %v", err)
	}

	stale := current.DeepCopy()
	stale.Status.Allocated = nil
	if err := controller.updateNamespaceQueueStatus(stale); err != nil {
		t.Fatalf("updateNamespaceQueueStatus() error = %v", err)
	}

	updated, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues(current.Namespace).Get(
		context.Background(), current.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get NamespaceQueue: %v", err)
	}
	if updated.Status.State != schedulingv1beta1.QueueStateClosing {
		t.Fatalf("status state = %q, want Closing", updated.Status.State)
	}
	ready := apiMeta.FindStatusCondition(updated.Status.Conditions, "Ready")
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != "QueueClosing" {
		t.Fatalf("Ready condition = %#v, want False/QueueClosing", ready)
	}
}

func TestNamespaceQueueRuntimeStatusUpdateRequeuesClosingQueue(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	oldNQ := newNamespaceQueue("team-a", "cluster/research")
	oldNQ.Name = "training"
	oldNQ.Spec.State = schedulingv1beta1.QueueStateClosed
	oldNQ.ResourceVersion = "1"
	oldNQ.Status.Allocated = v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}

	newNQ := oldNQ.DeepCopy()
	newNQ.ResourceVersion = "2"
	newNQ.Status.Allocated = v1.ResourceList{v1.ResourceCPU: resource.MustParse("0")}

	controller.updateNamespaceQueue(oldNQ, newNQ)
	expectWorkQueueKey(t, controller, "team-a/training")
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

func TestProcessNextWorkItemRetriesErrors(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	controller.syncHandler = func(string) error { return errors.New("temporary failure") }
	const key = "team-a/training"
	controller.workQueue.Add(key)

	if !controller.processNextWorkItem() {
		t.Fatal("processNextWorkItem() returned false")
	}
	if got := controller.workQueue.NumRequeues(key); got != 1 {
		t.Fatalf("NumRequeues() = %d, want 1", got)
	}
}

func TestNamespaceQueueResourceConstraintSetsNotReady(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	parent := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Spec: schedulingv1beta1.QueueSpec{
			AllowedNamespaces: []string{"team-a"},
			Capability:        v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")},
		},
	}
	queue := newNamespaceQueue("team-a", "cluster/research")
	queue.Name = "training"
	queue.Spec.Capability = v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")}
	addNamespaceQueueTestObjects(t, controller, queue, parent)

	if err := controller.syncNamespaceQueue("team-a/training"); err != nil {
		t.Fatalf("syncNamespaceQueue() error = %v", err)
	}
	assertReadyReason(t, controller, "team-a", "training", "ParentConstraintViolation")
}

func TestDuplicateClusterAttachmentSetsNotReady(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	parent := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Spec:       schedulingv1beta1.QueueSpec{AllowedNamespaces: []string{"team-a"}},
	}
	queue := newNamespaceQueue("team-a", "cluster/research")
	queue.Name = "training"
	duplicate := newNamespaceQueue("team-a", "cluster/research")
	duplicate.Name = "inference"
	addNamespaceQueueTestObjects(t, controller, queue, parent)
	if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(duplicate); err != nil {
		t.Fatalf("failed to add duplicate NamespaceQueue: %v", err)
	}

	if err := controller.syncNamespaceQueue("team-a/training"); err != nil {
		t.Fatalf("syncNamespaceQueue() error = %v", err)
	}
	assertReadyReason(t, controller, "team-a", "training", "DuplicateClusterAttachment")
}

func TestNamespaceQueueDepthLimitSetsNotReady(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	parentQueue := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Spec:       schedulingv1beta1.QueueSpec{AllowedNamespaces: []string{"team-a"}},
	}
	if err := controller.queueInformer.Informer().GetIndexer().Add(parentQueue); err != nil {
		t.Fatalf("failed to add Queue: %v", err)
	}

	parentName := ""
	for i := 1; i <= 6; i++ {
		parent := "cluster/research"
		if parentName != "" {
			parent = parentName
		}
		queue := newNamespaceQueue("team-a", parent)
		queue.Name = fmt.Sprintf("level-%d", i)
		queue.Status.State = schedulingv1beta1.QueueStateOpen
		queue.Status.Conditions = []metav1.Condition{
			{Type: "Authorized", Status: metav1.ConditionTrue},
			{Type: "Ready", Status: metav1.ConditionTrue},
		}
		if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(queue); err != nil {
			t.Fatalf("failed to add NamespaceQueue: %v", err)
		}
		parentName = queue.Name
		if i == 6 {
			if _, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues(queue.Namespace).
				Create(context.Background(), queue.DeepCopy(), metav1.CreateOptions{}); err != nil {
				t.Fatalf("failed to create NamespaceQueue: %v", err)
			}
		}
	}

	if err := controller.syncNamespaceQueue("team-a/level-6"); err != nil {
		t.Fatalf("syncNamespaceQueue() error = %v", err)
	}
	assertReadyReason(t, controller, "team-a", "level-6", "HierarchyDepthExceeded")
}

func TestNamespaceQueueFinalizerRemovalAfterDrain(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	now := metav1.Now()
	queue := newNamespaceQueue("team-a", "cluster/research")
	queue.Name = "training"
	queue.Spec.State = schedulingv1beta1.QueueStateClosed
	queue.DeletionTimestamp = &now
	queue.Finalizers = []string{namespaceQueueFinalizer}
	parent := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Spec:       schedulingv1beta1.QueueSpec{AllowedNamespaces: []string{"team-a"}},
	}
	addNamespaceQueueTestObjects(t, controller, queue, parent)

	if err := controller.syncNamespaceQueue("team-a/training"); err != nil {
		t.Fatalf("syncNamespaceQueue() error = %v", err)
	}
	updated, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues("team-a").
		Get(context.Background(), "training", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return
	}
	if err != nil {
		t.Fatalf("failed to get NamespaceQueue: %v", err)
	}
	if hasFinalizer(updated, namespaceQueueFinalizer) {
		t.Fatal("NamespaceQueue finalizer was not removed after drain")
	}
}

func TestNamespaceQueueFinalizerBlocksDeletionWithChild(t *testing.T) {
	controller := newFakeNamespaceQueueController(t)
	now := metav1.Now()
	queue := newNamespaceQueue("team-a", "cluster/research")
	queue.Name = "department"
	queue.Spec.State = schedulingv1beta1.QueueStateClosed
	queue.DeletionTimestamp = &now
	queue.Finalizers = []string{namespaceQueueFinalizer}
	child := newNamespaceQueue("team-a", "department")
	child.Name = "training"
	parent := &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Spec:       schedulingv1beta1.QueueSpec{AllowedNamespaces: []string{"team-a"}},
	}
	addNamespaceQueueTestObjects(t, controller, queue, parent)
	if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(child); err != nil {
		t.Fatalf("failed to add child NamespaceQueue: %v", err)
	}

	if err := controller.syncNamespaceQueue("team-a/department"); err != nil {
		t.Fatalf("syncNamespaceQueue() error = %v", err)
	}
	updated, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues("team-a").
		Get(context.Background(), "department", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get NamespaceQueue: %v", err)
	}
	if !hasFinalizer(updated, namespaceQueueFinalizer) {
		t.Fatal("NamespaceQueue finalizer was removed while a child exists")
	}
}

func TestSetNamespaceQueueConditionsPreservesTransitionTime(t *testing.T) {
	transitionTime := metav1.NewTime(time.Unix(1, 0))
	status := schedulingv1beta1.NamespaceQueueStatus{
		State: schedulingv1beta1.QueueStateOpen,
		Conditions: []metav1.Condition{
			{
				Type:               "Authorized",
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				Reason:             "NamespaceAllowed",
				Message:            "allowed",
				LastTransitionTime: transitionTime,
			},
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 1,
				Reason:             "Ready",
				Message:            "ready",
				LastTransitionTime: transitionTime,
			},
		},
	}
	setNamespaceQueueConditions(&status, 1, namespaceQueueConditionResult{
		authorizedStatus:  metav1.ConditionTrue,
		authorizedReason:  "NamespaceAllowed",
		authorizedMessage: "allowed",
		readyStatus:       metav1.ConditionTrue,
		readyReason:       "Ready",
		readyMessage:      "ready",
	})

	for _, condition := range status.Conditions {
		if !condition.LastTransitionTime.Equal(&transitionTime) {
			t.Fatalf("condition %q transition time changed: %v", condition.Type, condition.LastTransitionTime)
		}
	}
}

func addNamespaceQueueTestObjects(
	t *testing.T,
	controller *namespaceQueueController,
	queue *schedulingv1beta1.NamespaceQueue,
	parent *schedulingv1beta1.Queue,
) {
	t.Helper()
	if err := controller.queueInformer.Informer().GetIndexer().Add(parent); err != nil {
		t.Fatalf("failed to add Queue: %v", err)
	}
	if err := controller.namespaceQueueInformer.Informer().GetIndexer().Add(queue); err != nil {
		t.Fatalf("failed to add NamespaceQueue: %v", err)
	}
	if _, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues(queue.Namespace).
		Create(context.Background(), queue.DeepCopy(), metav1.CreateOptions{}); err != nil {
		t.Fatalf("failed to create NamespaceQueue: %v", err)
	}
}

func assertReadyReason(
	t *testing.T,
	controller *namespaceQueueController,
	namespace, name, reason string,
) {
	t.Helper()
	queue, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues(namespace).
		Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get NamespaceQueue: %v", err)
	}
	condition := apiMeta.FindStatusCondition(queue.Status.Conditions, "Ready")
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != reason {
		t.Fatalf("Ready condition = %#v, want False/%s", condition, reason)
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

func expectWorkQueueKeys(t *testing.T, controller *namespaceQueueController, want []string) {
	t.Helper()
	if controller.workQueue.Len() != len(want) {
		t.Fatalf("workQueue length = %d, want %d", controller.workQueue.Len(), len(want))
	}

	got := make([]string, 0, len(want))
	for range want {
		key, shutdown := controller.workQueue.Get()
		if shutdown {
			t.Fatal("workQueue unexpectedly shut down")
		}
		controller.workQueue.Done(key)
		controller.workQueue.Forget(key)
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("workQueue keys = %v, want %v", got, want)
		}
	}
}

func newPodGroup(
	namespace, name, queue string,
	phase schedulingv1beta1.PodGroupPhase,
) *schedulingv1beta1.PodGroup {
	return &schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       schedulingv1beta1.PodGroupSpec{Queue: queue},
		Status:     schedulingv1beta1.PodGroupStatus{Phase: phase},
	}
}

func assertNamespaceQueueCounters(
	t *testing.T,
	controller *namespaceQueueController,
	namespace, name string,
	unknown, pending, running, inqueue, completed int32,
) {
	t.Helper()
	nq, err := controller.vcClient.SchedulingV1beta1().NamespaceQueues(namespace).Get(
		context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get NamespaceQueue: %v", err)
	}
	if nq.Status.Unknown != unknown || nq.Status.Pending != pending ||
		nq.Status.Running != running || nq.Status.Inqueue != inqueue ||
		nq.Status.Completed != completed {
		t.Fatalf(
			"NamespaceQueue counters = unknown:%d pending:%d running:%d inqueue:%d completed:%d, want unknown:%d pending:%d running:%d inqueue:%d completed:%d",
			nq.Status.Unknown, nq.Status.Pending, nq.Status.Running, nq.Status.Inqueue, nq.Status.Completed,
			unknown, pending, running, inqueue, completed,
		)
	}
}
