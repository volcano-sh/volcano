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

func boolPtr(b bool) *bool { return &b }

func TestNewQueueInfo(t *testing.T) {
	tests := []struct {
		name            string
		queue           *scheduling.Queue
		wantUID         QueueID
		wantName        string
		wantWeight      int32
		wantHierarchy   string
		wantWeights     string
	}{
		{
			name: "basic queue with no annotations",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "default"},
				Spec:       scheduling.QueueSpec{Weight: 1},
			},
			wantUID:    QueueID("default"),
			wantName:   "default",
			wantWeight: 1,
		},
		{
			name: "queue with hierarchy annotations",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{
					Name: "child",
					Annotations: map[string]string{
						v1beta1.KubeHierarchyAnnotationKey:       "root/parent/child",
						v1beta1.KubeHierarchyWeightAnnotationKey: "1/2/3",
					},
				},
				Spec: scheduling.QueueSpec{Weight: 5},
			},
			wantUID:       QueueID("child"),
			wantName:      "child",
			wantWeight:    5,
			wantHierarchy: "root/parent/child",
			wantWeights:   "1/2/3",
		},
		{
			name: "queue with zero weight",
			queue: &scheduling.Queue{
				ObjectMeta: metav1.ObjectMeta{Name: "zero-weight"},
				Spec:       scheduling.QueueSpec{Weight: 0},
			},
			wantUID:    QueueID("zero-weight"),
			wantName:   "zero-weight",
			wantWeight: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qi := NewQueueInfo(tt.queue)
			if qi.UID != tt.wantUID {
				t.Errorf("UID: got %v, want %v", qi.UID, tt.wantUID)
			}
			if qi.Name != tt.wantName {
				t.Errorf("Name: got %v, want %v", qi.Name, tt.wantName)
			}
			if qi.Weight != tt.wantWeight {
				t.Errorf("Weight: got %v, want %v", qi.Weight, tt.wantWeight)
			}
			if qi.Hierarchy != tt.wantHierarchy {
				t.Errorf("Hierarchy: got %v, want %v", qi.Hierarchy, tt.wantHierarchy)
			}
			if qi.Weights != tt.wantWeights {
				t.Errorf("Weights: got %v, want %v", qi.Weights, tt.wantWeights)
			}
			if qi.Queue != tt.queue {
				t.Errorf("Queue pointer not preserved")
			}
		})
	}
}

func TestQueueInfoClone(t *testing.T) {
	original := &QueueInfo{
		UID:       QueueID("q1"),
		Name:      "q1",
		Weight:    3,
		Hierarchy: "root/q1",
		Weights:   "1/3",
		Queue: &scheduling.Queue{
			ObjectMeta: metav1.ObjectMeta{Name: "q1"},
			Spec:       scheduling.QueueSpec{Weight: 3},
		},
	}

	clone := original.Clone()

	if clone == original {
		t.Error("Clone returned the same pointer")
	}
	if clone.UID != original.UID {
		t.Errorf("UID: got %v, want %v", clone.UID, original.UID)
	}
	if clone.Name != original.Name {
		t.Errorf("Name: got %v, want %v", clone.Name, original.Name)
	}
	if clone.Weight != original.Weight {
		t.Errorf("Weight: got %v, want %v", clone.Weight, original.Weight)
	}
	if clone.Hierarchy != original.Hierarchy {
		t.Errorf("Hierarchy: got %v, want %v", clone.Hierarchy, original.Hierarchy)
	}
	if clone.Weights != original.Weights {
		t.Errorf("Weights: got %v, want %v", clone.Weights, original.Weights)
	}
	// Queue must be a deep copy, not the same pointer
	if clone.Queue == original.Queue {
		t.Error("Clone did not deep-copy the Queue pointer")
	}
	if clone.Queue.Name != original.Queue.Name {
		t.Errorf("Queue.Name: got %v, want %v", clone.Queue.Name, original.Queue.Name)
	}
}

func TestQueueInfoReclaimable(t *testing.T) {
	tests := []struct {
		name       string
		queueInfo  *QueueInfo
		wantResult bool
	}{
		{
			name:       "nil QueueInfo returns false",
			queueInfo:  nil,
			wantResult: false,
		},
		{
			name:       "nil Queue field returns false",
			queueInfo:  &QueueInfo{},
			wantResult: false,
		},
		{
			name: "nil Reclaimable field defaults to true",
			queueInfo: &QueueInfo{
				Queue: &scheduling.Queue{
					Spec: scheduling.QueueSpec{Reclaimable: nil},
				},
			},
			wantResult: true,
		},
		{
			name: "Reclaimable explicitly set to true",
			queueInfo: &QueueInfo{
				Queue: &scheduling.Queue{
					Spec: scheduling.QueueSpec{Reclaimable: boolPtr(true)},
				},
			},
			wantResult: true,
		},
		{
			name: "Reclaimable explicitly set to false",
			queueInfo: &QueueInfo{
				Queue: &scheduling.Queue{
					Spec: scheduling.QueueSpec{Reclaimable: boolPtr(false)},
				},
			},
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.queueInfo.Reclaimable()
			if got != tt.wantResult {
				t.Errorf("Reclaimable() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}
