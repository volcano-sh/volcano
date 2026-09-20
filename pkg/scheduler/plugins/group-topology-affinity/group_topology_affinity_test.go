/*
Copyright 2025 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the License and the specific language governing permissions and
limitations under the License.
*/

package grouptopologyaffinity

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	fwk "k8s.io/kube-scheduler/framework"

	scheduling "volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const testGroupLabel = "topology.volcano.sh/group"

func TestNew(t *testing.T) {
	tests := []struct {
		name           string
		args           framework.Arguments
		expectedWeight int
	}{
		{name: "default weight", args: framework.Arguments{}, expectedWeight: DefaultWeight},
		{name: "custom weight", args: framework.Arguments{PluginWeight: 3}, expectedWeight: 3},
		{name: "negative weight falls back", args: framework.Arguments{PluginWeight: -1}, expectedWeight: DefaultWeight},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := New(tt.args).(*groupTopologyAffinityPlugin)
			if plugin.weight != tt.expectedWeight {
				t.Fatalf("expected weight %d, got %d", tt.expectedWeight, plugin.weight)
			}
			if plugin.Name() != PluginName {
				t.Fatalf("unexpected plugin name %s", plugin.Name())
			}
		})
	}
}

func TestHyperNodeGradientForPodGroupAntiAffinity(t *testing.T) {
	prodSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{testGroupLabel: "prod"},
	}
	supernodeTerm := scheduling.PodGroupAffinityTerm{
		PodGroupSelector: prodSelector,
		TopologyTierName: "supernode",
	}
	tier2 := int32(2)
	supernodeTermByTier := scheduling.PodGroupAffinityTerm{
		PodGroupSelector: prodSelector,
		TopologyTier:     &tier2,
	}

	tests := []struct {
		name           string
		hn             api.HyperNodeInfoMap
		setByTier      map[int]sets.Set[string]
		jobs           map[api.JobID]*api.JobInfo
		selfJob        *api.JobInfo
		root           string
		wantEmpty      bool
		wantHyperNodes []string
		wantTierOrder  []int
	}{
		{
			name:           "no podGroupAntiAffinity passes through full subtree",
			hn:             buildTwoSupernodeTree(),
			setByTier:      twoSupernodeSetByTier(),
			jobs:           map[api.JobID]*api.JobInfo{},
			selfJob:        jobWithTopologyAffinity(nil, nil),
			root:           "root",
			wantHyperNodes: []string{"sn-a", "sn-b", "root"},
			wantTierOrder:  []int{2, 3},
		},
		{
			name:      "preferred only returns full subtree for hyperNode order",
			hn:        buildTwoSupernodeTree(),
			setByTier: twoSupernodeSetByTier(),
			jobs:      map[api.JobID]*api.JobInfo{},
			selfJob: jobWithTopologyAffinity(nil, []scheduling.PodGroupAffinityTerm{
				{
					PodGroupSelector: prodSelector,
					TopologyTierName: "supernode",
					Weight:           50,
				},
			}),
			root:           "root",
			wantHyperNodes: []string{"sn-a", "sn-b", "root"},
			wantTierOrder:  []int{2, 3},
		},
		{
			name:           "matching PodGroup on sn-a leaves sn-b",
			hn:             buildTwoSupernodeTree(),
			setByTier:      twoSupernodeSetByTier(),
			jobs:           map[api.JobID]*api.JobInfo{"other": otherJobOn("other", "sn-a", "prod")},
			selfJob:        jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{supernodeTerm}, nil),
			root:           "root",
			wantHyperNodes: []string{"sn-b"},
			wantTierOrder:  []int{2},
		},
		{
			name:           "no matching PodGroup label keeps both supernodes",
			hn:             buildTwoSupernodeTree(),
			setByTier:      twoSupernodeSetByTier(),
			jobs:           map[api.JobID]*api.JobInfo{"other": otherJobOn("other", "sn-a", "staging")},
			selfJob:        jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{supernodeTerm}, nil),
			root:           "root",
			wantHyperNodes: []string{"sn-a", "sn-b"},
			wantTierOrder:  []int{2},
		},
		{
			name:      "all supernodes occupied yields empty gradient",
			hn:        buildTwoSupernodeTree(),
			setByTier: twoSupernodeSetByTier(),
			jobs: map[api.JobID]*api.JobInfo{
				"j1": otherJobOn("j1", "sn-a", "prod"),
				"j2": otherJobOn("j2", "sn-b", "prod"),
			},
			selfJob:   jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{supernodeTerm}, nil),
			root:      "root",
			wantEmpty: true,
		},
		{
			name:           "topologyTier int selector works like tierName",
			hn:             buildTwoSupernodeTree(),
			setByTier:      twoSupernodeSetByTier(),
			jobs:           map[api.JobID]*api.JobInfo{"other": otherJobOn("other", "sn-a", "prod")},
			selfJob:        jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{supernodeTermByTier}, nil),
			root:           "root",
			wantHyperNodes: []string{"sn-b"},
		},
		{
			name:      "multiple required terms are ANDed",
			hn:        buildRackUnderSupernodeTree(),
			setByTier: rackSupernodeSetByTier(),
			jobs: map[api.JobID]*api.JobInfo{
				"j-sn": otherJobOn("j-sn", "sn-a", "prod"),
				"j-rk": otherJobOn("j-rk", "cab-a", "prod"),
			},
			selfJob: jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{
				supernodeTerm,
				{
					PodGroupSelector: prodSelector,
					TopologyTierName: "rack",
				},
			}, nil),
			root:           "root",
			wantHyperNodes: []string{"cab-b"},
			wantTierOrder:  []int{1},
		},
		{
			name:           "coarse root skipped but finer descendants remain",
			hn:             buildRackUnderSupernodeTree(),
			setByTier:      rackSupernodeSetByTier(),
			jobs:           map[api.JobID]*api.JobInfo{"other": otherJobOn("other", "sn-a", "prod")},
			selfJob:        jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{supernodeTerm}, nil),
			root:           "root",
			wantHyperNodes: []string{"sn-b", "cab-b"},
			wantTierOrder:  []int{1, 2},
		},
		{
			// podgroup-0 has no topologyAffinity; podgroup-1 anti-affinity should still see its placement.
			name:      "podgroup-1 anti-affinity against podgroup-0 without topologyAffinity",
			hn:        buildTwoSupernodeTree(),
			setByTier: twoSupernodeSetByTier(),
			jobs: map[api.JobID]*api.JobInfo{
				"podgroup-0": otherJobWithTaskOnNode("podgroup-0", "node-a", "prod"),
			},
			selfJob:        jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{supernodeTerm}, nil),
			root:           "root",
			wantHyperNodes: []string{"sn-b"},
			wantTierOrder:  []int{2},
		},
		{
			name:           "follow-up still excludes occupied supernode",
			hn:             buildTwoSupernodeTree(),
			setByTier:      twoSupernodeSetByTier(),
			jobs:           map[api.JobID]*api.JobInfo{"other": otherJobOn("other", "sn-a", "prod")},
			selfJob:        jobWithAllocatedHyperNode("self", "sn-b", supernodeTerm),
			root:           "root",
			wantHyperNodes: []string{"sn-b"},
			wantTierOrder:  []int{2},
		},
		{
			name:      "follow-up after minMember partial placement rejects peer supernode",
			hn:        buildTwoSupernodeTree(),
			setByTier: twoSupernodeSetByTier(),
			jobs: map[api.JobID]*api.JobInfo{
				"peer-instance": otherJobOn("peer-instance", "sn-a", "prod"),
			},
			selfJob: func() *api.JobInfo {
				job := jobWithAllocatedHyperNode("self-instance", "sn-b", supernodeTerm)
				task1 := &api.TaskInfo{UID: "task-1"}
				task1.Status = api.Allocated
				task1.NodeName = "node-b1"
				task2 := &api.TaskInfo{UID: "task-2"}
				task2.Status = api.Allocated
				task2.NodeName = "node-b2"
				subJobID := api.SubJobID("self-instance")
				job.SubJobs = map[api.SubJobID]*api.SubJobInfo{
					subJobID: {
						UID:                subJobID,
						AllocatedHyperNode: "sn-b",
						TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
							api.Allocated: {
								task1.UID: task1,
								task2.UID: task2,
							},
						},
					},
				}
				return job
			}(),
			root:           "root",
			wantHyperNodes: []string{"sn-b"},
			wantTierOrder:  []int{2},
		},
		{
			name:      "invalid term tierName yields empty gradient",
			hn:        buildTwoSupernodeTree(),
			setByTier: twoSupernodeSetByTier(),
			jobs:      map[api.JobID]*api.JobInfo{},
			selfJob: jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{
				{
					PodGroupSelector: prodSelector,
					TopologyTierName: "unknown-tier",
				},
			}, nil),
			root:      "root",
			wantEmpty: true,
		},
		{
			name:      "matching job spanning two supernodes leaves third supernode",
			hn:        buildThreeSupernodeTree(),
			setByTier: threeSupernodeSetByTier(),
			jobs: map[api.JobID]*api.JobInfo{
				"podgroup-1": otherJobWithTasksOnNodes("podgroup-1", "prod", "node-a", "node-b"),
			},
			selfJob:        jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{supernodeTerm}, nil),
			root:           "root",
			wantHyperNodes: []string{"sn-c"},
			wantTierOrder:  []int{2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ssn := &framework.Session{
				Jobs:                 tt.jobs,
				HyperNodes:           tt.hn,
				HyperNodesSetByTier:  tt.setByTier,
				HyperNodeTierNameMap: defaultTierNameMap(),
				RealNodesSet:         defaultRealNodesSet(),
			}
			plugin := New(framework.Arguments{}).(*groupTopologyAffinityPlugin)

			gradients := plugin.hyperNodeGradientForJob(ssn, tt.selfJob, tt.hn[tt.root])
			if tt.wantEmpty {
				if len(gradients) != 0 {
					t.Fatalf("expected empty gradient, got %#v", gradients)
				}
				return
			}

			gotNames := hyperNodeNamesFromGradients(gradients)
			if !sets.New(tt.wantHyperNodes...).Equal(sets.New(gotNames...)) {
				t.Fatalf("eligible hyperNodes mismatch: want %v, got %v", tt.wantHyperNodes, gotNames)
			}
			if tt.wantTierOrder != nil {
				gotTiers := tiersFromGradients(gradients)
				if len(gotTiers) != len(tt.wantTierOrder) {
					t.Fatalf("tier group count mismatch: want %v, got %v", tt.wantTierOrder, gotTiers)
				}
				for i, wantTier := range tt.wantTierOrder {
					if gotTiers[i] != wantTier {
						t.Fatalf("tier order mismatch at %d: want %d, got %d", i, wantTier, gotTiers[i])
					}
				}
			}
		})
	}
}

