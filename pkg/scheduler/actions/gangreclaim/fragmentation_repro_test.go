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
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/capacity"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// This characterization test reproduces #5958 on the unchanged scheduler.
// Changing only victim job names changes the stable bundle tie-breaker.
func TestGangReclaimFragmentationReproduction(t *testing.T) {
	previousOpts := options.ServerOpts
	options.ServerOpts = options.NewServerOption()
	options.ServerOpts.MinNodesToFind = 1
	t.Cleanup(func() { options.ServerOpts = previousOpts })

	for _, tc := range []struct {
		name           string
		spreadFirst    bool
		victimDeserved string
		wantEvictions  []string
		wantPipelined  bool
	}{
		{"spread first with four CPU reclaimable", true, "4", []string{"ns/spread-1", "ns/spread-2", "ns/packed-1", "ns/packed-2"}, true},
		{"packed first with four CPU reclaimable", false, "4", []string{"ns/packed-1", "ns/packed-2"}, true},
		{"spread first with two CPU reclaimable", true, "6", nil, false},
		{"packed first with two CPU reclaimable", false, "6", []string{"ns/packed-1", "ns/packed-2"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spreadPG, packedPG := "a-spread", "b-packed"
			if !tc.spreadFirst {
				spreadPG, packedPG = "b-spread", "a-packed"
			}
			pod := func(name, node, pg string, preemptable bool) *v1.Pod {
				value := "false"
				if preemptable {
					value = "true"
				}
				return util.BuildPod("ns", name, node, v1.PodRunning, api.BuildResourceList("1", "0"), pg,
					map[string]string{schedulingv1beta1.PodPreemptable: value}, nil)
			}
			test := uthelper.TestCommonStruct{
				Name: tc.name,
				Plugins: map[string]framework.PluginBuilder{
					gang.PluginName: gang.New, capacity.PluginName: capacity.New,
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList("4", "8Gi", api.ScalarResource{Name: "pods", Value: "100"}), nil),
					util.BuildNode("n2", api.BuildResourceList("4", "8Gi", api.ScalarResource{Name: "pods", Value: "100"}), nil),
				},
				PodGroups: []*schedulingv1beta1.PodGroup{
					util.BuildPodGroup(spreadPG, "ns", "victim", 1, nil, schedulingv1beta1.PodGroupRunning),
					util.BuildPodGroup(packedPG, "ns", "victim", 1, nil, schedulingv1beta1.PodGroupRunning),
					util.BuildPodGroup("protected", "ns", "victim", 2, nil, schedulingv1beta1.PodGroupRunning),
					util.BuildPodGroup("reclaimer", "ns", "reclaimer", 1, nil, schedulingv1beta1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					pod("spread-1", "n1", spreadPG, true),
					pod("spread-2", "n2", spreadPG, true),
					pod("spread-anchor", "n1", spreadPG, false),
					pod("packed-1", "n2", packedPG, true),
					pod("packed-2", "n2", packedPG, true),
					pod("packed-anchor", "n1", packedPG, false),
					pod("protected-1", "n1", "protected", false),
					pod("protected-2", "n2", "protected", false),
					util.BuildPod("ns", "reclaimer", "", v1.PodPending, api.BuildResourceList("2", "0"), "reclaimer", nil, nil),
				},
				Queues: []*schedulingv1beta1.Queue{
					util.BuildQueueWithResourcesQuantity("victim", api.BuildResourceList(tc.victimDeserved, "0"), nil),
					util.BuildQueueWithResourcesQuantity("reclaimer", api.BuildResourceList("2", "0"), nil),
				},
				ExpectEvictNum: len(tc.wantEvictions),
				ExpectEvicted:  tc.wantEvictions,
			}
			if tc.wantPipelined {
				test.ExpectPipeLined = map[string][]string{"ns/reclaimer": {"n2"}}
			}
			enabled, disabled := true, false
			ssn := test.RegisterSession([]conf.Tier{{Plugins: []conf.PluginOption{
				{Name: gang.PluginName, EnabledJobStarving: &enabled, EnabledJobPipelined: &enabled},
				{Name: capacity.PluginName, EnabledHierarchy: &disabled, EnablePreemptive: &enabled},
			}}}, nil)
			defer test.Close()
			test.Run([]framework.Action{New()})
			if err := test.CheckAll(0); err != nil {
				t.Fatal(err)
			}
			if got := ssn.JobPipelined(ssn.Jobs["ns/reclaimer"]); got != tc.wantPipelined {
				t.Errorf("reclaimer pipelined = %t, want %t", got, tc.wantPipelined)
			}
			t.Logf("spreadFirst=%t victimDeserved=%s evictions=%d reclaimerPipelined=%t", tc.spreadFirst, tc.victimDeserved, len(tc.wantEvictions), tc.wantPipelined)
		})
	}
}
