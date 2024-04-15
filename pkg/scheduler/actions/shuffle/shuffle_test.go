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
				util.BuildNode("node1", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
				util.BuildNode("node2", api.BuildResourceList("4", "8Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{
				util.BuildQueue("default", 1, nil),
			},
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "test", "default", 0, nil, schedulingv1beta1.PodGroupRunning),
				util.BuildPodGroup("pg2", "test", "default", 0, nil, schedulingv1beta1.PodGroupRunning),
				util.BuildPodGroup("pg3", "test", "default", 0, nil, schedulingv1beta1.PodGroupRunning),
			},
			Pods: []*v1.Pod{
				util.BuildPodWithPriority("test", "pod1-1", "node1", v1.PodRunning, api.BuildResourceList("1", "2G"), "pg1", make(map[string]string), make(map[string]string), &lowPriority),
				util.BuildPodWithPriority("test", "pod1-2", "node1", v1.PodRunning, api.BuildResourceList("1", "2G"), "pg1", make(map[string]string), make(map[string]string), &highPriority),
				util.BuildPodWithPriority("test", "pod1-3", "node1", v1.PodRunning, api.BuildResourceList("1", "2G"), "pg1", make(map[string]string), make(map[string]string), &highPriority),
				util.BuildPodWithPriority("test", "pod2-1", "node1", v1.PodRunning, api.BuildResourceList("1", "2G"), "pg2", make(map[string]string), make(map[string]string), &lowPriority),
				util.BuildPodWithPriority("test", "pod2-2", "node2", v1.PodRunning, api.BuildResourceList("1", "2G"), "pg2", make(map[string]string), make(map[string]string), &highPriority),
				util.BuildPodWithPriority("test", "pod3-1", "node2", v1.PodRunning, api.BuildResourceList("1", "2G"), "pg3", make(map[string]string), make(map[string]string), &lowPriority),
				util.BuildPodWithPriority("test", "pod3-2", "node2", v1.PodRunning, api.BuildResourceList("1", "2G"), "pg3", make(map[string]string), make(map[string]string), &highPriority),
			},
			EvictNum: 3,
			Evicted:  []string{"test/pod1-1", "test/pod2-1", "test/pod3-1"},
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
				t.Errorf("unexpected shuffle test; name: %s, err: %s", test.Name, err)
			}
		})
	}
}