func TestHyperNodeGradientForSubJobPreferredOnly(t *testing.T) {
	prodSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{testGroupLabel: "prod"},
	}
	selfJob := jobWithTopologyAffinity(nil, []scheduling.PodGroupAffinityTerm{
		{
			PodGroupSelector: prodSelector,
			TopologyTierName: "supernode",
			Weight:           50,
		},
	})
	subJob := &api.SubJobInfo{UID: "subjob-1", Job: selfJob.UID}
	hn := buildTwoSupernodeTree()
	ssn := &framework.Session{
		Jobs:                 map[api.JobID]*api.JobInfo{selfJob.UID: selfJob},
		HyperNodes:           hn,
		HyperNodesSetByTier:  twoSupernodeSetByTier(),
		HyperNodeTierNameMap: defaultTierNameMap(),
	}
	plugin := New(framework.Arguments{}).(*groupTopologyAffinityPlugin)

	gradients := plugin.hyperNodeGradientForSubJob(ssn, selfJob, subJob, hn["root"])
	gotNames := hyperNodeNamesFromGradients(gradients)
	if !sets.New("sn-a", "sn-b", "root").Equal(sets.New(gotNames...)) {
		t.Fatalf("eligible hyperNodes mismatch: got %v", gotNames)
	}
}

func TestHyperNodeOrderFn(t *testing.T) {
	prodSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{testGroupLabel: "prod"},
	}
	preferredTerm := scheduling.PodGroupAffinityTerm{
		PodGroupSelector: prodSelector,
		TopologyTierName: "supernode",
		Weight:           50,
	}

	tests := []struct {
		name       string
		weight     int
		jobs       map[api.JobID]*api.JobInfo
		selfJob    *api.JobInfo
		candidates map[string][]*api.NodeInfo
		wantNil    bool
		wantScores map[string]float64
		wantErr    bool
	}{
		{
			name:    "no preferred terms returns nil",
			selfJob: jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{{PodGroupSelector: prodSelector, TopologyTierName: "supernode"}}, nil),
			candidates: map[string][]*api.NodeInfo{
				"sn-a": {},
				"sn-b": {},
			},
			wantNil: true,
		},
		{
			name:       "conflict reduces score by term weight",
			weight:     1,
			jobs:       map[api.JobID]*api.JobInfo{"other": otherJobOn("other", "sn-a", "prod")},
			selfJob:    jobWithTopologyAffinity(nil, []scheduling.PodGroupAffinityTerm{preferredTerm}),
			candidates: map[string][]*api.NodeInfo{"sn-a": {}, "sn-b": {}},
			wantScores: map[string]float64{
				"sn-a": 0.5 * float64(fwk.MaxNodeScore),
				"sn-b": 1.0 * float64(fwk.MaxNodeScore),
			},
		},
		{
			name:       "plugin weight scales final score",
			weight:     2,
			jobs:       map[api.JobID]*api.JobInfo{},
			selfJob:    jobWithTopologyAffinity(nil, []scheduling.PodGroupAffinityTerm{preferredTerm}),
			candidates: map[string][]*api.NodeInfo{"sn-a": {}, "sn-b": {}},
			wantScores: map[string]float64{
				"sn-a": 2.0 * float64(fwk.MaxNodeScore),
				"sn-b": 2.0 * float64(fwk.MaxNodeScore),
			},
		},
		{
			name:   "invalid term weight is ignored",
			weight: 1,
			jobs:   map[api.JobID]*api.JobInfo{"other": otherJobOn("other", "sn-a", "prod")},
			selfJob: jobWithTopologyAffinity(nil, []scheduling.PodGroupAffinityTerm{
				{
					PodGroupSelector: prodSelector,
					TopologyTierName: "supernode",
					Weight:           0,
				},
			}),
			candidates: map[string][]*api.NodeInfo{"sn-a": {}, "sn-b": {}},
			wantScores: map[string]float64{
				"sn-a": 1.0 * float64(fwk.MaxNodeScore),
				"sn-b": 1.0 * float64(fwk.MaxNodeScore),
			},
		},
		{
			name:    "invalid preferred term tierName returns error",
			selfJob: jobWithTopologyAffinity(nil, []scheduling.PodGroupAffinityTerm{{PodGroupSelector: prodSelector, TopologyTierName: "missing"}}),
			candidates: map[string][]*api.NodeInfo{
				"sn-a": {},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hn := buildTwoSupernodeTree()
			ssn := &framework.Session{
				Jobs:                 tt.jobs,
				HyperNodes:           hn,
				HyperNodeTierNameMap: defaultTierNameMap(),
				RealNodesSet:         defaultRealNodesSet(),
			}
			plugin := New(framework.Arguments{PluginWeight: tt.weight}).(*groupTopologyAffinityPlugin)

			scores, err := plugin.hyperNodeOrderFn(ssn, tt.selfJob, tt.candidates)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if scores != nil {
					t.Fatalf("expected nil scores, got %#v", scores)
				}
				return
			}
			for hyperNode, want := range tt.wantScores {
				got, ok := scores[hyperNode]
				if !ok {
					t.Fatalf("missing score for %s", hyperNode)
				}
				if got != want {
					t.Fatalf("score for %s: want %v, got %v", hyperNode, want, got)
				}
			}
		})
	}
}

