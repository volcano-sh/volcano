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
	"sync"
	"testing"
	"time"

	testingclock "k8s.io/utils/clock/testing"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestUnschedulableJobCacheExpirationAndRetry(t *testing.T) {
	start := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	fakeClock := testingclock.NewFakeClock(start)
	cache := newUnschedulableJobCacheWithClock(
		fakeClock, defaultJobMaxInUnschedulableCacheDuration)
	jobID := api.JobID("default/job")

	cache.Add(jobID, "dequeue")
	if !cache.Contains(jobID) {
		t.Fatal("newly added job is not cached")
	}

	fakeClock.Step(defaultJobMaxInUnschedulableCacheDuration / 2)
	cache.Add(jobID, "duplicate")
	fakeClock.Step(defaultJobMaxInUnschedulableCacheDuration / 2)
	if cache.Contains(jobID) {
		t.Fatal("duplicate add extended the original expiration deadline")
	}

	cache.Add(jobID, "retry")
	if !cache.Contains(jobID) {
		t.Fatal("job was not cached for a new retry cycle")
	}
	fakeClock.Step(defaultJobMaxInUnschedulableCacheDuration)
	if cache.Contains(jobID) {
		t.Fatal("job remained cached at the retry cycle expiration boundary")
	}
}

func TestUnschedulableJobCacheDelete(t *testing.T) {
	cache := newUnschedulableJobCache()
	jobID := api.JobID("default/job")

	cache.Add(jobID, "dequeue")
	cache.Delete(jobID)

	if cache.Contains(jobID) {
		t.Fatal("deleted job remains in unschedulable job cache")
	}
}

func TestSchedulerCacheTracksUnschedulableJobUntilExpiration(t *testing.T) {
	start := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	fakeClock := testingclock.NewFakeClock(start)
	sc := NewDefaultMockSchedulerCache("volcano")
	sc.unschedulableJobs = newUnschedulableJobCacheWithClock(
		fakeClock, defaultJobMaxInUnschedulableCacheDuration)

	queue := util.BuildQueue("queue", 1, nil)
	podGroup := util.BuildPodGroup(
		"job", "default", "queue", 1, nil, schedulingv1beta1.PodGroupInqueue)
	sc.AddQueueV1beta1(queue)
	sc.AddPodGroupV1beta1(podGroup)

	jobID := api.JobID("default/job")
	sc.AddUnschedulableJob(jobID, "dequeue")
	if _, found := sc.Snapshot().Jobs[jobID]; !found {
		t.Fatal("cached job was excluded from the scheduling snapshot")
	}
	if phase := sc.Snapshot().Jobs[jobID].PodGroup.Status.Phase; phase != scheduling.PodGroupPending {
		t.Fatalf("cached job phase = %v, want %v", phase, scheduling.PodGroupPending)
	}
	if !sc.IsJobUnschedulable(jobID) {
		t.Fatal("newly cached job was not reported as unschedulable")
	}

	fakeClock.Step(defaultJobMaxInUnschedulableCacheDuration)
	if _, found := sc.Snapshot().Jobs[jobID]; !found {
		t.Fatal("expired job was excluded from the scheduling snapshot")
	}
	if sc.IsJobUnschedulable(jobID) {
		t.Fatal("job remained unschedulable at the expiration boundary")
	}

	sc.AddUnschedulableJob(api.JobID("default/unknown"), "dequeue")
	if sc.IsJobUnschedulable(api.JobID("default/unknown")) {
		t.Fatal("unknown job was retained in the unschedulable job cache")
	}
}

func TestSchedulerCacheJobDeletionCleansUnschedulableCache(t *testing.T) {
	sc := NewDefaultMockSchedulerCache("volcano")
	sc.AddQueueV1beta1(util.BuildQueue("queue", 1, nil))
	sc.AddPodGroupV1beta1(util.BuildPodGroup(
		"job", "default", "queue", 1, nil, schedulingv1beta1.PodGroupPending))

	jobID := api.JobID("default/job")
	sc.AddUnschedulableJob(jobID, "dequeue")
	if err := sc.deletePodGroup(jobID); err != nil {
		t.Fatalf("deletePodGroup returned error: %v", err)
	}
	sc.processCleanupJob()

	if _, found := sc.Jobs[jobID]; found {
		t.Fatal("deleted job remains in scheduler cache")
	}
	if sc.IsJobUnschedulable(jobID) {
		t.Fatal("deleted job remains in unschedulable job cache")
	}
}

func TestUnschedulableJobCacheConcurrentAccess(t *testing.T) {
	sc := NewDefaultMockSchedulerCache("volcano")
	sc.AddQueueV1beta1(util.BuildQueue("queue", 1, nil))
	sc.AddPodGroupV1beta1(util.BuildPodGroup(
		"job", "default", "queue", 1, nil, schedulingv1beta1.PodGroupPending))
	jobID := api.JobID("default/job")
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			sc.AddUnschedulableJob(jobID, "dequeue")
		}()
		go func() {
			defer wg.Done()
			sc.Snapshot()
		}()
		go func() {
			defer wg.Done()
			sc.unschedulableJobs.Delete(jobID)
		}()
	}
	wg.Wait()
}
