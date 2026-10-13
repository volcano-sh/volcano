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

package framework

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// TestTaskSchedulingReasonNominationLifecycle checks nomination publication
// across the real Pipeline/Discard/Commit paths.
func TestTaskSchedulingReasonNominationLifecycle(t *testing.T) {
	tests := []struct {
		name     string
		eviction bool
		discard  bool
	}{
		{name: "discarded preemption publishes no nomination", eviction: true, discard: true},
		{name: "committed preemption retains nomination", eviction: true},
		{name: "committed non-eviction pipeline publishes no nomination"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scherCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
			scherCache.AddOrUpdateNode(util.BuildNode("n1", api.BuildResourceList("4", "8Gi", api.ScalarResource{Name: "pods", Value: "10"}), nil))
			scherCache.AddPod(util.BuildPod("ns1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil))
			scherCache.AddPodGroupV1beta1(util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, schedulingv1.PodGroupInqueue))
			scherCache.AddQueueV1beta1(util.BuildQueue("q1", 1, nil))
			ssn := OpenSession(scherCache, nil, nil)
			defer CloseSession(ssn)

			var job *api.JobInfo
			var task *api.TaskInfo
			for _, j := range ssn.Jobs {
				job = j
				for _, tk := range j.Tasks {
					task = tk
				}
			}
			if task == nil {
				t.Fatal("no task found in session")
			}
			stmt := NewStatement(ssn)
			if err := stmt.Pipeline(task, "n1", tt.eviction); err != nil {
				t.Fatalf("Pipeline failed: %v", err)
			}
			wantNomination := ""
			if tt.eviction {
				wantNomination = "n1"
			}
			if _, _, nominated := job.TaskSchedulingReason(task.UID); nominated != wantNomination {
				t.Fatalf("pre-completion nomination = %q, want %q", nominated, wantNomination)
			}

			if tt.discard {
				stmt.Discard()
				wantNomination = ""
				if task.Status != api.Pending || task.NodeName != "" || task.EvictionOccurred {
					t.Errorf("discard did not restore live task context: %+v", task.TransactionContext)
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
			if _, _, nominated := job.TaskSchedulingReason(task.UID); nominated != wantNomination {
				t.Errorf("post-completion nomination = %q, want %q", nominated, wantNomination)
			}
		})
	}
}