func TestGetSearchRootForGradient(t *testing.T) {
	hn := buildRackUnderSupernodeTree()
	hn["sn-x"] = newTestHyperNode("sn-x", 2, "supernode", "")

	tests := []struct {
		name               string
		hn                 api.HyperNodeInfoMap
		available          string
		allocatedHyperNode string
		highestAllowedTier int
		wantRoot           string
		wantErr            bool
	}{
		{
			name:               "first allocation uses available root",
			hn:                 buildTwoSupernodeTree(),
			available:          "root",
			allocatedHyperNode: "",
			highestAllowedTier: 3,
			wantRoot:           "root",
		},
		{
			name:               "follow-up narrows to allocated envelope inside available subtree",
			hn:                 hn,
			available:          "sn-b",
			allocatedHyperNode: "cab-b",
			highestAllowedTier: 2,
			wantRoot:           "sn-b",
		},
		{
			name:               "shared root keeps available root when envelopes overlap",
			hn:                 buildTwoSupernodeTree(),
			available:          "root",
			allocatedHyperNode: "sn-b",
			highestAllowedTier: 3,
			wantRoot:           "root",
		},
		{
			name:               "disjoint allocated hyperNode has no intersection",
			hn:                 hn,
			available:          "sn-a",
			allocatedHyperNode: "sn-x",
			highestAllowedTier: 3,
			wantErr:            true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := getSearchRootForGradient(tt.hn, tt.hn[tt.available], tt.highestAllowedTier, tt.allocatedHyperNode)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if root.Name != tt.wantRoot {
				t.Fatalf("want root %s, got %s", tt.wantRoot, root.Name)
			}
		})
	}
}

