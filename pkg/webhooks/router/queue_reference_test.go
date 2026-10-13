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

package router

import (
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/tools/cache"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	schedulinglister "volcano.sh/apis/pkg/client/listers/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
	commonutil "volcano.sh/volcano/pkg/util"
	webhookutil "volcano.sh/volcano/pkg/webhooks/util"
)

func TestResolveQueueReference(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)
	tests := []struct {
		name              string
		workloadNamespace string
		reference         string
		defaultQueue      string
		want              ResolvedQueueReference
		wantErr           bool
	}{
		{
			name:         "empty reference uses default cluster queue",
			defaultQueue: "default",
			want: ResolvedQueueReference{
				Scope: ClusterQueueReferenceScope,
				Name:  "default",
			},
		},
		{
			name:      "plain name selects cluster queue",
			reference: "research",
			want: ResolvedQueueReference{
				Scope: ClusterQueueReferenceScope,
				Name:  "research",
			},
		},
		{
			name:              "namespace reference selects local namespace queue",
			workloadNamespace: "team-a",
			reference:         "namespace/training",
			want: ResolvedQueueReference{
				Scope:     NamespaceQueueReferenceScope,
				Namespace: "team-a",
				Name:      "training",
			},
		},
		{
			name:    "empty reference requires default queue",
			wantErr: true,
		},
		{
			name:              "cluster prefix is invalid for workload reference",
			workloadNamespace: "team-a",
			reference:         "cluster/research",
			wantErr:           true,
		},
		{
			name:              "namespace queue name is required",
			workloadNamespace: "team-a",
			reference:         "namespace/",
			wantErr:           true,
		},
		{
			name:              "cross namespace reference is invalid",
			workloadNamespace: "team-a",
			reference:         "team-b/training",
			wantErr:           true,
		},
		{
			name:      "namespace queue requires workload namespace",
			reference: "namespace/training",
			wantErr:   true,
		},
		{
			name:              "nested namespace queue reference is invalid",
			workloadNamespace: "team-a",
			reference:         "namespace/department/training",
			wantErr:           true,
		},
		{
			name:              "namespace queue name must be DNS compatible",
			workloadNamespace: "team-a",
			reference:         "namespace/Bad_Name",
			wantErr:           true,
		},
		{
			name:         "cluster queue name must be DNS compatible",
			reference:    "Bad_Name",
			defaultQueue: "default",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := webhookutil.ResolveQueueReference(tt.workloadNamespace, tt.reference, tt.defaultQueue)
			if (err != nil) != tt.wantErr {
				t.Fatalf("webhookutil.ResolveQueueReference() error = %v, wantErr = %t", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Fatalf("webhookutil.ResolveQueueReference() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestResolveQueueReferenceRejectsNamespaceQueueWhenDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, false)
	if _, err := webhookutil.ResolveQueueReference("team-a", "namespace/training", "default"); err == nil {
		t.Fatal("webhookutil.ResolveQueueReference() accepted NamespaceQueue while feature gate is disabled")
	}
}

func TestValidateWorkloadQueueReferenceRejectsCurrentNamespaceQueueState(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)

	queueIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	namespaceQueueIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	config := &AdmissionServiceConfig{
		QueueLister:          schedulinglister.NewQueueLister(queueIndexer),
		NamespaceQueueLister: schedulinglister.NewNamespaceQueueLister(namespaceQueueIndexer),
	}
	if err := namespaceQueueIndexer.Add(&schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "training",
			Namespace:  "team-a",
			Generation: 1,
		},
		Status: schedulingv1beta1.NamespaceQueueStatus{
			State: schedulingv1beta1.QueueStateOpen,
			Conditions: []metav1.Condition{
				{Type: commonutil.NamespaceQueueAuthorizedCondition, Status: metav1.ConditionFalse, ObservedGeneration: 1},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	err := webhookutil.ValidateWorkloadQueueReference(
		"team-a",
		"namespace/training",
		"default",
		config,
		webhookutil.QueueReferenceValidationOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("expected current authorization failure, got %v", err)
	}
}

func TestValidateWorkloadQueueReferenceReportsReadinessBeforeAuthorization(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)

	queueIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	namespaceQueueIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	if err := namespaceQueueIndexer.Add(&schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Name: "training", Namespace: "team-a", Generation: 1},
		Status: schedulingv1beta1.NamespaceQueueStatus{
			State: schedulingv1beta1.QueueStateOpen,
			Conditions: []metav1.Condition{
				{Type: commonutil.NamespaceQueueAuthorizedCondition, Status: metav1.ConditionUnknown, ObservedGeneration: 1},
				{Type: commonutil.NamespaceQueueReadyCondition, Status: metav1.ConditionFalse, ObservedGeneration: 1, Message: "parent is missing"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	config := &AdmissionServiceConfig{
		QueueLister:          schedulinglister.NewQueueLister(queueIndexer),
		NamespaceQueueLister: schedulinglister.NewNamespaceQueueLister(namespaceQueueIndexer),
	}
	err := webhookutil.ValidateWorkloadQueueReference(
		"team-a",
		"namespace/training",
		"default",
		config,
		webhookutil.QueueReferenceValidationOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "is not ready") {
		t.Fatalf("expected readiness failure, got %v", err)
	}
}

func TestValidateWorkloadQueueReferenceRejectsNonLeafQueues(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)

	queueIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if err := queueIndexer.Add(&schedulingv1beta1.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "research"},
		Status:     schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateOpen},
	}); err != nil {
		t.Fatal(err)
	}
	namespaceQueueIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
		cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
	})
	if err := namespaceQueueIndexer.Add(&schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Name: "namespace-child", Namespace: "team-a"},
		Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "cluster/research"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := namespaceQueueIndexer.Add(&schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "parent",
			Namespace:  "team-a",
			Generation: 1,
		},
		Status: schedulingv1beta1.NamespaceQueueStatus{
			State: schedulingv1beta1.QueueStateOpen,
			Conditions: []metav1.Condition{
				{Type: commonutil.NamespaceQueueAuthorizedCondition, Status: metav1.ConditionTrue, ObservedGeneration: 1},
				{Type: commonutil.NamespaceQueueReadyCondition, Status: metav1.ConditionTrue, ObservedGeneration: 1},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := namespaceQueueIndexer.Add(&schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Name: "local-child", Namespace: "team-a"},
		Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "parent"},
	}); err != nil {
		t.Fatal(err)
	}

	config := &AdmissionServiceConfig{
		QueueLister:          schedulinglister.NewQueueLister(queueIndexer),
		NamespaceQueueLister: schedulinglister.NewNamespaceQueueLister(namespaceQueueIndexer),
	}
	options := webhookutil.QueueReferenceValidationOptions{RequireClusterQueueLeaf: true}

	if err := webhookutil.ValidateWorkloadQueueReference("team-a", "research", "default", config, options); err == nil || !strings.Contains(err.Error(), "NamespaceQueue children") {
		t.Fatalf("expected cluster Queue with NamespaceQueue child to be rejected, got %v", err)
	}
	if err := webhookutil.ValidateWorkloadQueueReference("team-a", "namespace/parent", "default", config, options); err == nil || !strings.Contains(err.Error(), "leaf NamespaceQueue") {
		t.Fatalf("expected parent NamespaceQueue to be rejected, got %v", err)
	}
}

