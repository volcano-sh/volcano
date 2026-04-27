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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

// testDomainHyperNode registers nodes under a hypernode name in ssn.RealNodesList and returns a
// minimal HyperNodeInfo for BuildNominationPlanInDomain.
func testDomainHyperNode(ssn *framework.Session, name string, nodes []*api.NodeInfo) *api.HyperNodeInfo {
	if ssn.RealNodesList == nil {
		ssn.RealNodesList = map[string][]*api.NodeInfo{}
	}
	ssn.RealNodesList[name] = nodes
	return &api.HyperNodeInfo{Name: name}
}

func TestBuildNominationPlanInDomain_Guards(t *testing.T) {
	plan, ok := BuildNominationPlanInDomain(nil, nil, nil, nil, nil, "x")
	assert.False(t, ok)
	assert.Nil(t, plan)

	jobID := api.JobID("ns/guard-job")
	task := makeTask(jobID, "t1")
	job := api.NewJobInfo(jobID, task)
	node := api.NewNodeInfo(nil)
	node.Name = "n1"
	node.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
	node.Releasing = api.EmptyResource()
	node.Pipelined = api.EmptyResource()
	ssn := &framework.Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobID: job},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}
	plan, ok = BuildNominationPlanInDomain(ssn, nil, job, nil, nil, "x")
	assert.False(t, ok)
	assert.Nil(t, plan)
}

func TestCollectPendingTasks_SubJobAwareOrder(t *testing.T) {
	jobID := api.JobID("ns/job1")
	t1 := makeTask(jobID, "t1")
	t2 := makeTask(jobID, "t2")
	t3 := makeTask(jobID, "t3")
	job := api.NewJobInfo(jobID, t1, t2, t3)
	job.SubJobs = map[api.SubJobID]*api.SubJobInfo{
		"sj-late": {
			UID:        "sj-late",
			MatchIndex: 2,
			TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
				api.Pending: {t1.UID: t1},
			},
		},
		"sj-early": {
			UID:        "sj-early",
			MatchIndex: 1,
			TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
				api.Pending: {t3.UID: t3, t2.UID: t2},
			},
		},
	}

	out := collectPendingTasks(job, nil, nil)
	assert.Equal(t, 3, len(out))
	assert.Equal(t, api.TaskID("t2"), out[0].UID)
	assert.Equal(t, api.TaskID("t3"), out[1].UID)
	assert.Equal(t, api.TaskID("t1"), out[2].UID)
}

func TestCollectPendingTasks_FallbackIncludesPendingOutsideSubJobs(t *testing.T) {
	jobID := api.JobID("ns/job-fallback")
	t1 := makeTask(jobID, "t1")
	t2 := makeTask(jobID, "t2")
	job := api.NewJobInfo(jobID, t1, t2)
	job.SubJobs = map[api.SubJobID]*api.SubJobInfo{
		"sj-only-t1": {
			UID:        "sj-only-t1",
			MatchIndex: 1,
			TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
				api.Pending: {t1.UID: t1},
			},
		},
	}

	out := collectPendingTasks(job, nil, nil)
	assert.Equal(t, 2, len(out))
	assert.Equal(t, api.TaskID("t1"), out[0].UID)
	assert.ElementsMatch(t, []api.TaskID{"t1", "t2"}, []api.TaskID{out[0].UID, out[1].UID})
}

func TestCollectPendingTasksForJobStartup_WorksheetOrder(t *testing.T) {
	jobID := api.JobID("ns/startup-pending")
	t1 := makeTask(jobID, "t1")
	t2 := makeTask(jobID, "t2")
	job := api.NewJobInfo(jobID, t1, t2)
	job.SubJobs = map[api.SubJobID]*api.SubJobInfo{
		"sj-late": {
			UID:        "sj-late",
			GID:        "g1",
			MatchIndex: 2,
			TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
				api.Pending: {t2.UID: t2},
			},
		},
		"sj-early": {
			UID:        "sj-early",
			GID:        "g1",
			MatchIndex: 1,
			TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
				api.Pending: {t1.UID: t1},
			},
		},
	}
	ssn := &framework.Session{}
	out := CollectPendingTasksForJobStartup(ssn, job, nil, nil)
	assert.Equal(t, 2, len(out))
	assert.Equal(t, api.TaskID("t1"), out[0].UID)
	assert.Equal(t, api.TaskID("t2"), out[1].UID)
}

