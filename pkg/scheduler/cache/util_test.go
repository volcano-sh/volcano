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

package cache

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes/fake"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

func TestRemoveVolcanoSchGate(t *testing.T) {
	tests := []struct {
		name         string
		initialGates []v1.PodSchedulingGate
		want         []v1.PodSchedulingGate
	}{
		{
			name: "remove volcano gate from pod with only volcano gate",
			initialGates: []v1.PodSchedulingGate{
				{Name: scheduling.QueueAllocationGateKey},
			},
			want: nil,
		},
		{
			name: "remove volcano gate from pod with multiple gates",
			initialGates: []v1.PodSchedulingGate{
				{Name: "some-other-gate"},
				{Name: scheduling.QueueAllocationGateKey},
				{Name: "another-custom-gate"},
			},
			want: []v1.PodSchedulingGate{
				{Name: "some-other-gate"},
				{Name: "another-custom-gate"},
			},
		},
		{
			name: "idempotent: pod without volcano gate",
			initialGates: []v1.PodSchedulingGate{
				{Name: "some-other-gate"},
			},
			want: []v1.PodSchedulingGate{
				{Name: "some-other-gate"},
			},
		},
		{
			name:         "idempotent: pod with nil gates",
			initialGates: nil,
			want:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 1. Setup the initial Pod
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: v1.PodSpec{
					SchedulingGates: tt.initialGates,
				},
			}

			kubeClient := fake.NewSimpleClientset(pod)

			err := RemoveVolcanoSchGate(kubeClient, pod.Namespace, pod.Name)
			if err != nil {
				t.Fatalf("RemoveVolcanoSchGate returned an unexpected error: %v", err)
			}

			updatedPod, err := kubeClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Failed to fetch updated pod: %v", err)
			}

			if len(updatedPod.Spec.SchedulingGates) != len(tt.want) {
				t.Fatalf("Expected %d gates, got %d", len(tt.want), len(updatedPod.Spec.SchedulingGates))
			}

			for i, expectedGate := range tt.want {
				if updatedPod.Spec.SchedulingGates[i].Name != expectedGate.Name {
					t.Errorf("Mismatch at index %d: expected gate %q, got %q",
						i, expectedGate.Name, updatedPod.Spec.SchedulingGates[i].Name)
				}
			}
		})
	}
}

func TestResolveQueueReference(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, true)
	tests := []struct {
		name      string
		namespace string
		reference string
		expected  schedulingapi.QueueID
		wantErr   bool
	}{
		{
			name:      "empty reference uses default cluster queue",
			namespace: "team-a",
			reference: "",
			expected:  "default",
		},
		{
			name:      "plain reference selects cluster queue",
			namespace: "team-a",
			reference: "research",
			expected:  "research",
		},
		{
			name:      "namespace reference selects local namespace queue",
			namespace: "team-a",
			reference: "namespace/training",
			expected:  "team-a/training",
		},
		{
			name:      "cluster prefix is invalid for workload reference",
			namespace: "team-a",
			reference: "cluster/research",
			wantErr:   true,
		},
		{
			name:      "namespace queue name is required",
			namespace: "team-a",
			reference: "namespace/",
			wantErr:   true,
		},
		{
			name:      "cross namespace reference is invalid",
			namespace: "team-a",
			reference: "team-b/training",
			wantErr:   true,
		},
		{
			name:      "namespace queue requires workload namespace",
			reference: "namespace/training",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queueID, err := resolveQueueReference(
				tt.namespace,
				tt.reference,
				"default",
			)

			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr = %t", err, tt.wantErr)
			}

			if !tt.wantErr && queueID != tt.expected {
				t.Fatalf("QueueID = %q, want %q", queueID, tt.expected)
			}
		})
	}
}

func TestResolveQueueReferenceRejectsNamespaceQueueWhenDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, false)
	if _, err := resolveQueueReference("team-a", "namespace/training", "default"); err == nil {
		t.Fatal("resolveQueueReference() accepted NamespaceQueue while feature gate is disabled")
	}
}

func TestResolveQueueReferenceRejectsNamespaceDefaultQueueWhenDisabled(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.NamespaceQueue, false)
	if _, err := resolveQueueReference("team-a", "", "namespace/training"); err == nil {
		t.Fatal("resolveQueueReference() accepted a NamespaceQueue default while the feature gate was disabled")
	}
}
