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
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"

	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
)

func TestNewConflictAwareBinder(t *testing.T) {
	tests := []struct {
		name      string
		capacity  int
		wantError bool
	}{
		{name: "negative capacity", capacity: -1, wantError: true},
		{name: "unbuffered handoff", capacity: 0},
		{name: "one queued result", capacity: 1},
		{name: "capacity below former minimum", capacity: 7},
		{name: "capacity above former maximum", capacity: 128},
		{name: "legacy capacity", capacity: 5000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, queue := mockForTest()
			t.Cleanup(cache.cancel)
			t.Cleanup(cache.nodeQueue.ShutDown)
			binder, err := NewConflictAwareBinder(cache, queue, tt.capacity)
			if tt.wantError {
				require.EqualError(t, err, "binder-channel-capacity must be greater than or equal to 0")
				assert.Nil(t, binder)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.capacity, cap(binder.BindCheckChannel))
		})
	}
}

func TestEnqueueScheduleResultBackpressure(t *testing.T) {
	tests := []struct {
		name     string
		capacity int
	}{
		{name: "direct handoff", capacity: 0},
		{name: "single slot", capacity: 1},
		{name: "multiple slots", capacity: 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Keep the cache's unrelated background queues outside the bubble;
			// only the Binder handoff goroutines participate in synctest.Wait.
			cache, queue := mockForTest()
			t.Cleanup(cache.cancel)
			t.Cleanup(cache.nodeQueue.ShutDown)
			synctest.Test(t, func(t *testing.T) {
				binder, err := NewConflictAwareBinder(cache, queue, tt.capacity)
				require.NoError(t, err)
				results := make([]*agentapi.PodScheduleResult, tt.capacity+1)
				for i := range results {
					results[i] = newBinderScheduleResult(fmt.Sprintf("pod-%d", i), nil)
				}
				for _, result := range results[:tt.capacity] {
					binder.EnqueueScheduleResult(result)
				}
				enqueued := make(chan struct{})
				go func() {
					binder.EnqueueScheduleResult(results[tt.capacity])
					close(enqueued)
				}()
				// Wait until the sender is durably blocked, rather than
				// inferring backpressure from an arbitrary elapsed duration.
				synctest.Wait()
				select {
				case <-enqueued:
					t.Error("enqueue completed without a free slot or receiver")
				default:
				}
				assert.Same(t, results[0], <-binder.BindCheckChannel)
				<-enqueued
				for _, result := range results[1:] {
					assert.Same(t, result, <-binder.BindCheckChannel)
				}
				assert.Empty(t, binder.BindCheckChannel)
			})
		})
	}
}

