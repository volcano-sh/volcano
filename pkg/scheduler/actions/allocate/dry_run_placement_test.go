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

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	pluginutil "volcano.sh/volcano/pkg/scheduler/plugins/util"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
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
	if env.job.AllocatedHyperNodeDirty {
		t.Fatal("rolled back allocations must not invalidate unchanged placement")
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
	job.AllocatedHyperNodeDirty = true
	job.SubJobs[subJobA].AllocatedHyperNode = "polluted-a"
	job.SubJobs[subJobB].AllocatedHyperNode = "polluted-b"

	recorder.RecoverSubJobStatus(job)

	if job.AllocatedHyperNode != "initial-job" {
		t.Fatalf("job AllocatedHyperNode = %q, want initial-job", job.AllocatedHyperNode)
	}
	if job.AllocatedHyperNodeDirty {
		t.Fatal("recovering a dry-run must restore placement validity")
	}
	if job.SubJobs[subJobA].AllocatedHyperNode != "sn-a" {
		t.Fatalf("subJob-a AllocatedHyperNode = %q, want sn-a", job.SubJobs[subJobA].AllocatedHyperNode)
	}
	if job.SubJobs[subJobB].AllocatedHyperNode != "sn-b" {
		t.Fatalf("subJob-b AllocatedHyperNode = %q, want sn-b", job.SubJobs[subJobB].AllocatedHyperNode)
	}

	job.AllocatedHyperNodeDirty = true
	recorder.SnapshotSubJobStatus(job, worksheet)
	job.AllocatedHyperNodeDirty = false
	recorder.RecoverSubJobStatus(job)
	if !job.AllocatedHyperNodeDirty {
		t.Fatal("recovering a dry-run must retain an earlier invalidation")
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
	alloc.queueNodes = make(map[queueNodeCacheKey][]*api.NodeInfo)
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
		ssn.AddHyperNodeGradientForSubJobFn(dryRunTestPlugin, func(_ *api.SubJobInfo, _ *api.HyperNodeInfo, _ api.SearchPurpose) api.HyperNodeGradientResult {
			return api.HyperNodeGradientResult{Gradients: gradientLayers}
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
	// These fixtures exercise placement bookkeeping with a synthetic topology plugin.
	ssn.EnablePodGroupPlacement(job)

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

// queueCandidateTestPlugin supplies filters and scoring at the plugin boundary.
type queueCandidateTestPlugin struct {
	dryRunTestPluginImpl
	filter api.QueueNodeFilterFn
	score  api.NodeOrderFn
}

func (p *queueCandidateTestPlugin) OnSessionOpen(ssn *framework.Session) {
	if p.filter != nil {
		ssn.AddQueueNodeFilterFn(p.Name(), p.filter)
	}
	if p.score != nil {
		ssn.AddNodeOrderFn(p.Name(), p.score)
	}
}

func newQueueCandidateAllocation(t *testing.T, common uthelper.TestCommonStruct, filter api.QueueNodeFilterFn, score api.NodeOrderFn) (*framework.Session, *Action) {
	t.Helper()
	if common.Plugins == nil {
		common.Plugins = map[string]framework.PluginBuilder{}
	}
	common.Plugins[gang.PluginName] = gang.New
	common.Plugins[dryRunTestPlugin] = func(_ framework.Arguments) framework.Plugin {
		return &queueCandidateTestPlugin{filter: filter, score: score}
	}
	plugins := []conf.PluginOption{
		{Name: gang.PluginName, EnabledJobReady: ptr.To(true), EnabledJobPipelined: ptr.To(true)},
		{Name: dryRunTestPlugin, EnabledQueueNodeFilter: ptr.To(true), EnabledNodeOrder: ptr.To(true)},
	}
	for name := range common.Plugins {
		if name != gang.PluginName && name != dryRunTestPlugin {
			plugins = append(plugins, conf.PluginOption{Name: name, EnabledPredicate: ptr.To(true)})
		}
	}
	ssn := common.RegisterSession([]conf.Tier{{Plugins: plugins}}, nil)
	t.Cleanup(common.Close)
	alloc := New()
	alloc.session = ssn
	alloc.queueNodes = make(map[queueNodeCacheKey][]*api.NodeInfo)
	alloc.recorder = NewRecorder()
	return ssn, alloc
}

func TestAllocateResourcesForTasksQueueCandidateScopes(t *testing.T) {
	var inputs [][]string
	common := queueCandidateScopeFixture()
	common.Queues = []*schedulingv1.Queue{util.MakeQueue("q1").Obj()}
	common.PodGroups = []*schedulingv1.PodGroup{util.BuildPodGroup("pg1", "ns", "q1", 1, nil, schedulingv1.PodGroupInqueue)}
	common.Pods = []*v1.Pod{util.BuildPod("ns", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", nil, nil)}
	ssn, alloc := newQueueCandidateAllocation(t, common, func(_ *api.QueueInfo, nodes []*api.NodeInfo) []*api.NodeInfo {
		names := make([]string, 0, len(nodes))
		for _, node := range nodes {
			names = append(names, node.Name)
		}
		inputs = append(inputs, names)
		// Reject the cluster scope and accept the restricted scope.
		if len(nodes) > 1 {
			return nil
		}
		return nodes
	}, nil)
	job := ssn.Jobs["ns/pg1"]
	subJob := job.SubJobs[job.DefaultSubJobID()]
	for _, tc := range []struct {
		name      string
		hyperNode string
		wantNode  string
	}{
		{name: "empty cluster candidates", hyperNode: framework.ClusterTopHyperNode},
		{name: "restricted scope remains allocatable", hyperNode: "sn-a", wantNode: "node-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tasks := util.NewPriorityQueue(ssn.TaskOrderFn)
			tasks.Push(job.Tasks["ns-p1"])
			stmt := alloc.allocateResourcesForTasks(subJob, tasks, tc.hyperNode)
			if tc.wantNode == "" {
				require.Nil(t, stmt)
				require.Empty(t, job.Tasks["ns-p1"].NodeName)
				return
			}
			require.NotNil(t, stmt)
			defer stmt.Discard()
			require.Len(t, stmt.Operations(), 1)
			require.Equal(t, tc.wantNode, job.Tasks["ns-p1"].NodeName)
		})
	}
	require.Len(t, inputs, 2)
	require.ElementsMatch(t, []string{"node-a", "node-b"}, inputs[0])
	require.Equal(t, []string{"node-a"}, inputs[1])
}

func queueCandidateScopeFixture() uthelper.TestCommonStruct {
	return uthelper.TestCommonStruct{
		Nodes: []*v1.Node{
			util.BuildNode("node-a", api.BuildResourceList("8", "16Gi", api.ScalarResource{Name: "pods", Value: "110"}), nil),
			util.BuildNode("node-b", api.BuildResourceList("8", "16Gi", api.ScalarResource{Name: "pods", Value: "110"}), nil),
		},
		HyperNodes: map[string]sets.Set[string]{"sn-a": sets.New("node-a"), "sn-b": sets.New("node-b")},
		HyperNodesMap: api.HyperNodeInfoMap{
			"sn-a": newPlacementTestHyperNode("sn-a", 1, ""),
			"sn-b": newPlacementTestHyperNode("sn-b", 1, ""),
		},
		HyperNodesSetByTier: map[int]sets.Set[string]{1: sets.New("sn-a", "sn-b")},
	}
}

func TestAllocateResourcesForTasksQueueCandidateCache(t *testing.T) {
	common := queueCandidateScopeFixture()
	common.Queues = []*schedulingv1.Queue{util.MakeQueue("q1").Obj(), util.MakeQueue("q2").Obj()}
	for _, queue := range []string{"q1", "q2"} {
		common.PodGroups = append(common.PodGroups, util.BuildPodGroup(queue, "ns", queue, 1, nil, schedulingv1.PodGroupInqueue))
		common.Pods = append(common.Pods, util.BuildPod("ns", queue, "", v1.PodPending, api.BuildResourceList("1", "1Gi"), queue, nil, nil))
	}
	inputs := map[api.QueueID][][]string{}
	ssn, alloc := newQueueCandidateAllocation(t, common, func(queue *api.QueueInfo, nodes []*api.NodeInfo) []*api.NodeInfo {
		names := make([]string, 0, len(nodes))
		candidates := make([]*api.NodeInfo, 0, len(nodes))
		for _, node := range nodes {
			names = append(names, node.Name)
			if queue.UID == "q1" || node.Name == "node-b" {
				candidates = append(candidates, node)
			}
		}
		inputs[queue.UID] = append(inputs[queue.UID], names)
		return candidates
	}, nil)
	for _, tc := range []struct {
		name      string
		queue     api.QueueID
		hyperNode string
		wantNode  string
	}{
		{name: "first queue first scope", queue: "q1", hyperNode: "sn-a", wantNode: "node-a"},
		{name: "reuse first scope", queue: "q1", hyperNode: "sn-a", wantNode: "node-a"},
		{name: "first queue second scope", queue: "q1", hyperNode: "sn-b", wantNode: "node-b"},
		{name: "second queue empty scope", queue: "q2", hyperNode: "sn-a"},
		{name: "reuse empty result", queue: "q2", hyperNode: "sn-a"},
		{name: "second queue second scope", queue: "q2", hyperNode: "sn-b", wantNode: "node-b"},
		{name: "first queue cache survives second queue", queue: "q1", hyperNode: "sn-a", wantNode: "node-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := ssn.Jobs[api.JobID("ns/"+string(tc.queue))]
			task := job.Tasks[api.TaskID("ns-"+string(tc.queue))]
			tasks := util.NewPriorityQueue(ssn.TaskOrderFn)
			tasks.Push(task)
			stmt := alloc.allocateResourcesForTasks(job.SubJobs[job.DefaultSubJobID()], tasks, tc.hyperNode)
			if tc.wantNode == "" {
				require.Nil(t, stmt, "empty cached candidates must not fall back to other nodes")
				require.Empty(t, task.NodeName)
				return
			}
			require.NotNil(t, stmt)
			defer stmt.Discard()
			require.Len(t, stmt.Operations(), 1)
			require.Equal(t, tc.wantNode, task.NodeName)
		})
	}
	require.Equal(t, map[api.QueueID][][]string{
		"q1": {{"node-a"}, {"node-b"}},
		"q2": {{"node-a"}, {"node-b"}},
	}, inputs, "each queue and scope must be filtered once, including empty results")
}
