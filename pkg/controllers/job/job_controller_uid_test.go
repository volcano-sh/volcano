/*
Copyright 2019 The Volcano Authors.

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
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/time/rate"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	bus "volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	vcfake "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	"volcano.sh/volcano/pkg/controllers/apis"
	jobcache "volcano.sh/volcano/pkg/controllers/cache"
	"volcano.sh/volcano/pkg/controllers/job/state"
)

type uidWaitQueue struct {
	workqueue.TypedRateLimitingInterface[any]
	waits []apis.Request
}

func (q *uidWaitQueue) AddAfter(item any, _ time.Duration) {
	q.waits = append(q.waits, item.(apis.Request))
}
func hotfixController(t *testing.T) (*jobcontroller, *uidWaitQueue) {
	t.Helper()
	c := newFakeController()
	c.recorder = record.NewFakeRecorder(100)
	for _, q := range c.queueList {
		q.ShutDown()
	}
	q := &uidWaitQueue{TypedRateLimitingInterface: workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[any]())}
	c.workers = 1
	c.queueList = []workqueue.TypedRateLimitingInterface[any]{q}
	t.Cleanup(func() { q.ShutDown(); c.commandQueue.ShutDown(); c.errTasks.ShutDown() })
	return c, q
}
func hotfixJob(uid types.UID) *batch.Job {
	return &batch.Job{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "job", UID: uid, ResourceVersion: "1"},
		Spec:   batch.JobSpec{Queue: "default", MinAvailable: 1, Tasks: []batch.TaskSpec{{Name: "task", Replicas: 1, Template: v1.PodTemplateSpec{Spec: v1.PodSpec{Containers: []v1.Container{{Name: "container", Image: "image"}}}}}}},
		Status: batch.JobStatus{State: batch.JobState{Phase: batch.Running}, MinAvailable: 1}}
}
func hotfixPod(job *batch.Job, uid types.UID) *v1.Pod {
	return &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: job.Namespace, Name: "job-task-0", UID: uid, ResourceVersion: "1",
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(job, helpers.JobKind)},
		Annotations:     map[string]string{batch.JobNameKey: job.Name, batch.TaskSpecKey: "task", batch.JobVersion: "0"}},
		Status: v1.PodStatus{Phase: v1.PodRunning}}
}
func setupHotfixJob(t *testing.T, c *jobcontroller, job *batch.Job) {
	t.Helper()
	if _, err := c.vcClient.BatchV1alpha1().Jobs(job.Namespace).Create(context.TODO(), job, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.jobInformer.Informer().GetIndexer().Add(job); err != nil {
		t.Fatal(err)
	}
	if err := c.cache.Add(job); err != nil {
		t.Fatal(err)
	}
	if err := c.queueInformer.Informer().GetIndexer().Add(&scheduling.Queue{ObjectMeta: metav1.ObjectMeta{Name: job.Spec.Queue}}); err != nil {
		t.Fatal(err)
	}
	pg := &scheduling.PodGroup{ObjectMeta: metav1.ObjectMeta{Namespace: job.Namespace, Name: c.generateRelatedPodGroupName(job), UID: "pg", OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(job, helpers.JobKind)}}, Spec: scheduling.PodGroupSpec{MinMember: 1, MinResources: &v1.ResourceList{}, MinTaskMember: map[string]int32{"task": 1}}, Status: scheduling.PodGroupStatus{Phase: scheduling.PodGroupInqueue}}
	if _, err := c.vcClient.SchedulingV1beta1().PodGroups(job.Namespace).Create(context.TODO(), pg, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := c.pgInformer.Informer().GetIndexer().Add(pg); err != nil {
		t.Fatal(err)
	}
}

func TestUIDChangesReportedAsUpdates(t *testing.T) {
	c, _ := hotfixController(t)
	a, b := hotfixJob("a"), hotfixJob("b")
	c.addJob(a)
	c.updateJob(a, b)
	if _, err := c.cache.Get(a.UID); !errors.Is(err, jobcache.ErrJobDeleted) {
		t.Fatalf("old lifecycle still live: %v", err)
	}
	if _, err := c.cache.Get(b.UID); err != nil {
		t.Fatal(err)
	}
	pa, pb := hotfixPod(a, "pa"), hotfixPod(b, "pb")
	c.addPod(pa)
	c.updatePod(pa, pb)
	if !c.cache.HasPod(pb) || c.cache.HasPod(pa) {
		t.Fatal("Pod replacement mixed UIDs")
	}
	c.deletePod(pa)
	if !c.cache.HasPod(pb) {
		t.Fatal("late tombstone removed new Pod")
	}
}

func TestEarlyPolicyWaitsForItsJob(t *testing.T) {
	c, q := hotfixController(t)
	a, b := hotfixJob("a"), hotfixJob("b")
	if err := c.jobInformer.Informer().GetIndexer().Add(a); err != nil {
		t.Fatal(err)
	}
	if _, err := c.vcClient.BatchV1alpha1().Jobs(b.Namespace).Create(context.TODO(), b, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	p := hotfixPod(b, "pb")
	if err := c.cache.AddPod(p); err != nil {
		t.Fatal(err)
	}
	req := apis.Request{Namespace: b.Namespace, JobName: b.Name, JobUid: b.UID, PodName: p.Name, PodUID: p.UID, TaskName: "task", Event: bus.PodFailedEvent, ExitCode: 42}
	q.Add(req)
	c.processNextReq(0)
	if len(q.waits) != 1 || q.waits[0] != req || q.NumRequeues(req) != 0 {
		t.Fatal("early failure policy lost or counted as execution failure")
	}
	if err := c.cache.Add(b); err != nil {
		t.Fatal(err)
	}
	info, err := c.cache.Get(b.UID)
	if err != nil {
		t.Fatal(err)
	}
	code := int32(42)
	info.Job.Spec.Policies = []batch.LifecyclePolicy{{ExitCode: &code, Action: bus.RestartJobAction}}
	if applyPolicies(info.Job, &q.waits[0]).action != bus.RestartJobAction {
		t.Fatal("exit-code policy was lost")
	}
}

func TestPodNameWaitPreservesRetryBudgetAndRecovers(t *testing.T) {
	c, q := hotfixController(t)
	b := hotfixJob("b")
	setupHotfixJob(t, c, b)
	old := hotfixPod(hotfixJob("a"), "old")
	if _, err := c.kubeClient.CoreV1().Pods(b.Namespace).Create(context.TODO(), old, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	info, err := c.cache.Get(b.UID)
	if err != nil {
		t.Fatal(err)
	}
	statusCalled := false
	err = c.syncJob(info, func(*batch.JobStatus) bool { statusCalled = true; return true })
	if !errors.Is(err, errWaitForPods) || statusCalled {
		t.Fatalf("wait=%v statusCalled=%v", err, statusCalled)
	}
	req := jobSyncRequest(b)
	req.Action = bus.ResumeJobAction
	q.AddRateLimited(req)
	failures := q.NumRequeues(req)
	c.maxRequeueNum = 0
	c.handleJobError(q, req, nil, err, bus.SyncJobAction)
	if q.NumRequeues(req) != failures || len(q.waits) != 1 || q.waits[0] != req {
		t.Fatal("name wait changed failure budget or request")
	}
	if err := c.kubeClient.CoreV1().Pods(b.Namespace).Delete(context.TODO(), old.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	info, err = c.cache.Get(b.UID)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.syncJob(info, nil); err != nil {
		t.Fatal(err)
	}
	pod, err := c.kubeClient.CoreV1().Pods(b.Namespace).Get(context.TODO(), old.Name, metav1.GetOptions{})
	if err != nil || !metav1.IsControlledBy(pod, b) {
		t.Fatalf("replica did not recover: %v", err)
	}
}

func TestStalePodOperationsKeepReplacement(t *testing.T) {
	for _, operation := range []string{"patch", "delete"} {
		for _, replaced := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%v", operation, replaced), func(t *testing.T) {
				c, _ := hotfixController(t)
				job := hotfixJob("b")
				old := hotfixPod(job, "old")
				current := old.DeepCopy()
				if replaced {
					current.UID = "new"
				}
				client := c.kubeClient.(*kubefake.Clientset)
				if _, err := client.CoreV1().Pods(job.Namespace).Create(context.TODO(), current, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
				client.PrependReactor(operation, "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
					if operation == "delete" {
						opts := action.(ktesting.DeleteAction).GetDeleteOptions()
						if opts.Preconditions == nil || opts.Preconditions.UID == nil || *opts.Preconditions.UID != old.UID {
							t.Fatal("missing delete UID")
						}
					} else {
						var patch []map[string]any
						if err := json.Unmarshal(action.(ktesting.PatchAction).GetPatch(), &patch); err != nil {
							t.Fatal(err)
						}
						if patch[0]["op"] != "test" || patch[0]["path"] != "/metadata/uid" || patch[0]["value"] != string(old.UID) {
							t.Fatal("missing patch UID test")
						}
					}
					return true, nil, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, old.Name, errors.New("conflict"))
				})
				var err error
				if operation == "patch" {
					err = c.markPodOutOfSync(old)
				} else {
					err = c.deleteJobPod(job.Name, old)
				}
				if (err == nil) != replaced {
					t.Fatalf("must ignore only confirmed replacement: %v", err)
				}
				got, err := client.CoreV1().Pods(job.Namespace).Get(context.TODO(), current.Name, metav1.GetOptions{})
				if err != nil || got.UID != current.UID {
					t.Fatal("replacement was changed")
				}
			})
		}
	}
}

func TestRestartPodKeepsTriggerUIDAndWakeup(t *testing.T) {
	c, q := hotfixController(t)
	b := hotfixJob("b")
	p := hotfixPod(b, "new")
	if err := c.cache.Add(b); err != nil {
		t.Fatal(err)
	}
	if err := c.cache.AddPod(p); err != nil {
		t.Fatal(err)
	}
	info, err := c.cache.Get(b.UID)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.killTarget(info, state.Target{Type: state.TargetTypePod, TaskName: "task", PodName: p.Name, PodUID: "old"}, func(*batch.JobStatus) bool { t.Fatal("stale target updated status"); return false }); err != nil {
		t.Fatal(err)
	}
	if q.Len() != 1 {
		t.Fatal("lost replica wakeup")
	}
	item, _ := q.Get()
	q.Done(item)
	if item.(apis.Request) != jobSyncRequest(b) {
		t.Fatal("unexpected recovery request")
	}
	if len(c.kubeClient.(*kubefake.Clientset).Actions()) != 0 {
		t.Fatal("stale target issued API operations")
	}
}

func TestTimersAreIsolatedAndReplacementSurvives(t *testing.T) {
	c, _ := hotfixController(t)
	a, b := hotfixJob("a"), hotfixJob("b")
	reqA := jobSyncRequest(a)
	reqA.PodName = "pod"
	reqA.PodUID = "pa"
	reqB := jobSyncRequest(b)
	reqB.PodName = "pod"
	reqB.PodUID = "pb"
	old := &delayAction{jobKey: "ns/job", jobUID: a.UID, podName: "pod", podUID: "pa", delay: time.Hour, action: bus.RestartJobAction}
	current := &delayAction{jobKey: "ns/job", jobUID: b.UID, podName: "pod", podUID: "pb", delay: time.Hour, action: bus.RestartJobAction}
	c.AddDelayActionForJob(reqA, old)
	c.AddDelayActionForJob(reqB, current)
	replacement := *current
	replacement.podUID = "pb2"
	reqB.PodUID = "pb2"
	c.AddDelayActionForJob(reqB, &replacement)
	c.removeDelayAction(current)
	c.cleanupDelayActions(current)
	c.cancelJobDelayActions(a.UID)
	c.delayActionMapLock.RLock()
	got := c.delayActionMap[b.UID]["pod"]
	c.delayActionMapLock.RUnlock()
	if got != &replacement {
		t.Fatal("old action removed new timer")
	}
	c.cancelJobDelayActions(b.UID)
}

func TestRunningOnlyCancelsCurrentPodTimers(t *testing.T) {
	c, _ := hotfixController(t)
	job := hotfixJob("b")
	current := hotfixPod(job, "current")
	if err := c.podInformer.Informer().GetIndexer().Add(current); err != nil {
		t.Fatal(err)
	}
	for _, event := range []bus.Event{bus.PodFailedEvent, bus.PodEvictedEvent, bus.PodPendingEvent} {
		t.Run(string(event), func(t *testing.T) {
			req := jobSyncRequest(job)
			req.PodName, req.PodUID, req.Event = current.Name, "old", bus.PodRunningEvent
			action := &delayAction{jobUID: job.UID, podName: current.Name, podUID: "old", event: event, action: bus.RestartJobAction, delay: time.Hour}
			c.AddDelayActionForJob(req, action)
			c.CleanPodDelayActionsIfNeed(req)
			if c.delayActionMap[job.UID][current.Name] != action {
				t.Fatal("stale Running canceled timer")
			}
			req.PodUID = current.UID
			c.CleanPodDelayActionsIfNeed(req)
			_, retained := c.delayActionMap[job.UID][current.Name]
			if retained != (event == bus.PodPendingEvent) {
				t.Fatal("current Running changed cancellation semantics")
			}
			c.cancelJobDelayActions(job.UID)
		})
	}
}

func TestExpiredTimersCheckLifecycleVersionAndPendingPod(t *testing.T) {
	for _, scenario := range []string{"deleted", "restarted", "replacement", "running"} {
		t.Run(scenario, func(t *testing.T) {
			c, _ := hotfixController(t)
			job := hotfixJob("b")
			p := hotfixPod(job, "current")
			p.Status.Phase = v1.PodPending
			if scenario == "restarted" {
				job.Status.Version = 1
			}
			if scenario == "running" {
				p.Status.Phase = v1.PodRunning
			}
			if err := c.cache.Add(job); err != nil {
				t.Fatal(err)
			}
			if err := c.cache.AddPod(p); err != nil {
				t.Fatal(err)
			}
			if scenario == "deleted" {
				if err := c.cache.Delete(job); err != nil {
					t.Fatal(err)
				}
			}
			req := jobSyncRequest(job)
			req.PodName = p.Name
			action := &delayAction{jobKey: "ns/job", jobUID: job.UID, taskName: "task", podName: p.Name, podUID: p.UID, event: bus.PodPendingEvent, action: bus.RestartJobAction, delay: time.Millisecond}
			if scenario == "replacement" {
				action.podUID = "old"
			}
			c.AddDelayActionForJob(req, action)
			deadline := time.Now().Add(time.Second)
			for {
				c.delayActionMapLock.RLock()
				done := len(c.delayActionMap) == 0
				c.delayActionMapLock.RUnlock()
				if done {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("expired timer retained")
				}
				time.Sleep(time.Millisecond)
			}
			if len(c.kubeClient.(*kubefake.Clientset).Actions()) != 0 {
				t.Fatal("expired timer operated on a stale target")
			}
		})
	}
}

func TestLocalResourceReadsCheckJobOwner(t *testing.T) {
	c, _ := hotfixController(t)
	a, b := hotfixJob("a"), hotfixJob("b")
	minimum := int32(1)
	b.Spec.Tasks[0].MinAvailable = &minimum
	p := hotfixPod(a, "pod")
	p.Status.Phase = v1.PodSucceeded
	if err := c.podInformer.Informer().GetIndexer().Add(p); err != nil {
		t.Fatal(err)
	}
	if c.isDependsOnPodsReady("task", b) {
		t.Fatal("old Pod satisfied current dependency")
	}
	p = hotfixPod(b, "current")
	p.Status.Phase = v1.PodSucceeded
	if err := c.podInformer.Informer().GetIndexer().Update(p); err != nil {
		t.Fatal(err)
	}
	if !c.isDependsOnPodsReady("task", b) {
		t.Fatal("current Pod no longer satisfies dependency")
	}
	pg := &scheduling.PodGroup{ObjectMeta: metav1.ObjectMeta{Namespace: b.Namespace, Name: b.Name, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(a, helpers.JobKind)}}}
	if err := c.pgInformer.Informer().GetIndexer().Add(pg); err != nil {
		t.Fatal(err)
	}
	if _, err := c.getPodGroupByJob(b); !apierrors.IsNotFound(err) {
		t.Fatalf("adopted old legacy PodGroup: %v", err)
	}
	pg.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(b, helpers.JobKind)}
	if err := c.pgInformer.Informer().GetIndexer().Update(pg); err != nil {
		t.Fatal(err)
	}
	if _, err := c.getPodGroupByJob(b); err != nil {
		t.Fatalf("legacy PodGroup compatibility lost: %v", err)
	}
}

func TestRealCreateErrorTakesPriorityOverNameWait(t *testing.T) {
	c, _ := hotfixController(t)
	job := hotfixJob("b")
	job.Spec.Tasks[0].Replicas = 2
	setupHotfixJob(t, c, job)
	c.kubeClient.(*kubefake.Clientset).PrependReactor("create", "pods", func(action ktesting.Action) (bool, runtime.Object, error) {
		p := action.(ktesting.CreateAction).GetObject().(*v1.Pod)
		if p.Name == "job-task-0" {
			return true, nil, apierrors.NewAlreadyExists(schema.GroupResource{Resource: "pods"}, p.Name)
		}
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, p.Name, errors.New("denied"))
	})
	info, err := c.cache.Get(job.UID)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.syncJob(info, nil); err == nil || errors.Is(err, errWaitForPods) {
		t.Fatalf("real create error hidden: %v", err)
	}
}

func TestSyncTaskOnlyWakesItsOriginalJob(t *testing.T) {
	c, q := hotfixController(t)
	a, b := hotfixJob("a"), hotfixJob("b")
	old, current := hotfixPod(a, "old"), hotfixPod(b, "current")
	if err := c.cache.Add(b); err != nil {
		t.Fatal(err)
	}
	if err := c.cache.AddPod(current); err != nil {
		t.Fatal(err)
	}
	if err := c.syncTask(old); err != nil {
		t.Fatal(err)
	}
	if len(q.waits) != 1 || q.waits[0] != jobSyncRequest(a) {
		t.Fatal("resync lost original identity")
	}
	if !c.cache.HasPod(current) || len(c.kubeClient.(*kubefake.Clientset).Actions()) != 0 {
		t.Fatal("resync overwrote informer projection")
	}
}

func TestCommandIdentitySurvivesDeleteRetry(t *testing.T) {
	c, q := hotfixController(t)
	a, b := hotfixJob("a"), hotfixJob("b")
	client := c.vcClient.(*vcfake.Clientset)
	if _, err := client.BatchV1alpha1().Jobs(a.Namespace).Create(context.TODO(), a, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	cmd := &bus.Command{ObjectMeta: metav1.ObjectMeta{Name: "restart", Namespace: a.Namespace}, TargetObject: &metav1.OwnerReference{Name: a.Name}, Action: string(bus.RestartJobAction)}
	if _, err := client.BusV1alpha1().Commands(a.Namespace).Create(context.TODO(), cmd, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	failed := false
	client.PrependReactor("delete", "commands", func(ktesting.Action) (bool, runtime.Object, error) {
		if !failed {
			failed = true
			return true, nil, errors.New("temporary delete error")
		}
		return false, nil, nil
	})
	c.commandQueue.Add(cmd)
	c.processNextCommand()
	if cmd.TargetObject.UID != "" {
		t.Fatal("mutated informer object")
	}
	if err := client.BatchV1alpha1().Jobs(a.Namespace).Delete(context.TODO(), a.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.BatchV1alpha1().Jobs(b.Namespace).Create(context.TODO(), b, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	c.processNextCommand()
	if q.Len() != 1 {
		t.Fatal("command lost")
	}
	item, _ := q.Get()
	q.Done(item)
	if item.(apis.Request).JobUid != a.UID {
		t.Fatal("command rebound to replacement Job")
	}
}

func TestMissingJobConfirmationIsBoundedAndDrains(t *testing.T) {
	c, q := hotfixController(t)
	job := hotfixJob("gone")
	p := hotfixPod(job, "orphan")
	if err := c.cache.AddPod(p); err != nil {
		t.Fatal(err)
	}
	c.identityLimiter = rate.NewLimiter(0, 1)
	req := jobSyncRequest(job)
	c.waitForJob(q, req, jobcache.ErrJobNotReady)
	if _, err := c.cache.Get(job.UID); !errors.Is(err, jobcache.ErrJobDeleted) {
		t.Fatalf("orphan not retired: %v", err)
	}
	c.waitForJob(q, req, jobcache.ErrJobDeleted)
	if len(q.waits) != 0 {
		t.Fatal("deleted lifecycle did not drain")
	}
	req.JobUid = "unknown"
	c.waitForJob(q, req, jobcache.ErrJobNotFound)
	if len(q.waits) != 1 {
		t.Fatal("rate limited confirmation was lost")
	}
	gets := 0
	for _, action := range c.vcClient.(*vcfake.Clientset).Actions() {
		if action.Matches("get", "jobs") {
			gets++
		}
	}
	if gets != 1 {
		t.Fatalf("unexpected live queries: %d", gets)
	}
}

func TestLargePodEventBurstDoesNotReadAPI(t *testing.T) {
	c, q := hotfixController(t)
	a, b := hotfixJob("a"), hotfixJob("b")
	c.addJob(a)
	c.updateJob(a, b)
	for i := 0; i < 5000; i++ {
		old, current := hotfixPod(a, types.UID(fmt.Sprintf("old-%d", i))), hotfixPod(b, types.UID(fmt.Sprintf("new-%d", i)))
		old.Name, current.Name = fmt.Sprintf("job-task-%d", i), fmt.Sprintf("job-task-%d", i)
		c.addPod(old)
		c.updatePod(old, current)
	}
	if len(c.vcClient.(*vcfake.Clientset).Actions()) != 0 || len(c.kubeClient.(*kubefake.Clientset).Actions()) != 0 {
		t.Fatal("Pod callbacks issued live queries")
	}
	seen := make(map[apis.Request]bool)
	for q.Len() != 0 {
		item, _ := q.Get()
		q.Done(item)
		seen[item.(apis.Request)] = true
	}
	if !seen[jobSyncRequest(b)] {
		t.Fatal("current Job wakeup lost")
	}
	info, err := c.cache.Get(b.UID)
	if err != nil || len(info.Pods["task"]) != 5000 {
		t.Fatalf("large Job incomplete: %v", err)
	}
	small := hotfixJob("small")
	small.Name = "small"
	c.addJob(small)
	if _, err := c.cache.Get(small.UID); err != nil {
		t.Fatalf("small Job blocked after burst: %v", err)
	}
}

func TestPodReplacementKeepsPartitionIndex(t *testing.T) {
	c, _ := hotfixController(t)
	job := hotfixJob("job")
	job.Spec.Tasks[0].PartitionPolicy = &batch.PartitionPolicySpec{PartitionSize: 1, TotalPartitions: 2}
	c.addJob(job)
	old, current := hotfixPod(job, "old"), hotfixPod(job, "current")
	old.Labels = map[string]string{batch.TaskPartitionID: "0"}
	current.Labels = map[string]string{batch.TaskPartitionID: "1"}
	c.addPod(old)
	c.updatePod(old, current)
	c.deletePod(old)
	info, err := c.cache.Get(job.UID)
	if err != nil {
		t.Fatal(err)
	}
	partition := info.Partitions["task"].Partition
	if len(partition["0"]) != 0 || len(partition["1"]) != 1 || partition["1"][current.Name].UID != current.UID {
		t.Fatalf("partition index mixed Pod instances: %v", partition)
	}
}
