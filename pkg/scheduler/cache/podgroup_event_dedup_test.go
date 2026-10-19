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
	"context"
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/apis/pkg/client/clientset/versioned/scheme"

	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
)

func TestPodGroupEventDeduper(t *testing.T) {
	const uid = types.UID("pg-uid")
	d := &podGroupEventDeduper{}

	seen := func(msg string) bool {
		return d.shouldRecord(uid, v1.EventTypeWarning, "Unschedulable", msg)
	}

	if !seen("a") {
		t.Fatal("first event should be recorded")
	}
	if seen("a") {
		t.Fatal("repeat within a session should be suppressed")
	}
	d.endSession(uid)

	if seen("a") {
		t.Fatal("repeat in the next session should be suppressed")
	}
	if !seen("b") {
		t.Fatal("a different message should be recorded")
	}
	d.endSession(uid)

	if seen("b") {
		t.Fatal("b should still be suppressed")
	}
	d.endSession(uid)

	// A session without a.
	if !seen("a") {
		t.Fatal("a should be recorded again after it stopped being reported")
	}
	d.endSession(uid)

	if !d.shouldRecord(uid, v1.EventTypeNormal, "Unschedulable", "a") {
		t.Fatal("a different event type should be recorded")
	}
	if !d.shouldRecord(uid, v1.EventTypeWarning, "Other", "a") {
		t.Fatal("a different reason should be recorded")
	}
	if !d.shouldRecord("other-uid", v1.EventTypeWarning, "Unschedulable", "a") {
		t.Fatal("another PodGroup should be recorded")
	}

	d.forget(uid)
	if !seen("a") {
		t.Fatal("event should be recorded after the PodGroup is forgotten")
	}

	d.endSession(uid)
	d.endSession(uid)
	d.endSession("other-uid")
	d.endSession("other-uid")
	if len(d.histories) != 0 {
		t.Fatalf("expected idle histories to be cleaned up, got %d", len(d.histories))
	}

	if !d.shouldRecord("", v1.EventTypeWarning, "Unschedulable", "a") || !d.shouldRecord("", v1.EventTypeWarning, "Unschedulable", "a") {
		t.Fatal("events without a PodGroup UID should never be suppressed")
	}
}

func TestRecordJobStatusEventDeduplicatesAcrossSessions(t *testing.T) {
	client := fake.NewSimpleClientset()
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&corev1.EventSinkImpl{Interface: client.CoreV1().Events("")})
	defer broadcaster.Shutdown()

	sc := &SchedulerCache{
		Recorder: broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "volcano"}),
	}

	job := schedulingapi.NewJobInfo("ns/pg")
	job.SetPodGroup(&schedulingapi.PodGroup{
		PodGroup: scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "ns", UID: "pg-uid"},
			Status:     scheduling.PodGroupStatus{Phase: scheduling.PodGroupPending},
		},
		Version: schedulingapi.PodGroupVersionV1Beta1,
	})

	const sessions = 5
	for i := 0; i < sessions; i++ {
		sc.RecordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupUnschedulableType), "queue resource quota insufficient")
		sc.RecordJobStatusEvent(job, false)
	}

	// The cache event and the plugin event are each written once.
	if err := waitForEventWrites(client, 2); err != nil {
		t.Fatal(err)
	}

	// The PodGroup becomes schedulable for a session, then fails again.
	job.PodGroup.Status.Phase = scheduling.PodGroupRunning
	sc.RecordJobStatusEvent(job, false)
	job.PodGroup.Status.Phase = scheduling.PodGroupPending
	sc.RecordJobStatusEvent(job, false)

	if err := waitForEventWrites(client, 3); err != nil {
		t.Fatal(err)
	}

	sc.DeletePodGroupV1beta1(&schedulingv1beta1.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: "pg", Namespace: "ns", UID: "pg-uid"},
	})
	if len(sc.podGroupEvents.histories) != 0 {
		t.Fatalf("expected history to be removed on PodGroup deletion, got %d entries", len(sc.podGroupEvents.histories))
	}
}

// waitForEventWrites waits until the fake client has received exactly want
// Event create or patch requests, and checks that no more arrive.
func waitForEventWrites(client *fake.Clientset, want int) error {
	count := func() int {
		n := 0
		for _, action := range client.Actions() {
			if action.GetResource().Resource == "events" && (action.GetVerb() == "create" || action.GetVerb() == "patch") {
				n++
			}
		}
		return n
	}

	err := wait.PollUntilContextTimeout(context.Background(), 10*time.Millisecond, 5*time.Second, true, func(_ context.Context) (bool, error) {
		return count() >= want, nil
	})
	if err != nil {
		return fmt.Errorf("expected %d event writes, got %d", want, count())
	}
	time.Sleep(200 * time.Millisecond)
	if got := count(); got != want {
		return fmt.Errorf("expected %d event writes, got %d", want, got)
	}
	return nil
}
