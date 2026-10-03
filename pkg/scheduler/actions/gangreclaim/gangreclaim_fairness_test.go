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
	"testing"

	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins"
	"volcano.sh/volcano/pkg/scheduler/plugins/binpack"
	"volcano.sh/volcano/pkg/scheduler/plugins/capacity"
	"volcano.sh/volcano/pkg/scheduler/plugins/drf"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/nodeorder"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// tierWithDefaults builds a tier the way the scheduler config loader does: each plugin is
// named only, and ApplyPluginConfDefaults enables its extension points.
func tierWithDefaults(names ...string) []conf.Tier {
	opts := make([]conf.PluginOption, 0, len(names))
	for _, name := range names {
		opt := conf.PluginOption{Name: name}
		plugins.ApplyPluginConfDefaults(&opt)
		opts = append(opts, opt)
	}
	return []conf.Tier{{Plugins: opts}}
}

// A low priority Job in qa fills the node and a high priority Job in qb is pending.
func gangReclaimAcrossQueuesCase(name string, builders map[string]framework.PluginBuilder) uthelper.TestCommonStruct {
	pods := []*v1.Pod{
		util.BuildPod("c1", "high-0", "", v1.PodPending, api.BuildResourceList("2", "1G"), "pg-high", nil, nil),
	}
	for _, p := range []string{"low-0", "low-1", "low-2", "low-3", "low-4", "low-5", "low-6"} {
		pods = append(pods, util.BuildPod("c1", p, "n1", v1.PodRunning, api.BuildResourceList("1", "1G"), "pg-low", nil, nil))
	}
	return uthelper.TestCommonStruct{
		Name:    name,
		Plugins: builders,
		PriClass: []*schedulingv1.PriorityClass{
			util.BuildPriorityClass("low", 10),
			util.BuildPriorityClass("high", 1000),
		},
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroupWithPrio("pg-low", "c1", "qa", 1, nil, schedulingv1beta1.PodGroupRunning, "low"),
			util.BuildPodGroupWithPrio("pg-high", "c1", "qb", 1, nil, schedulingv1beta1.PodGroupInqueue, "high"),
		},
		Pods: pods,
		Nodes: []*v1.Node{
			util.BuildNode("n1", api.BuildResourceList("7", "16Gi", []api.ScalarResource{{Name: "pods", Value: "20"}}...), nil),
		},
		Queues: []*schedulingv1beta1.Queue{
			util.BuildQueue("qa", 1, nil),
			util.BuildQueue("qb", 1, nil),
		},
	}
}

