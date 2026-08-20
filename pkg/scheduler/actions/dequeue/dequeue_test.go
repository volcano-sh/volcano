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

package dequeue

import (
	"testing"

	v1 "k8s.io/api/core/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	enqueueaction "volcano.sh/volcano/pkg/scheduler/actions/enqueue"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestDequeueUnschedulableInqueueJob(t *testing.T) {
	options.Default()
	test := uthelper.TestCommonStruct{
		Plugins: dequeuePlugins(),
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroup("stuck", "default", "queue", 1, nil, schedulingv1beta1.PodGroupInqueue),
		},
		Pods: []*v1.Pod{
			util.BuildPod("default", "stuck-task", "", v1.PodPending,
				api.BuildResourceList("1", "1Gi"), "stuck", nil, nil),
		},
		Queues: []*schedulingv1beta1.Queue{
			util.BuildQueue("queue", 1, nil),
		},
	}

	ssn := test.RegisterSession(dequeueTiers(), nil)
	schedulerCache := test.SchedulerCache()
	closed := false
	defer func() {
		if !closed {
			test.Close()
		}
	}()

	New().Execute(ssn)

	job := ssn.Jobs[api.JobID("default/stuck")]
	if job.PodGroup.Status.Phase != scheduling.PodGroupPending {
		t.Fatalf("job phase = %v, want %v", job.PodGroup.Status.Phase, scheduling.PodGroupPending)
	}
	if _, found := schedulerCache.Snapshot().Jobs[job.UID]; !found {
		t.Fatal("dequeued job was excluded from the next scheduling snapshot")
	}
	if !schedulerCache.IsJobUnschedulable(job.UID) {
		t.Fatal("dequeued job was not added to the unschedulable job cache")
	}

	// Verify CloseSession's job updater preserves the explicit rollback.
	test.Close()
	closed = true
	if job.PodGroup.Status.Phase != scheduling.PodGroupPending {
		t.Fatalf("job phase after CloseSession = %v, want %v", job.PodGroup.Status.Phase, scheduling.PodGroupPending)
	}
	if _, found := schedulerCache.Snapshot().Jobs[job.UID]; !found {
		t.Fatal("dequeued job was excluded from a scheduling snapshot after CloseSession")
	}
	if !schedulerCache.IsJobUnschedulable(job.UID) {
		t.Fatal("dequeued job was removed from the unschedulable job cache after CloseSession")
	}
}

func TestDequeueSkipsIneligibleJobs(t *testing.T) {
	options.Default()
	test := uthelper.TestCommonStruct{
		Plugins: dequeuePlugins(),
		Nodes: []*v1.Node{
			util.BuildNode("node1", api.BuildResourceList("4", "4Gi",
				api.ScalarResource{Name: string(v1.ResourcePods), Value: "10"}), nil),
		},
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroup("ready", "default", "queue", 1, nil, schedulingv1beta1.PodGroupInqueue),
			util.BuildPodGroup("pipelined", "default", "queue", 1, nil, schedulingv1beta1.PodGroupInqueue),
			util.BuildPodGroup("no-pods", "default", "queue", 1, nil, schedulingv1beta1.PodGroupInqueue),
			util.BuildPodGroup("pending", "default", "queue", 1, nil, schedulingv1beta1.PodGroupPending),
			util.BuildPodGroup("running-phase", "default", "queue", 1, nil, schedulingv1beta1.PodGroupRunning),
			util.BuildPodGroup("orphan", "default", "queue", 1, nil, schedulingv1beta1.PodGroupInqueue),
		},
		Pods: []*v1.Pod{
			util.BuildPod("default", "ready-task", "node1", v1.PodRunning,
				api.BuildResourceList("1", "1Gi"), "ready", nil, nil),
			util.BuildPod("default", "pipelined-task", "", v1.PodPending,
				api.BuildResourceList("1", "1Gi"), "pipelined", nil, nil),
			util.BuildPod("default", "pending-task", "", v1.PodPending,
				api.BuildResourceList("1", "1Gi"), "pending", nil, nil),
			util.BuildPod("default", "running-phase-task", "", v1.PodPending,
				api.BuildResourceList("1", "1Gi"), "running-phase", nil, nil),
			util.BuildPod("default", "orphan-task", "", v1.PodPending,
				api.BuildResourceList("1", "1Gi"), "orphan", nil, nil),
		},
		Queues: []*schedulingv1beta1.Queue{
			util.BuildQueue("queue", 1, nil),
		},
	}

	ssn := test.RegisterSession(dequeueTiers(), nil)
	defer test.Close()

	ssn.Jobs[api.JobID("default/orphan")].Queue = api.QueueID("missing")
	pipelinedJob := ssn.Jobs[api.JobID("default/pipelined")]
	for _, task := range pipelinedJob.TaskStatusIndex[api.Pending] {
		pipelinedJob.UpdateTaskStatus(task, api.Pipelined)
	}

	New().Execute(ssn)

	expected := map[api.JobID]scheduling.PodGroupPhase{
		"default/ready":         scheduling.PodGroupInqueue,
		"default/pipelined":     scheduling.PodGroupInqueue,
		"default/no-pods":       scheduling.PodGroupInqueue,
		"default/pending":       scheduling.PodGroupPending,
		"default/running-phase": scheduling.PodGroupRunning,
		"default/orphan":        scheduling.PodGroupInqueue,
	}
	for jobID, phase := range expected {
		job := ssn.Jobs[jobID]
		if job == nil {
			t.Fatalf("job %q not found", jobID)
		}
		if job.PodGroup.Status.Phase != phase {
			t.Errorf("job %q phase = %v, want %v", jobID, job.PodGroup.Status.Phase, phase)
		}
	}

	schedulerCache := test.SchedulerCache()
	for _, jobID := range []api.JobID{
		"default/ready",
		"default/pipelined",
		"default/no-pods",
		"default/pending",
		"default/running-phase",
		"default/orphan",
	} {
		if schedulerCache.IsJobUnschedulable(jobID) {
			t.Errorf("ineligible job %q was added to the unschedulable job cache", jobID)
		}
	}
}

