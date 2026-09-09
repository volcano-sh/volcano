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

package gangpreempt

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/capacity"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestGangPlan_OnlyEvictsWhatTargetNeeds(t *testing.T) {
	options.Default()
	for _, idle := range []int{0, 1} {
		t.Run(fmt.Sprintf("idle-%d", idle), func(t *testing.T) {
			one := api.BuildResourceList("1", "0")
			fixture := &uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{gang.PluginName: gang.New, priority.PluginName: priority.New, capacity.PluginName: capacity.New},
				Nodes:   []*v1.Node{util.BuildNode("n1", api.BuildResourceList(fmt.Sprint(4+idle), "0", api.ScalarResource{Name: "pods", Value: "20"}), nil)},
				Queues: []*v1beta1.Queue{
					util.BuildQueueWithResourcesQuantity("victim", one, nil),
					util.BuildQueueWithResourcesQuantity("target", api.BuildResourceList("3", "0"), nil),
				},
				PodGroups: []*v1beta1.PodGroup{
					util.BuildPodGroup("victim", "ns", "victim", 1, nil, v1beta1.PodGroupRunning),
					util.BuildPodGroup("target", "ns", "victim", 1, nil, v1beta1.PodGroupInqueue),
				},
			}
			for i := 0; i < 4; i++ {
				fixture.Pods = append(fixture.Pods, util.BuildPod("ns", fmt.Sprintf("v%d", i), "n1", v1.PodRunning, one, "victim", nil, nil))
			}
			for i := 0; i < 3; i++ {
				fixture.Pods = append(fixture.Pods, util.BuildPod("ns", fmt.Sprintf("p%d", i), "", v1.PodPending, one, "target", nil, nil))
			}
			enabled := true
			ssn := fixture.RegisterSession([]conf.Tier{{Plugins: []conf.PluginOption{
				{Name: gang.PluginName, EnabledJobReady: &enabled, EnabledJobPipelined: &enabled},
				{Name: priority.PluginName},
				{Name: capacity.PluginName, EnablePreemptive: &enabled, EnabledAllocatable: &enabled},
			}}}, nil)
			defer fixture.Close()
			target, victim := ssn.Jobs["ns/target"], ssn.Jobs["ns/victim"]
			require.NotNil(t, target)
			require.NotNil(t, victim)
			target.Priority = 100
			victim.Priority = 1
			ssn.HyperNodes[framework.ClusterTopHyperNode] = &api.HyperNodeInfo{Name: framework.ClusterTopHyperNode}
			ssn.RealNodesList[framework.ClusterTopHyperNode] = []*api.NodeInfo{ssn.Nodes["n1"]}

			stmt := framework.NewStatement(ssn)
			defer stmt.Discard()
			nominations := New().preemptJobInDomains(ssn, stmt, ssn.Queues[target.Queue], target)
			require.NotEmpty(t, nominations)
			assert.Len(t, victim.TaskStatusIndex[api.Releasing], 1-idle)
			assert.Len(t, target.TaskStatusIndex[api.Pipelined], 1)
			assert.Len(t, target.TaskStatusIndex[api.Pending], 2, "optional pending tasks must not enlarge the gang target")
			assert.True(t, ssn.JobReady(victim), "Safe selection must preserve victim gang readiness")
		})
	}
}
