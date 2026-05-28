/*
Copyright 2024 The Volcano Authors.

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

package framework

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func newFitErr(taskName, nodeName string, sts ...*api.Status) *api.FitError {
	return api.NewFitErrWithStatus(&api.TaskInfo{Name: taskName}, &api.NodeInfo{Name: nodeName}, sts...)
}

func TestFilterOutPreemptMayNotHelpNodes(t *testing.T) {
	tests := []struct {
		Name      string
		PodGroups []*schedulingv1.PodGroup
		Pods      []*v1.Pod
		Nodes     []*v1.Node
		Queues    []*schedulingv1.Queue
		status    map[api.TaskID]*api.FitError
		want      map[api.TaskID][]string // task's nodes name list which is helpful for preemption
	}{
		{
			Name:      "all are helpful for preemption",
			PodGroups: []*schedulingv1.PodGroup{util.BuildPodGroup("pg1", "c1", "c1", 1, nil, schedulingv1.PodGroupInqueue)},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, nil),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, nil),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
				util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
			},
			Queues: []*schedulingv1.Queue{util.BuildQueue("c1", 1, nil)},
			status: map[api.TaskID]*api.FitError{},
			want:   map[api.TaskID][]string{"c1-p2": {"n1", "n2"}, "c1-p1": {"n1", "n2"}},
		},
		{
			Name:      "master predicate failed: node selector does not match",
			PodGroups: []*schedulingv1.PodGroup{util.BuildPodGroup("pg1", "c1", "c1", 1, nil, schedulingv1.PodGroupInqueue)},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("2", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("2", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
			},
			Nodes:  []*v1.Node{util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"})},
			Queues: []*schedulingv1.Queue{util.BuildQueue("c1", 1, nil)},
			status: map[api.TaskID]*api.FitError{"c1-p1": newFitErr("c1-p1", "n1", &api.Status{Reason: "node(s) didn't match Pod's node selector", Code: api.UnschedulableAndUnresolvable})},
			want:   map[api.TaskID][]string{"c1-p2": {"n1"}, "c1-p1": {}},
		},
		{
			Name:      "p1,p3 has node fit error",
			PodGroups: []*schedulingv1.PodGroup{util.BuildPodGroup("pg1", "c1", "c1", 2, map[string]int32{"master": 1, "worker": 1}, schedulingv1.PodGroupInqueue)},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "p0", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "master"}, map[string]string{"nodeRole": "master"}),
				util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
				util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"nodeRole": "worker"}),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "master"}),
				util.BuildNode("n2", api.BuildResourceList("1", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"nodeRole": "worker"}),
			},
			Queues: []*schedulingv1.Queue{util.BuildQueue("c1", 1, nil)},
			status: map[api.TaskID]*api.FitError{
				"c1-p1": newFitErr("c1-p1", "n2", &api.Status{Reason: "node(s) didn't match Pod's node selector", Code: api.UnschedulableAndUnresolvable}),
				"c1-p3": newFitErr("c1-p3", "n1", &api.Status{Reason: "node(s) didn't match Pod's node selector", Code: api.UnschedulableAndUnresolvable}),
			},
			// notes that are useful for preempting
			want: map[api.TaskID][]string{
				"c1-p0": {"n1", "n2"},
				"c1-p1": {"n1"},
				"c1-p2": {"n1", "n2"},
				"c1-p3": {"n2"},
			},
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			scherCache := cache.NewDefaultMockSchedulerCache("test-scheduler")
			for _, node := range test.Nodes {
				scherCache.AddOrUpdateNode(node)
			}
			for _, pod := range test.Pods {
				scherCache.AddPod(pod)
			}
			for _, pg := range test.PodGroups {
				scherCache.AddPodGroupV1beta1(pg)
			}
			for _, queue := range test.Queues {
				scherCache.AddQueueV1beta1(queue)
			}
			ssn := OpenSession(scherCache, nil, nil)
			defer CloseSession(ssn)
			for _, job := range ssn.Jobs {
				for _, task := range job.TaskStatusIndex[api.Pending] {
					if fitErr, exist := test.status[task.UID]; exist {
						fe := api.NewFitErrors()
						fe.SetNodeError(fitErr.NodeName, fitErr)
						job.NodesFitErrors[task.UID] = fe
					}

					// check potential nodes
					potentialNodes := ssn.FilterOutUnschedulableAndUnresolvableNodesForTask(task)
					want := test.want[task.UID]
					got := make([]string, 0, len(potentialNodes))
					for _, node := range potentialNodes {
						got = append(got, node.Name)
					}
					assert.Equal(t, want, got, fmt.Sprintf("case %d: task %s", i, task.UID))
				}
			}
		})
	}
}

func TestUnifiedEvictable_TierWalkAndIntersection(t *testing.T) {
	taskA := &api.TaskInfo{UID: "a"}
	taskB := &api.TaskInfo{UID: "b"}
	taskC := &api.TaskInfo{UID: "c"}
	candidates := []*api.TaskInfo{taskA, taskB, taskC}
	ctx := &api.EvictionContext{Kind: api.EvictionKindGangPreempt}

	ssn := &Session{
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{Name: "p1"},
					{Name: "p2"},
				},
			},
		},
		unifiedEvictableFns: map[string]api.UnifiedEvictableFn{},
	}

	ssn.AddUnifiedEvictableFn("p1", func(_ *api.EvictionContext, c []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return []*api.TaskInfo{taskA, taskB}, 1
	})
	ssn.AddUnifiedEvictableFn("p2", func(_ *api.EvictionContext, c []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return []*api.TaskInfo{taskB, taskC}, 1
	})

	result := ssn.UnifiedEvictable(ctx, candidates)
	assert.Len(t, result, 1)
	assert.Equal(t, api.TaskID("b"), result[0].UID)
}

func TestUnifiedEvictable_AbstainSkipped(t *testing.T) {
	taskA := &api.TaskInfo{UID: "a"}
	ctx := &api.EvictionContext{Kind: api.EvictionKindGangPreempt}

	ssn := &Session{
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{Name: "abstainer"},
					{Name: "permitter"},
				},
			},
		},
		unifiedEvictableFns: map[string]api.UnifiedEvictableFn{},
	}

	ssn.AddUnifiedEvictableFn("abstainer", func(_ *api.EvictionContext, c []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return nil, 0
	})
	ssn.AddUnifiedEvictableFn("permitter", func(_ *api.EvictionContext, c []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return []*api.TaskInfo{taskA}, 1
	})

	result := ssn.UnifiedEvictable(ctx, []*api.TaskInfo{taskA})
	assert.Len(t, result, 1)
	assert.Equal(t, api.TaskID("a"), result[0].UID)
}

func TestUnifiedEvictable_EmptyCandidatesBlocksTier(t *testing.T) {
	taskA := &api.TaskInfo{UID: "a"}
	ctx := &api.EvictionContext{Kind: api.EvictionKindGangReclaim}

	ssn := &Session{
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{Name: "blocker"},
					{Name: "permitter"},
				},
			},
			{
				Plugins: []conf.PluginOption{
					{Name: "fallback"},
				},
			},
		},
		unifiedEvictableFns: map[string]api.UnifiedEvictableFn{},
	}

	ssn.AddUnifiedEvictableFn("blocker", func(_ *api.EvictionContext, c []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return []*api.TaskInfo{}, 1 // non-abstain + empty => block
	})
	ssn.AddUnifiedEvictableFn("permitter", func(_ *api.EvictionContext, c []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return []*api.TaskInfo{taskA}, 1
	})
	ssn.AddUnifiedEvictableFn("fallback", func(_ *api.EvictionContext, c []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return []*api.TaskInfo{taskA}, 1
	})

	result := ssn.UnifiedEvictable(ctx, []*api.TaskInfo{taskA})
	// blocker in tier 0 returns empty with Permit, which sets victims=nil and breaks.
	// victims is nil after the tier-0 loop so tier 1 is tried and fallback returns taskA.
	assert.Len(t, result, 1)
	assert.Equal(t, api.TaskID("a"), result[0].UID)
}

func TestUnifiedEvictable_NoPluginsReturnsNil(t *testing.T) {
	ctx := &api.EvictionContext{Kind: api.EvictionKindGangPreempt}
	ssn := &Session{
		Tiers: []conf.Tier{
			{Plugins: []conf.PluginOption{{Name: "noop"}}},
		},
		unifiedEvictableFns: map[string]api.UnifiedEvictableFn{},
	}

	result := ssn.UnifiedEvictable(ctx, []*api.TaskInfo{{UID: "a"}})
	assert.Nil(t, result)
}

func TestUnifiedEvictable_ContextPassedThrough(t *testing.T) {
	taskA := &api.TaskInfo{UID: "a"}
	job := &api.JobInfo{UID: "job1", Priority: 42}
	ctx := &api.EvictionContext{
		Kind:      api.EvictionKindGangReclaim,
		Job:       job,
		HyperNode: "hn1",
	}

	var receivedCtx *api.EvictionContext
	ssn := &Session{
		Tiers: []conf.Tier{
			{Plugins: []conf.PluginOption{{Name: "spy"}}},
		},
		unifiedEvictableFns: map[string]api.UnifiedEvictableFn{},
	}
	ssn.AddUnifiedEvictableFn("spy", func(c *api.EvictionContext, candidates []*api.TaskInfo) ([]*api.TaskInfo, int) {
		receivedCtx = c
		return candidates, 1
	})

	ssn.UnifiedEvictable(ctx, []*api.TaskInfo{taskA})
	assert.Equal(t, api.EvictionKindGangReclaim, receivedCtx.Kind)
	assert.Equal(t, api.JobID("job1"), receivedCtx.Job.UID)
	assert.Equal(t, "hn1", receivedCtx.HyperNode)
}

func TestHyperNodeGradientForJobFn_ForwardsPurposeAndKeepsWinnerTakesAll(t *testing.T) {
	enabled := true
	ssn := &Session{
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{Name: "p1", EnabledHyperNodeGradient: &enabled},
					{Name: "p2", EnabledHyperNodeGradient: &enabled},
				},
			},
		},
		hyperNodeGradientForJobFns: map[string]api.HyperNodeGradientForJobFn{},
	}

	root := &api.HyperNodeInfo{Name: "root"}
	hn1 := &api.HyperNodeInfo{Name: "hn1"}
	hn2 := &api.HyperNodeInfo{Name: "hn2"}

	var gotPurpose api.SearchPurpose
	ssn.AddHyperNodeGradientForJobFn("p1", func(job *api.JobInfo, hyperNode *api.HyperNodeInfo, purpose api.SearchPurpose) [][]*api.HyperNodeInfo {
		gotPurpose = purpose
		return [][]*api.HyperNodeInfo{{hn1}}
	})
	ssn.AddHyperNodeGradientForJobFn("p2", func(job *api.JobInfo, hyperNode *api.HyperNodeInfo, purpose api.SearchPurpose) [][]*api.HyperNodeInfo {
		return [][]*api.HyperNodeInfo{{hn2}}
	})

	result := ssn.HyperNodeGradientForJobFn(&api.JobInfo{}, root, api.PurposeEvict)
	assert.Equal(t, api.PurposeEvict, gotPurpose)
	assert.Equal(t, [][]*api.HyperNodeInfo{{hn1}}, result)
}

func TestHyperNodeGradientForJobFn_NoPluginKeepsCurrentFallback(t *testing.T) {
	ssn := &Session{
		hyperNodeGradientForJobFns: map[string]api.HyperNodeGradientForJobFn{},
	}
	root := &api.HyperNodeInfo{Name: "root"}
	result := ssn.HyperNodeGradientForJobFn(&api.JobInfo{}, root, api.PurposeEvict)
	assert.Equal(t, [][]*api.HyperNodeInfo{{root}}, result)
}
