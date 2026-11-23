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
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type publisherCapturedUpdater struct {
	util.FakeStatusUpdater
	pod *v1.Pod
}

func (u *publisherCapturedUpdater) UpdatePodStatus(pod *v1.Pod) (*v1.Pod, error) {
	u.pod = pod
	return pod, nil
}

func TestPodStatusPublisherSynchronousNominationSupersedesQueue(t *testing.T) {
	sc, task, client := publisherFixture()
	p := sc.podStatusPublisher
	defer p.queue.ShutDown()
	p.enqueue(task, api.PodReasonUnschedulable, "older session")
	latest := task.Pod.DeepCopy()
	latest.ResourceVersion = "rv-after-background-write"
	_, _ = client.CoreV1().Pods("ns").Update(context.Background(), latest, metav1.UpdateOptions{})
	updater := &publisherCapturedUpdater{}
	sc.StatusUpdater = updater
	if err := sc.taskUnschedulable(task, api.PodReasonUnschedulable, "preemption", "node-1"); err != nil {
		t.Fatal(err)
	}
	if updater.pod == nil || updater.pod.Status.NominatedNodeName != "node-1" || updater.pod.ResourceVersion != latest.ResourceVersion {
		t.Fatal("nomination was deferred or used a stale version")
	}
	if sc.Jobs[task.Job].Tasks[task.UID].Pod != updater.pod || !p.pending[task.Pod.UID].canceled {
		t.Fatal("nomination cache writeback or cancellation failed")
	}
	p.processNext(context.Background())
	if len(p.pending) != 0 || p.bytes != 0 {
		t.Fatal("canceled queue slot was not released")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "update" && action.GetSubresource() == "status" {
			t.Fatal("older queued status was sent after nomination")
		}
	}
}

func TestPodStatusPublisherCancellationRetainsCapacity(t *testing.T) {
	sc, task, _ := publisherFixture()
	p := sc.podStatusPublisher
	defer p.queue.ShutDown()
	p.limit = 1
	if !p.enqueue(task, api.PodReasonUnschedulable, "old") {
		t.Fatal("enqueue failed")
	}
	p.cancel(task.Pod.UID)
	other := api.NewTaskInfo(task.Pod.DeepCopy())
	other.Pod.UID = "other-uid"
	if p.enqueue(other, api.PodReasonUnschedulable, "new") {
		t.Fatal("canceled key bypassed queue capacity")
	}
	p.processNext(context.Background())
	if !p.enqueue(other, api.PodReasonUnschedulable, "new") {
		t.Fatal("consumed canceled slot did not restore capacity")
	}
}

func TestPodStatusPublisherRetryRetainsCapacity(t *testing.T) {
	sc, task, client := publisherFixture()
	p := sc.podStatusPublisher
	defer p.queue.ShutDown()
	p.limit = 1
	client.PrependReactor("update", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() == "status" {
			return true, nil, apierrors.NewServiceUnavailable("temporary outage")
		}
		return false, nil, nil
	})
	p.enqueue(task, api.PodReasonUnschedulable, "retry")
	p.processNext(context.Background())
	if p.queue.NumRequeues(task.Pod.UID) != 1 || len(p.pending) != 1 {
		t.Fatal("retry lost its capacity slot")
	}
	p.cancel(task.Pod.UID)
	otherPod := task.Pod.DeepCopy()
	otherPod.UID = "other-uid"
	other := api.NewTaskInfo(otherPod)
	if p.enqueue(other, api.PodReasonUnschedulable, "other") {
		t.Fatal("delayed canceled retry bypassed the bound")
	}
	p.processNext(context.Background())
	if p.queue.Len() != 0 || len(p.pending) != 0 || p.bytes != 0 {
		t.Fatal("retry timer or canceled payload survived consumption")
	}
}

func TestPodStatusPublisherStopsAfterRetryLimit(t *testing.T) {
	sc, task, client := publisherFixture()
	p := sc.podStatusPublisher
	defer p.queue.ShutDown()
	attempts := 0
	client.PrependReactor("update", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() == "status" {
			attempts++
			return true, nil, apierrors.NewServiceUnavailable("temporary outage")
		}
		return false, nil, nil
	})
	if !p.enqueue(task, api.PodReasonUnschedulable, "retry") {
		t.Fatal("enqueue failed")
	}
	for attempt := 0; attempt < 6; attempt++ {
		p.processNext(context.Background())
		if attempt < 5 {
			if !p.enqueue(task, api.PodReasonUnschedulable, "retry") {
				t.Fatal("duplicate intent was rejected")
			}
			if p.queue.NumRequeues(task.Pod.UID) != attempt+1 {
				t.Fatal("duplicate intent reset the retry count")
			}
		}
	}
	if attempts != 6 || len(p.pending) != 0 || p.bytes != 0 || p.queue.Len() != 0 || p.queue.NumRequeues(task.Pod.UID) != 0 {
		t.Fatalf("retry limit did not release its state: attempts=%d, pending=%d, bytes=%d, queued=%d", attempts, len(p.pending), p.bytes, p.queue.Len())
	}
}

