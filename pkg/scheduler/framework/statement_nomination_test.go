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

package framework

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestTaskSchedulingReasonNominationLifecycle(t *testing.T) {
	tests := []struct {
		name             string
		evictionOccurred bool
		discard          bool
		wantNomination   string
	}{
		{name: "discarded preemption publishes no nomination", evictionOccurred: true, discard: true},
		{name: "committed preemption retains nomination", evictionOccurred: true, wantNomination: "n1"},
		{name: "committed non-eviction pipeline publishes no nomination"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedulerCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
			schedulerCache.AddOrUpdateNode(util.BuildNode("n1", api.BuildResourceList("4", "8Gi", api.ScalarResource{Name: "pods", Value: "10"}), nil))
			schedulerCache.AddPod(util.BuildPod("ns1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil))
			schedulerCache.AddPodGroupV1beta1(util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1.PodGroupInqueue))
			schedulerCache.AddQueueV1beta1(util.BuildQueue("q1", 1, nil))

			ssn := OpenSession(schedulerCache, nil, nil)
			defer CloseSession(ssn)

			var job *api.JobInfo
			var task *api.TaskInfo
			for _, j := range ssn.Jobs {
				job = j
				for _, tk := range j.Tasks {
					task = tk
					break
				}
				break
			}
			if task == nil {
				t.Fatal("no task found in session")
			}
			stmt := NewStatement(ssn)
			if err := stmt.Pipeline(task, "n1", tt.evictionOccurred); err != nil {
				t.Fatalf("Pipeline failed: %v", err)
			}
			if tt.evictionOccurred {
				if _, _, nominated := job.TaskSchedulingReason(task.UID); nominated != "n1" {
					t.Fatalf("pre-discard/commit nomination = %q, want n1", nominated)
				}
			}

			if tt.discard {
				stmt.Discard()
				if task.Status != api.Pending || task.NodeName != "" || task.EvictionOccurred {
					t.Errorf("discard did not restore live task: status=%v node=%q eviction=%v", task.Status, task.NodeName, task.EvictionOccurred)
				}
				if task.LastTransaction == nil || !task.LastTransaction.EvictionOccurred {
					t.Error("expected LastTransaction to retain the eviction snapshot")
				}
			} else {
				stmt.Commit()
				if task.LastTransaction != nil {
					t.Error("expected Commit to clear LastTransaction")
				}
			}
			if _, _, nominated := job.TaskSchedulingReason(task.UID); nominated != tt.wantNomination {
				t.Errorf("nominatedNodeName = %q, want %q", nominated, tt.wantNomination)
			}
		})
	}
}
