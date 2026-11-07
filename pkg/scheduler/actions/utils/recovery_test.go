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

package utils

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestRecoverNominationPlan_RollbackAndRetry(t *testing.T) {
	ensureServerOptsForTest()
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("pipeline-%d", failAt), func(t *testing.T) {
			t1, t2 := makeTask("ns/target", "t1"), makeTask("ns/target", "t2")
			target := api.NewJobInfo("ns/target", t1, t2)
			v := makeTask("ns/victim", "v")
			v.Status, v.NodeName = api.Running, "n1"
			v.Resreq.MilliCPU, v.InitResreq.MilliCPU = 2000, 2000
			victim := api.NewJobInfo("ns/victim", v)
			existingTask := makeTask("ns/existing", "existing")
			existing := api.NewJobInfo("ns/existing", existingTask)
			node := api.NewNodeInfo(util.BuildNode("n1", api.BuildResourceList("2", "0", api.ScalarResource{Name: "pods", Value: "10"}), nil))
			other := api.NewNodeInfo(util.BuildNode("n2", api.BuildResourceList("1", "0", api.ScalarResource{Name: "pods", Value: "10"}), nil))
			require.NoError(t, node.AddTask(v))
			ssn := &framework.Session{
				Jobs:  map[api.JobID]*api.JobInfo{target.UID: target, victim.UID: victim, existing.UID: existing},
				Nodes: map[string]*api.NodeInfo{node.Name: node, other.Name: other},
			}
			hn := testDomainHyperNode(ssn, framework.ClusterTopHyperNode, []*api.NodeInfo{node})
			parent := framework.NewStatement(ssn)
			defer parent.Discard()
			require.NoError(t, parent.Pipeline(existingTask, other.Name, false))
			plan, _, ok := BuildNominationPlanInDomain(ssn, nil, target, hn, []*api.TaskInfo{v}, "test", true)
			require.True(t, ok)

			calls, balance := 0, 0
			injectFailure := true
			ssn.AddEventHandler(&framework.EventHandler{
				AllocateFunc: func(event *framework.Event) {
					if event.Task.Job == target.UID || event.Task.Job == victim.UID {
						balance++
					}
					if event.Task.Job == target.UID {
						calls++
						if injectFailure && calls == failAt {
							event.Err = fmt.Errorf("injected reserve failure")
						}
					}
				},
				DeallocateFunc: func(event *framework.Event) {
					if event.Task.Job == target.UID || event.Task.Job == victim.UID {
						balance--
					}
				},
			})
			require.Error(t, RecoverNominationPlan(ssn, parent, plan))
			require.Len(t, parent.Operations(), 1, "failed recovery must preserve only pre-existing operations")
			require.False(t, parent.HasEvictions())
			require.Len(t, existing.TaskStatusIndex[api.Pipelined], 1)
			require.Len(t, target.TaskStatusIndex[api.Pending], 2)
			require.Empty(t, target.TaskStatusIndex[api.Pipelined])
			require.Len(t, victim.TaskStatusIndex[api.Running], 1)
			require.Empty(t, victim.TaskStatusIndex[api.Releasing])
			require.Zero(t, node.Releasing.MilliCPU)
			require.Zero(t, node.Pipelined.MilliCPU)
			require.Equal(t, float64(2000), node.Used.MilliCPU)
			require.Zero(t, balance, "plugin allocation/deallocation callbacks must balance")

			injectFailure = false
			plan, _, ok = BuildNominationPlanInDomain(ssn, nil, target, hn, []*api.TaskInfo{v}, "test", true)
			require.True(t, ok, "a fresh attempt must succeed after rollback")
			require.NoError(t, RecoverNominationPlan(ssn, parent, plan))
			require.Len(t, parent.Operations(), 4)
			require.Len(t, target.TaskStatusIndex[api.Pipelined], 2)
			require.Len(t, victim.TaskStatusIndex[api.Releasing], 1)
			parent.Discard()
			require.Zero(t, balance, "successful operations must transfer ownership without double rollback")
			require.Len(t, target.TaskStatusIndex[api.Pending], 2)
			require.Len(t, victim.TaskStatusIndex[api.Running], 1)
		})
	}
}

func TestRecoverNominationPlan_NilPlan(t *testing.T) {
	ssn := &framework.Session{}
	parent := framework.NewStatement(ssn)
	require.Error(t, RecoverNominationPlan(ssn, parent, nil))
	require.Empty(t, parent.Operations())
}
