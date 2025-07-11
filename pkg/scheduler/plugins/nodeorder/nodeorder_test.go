/*
Copyright 2025 The Kubernetes Authors.

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

package nodeorder

import (
	"os"
	"testing"

	v1 "k8s.io/api/core/v1"
	k8smetrics "k8s.io/kubernetes/pkg/scheduler/metrics"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestMain(m *testing.M) {
	options.Default()
	k8smetrics.Register()
	os.Exit(m.Run())
}

type nodeOrderTestCase struct {
	uthelper.TestCommonStruct
	LeastRequestedWeight int
	MostRequestedWeight  int
}

func TestNodeOrderPlugin(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		PluginName:      New,
		gang.PluginName: gang.New,
	}

	tests := []nodeOrderTestCase{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "leastAllocated strategy",
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					util.BuildNode("n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("c1", 1, nil),
				},
				ExpectBindsNum: 1,
				ExpectBindMap: map[string]string{
					"c1/p1": "n2",
				},
			},
			LeastRequestedWeight: 1,
			MostRequestedWeight:  0,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name: "mostAllocated strategy",
				PodGroups: []*schedulingv1.PodGroup{
					util.BuildPodGroup("pg1", "c1", "c1", 0, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: []*v1.Pod{
					util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1G"), "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*v1.Node{
					util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
					util.BuildNode("n2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				},
				Queues: []*schedulingv1.Queue{
					util.BuildQueue("c1", 1, nil),
				},
				ExpectBindsNum: 1,
				ExpectBindMap: map[string]string{
					"c1/p1": "n1",
				},
			},
			LeastRequestedWeight: 0,
			MostRequestedWeight:  1,
		},
	}

	trueValue := true

	for i, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:            gang.PluginName,
							EnabledJobReady: &trueValue,
						},
						{
							Name:             PluginName,
							EnabledNodeOrder: &trueValue,
							Arguments: framework.Arguments{
								LeastRequestedWeight: test.LeastRequestedWeight,
								MostRequestedWeight:  test.MostRequestedWeight,
							},
						},
					},
				},
			}

			test.Plugins = plugins
			test.RegisterSession(tiers, nil)
			defer test.Close()

			action := allocate.New()
			test.Run([]framework.Action{action})

			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