func TestValidateWorkloadQueueReferencePreservesClusterQueueLeafPolicy(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)
	queueIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, queue := range []*schedulingv1beta1.Queue{
		{ObjectMeta: metav1.ObjectMeta{Name: "research"}, Status: schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateOpen}},
		{ObjectMeta: metav1.ObjectMeta{Name: "subqueue"}, Spec: schedulingv1beta1.QueueSpec{Parent: "research"}},
	} {
		if err := queueIndexer.Add(queue); err != nil {
			t.Fatal(err)
		}
	}
	config := &AdmissionServiceConfig{
		QueueLister: schedulinglister.NewQueueLister(queueIndexer),
		NamespaceQueueLister: schedulinglister.NewNamespaceQueueLister(cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
			cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
		})),
	}
	if err := webhookutil.ValidateWorkloadQueueReference("team-a", "research", "default", config, webhookutil.QueueReferenceValidationOptions{}); err != nil {
		t.Fatalf("PodGroup's existing cluster Queue policy was changed: %v", err)
	}
	if err := webhookutil.ValidateWorkloadQueueReference("team-a", "research", "default", config, webhookutil.QueueReferenceValidationOptions{RequireClusterQueueLeaf: true}); err == nil {
		t.Fatal("Job was admitted to a cluster Queue with children")
	}
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	if err := indexer.Add(&schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-b", Name: "child"},
		Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "cluster/research"},
	}); err != nil {
		t.Fatal(err)
	}
	config.NamespaceQueueLister = schedulinglister.NewNamespaceQueueLister(indexer)
	if err := webhookutil.ValidateWorkloadQueueReference("team-a", "research", "default", config, webhookutil.QueueReferenceValidationOptions{}); err == nil || !strings.Contains(err.Error(), "NamespaceQueue children") {
		t.Fatalf("expected NSQ child to make cluster Queue non-leaf, got %v", err)
	}
}

