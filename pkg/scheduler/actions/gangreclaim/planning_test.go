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
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	actionutils "volcano.sh/volcano/pkg/scheduler/actions/utils"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/capacity"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/pdb"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestGangReclaimOrder_RejectedWholeLeavesAllowanceForSafe(t *testing.T) {
	options.Default()
	for _, policy := range []actionutils.VictimOrderPolicy{actionutils.VictimOrderSafeFirst, actionutils.VictimOrderPriorityFirst} {
		for _, allowWhole := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/whole-%t", policy, allowWhole), func(t *testing.T) {
				one := api.BuildResourceList("1", "0")
				fixture := &uthelper.TestCommonStruct{
					Plugins: map[string]framework.PluginBuilder{gang.PluginName: gang.New, capacity.PluginName: capacity.New, pdb.PluginName: pdb.New},
					Nodes:   []*v1.Node{util.BuildNode("n1", api.BuildResourceList("4", "0", api.ScalarResource{Name: "pods", Value: "20"}), nil)},
					Queues: []*v1beta1.Queue{
						util.BuildQueueWithResourcesQuantity("victim", api.BuildResourceList("3", "0"), nil),
						util.BuildQueueWithResourcesQuantity("target", one, nil),
					},
					PodGroups: []*v1beta1.PodGroup{
						util.BuildPodGroup("low", "ns", "victim", 2, nil, v1beta1.PodGroupRunning),
						util.BuildPodGroup("higher", "ns", "victim", 1, nil, v1beta1.PodGroupRunning),
						util.BuildPodGroup("target", "ns", "target", 1, nil, v1beta1.PodGroupInqueue),
					},
				}
				for _, group := range []string{"low", "higher"} {
					for i := 0; i < 2; i++ {
						fixture.Pods = append(fixture.Pods, util.BuildPod("ns", fmt.Sprintf("%s-%d", group, i), "n1", v1.PodRunning, one, group, nil, nil))
					}
				}
				fixture.Pods = append(fixture.Pods, util.BuildPod("ns", "target", "", v1.PodPending, one, "target", nil, nil))
				for _, pod := range fixture.Pods {
					pod.Labels = map[string]string{"protected": fmt.Sprint(pod.Name == "low-0" || pod.Name == "low-1")}
				}
				enabled := true
				ssn := fixture.RegisterSession([]conf.Tier{{Plugins: []conf.PluginOption{
					{Name: gang.PluginName, EnabledJobReady: &enabled, EnabledJobPipelined: &enabled},
					{Name: pdb.PluginName},
					{Name: capacity.PluginName, EnablePreemptive: &enabled, EnabledAllocatable: &enabled},
				}}}, nil)
				defer fixture.Close()
				target, low, higher := ssn.Jobs["ns/target"], ssn.Jobs["ns/low"], ssn.Jobs["ns/higher"]
				low.Priority, higher.Priority = 1, 2
				ssn.HyperNodes[framework.ClusterTopHyperNode] = &api.HyperNodeInfo{Name: framework.ClusterTopHyperNode}
				ssn.RealNodesList[framework.ClusterTopHyperNode] = []*api.NodeInfo{ssn.Nodes["n1"]}
				action := New()
				action.victimOrderPolicy, action.allowWholeBundle = policy, allowWhole

				// Capacity permits only one victim. PDB vetoes both low-priority
				// members, hiding the allowance spent by capacity in the intersection.
				var raw []*api.TaskInfo
				for _, job := range []*api.JobInfo{low, higher} {
					for _, task := range job.Tasks {
						raw = append(raw, task)
					}
				}
				ctx := &api.EvictionContext{Kind: api.EvictionKindGangReclaim, Job: target, HyperNode: framework.ClusterTopHyperNode}
				before := ssn.UnifiedEvictable(ctx, raw)
				require.Len(t, before, 1)
				require.Equal(t, low.UID, before[0].Job)
				require.NoError(t, ssn.InformerFactory().Policy().V1().PodDisruptionBudgets().Informer().GetStore().Add(&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "low", Namespace: "ns"},
					Spec:       policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"protected": "true"}}},
				}))
				require.Empty(t, ssn.UnifiedEvictable(ctx, raw), "PDB hides the member approved by capacity")
				stmt := framework.NewStatement(ssn)
				defer stmt.Discard()
				// The victim queue can spare only one CPU: the low-priority
				// two-task Whole must not consume the higher workload's Safe allowance.
				require.NotEmpty(t, action.reclaimJobInDomains(ssn, stmt, ssn.Queues[target.Queue], target))
				assert.Len(t, low.TaskStatusIndex[api.Running], 2)
				assert.Empty(t, low.TaskStatusIndex[api.Releasing])
				assert.Len(t, higher.TaskStatusIndex[api.Releasing], 1)
				assert.True(t, ssn.JobReady(higher))
				assert.Len(t, target.TaskStatusIndex[api.Pipelined], 1)
			})
		}
	}
}
