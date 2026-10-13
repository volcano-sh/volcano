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

package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShutdownMockSchedulerCache(t *testing.T) {
	for _, tc := range []struct {
		name     string
		newCache func() *SchedulerCache
	}{
		{name: "default", newCache: func() *SchedulerCache { return NewDefaultMockSchedulerCache("volcano") }},
		{name: "custom", newCache: func() *SchedulerCache { return NewCustomMockSchedulerCache("volcano", nil, nil, nil, nil, nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sc := tc.newCache()
			t.Cleanup(func() { ShutdownMockSchedulerCache(sc) })
			queues := map[string]interface{ ShuttingDown() bool }{
				"errTasks":    sc.errTasks,
				"nodes":       sc.nodeQueue,
				"deletedJobs": sc.DeletedJobs,
				"hyperNodes":  sc.hyperNodesQueue,
			}
			for name, queue := range queues {
				assert.False(t, queue.ShuttingDown(), "%s should initially be active", name)
			}

			ShutdownMockSchedulerCache(sc)
			for name, queue := range queues {
				assert.True(t, queue.ShuttingDown(), "%s must be shut down", name)
			}
			// Explicit shutdown followed by test cleanup must be safe.
			assert.NotPanics(t, func() { ShutdownMockSchedulerCache(sc) })
		})
	}
}
