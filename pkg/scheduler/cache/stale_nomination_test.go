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

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	nodeshardv1alpha1 "volcano.sh/apis/pkg/apis/shard/v1alpha1"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

// recordingStatusUpdater keeps the last pod status the cache wrote.
type recordingStatusUpdater struct {
	updated *v1.Pod
}

func (r *recordingStatusUpdater) UpdatePodStatus(pod *v1.Pod) (*v1.Pod, error) {
	r.updated = pod
	return pod, nil
}

func (r *recordingStatusUpdater) UpdatePodGroup(pg *schedulingapi.PodGroup) (*schedulingapi.PodGroup, error) {
	return pg, nil
}

func (r *recordingStatusUpdater) UpdateQueueStatus(*schedulingapi.QueueInfo) error { return nil }

func (r *recordingStatusUpdater) UpdateNodeShardStatus(ns *nodeshardv1alpha1.NodeShard) (*nodeshardv1alpha1.NodeShard, error) {
	return ns, nil
}

// pendingPod is a pending pod already carrying the Unschedulable condition
// taskUnschedulable would write, so only the nomination can trigger an update.
func pendingPod(nominated string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "c1", Name: "worker-3", UID: "worker-3"},
		Status: v1.PodStatus{
			Phase:             v1.PodPending,
			NominatedNodeName: nominated,
			Conditions: []v1.PodCondition{{
				Type:    v1.PodScheduled,
				Status:  v1.ConditionFalse,
				Reason:  v1.PodReasonUnschedulable,
				Message: "gang unschedulable",
			}},
		},
	}
}

func TestTaskUnschedulableNomination(t *testing.T) {
	tests := []struct {
		name          string
		nominated     string
		staleFlag     bool
		newNomination string
		wantWrite     bool
		wantNominated string
	}{
		{
			name:          "stale nomination is cleared",
			nominated:     "reclaimed-node",
			staleFlag:     true,
			wantWrite:     true,
			wantNominated: "",
		},
		{
			name:          "live nomination is kept when nothing is nominated this session",
			nominated:     "n1",
			wantWrite:     false,
			wantNominated: "n1",
		},
		{
			name:          "a newer nomination replaces a stale one",
			nominated:     "reclaimed-node",
			staleFlag:     true,
			newNomination: "n2",
			wantWrite:     true,
			wantNominated: "n2",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			updater := &recordingStatusUpdater{}
			sc := &SchedulerCache{StatusUpdater: updater, Recorder: record.NewFakeRecorder(10)}
			task := schedulingapi.NewTaskInfo(pendingPod(tc.nominated))
			task.ClearStaleNomination = tc.staleFlag

			if err := sc.taskUnschedulable(task, v1.PodReasonUnschedulable, "gang unschedulable", tc.newNomination); err != nil {
				t.Fatalf("taskUnschedulable: %v", err)
			}
			if (updater.updated != nil) != tc.wantWrite {
				t.Fatalf("pod status written = %v, want %v", updater.updated != nil, tc.wantWrite)
			}
			if updater.updated != nil && updater.updated.Status.NominatedNodeName != tc.wantNominated {
				t.Fatalf("nominatedNodeName = %q, want %q", updater.updated.Status.NominatedNodeName, tc.wantNominated)
			}
			if task.Pod.Status.NominatedNodeName != tc.nominated {
				t.Fatalf("the cached pod was mutated: nominatedNodeName = %q", task.Pod.Status.NominatedNodeName)
			}
		})
	}
}
