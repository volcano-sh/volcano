/*
Copyright 2024 The Volcano Authors.

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

package conformance

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestConformancePlugin(t *testing.T) {
	var (
		// prepare preemptor and preemptees
		task1 = api.NewTaskInfo(
			util.MakePod().
				Namespace("kube-system").
				Name("test-pod").
				NodeName("test-node").
				PodPhase(v1.PodRunning).
				ResourceList(api.BuildResourceList("1", "1Gi")).
				GroupName("pg1").
				Labels(make(map[string]string)).
				NodeSelector(make(map[string]string)).
				Obj(),
		)
		task2 = api.NewTaskInfo(
			util.MakePod().
				Namespace("test-namespace").
				Name("test-pod").
				NodeName("test-node").
				PodPhase(v1.PodRunning).
				ResourceList(api.BuildResourceList("1", "1Gi")).
				GroupName("pg1").
				Labels(make(map[string]string)).
				NodeSelector(make(map[string]string)).
				Obj(),
		)
	)
	tests := []struct {
		uthelper.TestCommonStruct
		preemptees    []*api.TaskInfo
		expectVictims []*api.TaskInfo
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "conformance plugin",
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			preemptees:    []*api.TaskInfo{task1, task2},
			expectVictims: []*api.TaskInfo{task2},
		},
	}

	for _, test := range tests {
		trueValue := true
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:               PluginName,
						EnabledPreemptable: &trueValue,
					},
				},
			},
		}
		t.Run(test.Name, func(t *testing.T) {
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()
			victims := ssn.Preemptable(&api.TaskInfo{}, test.preemptees)
			if !equality.Semantic.DeepEqual(victims, test.expectVictims) {
				t.Errorf("case: %s error,  expect %v, but get %v", test.Name, test.expectVictims, victims)
			}
		})

	}
}