func TestCollectPendingTasksForRunningJob_FirstWorksheetSubJobOnly(t *testing.T) {
	jobID := api.JobID("ns/running-subjob")
	t1 := makeTask(jobID, "t1")
	t2 := makeTask(jobID, "t2")
	job := api.NewJobInfo(jobID, t1, t2)
	job.SubJobs = map[api.SubJobID]*api.SubJobInfo{
		"sj-late": {
			UID:        "sj-late",
			GID:        "g1",
			MatchIndex: 2,
			TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
				api.Pending: {t2.UID: t2},
			},
		},
		"sj-early": {
			UID:        "sj-early",
			GID:        "g1",
			MatchIndex: 1,
			TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
				api.Pending: {t1.UID: t1},
			},
		},
	}
	ssn := &framework.Session{}
	out := CollectPendingTasksForRunningJob(ssn, job)
	assert.Equal(t, 1, len(out))
	assert.Equal(t, api.TaskID("t1"), out[0].UID)
}

func TestCollectPendingTasksForJobStartup_EmptyWorksheetReturnsNil(t *testing.T) {
	jobID := api.JobID("ns/empty-ws")
	job := api.NewJobInfo(jobID)
	ssn := &framework.Session{}
	assert.Nil(t, CollectPendingTasksForJobStartup(ssn, job, nil, nil))
}

func TestCollectPendingTasks_FiltersGatedAndBestEffort(t *testing.T) {
	jobID := api.JobID("ns/job-filter")
	gated := makeTask(jobID, "gated")
	gated.SchGated = true
	bestEffort := makeTask(jobID, "be")
	bestEffort.Resreq = api.EmptyResource()
	normal := makeTask(jobID, "normal")
	job := api.NewJobInfo(jobID, gated, bestEffort, normal)

	out := collectPendingTasks(job, nil, nil)
	assert.Equal(t, 1, len(out))
	assert.Equal(t, api.TaskID("normal"), out[0].UID)
}

func TestBuildNominationPlanInDomain_Success(t *testing.T) {
	ensureServerOptsForTest()
	jobID := api.JobID("ns/job2")
	task := makeTask(jobID, "t1")
	job := api.NewJobInfo(jobID, task)
	node := api.NewNodeInfo(nil)
	node.Name = "n1"
	node.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
	node.Releasing = api.EmptyResource()
	node.Pipelined = api.EmptyResource()

	ssn := &framework.Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobID: job},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}
	hn := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, []*api.NodeInfo{node})
	plan, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test")
	assert.True(t, ok)
	assert.NotNil(t, plan)
	assert.Equal(t, 1, len(plan.Operations()))
}

func TestBuildNominationPlanInDomain_WithVictimIncludesEvictAndPipeline(t *testing.T) {
	ensureServerOptsForTest()
	jobID := api.JobID("ns/job4")
	task := makeTask(jobID, "t1")
	job := api.NewJobInfo(jobID, task)

	victimJobID := api.JobID("ns/victim-job")
	victim := makeTask(victimJobID, "v1")
	victim.Status = api.Running
	victim.NodeName = "n1"
	victimJob := api.NewJobInfo(victimJobID, victim)

	node := api.NewNodeInfo(nil)
	node.Name = "n1"
	node.Idle = (&api.Resource{MilliCPU: 3000}).Clone()
	node.Releasing = api.EmptyResource()
	node.Pipelined = api.EmptyResource()
	assert.NoError(t, node.AddTask(victim))

	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			jobID:       job,
			victimJobID: victimJob,
		},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}
	hn := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, []*api.NodeInfo{node})
	plan, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, []*api.TaskInfo{victim}, "test")
	assert.True(t, ok)
	assert.NotNil(t, plan)
	assert.Equal(t, 2, len(plan.Operations()))
}

