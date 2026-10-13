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

	"volcano.sh/apis/pkg/apis/scheduling"
)

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
		name             string
		job              *JobInfo
		hard             bool
		soft             bool
		requiresTopology bool
		reqLen           int
		prefLen          int
	}{
		{name: "required only", job: withRequired, hard: true, requiresTopology: true, reqLen: 1},
		{name: "preferred only", job: withPreferred, soft: true, requiresTopology: true, prefLen: 1},
		{name: "both", job: withBoth, hard: true, soft: true, requiresTopology: true, reqLen: 1, prefLen: 1},
		{name: "nil podgroup", job: &JobInfo{}},
		{name: "nil topology affinity", job: &JobInfo{PodGroup: &PodGroup{}}},
		{
			name: "empty topology affinity",
			job: &JobInfo{PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{Spec: scheduling.PodGroupSpec{
				TopologyAffinity: &scheduling.TopologyAffinitySpec{},
			}}}},
		},
		{
			name: "empty podgroup anti-affinity",
			job: &JobInfo{PodGroup: &PodGroup{PodGroup: scheduling.PodGroup{Spec: scheduling.PodGroupSpec{
				TopologyAffinity: &scheduling.TopologyAffinitySpec{PodGroupAntiAffinity: &scheduling.PodGroupAntiAffinity{}},
			}}}},
		},
		{
			name: "job network topology without anti-affinity",
			job: &JobInfo{
				NetworkTopology: &scheduling.NetworkTopologySpec{Mode: scheduling.SoftNetworkTopologyMode},
			},
			requiresTopology: true,
		},
		{
			name: "subjob network topology without anti-affinity",
			job: &JobInfo{SubJobs: map[SubJobID]*SubJobInfo{
				"subjob": {NetworkTopology: &scheduling.NetworkTopologySpec{Mode: scheduling.SoftNetworkTopologyMode}},
			}},
			requiresTopology: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.job.ContainsHardPodGroupAntiAffinity(); got != tt.hard {
				t.Fatalf("ContainsHardPodGroupAntiAffinity = %v, want %v", got, tt.hard)
			}
			if got := tt.job.HasPreferredPodGroupAntiAffinity(); got != tt.soft {
				t.Fatalf("HasPreferredPodGroupAntiAffinity = %v, want %v", got, tt.soft)
			}
			if got := tt.job.RequiresHyperNodeTopology(); got != tt.requiresTopology {
				t.Fatalf("RequiresHyperNodeTopology = %v, want %v", got, tt.requiresTopology)
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
