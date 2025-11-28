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

func TestNewQueueInfo(t *testing.T) {
	tests := []struct {
		name                    string
		queue                   *scheduling.Queue
		expectedUID             QueueID
		expectedName            string
		expectedWeight          int32
		expectedHierarchy       string
		expectedWeights         string
		expectedDequeueStrategy DequeueStrategy
	}{
		{
			name: "queue with default dequeue strategy",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-queue-1",
					Annotations: map[string]string{
						v1beta1.KubeHierarchyAnnotationKey:       "root/child",
						v1beta1.KubeHierarchyWeightAnnotationKey: "1/2",
					},
				},
				Spec: scheduling.QueueSpec{
					Weight:          10,
					DequeueStrategy: "", // Empty string should use default
				},
			},
			expectedUID:             QueueID("test-queue-1"),
			expectedName:            "test-queue-1",
			expectedWeight:          10,
			expectedHierarchy:       "root/child",
			expectedWeights:         "1/2",
			expectedDequeueStrategy: DefaultDequeueStrategy,
		},
		{
			name: "queue with specified dequeue strategy",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-queue-2",
					Annotations: map[string]string{
						v1beta1.KubeHierarchyAnnotationKey:       "root/child/grandchild",
						v1beta1.KubeHierarchyWeightAnnotationKey: "1/2/3",
					},
				},
				Spec: scheduling.QueueSpec{
					Weight:          20,
					DequeueStrategy: DequeueStrategyTraverse,
				},
			},
			expectedUID:             QueueID("test-queue-2"),
			expectedName:            "test-queue-2",
			expectedWeight:          20,
			expectedHierarchy:       "root/child/grandchild",
			expectedWeights:         "1/2/3",
			expectedDequeueStrategy: DequeueStrategyTraverse,
		},
		{
			name: "queue with FIFO dequeue strategy",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-queue-3",
					Annotations: map[string]string{
						v1beta1.KubeHierarchyAnnotationKey: "root",
					},
				},
				Spec: scheduling.QueueSpec{
					Weight:          30,
					DequeueStrategy: DequeueStrategyFIFO,
				},
			},
			expectedUID:             QueueID("test-queue-3"),
			expectedName:            "test-queue-3",
			expectedWeight:          30,
			expectedHierarchy:       "root",
			expectedWeights:         "",
			expectedDequeueStrategy: DequeueStrategyFIFO,
		},
		{
			name: "queue with nil annotations",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-queue-4",
					Annotations: nil,
				},
				Spec: scheduling.QueueSpec{
					Weight:          40,
					DequeueStrategy: DequeueStrategyTraverse,
				},
			},
			expectedUID:             QueueID("test-queue-4"),
			expectedName:            "test-queue-4",
			expectedWeight:          40,
			expectedHierarchy:       "",
			expectedWeights:         "",
			expectedDequeueStrategy: DequeueStrategyTraverse,
		},
		{
			name: "queue with empty annotations",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-queue-5",
					Annotations: map[string]string{},
				},
				Spec: scheduling.QueueSpec{
					Weight:          50,
					DequeueStrategy: "",
				},
			},
			expectedUID:             QueueID("test-queue-5"),
			expectedName:            "test-queue-5",
			expectedWeight:          50,
			expectedHierarchy:       "",
			expectedWeights:         "",
			expectedDequeueStrategy: DefaultDequeueStrategy,
		},
		{
			name: "queue with partial annotations - only hierarchy",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-queue-6",
					Annotations: map[string]string{
						v1beta1.KubeHierarchyAnnotationKey: "root/child",
					},
				},
				Spec: scheduling.QueueSpec{
					Weight:          60,
					DequeueStrategy: DequeueStrategyFIFO,
				},
			},
			expectedUID:             QueueID("test-queue-6"),
			expectedName:            "test-queue-6",
			expectedWeight:          60,
			expectedHierarchy:       "root/child",
			expectedWeights:         "",
			expectedDequeueStrategy: DequeueStrategyFIFO,
		},
		{
			name: "queue with partial annotations - only weights",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-queue-7",
					Annotations: map[string]string{
						v1beta1.KubeHierarchyWeightAnnotationKey: "1/2/3",
					},
				},
				Spec: scheduling.QueueSpec{
					Weight:          70,
					DequeueStrategy: "",
				},
			},
			expectedUID:             QueueID("test-queue-7"),
			expectedName:            "test-queue-7",
			expectedWeight:          70,
			expectedHierarchy:       "",
			expectedWeights:         "1/2/3",
			expectedDequeueStrategy: DefaultDequeueStrategy,
		},
		{
			name: "queue with zero weight",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-queue-8",
					Annotations: map[string]string{
						v1beta1.KubeHierarchyAnnotationKey:       "root",
						v1beta1.KubeHierarchyWeightAnnotationKey: "1",
					},
				},
				Spec: scheduling.QueueSpec{
					Weight:          0,
					DequeueStrategy: DequeueStrategyTraverse,
				},
			},
			expectedUID:             QueueID("test-queue-8"),
			expectedName:            "test-queue-8",
			expectedWeight:          0,
			expectedHierarchy:       "root",
			expectedWeights:         "1",
			expectedDequeueStrategy: DequeueStrategyTraverse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queueInfo := NewQueueInfo(tt.queue)

			if queueInfo == nil {
				t.Fatal("NewQueueInfo returned nil")
			}

			if queueInfo.UID != tt.expectedUID {
				t.Errorf("expected UID %s, but got %s", tt.expectedUID, queueInfo.UID)
			}

			if queueInfo.Name != tt.expectedName {
				t.Errorf("expected Name %s, but got %s", tt.expectedName, queueInfo.Name)
			}

			if queueInfo.Weight != tt.expectedWeight {
				t.Errorf("expected Weight %d, but got %d", tt.expectedWeight, queueInfo.Weight)
			}

			if queueInfo.Hierarchy != tt.expectedHierarchy {
				t.Errorf("expected Hierarchy %s, but got %s", tt.expectedHierarchy, queueInfo.Hierarchy)
			}

			if queueInfo.Weights != tt.expectedWeights {
				t.Errorf("expected Weights %s, but got %s", tt.expectedWeights, queueInfo.Weights)
			}

			if queueInfo.DequeueStrategy != tt.expectedDequeueStrategy {
				t.Errorf("expected DequeueStrategy %s, but got %s", tt.expectedDequeueStrategy, queueInfo.DequeueStrategy)
			}

			if queueInfo.Queue != tt.queue {
				t.Errorf("expected Queue pointer to be the same as input")
			}
		})
	}
}

func TestQueueInfo_Clone(t *testing.T) {
	annotations := map[string]string{
		v1beta1.KubeHierarchyAnnotationKey:       "root/child",
		v1beta1.KubeHierarchyWeightAnnotationKey: "1/2",
	}

	queue := &scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-queue",
			Annotations: annotations,
		},
		Spec: scheduling.QueueSpec{
			Weight:          15,
			DequeueStrategy: DequeueStrategyTraverse,
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
	if cloned.Queue != original.Queue {
		t.Errorf("expected Queue pointer to be the same")
	}
	if cloned.DequeueStrategy != original.DequeueStrategy {
		t.Errorf("expected DequeueStrategy %s, but got %s", original.DequeueStrategy, cloned.DequeueStrategy)
	}
	// Verify it's a separate object (not the same pointer)
	if cloned == original {
		t.Error("cloned object should not be the same pointer as original")
	}
}
