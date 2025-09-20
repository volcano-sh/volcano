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

package api

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

func TestNewQueueInfo_DequeueStrategy(t *testing.T) {
	testCases := []struct {
		name             string
		annotations      map[string]string
		expectedStrategy string
	}{
		{
			name:             "no annotation should use default traverse strategy",
			annotations:      nil,
			expectedStrategy: DefaultDequeueStrategy,
		},
		{
			name:             "empty annotations should use default traverse strategy",
			annotations:      map[string]string{},
			expectedStrategy: DefaultDequeueStrategy,
		},
		{
			name: "valid fifo strategy",
			annotations: map[string]string{
				DequeueStrategyAnnotationKey: DequeueStrategyFIFO,
			},
			expectedStrategy: DequeueStrategyFIFO,
		},
		{
			name: "valid traverse strategy",
			annotations: map[string]string{
				DequeueStrategyAnnotationKey: DequeueStrategyTraverse,
			},
			expectedStrategy: DequeueStrategyTraverse,
		},

		{
			name: "invalid strategy should use default",
			annotations: map[string]string{
				DequeueStrategyAnnotationKey: "invalid-strategy",
			},
			expectedStrategy: DefaultDequeueStrategy,
		},
		{
			name: "empty strategy value should use default",
			annotations: map[string]string{
				DequeueStrategyAnnotationKey: "",
			},
			expectedStrategy: DefaultDequeueStrategy,
		},
		{
			name: "mixed annotations with valid strategy",
			annotations: map[string]string{
				DequeueStrategyAnnotationKey:             DequeueStrategyFIFO,
				v1beta1.KubeHierarchyAnnotationKey:       "root/child",
				v1beta1.KubeHierarchyWeightAnnotationKey: "1/2",
				"some.other.annotation":                  "value",
			},
			expectedStrategy: DequeueStrategyFIFO,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			queue := &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-queue",
					Annotations: tc.annotations,
				},
				Spec: scheduling.QueueSpec{
					Weight: 10,
				},
			}

			queueInfo := NewQueueInfo(queue)

			if queueInfo.DequeueStrategy != tc.expectedStrategy {
				t.Errorf("expected DequeueStrategy %s, but got %s", tc.expectedStrategy, queueInfo.DequeueStrategy)
			}

			// Verify other fields are set correctly
			if queueInfo.Name != "test-queue" {
				t.Errorf("expected Name test-queue, but got %s", queueInfo.Name)
			}
			if queueInfo.Weight != 10 {
				t.Errorf("expected Weight 10, but got %d", queueInfo.Weight)
			}
		})
	}
}

func TestQueueInfo_Clone(t *testing.T) {
	annotations := map[string]string{
		DequeueStrategyAnnotationKey:             DequeueStrategyFIFO,
		v1beta1.KubeHierarchyAnnotationKey:       "root/child",
		v1beta1.KubeHierarchyWeightAnnotationKey: "1/2",
	}

	queue := &scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-queue",
			Annotations: annotations,
		},
		Spec: scheduling.QueueSpec{
			Weight: 15,
		},
	}

	original := NewQueueInfo(queue)
	cloned := original.Clone()

	// Verify all fields are copied correctly
	if cloned.UID != original.UID {
		t.Errorf("expected UID %s, but got %s", original.UID, cloned.UID)
	}
	if cloned.Name != original.Name {
		t.Errorf("expected Name %s, but got %s", original.Name, cloned.Name)
	}
	if cloned.Weight != original.Weight {
		t.Errorf("expected Weight %d, but got %d", original.Weight, cloned.Weight)
	}
	if cloned.Hierarchy != original.Hierarchy {
		t.Errorf("expected Hierarchy %s, but got %s", original.Hierarchy, cloned.Hierarchy)
	}
	if cloned.Weights != original.Weights {
		t.Errorf("expected Weights %s, but got %s", original.Weights, cloned.Weights)
	}
	if cloned.DequeueStrategy != original.DequeueStrategy {
		t.Errorf("expected DequeueStrategy %s, but got %s", original.DequeueStrategy, cloned.DequeueStrategy)
	}
	if cloned.Queue != original.Queue {
		t.Errorf("expected Queue pointer to be the same")
	}

	// Verify it's a separate object (not the same pointer)
	if cloned == original {
		t.Error("cloned object should not be the same pointer as original")
	}
}

func TestDequeueStrategyConstants(t *testing.T) {
	// Verify the constants have expected values
	if DequeueStrategyFIFO != "fifo" {
		t.Errorf("expected DequeueStrategyFIFO to be 'fifo', got %s", DequeueStrategyFIFO)
	}
	if DequeueStrategyTraverse != "traverse" {
		t.Errorf("expected DequeueStrategyTraverse to be 'traverse', got %s", DequeueStrategyTraverse)
	}

	if DefaultDequeueStrategy != DequeueStrategyTraverse {
		t.Errorf("expected DefaultDequeueStrategy to be %s, got %s", DequeueStrategyTraverse, DefaultDequeueStrategy)
	}
	if DequeueStrategyAnnotationKey != "volcano.sh/dequeue-strategy" {
		t.Errorf("expected DequeueStrategyAnnotationKey to be 'volcano.sh/dequeue-strategy', got %s", DequeueStrategyAnnotationKey)
	}
}