func TestBuildNominationPlanInDomain_NoFeasibleNode(t *testing.T) {
	ensureServerOptsForTest()
	jobID := api.JobID("ns/job3")
	task := makeTask(jobID, "t1")
	job := api.NewJobInfo(jobID, task)
	node := api.NewNodeInfo(nil)
	node.Name = "n1"
	node.Idle = (&api.Resource{MilliCPU: 0}).Clone()
	node.Releasing = api.EmptyResource()
	node.Pipelined = api.EmptyResource()

	ssn := &framework.Session{
		Jobs:  map[api.JobID]*api.JobInfo{jobID: job},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}
	hn := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, []*api.NodeInfo{node})
	plan, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test")
	assert.False(t, ok)
	assert.Nil(t, plan)
}

func TestBuildNominationPlanInDomain_PipelineFailureRollsBackEviction(t *testing.T) {
	ensureServerOptsForTest()
	options.ServerOpts.MinNodesToFind = 1

	jobID := api.JobID("ns/job-pipeline-fail")
	t1 := makeTask(jobID, "t1")
	t1.NodeName = "other-node" // force Statement.Pipeline to fail on n1
	job := api.NewJobInfo(jobID, t1)

	victimJobID := api.JobID("ns/victim-job-fail")
	victim := makeTask(victimJobID, "v1")
	victim.Status = api.Running
	victim.NodeName = "n1"
	victimJob := api.NewJobInfo(victimJobID, victim)

	node := api.NewNodeInfo(nil)
	node.Name = "n1"
	node.Idle = (&api.Resource{MilliCPU: 1000}).Clone()
	node.Releasing = api.EmptyResource()
	node.Pipelined = api.EmptyResource()
	assert.NoError(t, node.AddTask(victim))
	ghostNode := api.NewNodeInfo(nil)
	ghostNode.Name = "ghost-node"
	ghostNode.Idle = (&api.Resource{MilliCPU: 1000}).Clone()
	ghostNode.Releasing = api.EmptyResource()
	ghostNode.Pipelined = api.EmptyResource()

	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			jobID:       job,
			victimJobID: victimJob,
		},
		Nodes: map[string]*api.NodeInfo{node.Name: node},
	}
	hn := testDomainHyperNode(ssn, "sim-domain", []*api.NodeInfo{ghostNode})
	plan, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, []*api.TaskInfo{victim}, "test")
	assert.False(t, ok)
	assert.Nil(t, plan)

	// Dry-run must rollback temporary victim eviction when all pipeline attempts fail.
	assert.Len(t, node.Tasks, 1)
	_, victimStillOnNode := node.Tasks[api.PodKey(victim.Pod)]
	assert.True(t, victimStillOnNode)
	assert.Equal(t, api.Running, victim.Status)
	assert.Equal(t, "", t1.NodeName)
	assert.Equal(t, 1, len(job.TaskStatusIndex[api.Pending]))
	assert.Equal(t, 0, len(job.TaskStatusIndex[api.Pipelined]))
}

func TestBuildNominationPlanInDomain_PrefersIdleNodeOverFutureIdle(t *testing.T) {
	ensureServerOptsForTest()
	options.ServerOpts.MinNodesToFind = 2

	jobID := api.JobID("ns/job5")
	task := makeTask(jobID, "t1")
	job := api.NewJobInfo(jobID, task)

	futureNode := api.NewNodeInfo(nil)
	futureNode.Name = "n-future"
	futureNode.Idle = (&api.Resource{MilliCPU: 0}).Clone()
	futureNode.Releasing = (&api.Resource{MilliCPU: 2000}).Clone()
	futureNode.Pipelined = api.EmptyResource()

	idleNode := api.NewNodeInfo(nil)
	idleNode.Name = "n-idle"
	idleNode.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
	idleNode.Releasing = api.EmptyResource()
	idleNode.Pipelined = api.EmptyResource()

	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			jobID: job,
		},
		Nodes: map[string]*api.NodeInfo{
			futureNode.Name: futureNode,
			idleNode.Name:   idleNode,
		},
	}

	hn := testDomainHyperNode(ssn, "sim-domain", []*api.NodeInfo{futureNode, idleNode})
	plan, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test")
	assert.True(t, ok)
	assert.NotNil(t, plan)

	stmt := framework.NewStatement(ssn)
	assert.NoError(t, stmt.RecoverOperations(plan))

	_, onIdle := idleNode.Tasks[api.PodKey(task.Pod)]
	_, onFuture := futureNode.Tasks[api.PodKey(task.Pod)]
	assert.True(t, onIdle)
	assert.False(t, onFuture)
}

