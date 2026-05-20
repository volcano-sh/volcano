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

package job

import (
	"errors"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	bus "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/apis"
	"volcano.sh/volcano/pkg/controllers/job/state"
)

type fakeState struct {
	lastAction state.Action
	executeErr error
}

func (f *fakeState) Execute(act state.Action) error {
	f.lastAction = act
	return f.executeErr
}

func TestPushRequest_NoDedup(t *testing.T) {
	controller := newController()

	req := apis.Request{
		Namespace: "default",
		JobName:   "job-a",
		Event:     bus.OutOfSyncEvent,
	}
	key := "default/job-a"

	controller.pushRequest(req)
	controller.pushRequest(req)

	if got := len(controller.pendingRequests[key]); got != 2 {
		t.Fatalf("expected pending len 2, got %d", got)
	}
}

func TestRequeueToHead_KeepsHeadBeforeNewTail(t *testing.T) {
	controller := newController()
	key := "default/job-a"

	r2 := apis.Request{Namespace: "default", JobName: "job-a", PodName: "pod-2"}
	r3 := apis.Request{Namespace: "default", JobName: "job-a", PodName: "pod-3"}
	r4 := apis.Request{Namespace: "default", JobName: "job-a", PodName: "pod-4"}

	controller.pushRequest(r4)
	controller.requeueToHead(key, []apis.Request{r2, r3})

	got := controller.pendingRequests[key]
	if len(got) != 3 {
		t.Fatalf("expected 3 pending requests, got %d", len(got))
	}
	if got[0].PodName != "pod-2" || got[1].PodName != "pod-3" || got[2].PodName != "pod-4" {
		t.Fatalf("unexpected pending order: %+v", got)
	}
}

func TestHandleJobError_RetryKeySeparatesTaskAndPartition(t *testing.T) {
	controller := newController()
	controller.maxRequeueNum = 1
	st := &fakeState{}
	jobKey := "default/job-a"

	err1 := controller.handleJobError(jobKey, apis.Request{
		Namespace:   "default",
		JobName:     "job-a",
		PodName:     "pod-a",
		TaskName:    "task-a",
		PartitionID: "p0",
		Event:       bus.PodPendingEvent,
	}, st, errors.New("boom"), bus.RestartJobAction)

	err2 := controller.handleJobError(jobKey, apis.Request{
		Namespace:   "default",
		JobName:     "job-a",
		PodName:     "pod-a",
		TaskName:    "task-a",
		PartitionID: "p1",
		Event:       bus.PodPendingEvent,
	}, st, errors.New("boom"), bus.RestartJobAction)

	if !err1 || !err2 {
		t.Fatalf("expected both requests to keep retry budget independently")
	}
}

func TestHandleJobError_OverBudgetTerminatesWholeJob(t *testing.T) {
	controller := newController()
	controller.maxRequeueNum = 0
	st := &fakeState{}
	jobKey := "default/job-a"

	shouldRetry := controller.handleJobError(jobKey, apis.Request{
		Namespace: "default",
		JobName:   "job-a",
		Event:     bus.PodFailedEvent,
	}, st, errors.New("boom"), bus.RestartJobAction)

	if shouldRetry {
		t.Fatalf("expected no retry when maxRequeueNum is exceeded")
	}
	if st.lastAction.Action != bus.TerminateJobAction {
		t.Fatalf("expected terminate action, got %s", st.lastAction.Action)
	}
}

func TestDeleteJob_ClearsPendingAndRetryState(t *testing.T) {
	controller := newController()
	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "job-a",
		},
	}
	req := apis.Request{
		Namespace: "default",
		JobName:   "job-a",
		PodName:   "pod-a",
		TaskName:  "task-a",
		Event:     bus.PodFailedEvent,
	}
	key := "default/job-a"

	controller.addJob(job)
	controller.pushRequest(req)
	controller.requestRetryCounter[controller.retryKeyByRequest(key, req, bus.RestartJobAction)] = 1

	controller.deleteJob(job)

	if _, found := controller.pendingRequests[key]; found {
		t.Fatalf("expected pending requests to be cleared for %s", key)
	}
	for retryKey := range controller.requestRetryCounter {
		if retryKey.jobKey == key {
			t.Fatalf("expected retry counter entries to be cleared for %s", key)
		}
	}
}

func TestProcessNextJob_CacheMissClearsPendingAndRetryState(t *testing.T) {
	controller := newController()
	key := "default/job-missing"
	req := apis.Request{
		Namespace: "default",
		JobName:   "job-missing",
		PodName:   "pod-a",
		TaskName:  "task-a",
		Event:     bus.PodFailedEvent,
	}

	controller.pendingRequests[key] = []apis.Request{req}
	controller.requestRetryCounter[controller.retryKeyByRequest(key, req, bus.RestartJobAction)] = 1
	controller.queue.Add(key)

	if !controller.processNextJob() {
		t.Fatalf("expected processNextJob to keep worker loop running")
	}
	if _, found := controller.pendingRequests[key]; found {
		t.Fatalf("expected pending requests to be cleared for cache miss key %s", key)
	}
	for retryKey := range controller.requestRetryCounter {
		if retryKey.jobKey == key {
			t.Fatalf("expected retry counter entries to be cleared for cache miss key %s", key)
		}
	}
}

func TestAddDelayActionForJob_ReplayRequestWithMaterializedAction(t *testing.T) {
	controller := newController()
	req := apis.Request{
		Namespace:   "default",
		JobName:     "job-a",
		TaskName:    "task-a",
		PodName:     "pod-a",
		PartitionID: "p0",
		Event:       bus.PodPendingEvent,
	}
	key := "default/job-a"

	controller.AddDelayActionForJob(req, &delayAction{
		jobKey:    key,
		taskName:  req.TaskName,
		podName:   req.PodName,
		partition: req.PartitionID,
		event:     req.Event,
		action:    bus.RestartTaskAction,
		delay:     10 * time.Millisecond,
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		controller.pendingMu.Lock()
		got := append([]apis.Request(nil), controller.pendingRequests[key]...)
		controller.pendingMu.Unlock()
		if len(got) > 0 {
			replayed := got[len(got)-1]
			if replayed.Action != bus.RestartTaskAction {
				t.Fatalf("expected replayed action %s, got %s", bus.RestartTaskAction, replayed.Action)
			}
			if replayed.TaskName != req.TaskName || replayed.PodName != req.PodName || replayed.PartitionID != req.PartitionID {
				t.Fatalf("expected replayed request to preserve task/pod/partition, got %+v", replayed)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for replayed delayed action")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestProcessNextJob_CreatesStatePerRequest(t *testing.T) {
	controller := newController()
	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "job-a",
		},
	}

	if err := controller.cache.Add(job); err != nil {
		t.Fatalf("failed to add job into cache: %v", err)
	}

	controller.pushRequest(apis.Request{
		Namespace: "default",
		JobName:   "job-a",
		PodName:   "pod-a",
		Action:    bus.SyncJobAction,
	})
	controller.pushRequest(apis.Request{
		Namespace: "default",
		JobName:   "job-a",
		PodName:   "pod-b",
		Action:    bus.SyncJobAction,
	})

	newStateCount := 0
	patches := gomonkey.ApplyFunc(state.NewState, func(*apis.JobInfo) state.State {
		newStateCount++
		return &fakeState{}
	})
	defer patches.Reset()

	if !controller.processNextJob() {
		t.Fatalf("expected processNextJob to keep worker loop running")
	}
	if newStateCount != 2 {
		t.Fatalf("expected NewState to be called per request, got %d", newStateCount)
	}
}
