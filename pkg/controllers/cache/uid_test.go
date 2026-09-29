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
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
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
