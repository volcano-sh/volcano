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

package gangevict

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"volcano.sh/volcano/pkg/scheduler/api"
)

func TestCreateJobBundles_SafeAndWhole(t *testing.T) {
	job := &api.JobInfo{
		MinAvailable: 3,
		TaskMinAvailable: map[string]int32{
			"worker": 3,
		},
		TaskMinAvailableTotal: 3,
		TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
			api.Running: {},
		},
		Tasks: map[api.TaskID]*api.TaskInfo{},
	}

	// ReadyTaskNum == 5 => surplus 2
	local := make([]*api.TaskInfo, 0, 5)
	for i := 0; i < 5; i++ {
		task := &api.TaskInfo{
			UID:      api.TaskID(string(rune('a' + i))),
			TaskRole: "worker",
			Resreq:   (&api.Resource{MilliCPU: 1000}).Clone(),
			Priority: int32(i),
		}
		job.TaskStatusIndex[api.Running][task.UID] = task
		job.Tasks[task.UID] = task
		local = append(local, task)
	}

	bundles := CreateJobBundles(job, local)
	assert.Len(t, bundles, 2)
	assert.Equal(t, BundleSafe, bundles[0].Type)
	assert.Equal(t, 2, len(bundles[0].Tasks))
	assert.Equal(t, BundleWhole, bundles[1].Type)
	assert.Equal(t, 3, len(bundles[1].Tasks))
}

func TestApplyAllowedTasks_WholeAtomicAndSafeShrink(t *testing.T) {
	t1 := &api.TaskInfo{UID: "t1", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}
	t2 := &api.TaskInfo{UID: "t2", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}
	t3 := &api.TaskInfo{UID: "t3", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}
	job := &api.JobInfo{}

	bundles := []*Bundle{
		{Type: BundleSafe, Job: job, Tasks: []*api.TaskInfo{t1, t2}, LocalRes: sumTasks([]*api.TaskInfo{t1, t2}), GlobalRes: api.EmptyResource()},
		{Type: BundleWhole, Job: job, Tasks: []*api.TaskInfo{t3}, LocalRes: sumTasks([]*api.TaskInfo{t3}), GlobalRes: sumTasks([]*api.TaskInfo{t1, t2, t3})},
	}
	allowed := map[api.TaskID]struct{}{"t1": {}, "t3x": {}}

	valid := ApplyAllowedTasks(bundles, allowed)
	assert.Len(t, valid, 1)
	assert.Equal(t, BundleSafe, valid[0].Type)
	assert.Len(t, valid[0].Tasks, 1)
	assert.Equal(t, api.TaskID("t1"), valid[0].Tasks[0].UID)
}

