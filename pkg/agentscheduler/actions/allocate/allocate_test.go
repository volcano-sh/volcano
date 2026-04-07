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

package allocate

import (
	"fmt"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/cmd/agent-scheduler/app/options"
	scheduleroptions "volcano.sh/volcano/cmd/scheduler/app/options"
	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/agentscheduler/framework"
	agentuthelper "volcano.sh/volcano/pkg/agentscheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
	commonutil "volcano.sh/volcano/pkg/util"
)

// TestConcurrentMultiWorkerScheduling verifies that multiple workers can concurrently
// schedule different pods without data races or scheduling conflicts.
func TestConcurrentMultiWorkerScheduling(t *testing.T) {
	agentuthelper.InitTestEnv(t)
	options.ServerOpts.ShardingMode = commonutil.NoneShardingMode
	scheduleroptions.ServerOpts.ShardingMode = commonutil.NoneShardingMode

	tests := []struct {
		name          string
		workerCount   int
		podsPerWorker int
		nodeCount     int
	}{
		{
			name:          "concurrent scheduling with 4 workers",
			workerCount:   4,
			podsPerWorker: 5,
			nodeCount:     20,
		},
		{
			name:          "stress scheduling with 10 workers",
			workerCount:   10,
			podsPerWorker: 100,
			nodeCount:     50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalPods := tt.workerCount * tt.podsPerWorker

			// Create a test environment with multiple workers
			testFwk, err := agentuthelper.NewTestFramework(
				"test-scheduler",
				tt.workerCount,
				[]framework.Action{New()},
				agentuthelper.DefaultTiers(),
				nil,
			)
			if err != nil {
				t.Fatalf("Failed to create test framework: %v", err)
			}
			defer testFwk.Close()

			// Add nodes to the shared cache.
			for i := 0; i < tt.nodeCount; i++ {
				node := util.BuildNode(
					fmt.Sprintf("node-%d", i),
					api.BuildResourceList("10", "20Gi", []api.ScalarResource{{Name: "pods", Value: "100"}}...),
					make(map[string]string),
				)
				testFwk.MockCache.AddOrUpdateNode(node)
			}

			// Pre-register all pods into the shared cache and push them into the shared scheduling queue.
			for i := 0; i < totalPods; i++ {
				pod := util.BuildPod("default", fmt.Sprintf("pod-%d", i), "", v1.PodPending,
					api.BuildResourceList("1", "1G"), "", make(map[string]string), make(map[string]string))
				task := api.NewTaskInfo(pod)
				testFwk.MockCache.AddTaskInfo(task)
				testFwk.SchedulingQueue.Add(klog.Background(), pod)
			}

			// Launch worker goroutines to schedule concurrently.
			var wg sync.WaitGroup
			errCh := make(chan error, totalPods)

			for w := 0; w < tt.workerCount; w++ {
				wg.Add(1)
				go func(workerIdx int) {
					defer wg.Done()
					fwk := testFwk.Frameworks[workerIdx]

					for iter := 0; iter < tt.podsPerWorker; iter++ {
						err := func() error {
							// 1. Pop from shared scheduling queue.
							queue := fwk.Cache.SchedulingQueue()
							podInfo, popErr := queue.Pop(klog.Background())
							if popErr != nil {
								return fmt.Errorf("worker %d iter %d: Pop failed: %v", workerIdx, iter, popErr)
							}
							defer queue.Done(podInfo.Pod.UID) // Mark this pod as done after processing

							// 2. GetTaskInfo from shared cache.
							task, exist := fwk.Cache.GetTaskInfo(api.TaskID(podInfo.Pod.UID))
							if !exist {
								return fmt.Errorf("worker %d iter %d: task %s not found in cache", workerIdx, iter, podInfo.Pod.UID)
							}

							schedCtx := &agentapi.SchedulingContext{
								Task:          task,
								QueuedPodInfo: podInfo,
							}

							// 3. UpdateSnapshot from shared cache into this worker's snapshot.
							snapshot := fwk.GetSnapshot()
							if snapErr := fwk.Cache.UpdateSnapshot(snapshot); snapErr != nil {
								return fmt.Errorf("worker %d iter %d: UpdateSnapshot failed: %v", workerIdx, iter, snapErr)
							}

							fwk.Cache.OnWorkerStartSchedulingCycle(workerIdx, schedCtx)

							// 4. Execute the action.
							for _, action := range fwk.Actions {
								action.Execute(fwk, schedCtx)
							}

							fwk.Cache.OnWorkerEndSchedulingCycle(workerIdx)
							fwk.ClearCycleState()
							return nil
						}()

						if err != nil {
							errCh <- err
							return
						}
					}
				}(w)
			}

			wg.Wait()
			close(errCh)

			for err := range errCh {
				t.Errorf("Concurrent scheduling error: %v", err)
			}

			// Collect all scheduling results from the shared ConflictAwareBinder.
			for i := 0; i < totalPods; i++ {
				select {
				case result := <-testFwk.MockCache.ConflictAwareBinder.BindCheckChannel:
					if result == nil || len(result.SuggestedNodes) == 0 {
						t.Errorf("pod %d: expected at least one suggested node, got nil or empty", i)
					}
				case <-time.After(5 * time.Second):
					t.Fatalf("Timeout: only received %d/%d scheduling results", i, totalPods)
				}
			}
		})
	}
}
