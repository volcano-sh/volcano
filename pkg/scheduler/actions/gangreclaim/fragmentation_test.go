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

package gangreclaim

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"volcano.sh/volcano/pkg/scheduler/actions/utils"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func fitResource(cpu, memory, gpu float64) *api.Resource {
	r := &api.Resource{MilliCPU: cpu, Memory: memory}
	if gpu > 0 {
		r.AddScalar(api.GPUResourceName, gpu)
	}
	return r
}

func fitNode(name string, idle *api.Resource) *api.NodeInfo {
	return &api.NodeInfo{Name: name, Idle: idle, Releasing: api.EmptyResource(), Pipelined: api.EmptyResource()}
}

func TestBundleResourceFitScores(t *testing.T) {
	for _, tc := range []struct {
		name    string
		idle    *api.Resource
		release *api.Resource
		target  *api.Resource
	}{
		{"CPU", fitResource(0, 0, 0), fitResource(1000, 0, 0), fitResource(2000, 0, 0)},
		{"memory", fitResource(4000, 1, 0), fitResource(0, 2, 0), fitResource(1000, 4, 0)},
		{"scalar GPU count", fitResource(4000, 0, 0), fitResource(0, 0, 1), fitResource(1000, 0, 2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes := []*api.NodeInfo{fitNode("n1", tc.idle.Clone()), fitNode("n2", tc.idle.Clone())}
			victim := func(node string) *api.TaskInfo {
				return &api.TaskInfo{TransactionContext: api.TransactionContext{NodeName: node}, Resreq: tc.release.Clone()}
			}
			spread := &utils.Bundle{Tasks: []*api.TaskInfo{victim("n1"), victim("n2")}}
			packed := &utils.Bundle{Tasks: []*api.TaskInfo{victim("n2"), victim("n2")}}
			targets := []*api.TaskInfo{{InitResreq: tc.target.Clone()}}
			setBundleResourceFitScores([]*utils.Bundle{spread, packed}, nodes, targets)
			assert.Equal(t, 0, spread.ResourceFitScore)
			assert.Equal(t, 1, packed.ResourceFitScore)
			// Resource capacity on an unaffected node (e.g. a tainted control
			// plane) must not mask the layout difference on victim nodes.
			withSpareNode := append(append([]*api.NodeInfo(nil), nodes...), fitNode("unaffected", tc.target.Clone()))
			setBundleResourceFitScores([]*utils.Bundle{spread, packed}, withSpareNode, targets)
			assert.Equal(t, 0, spread.ResourceFitScore)
			assert.Equal(t, 1, packed.ResourceFitScore)
			for _, node := range nodes {
				assert.Equal(t, tc.idle, node.Idle)
				assert.True(t, node.Releasing.IsEmpty())
				assert.True(t, node.Pipelined.IsEmpty())
			}
			assert.Equal(t, tc.target, targets[0].InitResreq)
			for _, task := range packed.Tasks {
				assert.Equal(t, tc.release, task.Resreq)
			}
		})
	}
}

func TestBundleResourceFitScores_CountsEachTargetOnce(t *testing.T) {
	nodes := []*api.NodeInfo{fitNode("n1", api.EmptyResource()), fitNode("n2", api.EmptyResource())}
	nodes[0].Releasing = fitResource(3000, 0, 0)
	nodes[0].Pipelined = fitResource(1000, 0, 0)
	targets := []*api.TaskInfo{{InitResreq: fitResource(2000, 0, 0)}, {InitResreq: fitResource(1000, 0, 2)}}
	noVictims := &utils.Bundle{}
	alreadyFits := &utils.Bundle{Tasks: []*api.TaskInfo{
		{TransactionContext: api.TransactionContext{NodeName: "n1"}, Resreq: fitResource(1000, 0, 0)},
	}}
	withGPU := &utils.Bundle{Tasks: []*api.TaskInfo{
		{TransactionContext: api.TransactionContext{NodeName: "n1"}, Resreq: fitResource(2000, 0, 2)},
		{TransactionContext: api.TransactionContext{NodeName: "n2"}, Resreq: fitResource(2000, 0, 2)},
	}}
	setBundleResourceFitScores([]*utils.Bundle{noVictims, alreadyFits, withGPU}, nodes, targets)
	assert.Equal(t, 0, noVictims.ResourceFitScore)
	assert.Equal(t, 0, alreadyFits.ResourceFitScore, "capacity available before eviction is not a new fit")
	assert.Equal(t, 2, withGPU.ResourceFitScore, "a target fitting both nodes must only count once")
	assert.Equal(t, fitResource(3000, 0, 0), nodes[0].Releasing)
	assert.Equal(t, fitResource(1000, 0, 0), nodes[0].Pipelined)
}

