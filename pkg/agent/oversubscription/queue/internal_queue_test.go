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

package queue

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/agent/apis"
)

func cpuResource(v int64) apis.Resource {
	return apis.Resource{corev1.ResourceCPU: v}
}

func TestSqQueueEnqueue(t *testing.T) {
	tests := []struct {
		name     string
		enqueued int
		want     []int64
	}{
		{name: "empty queue", enqueued: 0, want: []int64{}},
		{name: "fewer items than the size", enqueued: 3, want: []int64{0, 1, 2}},
		{name: "exactly the size", enqueued: queueSize, want: []int64{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{name: "one more than the size drops the oldest", enqueued: queueSize + 1, want: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}},
		{name: "many more than the size keeps the newest", enqueued: 25, want: []int64{15, 16, 17, 18, 19, 20, 21, 22, 23, 24}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := NewSqQueue()
			for i := 0; i < tt.enqueued; i++ {
				q.Enqueue(cpuResource(int64(i)))
			}

			got := make([]int64, 0, len(tt.want))
			for _, r := range q.GetAll() {
				got = append(got, r[corev1.ResourceCPU])
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSqQueueGetAllReturnsCopy(t *testing.T) {
	q := NewSqQueue()
	q.Enqueue(cpuResource(1))
	q.Enqueue(cpuResource(2))

	snapshot := q.GetAll()
	snapshot[0] = cpuResource(100)

	assert.Equal(t, []apis.Resource{cpuResource(1), cpuResource(2)}, q.GetAll())
}

func TestSqQueueConcurrentEnqueue(t *testing.T) {
	const writers = 8
	const perWriter = 50

	q := NewSqQueue()
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perWriter; i++ {
				q.Enqueue(cpuResource(int64(i)))
				_ = q.GetAll()
			}
		}()
	}
	wg.Wait()

	require.Len(t, q.GetAll(), queueSize)
}
