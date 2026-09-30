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
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"golang.org/x/time/rate"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	bus "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	vcfake "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	"volcano.sh/volcano/pkg/controllers/apis"
	jobcache "volcano.sh/volcano/pkg/controllers/cache"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
)

func TestPodUIDUpdatePreservesPolicies(t *testing.T) {
	for _, tc := range []struct {
		name            string
		oldPhase, phase v1.PodPhase
		event           bus.Event
		outOfSync       bool
	}{
		{"failed", v1.PodRunning, v1.PodFailed, bus.PodFailedEvent, false},
		{"failed-to-failed", v1.PodFailed, v1.PodFailed, bus.PodFailedEvent, false},
		{"completed", v1.PodSucceeded, v1.PodSucceeded, bus.TaskCompletedEvent, false},
		{"running", v1.PodRunning, v1.PodRunning, bus.PodRunningEvent, false},
		{"pending", v1.PodPending, v1.PodPending, bus.PodPendingEvent, false},
		{"out-of-sync", v1.PodRunning, v1.PodFailed, bus.OutOfSyncEvent, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, q := hotfixController(t)
			job := hotfixJob("current")
			exitCode := int32(42)
			job.Spec.Policies = []batch.LifecyclePolicy{{ExitCode: &exitCode, Action: bus.RestartJobAction}}
			c.addJob(job)
			item, _ := q.Get()
			q.Done(item)
			old, current := hotfixPod(job, "old"), hotfixPod(job, "current")
			old.Status.Phase, current.Status.Phase = tc.oldPhase, tc.phase
			// Equal resource versions must not hide a change of identity.
			current.ResourceVersion = old.ResourceVersion
			current.Status.ContainerStatuses = []v1.ContainerStatus{{State: v1.ContainerState{Terminated: &v1.ContainerStateTerminated{ExitCode: exitCode}}}}
			if tc.outOfSync {
				current.Annotations[jobhelpers.OutOfSyncKey] = "true"
			}
			if err := c.cache.AddPod(old); err != nil {
				t.Fatal(err)
			}
			c.updatePod(old, current)
			if !c.cache.HasPod(current) || c.cache.HasPod(old) {
				t.Fatal("replacement cache is incorrect")
			}
			found := false
			for q.Len() > 0 {
				item, _ := q.Get()
				q.Done(item)
				req := item.(apis.Request)
				if req.PodUID == old.UID {
					continue
				}
				found = true
				if req.Event != tc.event {
					t.Fatalf("event=%s, want %s", req.Event, tc.event)
				}
				if tc.event == bus.PodFailedEvent && (req.ExitCode != exitCode || applyPolicies(job, &req).action != bus.RestartJobAction) {
					t.Fatalf("failure policy lost: %+v", req)
				}
				if tc.outOfSync && applyPolicies(job, &req).action != bus.SyncJobAction {
					t.Fatal("out-of-sync Pod triggered a policy")
				}
			}
			if !found {
				t.Fatal("replacement event lost")
			}
		})
	}
}

