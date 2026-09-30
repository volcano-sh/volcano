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

// TestSchedulingGateRemoval verifies that waiting does not produce a binding
// request and that a pod update can make the same task schedulable again.
func TestSchedulingGateRemoval(t *testing.T) {
	agentuthelper.InitTestEnv(t)
	options.ServerOpts.ShardingMode = commonutil.NoneShardingMode
	scheduleroptions.ServerOpts.ShardingMode = commonutil.NoneShardingMode
	action := New()
	tf, err := agentuthelper.NewTestFramework("test-scheduler", 1,
		[]framework.Action{action}, agentuthelper.DefaultTiers(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tf.Close()
	node := util.BuildNode("node", api.BuildResourceList("2", "2Gi", api.ScalarResource{Name: "pods", Value: "10"}), nil)
	tf.MockCache.AddOrUpdateNode(node)
	pod := util.BuildPod("default", "gated", "", v1.PodPending,
		api.BuildResourceList("1", "1Gi"), "", nil, nil)
	pod.Spec.SchedulerName = "test-scheduler"
	pod.Spec.SchedulingGates = []v1.PodSchedulingGate{{Name: "example.com/hold"}, {Name: "example.com/second"}}
	pod.Status.Conditions = []v1.PodCondition{{Type: v1.PodScheduled, Status: v1.ConditionFalse, Reason: v1.PodReasonSchedulingGated}}
	task := api.NewTaskInfo(pod)
	if !task.SchGated {
		t.Fatal("expected a scheduling-gated task")
	}
	tf.MockCache.AddTaskInfo(task)
	queue := tf.SchedulingQueue
	logger := klog.Background()
	queue.Add(logger, pod)

	assertGated := func() {
		t.Helper()
		if len(queue.PodsInActiveQ()) != 0 || len(queue.PodsInBackoffQ()) != 0 || len(queue.UnschedulablePods()) != 1 {
			t.Fatal("gated pod must remain only in the unschedulable queue")
		}
		info, found := queue.GetPod(pod.Name, pod.Namespace)
		if !found || info.GatingPlugin != "SchedulingGates" || info.Attempts != 0 {
			t.Fatalf("expected SchedulingGates to reject pod before scheduling: %+v", info)
		}
		if info.Pod.Status.Conditions[0].Reason != v1.PodReasonSchedulingGated {
			t.Fatal("gated pod status changed")
		}
	}
	assertGated()
	control := pod.DeepCopy()
	control.Name = "control"
	control.UID = "control"
	control.Spec.SchedulingGates = nil
	queue.Add(logger, control)
	if active := queue.PodsInActiveQ(); len(active) != 1 || active[0].UID != control.UID {
		t.Fatal("ungated control pod did not enter the active queue")
	}
	controlInfo, err := queue.Pop(logger)
	if err != nil || controlInfo.Pod.UID != control.UID {
		t.Fatalf("expected ungated control pod, got %v, error: %v", controlInfo, err)
	}
	queue.Done(control.UID)
	assertGated()
	// An unrelated update and removal of only one gate must not activate the pod.
	labeled := pod.DeepCopy()
	labeled.Labels = map[string]string{"test": "updated"}
	queue.Update(logger, pod, labeled)
	assertGated()
	partial := labeled.DeepCopy()
	partial.Spec.SchedulingGates = partial.Spec.SchedulingGates[:1]
	queue.Update(logger, labeled, partial)
	assertGated()

	updated := pod.DeepCopy()
	updated.Spec.SchedulingGates = nil
	task = api.NewTaskInfo(updated)
	tf.MockCache.UpdateTaskInfo(task)
	queue.Update(logger, partial, updated)
	if len(queue.PodsInActiveQ()) != 1 {
		t.Fatal("removing the final gate did not activate the pod")
	}
	info, err := queue.Pop(logger)
	if err != nil {
		t.Fatal(err)
	}
	defer queue.Done(updated.UID)
	if info.Pod.UID != pod.UID {
		t.Fatal("expected the original pod after gate removal")
	}
	fwk := tf.Frameworks[0]
	if err := fwk.Cache.UpdateSnapshot(fwk.GetSnapshot()); err != nil {
		t.Fatal(err)
	}
	action.Execute(fwk, &agentapi.SchedulingContext{Task: task, QueuedPodInfo: info})
	select {
	case result := <-tf.MockCache.ConflictAwareBinder.BindCheckChannel:
		if result == nil || len(result.SuggestedNodes) == 0 {
			t.Fatal("released pod has no candidate node")
		}
	case <-time.After(time.Second):
		t.Fatal("released pod did not reach the binding path")
	}
}