func TestGroupHyperNodesByTierAsc(t *testing.T) {
	hn := buildRackUnderSupernodeTree()
	eligible := map[int][]*api.HyperNodeInfo{
		2: {hn["sn-b"]},
		1: {hn["cab-a"], hn["cab-b"]},
	}
	gradients := groupHyperNodesByTierAsc(eligible)
	if len(gradients) != 2 {
		t.Fatalf("expected 2 tier groups, got %d", len(gradients))
	}
	if tiersFromGradients(gradients)[0] != 1 || tiersFromGradients(gradients)[1] != 2 {
		t.Fatalf("expected tiers [1, 2], got %v", tiersFromGradients(gradients))
	}
}

func buildThreeSupernodeTree() api.HyperNodeInfoMap {
	hn := api.HyperNodeInfoMap{
		"root": newTestHyperNode("root", 3, "cluster", ""),
		"sn-a": newTestHyperNode("sn-a", 2, "supernode", "root"),
		"sn-b": newTestHyperNode("sn-b", 2, "supernode", "root"),
		"sn-c": newTestHyperNode("sn-c", 2, "supernode", "root"),
	}
	hn["root"].Children = sets.New("sn-a", "sn-b", "sn-c")
	return hn
}

func threeSupernodeSetByTier() map[int]sets.Set[string] {
	return map[int]sets.Set[string]{
		2: sets.New("sn-a", "sn-b", "sn-c"),
		3: sets.New("root"),
	}
}

