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

package grouptopologyaffinity

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	networktopologyaware "volcano.sh/volcano/pkg/scheduler/plugins/network-topology-aware"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestPlacementTrackingSelectsPeers(t *testing.T) {
	for _, tc := range []struct {
		name      string
		required  bool
		preferred bool
		gradient  bool
		order     bool
		ready     bool
		allPeers  bool
		invalid   bool
		want      bool
	}{
		{name: "required", required: true, gradient: true, ready: true, want: true},
		{name: "preferred", preferred: true, order: true, ready: true, want: true},
		{name: "both", required: true, preferred: true, gradient: true, order: true, ready: true, want: true},
		{name: "disabled", required: true, preferred: true, ready: true},
		{name: "required hook disabled", required: true, order: true, ready: true},
		{name: "preferred hook disabled", preferred: true, gradient: true, ready: true},
		{name: "topology not ready", required: true, gradient: true},
		{name: "no rules", gradient: true, order: true, ready: true},
		{name: "all local peers", required: true, gradient: true, ready: true, allPeers: true, want: true},
		{name: "invalid tier", required: true, gradient: true, ready: true, invalid: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term := scheduling.PodGroupAffinityTerm{TopologyTierName: "supernode", Weight: 50,
				PodGroupSelector: &metav1.LabelSelector{MatchLabels: map[string]string{testGroupLabel: "prod"}}}
			if tc.allPeers {
				term.PodGroupSelector = &metav1.LabelSelector{}
			}
			if tc.invalid {
				term.TopologyTierName = "missing"
			}
			var required, preferred []scheduling.PodGroupAffinityTerm
			if tc.required {
				required = []scheduling.PodGroupAffinityTerm{term}
			}
			if tc.preferred {
				preferred = []scheduling.PodGroupAffinityTerm{term}
			}
			job := jobWithTopologyAffinity(required, preferred)
			peer := otherJobWithTaskOnNode("peer", "node-a", "prod")
			unrelated := otherJobWithTaskOnNode("unrelated", "node-b", "staging")
			foreign := otherJobWithTaskOnNode("foreign", "node-b", "prod")
			foreign.Namespace = "other"
			ssn := &framework.Session{
				DirtyJobs:  sets.New[api.JobID](),
				Jobs:       map[api.JobID]*api.JobInfo{job.UID: job, peer.UID: peer, unrelated.UID: unrelated, foreign.UID: foreign},
				HyperNodes: buildTwoSupernodeTree(), RealNodesSet: defaultRealNodesSet(), HyperNodeTierNameMap: defaultTierNameMap(),
				HyperNodeGeneration: 1, HyperNodesReadyToSchedule: tc.ready,
				Tiers: []conf.Tier{{Plugins: []conf.PluginOption{{Name: PluginName,
					EnabledHyperNodeGradient: ptr.To(tc.gradient), EnabledHyperNodeOrder: ptr.To(tc.order)}}}},
			}
			plugin := New(framework.Arguments{}).(*groupTopologyAffinityPlugin)
			plugin.enablePlacementTracking(ssn)
			defer plugin.OnSessionClose(ssn)
			require.Equal(t, tc.want, ssn.PodGroupPlacementEnabled(job.UID))
			require.Equal(t, tc.want && !tc.invalid, ssn.PodGroupPlacementEnabled(peer.UID))
			require.Equal(t, tc.want && tc.allPeers, ssn.PodGroupPlacementEnabled(unrelated.UID))
			require.False(t, ssn.PodGroupPlacementEnabled(foreign.UID))
			require.False(t, ssn.DirtyJobs.Has(foreign.UID))
			if tc.want && !tc.invalid {
				require.Equal(t, "sn-a", peer.AllocatedHyperNode)
			}
			if !tc.allPeers {
				require.Empty(t, unrelated.AllocatedHyperNode)
			}
			if tc.want && tc.required {
				compiled := plugin.compiled[constraintKey{job: job}]
				plugin.hyperNodeGradientForJob(ssn, job, ssn.HyperNodes["root"])
				plugin.hyperNodeGradientForSubJob(ssn, job, &api.SubJobInfo{Job: job.UID}, ssn.HyperNodes["root"])
				if tc.invalid {
					require.Error(t, compiled.err)
				} else {
					require.Same(t, &compiled.terms[0], &plugin.compiled[constraintKey{job: job}].terms[0])
				}
			}
		})
	}
}

