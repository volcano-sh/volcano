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
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	scheduling "volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const emptyGradientPluginName = "test-empty-gradient"

type emptyGradientPlugin struct{}

func (p *emptyGradientPlugin) Name() string { return emptyGradientPluginName }

func (p *emptyGradientPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddHyperNodeGradientForJobFn(p.Name(), func(*api.JobInfo, *api.HyperNodeInfo, api.SearchPurpose) api.HyperNodeGradientResult {
		return api.HyperNodeGradientResult{}
	})
}

func (p *emptyGradientPlugin) OnSessionClose(*framework.Session) {}

func TestGetCandidateDomains_EmptyGradient_HardTopologyNoFallback(t *testing.T) {
	framework.RegisterPluginBuilder(emptyGradientPluginName, func(framework.Arguments) framework.Plugin {
		return &emptyGradientPlugin{}
	})
	defer framework.CleanupPluginBuilders()

	schedulerCache := &cache.SchedulerCache{
		Nodes:             map[string]*api.NodeInfo{},
		Jobs:              map[api.JobID]*api.JobInfo{},
		Queues:            map[api.QueueID]*api.QueueInfo{},
		HyperNodesInfo:    api.NewHyperNodesInfo(nil),
		InUseNodesInShard: sets.Set[string]{},
	}
	ssn := framework.OpenSession(schedulerCache, []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                     emptyGradientPluginName,
					EnabledHyperNodeGradient: boolPtr(true),
				},
			},
		},
	}, nil)
	defer framework.CloseSession(ssn)

	ssn.HyperNodes = map[string]*api.HyperNodeInfo{
		framework.ClusterTopHyperNode: {Name: framework.ClusterTopHyperNode},
	}

	highestTierAllowed := 1
	job := &api.JobInfo{
		NetworkTopology: &scheduling.NetworkTopologySpec{
			Mode:               scheduling.HardNetworkTopologyMode,
			HighestTierAllowed: &highestTierAllowed,
		},
	}

	domains := GetCandidateDomains(ssn, job, nil, 8)
	assert.Empty(t, domains)
}

func TestGetCandidateDomains_EmptyGradient_HardPodGroupAntiAffinityNoFallback(t *testing.T) {
	framework.RegisterPluginBuilder(emptyGradientPluginName, func(framework.Arguments) framework.Plugin {
		return &emptyGradientPlugin{}
	})
	defer framework.CleanupPluginBuilders()

	schedulerCache := &cache.SchedulerCache{
		Nodes:             map[string]*api.NodeInfo{},
		Jobs:              map[api.JobID]*api.JobInfo{},
		Queues:            map[api.QueueID]*api.QueueInfo{},
		HyperNodesInfo:    api.NewHyperNodesInfo(nil),
		InUseNodesInShard: sets.Set[string]{},
	}
	ssn := framework.OpenSession(schedulerCache, []conf.Tier{{Plugins: []conf.PluginOption{{
		Name:                     emptyGradientPluginName,
		EnabledHyperNodeGradient: boolPtr(true),
	}}}}, nil)
	defer framework.CloseSession(ssn)

	ssn.HyperNodes = map[string]*api.HyperNodeInfo{
		framework.ClusterTopHyperNode: {Name: framework.ClusterTopHyperNode},
	}
	tier := int32(1)
	job := &api.JobInfo{PodGroup: &api.PodGroup{PodGroup: scheduling.PodGroup{
		Spec: scheduling.PodGroupSpec{TopologyAffinity: &scheduling.TopologyAffinitySpec{
			PodGroupAntiAffinity: &scheduling.PodGroupAntiAffinity{Required: []scheduling.PodGroupAffinityTerm{{
				PodGroupSelector: &metav1.LabelSelector{},
				TopologyTier:     &tier,
			}}},
		}},
	}}}

	domains := GetCandidateDomains(ssn, job, nil, 8)
	assert.Empty(t, domains, "required PodGroup anti-affinity must not fall back to the cluster root")
}

func TestGetCandidateDomains_EmptyGradient_NonHardTopologyFallbackToRoot(t *testing.T) {
	framework.RegisterPluginBuilder(emptyGradientPluginName, func(framework.Arguments) framework.Plugin {
		return &emptyGradientPlugin{}
	})
	defer framework.CleanupPluginBuilders()

	schedulerCache := &cache.SchedulerCache{
		Nodes:             map[string]*api.NodeInfo{},
		Jobs:              map[api.JobID]*api.JobInfo{},
		Queues:            map[api.QueueID]*api.QueueInfo{},
		HyperNodesInfo:    api.NewHyperNodesInfo(nil),
		InUseNodesInShard: sets.Set[string]{},
	}
	ssn := framework.OpenSession(schedulerCache, []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                     emptyGradientPluginName,
					EnabledHyperNodeGradient: boolPtr(true),
				},
			},
		},
	}, nil)
	defer framework.CloseSession(ssn)

	ssn.HyperNodes = map[string]*api.HyperNodeInfo{
		framework.ClusterTopHyperNode: {Name: framework.ClusterTopHyperNode},
	}

	domains := GetCandidateDomains(ssn, &api.JobInfo{}, nil, 8)
	assert.Equal(t, []string{framework.ClusterTopHyperNode}, domains)
}

