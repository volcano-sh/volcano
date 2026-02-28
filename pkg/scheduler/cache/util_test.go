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
	"k8s.io/client-go/kubernetes/fake"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
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

			err := RemoveVolcanoSchGate(kubeClient, pod)
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
