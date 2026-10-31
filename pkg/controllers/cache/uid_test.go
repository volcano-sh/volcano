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

package cache

import (
	"errors"
	"fmt"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	"volcano.sh/volcano/pkg/controllers/apis"
)

func uidJob(uid types.UID) *batch.Job {
	return &batch.Job{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "job", UID: uid, ResourceVersion: "1"}}
}
func uidPod(job *batch.Job, uid types.UID) *v1.Pod {
	return &v1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: job.Namespace, Name: "job-task-0", UID: uid,
		OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(job, helpers.JobKind)},
		Annotations:     map[string]string{batch.JobNameKey: job.Name, batch.TaskSpecKey: "task", batch.JobVersion: "0"}}}
}
func TestRecreatedJobKeepsSeparatePods(t *testing.T) {
	c := New()
	a, b := uidJob("a"), uidJob("b")
	pa, pb := uidPod(a, "pa"), uidPod(b, "pb")
	for _, err := range []error{c.Add(a), c.AddPod(pa), c.Delete(a), c.Add(b), c.AddPod(pb)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := c.DeletePod(pa); err != nil {
		t.Fatal(err)
	}
	if !c.HasPod(pb) {
		t.Fatal("old Pod delete removed successor")
	}
	got, err := c.Get(b.UID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Job.UID != b.UID || got.Pods["task"][pb.Name].UID != pb.UID {
		t.Fatal("mixed lifecycles")
	}
}
func TestPodBeforeRecreatedJob(t *testing.T) {
	c := New()
	a, b := uidJob("a"), uidJob("b")
	if err := c.Add(a); err != nil {
		t.Fatal(err)
	}
	if err := c.AddPod(uidPod(a, "pa")); err != nil {
		t.Fatal(err)
	}
	pb := uidPod(b, "pb")
	if err := c.AddPod(pb); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(b); err != nil {
		t.Fatal(err)
	}
	got, err := c.Get(b.UID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pods["task"][pb.Name].UID != pb.UID {
		t.Fatal("early Pod lost")
	}
}

func TestDeletedJobCannotBeRevivedByStatusWrite(t *testing.T) {
	c := New().(*jobCache)
	defer c.deletedJobs.ShutDown()
	a := uidJob("a")
	p := uidPod(a, "pa")
	for _, err := range []error{c.Add(a), c.AddPod(p), c.Delete(a)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Update(a); err != ErrJobDeleted {
		t.Fatalf("status resurrected deleted Job: %v", err)
	}
	if err := c.Add(a); err != ErrJobDeleted {
		t.Fatalf("Add resurrected deleted entry: %v", err)
	}
	if err := c.DeletePod(p); err != nil {
		t.Fatal(err)
	}
	c.processCleanupJob()
	if err := c.Update(a); err != ErrJobNotFound {
		t.Fatalf("late status recreated cache: %v", err)
	}
	// A late Pod may recreate a placeholder, but an API status response cannot initialize it.
	if err := c.AddPod(p); err != nil {
		t.Fatal(err)
	}
	if err := c.Update(a); err != ErrJobNotReady {
		t.Fatalf("status initialized placeholder: %v", err)
	}
}

func TestCleanupChecksEntryIdentity(t *testing.T) {
	c := New().(*jobCache)
	defer c.deletedJobs.ShutDown()
	a, b := uidJob("a"), uidJob("b")
	if err := c.Add(a); err != nil {
		t.Fatal(err)
	}
	old := c.jobs[a.UID]
	if err := c.Delete(a); err != nil {
		t.Fatal(err)
	}
	c.processCleanupJob()
	// A delayed Pod event can install a different entry even under the old UID.
	pa := uidPod(a, "pa")
	if err := c.AddPod(pa); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(b); err != nil {
		t.Fatal(err)
	}
	c.deletedJobs.Add(old)
	c.processCleanupJob()
	if !c.HasPod(pa) {
		t.Fatal("old cleanup removed a new placeholder")
	}
	if _, err := c.Get(b.UID); err != nil {
		t.Fatal("old cleanup removed successor", err)
	}
	if err := c.DeletePod(pa); err != nil {
		t.Fatal(err)
	}
	c.processCleanupJob()
	if _, exists := c.jobs[a.UID]; exists {
		t.Fatal("orphan placeholder leaked")
	}
}

func TestDelayedOldCleanupCannotRemoveRecreatedJob(t *testing.T) {
	c := New().(*jobCache)
	c.deletedJobs.ShutDown()
	// Keep the real rate-limited retry pending until the test explicitly
	// delivers it, so the lifecycle ordering is independent of wall time.
	c.deletedJobs = workqueue.NewTypedRateLimitingQueue(
		workqueue.NewTypedItemExponentialFailureRateLimiter[*apis.JobInfo](time.Hour, time.Hour))
	defer c.deletedJobs.ShutDown()

	a, b := uidJob("a"), uidJob("b")
	pa, pb := uidPod(a, "pa"), uidPod(b, "pb")
	for _, err := range []error{c.Add(a), c.AddPod(pa), c.Delete(a)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	old := c.jobs[a.UID]
	c.processCleanupJob() // Old Pod remains, so cleanup schedules a delayed retry.
	if c.deletedJobs.NumRequeues(old) != 1 {
		t.Fatal("old cleanup did not schedule a rate-limited retry")
	}
	if err := c.DeletePod(pa); err != nil {
		t.Fatal(err)
	}
	c.processCleanupJob() // The last Pod delete immediately removes A.
	if _, found := c.jobs[a.UID]; found {
		t.Fatal("old Job remained after its last Pod was deleted")
	}
	for _, err := range []error{c.Add(b), c.AddPod(pb)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	c.deletedJobs.Add(old) // Deliver the previously scheduled retry after B exists.
	c.processCleanupJob()
	if _, found := c.jobs[a.UID]; found {
		t.Fatal("old cleanup revived deleted Job")
	}
	got, err := c.Get(b.UID)
	if err != nil {
		t.Fatalf("old cleanup removed the new Job: %v", err)
	}
	if got.Pods["task"][pb.Name] == nil || got.Pods["task"][pb.Name].UID != pb.UID {
		t.Fatal("old cleanup changed the new Job's Pod")
	}
}

func TestDeleteRejectsJobIdentityMismatch(t *testing.T) {
	for _, placeholder := range []bool{false, true} {
		t.Run(fmt.Sprintf("placeholder=%t", placeholder), func(t *testing.T) {
			c := New().(*jobCache)
			defer c.deletedJobs.ShutDown()
			job := uidJob("current")
			job.Namespace = "other"
			pod := uidPod(job, "pod")
			if placeholder {
				if err := c.AddPod(pod); err != nil {
					t.Fatal(err)
				}
			} else if err := c.Add(job); err != nil {
				t.Fatal(err)
			}
			for _, identity := range []struct{ namespace, name string }{
				{namespace: "ns", name: job.Name},
				{namespace: job.Namespace, name: "different"},
			} {
				forged := uidJob(job.UID)
				forged.Namespace, forged.Name = identity.namespace, identity.name
				if err := c.Delete(forged); !errors.Is(err, ErrJobNotFound) {
					t.Fatalf("Delete(%s/%s) = %v", forged.Namespace, forged.Name, err)
				}
				if c.jobs[job.UID].Deleted {
					t.Fatal("foreign identity deleted Job cache entry")
				}
			}
			if placeholder && !c.HasPod(pod) {
				t.Fatal("foreign identity removed Pod placeholder")
			}
		})
	}
}

func TestLatePodInstanceDoesNotModifyReplacement(t *testing.T) {
	c := New()
	job := uidJob("a")
	old := uidPod(job, "p1")
	current := uidPod(job, "p2")
	for _, err := range []error{c.Add(job), c.AddPod(current)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := c.UpdatePod(old); err == nil {
		t.Fatal("stale update accepted")
	}
	if err := c.DeletePod(old); err != nil {
		t.Fatal(err)
	}
	if c.HasPod(old) || !c.HasPod(current) {
		t.Fatal("Pod instance guard failed")
	}
}

func TestLargeJobUIDIsolationAndCleanup(t *testing.T) {
	c := New().(*jobCache)
	defer c.deletedJobs.ShutDown()
	a, b := uidJob("a"), uidJob("b")
	if err := c.Add(a); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5000; i++ {
		p := uidPod(a, types.UID(fmt.Sprintf("a-%d", i)))
		p.Name = fmt.Sprintf("job-task-%d", i)
		if err := c.AddPod(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Delete(a); err != nil {
		t.Fatal(err)
	}
	if err := c.Add(b); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5000; i++ {
		pa := uidPod(a, types.UID(fmt.Sprintf("a-%d", i)))
		pa.Name = fmt.Sprintf("job-task-%d", i)
		pb := uidPod(b, types.UID(fmt.Sprintf("b-%d", i)))
		pb.Name = pa.Name
		if err := c.AddPod(pb); err != nil {
			t.Fatal(err)
		}
		if err := c.DeletePod(pa); err != nil {
			t.Fatal(err)
		}
	}
	c.processCleanupJob()
	got, err := c.Get(b.UID)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.jobs) != 1 || len(got.Pods["task"]) != 5000 {
		t.Fatalf("entries=%d successor pods=%d", len(c.jobs), len(got.Pods["task"]))
	}
}
