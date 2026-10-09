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

package unschedulable

import (
	"fmt"
	"sync/atomic"
	"testing"

	fwk "k8s.io/kube-scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// churnJobCount is the sustained-fanout population size: 5,000 cached Jobs
// spread evenly over churnKeyGroups, matching the secondary-index design's
// sustained Pod-churn scenario.
const (
	churnJobCount  = 5000
	churnKeyGroups = 100
)

// BenchmarkJobCachePodChurn measures sustained Pod/Delete
// dispatch against 5,000 cached Jobs whose keys are spread deterministically
// across 100 HintKey groups. indexed=false records every Job without HintKeys
// (no JobKeysFn/EventKeysFn, the exact registration shape used by an adapted
// kube-scheduler plugin event); indexed=true lets OnEvent narrow candidates to
// the HintKey groups named by the event. Selectivity is the percentage of groups
// (and therefore of the 5,000 Jobs) the incoming event's EventKeysFn names.
// After timing, the test asserts the callback ran exactly b.N times the
// candidate count that selectivity/indexing implies; a wrong candidate
// selection fails the benchmark instead of only skewing ns/op.
func BenchmarkJobCachePodChurn(b *testing.B) {
	for _, indexed := range []bool{false, true} {
		for _, selectivity := range []int{1, 10, 100} {
			name := fmt.Sprintf("indexed=%t/selectivity=%d%%", indexed, selectivity)
			b.Run(name, func(b *testing.B) {
				benchmarkPodChurn(b, churnJobCount, selectivity, indexed)
			})
		}
	}
}

func benchmarkPodChurn(b *testing.B, jobCount, selectivityPercent int, indexed bool) {
	if jobCount%churnKeyGroups != 0 {
		b.Fatalf("jobCount %d must divide evenly by churnKeyGroups %d", jobCount, churnKeyGroups)
	}
	matchedKeyGroups := churnKeyGroups * selectivityPercent / 100
	if matchedKeyGroups < 1 {
		matchedKeyGroups = 1
	}
	if matchedKeyGroups > churnKeyGroups {
		matchedKeyGroups = churnKeyGroups
	}
	jobsPerKeyGroup := jobCount / churnKeyGroups

	const plugin = "benchmark-churn"
	cache := NewJobCache(DefaultMaxSkipDuration)
	registry := cache.registry
	event := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}

	keyForGroup := func(group int) HintKey {
		return HintKey(fmt.Sprintf("churn-key-group-%d", group))
	}
	keyGroupByJobID := make(map[api.JobID]int, jobCount)

	var callCount int64
	// hintFn reads the rejected task's own request data, as a real HintFn may do
	// when deciding whether an event intersects the Job's rejected demand,
	// instead of measuring an empty callback as a proxy for the real cost.
	hintFn := func(job *api.JobInfo, rejection Rejection, _, _ any) (HintResult, error) {
		atomic.AddInt64(&callCount, 1)
		for _, taskID := range rejection.Tasks {
			if task := job.Tasks[taskID]; task != nil && task.InitResreq != nil {
				_ = task.InitResreq.MilliCPU
			}
		}
		return HintSkip, nil
	}

	if indexed {
		jobKeysFn := func(job *api.JobInfo, _ Rejection) ([]HintKey, error) {
			return []HintKey{keyForGroup(keyGroupByJobID[job.UID])}, nil
		}
		eventKeysFn := func(_, _ any) ([]HintKey, error) {
			keys := make([]HintKey, matchedKeyGroups)
			for i := 0; i < matchedKeyGroups; i++ {
				keys[i] = keyForGroup(i)
			}
			return keys, nil
		}
		registerTestIndexedHint(registry, plugin, event, jobKeysFn, eventKeysFn, hintFn)
	} else {
		registerTestHint(registry, plugin, event, hintFn)
	}

	for i := 0; i < jobCount; i++ {
		jobID := api.JobID(fmt.Sprintf("job-%d", i))
		keyGroupByJobID[jobID] = i % churnKeyGroups
		taskID := api.TaskID(fmt.Sprintf("task-%d", i))
		request := &api.Resource{MilliCPU: 1000}
		task := &api.TaskInfo{
			UID:        taskID,
			Job:        jobID,
			Name:       string(taskID),
			Namespace:  "benchmark",
			Resreq:     request.Clone(),
			InitResreq: request.Clone(),
			NumaInfo:   &api.TopologyInfo{},
			TransactionContext: api.TransactionContext{
				Status: api.Pending,
			},
		}
		job := api.NewJobInfo(jobID, task)
		job.Name = string(jobID)
		job.Namespace = "benchmark"
		cache.Record(job, []Rejection{{
			Plugin: plugin,
			Source: RejectionPredicate,
			Tasks:  []api.TaskID{taskID},
		}})
	}

	expectedCandidates := jobCount
	if indexed {
		expectedCandidates = matchedKeyGroups * jobsPerKeyGroup
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.OnEvent(event, nil, "event")
	}
	b.StopTimer()

	if got, want := atomic.LoadInt64(&callCount), int64(b.N)*int64(expectedCandidates); got != want {
		b.Fatalf("indexed=%t selectivity=%d%%: hint calls = %d, want %d (b.N=%d, candidates=%d)",
			indexed, selectivityPercent, got, want, b.N, expectedCandidates)
	}
}