func TestPlacementTrackingRebuiltEachSession(t *testing.T) {
	sc := cache.NewCustomMockSchedulerCache("tracking-test", util.NewFakeBinder(0), util.NewFakeEvictor(0), &util.FakeStatusUpdater{}, nil, nil)
	t.Cleanup(func() { cache.ShutdownMockSchedulerCache(sc) })
	plugin := New(framework.Arguments{}).(*groupTopologyAffinityPlugin)
	term := scheduling.PodGroupAffinityTerm{TopologyTierName: "supernode",
		PodGroupSelector:  &metav1.LabelSelector{MatchLabels: map[string]string{testGroupLabel: "prod"}},
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "selected"}},
	}
	peer := otherJobWithTaskOnNode("peer", "node-a", "prod")
	peer.Namespace = "other"
	for _, tc := range []struct {
		name, group, team   string
		policy, moved, want bool
	}{
		{name: "initial", group: "prod", team: "selected", policy: true, want: true},
		{name: "PodGroup label removed", group: "staging", team: "selected", policy: true},
		{name: "PodGroup label restored", group: "prod", team: "selected", policy: true, want: true},
		{name: "Namespace label removed", group: "prod", team: "other", policy: true},
		{name: "Namespace label restored", group: "prod", team: "selected", policy: true, want: true},
		{name: "policy removed", group: "prod", team: "selected"},
		{name: "policy restored after topology change", group: "prod", team: "selected", policy: true, moved: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ssn := framework.OpenSession(sc, nil, nil)
			defer framework.CloseSession(ssn)
			defer plugin.OnSessionClose(ssn)
			for _, ns := range []*v1.Namespace{
				{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "other", Labels: map[string]string{"team": tc.team}}},
			} {
				require.NoError(t, ssn.InformerFactory().Core().V1().Namespaces().Informer().GetIndexer().Update(ns))
			}
			job := jobWithTopologyAffinity(nil, nil)
			if tc.policy {
				job = jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{term}, nil)
			}
			peer.PodGroup.Labels[testGroupLabel] = tc.group
			ssn.Jobs = map[api.JobID]*api.JobInfo{job.UID: job, peer.UID: peer}
			ssn.HyperNodes = buildTwoSupernodeTree()
			ssn.RealNodesSet = defaultRealNodesSet()
			ssn.HyperNodeTierNameMap = defaultTierNameMap()
			ssn.HyperNodeGeneration = 1
			ssn.HyperNodesReadyToSchedule = true
			if tc.moved {
				ssn.HyperNodeGeneration++
				ssn.RealNodesSet["sn-a"] = sets.New("node-b")
				ssn.RealNodesSet["sn-b"] = sets.New("node-a")
			}
			ssn.Tiers = []conf.Tier{{Plugins: []conf.PluginOption{{Name: PluginName, EnabledHyperNodeGradient: ptr.To(true)}}}}
			plugin.OnSessionOpen(ssn)
			require.Equal(t, tc.want, ssn.PodGroupPlacementEnabled(peer.UID))
			if !tc.want {
				require.False(t, ssn.DirtyJobs.Has(peer.UID))
			} else if tc.moved {
				require.Equal(t, "sn-b", peer.AllocatedHyperNode)
			} else {
				require.Equal(t, "sn-a", peer.AllocatedHyperNode)
			}
		})
	}
}

func TestCompiledTermsObserveLivePlacement(t *testing.T) {
	term := scheduling.PodGroupAffinityTerm{TopologyTierName: "supernode", PodGroupSelector: &metav1.LabelSelector{}, Weight: 50}
	job := jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{term, term}, []scheduling.PodGroupAffinityTerm{term})
	peer := otherJobWithTaskOnNode("peer", "node-a", "prod")
	ssn := &framework.Session{Jobs: map[api.JobID]*api.JobInfo{job.UID: job, peer.UID: peer},
		HyperNodes: buildTwoSupernodeTree(), RealNodesSet: defaultRealNodesSet(), HyperNodeTierNameMap: defaultTierNameMap()}
	plugin := New(framework.Arguments{}).(*groupTopologyAffinityPlugin)
	evaluate := func(want string) {
		t.Helper()
		constraints, err := plugin.constraintsFor(ssn, job, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, constraint := range constraints {
			if !constraint.occupied.Equal(sets.New(want)) {
				t.Fatalf("stale occupancy: %v, want %s", constraint.occupied, want)
			}
		}
	}
	evaluate("sn-a")
	compiled := plugin.compiled[constraintKey{job: job}]
	if len(compiled.terms) != 2 || compiled.terms[0].peers[0] != peer {
		t.Fatal("peer selection not compiled")
	}
	task := peer.SubJobs["default"].TaskStatusIndex[api.Allocated]["peer-task-0"]
	task.NodeName = "node-b"
	evaluate("sn-b")
	task.NodeName = "node-a"
	evaluate("sn-a")
	if &compiled.terms[0] != &plugin.compiled[constraintKey{job: job}].terms[0] {
		t.Fatal("terms were recompiled during the same Session")
	}
	// Preferred evaluation must see the same live placement, independently of
	// the cached required-policy compilation.
	scores, err := plugin.hyperNodeOrderFn(ssn, job, map[string][]*api.NodeInfo{"sn-a": nil, "sn-b": nil})
	if err != nil || scores["sn-a"] >= scores["sn-b"] {
		t.Fatalf("unexpected preferred scores %v, %v", scores, err)
	}
	plugin.OnSessionClose(ssn)
	if plugin.compiled != nil || plugin.constraintSession != nil {
		t.Fatal("Session compilation retained after close")
	}
}

