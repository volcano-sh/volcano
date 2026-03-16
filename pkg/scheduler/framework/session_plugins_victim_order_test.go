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

package framework_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestBuildVictimsPriorityQueueJobTieBreaksWithTaskOrder(t *testing.T) {
	trueValue := true
	plugins := map[string]framework.PluginBuilder{priority.PluginName: priority.New}
	highPri, lowPri := int32(100), int32(1)
	queueQ1 := util.BuildQueue("q1", 1, nil)
	nodeN1 := util.BuildNode("n1", api.BuildResourceList("10", "10Gi", []api.ScalarResource{{Name: "pods", Value: "20"}}...), nil)
	pgHigh := util.BuildPodGroup("pg-high", "ns1", "q1", 1, nil, schedulingv1.PodGroupRunning)
	pgLow := util.BuildPodGroup("pg-low", "ns1", "q1", 1, nil, schedulingv1.PodGroupRunning)
	pgPreemptor := util.BuildPodGroup("pg-preemptor", "ns1", "q1", 1, nil, schedulingv1.PodGroupPending)
	pHigh := util.BuildPodWithPriority("ns1", "p-high", "n1", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg-high", nil, nil, &highPri)
	pLow := util.BuildPodWithPriority("ns1", "p-low", "n1", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg-low", nil, nil, &lowPri)
	pPreemptor := util.BuildPodWithPriority("ns1", "p-preemptor", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg-preemptor", nil, nil, &highPri)

	tiers := []conf.Tier{{
		Plugins: []conf.PluginOption{{
			Name:             priority.PluginName,
			EnabledJobOrder:  &trueValue,
			EnabledTaskOrder: &trueValue,
		}},
	}}

	findTask := func(ssn *framework.Session, podName string) *api.TaskInfo {
		for _, job := range ssn.Jobs {
			for _, task := range job.Tasks {
				if task.Name == podName {
					return task
				}
			}
		}
		return nil
	}

	baseTestStruct := func() uthelper.TestCommonStruct {
		return uthelper.TestCommonStruct{
			Plugins: plugins,
			Queues:  []*schedulingv1.Queue{queueQ1.DeepCopy()},
			Nodes:   []*v1.Node{nodeN1.DeepCopy()},
			PodGroups: []*schedulingv1.PodGroup{
				pgHigh.DeepCopy(),
				pgLow.DeepCopy(),
			},
			Pods: []*v1.Pod{
				pHigh.DeepCopy(),
				pLow.DeepCopy(),
			},
		}
	}

	t.Run("preemptor job found", func(t *testing.T) {
		tc := baseTestStruct()
		tc.PodGroups = append(tc.PodGroups, pgPreemptor.DeepCopy())
		tc.Pods = append(tc.Pods, pPreemptor.DeepCopy())
		ssn := tc.RegisterSession(tiers, nil)
		defer tc.Close()

		high := findTask(ssn, "p-high")
		low := findTask(ssn, "p-low")
		preemptor := findTask(ssn, "p-preemptor")
		assert.NotNil(t, high)
		assert.NotNil(t, low)
		assert.NotNil(t, preemptor)

		victims := []*api.TaskInfo{high, low}
		pq := ssn.BuildVictimsPriorityQueue(victims, preemptor)
		first := pq.Pop().(*api.TaskInfo)
		assert.Equal(t, "p-low", first.Name)
	})

	t.Run("preemptor job missing", func(t *testing.T) {
		tc := baseTestStruct()
		ssn := tc.RegisterSession(tiers, nil)
		defer tc.Close()

		high := findTask(ssn, "p-high")
		low := findTask(ssn, "p-low")
		assert.NotNil(t, high)
		assert.NotNil(t, low)

		victims := []*api.TaskInfo{high, low}
		pq := ssn.BuildVictimsPriorityQueue(victims, &api.TaskInfo{Job: api.JobID("missing")})
		first := pq.Pop().(*api.TaskInfo)
		assert.Equal(t, "p-low", first.Name)
	})
}