const blockGradientPluginName = "test-block-gradient"

// blockGradientPlugin offers every block as an eviction domain and admits a
// task only on nodes of its node group. Like the k8s filters, its predicate
// fails with an Error when the task's pre-predicate state is missing.
type blockGradientPlugin struct {
	blocks        []*api.HyperNodeInfo
	resolvable    bool
	prePredicated sets.Set[api.TaskID]
}

func (p *blockGradientPlugin) Name() string { return blockGradientPluginName }

func (p *blockGradientPlugin) OnSessionOpen(ssn *framework.Session) {
	p.prePredicated = sets.New[api.TaskID]()
	ssn.AddHyperNodeGradientForJobFn(p.Name(), func(*api.JobInfo, *api.HyperNodeInfo, api.SearchPurpose) api.HyperNodeGradientResult {
		return api.HyperNodeGradientResult{Gradients: [][]*api.HyperNodeInfo{p.blocks}}
	})
	ssn.AddPrePredicateFn(p.Name(), func(task *api.TaskInfo) error {
		p.prePredicated.Insert(task.UID)
		return nil
	})
	ssn.AddPredicateFn(p.Name(), func(task *api.TaskInfo, node *api.NodeInfo) error {
		if !p.prePredicated.Has(task.UID) {
			return api.NewFitErrWithStatus(task, node, &api.Status{Code: api.Error, Reason: "pre-predicate state not found"})
		}
		if node.Node.Labels[v1beta1.NodeGroupNameKey] == task.Pod.Labels[v1beta1.NodeGroupNameKey] {
			return nil
		}
		code := api.UnschedulableAndUnresolvable
		if p.resolvable {
			code = api.Unschedulable
		}
		return api.NewFitErrWithStatus(task, node, &api.Status{Code: code, Reason: "node group mismatch"})
	})
}

func (p *blockGradientPlugin) OnSessionClose(*framework.Session) {}

func TestGetCandidateDomains_SkipsDomainsWithoutFeasibleNodes(t *testing.T) {
	const blockCount = 9
	highestTierAllowed := 1
	hardTopologyJob := &api.JobInfo{
		NetworkTopology: &scheduling.NetworkTopologySpec{
			Mode:               scheduling.HardNetworkTopologyMode,
			HighestTierAllowed: &highestTierAllowed,
		},
	}
	type pendingTask struct {
		role      string
		nodeGroup string
	}
	teamAWorker := []pendingTask{{role: "worker", nodeGroup: "team-a"}}

	tests := []struct {
		name string
		job  *api.JobInfo
		// teamABlocks are in node group team-a; the rest are in shared.
		teamABlocks []int
		pending     []pendingTask
		resolvable  bool
		maxDomains  int
		want        []string
	}{
		{
			name:        "only feasible block sorts past the cap",
			job:         hardTopologyJob,
			teamABlocks: []int{9},
			pending:     teamAWorker,
			maxDomains:  8,
			want:        []string{"block-9"},
		},
		{
			name:        "feasible blocks keep name order and cap",
			job:         hardTopologyJob,
			teamABlocks: []int{2, 4, 6, 8},
			pending:     teamAWorker,
			maxDomains:  3,
			want:        []string{"block-2", "block-4", "block-6"},
		},
		{
			name:        "every block feasible",
			job:         hardTopologyJob,
			teamABlocks: []int{1, 2, 3, 4, 5, 6, 7, 8, 9},
			pending:     teamAWorker,
			maxDomains:  3,
			want:        []string{"block-1", "block-2", "block-3"},
		},
		{
			name:        "resolvable predicate failure keeps the block",
			job:         hardTopologyJob,
			teamABlocks: []int{9},
			pending:     teamAWorker,
			resolvable:  true,
			maxDomains:  3,
			want:        []string{"block-1", "block-2", "block-3"},
		},
		{
			name:        "second role fits a block the first role does not",
			job:         hardTopologyJob,
			teamABlocks: []int{9},
			pending: []pendingTask{
				{role: "worker", nodeGroup: "team-b"},
				{role: "worker", nodeGroup: "team-b"},
				{role: "ps", nodeGroup: "team-a"},
			},
			maxDomains: 8,
			want:       []string{"block-9"},
		},
		{
			name:       "no feasible block under hard topology",
			job:        hardTopologyJob,
			pending:    teamAWorker,
			maxDomains: 8,
			want:       []string{},
		},
		{
			name:       "no feasible block falls back to the cluster root",
			job:        &api.JobInfo{},
			pending:    teamAWorker,
			maxDomains: 8,
			want:       []string{framework.ClusterTopHyperNode},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &blockGradientPlugin{resolvable: tt.resolvable}
			framework.RegisterPluginBuilder(blockGradientPluginName, func(framework.Arguments) framework.Plugin {
				return plugin
			})
			defer framework.CleanupPluginBuilders()

			schedulerCache := &cache.SchedulerCache{
				Nodes:             map[string]*api.NodeInfo{},
				Jobs:              map[api.JobID]*api.JobInfo{},
				Queues:            map[api.QueueID]*api.QueueInfo{},
				HyperNodesInfo:    api.NewHyperNodesInfo(nil),
				InUseNodesInShard: sets.Set[string]{},
			}
			ssn := framework.OpenSession(schedulerCache, []conf.Tier{{Plugins: []conf.PluginOption{{
				Name:                     blockGradientPluginName,
				EnabledHyperNodeGradient: boolPtr(true),
				EnabledPredicate:         boolPtr(true),
			}}}}, nil)
			defer framework.CloseSession(ssn)

			ssn.HyperNodes = map[string]*api.HyperNodeInfo{
				framework.ClusterTopHyperNode: {Name: framework.ClusterTopHyperNode},
			}
			for block := 1; block <= blockCount; block++ {
				name := fmt.Sprintf("block-%d", block)
				group := "shared"
				if slices.Contains(tt.teamABlocks, block) {
					group = "team-a"
				}
				node := api.NewNodeInfo(util.BuildNode(name+"-node", nil, map[string]string{v1beta1.NodeGroupNameKey: group}))
				testDomainHyperNode(ssn, name, []*api.NodeInfo{node})
				plugin.blocks = append(plugin.blocks, api.NewHyperNodeInfo(&topologyv1alpha1.HyperNode{
					ObjectMeta: metav1.ObjectMeta{Name: name},
					Spec:       topologyv1alpha1.HyperNodeSpec{Tier: 1},
				}))
			}

			pending := make([]*api.TaskInfo, 0, len(tt.pending))
			for i, p := range tt.pending {
				task := makeTask("ns/job", fmt.Sprintf("%s-%d", p.role, i))
				task.TaskRole = p.role
				task.Pod.Labels = map[string]string{v1beta1.NodeGroupNameKey: p.nodeGroup}
				pending = append(pending, task)
			}

			domains := GetCandidateDomains(ssn, tt.job, pending, tt.maxDomains)
			assert.Equal(t, tt.want, domains)
		})
	}
}