func TestBuildNominationPlanInDomain_NominatedHintDoesNotShortCircuitPredicate(t *testing.T) {
	ensureServerOptsForTest()
	options.ServerOpts.MinNodesToFind = 2

	jobID := api.JobID("ns/job-nominated")
	task := makeTask(jobID, "t1")
	task.Pod.Status.NominatedNodeName = "n-nominated"
	job := api.NewJobInfo(jobID, task)

	nominated := api.NewNodeInfo(nil)
	nominated.Name = "n-nominated"
	nominated.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
	nominated.Releasing = api.EmptyResource()
	nominated.Pipelined = api.EmptyResource()

	other := api.NewNodeInfo(nil)
	other.Name = "n-other"
	other.Idle = (&api.Resource{MilliCPU: 3000}).Clone()
	other.Releasing = api.EmptyResource()
	other.Pipelined = api.EmptyResource()

	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			jobID: job,
		},
		Nodes: map[string]*api.NodeInfo{
			nominated.Name: nominated,
			other.Name:     other,
		},
	}

	hn := testDomainHyperNode(ssn, "sim-domain", []*api.NodeInfo{other, nominated})
	plan, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test")
	assert.True(t, ok)
	assert.NotNil(t, plan)
	stmt := framework.NewStatement(ssn)
	assert.NoError(t, stmt.RecoverOperations(plan))

	_, onNominated := nominated.Tasks[api.PodKey(task.Pod)]
	_, onOther := other.Tasks[api.PodKey(task.Pod)]
	// Nomination is not a fast-path; placement follows preempt predicate + node scoring over the full
	// hypernode candidate list (either node may win depending on predicate helper ordering).
	assert.True(t, onNominated != onOther)
}

func TestBuildNominationPlanInDomain_IgnoresNominatedNodeOutsideDomain(t *testing.T) {
	ensureServerOptsForTest()
	options.ServerOpts.MinNodesToFind = 1

	jobID := api.JobID("ns/job-nominated-outside-domain")
	task := makeTask(jobID, "t1")
	task.Pod.Status.NominatedNodeName = "n-nominated"
	job := api.NewJobInfo(jobID, task)

	nominated := api.NewNodeInfo(nil)
	nominated.Name = "n-nominated"
	nominated.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
	nominated.Releasing = api.EmptyResource()
	nominated.Pipelined = api.EmptyResource()

	inDomain := api.NewNodeInfo(nil)
	inDomain.Name = "n-domain"
	inDomain.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
	inDomain.Releasing = api.EmptyResource()
	inDomain.Pipelined = api.EmptyResource()

	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			jobID: job,
		},
		Nodes: map[string]*api.NodeInfo{
			nominated.Name: nominated,
			inDomain.Name:  inDomain,
		},
	}

	hn := testDomainHyperNode(ssn, "sim-domain", []*api.NodeInfo{inDomain})
	plan, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test")
	assert.True(t, ok)
	assert.NotNil(t, plan)
	stmt := framework.NewStatement(ssn)
	assert.NoError(t, stmt.RecoverOperations(plan))

	_, onNominated := nominated.Tasks[api.PodKey(task.Pod)]
	_, onDomain := inDomain.Tasks[api.PodKey(task.Pod)]
	assert.False(t, onNominated)
	assert.True(t, onDomain)
}

