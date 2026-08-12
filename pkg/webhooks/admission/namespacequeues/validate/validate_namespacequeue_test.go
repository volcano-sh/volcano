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

package validate

import (
	"encoding/json"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/tools/cache"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	schedulinglister "volcano.sh/apis/pkg/client/listers/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
	commonutil "volcano.sh/volcano/pkg/util"
)

func TestValidateNamespaceQueueParent(t *testing.T) {
	tests := []struct {
		name            string
		namespaceQueue  *schedulingv1beta1.NamespaceQueue
		queues          []*schedulingv1beta1.Queue
		expectedMessage string
	}{
		{
			name: "authorized cluster parent",
			namespaceQueue: newNamespaceQueue(
				"team-a",
				"training",
				"cluster/research",
			),
			queues: []*schedulingv1beta1.Queue{
				newQueue("research", "team-a", "team-b"),
			},
		},
		{
			name: "wildcard authorizes cluster parent",
			namespaceQueue: newNamespaceQueue(
				"team-a",
				"training",
				"cluster/research",
			),
			queues: []*schedulingv1beta1.Queue{
				newQueue("research", "*"),
			},
		},
		{
			name: "empty parent resolves to authorized default queue",
			namespaceQueue: newNamespaceQueue(
				"team-a",
				"training",
				"",
			),
			queues: []*schedulingv1beta1.Queue{
				newQueue("default", "team-a"),
			},
		},
		{
			name: "local parent only requires a valid reference",
			namespaceQueue: newNamespaceQueue(
				"team-a",
				"training",
				"department",
			),
		},
		{
			name: "cluster parent does not exist",
			namespaceQueue: newNamespaceQueue(
				"team-a",
				"training",
				"cluster/research",
			),
			expectedMessage: "unable to find parent Queue \"research\"",
		},
		{
			name: "namespace is not authorized",
			namespaceQueue: newNamespaceQueue(
				"team-a",
				"training",
				"cluster/research",
			),
			queues: []*schedulingv1beta1.Queue{
				newQueue("research", "team-b"),
			},
			expectedMessage: "namespace \"team-a\" is not allowed to use parent Queue \"research\"",
		},
		{
			name: "empty authorization list denies attachment",
			namespaceQueue: newNamespaceQueue(
				"team-a",
				"training",
				"cluster/research",
			),
			queues: []*schedulingv1beta1.Queue{
				newQueue("research"),
			},
			expectedMessage: "namespace \"team-a\" is not allowed",
		},
		{
			name: "cluster root is forbidden",
			namespaceQueue: newNamespaceQueue(
				"team-a",
				"training",
				"cluster/root",
			),
			expectedMessage: "cannot be used as a NamespaceQueue parent",
		},
		{
			name: "empty cluster parent name is invalid",
			namespaceQueue: newNamespaceQueue(
				"team-a",
				"training",
				"cluster/",
			),
			expectedMessage: "invalid cluster parent",
		},
		{
			name: "nested local parent is invalid",
			namespaceQueue: newNamespaceQueue(
				"team-a",
				"training",
				"department/training",
			),
			expectedMessage: "invalid parent name",
		},
		{
			name:            "nil NamespaceQueue is invalid",
			expectedMessage: "NamespaceQueue is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			for _, queue := range tt.queues {
				if err := indexer.Add(queue); err != nil {
					t.Fatalf("failed to add Queue to indexer: %v", err)
				}
			}
			config.QueueLister = schedulinglister.NewQueueLister(indexer)

			err := validateNamespaceQueueParent(tt.namespaceQueue)
			if tt.expectedMessage == "" {
				if err != nil {
					t.Fatalf("expected validation to succeed, got %v", err)
				}
				return
			}

			if err == nil || !strings.Contains(err.Error(), tt.expectedMessage) {
				t.Fatalf("expected error containing %q, got %v", tt.expectedMessage, err)
			}
		})
	}
}

