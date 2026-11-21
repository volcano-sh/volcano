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

package utils

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const antiAffinitySimulationGradientPluginName = "anti-affinity-simulation-gradient"

type antiAffinitySimulationGradientPlugin struct{}

func (p *antiAffinitySimulationGradientPlugin) Name() string {
	return antiAffinitySimulationGradientPluginName
}

func (p *antiAffinitySimulationGradientPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddHyperNodeGradientForSubJobFn(p.Name(), func(*api.SubJobInfo, *api.HyperNodeInfo, api.SearchPurpose) api.HyperNodeGradientResult {
		allowed := ssn.HyperNodes["hn-allowed"]
		if allowed == nil {
			return api.HyperNodeGradientResult{}
		}
		return api.HyperNodeGradientResult{Gradients: [][]*api.HyperNodeInfo{{allowed}}}
	})
}

func (p *antiAffinitySimulationGradientPlugin) OnSessionClose(*framework.Session) {}

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
	plan, _, ok := BuildNominationPlanInDomain(nil, nil, nil, nil, nil, "x", true)
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
	plan, _, ok = BuildNominationPlanInDomain(ssn, nil, job, nil, nil, "x", true)
	assert.False(t, ok)
	assert.Nil(t, plan)
}

func TestOrganizeEvictionWorksheet(t *testing.T) {
	tests := []struct {
		name      string
		requests  v1.ResourceList
		claims    []v1.PodResourceClaim
		gates     []v1.PodSchedulingGate
		wantTasks []api.TaskID
	}{
		{
			name:      "ordinary requests need placement",
			requests:  api.BuildResourceList("1", "0"),
			wantTasks: []api.TaskID{"ns-t1"},
		},
		{
			name: "DRA-only direct claim needs placement",
			claims: []v1.PodResourceClaim{
				{
					Name:              "gpu",
					ResourceClaimName: ptr.To("gpu-claim"),
				},
			},
			wantTasks: []api.TaskID{"ns-t1"},
		},
		{
			name: "DRA-only template claim needs placement",
			claims: []v1.PodResourceClaim{
				{
					Name:                      "gpu",
					ResourceClaimTemplateName: ptr.To("gpu-template"),
				},
			},
			wantTasks: []api.TaskID{"ns-t1"},
		},
		{
			name: "no resource requests are left to backfill",
		},
		{
			name: "scheduling gate still excludes DRA-only tasks",
			claims: []v1.PodResourceClaim{
				{
					Name:              "gpu",
					ResourceClaimName: ptr.To("gpu-claim"),
				},
			},
			gates: []v1.PodSchedulingGate{
				{Name: "example.com/ready"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var containerClaims []v1.ResourceClaim
			for _, claim := range tt.claims {
				containerClaims = append(containerClaims, v1.ResourceClaim{Name: claim.Name})
			}
			pod := util.BuildPodWithResourceClaim("ns", "t1", "", v1.PodPending, tt.requests,
				"job", nil, nil, containerClaims, tt.claims)
			pod.Spec.SchedulingGates = tt.gates
			task := api.NewTaskInfo(pod)
			job := api.NewJobInfo(task.Job, task)
			worksheet := organizeEvictionWorksheet(&framework.Session{}, job)
			var gotTasks []api.TaskID
			for !worksheet.Empty() {
				subJob := worksheet.subJobs.Pop().(*api.SubJobInfo)
				tasks := worksheet.subJobWorksheets[subJob.UID].tasks
				for !tasks.Empty() {
					gotTasks = append(gotTasks, tasks.Pop().(*api.TaskInfo).UID)
				}
			}
			assert.Equal(t, tt.wantTasks, gotTasks)
		})
	}
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
	out := collectPendingTasksForJobStartup(ssn, job)
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
	out := collectPendingTasksForRunningJob(ssn, job)
	assert.Equal(t, 1, len(out))
	assert.Equal(t, api.TaskID("t1"), out[0].UID)
}

func TestCollectPendingTasksForJobStartup_EmptyWorksheetReturnsNil(t *testing.T) {
	jobID := api.JobID("ns/empty-ws")
	job := api.NewJobInfo(jobID)
	ssn := &framework.Session{}
	assert.Nil(t, collectPendingTasksForJobStartup(ssn, job))
}

func TestBuildNominationPlanInDomain_HardPodGroupAntiAffinityUsesGradient(t *testing.T) {
	ensureServerOptsForTest()
	originalMinNodesToFind := options.ServerOpts.MinNodesToFind
	options.ServerOpts.MinNodesToFind = 2
	defer func() { options.ServerOpts.MinNodesToFind = originalMinNodesToFind }()

	framework.RegisterPluginBuilder(antiAffinitySimulationGradientPluginName, func(framework.Arguments) framework.Plugin {
		return &antiAffinitySimulationGradientPlugin{}
	})
	defer framework.CleanupPluginBuilders()

	for _, reason := range []string{ReasonGangPreempt, ReasonGangReclaim} {
		t.Run(reason, func(t *testing.T) {
			jobID := api.JobID("ns/" + reason)
			task := makeTask(jobID, "t1")
			job := api.NewJobInfo(jobID, task)
			tier := int32(2)
			job.PodGroup = &api.PodGroup{PodGroup: scheduling.PodGroup{
				Spec: scheduling.PodGroupSpec{
					TopologyAffinity: &scheduling.TopologyAffinitySpec{
						PodGroupAntiAffinity: &scheduling.PodGroupAntiAffinity{
							Required: []scheduling.PodGroupAffinityTerm{{
								PodGroupSelector: &metav1.LabelSelector{},
								TopologyTier:     &tier,
							}},
						},
					},
				},
			}}

			deniedNode := api.NewNodeInfo(nil)
			deniedNode.Name = "node-denied"
			deniedNode.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
			deniedNode.Releasing = api.EmptyResource()
			deniedNode.Pipelined = api.EmptyResource()
			allowedNode := api.NewNodeInfo(nil)
			allowedNode.Name = "node-allowed"
			allowedNode.Idle = (&api.Resource{MilliCPU: 2000}).Clone()
			allowedNode.Releasing = api.EmptyResource()
			allowedNode.Pipelined = api.EmptyResource()

			schedulerCache := &cache.SchedulerCache{
				Nodes: map[string]*api.NodeInfo{
					deniedNode.Name:  deniedNode,
					allowedNode.Name: allowedNode,
				},
				Jobs:              map[api.JobID]*api.JobInfo{jobID: job},
				Queues:            map[api.QueueID]*api.QueueInfo{},
				HyperNodesInfo:    api.NewHyperNodesInfo(nil),
				InUseNodesInShard: sets.Set[string]{},
			}
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{{Plugins: []conf.PluginOption{{
				Name:                     antiAffinitySimulationGradientPluginName,
				EnabledHyperNodeGradient: boolPtr(true),
			}}}}, nil)
			ssn.Jobs = map[api.JobID]*api.JobInfo{jobID: job}
			ssn.Nodes = map[string]*api.NodeInfo{
				deniedNode.Name:  deniedNode,
				allowedNode.Name: allowedNode,
			}

			root := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, []*api.NodeInfo{deniedNode, allowedNode})
			allowed := testDomainHyperNode(ssn, "hn-allowed", []*api.NodeInfo{allowedNode})
			ssn.HyperNodes = api.HyperNodeInfoMap{
				root.Name:    root,
				allowed.Name: allowed,
			}

			plan, placements, ok := BuildNominationPlanInDomain(ssn, nil, job, root, nil, reason, true)
			if !assert.True(t, ok) || !assert.NotNil(t, plan) {
				return
			}
			assert.Equal(t, 1, len(plan.Operations()))
			assert.Equal(t, "hn-allowed", placements[job.DefaultSubJobID()])
			assert.Empty(t, task.NodeName, "simulation must leave the session clean")
		})
	}
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
	plan, _, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test", true)
	assert.False(t, ok)
	assert.Nil(t, plan)
}

