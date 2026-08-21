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

package tasktopology

import (
	"maps"
	"reflect"
	"sort"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type bucketState struct {
	tasks       map[types.UID]struct{}
	taskNameSet map[string]int
	reqScore    float64
	request     api.Resource
	boundTask   int
	node        map[string]int
}

type managerState struct {
	buckets     []bucketState
	nodeTaskSet map[string]map[string]int
}

func snapshotManager(jm *JobManager) managerState {
	state := managerState{
		buckets:     make([]bucketState, len(jm.buckets)),
		nodeTaskSet: make(map[string]map[string]int, len(jm.nodeTaskSet)),
	}
	for index, bucket := range jm.buckets {
		tasks := make(map[types.UID]struct{}, len(bucket.tasks))
		for uid := range bucket.tasks {
			tasks[uid] = struct{}{}
		}
		state.buckets[index] = bucketState{
			tasks:       tasks,
			taskNameSet: maps.Clone(bucket.taskNameSet),
			reqScore:    bucket.reqScore,
			request:     *bucket.request.Clone(),
			boundTask:   bucket.boundTask,
			node:        maps.Clone(bucket.node),
		}
	}
	for node, taskSet := range jm.nodeTaskSet {
		state.nodeTaskSet[node] = maps.Clone(taskSet)
	}
	return state
}

func newRollbackSession(t *testing.T, withBoundTask bool) (*framework.Session, *JobManager, []*api.TaskInfo, *api.NodeInfo) {
	t.Helper()

	schedulerCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
	schedulerCache.AddOrUpdateNode(
		util.BuildNode("n1", api.BuildResourceList("4", "8Gi", api.ScalarResource{Name: "pods", Value: "10"}), nil),
	)
	for index, name := range []string{"worker-0", "worker-1"} {
		nodeName := ""
		phase := v1.PodPending
		if withBoundTask && index == 0 {
			nodeName = "n1"
			phase = v1.PodRunning
		}
		pod := util.BuildPod("ns1", name, nodeName, phase, api.BuildResourceList("1", "1Gi"), "pg1", nil, nil)
		pod.Annotations[batchv1alpha1.TaskSpecKey] = "worker"
		schedulerCache.AddPod(pod)
	}
	podGroup := util.BuildPodGroup("pg1", "ns1", "q1", 2, nil, v1beta1.PodGroupInqueue)
	podGroup.Annotations = map[string]string{JobAffinityAnnotations: "worker"}
	schedulerCache.AddPodGroupV1beta1(podGroup)
	schedulerCache.AddQueueV1beta1(util.BuildQueue("q1", 1, nil))

	session := framework.OpenSession(schedulerCache, nil, nil)
	plugin := New(framework.Arguments{}).(*taskTopologyPlugin)
	plugin.OnSessionOpen(session)
	t.Cleanup(func() {
		plugin.OnSessionClose(session)
		framework.CloseSession(session)
	})

	var job *api.JobInfo
	for _, candidate := range session.Jobs {
		job = candidate
		break
	}
	if job == nil {
		t.Fatal("session has no job")
	}
	manager := plugin.managers[job.UID]
	if manager == nil {
		t.Fatal("task-topology manager was not initialized")
	}

	tasks := make([]*api.TaskInfo, 0, len(job.Tasks))
	for _, task := range job.Tasks {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].Name < tasks[j].Name })

	return session, manager, tasks, session.Nodes["n1"]
}

func TestStatementRollbackRestoresTaskTopologyState(t *testing.T) {
	t.Run("discard", func(t *testing.T) {
		session, manager, tasks, node := newRollbackSession(t, false)
		before := snapshotManager(manager)
		statement := framework.NewStatement(session)

		for _, task := range tasks {
			if err := statement.Allocate(task, node); err != nil {
				t.Fatalf("allocate %s: %v", task.Name, err)
			}
		}
		if got := manager.nodeTaskSet[node.Name]["worker"]; got != len(tasks) {
			t.Fatalf("bound worker count = %d, want %d", got, len(tasks))
		}

		statement.Discard()
		if after := snapshotManager(manager); !reflect.DeepEqual(before, after) {
			t.Fatalf("discard did not restore task-topology state\nbefore: %#v\nafter:  %#v", before, after)
		}

		statement.Discard()
		if after := snapshotManager(manager); !reflect.DeepEqual(before, after) {
			t.Fatalf("repeated discard changed task-topology state\nbefore: %#v\nafter:  %#v", before, after)
		}
	})

	t.Run("failed allocation", func(t *testing.T) {
		session, manager, tasks, _ := newRollbackSession(t, false)
		before := snapshotManager(manager)

		err := framework.NewStatement(session).Allocate(tasks[0], &api.NodeInfo{Name: "missing"})
		if err == nil {
			t.Fatal("allocate to missing node succeeded")
		}
		if after := snapshotManager(manager); !reflect.DeepEqual(before, after) {
			t.Fatalf("failed allocation did not restore task-topology state\nbefore: %#v\nafter:  %#v", before, after)
		}
	})

	t.Run("discard eviction", func(t *testing.T) {
		session, manager, tasks, _ := newRollbackSession(t, true)
		before := snapshotManager(manager)
		boundTask := tasks[0]
		if boundTask.NodeName == "" {
			boundTask = tasks[1]
		}

		statement := framework.NewStatement(session)
		statement.Evict(boundTask, "test")
		statement.Discard()

		if after := snapshotManager(manager); !reflect.DeepEqual(before, after) {
			t.Fatalf("eviction discard did not restore task-topology state\nbefore: %#v\nafter:  %#v", before, after)
		}
	})
}
