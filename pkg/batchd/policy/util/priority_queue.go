/*
Copyright 2017 The Kubernetes Authors.

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
	"container/heap"
)

// An Item is something we manage in a priority queue.
type Item struct {
	Value    interface{} // The value of the item; arbitrary.
	Priority float64     // The priority of the item in the queue.
}

func NewItem(v interface{}, p float64) *Item {
	return &Item{
		Value:    v,
		Priority: p,
	}
}

// A PriorityQueue implements heap.Interface and holds Items.

type PriorityQueue struct {
	queue priorityQueue
}

type priorityQueue []*Item

func NewPriorityQueue() *PriorityQueue {
	return &PriorityQueue{
		queue: make(priorityQueue, 0),
	}
}

func (q *PriorityQueue) Push(it interface{}, priority float64) {
	heap.Push(&q.queue, NewItem(it, priority))
}

func (q *PriorityQueue) Pop() interface{} {
	return heap.Pop(&q.queue).(*Item).Value
}

func (q *PriorityQueue) Empty() bool {
	return q.queue.Len() == 0
}

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	// We want Pop to give us the highest, not lowest, priority so we use greater than here.
	return pq[i].Priority < pq[j].Priority
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *priorityQueue) Push(x interface{}) {
	item := x.(*Item)
	*pq = append(*pq, item)
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}