func TestCompiledNamespaceSelectors(t *testing.T) {
	schedulerCache := cache.NewCustomMockSchedulerCache("selector-test", util.NewFakeBinder(0), util.NewFakeEvictor(0), &util.FakeStatusUpdater{}, nil, nil)
	ssn := framework.OpenSession(schedulerCache, nil, nil)
	t.Cleanup(func() { cache.ShutdownMockSchedulerCache(schedulerCache) })
	defer framework.CloseSession(ssn)
	for _, ns := range []*v1.Namespace{
		{ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"team": "a"}}},
		{ObjectMeta: metav1.ObjectMeta{Name: "other", Labels: map[string]string{"team": "b"}}},
	} {
		if err := ssn.InformerFactory().Core().V1().Namespaces().Informer().GetIndexer().Add(ns); err != nil {
			t.Fatal(err)
		}
	}
	job := jobWithTopologyAffinity(nil, nil)
	local := otherJobOn("local", "sn-a", "prod")
	remote := otherJobOn("remote", "sn-b", "prod")
	remote.Namespace = "other"
	ssn.Jobs = map[api.JobID]*api.JobInfo{job.UID: job, local.UID: local, remote.UID: remote}
	ssn.HyperNodeTierNameMap = defaultTierNameMap()
	for _, tt := range []struct {
		name     string
		selector *metav1.LabelSelector
		want     sets.Set[api.JobID]
	}{
		{name: "omitted", want: sets.New(local.UID)},
		{name: "all namespaces", selector: &metav1.LabelSelector{}, want: sets.New(local.UID, remote.UID)},
		{name: "namespace labels", selector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "b"}}, want: sets.New(remote.UID)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			terms, err := compileTerms(ssn, job, []scheduling.PodGroupAffinityTerm{{TopologyTierName: "supernode", PodGroupSelector: &metav1.LabelSelector{}, NamespaceSelector: tt.selector}})
			if err != nil {
				t.Fatal(err)
			}
			got := sets.New[api.JobID]()
			for _, peer := range terms[0].peers {
				got.Insert(peer.UID)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("got peers %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompiledPodGroupSelectors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selector *metav1.LabelSelector
		want     int
	}{
		{name: "nil selector"},
		{name: "empty selector", selector: &metav1.LabelSelector{}, want: 1},
		{name: "matching labels", selector: &metav1.LabelSelector{MatchLabels: map[string]string{testGroupLabel: "prod"}}, want: 1},
		{name: "different labels", selector: &metav1.LabelSelector{MatchLabels: map[string]string{testGroupLabel: "staging"}}},
		{name: "matching expression", selector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{Key: testGroupLabel, Operator: metav1.LabelSelectorOpIn, Values: []string{"prod"}}}}, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			job := otherJobOn("self", "sn-a", "prod")
			peer := otherJobOn("peer", "sn-b", "prod")
			foreign := otherJobOn("foreign", "sn-b", "prod")
			foreign.Namespace = "other"
			ssn := &framework.Session{Jobs: map[api.JobID]*api.JobInfo{job.UID: job, peer.UID: peer, foreign.UID: foreign, "nil": nil, "no-podgroup": {UID: "no-podgroup"}}, HyperNodeTierNameMap: defaultTierNameMap()}
			terms, err := compileTerms(ssn, job, []scheduling.PodGroupAffinityTerm{{TopologyTierName: "supernode", PodGroupSelector: tc.selector}})
			require.NoError(t, err)
			require.Len(t, terms[0].peers, tc.want)
			if tc.want > 0 {
				require.Same(t, peer, terms[0].peers[0])
			}
		})
	}
}