// BenchmarkJobCacheRecord measures Record's cost of
// building (and, on repeated calls for the same Job, replacing) the bounded
// key index at representative key counts, including the 256-key limit and
// the 257-key case that must dispatch the Job without HintKeys.
func BenchmarkJobCacheRecord(b *testing.B) {
	for _, keyCount := range []int{1, 16, 64, 256, 257} {
		b.Run(fmt.Sprintf("keys=%d", keyCount), func(b *testing.B) {
			benchmarkRecord(b, keyCount)
		})
	}
}

func benchmarkRecord(b *testing.B, keyCount int) {
	const plugin = "benchmark-record"
	cache := NewJobCache(DefaultMaxSkipDuration)
	registry := cache.registry
	event := fwk.ClusterEvent{Resource: fwk.Pod, ActionType: fwk.Delete}

	keys := make([]HintKey, keyCount)
	for i := range keys {
		keys[i] = HintKey(fmt.Sprintf("record-key-%d", i))
	}
	jobKeysFn := func(*api.JobInfo, Rejection) ([]HintKey, error) {
		return append([]HintKey(nil), keys...), nil
	}
	// eventKeysFn always names a key disjoint from every jobKeysFn key above,
	// so the assertion below can distinguish indexed dispatch (never wakes on
	// this event) from dispatch without HintKeys (always evaluates, regardless
	// of the event key) without reaching into cache-internal fields.
	eventKeysFn := func(_, _ any) ([]HintKey, error) {
		return []HintKey{"record-key-disjoint"}, nil
	}
	var hintCalls int
	hintFn := func(job *api.JobInfo, rejection Rejection, _, _ any) (HintResult, error) {
		hintCalls++
		for _, taskID := range rejection.Tasks {
			if task := job.Tasks[taskID]; task != nil && task.InitResreq != nil {
				_ = task.InitResreq.MilliCPU
			}
		}
		return HintSkip, nil
	}
	registerTestIndexedHint(registry, plugin, event, jobKeysFn, eventKeysFn, hintFn)

	// One stable Job snapshot is recorded (and, from the second iteration on,
	// replaced) repeatedly so the benchmark measures both fresh-record and
	// replace-record cost at this key count.
	taskID := api.TaskID("task-0")
	request := &api.Resource{MilliCPU: 1000, Memory: 1024}
	task := &api.TaskInfo{
		UID:        taskID,
		Job:        "benchmark/job",
		Name:       string(taskID),
		Namespace:  "benchmark",
		Resreq:     request.Clone(),
		InitResreq: request.Clone(),
		NumaInfo:   &api.TopologyInfo{},
		TransactionContext: api.TransactionContext{
			Status: api.Pending,
		},
	}
	job := api.NewJobInfo("benchmark/job", task)
	job.Name = "job"
	job.Namespace = "benchmark"
	rejections := []Rejection{{Plugin: plugin, Source: RejectionPredicate, Tasks: []api.TaskID{taskID}}}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Record(job, rejections)
	}
	b.StopTimer()

	hintCalls = 0
	cache.OnEvent(event, nil, "event")
	wantDispatchWithoutHintKeys := keyCount > MaxHintKeysPerPluginEvent
	gotDispatchWithoutHintKeys := hintCalls == 1
	if gotDispatchWithoutHintKeys != wantDispatchWithoutHintKeys {
		b.Fatalf("keys=%d: dispatch without HintKeys = %v (hint calls %d), want %v",
			keyCount, gotDispatchWithoutHintKeys, hintCalls, wantDispatchWithoutHintKeys)
	}
}
