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

package reclaim

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/conformance"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func buildTopologyAwareReclaimSession(t *testing.T, victimPreemptable string) (*uthelper.TestCommonStruct, *framework.Session) {
	t.Helper()
	trueValue := true
	test := &uthelper.TestCommonStruct{
		Name: "topology-aware-reclaim",
		Plugins: map[string]framework.PluginBuilder{
			conformance.PluginName: conformance.New,
		},
		Pods: []*v1.Pod{
			util.BuildPod("c1", "victim", "n1", v1.PodRunning, api.BuildResourceList("2", "2G"),
				"pg1", map[string]string{schedulingv1beta1.PodPreemptable: victimPreemptable}, make(map[string]string)),
			util.BuildPod("c1", "reclaimor", "", v1.PodPending, api.BuildResourceList("2", "2G"),
				"pg2", make(map[string]string), make(map[string]string)),
		},
		Nodes: []*v1.Node{
			util.BuildNode("n1", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
		},
		PodGroups: []*schedulingv1beta1.PodGroup{
			util.BuildPodGroupWithAnno("pg1", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning, nil),
			util.BuildPodGroupWithAnno("pg2", "c1", "q2", 1, nil, schedulingv1beta1.PodGroupInqueue, nil),
		},
		Queues: []*schedulingv1beta1.Queue{
			util.BuildQueue("q1", 1, nil),
			util.BuildQueue("q2", 1, nil),
		},
	}
	ssn := test.RegisterSession([]conf.Tier{{
		Plugins: []conf.PluginOption{
			{
				Name:               conformance.PluginName,
				EnabledReclaimable: &trueValue,
			},
			{
				Name:               "victim-score",
				EnabledVictimScore: &trueValue,
			},
		},
	}}, nil)
	ssn.AddBatchVictimScoreFn("victim-score", func(_ *api.TaskInfo, nodesToVictims map[string][]*api.TaskInfo) (map[string]float64, error) {
		scores := make(map[string]float64, len(nodesToVictims))
		for node := range nodesToVictims {
			scores[node] = 1
		}
		return scores, nil
	})
	return test, ssn
}

func findTaskByName(ssn *framework.Session, name string) *api.TaskInfo {
	for _, job := range ssn.Jobs {
		for _, task := range job.Tasks {
			if task.Name == name {
				return task
			}
		}
	}
	return nil
}

func TestTopologyAwareReclaim(t *testing.T) {
	ra := New()
	ra.topologyAwareReclaimWorkerNum = 1
	ra.minCandidateNodesPercentage = 100
	ra.minCandidateNodesAbsolute = 1
	ra.maxCandidateNodesAbsolute = 10

	t.Run("no reclaimable victims", func(t *testing.T) {
		test, ssn := buildTopologyAwareReclaimSession(t, "false")
		defer test.Close()

		reclaimor := findTaskByName(ssn, "reclaimor")
		job := ssn.Jobs[reclaimor.Job]
		stmt := framework.NewStatement(ssn)
		ok, err := ra.topologyAwareReclaim(ssn, stmt, reclaimor, job)
		assert.False(t, ok)
		assert.Error(t, err)
	})

	t.Run("same-queue victims are filtered out", func(t *testing.T) {
		trueValue := true
		test := &uthelper.TestCommonStruct{
			Name: "same-queue",
			Plugins: map[string]framework.PluginBuilder{
				conformance.PluginName: conformance.New,
			},
			Pods: []*v1.Pod{
				util.BuildPod("c1", "victim", "n1", v1.PodRunning, api.BuildResourceList("2", "2G"),
					"pg1", map[string]string{schedulingv1beta1.PodPreemptable: "true"}, make(map[string]string)),
				util.BuildPod("c1", "reclaimor", "", v1.PodPending, api.BuildResourceList("2", "2G"),
					"pg2", make(map[string]string), make(map[string]string)),
			},
			Nodes: []*v1.Node{
				util.BuildNode("n1", api.BuildResourceList("2", "2Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroupWithAnno("pg1", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupRunning, nil),
				util.BuildPodGroupWithAnno("pg2", "c1", "q1", 1, nil, schedulingv1beta1.PodGroupInqueue, nil),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("q1", 1, nil),
			},
		}
		ssn := test.RegisterSession([]conf.Tier{{
			Plugins: []conf.PluginOption{{
				Name:               conformance.PluginName,
				EnabledReclaimable: &trueValue,
			}},
		}}, nil)
		defer test.Close()

		reclaimor := findTaskByName(ssn, "reclaimor")
		job := ssn.Jobs[reclaimor.Job]
		stmt := framework.NewStatement(ssn)
		ok, err := ra.topologyAwareReclaim(ssn, stmt, reclaimor, job)
		assert.False(t, ok)
		assert.Error(t, err)
	})

	t.Run("successfully reclaims cross-queue victim", func(t *testing.T) {
		test, ssn := buildTopologyAwareReclaimSession(t, "true")
		defer test.Close()

		reclaimor := findTaskByName(ssn, "reclaimor")
		assert.NotNil(t, reclaimor)
		job := ssn.Jobs[reclaimor.Job]
		stmt := framework.NewStatement(ssn)
		ok, err := ra.topologyAwareReclaim(ssn, stmt, reclaimor, job)
		assert.NoError(t, err)
		assert.True(t, ok)
		assert.Equal(t, "n1", reclaimor.NodeName)
		assert.Equal(t, api.Pipelined, reclaimor.Status)
	})
}
