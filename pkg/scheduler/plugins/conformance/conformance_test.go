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
			util.BuildPod("kube-system", "test-pod",
				"test-node", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
		)
		task2 = api.NewTaskInfo(
			util.BuildPod("test-namespace", "test-pod",
				"test-node", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
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