// Without a queue fairness plugin nothing defines whether qa is over its share, so
// gangreclaim must not evict from it. With the v1.15.0 release notes config it did, and
// across sessions the evicted Pods were allocated back to qa and evicted again (#6045).
func TestGangReclaimNeedsQueueFairnessPlugin(t *testing.T) {
	releaseNotes := []string{priority.PluginName, gang.PluginName, drf.PluginName, predicates.PluginName, nodeorder.PluginName, binpack.PluginName}
	builders := map[string]framework.PluginBuilder{
		priority.PluginName:   priority.New,
		gang.PluginName:       gang.New,
		drf.PluginName:        drf.New,
		predicates.PluginName: predicates.New,
		nodeorder.PluginName:  nodeorder.New,
		binpack.PluginName:    binpack.New,
	}

	t.Run("no queue fairness plugin", func(t *testing.T) {
		test := gangReclaimAcrossQueuesCase("release notes config", builders)
		test.ExpectEvictNum = 0
		test.RegisterSession(tierWithDefaults(releaseNotes...), nil)
		defer test.Close()
		test.Run([]framework.Action{New()})
		if err := test.CheckAll(0); err != nil {
			t.Fatal(err)
		}
	})

	// drf with hierarchy orders queues but defines no Overused or Preemptive function, so
	// gangreclaim has no share to reclaim against and must evict nothing, as reclaim does.
	t.Run("drf with hierarchy", func(t *testing.T) {
		enabled := true
		tiers := tierWithDefaults(releaseNotes...)
		for i := range tiers[0].Plugins {
			if tiers[0].Plugins[i].Name == drf.PluginName {
				tiers[0].Plugins[i].EnabledHierarchy = &enabled
			}
		}
		test := gangReclaimAcrossQueuesCase("release notes config with drf hierarchy", builders)
		test.ExpectEvictNum = 0
		test.RegisterSession(tiers, nil)
		defer test.Close()
		test.Run([]framework.Action{New()})
		if err := test.CheckAll(0); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("with proportion", func(t *testing.T) {
		withProportion := map[string]framework.PluginBuilder{proportion.PluginName: proportion.New}
		for k, v := range builders {
			withProportion[k] = v
		}
		test := gangReclaimAcrossQueuesCase("release notes config plus proportion", withProportion)
		test.RegisterSession(tierWithDefaults(append(releaseNotes, proportion.PluginName)...), nil)
		defer test.Close()
		// Only qa's Pods above its minAvailable are evicted, the known SAFE bundle behaviour from #5994.
		test.ExpectEvictNum = 6
		test.ExpectEvicted = []string{"c1/low-0", "c1/low-1", "c1/low-2", "c1/low-3", "c1/low-4", "c1/low-5"}
		test.Run([]framework.Action{New()})
		if err := test.CheckAll(0); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("with capacity", func(t *testing.T) {
		withCapacity := map[string]framework.PluginBuilder{capacity.PluginName: capacity.New}
		for k, v := range builders {
			withCapacity[k] = v
		}
		test := gangReclaimAcrossQueuesCase("release notes config plus capacity", withCapacity)
		test.Queues = []*schedulingv1beta1.Queue{
			util.BuildQueueWithResourcesQuantity("qa", api.BuildResourceList("3500m", "8Gi"), nil),
			util.BuildQueueWithResourcesQuantity("qb", api.BuildResourceList("3500m", "8Gi"), nil),
		}
		test.RegisterSession(tierWithDefaults(append(releaseNotes, capacity.PluginName)...), nil)
		defer test.Close()
		// capacity reclaims qa down towards its deserved share.
		test.ExpectEvictNum = 4
		test.ExpectEvicted = []string{"c1/low-0", "c1/low-1", "c1/low-2", "c1/low-3"}
		test.Run([]framework.Action{New()})
		if err := test.CheckAll(0); err != nil {
			t.Fatal(err)
		}
	})
}

func TestQueueFairnessEnabled(t *testing.T) {
	builders := map[string]framework.PluginBuilder{
		priority.PluginName:   priority.New,
		gang.PluginName:       gang.New,
		drf.PluginName:        drf.New,
		proportion.PluginName: proportion.New,
		capacity.PluginName:   capacity.New,
	}
	enabled := true
	drfHierarchy := []conf.Tier{{Plugins: []conf.PluginOption{{Name: drf.PluginName, EnabledHierarchy: &enabled}}}}
	plugins.ApplyPluginConfDefaults(&drfHierarchy[0].Plugins[0])

	cases := map[string]struct {
		tiers []conf.Tier
		want  bool
	}{
		"release notes plugins": {tiers: tierWithDefaults(priority.PluginName, gang.PluginName, drf.PluginName), want: false},
		"proportion":            {tiers: tierWithDefaults(priority.PluginName, gang.PluginName, proportion.PluginName), want: true},
		"capacity":              {tiers: tierWithDefaults(priority.PluginName, gang.PluginName, capacity.PluginName), want: true},
		"drf with hierarchy":    {tiers: drfHierarchy, want: false},
		"functions turned off":  {tiers: []conf.Tier{{Plugins: []conf.PluginOption{{Name: proportion.PluginName}}}}, want: false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			test := uthelper.TestCommonStruct{Name: name, Plugins: builders}
			ssn := test.RegisterSession(tc.tiers, nil)
			defer test.Close()
			if got := ssn.QueueFairnessEnabled(); got != tc.want {
				t.Errorf("QueueFairnessEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