func TestPodStatusPublisherRefreshesUnrelatedStatus(t *testing.T) {
	sc, task, client := publisherFixture()
	p := sc.podStatusPublisher
	defer p.queue.ShutDown()
	latest := task.Pod.DeepCopy()
	latest.ResourceVersion = "new-version"
	latest.Status.Conditions[0].Status = v1.ConditionTrue
	_, _ = client.CoreV1().Pods("ns").Update(context.Background(), latest, metav1.UpdateOptions{})
	p.enqueue(task, api.PodReasonUnschedulable, "current decision")
	p.processNext(context.Background())
	p.processNext(context.Background()) // rejected stale version refreshes from API
	pod, _ := client.CoreV1().Pods("ns").Get(context.Background(), "pod", metav1.GetOptions{})
	if !strings.HasPrefix(pod.ResourceVersion, "new-version") || pod.Status.Conditions[0].Status != v1.ConditionTrue || scheduledCondition(pod).Message != "current decision" {
		t.Fatal("publication used the stale snapshot or changed unrelated status")
	}
}

func TestPodStatusPublisherFullQueueUsesSynchronousPath(t *testing.T) {
	sc, task, _ := publisherFixture()
	p := sc.podStatusPublisher
	defer p.queue.ShutDown()
	p.limit = 0
	updater := &publisherCapturedUpdater{}
	sc.StatusUpdater = updater
	sc.RecordJobStatusEvent(sc.Jobs[task.Job], false)
	if updater.pod == nil || scheduledCondition(updater.pod) == nil {
		t.Fatal("queue saturation silently lost status")
	}
}

func TestPodStatusPublisherSynchronousPathPreservesNewGate(t *testing.T) {
	sc, task, client := publisherFixture()
	defer sc.podStatusPublisher.queue.ShutDown()
	latest := task.Pod.DeepCopy()
	latest.Spec.SchedulingGates = []v1.PodSchedulingGate{{Name: "new-admission-gate"}}
	_, _ = client.CoreV1().Pods("ns").Update(context.Background(), latest, metav1.UpdateOptions{})
	updater := &publisherCapturedUpdater{}
	sc.StatusUpdater = updater
	if err := sc.taskUnschedulable(task, api.PodReasonSchedulerError, "old failure", ""); err != nil {
		t.Fatal(err)
	}
	if updater.pod != nil {
		t.Fatal("synchronous refresh overwrote the new scheduling gate")
	}
}

func publisherFixture() (*SchedulerCache, *api.TaskInfo, *fake.Clientset) {
	pod := &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "pod", UID: "uid-1", ResourceVersion: "rv-a"}, Status: v1.PodStatus{Phase: v1.PodPending, Conditions: []v1.PodCondition{{Type: v1.PodReady, Status: v1.ConditionFalse, Reason: "ContainersNotReady"}}}}
	task := api.NewTaskInfo(pod)
	job := api.NewJobInfo(task.Job, task)
	client := fake.NewSimpleClientset(pod.DeepCopy())
	// The default fake tracker accepts stale versions. Emulate the API's UID
	// and resourceVersion preconditions so optimistic writes cannot pass tests
	// by overwriting a newer bind, condition, or same-name replacement.
	client.PrependReactor("update", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "status" {
			return false, nil, nil
		}
		requested := a.(clienttesting.UpdateAction).GetObject().(*v1.Pod).DeepCopy()
		stored, err := client.Tracker().Get(v1.SchemeGroupVersion.WithResource("pods"), requested.Namespace, requested.Name)
		if err != nil {
			return true, nil, err
		}
		current := stored.(*v1.Pod)
		if current.UID != requested.UID || current.ResourceVersion != requested.ResourceVersion {
			return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, requested.Name, fmt.Errorf("object has been modified"))
		}
		requested.ResourceVersion += "+"
		err = client.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), requested, requested.Namespace)
		return true, requested, err
	})
	sc := &SchedulerCache{kubeClient: client, Jobs: map[api.JobID]*api.JobInfo{task.Job: job}, Recorder: record.NewFakeRecorder(100)}
	sc.podStatusPublisher = newPodStatusPublisher(sc, 100)
	return sc, task, client
}

