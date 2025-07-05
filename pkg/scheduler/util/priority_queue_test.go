package util

import (
	"container/heap"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"
)

type intItem int

func (i intItem) Value() int { return int(i) }

func makeLessFn() api.LessFn {
	return func(a, b interface{}) bool {
		return a.(intItem).Value() > b.(intItem).Value()
	}
}

func TestPriorityQueueBasic(t *testing.T) {
	pq := NewPriorityQueue(makeLessFn())

	if !pq.Empty() {
		t.Error("empty queue should report Empty()=true")
	}
	if pq.Len() != 0 {
		t.Errorf("Len() = %d; want 0", pq.Len())
	}
	// popping empty should give nil
	if got := pq.Pop(); got != nil {
		t.Errorf("Pop() on empty = %v; want nil", got)
	}

	// push a few items
	for _, v := range []intItem{3, 1, 4, 2} {
		pq.Push(v)
	}

	if pq.Empty() {
		t.Error("after pushes, Empty() should be false")
	}
	if got := pq.Len(); got != 4 {
		t.Errorf("Len() = %d; want 4", got)
	}

	wantOrder := []intItem{4, 3, 2, 1}
	for _, want := range wantOrder {
		got := pq.Pop().(intItem)
		if got != want {
			t.Errorf("Pop() = %v; want %v", got, want)
		}
	}

	if !pq.Empty() || pq.Len() != 0 {
		t.Errorf("after draining, Empty()=%v Len()=%d; want true,0", pq.Empty(), pq.Len())
	}
}

func TestPriorityQueueClone(t *testing.T) {
	pq := NewPriorityQueue(makeLessFn())
	for _, v := range []intItem{10, 20, 30} {
		pq.Push(v)
	}

	clone := pq.Clone()
	// clone should have same Len, but be a distinct object
	if clone == pq {
		t.Error("Clone returned same pointer; want distinct")
	}
	if clone.Len() != pq.Len() {
		t.Errorf("Clone.Len() = %d; want %d", clone.Len(), pq.Len())
	}

	// popping from clone should not affect original
	cloneFirst := clone.Pop().(intItem)
	if cloneFirst != 30 {
		t.Errorf("clone Pop() = %v; want 30", cloneFirst)
	}
	if pq.Len() != 3 {
		t.Errorf("original Len() after clone.Pop() = %d; want still 3", pq.Len())
	}
}

func TestHeapInterfaceCompliance(t *testing.T) {
	var h heap.Interface = &priorityQueue{
		items:  []interface{}{intItem(1), intItem(3), intItem(2)},
		lessFn: makeLessFn(),
	}
	heap.Init(h)
	got := heap.Pop(h).(intItem)
	if got != 3 {
		t.Errorf("heap.Pop() = %v; want 3", got)
	}
}