func TestValidateWorkloadQueueReferenceClusterOnlyWithoutNamespaceQueueLister(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, false)
	queueIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	for _, queue := range []*schedulingv1beta1.Queue{
		{ObjectMeta: metav1.ObjectMeta{Name: "research"}, Status: schedulingv1beta1.QueueStatus{State: schedulingv1beta1.QueueStateOpen}},
		{ObjectMeta: metav1.ObjectMeta{Name: "child"}, Spec: schedulingv1beta1.QueueSpec{Parent: "research"}},
	} {
		if err := queueIndexer.Add(queue); err != nil {
			t.Fatal(err)
		}
	}
	config := &AdmissionServiceConfig{QueueLister: schedulinglister.NewQueueLister(queueIndexer)}
	if err := webhookutil.ValidateWorkloadQueueReference("team-a", "research", "default", config, webhookutil.QueueReferenceValidationOptions{}); err != nil {
		t.Fatalf("PodGroup cluster Queue validation rejected an existing non-leaf queue: %v", err)
	}
	if err := webhookutil.ValidateWorkloadQueueReference("team-a", "research", "default", config, webhookutil.QueueReferenceValidationOptions{RequireClusterQueueLeaf: true}); err == nil {
		t.Fatal("Job cluster Queue validation accepted a non-leaf queue")
	}
}

func TestValidateWorkloadQueueReferenceRejectsNamespaceQueueParentForPodGroup(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	for _, queue := range []*schedulingv1beta1.NamespaceQueue{
		{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "parent", Generation: 1}, Status: schedulingv1beta1.NamespaceQueueStatus{
			State: schedulingv1beta1.QueueStateOpen, Conditions: []metav1.Condition{
				{Type: commonutil.NamespaceQueueAuthorizedCondition, Status: metav1.ConditionTrue, ObservedGeneration: 1},
				{Type: commonutil.NamespaceQueueReadyCondition, Status: metav1.ConditionTrue, ObservedGeneration: 1},
			},
		}},
		{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "child"}, Spec: schedulingv1beta1.NamespaceQueueSpec{Parent: "parent"}},
	} {
		if err := indexer.Add(queue); err != nil {
			t.Fatal(err)
		}
	}
	config := &AdmissionServiceConfig{NamespaceQueueLister: schedulinglister.NewNamespaceQueueLister(indexer)}
	if err := webhookutil.ValidateWorkloadQueueReference("team-a", "namespace/parent", "default", config, webhookutil.QueueReferenceValidationOptions{}); err == nil || !strings.Contains(err.Error(), "leaf NamespaceQueue") {
		t.Fatalf("expected non-leaf NamespaceQueue to be rejected, got %v", err)
	}
}

func TestNamespaceQueueParentIndexFunc(t *testing.T) {
	queue := &schedulingv1beta1.NamespaceQueue{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "training"},
		Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "cluster/research"},
	}
	keys, err := NamespaceQueueParentIndexFunc(queue)
	if err != nil {
		t.Fatalf("NamespaceQueueParentIndexFunc() error = %v", err)
	}
	if len(keys) != 1 || keys[0] != "cluster/research" {
		t.Fatalf("index keys = %v", keys)
	}
}

func TestGetNamespaceQueueDescendants(t *testing.T) {
	queues := []*schedulingv1beta1.NamespaceQueue{
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "department"},
			Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "cluster/research"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "training"},
			Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "department"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team-b", Name: "research"},
			Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "cluster/research"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "unrelated"},
			Spec:       schedulingv1beta1.NamespaceQueueSpec{Parent: "cluster/production"},
		},
	}

	tests := []struct {
		name       string
		withIndex  bool
		wantNames  []string
		wantErrSub string
	}{
		{
			name:      "lister fallback traverses only descendants",
			wantNames: []string{"team-a/department", "team-a/training", "team-b/research"},
		},
		{
			name:      "informer index traverses only descendants",
			withIndex: true,
			wantNames: []string{"team-a/department", "team-a/training", "team-b/research"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{
				cache.NamespaceIndex: cache.MetaNamespaceIndexFunc,
			})
			config := &AdmissionServiceConfig{
				NamespaceQueueLister: schedulinglister.NewNamespaceQueueLister(indexer),
			}
			if tt.withIndex {
				informer := cache.NewSharedIndexInformer(
					&cache.ListWatch{},
					&schedulingv1beta1.NamespaceQueue{},
					0,
					cache.Indexers{NamespaceQueueParentIndexName: NamespaceQueueParentIndexFunc},
				)
				config.NamespaceQueueInformer = informer
				config.NamespaceQueueLister = schedulinglister.NewNamespaceQueueLister(informer.GetIndexer())
				indexer = informer.GetIndexer()
			}
			for _, queue := range queues {
				if err := indexer.Add(queue); err != nil {
					t.Fatalf("failed to add NamespaceQueue: %v", err)
				}
			}

			got, err := config.GetNamespaceQueueDescendants("research")
			if err != nil {
				t.Fatalf("GetNamespaceQueueDescendants() error = %v", err)
			}
			gotNames := make([]string, 0, len(got))
			for _, queue := range got {
				gotNames = append(gotNames, queue.Namespace+"/"+queue.Name)
			}
			slices.Sort(gotNames)
			if !slices.Equal(gotNames, tt.wantNames) {
				t.Fatalf("descendants = %v, want %v", gotNames, tt.wantNames)
			}
		})
	}
}
