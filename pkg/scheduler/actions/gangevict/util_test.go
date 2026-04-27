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

package gangevict

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/sets"
	scheduling "volcano.sh/apis/pkg/apis/scheduling"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const emptyGradientPluginName = "test-empty-gradient"

type emptyGradientPlugin struct{}

func (p *emptyGradientPlugin) Name() string { return emptyGradientPluginName }

func (p *emptyGradientPlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddHyperNodeGradientForJobFn(p.Name(), func(*api.JobInfo, *api.HyperNodeInfo, api.SearchPurpose) [][]*api.HyperNodeInfo {
		return nil
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
		PodGroup: &api.PodGroup{
			PodGroup: scheduling.PodGroup{
				Spec: scheduling.PodGroupSpec{
					NetworkTopology: &scheduling.NetworkTopologySpec{
						Mode:               scheduling.HardNetworkTopologyMode,
						HighestTierAllowed: &highestTierAllowed,
					},
				},
			},
		},
	}

	domains := GetCandidateDomains(ssn, job, 8)
	assert.Empty(t, domains)
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

	domains := GetCandidateDomains(ssn, &api.JobInfo{}, 8)
	assert.Equal(t, []string{framework.ClusterTopHyperNode}, domains)
}

func boolPtr(v bool) *bool { return &v }