func TestPodStatusPublisherCoalescesAndBounds(t *testing.T) {
	sc, task, client := publisherFixture()
	p := sc.podStatusPublisher
	defer p.queue.ShutDown()
	for _, message := range []string{"old", "middle", "latest"} {
		if !p.enqueue(task, api.PodReasonUnschedulable, message) {
			t.Fatal("enqueue failed")
		}
	}
	if p.queue.Len() != 1 || len(p.pending) != 1 {
		t.Fatal("one Pod retained more than one queue item")
	}
	p.processNext(context.Background())
	for _, action := range client.Actions() {
		if action.GetVerb() == "get" {
			t.Fatal("uncontended publication added an API GET")
		}
	}
	pod, _ := client.CoreV1().Pods("ns").Get(context.Background(), "pod", metav1.GetOptions{})
	if c := scheduledCondition(pod); c == nil || c.Message != "latest" {
		t.Fatalf("wrong condition: %+v", c)
	}
	if len(pod.Status.Conditions) != 2 || pod.Status.Conditions[0] != task.Pod.Status.Conditions[0] {
		t.Fatal("unrelated condition changed")
	}
	if len(task.Pod.Status.Conditions) != 1 || len(p.pending) != 0 || p.bytes != 0 {
		t.Fatal("snapshot mutated or completed intent retained")
	}
	p.limit = 0
	if p.enqueue(task, api.PodReasonUnschedulable, "full") {
		t.Fatal("capacity limit ignored")
	}
	p.limit = 10
	p.byteLimit = 1
	if p.enqueue(task, api.PodReasonUnschedulable, "bytes") {
		t.Fatal("byte limit ignored")
	}
}

func TestPodStatusPublisherRateWaitDoesNotBlockNomination(t *testing.T) {
	sc, task, _ := publisherFixture()
	p := sc.podStatusPublisher
	defer p.queue.ShutDown()
	p.budget = rate.NewLimiter(1, 1)
	p.budget.Allow() // consume the initial token, forcing the worker to wait
	p.enqueue(task, api.PodReasonUnschedulable, "old observation")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { p.processNext(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	deadline := time.After(2 * time.Second)
	for p.budget.Tokens() >= -.5 {
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatal("worker did not reserve its rate token")
		}
	}
	updater := &publisherCapturedUpdater{}
	sc.StatusUpdater = updater
	returned := make(chan error, 1)
	go func() { returned <- sc.taskUnschedulable(task, api.PodReasonUnschedulable, "preemption", "node-1") }()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("rate budget blocked synchronous nomination")
	}
	if updater.pod == nil || updater.pod.Status.NominatedNodeName != "node-1" {
		t.Fatal("nomination was deferred")
	}
}

func TestPodStatusPublisherSkipsObsoleteDecisions(t *testing.T) {
	for _, kind := range []string{"bound", "deleted", "recreated", "nominated", "gated", "terminal", "newer-condition", "cache-binding"} {
		t.Run(kind, func(t *testing.T) {
			sc, task, client := publisherFixture()
			p := sc.podStatusPublisher
			defer p.queue.ShutDown()
			p.enqueue(task, api.PodReasonUnschedulable, "stale")
			pod := task.Pod.DeepCopy()
			pod.ResourceVersion = "rv-after-change"
			switch kind {
			case "bound":
				pod.Spec.NodeName = "node"
			case "recreated":
				pod.UID = "replacement"
			case "nominated":
				pod.Status.NominatedNodeName = "preempted-node"
			case "gated":
				pod.Spec.SchedulingGates = []v1.PodSchedulingGate{{Name: "admission"}}
			case "terminal":
				pod.Status.Phase = v1.PodSucceeded
			case "newer-condition":
				pod.Status.Conditions = append(pod.Status.Conditions, v1.PodCondition{Type: v1.PodScheduled, Status: v1.ConditionFalse, Reason: api.PodReasonSchedulerError, Message: "newer failure"})
			case "cache-binding":
				task.Status = api.Binding
			}
			if kind == "deleted" {
				_ = client.CoreV1().Pods("ns").Delete(context.Background(), "pod", metav1.DeleteOptions{})
			} else {
				_, _ = client.CoreV1().Pods("ns").Update(context.Background(), pod, metav1.UpdateOptions{})
			}
			updates := 0
			client.PrependReactor("update", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
				if a.GetSubresource() == "status" {
					requested := a.(clienttesting.UpdateAction).GetObject().(*v1.Pod)
					stored, err := client.Tracker().Get(v1.SchemeGroupVersion.WithResource("pods"), requested.Namespace, requested.Name)
					if err == nil && stored.(*v1.Pod).UID == requested.UID && stored.(*v1.Pod).ResourceVersion == requested.ResourceVersion {
						updates++
					}
				}
				return false, nil, nil
			})
			p.processNext(context.Background())
			if len(p.pending) != 0 {
				p.processNext(context.Background())
			}
			if updates != 0 {
				t.Fatal("obsolete status was accepted by UpdateStatus")
			}
		})
	}
}

