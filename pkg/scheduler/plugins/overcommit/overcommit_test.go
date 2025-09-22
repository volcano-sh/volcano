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

package overcommit

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestOvercommitPlugin(t *testing.T) {

	n1 := util.MakeNode().
		Name("n1").
		Allocatable(api.BuildResourceList("2", "4Gi")).
		Capacity(api.BuildResourceList("2", "4Gi")).
		Annotations(map[string]string{}).
		Labels(map[string]string{}).
		Obj()
	n2 := util.MakeNode().
		Name("n2").
		Allocatable(api.BuildResourceList("4", "16Gi")).
		Capacity(api.BuildResourceList("4", "16Gi")).
		Annotations(map[string]string{}).
		Labels(map[string]string{}).
		Obj()

	hugeResource := api.BuildResourceList("20000m", "20G")
	normalResource := api.BuildResourceList("2000m", "2G")
	smallResource := api.BuildResourceList("200m", "0.5G")

	// pg that requires normal resources
	pg1 := util.MakePodGroup().
		Name("pg1").
		Namespace("test-namespace").
		Queue("c1").
		MinMember(2).
		MinTaskMember(nil).
		Phase(schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue)).
		Obj()

	pg1.Spec.MinResources = &normalResource
	// pg that requires small resources
	pg2 := util.MakePodGroup().
		Name("pg2").
		Namespace("test-namespace").
		Queue("c1").
		MinMember(2).
		MinTaskMember(nil).
		Phase(schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue)).
		Obj()
	pg2.Spec.MinResources = &hugeResource
	// pg that no requires resources
	pg3 := util.MakePodGroup().
		Name("pg2").
		Namespace("test-namespace").
		Queue("c1").
		MinMember(2).
		MinTaskMember(nil).
		Phase(schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue)).
		Obj()

	queue1 := util.MakeQueue().Name("c1").Weight(1).Capability(nil).Obj()
	queue2 := util.MakeQueue().Name("c1").Weight(1).Capability(smallResource).Obj()

	tests := []struct {
		uthelper.TestCommonStruct
		arguments           framework.Arguments
		expectedEnqueueAble bool
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "overCommitFactor is more than 0",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Nodes:     []*v1.Node{n1, n2},
			},
			arguments: framework.Arguments{
				overCommitFactor: 1.2,
			},
			expectedEnqueueAble: true,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "overCommitFactor is less than 0",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Nodes:     []*v1.Node{n1, n2},
			},
			arguments: framework.Arguments{
				overCommitFactor: 0.8,
			},
			expectedEnqueueAble: true,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "when the required resources of pg are too large",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg2},
				Queues:    []*schedulingv1.Queue{queue1},
				Nodes:     []*v1.Node{n1, n2},
			},
			arguments: framework.Arguments{
				overCommitFactor: 1.2,
			},
			expectedEnqueueAble: false,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "when pg does not fill MinResources",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg3},
				Queues:    []*schedulingv1.Queue{queue2},
				Nodes:     []*v1.Node{n1, n2},
			},
			arguments: framework.Arguments{
				overCommitFactor: 1.2,
			},
			expectedEnqueueAble: true,
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			trueValue := true
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:               PluginName,
							EnabledJobEnqueued: &trueValue,
							Arguments:          test.arguments,
						},
					},
				},
			}
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()
			for _, job := range ssn.Jobs {
				ssn.JobEnqueued(job)
				isEnqueue := ssn.JobEnqueueable(job)
				if !equality.Semantic.DeepEqual(test.expectedEnqueueAble, isEnqueue) {
					t.Errorf("case: %s error,  expect %v, but get %v", test.Name, test.expectedEnqueueAble, isEnqueue)
				}
			}
		})
	}

}
