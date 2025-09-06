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
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						Queue("c1").
						MinMember(0).
						MinTaskMember(nil).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("1", "1G")).
						GroupName("pg1").
						Labels(make(map[string]string)).
						NodeSelector(make(map[string]string)).
						Obj(),
				},
				Nodes: []*v1.Node{
				
					util.MakeNode().
						Name("n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(make(map[string]string)).
						Obj(),

					util.MakeNode().
						Name("n2").
						Allocatable(api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(make(map[string]string)).
						Obj(),
				},
				Queues: []*schedulingv1.Queue{
					util.MakeQueue().Name("c1").Weight(1).Capability(nil).Obj(),
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
					util.MakePodGroup().
						Name("pg1").
						Namespace("c1").
						Queue("c1").
						MinMember(0).
						MinTaskMember(nil).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: []*v1.Pod{
					util.MakePod().
						Namespace("c1").
						Name("p1").
						NodeName("").
						PodPhase(v1.PodPending).
						ResourceList(api.BuildResourceList("1", "1G")).
						GroupName("pg1").
						Labels(make(map[string]string)).
						NodeSelector(make(map[string]string)).
						Obj(),
				},
				Nodes: []*v1.Node{
				
					util.MakeNode().
						Name("n1").
						Allocatable(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(make(map[string]string)).
						Obj(),

					util.MakeNode().
						Name("n2").
						Allocatable(api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Annotations(map[string]string{}).
						Labels(make(map[string]string)).
						Obj(),
				},
				Queues: []*schedulingv1.Queue{
					util.MakeQueue().Name("c1").Weight(1).Capability(nil).Obj(),
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