func TestValidateNamespaceQueueDeleting(t *testing.T) {
	tests := []struct {
		name            string
		namespaceQueue  *schedulingv1beta1.NamespaceQueue
		children        []*schedulingv1beta1.NamespaceQueue
		expectedMessage string
	}{
		{
			name:           "closed and drained namespace queue",
			namespaceQueue: closedNamespaceQueue("team-a", "department"),
		},
		{
			name:            "open namespace queue",
			namespaceQueue:  newNamespaceQueue("team-a", "department", "cluster/research"),
			expectedMessage: "must be closed and drained",
		},
		{
			name: "namespace queue with pending workloads",
			namespaceQueue: func() *schedulingv1beta1.NamespaceQueue {
				namespaceQueue := closedNamespaceQueue("team-a", "department")
				namespaceQueue.Status.Pending = 1
				return namespaceQueue
			}(),
			expectedMessage: "must be closed and drained",
		},
		{
			name: "namespace queue with allocated resources",
			namespaceQueue: func() *schedulingv1beta1.NamespaceQueue {
				namespaceQueue := closedNamespaceQueue("team-a", "department")
				namespaceQueue.Status.Allocated = v1.ResourceList{
					v1.ResourceCPU: resource.MustParse("1"),
				}
				return namespaceQueue
			}(),
			expectedMessage: "must be closed and drained",
		},
		{
			name: "namespace queue with reserved resources",
			namespaceQueue: func() *schedulingv1beta1.NamespaceQueue {
				namespaceQueue := closedNamespaceQueue("team-a", "department")
				namespaceQueue.Status.Reservation.Resource = v1.ResourceList{
					v1.ResourceMemory: resource.MustParse("1Gi"),
				}
				return namespaceQueue
			}(),
			expectedMessage: "must be closed and drained",
		},
		{
			name:            "namespace queue with reserved nodes",
			namespaceQueue:  closedNamespaceQueue("team-a", "department"),
			expectedMessage: "must be closed and drained",
		},
		{
			name:           "namespace queue with child",
			namespaceQueue: closedNamespaceQueue("team-a", "department"),
			children: []*schedulingv1beta1.NamespaceQueue{
				newNamespaceQueue("team-a", "training", "department"),
			},
			expectedMessage: "child NamespaceQueues: training",
		},
		{
			name:           "same parent name in another namespace is ignored",
			namespaceQueue: closedNamespaceQueue("team-a", "department"),
			children: []*schedulingv1beta1.NamespaceQueue{
				newNamespaceQueue("team-b", "training", "department"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "namespace queue with reserved nodes" {
				tt.namespaceQueue.Status.Reservation.Nodes = []string{"node-a"}
			}

			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
				cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
			})
			for _, child := range tt.children {
				if err := indexer.Add(child); err != nil {
					t.Fatalf("failed to add child NamespaceQueue to indexer: %v", err)
				}
			}
			config.NamespaceQueueLister = schedulinglister.NewNamespaceQueueLister(indexer)

			err := validateNamespaceQueueDeleting(tt.namespaceQueue)
			if tt.expectedMessage == "" {
				if err != nil {
					t.Fatalf("expected deletion to be allowed, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.expectedMessage) {
				t.Fatalf("expected error containing %q, got %v", tt.expectedMessage, err)
			}
		})
	}
}

func TestAdmitNamespaceQueuesDelete(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)
	namespaceQueue := closedNamespaceQueue("team-a", "training")
	namespaceQueue.TypeMeta = metav1.TypeMeta{
		APIVersion: schedulingv1beta1.SchemeGroupVersion.String(),
		Kind:       "NamespaceQueue",
	}
	namespaceQueueJSON, err := json.Marshal(namespaceQueue)
	if err != nil {
		t.Fatalf("failed to marshal NamespaceQueue: %v", err)
	}

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	config.NamespaceQueueLister = schedulinglister.NewNamespaceQueueLister(indexer)

	response := AdmitNamespaceQueues(admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			Resource: metav1.GroupVersionResource{
				Group:    schedulingv1beta1.SchemeGroupVersion.Group,
				Version:  schedulingv1beta1.SchemeGroupVersion.Version,
				Resource: "namespacequeues",
			},
			Namespace: namespaceQueue.Namespace,
			Name:      namespaceQueue.Name,
			Operation: admissionv1.Delete,
			OldObject: runtime.RawExtension{Raw: namespaceQueueJSON},
		},
	})

	if !response.Allowed {
		t.Fatalf("expected DELETE admission to be allowed, got %#v", response.Result)
	}
}

func TestAdmitNamespaceQueuesRejectsWhenFeatureDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, false)
	response := AdmitNamespaceQueues(admissionv1.AdmissionReview{})
	if response.Allowed || response.Result == nil || !strings.Contains(response.Result.Message, "feature is disabled") {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestAdmitNamespaceQueuesAllowsDeleteWhenFeatureDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, false)
	response := AdmitNamespaceQueues(admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{Operation: admissionv1.Delete},
	})
	if !response.Allowed {
		t.Fatalf("expected DELETE to be allowed while feature is disabled, got %#v", response.Result)
	}
}

