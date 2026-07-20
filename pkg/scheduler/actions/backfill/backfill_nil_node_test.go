/*
Copyright 2025 The Volcano Authors.

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

package backfill

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// nilBestNodePlugin is a test plugin that registers a BestNodeFn which
// always returns nil and a BatchNodeOrderFn that returns an error,
// causing PrioritizeNodes to return an empty score map. Together these
// ensure both BestNodeFn and SelectBestNodeAndScore return nil.
const nilBestNodePluginName = "nilbestnode"

type nilBestNodePlugin struct{}

func nilBestNodeNew(_ framework.Arguments) framework.Plugin {
	return &nilBestNodePlugin{}
}

func (p *nilBestNodePlugin) Name() string {
	return nilBestNodePluginName
}

func (p *nilBestNodePlugin) OnSessionOpen(ssn *framework.Session) {
	ssn.AddBestNodeFn(nilBestNodePluginName, func(task *api.TaskInfo, nodeScores map[float64][]*api.NodeInfo) *api.NodeInfo {
		// Always return nil — no node is "best"
		return nil
	})

	ssn.AddBatchNodeOrderFn(nilBestNodePluginName, func(task *api.TaskInfo, nodes []*api.NodeInfo) (map[string]float64, error) {
		// Return error to force PrioritizeNodes to return empty nodeScores,
		// which makes SelectBestNodeAndScore also return nil.
		return nil, fmt.Errorf("simulated scoring failure")
	})
}

func (p *nilBestNodePlugin) OnSessionClose(_ *framework.Session) {}

// TestBackfillNilNodeNoPanic verifies that the backfill action does not
// panic when BestNodeFn returns nil for all candidate nodes and
// SelectBestNodeAndScore also returns nil (due to empty scores).
//
// Before the fix at backfill.go:103, this test panics with:
//
//	panic: runtime error: invalid memory address or nil pointer dereference
//	goroutine ... [running]:
//	volcano.sh/volcano/pkg/scheduler/actions/backfill.(*Action).Execute(...)
//	    pkg/scheduler/actions/backfill/backfill.go:103
//
// After the fix, the task is gracefully skipped via continue.
func TestBackfillNilNodeNoPanic(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		gang.PluginName:       gang.New,
		predicates.PluginName: predicates.New,
		nilBestNodePluginName: nilBestNodeNew,
	}

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:            gang.PluginName,
					EnabledJobReady: &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
				{
					Name:             nilBestNodePluginName,
					EnabledBestNode:  &trueValue,
					EnabledNodeOrder: &trueValue,
				},
			},
		},
	}

	tests := []uthelper.TestCommonStruct{
		{
			Name: "backfill does not panic when BestNodeFn returns nil for all shards",
			PodGroups: []*schedulingv1.PodGroup{
				util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				// BestEffort pod (zero resource requests) for backfill
				util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("0", "0"), "pg1", make(map[string]string), make(map[string]string)),
			},
			// 2+ nodes so len(predicateNodes) > 1, which triggers BestNodeFn loop
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1.Queue{
				util.BuildQueue("c1", 1, nil),
			},
			// Expect 0 binds because scoring fails and BestNodeFn returns nil
			ExpectBindsNum:   0,
			MinimalBindCheck: true,
		},
	}

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			test.Plugins = plugins
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()

			// This is the line that panics without the nil check fix.
			// If this test passes without panic, the fix is working.
			action := New()
			action.Execute(ssn)

			if err := test.CheckAll(i); err != nil {
				t.Fatalf("Test %s failed: %v", test.Name, err)
			}
		})
	}
}