func TestEnqueueScheduleResultKeepsBlockedCandidatesVisible(t *testing.T) {
	// Cross serial/concurrent workers with no buffer, one slot and a buffer
	// larger than the worker pool; independently vary single/default/custom K.
	type handoffCase struct {
		name                          string
		capacity, workers, candidates int
	}
	var tests []handoffCase
	for _, workers := range []int{1, 4} {
		for _, capacity := range []int{0, 1, workers + 1} {
			for _, candidates := range []int{1, 3, 5} {
				tests = append(tests, handoffCase{
					name:     fmt.Sprintf("workers=%d/capacity=%d/candidates=%d", workers, capacity, candidates),
					capacity: capacity, workers: workers, candidates: candidates,
				})
			}
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Keep the cache's unrelated background queues outside the bubble;
			// only the Binder handoff goroutines participate in synctest.Wait.
			cache, queue := mockForTest()
			t.Cleanup(cache.cancel)
			t.Cleanup(cache.nodeQueue.ShutDown)
			synctest.Test(t, func(t *testing.T) {
				for i := range tt.candidates {
					setupCacheWithNode(cache, fmt.Sprintf("node-%d", i))
				}
				snapshot := k8sutil.NewEmptySnapshot()
				require.NoError(t, cache.UpdateSnapshot(snapshot))
				nodes := make([]*api.NodeInfo, 0, tt.candidates)
				for i := range tt.candidates {
					node, err := snapshot.GetVolcanoNodeInfo(fmt.Sprintf("node-%d", i))
					require.NoError(t, err)
					nodes = append(nodes, node)
				}
				pausedCache := &pausedAssumeCache{Cache: cache, entered: make(chan struct{}), resume: make(chan struct{})}
				binder, err := NewConflictAwareBinder(pausedCache, queue, tt.capacity)
				require.NoError(t, err)
				firstDone := make(chan struct{})
				go func() {
					binder.CheckAndBindPod(<-binder.BindCheckChannel)
					close(firstDone)
				}()
				cache.RecordCandidateNodesInBinder(nodes)
				binder.EnqueueScheduleResult(newBinderScheduleResult("consumer-pod", nodes))
				<-pausedCache.entered

				// Fill the queue with earlier results. Each current worker
				// then registers one more result before its blocking send,
				// matching allocateTask's production handoff sequence.
				for i := range tt.capacity {
					cache.RecordCandidateNodesInBinder(nodes)
					binder.EnqueueScheduleResult(newBinderScheduleResult(fmt.Sprintf("queued-%d", i), nodes))
				}
				workerDone := make(chan struct{}, tt.workers)
				for i := range tt.workers {
					go func() {
						cache.RecordCandidateNodesInBinder(nodes)
						binder.EnqueueScheduleResult(newBinderScheduleResult(fmt.Sprintf("worker-%d", i), nodes))
						workerDone <- struct{}{}
					}()
				}
				resume := sync.OnceFunc(func() { close(pausedCache.resume) })
				defer func() {
					resume()
					<-firstDone
					// Drain and release the fixture's remaining reservations;
					// binding success/conflict cleanup is covered separately.
					for range tt.capacity + tt.workers {
						result := <-binder.BindCheckChannel
						cache.RemoveCandidateNodesFromBinder(result.SuggestedNodes)
					}
					for range tt.workers {
						<-workerDone
					}
				}()
				synctest.Wait()
				assert.Len(t, binder.BindCheckChannel, tt.capacity)
				assert.Empty(t, workerDone, "senders must wait while the Binder is paused")
				require.NoError(t, cache.UpdateSnapshot(snapshot))
				pendingResults := tt.capacity + tt.workers + 1
				totalReferences := 0
				for _, node := range nodes {
					refs := snapshot.NodesInBinder[node.Name]
					assert.Equal(t, pendingResults, refs)
					totalReferences += refs
				}
				assert.Equal(t, tt.candidates*pendingResults, totalReferences)
				// Finishing the single consumer releases exactly its K refs.
				// The C buffered results and W blocked senders remain pending.
				resume()
				<-firstDone
				synctest.Wait()
				require.NoError(t, cache.UpdateSnapshot(snapshot))
				remainingReferences := 0
				for _, node := range nodes {
					refs := snapshot.NodesInBinder[node.Name]
					assert.Equal(t, tt.capacity+tt.workers, refs)
					remainingReferences += refs
				}
				assert.Equal(t, tt.candidates*(tt.capacity+tt.workers), remainingReferences)
			})
		})
	}
}

func newBinderScheduleResult(name string, nodes []*api.NodeInfo) *agentapi.PodScheduleResult {
	task := api.NewTaskInfo(createTestPod(name, name))
	schedCtx := &agentapi.SchedulingContext{Task: task}
	return &agentapi.PodScheduleResult{
		SchedCtx:       schedCtx,
		SuggestedNodes: nodes,
		BindContext:    &agentapi.BindContext{SchedCtx: schedCtx},
	}
}

func TestNewRejectsNegativeBinderCapacity(t *testing.T) {
	opt := options.NewServerOption()
	opt.BinderChannelCapacity = -1
	// Capacity validation must precede client construction, even when the
	// programmatic caller also supplied an invalid REST configuration.
	schedulerCache, err := New(&rest.Config{Host: "://invalid"}, opt)
	require.EqualError(t, err, "binder-channel-capacity must be greater than or equal to 0")
	assert.True(t, schedulerCache == nil, "the Cache interface must not contain a typed nil")
}