func TestNominationFastPathMatchesPluginGradients(t *testing.T) {
	for _, networkEnabled := range []bool{false, true} {
		for _, hardJob := range []bool{false, true} {
			for _, hardSubJob := range []bool{false, true} {
				for _, placed := range []bool{false, true} {
					t.Run(fmt.Sprintf("network=%t/job=%t/subJob=%t/placed=%t", networkEnabled, hardJob, hardSubJob, placed), func(t *testing.T) {
						sc := cache.NewCustomMockSchedulerCache("nomination-test", util.NewFakeBinder(0), util.NewFakeEvictor(0), &util.FakeStatusUpdater{}, nil, nil)
						ssn := framework.OpenSession(sc, nil, nil)
						defer framework.CloseSession(ssn)
						ssn.HyperNodes = buildRackUnderSupernodeTree()
						ssn.HyperNodesSetByTier = rackSupernodeSetByTier()
						ssn.HyperNodesTiers = []int{1, 2, 3}
						ssn.HyperNodeTierNameMap = defaultTierNameMap()
						ssn.RealNodesSet = map[string]sets.Set[string]{
							"root": sets.New("node-a", "node-b"), "sn-a": sets.New("node-a"), "sn-b": sets.New("node-b"),
							"cab-a": sets.New("node-a"), "cab-b": sets.New("node-b"),
						}
						for _, name := range []string{"node-a", "node-b"} {
							ssn.Nodes[name] = api.NewNodeInfo(util.BuildNode(name, api.BuildResourceList("4", "8Gi"), nil))
						}
						job := jobWithTopologyAffinity([]scheduling.PodGroupAffinityTerm{{TopologyTierName: "supernode", PodGroupSelector: &metav1.LabelSelector{}}}, nil)
						subJob := &api.SubJobInfo{Job: job.UID, UID: "sub"}
						if hardJob {
							job.NetworkTopology = &scheduling.NetworkTopologySpec{Mode: scheduling.HardNetworkTopologyMode, HighestTierAllowed: ptr.To(2)}
						}
						if hardSubJob {
							subJob.NetworkTopology = &scheduling.NetworkTopologySpec{Mode: scheduling.HardNetworkTopologyMode, HighestTierAllowed: ptr.To(1)}
						}
						if placed {
							job.AllocatedHyperNode = "sn-b"
							subJob.AllocatedHyperNode = "cab-b"
						}
						ssn.Jobs = map[api.JobID]*api.JobInfo{job.UID: job, "peer": otherJobWithTaskOnNode("peer", "node-a", "prod")}
						ssn.Tiers = []conf.Tier{{Plugins: []conf.PluginOption{
							{Name: PluginName, EnabledHyperNodeGradient: ptr.To(true)},
							{Name: networktopologyaware.PluginName, EnabledHyperNodeGradient: ptr.To(networkEnabled)},
						}}}
						plugin := New(framework.Arguments{})
						plugin.OnSessionOpen(ssn)
						if networkEnabled {
							networktopologyaware.New(framework.Arguments{}).OnSessionOpen(ssn)
						}
						for _, sub := range []*api.SubJobInfo{nil, subJob} {
							for _, rootName := range []string{"root", "sn-a", "sn-b"} {
								root := ssn.HyperNodes[rootName]
								var gradients [][]*api.HyperNodeInfo
								if sub == nil {
									gradients, _ = ssn.HyperNodeGradientForJobFn(job, root, api.PurposeAllocate)
								} else {
									gradients, _ = ssn.HyperNodeGradientForSubJobFn(sub, root, api.PurposeAllocate)
								}
								for name, nodes := range ssn.RealNodesSet {
									want := false
									for _, layer := range gradients {
										for _, candidate := range layer {
											if ssn.RealNodesSet[candidate.Name].IsSuperset(nodes) {
												want = true
											}
										}
									}
									if got := ssn.HyperNodeContainsNomination(job, sub, root, name); got != want {
										t.Fatalf("root %s pinned %s sub=%t: fast=%t full=%t", rootName, name, sub != nil, got, want)
									}
								}
							}
						}
						plugin.OnSessionClose(ssn)
					})
				}
			}
		}
	}
}
