/*
 Copyright 2022 The Volcano Authors.

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

package shuffle

import (
	"testing"

	"github.com/golang/mock/gomock"
	v1 "k8s.io/api/core/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	mock_framework "volcano.sh/volcano/pkg/scheduler/framework/mock_gen"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestShuffle(t *testing.T) {
	var highPriority int32
	var lowPriority int32
	highPriority = 100
	lowPriority = 10

	ctl := gomock.NewController(t)
	fakePlugin := mock_framework.NewMockPlugin(ctl)
	fakePlugin.EXPECT().Name().AnyTimes().Return("fake")
	fakePlugin.EXPECT().OnSessionOpen(gomock.Any()).Return()
	fakePlugin.EXPECT().OnSessionClose(gomock.Any()).Return()
	fakePluginBuilder := func(arguments framework.Arguments) framework.Plugin {
		return fakePlugin
	}

	plugins := map[string]framework.PluginBuilder{"fake": fakePluginBuilder}

	tests := []uthelper.TestCommonStruct{
		{
			Name:    "select pods with low priority and evict them",
			Plugins: plugins,
			Nodes: []*v1.Node{
				util.MakeNode("node1").
					Allocatable(api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
				util.MakeNode("node2").
					Allocatable(api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Capacity(api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
					Obj(),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.MakeQueue("default").Weight(1).Obj(),
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.MakePodGroup("pg1", "test").Queue("default").MinMember(0).Phase(schedulingv1beta1.PodGroupRunning).Obj(),
				util.MakePodGroup("pg2", "test").Queue("default").MinMember(0).Phase(schedulingv1beta1.PodGroupRunning).Obj(),
				util.MakePodGroup("pg3", "test").Queue("default").MinMember(0).Phase(schedulingv1beta1.PodGroupRunning).Obj(),
			},
			Pods: []*v1.Pod{
				util.MakePod("test", "pod1-1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "2G")).Obj()},
				).NodeName("node1").GroupName("pg1").Priority(&lowPriority).Phase(v1.PodRunning).Obj(),
				util.MakePod("test", "pod1-2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "2G")).Obj()},
				).NodeName("node1").GroupName("pg1").Priority(&highPriority).Phase(v1.PodRunning).Obj(),
				util.MakePod("test", "pod1-3").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "2G")).Obj()},
				).NodeName("node1").GroupName("pg1").Priority(&highPriority).Phase(v1.PodRunning).Obj(),
				util.MakePod("test", "pod2-1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "2G")).Obj()},
				).NodeName("node1").GroupName("pg1").Priority(&lowPriority).Phase(v1.PodRunning).Obj(),
				util.MakePod("test", "pod2-2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "2G")).Obj()},
				).NodeName("node2").GroupName("pg1").Priority(&highPriority).Phase(v1.PodRunning).Obj(),
				util.MakePod("test", "pod3-1").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "2G")).Obj()},
				).NodeName("node2").GroupName("pg1").Priority(&lowPriority).Phase(v1.PodRunning).Obj(),
				util.MakePod("test", "pod3-2").Containers(
					[]v1.Container{*util.NewContainer("test", "test").Requests(api.BuildResourceList("1", "2G")).Obj()},
				).NodeName("node2").GroupName("pg1").Priority(&highPriority).Phase(v1.PodRunning).Obj(),
			},
			ExpectEvictNum: 3,
			ExpectEvicted:  []string{"test/pod1-1", "test/pod2-1", "test/pod3-1"},
		},
	}
	shuffle := New()

	for i, test := range tests {
		trueValue := true
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:          "fake",
						EnabledVictim: &trueValue,
					},
				},
			},
		}

		fakePluginVictimFns := func() []api.VictimTasksFn {
			victimTasksFn := func(candidates []*api.TaskInfo) []*api.TaskInfo {
				evicts := make([]*api.TaskInfo, 0)
				for _, task := range candidates {
					if task.Priority == lowPriority {
						evicts = append(evicts, task)
					}
				}
				return evicts
			}

			victimTasksFns := make([]api.VictimTasksFn, 0)
			victimTasksFns = append(victimTasksFns, victimTasksFn)
			return victimTasksFns
		}

		t.Run(test.Name, func(t *testing.T) {
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()
			ssn.AddVictimTasksFns("fake", fakePluginVictimFns())
			test.Run([]framework.Action{shuffle})
			if err := test.CheckAll(i); err != nil {
				t.Fatal(err)
			}
		})
	}
}
