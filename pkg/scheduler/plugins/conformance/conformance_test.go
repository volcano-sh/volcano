package conformance

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestConformancePlugin(t *testing.T) {
	tests := []struct {
		name string
		uthelper.TestCommonStruct
		arguments  framework.Arguments
		preemptees []*api.TaskInfo
	}{
		{
			name: "conformance plugin",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
		},
	}

	trueValue := true
	for _, test := range tests {
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:               PluginName,
						EnabledPreemptable: &trueValue,
						Arguments:          test.arguments,
					},
				},
			},
		}
		// prepare preemptor and preemptees
		task1 := api.NewTaskInfo(
			util.BuildPod("kube-system", "test-pod",
				"test-node", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
		)
		task2 := api.NewTaskInfo(
			util.BuildPod("test-namespace", "test-pod",
				"test-node", v1.PodRunning, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
		)

		ssn := test.RegisterSession(tiers, nil)
		preemptees := []*api.TaskInfo{task1, task2}
		victims := ssn.Preemptable(&api.TaskInfo{}, preemptees)
		expectVictims := []*api.TaskInfo{task2}
		if !reflect.DeepEqual(victims, expectVictims) {
			t.Errorf("case: %s error,  expect %v, but get %v", test.name, expectVictims, victims)
		}
		test.Close()
	}
}
