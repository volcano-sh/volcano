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

package grouptopologyaffinity

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func TestCompiledTermsOccupiedDomains(t *testing.T) {
	tierNameMap := api.HyperNodeTierNameMap{"supernode": 2, "rack": 1}
	hn := api.HyperNodeInfoMap{
		"root":  newTestHyperNode("root", 3, "cluster", ""),
		"sn-a":  newTestHyperNode("sn-a", 2, "supernode", "root"),
		"sn-b":  newTestHyperNode("sn-b", 2, "supernode", "root"),
		"sn-c":  newTestHyperNode("sn-c", 2, "supernode", "root"),
		"cab-a": newTestHyperNode("cab-a", 1, "rack", "sn-a"),
	}
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{"topology.volcano.sh/group": "prod"},
	}
	selfJob := &api.JobInfo{UID: "self", Namespace: "default", PodGroup: &api.PodGroup{}}

	spanningPeer := otherJobWithTasksOnNodes("spanning", "prod", "node-a", "node-b")
	spanningPeer.AllocatedHyperNode = "root"

	tests := []struct {
		name    string
		jobs    map[api.JobID]*api.JobInfo
		term    scheduling.PodGroupAffinityTerm
		want    sets.Set[string]
		wantErr bool
	}{
		{
			name: "single matching PodGroup at supernode tier",
			jobs: map[api.JobID]*api.JobInfo{
				"other": {
					UID: "other", Namespace: "default", AllocatedHyperNode: "sn-a",
					PodGroup: &api.PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "supernode"},
			want: sets.New("sn-a"),
		},
		{
			name: "rack tier resolves ancestor hyperNode",
			jobs: map[api.JobID]*api.JobInfo{
				"other": {
					UID: "other", Namespace: "default", AllocatedHyperNode: "cab-a",
					PodGroup: &api.PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "rack"},
			want: sets.New("cab-a"),
		},
		{
			name: "coarser allocated hyperNode expands to descendants at finer term tier",
			jobs: map[api.JobID]*api.JobInfo{
				"other": {
					UID: "other", Namespace: "default", AllocatedHyperNode: "sn-a",
					PodGroup: &api.PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "rack"},
			want: sets.New("cab-a"),
		},
		{
			name: "multiple matching PodGroups collapse to set",
			jobs: map[api.JobID]*api.JobInfo{
				"j1": {
					UID: "j1", Namespace: "default", AllocatedHyperNode: "sn-a",
					PodGroup: &api.PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
				"j2": {
					UID: "j2", Namespace: "default", AllocatedHyperNode: "sn-b",
					PodGroup: &api.PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "supernode"},
			want: sets.New("sn-a", "sn-b"),
		},
		{
			name: "self job in map is ignored",
			jobs: map[api.JobID]*api.JobInfo{
				"self": {
					UID: "self", Namespace: "default", AllocatedHyperNode: "sn-a",
					PodGroup: &api.PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "supernode"},
			want: sets.New[string](),
		},
		{
			name: "matching PodGroup without AllocatedHyperNode is skipped without node mapping",
			jobs: map[api.JobID]*api.JobInfo{
				"other": {
					UID: "other", Namespace: "default",
					PodGroup: &api.PodGroup{PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"topology.volcano.sh/group": "prod"}},
					}},
				},
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "supernode"},
			want: sets.New[string](),
		},
		{
			name: "matching PodGroup without AllocatedHyperNode is inferred from allocated tasks",
			jobs: map[api.JobID]*api.JobInfo{
				"other": otherJobWithTaskOnNode("other", "node-a", "prod"),
			},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "supernode"},
			want: sets.New("sn-a"),
		},
		{
			name: "spanning peer does not block an unoccupied sibling",
			jobs: map[api.JobID]*api.JobInfo{spanningPeer.UID: spanningPeer},
			term: scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "supernode"},
			want: sets.New("sn-a", "sn-b"),
		},
		{
			name:    "invalid term returns error",
			jobs:    map[api.JobID]*api.JobInfo{},
			term:    scheduling.PodGroupAffinityTerm{PodGroupSelector: selector, TopologyTierName: "missing"},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodesByHyperNode := map[string]sets.Set[string]{
				"sn-a":  sets.New("node-a"),
				"sn-b":  sets.New("node-b"),
				"sn-c":  sets.New("node-c"),
				"cab-a": sets.New("node-cab-a"),
			}
			selfJob.PodGroup.Spec.TopologyAffinity = &scheduling.TopologyAffinitySpec{
				PodGroupAntiAffinity: &scheduling.PodGroupAntiAffinity{Required: []scheduling.PodGroupAffinityTerm{tt.term}},
			}
			ssn := &framework.Session{Jobs: tt.jobs, HyperNodes: hn, HyperNodeTierNameMap: tierNameMap, RealNodesSet: nodesByHyperNode}
			plugin := New(framework.Arguments{}).(*groupTopologyAffinityPlugin)
			constraints, err := plugin.constraintsFor(ssn, selfJob, false)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := constraints[0].occupied
			if !got.Equal(tt.want) {
				t.Fatalf("want %v, got %v", tt.want.UnsortedList(), got.UnsortedList())
			}
		})
	}
}