func TestDequeueReleasesPartialReservations(t *testing.T) {
	options.Default()
	test := uthelper.TestCommonStruct{
		Plugins: dequeuePlugins(),
		Nodes: []*v1.Node{
			util.BuildNode("node1", api.BuildResourceList("4", "4Gi",
				api.ScalarResource{Name: string(v1.ResourcePods), Value: "10"}), nil),
		},
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroup("partial", "default", "queue", 3, nil, schedulingv1beta1.PodGroupInqueue),
		},
		Pods: []*v1.Pod{
			util.BuildPod("default", "partial-allocated", "", v1.PodPending,
				api.BuildResourceList("1", "1Gi"), "partial", nil, nil),
			util.BuildPod("default", "partial-pipelined", "", v1.PodPending,
				api.BuildResourceList("1", "1Gi"), "partial", nil, nil),
			util.BuildPod("default", "partial-pending", "", v1.PodPending,
				api.BuildResourceList("1", "1Gi"), "partial", nil, nil),
		},
		Queues: []*schedulingv1beta1.Queue{
			util.BuildQueue("queue", 1, nil),
		},
	}

	ssn := test.RegisterSession(dequeueTiers(), nil)
	defer test.Close()

	job := ssn.Jobs[api.JobID("default/partial")]
	node := ssn.Nodes["node1"]
	stmt := framework.NewStatement(ssn)
	if err := stmt.Allocate(job.Tasks[api.TaskID("default-partial-allocated")], node); err != nil {
		t.Fatalf("failed to allocate task: %v", err)
	}
	if err := stmt.Pipeline(job.Tasks[api.TaskID("default-partial-pipelined")], node.Name, false); err != nil {
		t.Fatalf("failed to pipeline task: %v", err)
	}
	if got := node.Idle.Get(v1.ResourceCPU); got != 3000 {
		t.Fatalf("node idle CPU before dequeue = %v, want 3000", got)
	}
	if got := node.Pipelined.Get(v1.ResourceCPU); got != 1000 {
		t.Fatalf("node pipelined CPU before dequeue = %v, want 1000", got)
	}

	deallocated := 0
	ssn.AddEventHandler(&framework.EventHandler{
		DeallocateFunc: func(*framework.Event) {
			deallocated++
		},
	})

	New().Execute(ssn)

	if job.PodGroup.Status.Phase != scheduling.PodGroupPending {
		t.Fatalf("job phase = %v, want %v", job.PodGroup.Status.Phase, scheduling.PodGroupPending)
	}
	schedulerCache := test.SchedulerCache()
	if _, found := schedulerCache.Snapshot().Jobs[job.UID]; !found {
		t.Fatal("dequeued job with released reservations was excluded from the next snapshot")
	}
	if !schedulerCache.IsJobUnschedulable(job.UID) {
		t.Fatal("dequeued job with released reservations was not cached")
	}
	if len(job.TaskStatusIndex[api.Allocated]) != 0 || len(job.TaskStatusIndex[api.Pipelined]) != 0 {
		t.Fatalf("reserved tasks remain: allocated=%d, pipelined=%d",
			len(job.TaskStatusIndex[api.Allocated]), len(job.TaskStatusIndex[api.Pipelined]))
	}
	if len(job.TaskStatusIndex[api.Pending]) != 3 {
		t.Fatalf("pending task count = %d, want 3", len(job.TaskStatusIndex[api.Pending]))
	}
	if len(node.Tasks) != 0 {
		t.Fatalf("node task count = %d, want 0", len(node.Tasks))
	}
	if got := node.Idle.Get(v1.ResourceCPU); got != 4000 {
		t.Fatalf("node idle CPU after dequeue = %v, want 4000", got)
	}
	if got := node.Pipelined.Get(v1.ResourceCPU); got != 0 {
		t.Fatalf("node pipelined CPU after dequeue = %v, want 0", got)
	}
	if deallocated != 2 {
		t.Fatalf("deallocate handler calls = %d, want 2", deallocated)
	}
	for _, task := range job.Tasks {
		if task.NodeName != "" {
			t.Errorf("task %q node = %q, want empty", task.Name, task.NodeName)
		}
	}
}