func TestAdmitNamespaceQueuesParentChangeRequiresDrain(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)
	tests := []struct {
		name    string
		old     *schedulingv1beta1.NamespaceQueue
		current *schedulingv1beta1.NamespaceQueue
		wantErr bool
	}{
		{
			name:    "active parent change is rejected",
			old:     newNamespaceQueue("team-a", "training", "cluster/research"),
			current: newNamespaceQueue("team-a", "training", "cluster/production"),
			wantErr: true,
		},
		{
			name:    "closed and drained parent change is allowed",
			old:     closedNamespaceQueue("team-a", "training"),
			current: newNamespaceQueue("team-a", "training", "cluster/production"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := commonutil.ValidateNamespaceQueueParentChange(tt.old, tt.current); (err != nil) != tt.wantErr {
				t.Fatalf("ValidateNamespaceQueueParentChange() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

func TestValidateNamespaceQueueHierarchy(t *testing.T) {
	tests := []struct {
		name            string
		queue           *schedulingv1beta1.NamespaceQueue
		existing        []*schedulingv1beta1.NamespaceQueue
		maxDepth        int
		expectedMessage string
	}{
		{
			name:  "duplicate cluster attachment",
			queue: newNamespaceQueue("team-a", "training", "cluster/research"),
			existing: []*schedulingv1beta1.NamespaceQueue{
				newNamespaceQueue("team-a", "inference", "cluster/research"),
			},
			expectedMessage: "already attaches",
		},
		{
			name:  "cycle",
			queue: newNamespaceQueue("team-a", "a", "b"),
			existing: []*schedulingv1beta1.NamespaceQueue{
				newNamespaceQueue("team-a", "b", "a"),
			},
			expectedMessage: "contains a cycle",
		},
		{
			name:     "depth exceeded",
			queue:    newNamespaceQueue("team-a", "level-4", "level-3"),
			maxDepth: 3,
			existing: []*schedulingv1beta1.NamespaceQueue{
				newNamespaceQueue("team-a", "level-1", "cluster/research"),
				newNamespaceQueue("team-a", "level-2", "level-1"),
				newNamespaceQueue("team-a", "level-3", "level-2"),
			},
			expectedMessage: "exceeds maximum depth",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldMaxDepth := config.MaxNamespaceQueueDepth
			defer func() { config.MaxNamespaceQueueDepth = oldMaxDepth }()
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
				cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
			})
			for _, queue := range tt.existing {
				if err := indexer.Add(queue); err != nil {
					t.Fatalf("failed to add NamespaceQueue: %v", err)
				}
			}
			config.NamespaceQueueLister = schedulinglister.NewNamespaceQueueLister(indexer)
			config.MaxNamespaceQueueDepth = tt.maxDepth

			err := validateNamespaceQueueHierarchy(tt.queue)
			if err == nil || !strings.Contains(err.Error(), tt.expectedMessage) {
				t.Fatalf("expected error containing %q, got %v", tt.expectedMessage, err)
			}
		})
	}
}

func TestValidateNamespaceQueueHierarchyAtDepthLimit(t *testing.T) {
	oldMaxDepth := config.MaxNamespaceQueueDepth
	defer func() { config.MaxNamespaceQueueDepth = oldMaxDepth }()

	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	for _, queue := range []*schedulingv1beta1.NamespaceQueue{
		newNamespaceQueue("team-a", "level-1", "cluster/research"),
		newNamespaceQueue("team-a", "level-2", "level-1"),
	} {
		if err := indexer.Add(queue); err != nil {
			t.Fatalf("failed to add NamespaceQueue: %v", err)
		}
	}
	config.NamespaceQueueLister = schedulinglister.NewNamespaceQueueLister(indexer)
	config.MaxNamespaceQueueDepth = 3

	if err := validateNamespaceQueueHierarchy(
		newNamespaceQueue("team-a", "level-3", "level-2"),
	); err != nil {
		t.Fatalf("validateNamespaceQueueHierarchy() error = %v", err)
	}
}

func TestAdmitNamespaceQueuesRejectsInvalidResources(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)
	queue := newNamespaceQueue("team-a", "training", "cluster/research")
	queue.TypeMeta = metav1.TypeMeta{
		APIVersion: schedulingv1beta1.SchemeGroupVersion.String(),
		Kind:       "NamespaceQueue",
	}
	queue.Spec.Capability = v1.ResourceList{v1.ResourceCPU: resource.MustParse("-1")}
	raw, err := json.Marshal(queue)
	if err != nil {
		t.Fatalf("failed to marshal NamespaceQueue: %v", err)
	}

	response := AdmitNamespaceQueues(admissionv1.AdmissionReview{
		Request: &admissionv1.AdmissionRequest{
			Resource: metav1.GroupVersionResource{
				Group: schedulingv1beta1.SchemeGroupVersion.Group, Version: schedulingv1beta1.SchemeGroupVersion.Version, Resource: "namespacequeues",
			},
			Namespace: queue.Namespace,
			Name:      queue.Name,
			Operation: admissionv1.Create,
			Object:    runtime.RawExtension{Raw: raw},
		},
	})
	if response.Allowed || response.Result == nil || !strings.Contains(response.Result.Message, "must be non-negative") {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func closedNamespaceQueue(namespace, name string) *schedulingv1beta1.NamespaceQueue {
	namespaceQueue := newNamespaceQueue(namespace, name, "cluster/research")
	namespaceQueue.Status.State = schedulingv1beta1.QueueStateClosed
	return namespaceQueue
}

func newNamespaceQueue(namespace, name, parent string) *schedulingv1beta1.NamespaceQueue {
	return &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: schedulingv1beta1.NamespaceQueueSpec{Parent: parent},
	}
}

func newQueue(name string, allowedNamespaces ...string) *schedulingv1beta1.Queue {
	return &schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: schedulingv1beta1.QueueSpec{
			AllowedNamespaces: allowedNamespaces,
		},
	}
}