func TestBundleResourceFitScores_RecomputedAfterFiltering(t *testing.T) {
	nodes := []*api.NodeInfo{fitNode("n1", api.EmptyResource())}
	first := &api.TaskInfo{UID: "first", TransactionContext: api.TransactionContext{NodeName: "n1"}, Resreq: fitResource(1000, 0, 0)}
	second := &api.TaskInfo{UID: "second", TransactionContext: api.TransactionContext{NodeName: "n1"}, Resreq: fitResource(1000, 0, 0)}
	bundles := []*utils.Bundle{{Type: utils.BundleSafe, Tasks: []*api.TaskInfo{first, second}, LocalRes: fitResource(2000, 0, 0)}}
	targets := []*api.TaskInfo{{InitResreq: fitResource(2000, 0, 0)}}
	setBundleResourceFitScores(bundles, nodes, targets)
	assert.Equal(t, 1, bundles[0].ResourceFitScore)
	filtered := utils.ApplyAllowedTasks(bundles, map[api.TaskID]struct{}{"first": {}})
	require.Len(t, filtered, 1)
	setBundleResourceFitScores(filtered, nodes, targets)
	assert.Equal(t, 0, filtered[0].ResourceFitScore)
}

func TestFragmentationOrderDefaultsOffAfterConfigRemoval(t *testing.T) {
	action := New()
	ssn := &framework.Session{Configurations: []conf.Configuration{{Name: "gangreclaim", Arguments: map[string]interface{}{
		EnableFragmentationAwareVictimOrderKey: true,
	}}}}
	action.parseArguments(ssn)
	assert.True(t, action.enableFragmentationAwareVictimOrder)
	ssn.Configurations = nil
	action.parseArguments(ssn)
	assert.False(t, action.enableFragmentationAwareVictimOrder)
}

func BenchmarkBundleResourceFitScores(b *testing.B) {
	for _, size := range []struct{ nodes, bundles, targets int }{{100, 8, 1}, {1000, 32, 16}} {
		for _, enabled := range []bool{false, true} {
			b.Run(fmt.Sprintf("nodes=%d/bundles=%d/targets=%d/enabled=%t", size.nodes, size.bundles, size.targets, enabled), func(b *testing.B) {
				nodes := make([]*api.NodeInfo, size.nodes)
				for i := range nodes {
					nodes[i] = fitNode(fmt.Sprintf("n%d", i), api.EmptyResource())
				}
				targets := make([]*api.TaskInfo, size.targets)
				for i := range targets {
					targets[i] = &api.TaskInfo{InitResreq: fitResource(2000, 0, 0)}
				}
				bundles := make([]*utils.Bundle, size.bundles)
				for i := range bundles {
					bundles[i] = &utils.Bundle{Job: &api.JobInfo{UID: api.JobID(fmt.Sprintf("j%d", i))}, LocalRes: fitResource(2000, 0, 0), Tasks: []*api.TaskInfo{
						{TransactionContext: api.TransactionContext{NodeName: nodes[i].Name}, Resreq: fitResource(1000, 0, 0)},
						{TransactionContext: api.TransactionContext{NodeName: nodes[i+1].Name}, Resreq: fitResource(1000, 0, 0)},
					}}
				}
				b.ReportAllocs()
				for b.Loop() {
					if enabled {
						setBundleResourceFitScores(bundles, nodes, targets)
					}
					utils.SortBundlesForReclaim(bundles, targets[0].InitResreq, nil, nil)
				}
			})
		}
	}
}