func buildTwoSupernodeTree() api.HyperNodeInfoMap {
	hn := api.HyperNodeInfoMap{
		"root": newTestHyperNode("root", 3, "cluster", ""),
		"sn-a": newTestHyperNode("sn-a", 2, "supernode", "root"),
		"sn-b": newTestHyperNode("sn-b", 2, "supernode", "root"),
	}
	hn["root"].Children = sets.New("sn-a", "sn-b")
	return hn
}

func buildRackUnderSupernodeTree() api.HyperNodeInfoMap {
	hn := buildTwoSupernodeTree()
	hn["cab-a"] = newTestHyperNode("cab-a", 1, "rack", "sn-a")
	hn["cab-b"] = newTestHyperNode("cab-b", 1, "rack", "sn-b")
	hn["sn-a"].Children = sets.New("cab-a")
	hn["sn-b"].Children = sets.New("cab-b")
	return hn
}

func twoSupernodeSetByTier() map[int]sets.Set[string] {
	return map[int]sets.Set[string]{
		2: sets.New("sn-a", "sn-b"),
		3: sets.New("root"),
	}
}

func rackSupernodeSetByTier() map[int]sets.Set[string] {
	return map[int]sets.Set[string]{
		1: sets.New("cab-a", "cab-b"),
		2: sets.New("sn-a", "sn-b"),
		3: sets.New("root"),
	}
}