func TestPodSyncRequestsCoalesceBeforeRetry(t *testing.T) {
	c, q := hotfixController(t)
	job := hotfixJob("current")
	job.Spec.Tasks[0].Replicas = 11
	setupHotfixJob(t, c, job)
	old := hotfixPod(hotfixJob("previous"), "old")
	if _, err := c.kubeClient.CoreV1().Pods(job.Namespace).Create(context.TODO(), old, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 10; i++ {
		pod := hotfixPod(job, types.UID(fmt.Sprintf("pod-%d", i)))
		pod.Name = fmt.Sprintf("job-task-%d", i)
		if err := c.cache.AddPod(pod); err != nil {
			t.Fatal(err)
		}
		if _, err := c.kubeClient.CoreV1().Pods(job.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		current := pod.DeepCopy()
		current.ResourceVersion = "2"
		c.updatePod(pod, current)
	}
	if q.Len() != 1 {
		t.Fatalf("ordinary updates produced %d sync requests, want 1", q.Len())
	}
	c.kubeClient.(*kubefake.Clientset).ClearActions()
	c.maxRequeueNum = 1
	failed := false
	c.kubeClient.(*kubefake.Clientset).PrependReactor("create", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		if !failed {
			failed = true
			return true, nil, errors.New("temporary create failure")
		}
		return false, nil, nil
	})
	c.processNextReq(0)
	if q.NumRequeues(jobSyncRequest(job)) != 1 {
		t.Fatal("real error did not consume a retry")
	}
	// The rate-limited request keeps the same key when it encounters name wait.
	c.processNextReq(0)
	if len(q.waits) != 1 || q.waits[0] != jobSyncRequest(job) {
		t.Fatalf("name wait was not coalesced: %+v", q.waits)
	}
	creates := 0
	for _, action := range c.kubeClient.(*kubefake.Clientset).Actions() {
		if action.Matches("create", "pods") {
			creates++
		}
	}
	if creates != 2 || q.NumRequeues(jobSyncRequest(job)) != 1 {
		t.Fatalf("create attempts=%d, failure count=%d", creates, q.NumRequeues(jobSyncRequest(job)))
	}
	if err := c.kubeClient.CoreV1().Pods(job.Namespace).Delete(context.TODO(), old.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	q.Add(q.waits[0])
	c.processNextReq(0)
	if q.NumRequeues(jobSyncRequest(job)) != 0 {
		t.Fatal("successful recovery did not reset retries")
	}
	pod, err := c.kubeClient.CoreV1().Pods(job.Namespace).Get(context.TODO(), old.Name, metav1.GetOptions{})
	if err != nil || !metav1.IsControlledBy(pod, job) {
		t.Fatalf("replacement not created: %v", err)
	}
}

func TestAbsentJobRequestsDrainThroughWorker(t *testing.T) {
	c, q := hotfixController(t)
	current := hotfixJob("current")
	setupHotfixJob(t, c, current)
	small := hotfixJob("small")
	small.Name = "small"
	setupHotfixJob(t, c, small)
	// Model requests left after the old UID's cache entry was reclaimed.
	// Disable pacing to measure total API reads, not elapsed time.
	c.identityLimiter = rate.NewLimiter(rate.Inf, 1)
	for i := 0; i < 5000; i++ {
		q.Add(apis.Request{Namespace: current.Namespace, JobName: current.Name, JobUid: "reclaimed",
			PodName: fmt.Sprintf("job-task-%d", i), PodUID: types.UID(fmt.Sprintf("old-%d", i)),
			TaskName: "task", Event: bus.PodFailedEvent, ExitCode: 42})
	}
	q.Add(jobSyncRequest(small))
	for q.Len() > 0 {
		c.processNextReq(0)
	}
	gets := 0
	for _, action := range c.vcClient.(*vcfake.Clientset).Actions() {
		if action.Matches("get", "jobs") {
			gets++
		}
	}
	if gets != 1 || len(q.waits) != 0 {
		t.Fatalf("old requests: %d Job GETs, %d delayed requests", gets, len(q.waits))
	}
	pod, err := c.kubeClient.CoreV1().Pods(small.Namespace).Get(context.TODO(), "small-task-0", metav1.GetOptions{})
	if err != nil || !metav1.IsControlledBy(pod, small) {
		t.Fatalf("small Job did not reconcile after burst: %v", err)
	}
}

func TestOrdinaryPodEventsKeepPolicySemantics(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		add, changed, sameVersion bool
		want                      bus.Event
	}{
		{"initial-add", true, false, false, bus.PodPendingEvent},
		{"failure-transition", false, true, false, bus.PodFailedEvent},
		{"unchanged-failure", false, false, false, bus.OutOfSyncEvent},
		{"same-resource-version", false, true, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, q := hotfixController(t)
			job := hotfixJob("job")
			if err := c.cache.Add(job); err != nil {
				t.Fatal(err)
			}
			old := hotfixPod(job, "pod")
			old.Status.Phase = v1.PodFailed
			if tc.changed {
				old.Status.Phase = v1.PodRunning
			}
			current := old.DeepCopy()
			current.Status.Phase = v1.PodFailed
			if !tc.sameVersion {
				current.ResourceVersion = "2"
			}
			if tc.add {
				c.addPod(current)
			} else {
				if err := c.cache.AddPod(old); err != nil {
					t.Fatal(err)
				}
				c.updatePod(old, current)
			}
			if tc.want == "" {
				if q.Len() != 0 {
					t.Fatal("unchanged resource version produced an event")
				}
				return
			}
			if q.Len() != 1 {
				t.Fatalf("got %d events", q.Len())
			}
			item, _ := q.Get()
			q.Done(item)
			if item.(apis.Request).Event != tc.want {
				t.Fatalf("event=%s, want %s", item.(apis.Request).Event, tc.want)
			}
		})
	}
}

