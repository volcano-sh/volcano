/*
Copyright 2024 The Volcano Authors.

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

package queue

import (
	"sync"

	"volcano.sh/volcano/pkg/agent/apis"
)

const (
	queueSize = 10
)

type SqQueue struct {
	sync.Mutex
	data []apis.Resource
}

func (q *SqQueue) Enqueue(r apis.Resource) {
	q.Lock()
	defer q.Unlock()

	q.data = append(q.data, r)
	if len(q.data) > queueSize {
		q.data = q.data[1:len(q.data)]
	}
}

func (q *SqQueue) GetAll() []apis.Resource {
	q.Lock()
	defer q.Unlock()

	queue := make([]apis.Resource, 0, len(q.data))
	queue = append(queue, q.data...)
	return queue
}

func NewSqQueue() *SqQueue {
	return &SqQueue{
		data: make([]apis.Resource, 0),
	}
}