func boolPtr(v bool) *bool { return &v }

// TestApplySubJobNominations_SetsNominationAndMarksJobDirty asserts the helper
// both pins each SubJob's NominatedHyperNode and marks the job dirty so the
// cache writeback path persists the pin into the next scheduling cycle.
func TestApplySubJobNominations_SetsNominationAndMarksJobDirty(t *testing.T) {
	jobID := api.JobID("ns/job")
	subA := api.SubJobID("sub-a")
	subB := api.SubJobID("sub-b")
	job := &api.JobInfo{
		UID: jobID,
		SubJobs: map[api.SubJobID]*api.SubJobInfo{
			subA: {UID: subA, Job: jobID},
			subB: {UID: subB, Job: jobID},
		},
	}
	ssn := &framework.Session{DirtyJobs: sets.New[api.JobID]()}

	ApplySubJobNominations(ssn, job, map[api.SubJobID]string{
		subA: "hn-a",
		subB: "hn-b",
	})

	assert.Equal(t, "hn-a", job.SubJobs[subA].NominatedHyperNode)
	assert.Equal(t, "hn-b", job.SubJobs[subB].NominatedHyperNode)
	assert.True(t, ssn.DirtyJobs.Has(jobID),
		"job must be marked dirty so cache.updateJobInfo propagates NominatedHyperNode")
}

// TestApplySubJobNominations_EmptyHyperNodeSkipped ensures empty entries do
// not overwrite an existing pin or mark the job dirty by themselves.
func TestApplySubJobNominations_EmptyHyperNodeSkipped(t *testing.T) {
	jobID := api.JobID("ns/job")
	sub := api.SubJobID("sub-a")
	job := &api.JobInfo{
		UID: jobID,
		SubJobs: map[api.SubJobID]*api.SubJobInfo{
			sub: {UID: sub, Job: jobID, NominatedHyperNode: "existing"},
		},
	}
	ssn := &framework.Session{DirtyJobs: sets.New[api.JobID]()}

	ApplySubJobNominations(ssn, job, map[api.SubJobID]string{sub: ""})

	assert.Equal(t, "existing", job.SubJobs[sub].NominatedHyperNode,
		"empty hyperNode entry must not overwrite an existing nomination")
	assert.False(t, ssn.DirtyJobs.Has(jobID),
		"no-op application must not mark the job dirty")
}

// TestApplySubJobNominations_NilSessionDoesNotPanic guards the helper from
// callers (e.g. legacy tests) that don't pass a session.
func TestApplySubJobNominations_NilSessionDoesNotPanic(t *testing.T) {
	jobID := api.JobID("ns/job")
	sub := api.SubJobID("sub-a")
	job := &api.JobInfo{
		UID: jobID,
		SubJobs: map[api.SubJobID]*api.SubJobInfo{
			sub: {UID: sub, Job: jobID},
		},
	}
	assert.NotPanics(t, func() {
		ApplySubJobNominations(nil, job, map[api.SubJobID]string{sub: "hn"})
	})
	assert.Equal(t, "hn", job.SubJobs[sub].NominatedHyperNode)
}
