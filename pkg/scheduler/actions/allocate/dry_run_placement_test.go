/*
Copyright 2025 The Volcano Authors.

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

package allocate

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	pluginutil "volcano.sh/volcano/pkg/scheduler/plugins/util"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type dryRunTestPluginImpl struct{}

func (p *dryRunTestPluginImpl) Name() string { return dryRunTestPlugin }

func (p *dryRunTestPluginImpl) OnSessionOpen(_ *framework.Session) {}

func (p *dryRunTestPluginImpl) OnSessionClose(_ *framework.Session) {}

const dryRunTestPlugin = "dry-run-test"

func registerDryRunTestPlugins() {
	framework.RegisterPluginBuilder(gang.PluginName, gang.New)
	framework.RegisterPluginBuilder(dryRunTestPlugin, func(_ framework.Arguments) framework.Plugin {
		return &dryRunTestPluginImpl{}
	})
}

// TestAllocateResourcesForTasks_RestoresPlacementOnTotalFailure verifies placement is
// rolled back when no task can be allocated and the subJob is neither ready nor pipelined.
func TestAllocateResourcesForTasks_RestoresPlacementOnTotalFailure(t *testing.T) {
	env := newDryRunPlacementEnv(t, dryRunEnvOptions{
		nodeCPU: map[string]string{
			"node-a": "1",
		},
		tasks: []dryRunTaskSpec{
			{name: "p1", cpu: "4", mem: "4G", role: "worker"},
		},
		minMember:    1,
		subGroupSize: 1,
	})

	subJob := env.subJob
	subJob.AllocatedHyperNode = "sn-a"
	env.job.AllocatedHyperNode = "sn-a"

	tasks := env.pendingTasks()
	alloc := env.action()

	stmt := alloc.allocateResourcesForTasks(subJob, tasks, "sn-a")
	if stmt != nil && len(stmt.Operations()) > 0 {
		t.Fatalf("expected no successful allocations, got %d ops", len(stmt.Operations()))
	}
	if subJob.AllocatedHyperNode != "sn-a" {
		t.Fatalf("subJob AllocatedHyperNode = %q, want restored sn-a", subJob.AllocatedHyperNode)
	}
	if env.job.AllocatedHyperNode != "sn-a" {
		t.Fatalf("job AllocatedHyperNode = %q, want restored sn-a", env.job.AllocatedHyperNode)
	}
}

// TestAllocateResourcesForTasks_RestoresPlacementWhenPartialAllocNotPipelined verifies that
// partial allocation which does not satisfy gang pipelined semantics rolls back placement.
func TestAllocateResourcesForTasks_RestoresPlacementWhenPartialAllocNotPipelined(t *testing.T) {
	env := newDryRunPlacementEnv(t, dryRunEnvOptions{
		nodeCPU: map[string]string{
			"node-a": "8",
		},
		tasks: []dryRunTaskSpec{
			{name: "p1", cpu: "4", mem: "4G", role: "worker"},
			{name: "p2", cpu: "4", mem: "4G", role: "worker"},
			{name: "p3", cpu: "4", mem: "4G", role: "worker"},
		},
		minMember:    3,
		subGroupSize: 3,
	})

	subJob := env.subJob
	tasks := env.pendingTasks()
	alloc := env.action()

	stmt := alloc.allocateResourcesForTasks(subJob, tasks, "sn-a")
	if stmt != nil {
		t.Fatalf("expected nil statement when subJob is not pipelined, got %d ops", len(stmt.Operations()))
	}
	if subJob.AllocatedHyperNode != "" {
		t.Fatalf("subJob AllocatedHyperNode = %q, want empty after rollback", subJob.AllocatedHyperNode)
	}
	if env.job.AllocatedHyperNode != "" {
		t.Fatalf("job AllocatedHyperNode = %q, want empty after rollback", env.job.AllocatedHyperNode)
	}
}

// TestAllocateResourcesForTasks_KeepsPlacementWhenSubJobPipelined verifies placement is kept
// when a plugin explicitly permits pipelined status after partial allocation.
func TestAllocateResourcesForTasks_KeepsPlacementWhenSubJobPipelined(t *testing.T) {
	env := newDryRunPlacementEnv(t, dryRunEnvOptions{
		nodeCPU: map[string]string{
			"node-a": "8",
		},
		tasks: []dryRunTaskSpec{
			{name: "p1", cpu: "4", mem: "4G", role: "worker"},
			{name: "p2", cpu: "4", mem: "4G", role: "worker"},
			{name: "p3", cpu: "4", mem: "4G", role: "worker"},
		},
		minMember:            3,
		subGroupSize:         3,
		forceSubJobPipelined: true,
	})

	subJob := env.subJob
	tasks := env.pendingTasks()
	alloc := env.action()

	stmt := alloc.allocateResourcesForTasks(subJob, tasks, "sn-a")
	if stmt == nil || len(stmt.Operations()) != 2 {
		t.Fatalf("expected pipelined statement with 2 ops, got %v", stmt)
	}
	if subJob.AllocatedHyperNode != "sn-a" {
		t.Fatalf("subJob AllocatedHyperNode = %q, want sn-a while pipelined", subJob.AllocatedHyperNode)
	}
	stmt.Discard()
}

// TestAllocateResourcesForTasks_KeepsPlacementOnSuccess verifies placement is retained when
// allocation succeeds and SubJobReady is satisfied.
func TestAllocateResourcesForTasks_KeepsPlacementOnSuccess(t *testing.T) {
	env := newDryRunPlacementEnv(t, dryRunEnvOptions{
		nodeCPU: map[string]string{
			"node-a": "8",
		},
		tasks: []dryRunTaskSpec{
			{name: "p1", cpu: "4", mem: "4G"},
		},
		minMember: 1,
	})

	subJob := env.subJob
	tasks := env.pendingTasks()
	alloc := env.action()

	stmt := alloc.allocateResourcesForTasks(subJob, tasks, "sn-a")
	if stmt == nil || len(stmt.Operations()) == 0 {
		t.Fatal("expected non-empty statement for successful allocation")
	}
	if subJob.AllocatedHyperNode != "sn-a" {
		t.Fatalf("subJob AllocatedHyperNode = %q, want sn-a", subJob.AllocatedHyperNode)
	}
	if env.job.AllocatedHyperNode != "sn-a" {
		t.Fatalf("job AllocatedHyperNode = %q, want sn-a", env.job.AllocatedHyperNode)
	}
	stmt.Discard()
}

// TestAllocateForSubJob_DryRunSelectsSiblingNotLCA verifies dry-run on sn-a does not leak
// placement into the final sn-b selection. Without rollback, LCA(sn-a, sn-b) would be root.
func TestAllocateForSubJob_DryRunSelectsSiblingNotLCA(t *testing.T) {
	env := newDryRunPlacementEnv(t, dryRunEnvOptions{
		nodeCPU: map[string]string{
			"node-a": "8",
			"node-b": "8",
		},
		tasks: []dryRunTaskSpec{
			{name: "p1", cpu: "4", mem: "4G"},
		},
		minMember:       1,
		subJobGradients: [][]string{{"sn-a", "sn-b"}},
		hyperNodeScores: map[string]float64{
			"sn-a": 1,
			"sn-b": 100,
		},
	})

	subJob := env.subJob
	worksheet := &SubJobWorksheet{tasks: env.pendingTasks()}
	rootHN := env.ssn.HyperNodes["root"]
	alloc := env.action()

	stmt, _ := alloc.allocateForSubJob(subJob, worksheet, rootHN, "")
	if stmt == nil || len(stmt.Operations()) == 0 {
		t.Fatal("expected successful subJob allocation statement")
	}
	if subJob.AllocatedHyperNode != "sn-b" {
		t.Fatalf("subJob AllocatedHyperNode = %q, want sn-b (not root from leaked sn-a dry-run)", subJob.AllocatedHyperNode)
	}
	if env.job.AllocatedHyperNode != "sn-b" {
		t.Fatalf("job AllocatedHyperNode = %q, want sn-b", env.job.AllocatedHyperNode)
	}
	if env.ssn.HyperNodes.GetLCAHyperNode("sn-a", "sn-b") == subJob.AllocatedHyperNode {
		t.Fatalf("placement incorrectly collapsed to LCA %q", subJob.AllocatedHyperNode)
	}
}

// TestAllocateForSubJob_DryRunRestoresBetweenHyperNodeTries verifies each hyperNode dry-run
// attempt restores placement before the next candidate is evaluated.
func TestAllocateForSubJob_DryRunRestoresBetweenHyperNodeTries(t *testing.T) {
	env := newDryRunPlacementEnv(t, dryRunEnvOptions{
		nodeCPU: map[string]string{
			"node-a": "8",
			"node-b": "8",
		},
		tasks: []dryRunTaskSpec{
			{name: "p1", cpu: "4", mem: "4G"},
		},
		minMember: 1,
	})

	subJob := env.subJob
	job := env.job
	worksheet := &SubJobWorksheet{tasks: env.pendingTasks()}
	alloc := env.action()

	placementAtTryStart := make(map[string]string)
	for _, hnName := range []string{"sn-a", "sn-b"} {
		placementAtTryStart[hnName] = subJob.AllocatedHyperNode

		placementBeforeTry := captureHyperNodePlacement(job, subJob)
		stmt := alloc.allocateResourcesForTasks(subJob, worksheet.Clone().tasks, hnName)
		if stmt != nil && len(stmt.Operations()) > 0 {
			stmt.Discard()
		}
		restoreHyperNodePlacement(job, subJob, placementBeforeTry)
	}

	if placementAtTryStart["sn-a"] != "" {
		t.Fatalf("sn-a try started with polluted placement %q, want empty", placementAtTryStart["sn-a"])
	}
	if placementAtTryStart["sn-b"] != "" {
		t.Fatalf("sn-b try started with polluted placement %q, want empty", placementAtTryStart["sn-b"])
	}
	if subJob.AllocatedHyperNode != "" {
		t.Fatalf("subJob AllocatedHyperNode = %q after dry-run loop, want empty", subJob.AllocatedHyperNode)
	}
}

// TestAllocateForJobLoop_RecoverSubJobStatusBetweenHyperNodes mirrors allocateForJob's per-hyperNode
// dry-run loop and verifies RecoverSubJobStatus clears placement before the next hyperNode attempt.
func TestAllocateForJobLoop_RecoverSubJobStatusBetweenHyperNodes(t *testing.T) {
	env := newDryRunPlacementEnv(t, dryRunEnvOptions{
		nodeCPU: map[string]string{
			"node-a": "8",
			"node-b": "8",
		},
		tasks: []dryRunTaskSpec{
			{name: "p1", cpu: "4", mem: "4G"},
		},
		minMember: 1,
	})

	subJob := env.subJob
	job := env.job
	worksheet := &JobWorksheet{
		subJobs: util.NewPriorityQueue(func(l, r interface{}) bool { return true }),
		subJobWorksheets: map[api.SubJobID]*SubJobWorksheet{
			subJob.UID: {tasks: env.pendingTasks()},
		},
	}
	worksheet.subJobs.Push(subJob)

	recorder := NewRecorder()
	alloc := env.action()
	alloc.recorder = recorder

	jobHyperNodes := []*api.HyperNodeInfo{
		env.ssn.HyperNodes["sn-a"],
		env.ssn.HyperNodes["sn-b"],
	}

	placementBeforeSecondTry := "unset"
	recorder.SnapshotSubJobStatus(job, worksheet)

	for i, hn := range jobHyperNodes {
		if i == 1 {
			placementBeforeSecondTry = subJob.AllocatedHyperNode
		}
		wsCopy := worksheet.Clone()
		subJobWS := wsCopy.subJobWorksheets[subJob.UID]
		stmt, _ := alloc.allocateForSubJob(subJob, subJobWS, hn, "")
		if stmt != nil && len(stmt.Operations()) > 0 {
			stmt.Discard()
		}
		recorder.RecoverSubJobStatus(job)
	}

	if placementBeforeSecondTry != "" {
		t.Fatalf("subJob placement before second hyperNode try = %q, want empty after RecoverSubJobStatus", placementBeforeSecondTry)
	}
	if job.AllocatedHyperNode != "" {
		t.Fatalf("job AllocatedHyperNode = %q, want empty after all dry-run attempts", job.AllocatedHyperNode)
	}
}

// TestRecoverSubJobStatus_MultiSubJobIndependentRestore verifies snapshot/recover keeps each
// subJob's placement isolated when only one subJob is mutated during dry-run.
func TestRecoverSubJobStatus_MultiSubJobIndependentRestore(t *testing.T) {
	recorder := NewRecorder()
	jobID := api.JobID("c1/pg1")
	subJobA := api.SubJobID("sub-a")
	subJobB := api.SubJobID("sub-b")
	job := &api.JobInfo{
		UID:                jobID,
		AllocatedHyperNode: "initial-job",
		SubJobs: map[api.SubJobID]*api.SubJobInfo{
			subJobA: {UID: subJobA, AllocatedHyperNode: "sn-a"},
			subJobB: {UID: subJobB, AllocatedHyperNode: "sn-b"},
		},
	}
	worksheet := &JobWorksheet{
		subJobWorksheets: map[api.SubJobID]*SubJobWorksheet{
			subJobA: {},
			subJobB: {},
		},
	}

	recorder.SnapshotSubJobStatus(job, worksheet)

	job.AllocatedHyperNode = "polluted-job"
	job.SubJobs[subJobA].AllocatedHyperNode = "polluted-a"
	job.SubJobs[subJobB].AllocatedHyperNode = "polluted-b"

	recorder.RecoverSubJobStatus(job)

	if job.AllocatedHyperNode != "initial-job" {
		t.Fatalf("job AllocatedHyperNode = %q, want initial-job", job.AllocatedHyperNode)
	}
	if job.SubJobs[subJobA].AllocatedHyperNode != "sn-a" {
		t.Fatalf("subJob-a AllocatedHyperNode = %q, want sn-a", job.SubJobs[subJobA].AllocatedHyperNode)
	}
	if job.SubJobs[subJobB].AllocatedHyperNode != "sn-b" {
		t.Fatalf("subJob-b AllocatedHyperNode = %q, want sn-b", job.SubJobs[subJobB].AllocatedHyperNode)
	}
}

type dryRunTaskSpec struct {
	name string
	cpu  string
	mem  string
	role string
}

type dryRunEnvOptions struct {
	nodeCPU              map[string]string
	tasks                []dryRunTaskSpec
	minMember            int32
	subGroupSize         int32
	subJobGradients      [][]string
	hyperNodeScores      map[string]float64
	forceSubJobPipelined bool
}

type dryRunPlacementEnv struct {
	t         *testing.T
	ssn       *framework.Session
	job       *api.JobInfo
	subJob    *api.SubJobInfo
	taskSpecs []dryRunTaskSpec
}

func (e *dryRunPlacementEnv) action() *Action {
	alloc := New()
	alloc.session = e.ssn
	alloc.recorder = NewRecorder()
	return alloc
}

func (e *dryRunPlacementEnv) pendingTasks() *util.PriorityQueue {
	tasks := util.NewPriorityQueue(func(l, r interface{}) bool { return true })
	for _, spec := range e.taskSpecs {
		task, ok := e.job.Tasks[api.TaskID(spec.name)]
		if !ok {
			e.t.Fatalf("task %s not found in job", spec.name)
		}
		tasks.Push(task)
	}
	return tasks
}

func newDryRunPlacementEnv(t *testing.T, opts dryRunEnvOptions) *dryRunPlacementEnv {
	t.Helper()

	registerDryRunTestPlugins()

	trueVal := true
	gangPlugin := conf.PluginOption{
		Name:               gang.PluginName,
		EnabledSubJobReady: &trueVal,
	}
	if !opts.forceSubJobPipelined {
		gangPlugin.EnabledSubJobPipelined = &trueVal
	}
	testPlugin := conf.PluginOption{
		Name:                     dryRunTestPlugin,
		EnabledHyperNodeGradient: &trueVal,
		EnabledHyperNodeOrder:    &trueVal,
	}
	if opts.forceSubJobPipelined {
		testPlugin.EnabledSubJobPipelined = &trueVal
	}
	tiers := []conf.Tier{{
		Plugins: []conf.PluginOption{gangPlugin, testPlugin},
	}}

	schedulerCache := cache.NewCustomMockSchedulerCache(
		"dry-run-test",
		util.NewFakeBinder(0),
		util.NewFakeEvictor(0),
		&util.FakeStatusUpdater{},
		nil,
		nil,
	)
	ssn := framework.OpenSession(schedulerCache, tiers, nil)
	ssn.Queues["q1"] = api.NewQueueInfo(&scheduling.Queue{
		ObjectMeta: metav1.ObjectMeta{Name: "q1"},
		Spec:       scheduling.QueueSpec{Weight: 1},
	})
	ssn.HyperNodesReadyToSchedule = true

	buildDryRunHyperNodeTree(ssn, opts.nodeCPU)

	if opts.forceSubJobPipelined {
		ssn.AddSubJobPipelinedFn(dryRunTestPlugin, func(obj interface{}) int {
			return pluginutil.Permit
		})
	}

	if len(opts.subJobGradients) > 0 {
		gradientLayers := make([][]*api.HyperNodeInfo, len(opts.subJobGradients))
		for i, names := range opts.subJobGradients {
			for _, name := range names {
				gradientLayers[i] = append(gradientLayers[i], ssn.HyperNodes[name])
			}
		}
		ssn.AddHyperNodeGradientForSubJobFn(dryRunTestPlugin, func(_ *api.SubJobInfo, _ *api.HyperNodeInfo, _ api.SearchPurpose) [][]*api.HyperNodeInfo {
			return gradientLayers
		})
	}

	if opts.hyperNodeScores != nil {
		scores := opts.hyperNodeScores
		ssn.AddHyperNodeOrderFn(dryRunTestPlugin, func(_ *api.SubJobInfo, hyperNodes map[string][]*api.NodeInfo) (map[string]float64, error) {
			result := make(map[string]float64, len(hyperNodes))
			for name := range hyperNodes {
				if score, ok := scores[name]; ok {
					result[name] = score
				}
			}
			return result, nil
		})
	}

	job, subJob := buildDryRunJob(t, opts)
	ssn.Jobs[job.UID] = job

	return &dryRunPlacementEnv{
		t:         t,
		ssn:       ssn,
		job:       job,
		subJob:    subJob,
		taskSpecs: opts.tasks,
	}
}

func buildDryRunHyperNodeTree(ssn *framework.Session, nodeCPU map[string]string) {
	realNodesList := make(map[string][]*api.NodeInfo)
	realNodesSet := make(map[string]sets.Set[string])

	for nodeName, cpu := range nodeCPU {
		node := util.BuildNode(nodeName, api.BuildResourceList(cpu, "16Gi", []api.ScalarResource{{Name: "pods", Value: "110"}}...), nil)
		nodeInfo := api.NewNodeInfo(node)
		ssn.Nodes[nodeName] = nodeInfo

		hyperNode := "sn-a"
		if nodeName == "node-b" {
			hyperNode = "sn-b"
		}
		realNodesList[hyperNode] = append(realNodesList[hyperNode], nodeInfo)
		if _, ok := realNodesSet[hyperNode]; !ok {
			realNodesSet[hyperNode] = sets.New[string]()
		}
		realNodesSet[hyperNode].Insert(nodeName)
	}

	ssn.HyperNodes = api.HyperNodeInfoMap{
		"root": newPlacementTestHyperNode("root", 3, ""),
		"sn-a": newPlacementTestHyperNode("sn-a", 2, "root"),
		"sn-b": newPlacementTestHyperNode("sn-b", 2, "root"),
	}
	ssn.HyperNodes["root"].Parent = ""
	ssn.HyperNodesSetByTier = map[int]sets.Set[string]{
		2: sets.New("sn-a", "sn-b"),
		3: sets.New("root"),
	}
	ssn.HyperNodesTiers = []int{2, 3}
	ssn.RealNodesList = realNodesList
	ssn.RealNodesSet = realNodesSet
}

func buildDryRunJob(t *testing.T, opts dryRunEnvOptions) (*api.JobInfo, *api.SubJobInfo) {
	t.Helper()

	pgSpec := scheduling.PodGroupSpec{
		Queue:     "q1",
		MinMember: opts.minMember,
	}
	if opts.subGroupSize > 0 {
		subGroupSize := opts.subGroupSize
		pgSpec.SubGroupPolicy = []scheduling.SubGroupPolicySpec{
			{
				Name:         "task1",
				SubGroupSize: &subGroupSize,
				MatchLabelKeys: []string{
					"volcano.sh/task-spec",
				},
			},
		}
	}

	pg := scheduling.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pg1",
			Namespace: "c1",
		},
		Spec: pgSpec,
		Status: scheduling.PodGroupStatus{
			Phase: scheduling.PodGroupInqueue,
		},
	}
	job := api.NewJobInfo(api.JobID("c1/pg1"))
	job.SetPodGroup(&api.PodGroup{PodGroup: pg})

	for _, spec := range opts.tasks {
		labels := map[string]string{}
		if spec.role != "" {
			labels["volcano.sh/task-spec"] = spec.role
		}
		pod := util.BuildPod("c1", spec.name, "", v1.PodPending, api.BuildResourceList(spec.cpu, spec.mem), "pg1", labels, nil)
		pod.UID = types.UID(spec.name)
		task := api.NewTaskInfo(pod)
		job.AddTaskInfo(task)
	}

	var subJob *api.SubJobInfo
	if opts.subGroupSize > 0 {
		for _, sj := range job.SubJobs {
			subJob = sj
			break
		}
	} else {
		subJob = job.SubJobs[job.DefaultSubJobID()]
	}
	if subJob == nil {
		t.Fatal("subJob not found")
	}
	return job, subJob
}
