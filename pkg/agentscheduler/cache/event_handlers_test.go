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

	agentapi "volcano.sh/volcano/pkg/agentscheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api"
	k8sutil "volcano.sh/volcano/pkg/scheduler/plugins/util/k8s"
)

func TestRemoveNode(t *testing.T) {
	t.Run("node added again with the same name keeps accepting binds", func(t *testing.T) {
		cache, schedulingQueue := mockForTest()
		defer cache.cancel()

		binder := NewConflictAwareBinder(cache, schedulingQueue)
		cache.ConflictAwareBinder = binder
		snapshot := k8sutil.NewEmptySnapshot()

		// Bind a pod the way a worker does: refresh the snapshot, then suggest the node from the snapshot.
		bindPod := func(podName string) {
			t.Helper()
			if err := cache.UpdateSnapshot(snapshot); err != nil {
				t.Fatalf("UpdateSnapshot failed: %v", err)
			}
			node, err := snapshot.GetVolcanoNodeInfo("node-1")
			if err != nil {
				t.Fatalf("GetVolcanoNodeInfo(node-1) failed: %v", err)
			}

			task := api.NewTaskInfo(createTestPod(podName, podName))
			schedCtx := &agentapi.SchedulingContext{Task: task}
			binder.CheckAndBindPod(&agentapi.PodScheduleResult{
				SchedCtx:       schedCtx,
				SuggestedNodes: []*api.NodeInfo{node},
				BindContext:    &agentapi.BindContext{SchedCtx: schedCtx},
			})

			if _, bound := cache.Nodes["node-1"].info.Tasks[api.PodKey(task.Pod)]; !bound {
				t.Errorf("Expected %s to be bound to node-1", podName)
			}
		}

		setupCacheWithNode(cache, "node-1")
		bindPod("pod-1")

		// The snapshot is not refreshed between RemoveNode and adding the node again,
		// as for a worker that schedules no pod in that time.
		if err := cache.RemoveNode("node-1"); err != nil {
			t.Fatalf("RemoveNode(node-1) failed: %v", err)
		}
		setupCacheWithNode(cache, "node-1")
		bindPod("pod-2")
		bindPod("pod-3")
		checkNodeList(t, cache)
	})
}