func TestPodWakeupsMergeWithoutChangingPolicyRequests(t *testing.T) {
	for _, operation := range []string{"add", "late-delete"} {
		t.Run(operation, func(t *testing.T) {
			c, q := hotfixController(t)
			job := hotfixJob("job")
			for i := 0; i < 10; i++ {
				pod := hotfixPod(job, types.UID(fmt.Sprintf("pod-%d", i)))
				pod.Name = fmt.Sprintf("job-task-%d", i)
				pod.Annotations[jobhelpers.OutOfSyncKey] = "true"
				if operation == "add" {
					c.addPod(pod)
				} else {
					c.deletePod(pod)
				}
			}
			if q.Len() != 1 {
				t.Fatalf("got %d ordinary wakeups", q.Len())
			}
			item, _ := q.Get()
			q.Done(item)
			if item != jobSyncRequest(job) {
				t.Fatalf("unexpected wakeup: %+v", item)
			}
		})
	}
	c, q := hotfixController(t)
	for _, event := range []bus.Event{bus.PodFailedEvent, bus.PodPendingEvent, bus.PodEvictedEvent, bus.CommandIssuedEvent, bus.OutOfSyncEvent} {
		req := apis.Request{Namespace: "ns", JobName: "job", JobUid: "job", PodName: "pod", PodUID: "pod-uid", TaskName: "task", PartitionID: "2", JobVersion: 3, Event: event, ExitCode: 42}
		if event == bus.CommandIssuedEvent || event == bus.OutOfSyncEvent {
			req.Action = bus.ResumeJobAction
		}
		c.enqueueJobRequest(req)
		item, _ := q.Get()
		q.Done(item)
		if item != req {
			t.Fatalf("policy/command context changed: %+v", item)
		}
		c.handleJobError(q, req, nil, errWaitForPods, bus.ResumeJobAction)
		if q.waits[len(q.waits)-1] != req {
			t.Fatal("waiting changed policy context")
		}
	}
	for _, uid := range []types.UID{"old", "current"} {
		c.enqueueJobRequest(apis.Request{Namespace: "ns", JobName: "job", JobUid: uid, Event: bus.OutOfSyncEvent})
	}
	if q.Len() != 2 {
		t.Fatal("coalesced different Job lifecycles")
	}
}

func TestAbsentJobConfirmationPreservesUncertainRequests(t *testing.T) {
	for _, firstError := range []error{apierrors.NewForbidden(batch.Resource("jobs"), "job", errors.New("denied")), apierrors.NewServiceUnavailable("unavailable"), nil} {
		t.Run(fmt.Sprint(firstError), func(t *testing.T) {
			c, q := hotfixController(t)
			c.identityLimiter = rate.NewLimiter(rate.Inf, 1)
			old, early := hotfixJob("old"), hotfixJob("early")
			if err := c.jobInformer.Informer().GetIndexer().Add(old); err != nil {
				t.Fatal(err)
			}
			client := c.vcClient.(*vcfake.Clientset)
			apiErr := firstError
			client.PrependReactor("get", "jobs", func(ktesting.Action) (bool, runtime.Object, error) { return true, early.DeepCopy(), apiErr })
			req := apis.Request{Namespace: early.Namespace, JobName: early.Name, JobUid: early.UID, TaskName: "task", PodName: "pod", PodUID: "pod-uid", Event: bus.PodFailedEvent, ExitCode: 42, JobVersion: 3}
			for i := 0; i < 2; i++ {
				q.Add(req)
				c.processNextReq(0)
				if len(q.waits) != i+1 || q.waits[i] != req || c.absentJobUIDs.Len() != 0 {
					t.Fatal("uncertain or early request was discarded")
				}
				apiErr = nil
			}
			if len(client.Actions()) != 2 {
				t.Fatal("uncertain response prevented another confirmation")
			}
		})
	}
}