func TestDequeuedJobYieldsNextSessionQueueQuota(t *testing.T) {
	options.Default()
	waitingPodGroup := util.BuildPodGroupWithMinResources(
		"b-waiting", "default", "queue", 1, nil,
		api.BuildResourceList("1", "1Gi"), schedulingv1beta1.PodGroupPending)
	firstSession := uthelper.TestCommonStruct{
		Plugins: dequeuePlugins(),
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroupWithMinResources(
				"a-stuck", "default", "queue", 2, nil,
				api.BuildResourceList("1", "1Gi"), schedulingv1beta1.PodGroupInqueue),
			waitingPodGroup,
		},
		Pods: []*v1.Pod{
			util.BuildPod("default", "stuck-task", "", v1.PodPending,
				api.BuildResourceList("1", "1Gi"), "a-stuck", nil, nil),
		},
		Nodes: []*v1.Node{
			util.BuildNode("node", api.BuildResourceList("1", "1Gi"), nil),
		},
		Queues: []*schedulingv1beta1.Queue{
			util.BuildQueue("queue", 1, api.BuildResourceList("1", "1Gi")),
		},
	}

	ssn := firstSession.RegisterSession(dequeueTiers(), nil)
	firstClosed := false
	defer func() {
		if !firstClosed {
			firstSession.Close()
		}
	}()

	New().Execute(ssn)
	schedulerCache := firstSession.SchedulerCache()
	firstSession.Close()
	firstClosed = true

	enabled := true
	uthelper.RegisterPlugins(map[string]framework.PluginBuilder{
		proportion.PluginName: proportion.New,
	})
	defer framework.CleanupPluginBuilders()
	next := framework.OpenSession(schedulerCache, []conf.Tier{{
		Plugins: []conf.PluginOption{{
			Name:               proportion.PluginName,
			EnabledQueueOrder:  &enabled,
			EnabledJobEnqueued: &enabled,
		}},
	}}, nil)
	defer framework.CloseSession(next)

	enqueueaction.New().Execute(next)

	stuck := next.Jobs[api.JobID("default/a-stuck")]
	if stuck == nil {
		t.Fatal("dequeued job was excluded from the next session")
	}
	if phase := stuck.PodGroup.Status.Phase; phase != scheduling.PodGroupPending {
		t.Fatalf("dequeued job phase in next session = %v, want %v", phase, scheduling.PodGroupPending)
	}
	if phase := next.Jobs[api.JobID("default/b-waiting")].PodGroup.Status.Phase; phase != scheduling.PodGroupInqueue {
		t.Fatalf("waiting job phase in next session = %v, want %v", phase, scheduling.PodGroupInqueue)
	}
}

func dequeuePlugins() map[string]framework.PluginBuilder {
	return map[string]framework.PluginBuilder{
		gang.PluginName: gang.New,
	}
}

func dequeueTiers() []conf.Tier {
	enabled := true
	return []conf.Tier{{
		Plugins: []conf.PluginOption{{
			Name:                gang.PluginName,
			EnabledJobReady:     &enabled,
			EnabledJobPipelined: &enabled,
		}},
	}}
}