func TestBuildNominationPlanInDomain_FallbackWhenNominatedMissing(t *testing.T) {
	ensureServerOptsForTest()
	options.ServerOpts.MinNodesToFind = 1

	jobID := api.JobID("ns/job-nominated-fallback")
	task := makeTask(jobID, "t1")
	task.Pod.Status.NominatedNodeName = "n-missing"
	job := api.NewJobInfo(jobID, task)

	node := api.NewNodeInfo(nil)
	node.Name = "n1"
	node.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
	node.Releasing = api.EmptyResource()
	node.Pipelined = api.EmptyResource()

	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			jobID: job,
		},
		Nodes: map[string]*api.NodeInfo{
			node.Name: node,
		},
	}

	hn := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, []*api.NodeInfo{node})
	plan, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test")
	assert.True(t, ok)
	assert.NotNil(t, plan)
}

func TestBuildNominationPlanInDomain_FallbackWhenNominatedInfeasible(t *testing.T) {
	ensureServerOptsForTest()
	options.ServerOpts.MinNodesToFind = 2

	jobID := api.JobID("ns/job-nominated-infeasible")
	task := makeTask(jobID, "t1")
	task.Pod.Status.NominatedNodeName = "n-nominated"
	job := api.NewJobInfo(jobID, task)

	nominated := api.NewNodeInfo(nil)
	nominated.Name = "n-nominated"
	nominated.Idle = api.EmptyResource()
	nominated.Releasing = api.EmptyResource() // infeasible for task
	nominated.Pipelined = api.EmptyResource()

	other := api.NewNodeInfo(nil)
	other.Name = "n-other"
	other.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
	other.Releasing = api.EmptyResource()
	other.Pipelined = api.EmptyResource()

	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			jobID: job,
		},
		Nodes: map[string]*api.NodeInfo{
			nominated.Name: nominated,
			other.Name:     other,
		},
	}

	hn := testDomainHyperNode(ssn, "sim-domain", []*api.NodeInfo{nominated, other})
	plan, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test")
	assert.True(t, ok)
	assert.NotNil(t, plan)
	stmt := framework.NewStatement(ssn)
	assert.NoError(t, stmt.RecoverOperations(plan))

	_, onNominated := nominated.Tasks[api.PodKey(task.Pod)]
	_, onOther := other.Tasks[api.PodKey(task.Pod)]
	assert.False(t, onNominated)
	assert.True(t, onOther)
}

func TestPickBestNodeForTask_PrefersIdleGroup(t *testing.T) {
	task := makeTask(api.JobID("ns/job6"), "t1")
	idleNode := api.NewNodeInfo(nil)
	idleNode.Name = "n-idle"
	idleNode.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
	idleNode.Releasing = api.EmptyResource()
	idleNode.Pipelined = api.EmptyResource()

	futureNode := api.NewNodeInfo(nil)
	futureNode.Name = "n-future"
	futureNode.Idle = api.EmptyResource()
	futureNode.Releasing = (&api.Resource{MilliCPU: 3000}).Clone()
	futureNode.Pipelined = api.EmptyResource()

	best := prioritizeNodesForSimulate(&framework.Session{}, task, []*api.NodeInfo{futureNode, idleNode})
	if assert.NotNil(t, best) {
		assert.Equal(t, "n-idle", best.Name)
	}
}

func TestPickBestNodeForTask_FallsBackFutureIdleGroup(t *testing.T) {
	task := makeTask(api.JobID("ns/job7"), "t1")
	futureNode := api.NewNodeInfo(nil)
	futureNode.Name = "n-future"
	futureNode.Idle = api.EmptyResource()
	futureNode.Releasing = (&api.Resource{MilliCPU: 2000}).Clone()
	futureNode.Pipelined = api.EmptyResource()

	best := prioritizeNodesForSimulate(&framework.Session{}, task, []*api.NodeInfo{futureNode})
	if assert.NotNil(t, best) {
		assert.Equal(t, "n-future", best.Name)
	}
}