func TestSelectBundles_RespectsAllowWhole(t *testing.T) {
	job := &api.JobInfo{}
	safe := &Bundle{
		Type:     BundleSafe,
		Job:      job,
		Tasks:    []*api.TaskInfo{{UID: "t1", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes: (&api.Resource{MilliCPU: 1000}).Clone(),
	}
	whole := &Bundle{
		Type:     BundleWhole,
		Job:      job,
		Tasks:    []*api.TaskInfo{{UID: "t2", Resreq: (&api.Resource{MilliCPU: 2000}).Clone()}},
		LocalRes: (&api.Resource{MilliCPU: 2000}).Clone(),
	}
	need := (&api.Resource{MilliCPU: 1500}).Clone()

	selectedNoWhole := SelectBundles([]*Bundle{safe, whole}, need, false)
	assert.Len(t, selectedNoWhole, 1)
	assert.Equal(t, BundleSafe, selectedNoWhole[0].Type)

	selectedWhole := SelectBundles([]*Bundle{safe, whole}, need, true)
	assert.Len(t, selectedWhole, 2)
}

func TestSortBundlesForPreempt_TypePriorityAndROI(t *testing.T) {
	lowPriJob := &api.JobInfo{Priority: 1}
	highPriJob := &api.JobInfo{Priority: 10}
	safe := &Bundle{
		Type:     BundleSafe,
		Job:      highPriJob,
		Tasks:    []*api.TaskInfo{{UID: "s1", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes: (&api.Resource{MilliCPU: 1000}).Clone(),
	}
	wholeLowROI := &Bundle{
		Type:      BundleWhole,
		Job:       lowPriJob,
		Tasks:     []*api.TaskInfo{{UID: "w1", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes:  (&api.Resource{MilliCPU: 1000}).Clone(),
		GlobalRes: (&api.Resource{MilliCPU: 4000}).Clone(),
	}
	wholeHighROI := &Bundle{
		Type:      BundleWhole,
		Job:       lowPriJob,
		Tasks:     []*api.TaskInfo{{UID: "w2", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes:  (&api.Resource{MilliCPU: 1000}).Clone(),
		GlobalRes: (&api.Resource{MilliCPU: 2000}).Clone(),
	}
	bundles := []*Bundle{wholeLowROI, safe, wholeHighROI}

	SortBundlesForPreempt(bundles, (&api.Resource{MilliCPU: 1000}).Clone(), func(l, r *api.JobInfo) bool {
		return l.Priority < r.Priority
	})

	assert.Equal(t, api.TaskID("s1"), bundles[0].Tasks[0].UID)
	assert.Equal(t, api.TaskID("w2"), bundles[1].Tasks[0].UID)
	assert.Equal(t, api.TaskID("w1"), bundles[2].Tasks[0].UID)
}

func TestSortBundlesForReclaim_UsesQueueOrderThenROI(t *testing.T) {
	jobA := &api.JobInfo{Queue: "qa"}
	jobB := &api.JobInfo{Queue: "qb"}
	queues := map[api.QueueID]*api.QueueInfo{
		"qa": {UID: "qa", Name: "qa"},
		"qb": {UID: "qb", Name: "qb"},
	}
	bA := &Bundle{
		Type:      BundleWhole,
		Job:       jobA,
		Tasks:     []*api.TaskInfo{{UID: "a", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes:  (&api.Resource{MilliCPU: 1000}).Clone(),
		GlobalRes: (&api.Resource{MilliCPU: 4000}).Clone(),
	}
	bB := &Bundle{
		Type:      BundleWhole,
		Job:       jobB,
		Tasks:     []*api.TaskInfo{{UID: "b", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes:  (&api.Resource{MilliCPU: 1000}).Clone(),
		GlobalRes: (&api.Resource{MilliCPU: 2000}).Clone(),
	}
	lessQueue := func(l, r *api.QueueInfo) bool { return l.Name < r.Name }
	bundles := []*Bundle{bB, bA}

	SortBundlesForReclaim(bundles, (&api.Resource{MilliCPU: 1000}).Clone(), lessQueue, queues)
	assert.Equal(t, api.TaskID("a"), bundles[0].Tasks[0].UID)

	// If queue order ties/absent, ROI should be used.
	jobC := &api.JobInfo{Queue: "qc"}
	jobD := &api.JobInfo{Queue: "qd"}
	bC := &Bundle{
		Type:      BundleWhole,
		Job:       jobC,
		Tasks:     []*api.TaskInfo{{UID: "c", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes:  (&api.Resource{MilliCPU: 1000}).Clone(),
		GlobalRes: (&api.Resource{MilliCPU: 4000}).Clone(),
	}
	bD := &Bundle{
		Type:      BundleWhole,
		Job:       jobD,
		Tasks:     []*api.TaskInfo{{UID: "d", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes:  (&api.Resource{MilliCPU: 1000}).Clone(),
		GlobalRes: (&api.Resource{MilliCPU: 2000}).Clone(),
	}
	bundles = []*Bundle{bC, bD}
	SortBundlesForReclaim(bundles, (&api.Resource{MilliCPU: 1000}).Clone(), nil, queues)
	assert.Equal(t, api.TaskID("d"), bundles[0].Tasks[0].UID)
}

func TestSortBundlesForPreempt_UsesVictimJobOrder(t *testing.T) {
	jobA := &api.JobInfo{UID: "ja"}
	jobB := &api.JobInfo{UID: "jb"}

	a := &Bundle{
		Type:      BundleWhole,
		Job:       jobA,
		Tasks:     []*api.TaskInfo{{UID: "a", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes:  (&api.Resource{MilliCPU: 1000}).Clone(),
		GlobalRes: (&api.Resource{MilliCPU: 2000}).Clone(),
	}
	b := &Bundle{
		Type:      BundleWhole,
		Job:       jobB,
		Tasks:     []*api.TaskInfo{{UID: "b", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes:  (&api.Resource{MilliCPU: 1000}).Clone(),
		GlobalRes: (&api.Resource{MilliCPU: 2000}).Clone(),
	}
	bundles := []*Bundle{a, b}

	SortBundlesForPreempt(bundles, (&api.Resource{MilliCPU: 1000}).Clone(), func(l, r *api.JobInfo) bool {
		return l.UID > r.UID
	})

	assert.Equal(t, api.TaskID("b"), bundles[0].Tasks[0].UID)
}

func TestSortBundlesForReclaim_DeterministicTieBreaker(t *testing.T) {
	jobA := &api.JobInfo{UID: "ja", Queue: "q"}
	jobB := &api.JobInfo{UID: "jb", Queue: "q"}
	queues := map[api.QueueID]*api.QueueInfo{
		"q": {UID: "q", Name: "q"},
	}
	a := &Bundle{
		Type:      BundleWhole,
		Job:       jobA,
		Tasks:     []*api.TaskInfo{{UID: "a", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes:  (&api.Resource{MilliCPU: 1000}).Clone(),
		GlobalRes: (&api.Resource{MilliCPU: 2000}).Clone(),
	}
	b := &Bundle{
		Type:      BundleWhole,
		Job:       jobB,
		Tasks:     []*api.TaskInfo{{UID: "b", Resreq: (&api.Resource{MilliCPU: 1000}).Clone()}},
		LocalRes:  (&api.Resource{MilliCPU: 1000}).Clone(),
		GlobalRes: (&api.Resource{MilliCPU: 2000}).Clone(),
	}
	bundles := []*Bundle{b, a}

	SortBundlesForReclaim(bundles, (&api.Resource{MilliCPU: 1000}).Clone(), nil, queues)
	assert.Equal(t, api.TaskID("a"), bundles[0].Tasks[0].UID)
	assert.Equal(t, api.TaskID("b"), bundles[1].Tasks[0].UID)
}

// addRunningTaskToSubJob wires a Running task into both the job-level indexes
// and the named sub-job. It also records the task -> sub-job mapping so the
// CreateJobBundles sub-job gate can resolve the task's sub-job.
func addRunningTaskToSubJob(job *api.JobInfo, sj *api.SubJobInfo, uid api.TaskID, role string, priority int32) *api.TaskInfo {
	task := &api.TaskInfo{
		UID:      uid,
		TaskRole: role,
		Resreq:   (&api.Resource{MilliCPU: 1000}).Clone(),
		Priority: priority,
	}
	if job.TaskStatusIndex[api.Running] == nil {
		job.TaskStatusIndex[api.Running] = api.TasksMap{}
	}
	job.TaskStatusIndex[api.Running][task.UID] = task
	job.Tasks[task.UID] = task
	job.TaskToSubJob[task.UID] = sj.UID
	if sj.TaskStatusIndex[api.Running] == nil {
		sj.TaskStatusIndex[api.Running] = api.TasksMap{}
	}
	sj.TaskStatusIndex[api.Running][task.UID] = task
	sj.Tasks[task.UID] = task
	return task
}

func newSubJob(gid api.SubJobGID, uid api.SubJobID, minAvailable int32) *api.SubJobInfo {
	return &api.SubJobInfo{
		GID:             gid,
		UID:             uid,
		MinAvailable:    minAvailable,
		Tasks:           map[api.TaskID]*api.TaskInfo{},
		TaskStatusIndex: map[api.TaskStatus]api.TasksMap{},
	}
}

func newJobForSubJobBundling(jobMinAvailable int32, minSubJobs map[api.SubJobGID]int32) *api.JobInfo {
	return &api.JobInfo{
		MinAvailable:    jobMinAvailable,
		MinSubJobs:      minSubJobs,
		TaskStatusIndex: map[api.TaskStatus]api.TasksMap{},
		Tasks:           map[api.TaskID]*api.TaskInfo{},
		SubJobs:         map[api.SubJobID]*api.SubJobInfo{},
		TaskToSubJob:    map[api.TaskID]api.SubJobID{},
	}
}

// Group slack (1) caps safe evictions even when jobSurplus (2) would allow more.
func TestCreateJobBundles_SubJobGroupSlackCapsSafe(t *testing.T) {
	const gid = api.SubJobGID("g")
	job := newJobForSubJobBundling(4, map[api.SubJobGID]int32{gid: 2})

	sj1 := newSubJob(gid, "sj1", 2)
	sj2 := newSubJob(gid, "sj2", 2)
	sj3 := newSubJob(gid, "sj3", 2)
	job.SubJobs["sj1"] = sj1
	job.SubJobs["sj2"] = sj2
	job.SubJobs["sj3"] = sj3

	addRunningTaskToSubJob(job, sj1, "t1a", "worker", 100)
	addRunningTaskToSubJob(job, sj1, "t1b", "worker", 101)
	addRunningTaskToSubJob(job, sj2, "t2a", "worker", 102)
	addRunningTaskToSubJob(job, sj2, "t2b", "worker", 103)
	addRunningTaskToSubJob(job, sj3, "t3a", "worker", 104)
	addRunningTaskToSubJob(job, sj3, "t3b", "worker", 105)

	local := []*api.TaskInfo{
		job.Tasks["t1a"], job.Tasks["t2a"], job.Tasks["t3a"],
	}

	bundles := CreateJobBundles(job, local)
	assert.Len(t, bundles, 2)
	assert.Equal(t, BundleSafe, bundles[0].Type)
	assert.Len(t, bundles[0].Tasks, 1)
	assert.Equal(t, api.TaskID("t1a"), bundles[0].Tasks[0].UID, "lowest-priority task should be the single safe eviction")
	assert.Equal(t, BundleWhole, bundles[1].Type)
	assert.Len(t, bundles[1].Tasks, 2)
}

// Role gate and sub-job gate compose: master (no role min) goes safe on m-g
// slack; worker (role-tight, sj_w1 broken) is rejected by role gate alone.
func TestCreateJobBundles_MixedLegacyAndSubJobBothEnforced(t *testing.T) {
	const mg = api.SubJobGID("m-g")
	const wg = api.SubJobGID("w-g")
	job := newJobForSubJobBundling(5, map[api.SubJobGID]int32{mg: 1, wg: 2})
	job.TaskMinAvailable = map[string]int32{"worker": 5}
	job.TaskMinAvailableTotal = 5

	sjM1 := newSubJob(mg, "sj_m1", 1)
	sjM2 := newSubJob(mg, "sj_m2", 1)
	sjW1 := newSubJob(wg, "sj_w1", 2)
	sjW2 := newSubJob(wg, "sj_w2", 2)
	sjW3 := newSubJob(wg, "sj_w3", 2)
	job.SubJobs["sj_m1"] = sjM1
	job.SubJobs["sj_m2"] = sjM2
	job.SubJobs["sj_w1"] = sjW1
	job.SubJobs["sj_w2"] = sjW2
	job.SubJobs["sj_w3"] = sjW3

	addRunningTaskToSubJob(job, sjM1, "m1", "master", 0)
	addRunningTaskToSubJob(job, sjM2, "m2", "master", 10)
	addRunningTaskToSubJob(job, sjW1, "w1a", "worker", 1)
	addRunningTaskToSubJob(job, sjW2, "w2a", "worker", 11)
	addRunningTaskToSubJob(job, sjW2, "w2b", "worker", 12)
	addRunningTaskToSubJob(job, sjW3, "w3a", "worker", 13)
	addRunningTaskToSubJob(job, sjW3, "w3b", "worker", 14)

	local := []*api.TaskInfo{job.Tasks["m1"], job.Tasks["w1a"]}

	bundles := CreateJobBundles(job, local)
	assert.Len(t, bundles, 2)
	assert.Equal(t, BundleSafe, bundles[0].Type)
	assert.Len(t, bundles[0].Tasks, 1)
	assert.Equal(t, api.TaskID("m1"), bundles[0].Tasks[0].UID)
	assert.Equal(t, BundleWhole, bundles[1].Type)
	assert.Len(t, bundles[1].Tasks, 1)
	assert.Equal(t, api.TaskID("w1a"), bundles[1].Tasks[0].UID)
}

// Already-broken sub-job stays evictable even when group slack is 0; ready
// sub-jobs in the same group are not.
func TestCreateJobBundles_AlreadyBrokenSubJobIsSubjectToFurtherEviction(t *testing.T) {
	const gid = api.SubJobGID("g")
	job := newJobForSubJobBundling(3, map[api.SubJobGID]int32{gid: 2})

	sj1 := newSubJob(gid, "sj1", 2)
	sj2 := newSubJob(gid, "sj2", 2)
	sj3 := newSubJob(gid, "sj3", 2)
	job.SubJobs["sj1"] = sj1
	job.SubJobs["sj2"] = sj2
	job.SubJobs["sj3"] = sj3

	addRunningTaskToSubJob(job, sj1, "sj1a", "worker", 1)
	addRunningTaskToSubJob(job, sj2, "sj2a", "worker", 2)
	addRunningTaskToSubJob(job, sj2, "sj2b", "worker", 3)
	addRunningTaskToSubJob(job, sj3, "sj3a", "worker", 4)
	addRunningTaskToSubJob(job, sj3, "sj3b", "worker", 5)

	local := []*api.TaskInfo{job.Tasks["sj1a"], job.Tasks["sj2a"], job.Tasks["sj3a"]}

	bundles := CreateJobBundles(job, local)
	assert.Len(t, bundles, 2)
	assert.Equal(t, BundleSafe, bundles[0].Type)
	assert.Len(t, bundles[0].Tasks, 1)
	assert.Equal(t, api.TaskID("sj1a"), bundles[0].Tasks[0].UID)
	assert.Equal(t, BundleWhole, bundles[1].Type)
	assert.Len(t, bundles[1].Tasks, 2)
	wholeUIDs := []api.TaskID{bundles[1].Tasks[0].UID, bundles[1].Tasks[1].UID}
	assert.Contains(t, wholeUIDs, api.TaskID("sj2a"))
	assert.Contains(t, wholeUIDs, api.TaskID("sj3a"))
}

// Job already not gang-ready (jobSurplus < 0): every local task goes safe
// regardless of role / sub-job state.
func TestCreateJobBundles_JobNotReadyShortCircuitsAllSafe(t *testing.T) {
	const gid = api.SubJobGID("g")
	job := newJobForSubJobBundling(10, map[api.SubJobGID]int32{gid: 2})

	sj1 := newSubJob(gid, "sj1", 3)
	job.SubJobs["sj1"] = sj1
	addRunningTaskToSubJob(job, sj1, "a", "worker", 0)
	addRunningTaskToSubJob(job, sj1, "b", "worker", 1)

	local := []*api.TaskInfo{job.Tasks["a"], job.Tasks["b"]}
	bundles := CreateJobBundles(job, local)
	assert.Len(t, bundles, 1)
	assert.Equal(t, BundleSafe, bundles[0].Type)
	assert.Len(t, bundles[0].Tasks, 2)
}

// No TaskMinAvailable entries: role gate is vacuously true, tasks are not
// misrejected on a zero-valued lookup for an unmapped role key.
func TestCreateJobBundles_NoTaskMinAvailableTreatsAllRolesAsUnconstrained(t *testing.T) {
	job := &api.JobInfo{
		MinAvailable: 2,
		TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
			api.Running: {},
		},
		Tasks: map[api.TaskID]*api.TaskInfo{},
	}

	local := make([]*api.TaskInfo, 0, 4)
	for i := 0; i < 4; i++ {
		task := &api.TaskInfo{
			UID:      api.TaskID(string(rune('a' + i))),
			TaskRole: "worker",
			Resreq:   (&api.Resource{MilliCPU: 1000}).Clone(),
			Priority: int32(i),
		}
		job.TaskStatusIndex[api.Running][task.UID] = task
		job.Tasks[task.UID] = task
		local = append(local, task)
	}

	// ReadyTaskNum=4, MinAvailable=2 -> jobSurplus=2. No role mins, no sub-jobs:
	// 2 lowest-priority tasks go safe, remaining 2 go whole.
	bundles := CreateJobBundles(job, local)
	assert.Len(t, bundles, 2)
	assert.Equal(t, BundleSafe, bundles[0].Type)
	assert.Len(t, bundles[0].Tasks, 2)
	assert.Equal(t, BundleWhole, bundles[1].Type)
	assert.Len(t, bundles[1].Tasks, 2)
}
