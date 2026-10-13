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

package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBinderChannelCapacity(t *testing.T) {
	tests := []struct {
		name            string
		workerCount     int
		expectedMinCap  int
		expectedMaxCap  int
		expectedExactly int
	}{
		{
			name:            "single worker",
			workerCount:     1,
			expectedMinCap:  10,
			expectedMaxCap:  10,
			expectedExactly: 10,
		},
		{
			name:            "4 workers",
			workerCount:     4,
			expectedMinCap:  10,
			expectedMaxCap:  100,
			expectedExactly: 24, // 4 * 3 * 2
		},
		{
			name:            "6 workers",
			workerCount:     6,
			expectedMinCap:  10,
			expectedMaxCap:  100,
			expectedExactly: 36, // 6 * 3 * 2
		},
		{
			name:            "20 workers should cap at 100",
			workerCount:     20,
			expectedMinCap:  100,
			expectedMaxCap:  100,
			expectedExactly: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewDefaultMockSchedulerCache("test-scheduler")
			schedulingQueue := cache.schedulingQueue
			binder := NewConflictAwareBinder(cache, schedulingQueue, tt.workerCount)

			capacity := cap(binder.BindCheckChannel)
			assert.GreaterOrEqual(t, capacity, tt.expectedMinCap, "Channel capacity should meet minimum")
			assert.LessOrEqual(t, capacity, tt.expectedMaxCap, "Channel capacity should not exceed maximum")
			assert.Equal(t, tt.expectedExactly, capacity, "Channel capacity should match expected value")
		})
	}
}

func TestBinderChannelBackpressure(t *testing.T) {
	cache := NewDefaultMockSchedulerCache("test-scheduler")
	schedulingQueue := cache.schedulingQueue
	binder := NewConflictAwareBinder(cache, schedulingQueue, 4)

	capacity := cap(binder.BindCheckChannel)
	assert.Equal(t, 24, capacity, "Expected capacity of 24 for 4 workers")

	// Verify channel starts empty
	assert.Equal(t, 0, len(binder.BindCheckChannel), "Channel should start empty")
}
