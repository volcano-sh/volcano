/*
Copyright 2018-2025 The Volcano Authors.

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
package util

import (
	"testing"
)

// intHigherPriority returns true if a has higher priority than b (max-heap ordering).
func intHigherPriority(a, b interface{}) bool {
	return a.(int) > b.(int)
}

func TestNewPriorityQueue(t *testing.T) {
	pq := NewPriorityQueue(intHigherPriority)
	if pq == nil {
		t.Fatal("expected non-nil priority queue")
	}
	if !pq.Empty() {
		t.Fatal("expected empty queue")
	}
	if pq.Len() != 0 {
		t.Fatalf("expected length 0, got %d", pq.Len())
	}
}

func TestPriorityQueue_PushPop(t *testing.T) {
	pq := NewPriorityQueue(intHigherPriority)
	pq.Push(3)
	pq.Push(1)
	pq.Push(4)
	pq.Push(2)

	if pq.Len() != 4 {
		t.Fatalf("expected length 4, got %d", pq.Len())
	}

	expected := []int{4, 3, 2, 1}
	for i, exp := range expected {
		got := pq.Pop().(int)
		if got != exp {
			t.Errorf("pop %d: expected %d, got %d", i, exp, got)
		}
	}
}

func TestPriorityQueue_PopEmpty(t *testing.T) {
	pq := NewPriorityQueue(intHigherPriority)
	if got := pq.Pop(); got != nil {
		t.Fatalf("expected nil on empty pop, got %v", got)
	}
}

func TestPriorityQueue_Empty(t *testing.T) {
	pq := NewPriorityQueue(intHigherPriority)
	if !pq.Empty() {
		t.Fatal("expected empty")
	}
	pq.Push(1)
	if pq.Empty() {
		t.Fatal("expected non-empty after push")
	}
	pq.Pop()
	if !pq.Empty() {
		t.Fatal("expected empty after pop")
	}
}

func TestPriorityQueue_Clone(t *testing.T) {
	pq := NewPriorityQueue(intHigherPriority)
	pq.Push(1)
	pq.Push(3)
	pq.Push(2)

	clone := pq.Clone()

	if clone.Len() != pq.Len() {
		t.Fatalf("expected clone length %d, got %d", pq.Len(), clone.Len())
	}

	// Mutate clone and verify original is unaffected
	clone.Push(100)

	// Verify original pop order is unchanged
	expectedPQ := []int{3, 2, 1}
	for i, exp := range expectedPQ {
		got := pq.Pop().(int)
		if got != exp {
			t.Errorf("original pq pop %d: expected %d, got %d", i, exp, got)
		}
	}

	// Verify clone contains the extra element
	expectedClone := []int{100, 3, 2, 1}
	for i, exp := range expectedClone {
		got := clone.Pop().(int)
		if got != exp {
			t.Errorf("clone pop %d: expected %d, got %d", i, exp, got)
		}
	}
}

func TestPriorityQueue_NilLessFn(t *testing.T) {
	pq := NewPriorityQueue(nil)
	pq.Push(3)
	pq.Push(1)
	pq.Push(2)

	// Drain the queue completely to verify no panic during any heap operations
	for pq.Len() > 0 {
		pq.Pop()
	}
}