func TestPodStatusPublisherBindingConflictRefreshes(t *testing.T) {
	sc, task, client := publisherFixture()
	p := sc.podStatusPublisher
	defer p.queue.ShutDown()
	p.enqueue(task, api.PodReasonUnschedulable, "before bind")
	updates := 0
	client.PrependReactor("update", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "status" {
			return false, nil, nil
		}
		updates++
		pod := task.Pod.DeepCopy()
		pod.Spec.NodeName = "bound-after-get"
		pod.ResourceVersion = "rv-after-bind"
		_ = client.Tracker().Update(v1.SchemeGroupVersion.WithResource("pods"), pod, "ns")
		return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, "pod", fmt.Errorf("bound concurrently"))
	})
	p.processNext(context.Background())
	p.processNext(context.Background()) // retries GET; the bound Pod must be skipped
	if updates != 1 || len(p.pending) != 0 {
		t.Fatal("retry overwrote a bound Pod or retained its intent")
	}
}

func TestPodStatusPublisherCloseDoesNotWaitForAPI(t *testing.T) {
	sc, task, client := publisherFixture()
	p := sc.podStatusPublisher
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	client.PrependReactor("update", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() == "status" {
			once.Do(func() { close(entered) })
			<-release
		}
		return false, nil, nil
	})
	stop, done := make(chan struct{}), make(chan struct{})
	go func() { p.run(stop); close(done) }()
	defer func() { close(release); close(stop); <-done }()
	job := sc.Jobs[task.Job]
	sc.RecordJobStatusEvent(job, false)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reach API")
	}
	returned := make(chan struct{})
	go func() { sc.RecordJobStatusEvent(job, false); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("session publication waited for blocked API")
	}
}

type blockingStatusTransport struct{ entered chan struct{} }

func (b blockingStatusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	close(b.entered)
	<-r.Context().Done()
	return nil, r.Context().Err()
}

func TestPodStatusPublisherShutdownCancelsHTTP(t *testing.T) {
	sc, task, _ := publisherFixture()
	entered := make(chan struct{})
	client, err := kubernetes.NewForConfig(&rest.Config{Host: "http://status-publisher.test", Transport: blockingStatusTransport{entered: entered}})
	if err != nil {
		t.Fatal(err)
	}
	sc.kubeClient = client
	p := sc.podStatusPublisher
	p.enqueue(task, api.PodReasonUnschedulable, "cancel me")
	stop, done := make(chan struct{}), make(chan struct{})
	go func() { p.run(stop); close(done) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("status request did not start")
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not cancel HTTP")
	}
	if len(p.pending) != 0 || p.bytes != 0 {
		t.Fatal("shutdown retained intents")
	}
}

func TestPodStatusPublisherCoalescesDuringWrite(t *testing.T) {
	sc, task, client := publisherFixture()
	p := sc.podStatusPublisher
	defer p.queue.ShutDown()
	p.enqueue(task, api.PodReasonUnschedulable, "first")
	writes := 0
	client.PrependReactor("update", "pods", func(a clienttesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "status" {
			return false, nil, nil
		}
		writes++
		if writes == 1 {
			p.enqueue(task, api.PodReasonUnschedulable, "latest during write")
		}
		return false, nil, nil
	})
	p.processNext(context.Background())
	p.processNext(context.Background())
	pod, _ := client.CoreV1().Pods("ns").Get(context.Background(), "pod", metav1.GetOptions{})
	if writes != 2 || !strings.Contains(scheduledCondition(pod).Message, "latest") || len(p.pending) != 0 {
		t.Fatal("coalesced update was lost")
	}
}
