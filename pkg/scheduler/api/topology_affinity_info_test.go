/*
Copyright 2017 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	"volcano.sh/apis/pkg/apis/scheduling"
)

func TestPodGroupMatchesTermWithNamespaceLister(t *testing.T) {
	indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if err := indexer.Add(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: "selected", Labels: map[string]string{"environment": "production"},
	}}); err != nil {
		t.Fatal(err)
	}
	lister := corelisters.NewNamespaceLister(indexer)
	self := NewJobInfo("self")
	self.Namespace = "default"
	self.PodGroup = &PodGroup{PodGroup: scheduling.PodGroup{ObjectMeta: metav1.ObjectMeta{Name: "self", Namespace: "default"}}}
	other := NewJobInfo("other")
	other.Namespace = "selected"
	other.PodGroup = &PodGroup{PodGroup: scheduling.PodGroup{ObjectMeta: metav1.ObjectMeta{
		Name: "other", Namespace: "selected", Labels: map[string]string{"team": "ml"},
	}}}
	term := scheduling.PodGroupAffinityTerm{
		PodGroupSelector:  &metav1.LabelSelector{MatchLabels: map[string]string{"team": "ml"}},
		NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"environment": "production"}},
	}
	matched, err := PodGroupMatchesTermWithNamespaceLister(term, self, other, lister)
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Fatal("expected cross-namespace PodGroup to match namespace labels")
	}
}

func TestResolvePodGroupTermTier(t *testing.T) {
	tierNameMap := HyperNodeTierNameMap{"supernode": 2}
	tier2 := int32(2)

	tests := []struct {
		name    string
		term    scheduling.PodGroupAffinityTerm
		want    int
		wantErr string
	}{
		{
			name: "topologyTierName",
			term: scheduling.PodGroupAffinityTerm{TopologyTierName: "supernode"},
			want: 2,
		},
		{
			name: "topologyTier",
			term: scheduling.PodGroupAffinityTerm{TopologyTier: &tier2},
			want: 2,
		},
		{
			name:    "mutually exclusive fields",
			term:    scheduling.PodGroupAffinityTerm{TopologyTierName: "supernode", TopologyTier: &tier2},
			wantErr: "mutually exclusive",
		},
		{
			name:    "missing both tier fields",
			term:    scheduling.PodGroupAffinityTerm{},
			wantErr: "must be set",
		},
		{
			name:    "unknown topologyTierName",
			term:    scheduling.PodGroupAffinityTerm{TopologyTierName: "missing"},
			wantErr: "unknown topologyTierName",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolvePodGroupTermTier(tt.term, tierNameMap)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("want tier %d, got %d", tt.want, got)
			}
		})
	}
}

func TestPodGroupMatchesTerm(t *testing.T) {
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"topology.volcano.sh/group": "prod"},
	}
	selfJob := &JobInfo{UID: "self", Namespace: "default", PodGroup: &PodGroup{}}
	otherJob := &JobInfo{
		UID:       "other",
		Namespace: "default",
		PodGroup: &PodGroup{
			PodGroup: scheduling.PodGroup{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
			},
		},
	}

	tests := []struct {
		name  string
		term  scheduling.PodGroupAffinityTerm
		self  *JobInfo
		other *JobInfo
		want  bool
	}{
		{
			name: "matching label in same namespace",
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector},
			self: selfJob, other: otherJob, want: true,
		},
		{
			name: "self job is excluded",
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector},
			self: otherJob, other: otherJob, want: false,
		},
		{
			name: "label mismatch",
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector},
			self: selfJob,
			other: &JobInfo{
				UID: "other", Namespace: "default",
				PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "staging"}},
				}},
			},
			want: false,
		},
		{
			name: "nil podGroupSelector",
			term: scheduling.PodGroupAffinityTerm{},
			self: selfJob, other: otherJob, want: false,
		},
		{
			name: "different namespace without selector",
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector},
			self: selfJob,
			other: &JobInfo{
				UID: "other", Namespace: "other-ns",
				PodGroup: otherJob.PodGroup,
			},
			want: false,
		},
		{
			name: "nil other job",
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector},
			self: selfJob, other: nil, want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PodGroupMatchesTerm(tt.term, tt.self, tt.other); got != tt.want {
				t.Fatalf("PodGroupMatchesTerm() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMatchingPodGroupsAllocatedHyperNodesForTerm(t *testing.T) {
	tierNameMap := HyperNodeTierNameMap{"supernode": 2, "rack": 1}
	hn := HyperNodeInfoMap{
		"root":  newTestHyperNode("root", 3, "cluster", ""),
		"sn-a":  newTestHyperNode("sn-a", 2, "supernode", "root"),
		"sn-b":  newTestHyperNode("sn-b", 2, "supernode", "root"),
		"cab-a": newTestHyperNode("cab-a", 1, "rack", "sn-a"),
	}
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"topology.volcano.sh/group": "prod"},
	}
	selfJob := &JobInfo{UID: "self", Namespace: "default", PodGroup: &PodGroup{}}

	tests := []struct {
		name    string
		jobs    map[JobID]*JobInfo
		term    scheduling.PodGroupAffinityTerm
		want    sets.Set[string]
		wantErr bool
	}{
		{
			name: "single matching PodGroup at supernode tier",
			jobs: map[JobID]*JobInfo{
				"other": {
					UID: "other", Namespace: "default", AllocatedHyperNode: "sn-a",
					PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "supernode"},
			want: sets.New("sn-a"),
		},
		{
			name: "rack tier resolves ancestor hyperNode",
			jobs: map[JobID]*JobInfo{
				"other": {
					UID: "other", Namespace: "default", AllocatedHyperNode: "cab-a",
					PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "rack"},
			want: sets.New("cab-a"),
		},
		{
			name: "coarser allocated hyperNode expands to descendants at finer term tier",
			jobs: map[JobID]*JobInfo{
				"other": {
					UID: "other", Namespace: "default", AllocatedHyperNode: "sn-a",
					PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "rack"},
			want: sets.New("cab-a"),
		},
		{
			name: "multiple matching PodGroups collapse to set",
			jobs: map[JobID]*JobInfo{
				"j1": {
					UID: "j1", Namespace: "default", AllocatedHyperNode: "sn-a",
					PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
				"j2": {
					UID: "j2", Namespace: "default", AllocatedHyperNode: "sn-b",
					PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "supernode"},
			want: sets.New("sn-a", "sn-b"),
		},
		{
			name: "self job in map is ignored",
			jobs: map[JobID]*JobInfo{
				"self": {
					UID: "self", Namespace: "default", AllocatedHyperNode: "sn-a",
					PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "supernode"},
			want: sets.New[string](),
		},
		{
			name: "matching PodGroup without AllocatedHyperNode is skipped without node mapping",
			jobs: map[JobID]*JobInfo{
				"other": {
					UID: "other", Namespace: "default",
					PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "supernode"},
			want: sets.New[string](),
		},
		{
			name: "matching PodGroup without AllocatedHyperNode is inferred from allocated tasks",
			jobs: map[JobID]*JobInfo{
				"other": otherJobWithTaskOnNode("other", "node-a", "prod"),
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "supernode"},
			want: sets.New("sn-a"),
		},
		{
			name:    "invalid term returns error",
			jobs:    map[JobID]*JobInfo{},
			term:    scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "missing"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodesByHyperNode := map[string]sets.Set[string]{
				"sn-a":  sets.New("node-a"),
				"sn-b":  sets.New("node-b"),
				"cab-a": sets.New("node-cab-a"),
			}
			got, err := MatchingPodGroupsAllocatedHyperNodesForTerm(tt.jobs, hn, tierNameMap, selfJob, tt.term, nodesByHyperNode)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("want %v, got %v", tt.want.UnsortedList(), got.UnsortedList())
			}
		})
	}
}

func TestRequiresHyperNodeAllocate(t *testing.T) {
	required := []scheduling.PodGroupAffinityTerm{{TopologyTierName: "supernode"}}
	preferred := []scheduling.PodGroupAffinityTerm{{TopologyTierName: "rack", Weight: 1}}

	jobWithAntiAffinity := func(anti *scheduling.PodGroupAntiAffinity) *JobInfo {
		return &JobInfo{
			PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{Spec: scheduling.PodGroupSpec{
				TopologyAffinity: &scheduling.TopologyAffinitySpec{PodGroupAntiAffinity: anti},
			}}},
		}
	}

	tests := []struct {
		name string
		job  *JobInfo
		want bool
	}{
		{
			name: "soft (preferred) anti-affinity requires hyperNode path",
			job:  jobWithAntiAffinity(&scheduling.PodGroupAntiAffinity{Preferred: preferred}),
			want: true,
		},
		{
			name: "hard (required) anti-affinity requires hyperNode path",
			job:  jobWithAntiAffinity(&scheduling.PodGroupAntiAffinity{Required: required}),
			want: true,
		},
		{
			name: "both hard and soft anti-affinity",
			job:  jobWithAntiAffinity(&scheduling.PodGroupAntiAffinity{Required: required, Preferred: preferred}),
			want: true,
		},
		{
			name: "plain job without topology or anti-affinity does not require hyperNode path",
			job:  &JobInfo{PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{Spec: scheduling.PodGroupSpec{}}}},
			want: false,
		},
		{
			name: "nil podgroup does not require hyperNode path",
			job:  &JobInfo{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.job.RequiresHyperNodeAllocate(); got != tt.want {
				t.Fatalf("RequiresHyperNodeAllocate = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobTopologyAffinityHelpers(t *testing.T) {
	required := []scheduling.PodGroupAffinityTerm{{TopologyTierName: "supernode"}}
	preferred := []scheduling.PodGroupAffinityTerm{{TopologyTierName: "rack", Weight: 1}}

	withRequired := &JobInfo{
		PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{Spec: scheduling.PodGroupSpec{
			TopologyAffinity: &scheduling.TopologyAffinitySpec{
				PodGroupAntiAffinity: &scheduling.PodGroupAntiAffinity{Required: required},
			},
		}}},
	}
	withPreferred := &JobInfo{
		PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{Spec: scheduling.PodGroupSpec{
			TopologyAffinity: &scheduling.TopologyAffinitySpec{
				PodGroupAntiAffinity: &scheduling.PodGroupAntiAffinity{Preferred: preferred},
			},
		}}},
	}
	withBoth := &JobInfo{
		PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{Spec: scheduling.PodGroupSpec{
			TopologyAffinity: &scheduling.TopologyAffinitySpec{
				PodGroupAntiAffinity: &scheduling.PodGroupAntiAffinity{
					Required: required, Preferred: preferred,
				},
			},
		}}},
	}

	tests := []struct {
		name     string
		job      *JobInfo
		hard     bool
		soft     bool
		withTopo bool
		reqLen   int
		prefLen  int
	}{
		{name: "required only", job: withRequired, hard: true, withTopo: true, reqLen: 1},
		{name: "preferred only", job: withPreferred, soft: true, withTopo: true, prefLen: 1},
		{name: "both", job: withBoth, hard: true, soft: true, withTopo: true, reqLen: 1, prefLen: 1},
		{name: "nil podgroup", job: &JobInfo{}, hard: false, soft: false, withTopo: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.job.ContainsHardPodGroupAntiAffinity(); got != tt.hard {
				t.Fatalf("ContainsHardPodGroupAntiAffinity = %v, want %v", got, tt.hard)
			}
			if got := tt.job.HasPreferredPodGroupAntiAffinity(); got != tt.soft {
				t.Fatalf("HasPreferredPodGroupAntiAffinity = %v, want %v", got, tt.soft)
			}
			if got := tt.job.WithTopologyAffinity(); got != tt.withTopo {
				t.Fatalf("WithTopologyAffinity = %v, want %v", got, tt.withTopo)
			}
			if len(tt.job.RequiredPodGroupAntiAffinityTerms()) != tt.reqLen {
				t.Fatalf("RequiredPodGroupAntiAffinityTerms len = %d, want %d", len(tt.job.RequiredPodGroupAntiAffinityTerms()), tt.reqLen)
			}
			if len(tt.job.PreferredPodGroupAntiAffinityTerms()) != tt.prefLen {
				t.Fatalf("PreferredPodGroupAntiAffinityTerms len = %d, want %d", len(tt.job.PreferredPodGroupAntiAffinityTerms()), tt.prefLen)
			}
		})
	}
}

func TestGetJobAllocatedHyperNode(t *testing.T) {
	hn := HyperNodeInfoMap{
		"root": newTestHyperNode("root", 3, "cluster", ""),
		"sn-a": newTestHyperNode("sn-a", 2, "supernode", "root"),
		"sn-b": newTestHyperNode("sn-b", 2, "supernode", "root"),
	}
	nodesByHyperNode := map[string]sets.Set[string]{
		"sn-a": sets.New("node-a"),
		"sn-b": sets.New("node-b"),
	}

	tests := []struct {
		name string
		job  *JobInfo
		want string
	}{
		{
			name: "uses AllocatedHyperNode when already recorded",
			job:  &JobInfo{UID: "job-0", AllocatedHyperNode: "sn-a"},
			want: "sn-a",
		},
		{
			name: "uses AllocatedHyperNode when node mapping is unavailable",
			job:  &JobInfo{UID: "job-1", AllocatedHyperNode: "sn-b"},
			want: "sn-b",
		},
		{
			name: "infers from allocated task without topology config",
			job:  otherJobWithTaskOnNode("podgroup-0", "node-a", "prod"),
			want: "sn-a",
		},
		{
			name: "empty when no placement and no tasks",
			job: &JobInfo{
				UID: "job-empty",
				PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
				}},
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodes := nodesByHyperNode
			if tt.name == "uses AllocatedHyperNode when node mapping is unavailable" {
				nodes = nil
			}
			got := getJobAllocatedHyperNode(tt.job, hn, nodes)
			if got != tt.want {
				t.Fatalf("getJobAllocatedHyperNode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPodGroupAntiAffinityAgainstMatchingJobWithoutTopology(t *testing.T) {
	// podgroup-0: same label, no topologyAffinity, already placed on node-a (sn-a).
	// podgroup-1: topologyAffinity podGroupAntiAffinity against matching label.
	prodSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"topology.volcano.sh/group": "prod"},
	}
	term := scheduling.PodGroupAffinityTerm{
		PodGroupSelector: prodSelector,
		TopologyTierName: "supernode",
	}
	hn := HyperNodeInfoMap{
		"root": newTestHyperNode("root", 3, "cluster", ""),
		"sn-a": newTestHyperNode("sn-a", 2, "supernode", "root"),
		"sn-b": newTestHyperNode("sn-b", 2, "supernode", "root"),
	}
	hn["root"].Children = sets.New("sn-a", "sn-b")
	tierNameMap := HyperNodeTierNameMap{"supernode": 2, "cluster": 3}

	podgroup0 := otherJobWithTaskOnNode("podgroup-0", "node-a", "prod")
	podgroup1 := &JobInfo{
		UID:       "podgroup-1",
		Namespace: "default",
		PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
			Spec: scheduling.PodGroupSpec{
				TopologyAffinity: &scheduling.TopologyAffinitySpec{
					PodGroupAntiAffinity: &scheduling.PodGroupAntiAffinity{
						Required: []scheduling.PodGroupAffinityTerm{term},
					},
				},
			},
		}},
	}
	jobs := map[JobID]*JobInfo{
		"podgroup-0": podgroup0,
		"podgroup-1": podgroup1,
	}
	nodesByHyperNode := map[string]sets.Set[string]{
		"sn-a": sets.New("node-a"),
		"sn-b": sets.New("node-b"),
	}

	got, err := MatchingPodGroupsAllocatedHyperNodesForTerm(jobs, hn, tierNameMap, podgroup1, term, nodesByHyperNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.Equal(sets.New("sn-a")) {
		t.Fatalf("occupied supernode mismatch: want [sn-a], got %v", got.UnsortedList())
	}
}

func TestMatchingPodGroupsOccupiedHyperNodesWhenJobSpansSiblingDomains(t *testing.T) {
	// Three sibling supernodes under root; podgroup-1 spans sn-a and sn-b.
	// Anti-affinity at supernode tier must only block sn-a/sn-b, not sn-c.
	prodSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"topology.volcano.sh/group": "prod"},
	}
	term := scheduling.PodGroupAffinityTerm{
		PodGroupSelector: prodSelector,
		TopologyTierName: "supernode",
	}
	hn := HyperNodeInfoMap{
		"root": newTestHyperNode("root", 3, "cluster", ""),
		"sn-a": newTestHyperNode("sn-a", 2, "supernode", "root"),
		"sn-b": newTestHyperNode("sn-b", 2, "supernode", "root"),
		"sn-c": newTestHyperNode("sn-c", 2, "supernode", "root"),
	}
	tierNameMap := HyperNodeTierNameMap{"supernode": 2, "cluster": 3}
	nodesByHyperNode := map[string]sets.Set[string]{
		"sn-a": sets.New("node-a"),
		"sn-b": sets.New("node-b"),
		"sn-c": sets.New("node-c"),
		"root": sets.New("node-a", "node-b", "node-c"),
	}

	podgroup1 := &JobInfo{
		UID:                "podgroup-1",
		Namespace:          "default",
		AllocatedHyperNode: "root",
		PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
		}},
	}
	taskA := &TaskInfo{UID: "task-a"}
	taskA.Status = Allocated
	taskA.NodeName = "node-a"
	taskB := &TaskInfo{UID: "task-b"}
	taskB.Status = Allocated
	taskB.NodeName = "node-b"
	podgroup1.SubJobs = map[SubJobID]*SubJobInfo{
		SubJobID("default"): {
			UID: SubJobID("default"),
			TaskStatusIndex: map[TaskStatus]TasksMap{
				Allocated: {
					taskA.UID: taskA,
					taskB.UID: taskB,
				},
			},
		},
	}
	podgroup2 := &JobInfo{
		UID:       "podgroup-2",
		Namespace: "default",
		PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
			Spec: scheduling.PodGroupSpec{
				TopologyAffinity: &scheduling.TopologyAffinitySpec{
					PodGroupAntiAffinity: &scheduling.PodGroupAntiAffinity{
						Required: []scheduling.PodGroupAffinityTerm{term},
					},
				},
			},
		}},
	}
	jobs := map[JobID]*JobInfo{
		"podgroup-1": podgroup1,
		"podgroup-2": podgroup2,
	}

	got, err := MatchingPodGroupsAllocatedHyperNodesForTerm(jobs, hn, tierNameMap, podgroup2, term, nodesByHyperNode)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := sets.New("sn-a", "sn-b")
	if !got.Equal(want) {
		t.Fatalf("occupied supernodes mismatch: want %v, got %v", want.UnsortedList(), got.UnsortedList())
	}
}

func TestSyncJobAllocatedHyperNodeAfterPodRemoval(t *testing.T) {
	hn := HyperNodeInfoMap{
		"root": newTestHyperNode("root", 3, "cluster", ""),
		"sn-a": newTestHyperNode("sn-a", 2, "supernode", "root"),
		"sn-b": newTestHyperNode("sn-b", 2, "supernode", "root"),
	}
	nodesByHyperNode := map[string]sets.Set[string]{
		"sn-a": sets.New("node-a"),
		"sn-b": sets.New("node-b"),
		"root": sets.New("node-a", "node-b"),
	}

	subJobID := SubJobID("default")
	task1 := &TaskInfo{UID: "task-1"}
	task1.Status = Allocated
	task1.NodeName = "node-a"
	job := &JobInfo{
		UID:                JobID("job-1"),
		AllocatedHyperNode: "root",
		SubJobs: map[SubJobID]*SubJobInfo{
			subJobID: {
				UID:                subJobID,
				AllocatedHyperNode: "root",
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Allocated: {task1.UID: task1},
				},
			},
		},
	}

	SyncJobAllocatedHyperNode(job, hn, nodesByHyperNode)

	if job.SubJobs[subJobID].AllocatedHyperNode != "sn-a" {
		t.Fatalf("subJob AllocatedHyperNode = %q, want sn-a", job.SubJobs[subJobID].AllocatedHyperNode)
	}
	if job.AllocatedHyperNode != "sn-a" {
		t.Fatalf("job AllocatedHyperNode = %q, want sn-a", job.AllocatedHyperNode)
	}
}

func TestSyncJobAllocatedHyperNodeWidensAfterPodAdded(t *testing.T) {
	hn := HyperNodeInfoMap{
		"root": newTestHyperNode("root", 3, "cluster", ""),
		"sn-a": newTestHyperNode("sn-a", 2, "supernode", "root"),
		"sn-b": newTestHyperNode("sn-b", 2, "supernode", "root"),
	}
	nodesByHyperNode := map[string]sets.Set[string]{
		"sn-a": sets.New("node-a"),
		"sn-b": sets.New("node-b"),
		"root": sets.New("node-a", "node-b"),
	}

	subJobID := SubJobID("default")
	// task1 is the existing pod on sn-a; task2 is the scale-up pod landing on sn-b.
	task1 := &TaskInfo{UID: "task-1"}
	task1.Status = Allocated
	task1.NodeName = "node-a"
	task2 := &TaskInfo{UID: "task-2"}
	task2.Status = Allocated
	task2.NodeName = "node-b"
	job := &JobInfo{
		UID:                JobID("job-1"),
		AllocatedHyperNode: "sn-a", // stale value from before scale-up
		SubJobs: map[SubJobID]*SubJobInfo{
			subJobID: {
				UID:                subJobID,
				AllocatedHyperNode: "sn-a",
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Allocated: {task1.UID: task1, task2.UID: task2},
				},
			},
		},
	}

	SyncJobAllocatedHyperNode(job, hn, nodesByHyperNode)

	if job.SubJobs[subJobID].AllocatedHyperNode != "root" {
		t.Fatalf("subJob AllocatedHyperNode = %q, want root", job.SubJobs[subJobID].AllocatedHyperNode)
	}
	if job.AllocatedHyperNode != "root" {
		t.Fatalf("job AllocatedHyperNode = %q, want root", job.AllocatedHyperNode)
	}
}

func TestSyncJobAllocatedHyperNodeClearsWhenNoAllocatedTasks(t *testing.T) {
	hn := HyperNodeInfoMap{
		"root": newTestHyperNode("root", 3, "cluster", ""),
	}
	subJobID := SubJobID("default")
	job := &JobInfo{
		UID:                JobID("job-1"),
		AllocatedHyperNode: "root",
		SubJobs: map[SubJobID]*SubJobInfo{
			subJobID: {
				UID:                subJobID,
				AllocatedHyperNode: "root",
			},
		},
	}

	SyncJobAllocatedHyperNode(job, hn, nil)

	if job.SubJobs[subJobID].AllocatedHyperNode != "" {
		t.Fatalf("subJob AllocatedHyperNode = %q, want empty", job.SubJobs[subJobID].AllocatedHyperNode)
	}
	if job.AllocatedHyperNode != "" {
		t.Fatalf("job AllocatedHyperNode = %q, want empty", job.AllocatedHyperNode)
	}
}

func newTestHyperNode(name string, tier int, tierName, parent string) *HyperNodeInfo {
	return &HyperNodeInfo{
		Name:     name,
		tier:     tier,
		tierName: tierName,
		Parent:   parent,
		Children: sets.New[string](),
	}
}

func otherJobWithTaskOnNode(uid, nodeName, group string) *JobInfo {
	task := &TaskInfo{UID: TaskID(uid + "-task")}
	task.Status = Allocated
	task.NodeName = nodeName
	subJobID := SubJobID("default")
	return &JobInfo{
		UID:       JobID(uid),
		Namespace: "default",
		PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": group}},
		}},
		SubJobs: map[SubJobID]*SubJobInfo{
			subJobID: {
				UID: subJobID,
				TaskStatusIndex: map[TaskStatus]TasksMap{
					Allocated: {task.UID: task},
				},
			},
		},
	}
}