func TestAbsentJobConfirmationHandlesLatePodsAndNewLifecycle(t *testing.T) {
	c, q := hotfixController(t)
	old, current := hotfixJob("old"), hotfixJob("current")
	q.Add(jobSyncRequest(old))
	c.processNextReq(0) // NotFound confirms old UID absent.
	if c.absentJobUIDs.Len() != 1 {
		t.Fatal("absence was not remembered")
	}
	client := c.vcClient.(*vcfake.Clientset)
	client.ClearActions()
	// A stale informer view and a newly created placeholder must not revive old work.
	if err := c.jobInformer.Informer().GetIndexer().Add(old); err != nil {
		t.Fatal(err)
	}
	if err := c.cache.AddPod(hotfixPod(old, "late")); err != nil {
		t.Fatal(err)
	}
	q.Add(jobSyncRequest(old))
	c.processNextReq(0)
	if _, err := c.cache.Get(old.UID); !errors.Is(err, jobcache.ErrJobDeleted) {
		t.Fatalf("late placeholder was not retired: %v", err)
	}
	if len(client.Actions()) != 0 || len(q.waits) != 0 {
		t.Fatal("confirmed old request did not drain")
	}
	if _, err := client.BatchV1alpha1().Jobs(current.Namespace).Create(context.TODO(), current, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	c.identityLimiter = rate.NewLimiter(rate.Inf, 1)
	if err := c.cache.AddPod(hotfixPod(current, "early")); err != nil {
		t.Fatal(err)
	}
	req := jobSyncRequest(current)
	q.Add(req)
	c.processNextReq(0)
	if len(q.waits) != 1 || q.waits[0] != req {
		t.Fatal("same-name new lifecycle was discarded")
	}
	if _, err := c.cache.Get(current.UID); !errors.Is(err, jobcache.ErrJobNotReady) {
		t.Fatalf("early Pod placeholder lost: %v", err)
	}
}

func TestAbsentJobConfirmationCacheIsBounded(t *testing.T) {
	c, q := hotfixController(t)
	c.identityLimiter = rate.NewLimiter(rate.Inf, 1)
	for i := 0; i <= maxAbsentJobUIDs; i++ {
		req := apis.Request{Namespace: "ns", JobName: "job", JobUid: types.UID(fmt.Sprintf("gone-%d", i)), Event: bus.OutOfSyncEvent}
		q.Add(req)
		c.processNextReq(0)
	}
	if c.absentJobUIDs.Len() != maxAbsentJobUIDs {
		t.Fatal("confirmation cache is not bounded")
	}
	client := c.vcClient.(*vcfake.Clientset)
	client.ClearActions()
	q.Add(apis.Request{Namespace: "ns", JobName: "job", JobUid: "gone-0", Event: bus.OutOfSyncEvent})
	c.processNextReq(0)
	if len(client.Actions()) != 1 || len(q.waits) != 0 || c.absentJobUIDs.Len() != maxAbsentJobUIDs {
		t.Fatal("evicted UID did not safely recheck")
	}
	// Different workers may confirm different Job UIDs concurrently.
	var wg sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				req := apis.Request{Namespace: "ns", JobName: fmt.Sprintf("job-%d", worker), JobUid: types.UID(fmt.Sprintf("worker-%d-%d", worker, i))}
				c.waitForJob(q, req, jobcache.ErrJobNotFound)
			}
		}()
	}
	wg.Wait()
	if c.absentJobUIDs.Len() != maxAbsentJobUIDs {
		t.Fatal("concurrent confirmations exceeded capacity")
	}
}