func TestBuildNominationPlanInDomain_PlanOrRollback(t *testing.T) {
	ensureServerOptsForTest()
	plugins := map[string]framework.PluginBuilder{drf.PluginName: drf.New, gang.PluginName: gang.New}
	tiers := []conf.Tier{{Plugins: []conf.PluginOption{
		{Name: drf.PluginName}, {Name: gang.PluginName, EnabledJobReady: ptr.To(true), EnabledJobPipelined: ptr.To(true)},
	}}}
	resources := api.BuildResourceList("1", "1Gi")
	victim := util.BuildPod("ns", "victim", "node1", v1.PodRunning, resources, "victim", nil, nil)
	first := util.BuildPod("ns", "requester-0", "", v1.PodPending, resources, "requester", nil, nil)
	second := util.BuildPod("ns", "requester-1", "", v1.PodPending, resources, "requester", nil, nil)
	nodes := []*v1.Node{util.BuildNode("node1", api.BuildResourceList("8", "8Gi", api.ScalarResource{Name: "pods", Value: "10"}), nil)}
	queues := []*schedulingv1beta1.Queue{util.BuildQueue("q", 1, nil)}
	reservationErr := errors.New("external reservation unavailable")
	tests := []struct {
		uthelper.TestCommonStruct
		jobID          api.JobID
		victims        map[api.JobID][]api.TaskID
		allocateErrors map[api.TaskID]error
		wantPlan       bool
		wantOperations int
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "placement without eviction",
				Pods: []*v1.Pod{first},
				PodGroups: []*schedulingv1beta1.PodGroup{
					util.BuildPodGroup("requester", "ns", "q", 1, nil, schedulingv1beta1.PodGroupInqueue),
				},
				ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
					"ns/requester": {api.Pending: 1},
				},
			},
			jobID:          "ns/requester",
			wantPlan:       true,
			wantOperations: 1,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "placement with eviction",
				Pods: []*v1.Pod{victim, first},
				PodGroups: []*schedulingv1beta1.PodGroup{
					util.BuildPodGroup("victim", "ns", "q", 1, nil, schedulingv1beta1.PodGroupRunning),
					util.BuildPodGroup("requester", "ns", "q", 1, nil, schedulingv1beta1.PodGroupInqueue),
				},
				ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
					"ns/victim":    {api.Running: 1},
					"ns/requester": {api.Pending: 1},
				},
			},
			jobID:          "ns/requester",
			victims:        map[api.JobID][]api.TaskID{"ns/victim": {"ns-victim"}},
			wantPlan:       true,
			wantOperations: 2,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "first pipeline fails",
				Pods: []*v1.Pod{victim, first},
				PodGroups: []*schedulingv1beta1.PodGroup{
					util.BuildPodGroup("victim", "ns", "q", 1, nil, schedulingv1beta1.PodGroupRunning),
					util.BuildPodGroup("requester", "ns", "q", 1, nil, schedulingv1beta1.PodGroupInqueue),
				},
				ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
					"ns/victim":    {api.Running: 1},
					"ns/requester": {api.Pending: 1},
				},
			},
			allocateErrors: map[api.TaskID]error{"ns-requester-0": reservationErr},
			jobID:          "ns/requester",
			victims:        map[api.JobID][]api.TaskID{"ns/victim": {"ns-victim"}},
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "failure after a successful pipeline",
				Pods: []*v1.Pod{victim, first, second},
				PodGroups: []*schedulingv1beta1.PodGroup{
					util.BuildPodGroup("victim", "ns", "q", 1, nil, schedulingv1beta1.PodGroupRunning),
					util.BuildPodGroup("requester", "ns", "q", 2, nil, schedulingv1beta1.PodGroupInqueue),
				},
				ExpectTaskStatusNums: map[api.JobID]map[api.TaskStatus]int{
					"ns/victim":    {api.Running: 1},
					"ns/requester": {api.Pending: 2},
				},
			},
			allocateErrors: map[api.TaskID]error{"ns-requester-1": reservationErr},
			jobID:          "ns/requester",
			victims:        map[api.JobID][]api.TaskID{"ns/victim": {"ns-victim"}},
		},
	}
	for i, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			tc.Plugins, tc.Nodes, tc.Queues = plugins, nodes, queues
			ssn := tc.RegisterSession(tiers, nil)
			t.Cleanup(tc.Close)
			ssn.AddEventHandler(&framework.EventHandler{
				AllocateFunc: func(event *framework.Event) {
					if err := tc.allocateErrors[event.Task.UID]; err != nil {
						event.Err = err
					}
				},
			})
			job := ssn.Jobs[tc.jobID]
			require.NotNil(t, job)
			var victims []*api.TaskInfo
			for jobID, taskIDs := range tc.victims {
				victimJob := ssn.Jobs[jobID]
				require.NotNil(t, victimJob)
				for _, taskID := range taskIDs {
					task := victimJob.Tasks[taskID]
					require.NotNil(t, task)
					victims = append(victims, task)
				}
			}
			root := ssn.HyperNodes[framework.ClusterTopHyperNode]
			require.NotNil(t, root)
			require.NotPanics(t, func() {
				plan, _, ok := BuildNominationPlanInDomain(ssn, ssn.Queues[job.Queue], job, root, victims, ReasonGangPreempt, false)
				assert.Equal(t, tc.wantPlan, ok)
				if tc.wantPlan {
					require.NotNil(t, plan)
					assert.Len(t, plan.Operations(), tc.wantOperations)
				} else {
					assert.Nil(t, plan)
				}
			})
			assert.NoError(t, tc.CheckAll(i))
		})
	}
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
	plan, _, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test", true)
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
	plan, _, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test", true)
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
	plan, _, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test", true)
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
	plan, _, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test", true)
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
	plan, _, ok := BuildNominationPlanInDomain(ssn, nil, job, hn, nil, "test", true)
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
