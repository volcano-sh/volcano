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

package framework

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/ptr"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
)

func TestNominationCandidateValidationMatchesFullGradients(t *testing.T) {
	tests := []struct {
		name                        string
		first, second               []string
		neutralFirst, neutralSecond bool
		fallback                    bool
	}{
		{name: "intersection", first: []string{"a", "b"}, second: []string{"a"}},
		{name: "different covering domains do not intersect", first: []string{"root"}, second: []string{"a"}},
		{name: "child with same leaf set", first: []string{"leaf"}, second: []string{"leaf"}},
		{name: "neutral and constrained", neutralFirst: true, second: []string{"a"}},
		{name: "all neutral", neutralFirst: true, neutralSecond: true},
		{name: "rejected", second: []string{"a"}},
		{name: "legacy plugin fallback", first: []string{"a", "b"}, second: []string{"a"}, fallback: true},
		{name: "invalid neutral result", neutralFirst: true, first: []string{"a"}, neutralSecond: true},
	}
	for _, tt := range tests {
		for _, subJobLevel := range []bool{false, true} {
			t.Run(tt.name+map[bool]string{false: "/job", true: "/subJob"}[subJobLevel], func(t *testing.T) {
				ssn := &Session{
					HyperNodes: api.HyperNodeInfoMap{
						"root": {Name: "root", Children: sets.New("a", "b")},
						"a":    {Name: "a", Parent: "root", Children: sets.New("leaf")},
						"b":    {Name: "b", Parent: "root"},
						"leaf": {Name: "leaf", Parent: "a"},
					},
					RealNodesSet: map[string]sets.Set[string]{
						"root": sets.New("n1", "n2"), "a": sets.New("n1"), "b": sets.New("n2"), "leaf": sets.New("n1"),
					},
					hyperNodeGradientForJobFns:    map[string]api.HyperNodeGradientForJobFn{},
					hyperNodeGradientForSubJobFns: map[string]api.HyperNodeGradientForSubJobFn{},
					Tiers: []conf.Tier{{Plugins: []conf.PluginOption{
						{Name: "first", EnabledHyperNodeGradient: ptr.To(true)},
						{Name: "second", EnabledHyperNodeGradient: ptr.To(true)},
					}}},
				}
				for name, hn := range ssn.HyperNodes {
					tier := map[string]int{"root": 3, "a": 2, "b": 2, "leaf": 1}[name]
					copy := api.NewHyperNodeInfo(&topologyv1alpha1.HyperNode{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: topologyv1alpha1.HyperNodeSpec{Tier: tier}})
					copy.Parent, copy.Children = hn.Parent, hn.Children
					ssn.HyperNodes[name] = copy
				}
				fullCalls := 0
				for i, name := range []string{"first", "second"} {
					names, neutral := tt.first, tt.neutralFirst
					if i == 1 {
						names, neutral = tt.second, tt.neutralSecond
					}
					var layer []*api.HyperNodeInfo
					for _, name := range names {
						layer = append(layer, ssn.HyperNodes[name])
					}
					result := api.HyperNodeGradientResult{Unconstrained: neutral}
					if len(layer) > 0 {
						result.Gradients = [][]*api.HyperNodeInfo{layer}
					}
					ssn.AddHyperNodeGradientForJobFn(name, func(*api.JobInfo, *api.HyperNodeInfo, api.SearchPurpose) api.HyperNodeGradientResult {
						fullCalls++
						return result
					})
					ssn.AddHyperNodeGradientForSubJobFn(name, func(*api.SubJobInfo, *api.HyperNodeInfo, api.SearchPurpose) api.HyperNodeGradientResult {
						fullCalls++
						return result
					})
					if !(tt.fallback && i == 1) {
						ssn.AddHyperNodeCandidateFn(name, func(*api.JobInfo, *api.SubJobInfo, *api.HyperNodeInfo, []*api.HyperNodeInfo) api.HyperNodeGradientResult {
							return result
						})
					}
				}
				job := &api.JobInfo{}
				var subJob *api.SubJobInfo
				if subJobLevel {
					subJob = &api.SubJobInfo{}
				}
				for _, pinned := range []string{"root", "a", "b", "leaf", "missing"} {
					var gradients [][]*api.HyperNodeInfo
					if subJob == nil {
						gradients, _ = ssn.HyperNodeGradientForJobFn(job, ssn.HyperNodes["root"], api.PurposeAllocate)
					} else {
						gradients, _ = ssn.HyperNodeGradientForSubJobFn(subJob, ssn.HyperNodes["root"], api.PurposeAllocate)
					}
					want := false
					for _, layer := range gradients {
						for _, hn := range layer {
							if len(ssn.RealNodesSet[pinned]) > 0 && ssn.RealNodesSet[hn.Name].IsSuperset(ssn.RealNodesSet[pinned]) {
								want = true
							}
						}
					}
					fullCalls = 0
					got := ssn.HyperNodeContainsNomination(job, subJob, ssn.HyperNodes["root"], pinned)
					if got != want {
						t.Fatalf("pinned %s: fast=%t full=%t", pinned, got, want)
					}
					if !tt.fallback && fullCalls != 0 {
						t.Fatal("fast path unexpectedly evaluated full gradients")
					}
				}
			})
		}
	}
}
