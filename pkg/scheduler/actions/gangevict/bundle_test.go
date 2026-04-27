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