func TestCollectPendingTasks_UsesProvidedOrderFns(t *testing.T) {
	jobID := api.JobID("ns/job-order")
	t1 := makeTask(jobID, "t1")
	t2 := makeTask(jobID, "t2")
	t3 := makeTask(jobID, "t3")
	job := api.NewJobInfo(jobID, t1, t2, t3)
	job.SubJobs = map[api.SubJobID]*api.SubJobInfo{
		"sj-a": {
			UID: "sj-a",
			TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
				api.Pending: {t1.UID: t1},
			},
		},
		"sj-b": {
			UID: "sj-b",
			TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
				api.Pending: {t2.UID: t2, t3.UID: t3},
			},
		},
	}
	subJobLess := func(l, r interface{}) bool {
		return l.(*api.SubJobInfo).UID > r.(*api.SubJobInfo).UID
	}
	taskLess := func(l, r interface{}) bool {
		return l.(*api.TaskInfo).UID > r.(*api.TaskInfo).UID
	}

	out := collectPendingTasks(job, subJobLess, taskLess)
	assert.Equal(t, 3, len(out))
	assert.Equal(t, api.TaskID("t3"), out[0].UID)
	assert.Equal(t, api.TaskID("t2"), out[1].UID)
	assert.Equal(t, api.TaskID("t1"), out[2].UID)
}

func TestCollectPendingTasks_DedupsBetweenSubJobsAndFallback(t *testing.T) {
	jobID := api.JobID("ns/job-dedup")
	t1 := makeTask(jobID, "t1")
	t2 := makeTask(jobID, "t2")
	job := api.NewJobInfo(jobID, t1, t2)
	job.SubJobs = map[api.SubJobID]*api.SubJobInfo{
		"sj-a": {
			UID: "sj-a",
			TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
				api.Pending: {t1.UID: t1, t2.UID: t2},
			},
		},
	}

	out := collectPendingTasks(job, nil, nil)
	assert.Equal(t, 2, len(out))
	assert.ElementsMatch(t, []api.TaskID{"t1", "t2"}, []api.TaskID{out[0].UID, out[1].UID})
}

func TestCollectPendingTasks_NoSubJobsUsesTaskOrder(t *testing.T) {
	jobID := api.JobID("ns/job-no-subjobs")
	t1 := makeTask(jobID, "a")
	t2 := makeTask(jobID, "b")
	t3 := makeTask(jobID, "c")
	job := api.NewJobInfo(jobID, t1, t2, t3)
	job.SubJobs = map[api.SubJobID]*api.SubJobInfo{}

	taskLess := func(l, r interface{}) bool {
		return l.(*api.TaskInfo).UID > r.(*api.TaskInfo).UID
	}
	out := collectPendingTasks(job, nil, taskLess)
	assert.Equal(t, 3, len(out))
	assert.Equal(t, api.TaskID("c"), out[0].UID)
	assert.Equal(t, api.TaskID("b"), out[1].UID)
	assert.Equal(t, api.TaskID("a"), out[2].UID)
}

func makeTask(jobID api.JobID, name string) *api.TaskInfo {
	res := (&api.Resource{MilliCPU: 1000}).Clone()
	return &api.TaskInfo{
		UID:        api.TaskID(name),
		Job:        jobID,
		Name:       name,
		Namespace:  "ns",
		Resreq:     res.Clone(),
		InitResreq: res.Clone(),
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "ns",
				UID:       types.UID(name),
			},
		},
		NumaInfo: &api.TopologyInfo{
			ResMap: map[int]v1.ResourceList{},
		},
		TransactionContext: api.TransactionContext{
			Status: api.Pending,
		},
	}
}

func ensureServerOptsForTest() {
	if options.ServerOpts == nil {
		options.ServerOpts = options.NewServerOption()
	}
	if options.ServerOpts.MinNodesToFind <= 0 {
		options.ServerOpts.MinNodesToFind = 1
	}
}