func defaultTierNameMap() api.HyperNodeTierNameMap {
	return api.HyperNodeTierNameMap{
		"cluster":   3,
		"supernode": 2,
		"rack":      1,
	}
}

func otherJobOn(uid, hyperNode, group string) *api.JobInfo {
	return &api.JobInfo{
		UID:                api.JobID(uid),
		Namespace:          "default",
		AllocatedHyperNode: hyperNode,
		PodGroup: &api.PodGroup{
			PodGroup: scheduling.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{testGroupLabel: group}},
			},
		},
	}
}

func otherJobWithTaskOnNode(uid, nodeName, group string) *api.JobInfo {
	return otherJobWithTasksOnNodes(uid, group, nodeName)
}

func otherJobWithTasksOnNodes(uid, group string, nodeNames ...string) *api.JobInfo {
	tasks := make(api.TasksMap, len(nodeNames))
	for i, nodeName := range nodeNames {
		task := &api.TaskInfo{UID: api.TaskID(fmt.Sprintf("%s-task-%d", uid, i))}
		task.Status = api.Allocated
		task.NodeName = nodeName
		tasks[task.UID] = task
	}
	subJobID := api.SubJobID("default")
	return &api.JobInfo{
		UID:       api.JobID(uid),
		Namespace: "default",
		PodGroup: &api.PodGroup{
			PodGroup: scheduling.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{testGroupLabel: group}},
			},
		},
		SubJobs: map[api.SubJobID]*api.SubJobInfo{
			subJobID: {
				UID: subJobID,
				TaskStatusIndex: map[api.TaskStatus]api.TasksMap{
					api.Allocated: tasks,
				},
			},
		},
	}
}

func defaultRealNodesSet() map[string]sets.Set[string] {
	return map[string]sets.Set[string]{
		"sn-a":  sets.New("node-a"),
		"sn-b":  sets.New("node-b"),
		"sn-c":  sets.New("node-c"),
		"cab-a": sets.New("node-cab-a"),
		"cab-b": sets.New("node-cab-b"),
	}
}

func jobWithTopologyAffinity(required, preferred []scheduling.PodGroupAffinityTerm) *api.JobInfo {
	spec := &scheduling.TopologyAffinitySpec{}
	if len(required) > 0 {
		spec.PodGroupAntiAffinity = &scheduling.PodGroupAntiAffinity{Required: required}
	}
	if len(preferred) > 0 {
		if spec.PodGroupAntiAffinity == nil {
			spec.PodGroupAntiAffinity = &scheduling.PodGroupAntiAffinity{}
		}
		spec.PodGroupAntiAffinity.Preferred = preferred
	}
	return &api.JobInfo{
		UID:       "self",
		Namespace: "default",
		PodGroup: &api.PodGroup{
			PodGroup: scheduling.PodGroup{Spec: scheduling.PodGroupSpec{TopologyAffinity: spec}},
		},
	}
}

func jobWithAllocatedHyperNode(uid, hyperNode string, term scheduling.PodGroupAffinityTerm) *api.JobInfo {
	if term.PodGroupSelector == nil {
		term.PodGroupSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{testGroupLabel: "prod"},
		}
	}
	job := jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{term}, nil)
	job.UID = api.JobID(uid)
	job.AllocatedHyperNode = hyperNode
	return job
}

func hyperNodeNamesFromGradients(gradients [][]*api.HyperNodeInfo) []string {
	var names []string
	for _, tierGroup := range gradients {
		for _, hn := range tierGroup {
			names = append(names, hn.Name)
		}
	}
	return names
}

func tiersFromGradients(gradients [][]*api.HyperNodeInfo) []int {
	tiers := make([]int, 0, len(gradients))
	for _, tierGroup := range gradients {
		if len(tierGroup) == 0 {
			continue
		}
		tiers = append(tiers, tierGroup[0].Tier())
	}
	return tiers
}

func newTestHyperNode(name string, tier int, tierName, parent string) *api.HyperNodeInfo {
	return api.NewHyperNodeInfo(
		api.BuildHyperNodeWithTierName(name, tier, tierName, nil),
		api.ParentOpt(parent),
	)
}
