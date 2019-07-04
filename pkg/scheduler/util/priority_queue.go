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

	"volcano.sh/volcano/pkg/scheduler/api"
)

//PriorityQueue implements a scheduling queue.
type PriorityQueue struct {
	queue priorityQueue
}

type priorityQueue struct {
	items  []interface{}
	lessFn api.LessFn
}

// NewPriorityQueue returns a PriorityQueue
func NewPriorityQueue(lessFn api.LessFn) *PriorityQueue {
	return &PriorityQueue{
		queue: priorityQueue{
			items:  make([]interface{}, 0),
			lessFn: lessFn,
		},
	}
}

// Push pushes element in the priority Queue
func (q *PriorityQueue) Push(it interface{}) {
	heap.Push(&q.queue, it)
}

// Pop pops element in the priority Queue
func (q *PriorityQueue) Pop() interface{} {
	if q.Len() == 0 {
		return nil
	}

	return heap.Pop(&q.queue)
}

// Empty check if queue is empty
func (q *PriorityQueue) Empty() bool {
	return q.queue.Len() == 0
}

// Len returns Len of the priority queue
func (q *PriorityQueue) Len() int {
	return q.queue.Len()
}

func (pq *priorityQueue) Len() int { return len(pq.items) }

func (pq *priorityQueue) Less(i, j int) bool {
	if pq.lessFn == nil {
		return i < j
	}

	// We want Pop to give us the highest, not lowest, priority so we use greater than here.
	return pq.lessFn(pq.items[i], pq.items[j])
}

func (pq priorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
}

func (pq *priorityQueue) Push(x interface{}) {
	(*pq).items = append((*pq).items, x)
}

func (pq *priorityQueue) Pop() interface{} {
	old := (*pq).items
	n := len(old)
	item := old[n-1]
	(*pq).items = old[0 : n-1]
	return item
}
