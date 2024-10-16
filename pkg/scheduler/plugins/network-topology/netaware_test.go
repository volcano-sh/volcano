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

package networktopology

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/plugins/priority"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestMain(m *testing.M) {
	options.Default() // init default options params which used in some packages
	os.Exit(m.Run())
}

func TestParse(t *testing.T) {
	plug := New(framework.Arguments{networkTopologyWeight: 10, networkTopologyType: dynamicAware})
	netplug := plug.(*netTopPlugin)
	netplug.parseArguments()
	assert.Equal(t, netplug.weight, 10)
	assert.Equal(t, netplug.topologyType, dynamicAware)

	plug = New(framework.Arguments{networkTopologyWeight: 10, networkTopologyKeys: " switch, idc"})
	netplug = plug.(*netTopPlugin)
	netplug.parseArguments()
	assert.Equal(t, netplug.topologyType, staticAware)
	netplug.parseStaticAwareArguments()
	assert.Equal(t, netplug.staticTopAware.topologyKeys, []string{"switch", "idc"})
	assert.Equal(t, netplug.staticTopAware.weight, 10)
}

func TestStaticNetTopAware(t *testing.T) {
	trueValue := true
	plugins := map[string]framework.PluginBuilder{
		PluginName: New,
		//Note: when enable gang plugin: pods need to set different role spec, because predicate plugin default enabled cache for same spec task
		gang.PluginName:       gang.New,
		priority.PluginName:   priority.New,
		predicates.PluginName: predicates.New,
	}
	high, low := int32(100000), int32(10)
	highPrioCls := util.BuildPriorityClass("high-priority", high)
	lowPrioCls := util.BuildPriorityClass("low-priority", low)
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
				},
				{
					Name:             priority.PluginName,
					EnabledJobOrder:  &trueValue,
					EnabledTaskOrder: &trueValue,
				},
				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
					Arguments: map[string]interface{}{
						predicates.NodeAffinityEnable: true,
						predicates.PodAffinityEnable:  true,
					},
				},
				{
					Name:             PluginName,
					EnabledNodeOrder: &trueValue,
					Arguments: map[string]interface{}{
						networkTopologyKeys: "role, switch, idc", // same role nodes get higher score, then same switch, final same idc
					},
				},
			},
		},
	}
	tests := []uthelper.TestCommonStruct{
		{
			Name: "with only one key, best effort to allocate to nodes labeled with this key",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg1", "ns1", "q1", 2, nil, schedulingv1beta1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{ // let pod1 choose node with label role=worker, then check pod2 also scheduled to that node
				util.BuildPodWithPriority("ns1", "pod1", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg1", make(map[string]string), map[string]string{"role": "worker"}, &high),
				util.BuildPod("ns1", "pod2", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg1", make(map[string]string), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{util.BuildQueue("q1", 1, nil)},
			Nodes: []*v1.Node{
				util.BuildNode("node1", api.BuildResourceList("20", "20G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"role": "worker"}),
				util.BuildNode("node2", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"role": "ps"}),
				util.BuildNode("node3", api.BuildResourceList("20", "20G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"role": "ps"}),
			},
			ExpectBindsNum: 2,
			ExpectBindMap:  map[string]string{"ns1/pod1": "node1", "ns1/pod2": "node1"},
		},
		{
			Name: "with multiple keys, the front keys get higher score",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg2", "ns1", "q1", 2, nil, schedulingv1beta1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{ // let pod1 choose node with label role=worker, then check pod2 also scheduled to other node with same label
				util.BuildPodWithPriority("ns1", "pod1", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg2", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"role": "worker"}, &high),
				util.BuildPod("ns1", "pod2", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg2", map[string]string{"volcano.sh/task-spec": "master"}, make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{util.BuildQueue("q1", 1, nil)},
			Nodes: []*v1.Node{ // let pod1 first choose node1, which also with label switch; then pod2 preferred to choose node2 with same label switch=swt1
				util.BuildNode("node1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"role": "worker", "switch": "swt1", "idc": "idc1"}),
				util.BuildNode("node2", api.BuildResourceList("20", "20G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"switch": "swt1"}),
				util.BuildNode("node3", api.BuildResourceList("20", "20G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"idc": "idc1"}),
			},
			ExpectBindsNum: 2,
			ExpectBindMap:  map[string]string{"ns1/pod1": "node1", "ns1/pod2": "node2"},
		},
		{
			Name: "with not enougth resource, then choose other nodes without configed key",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg3", "ns1", "q1", 3, nil, schedulingv1beta1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{ // all pods must set different role spec, because predicate plugin default enabled cache for same spec task
				util.BuildPodWithPriority("ns1", "pod1", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg3", map[string]string{"volcano.sh/task-spec": "worker"}, map[string]string{"role": "worker"}, &high),
				util.BuildPod("ns1", "pod-work-2", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg3", map[string]string{"volcano.sh/task-spec": "master"}, make(map[string]string)),
				util.BuildPod("ns1", "pod-work-3", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg3", make(map[string]string), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{util.BuildQueue("q1", 1, nil)},
			Nodes: []*v1.Node{
				util.BuildNode("node1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"role": "worker", "switch": "swt1"}),
				util.BuildNode("node2", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"switch": "swt1"}),
				util.BuildNode("node3", api.BuildResourceList("20", "20G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"idc": "idc1"}),
			},
			ExpectBindsNum: 3,
			ExpectBindMap:  map[string]string{"ns1/pod1": "node1", "ns1/pod-work-2": "node2", "ns1/pod-work-3": "node3"},
		},
		{
			Name: "with not enougth resource and cannot allocate whole job, deallocating",
			PodGroups: []*schedulingv1beta1.PodGroup{
				util.BuildPodGroup("pg4", "ns1", "q1", 2, nil, schedulingv1beta1.PodGroupInqueue),
			},
			Pods: []*v1.Pod{
				util.BuildPod("ns1", "pod-work-2", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg4", make(map[string]string), make(map[string]string)),
				util.BuildPod("ns1", "pod-work-3", "", v1.PodPending, api.BuildResourceList("10", "10G"), "pg4", make(map[string]string), make(map[string]string)),
			},
			Queues: []*schedulingv1beta1.Queue{util.BuildQueue("q1", 1, nil)},
			Nodes: []*v1.Node{
				util.BuildNode("node1", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{"role": "worker", "switch": "swt1"}),
			},
			ExpectBindsNum: 0,
			ExpectBindMap:  map[string]string{},
		},
	}
	for i, test := range tests {
		test.Plugins = plugins
		test.PriClass = []*schedulingv1.PriorityClass{highPrioCls, lowPrioCls}
		t.Run(test.Name, func(t *testing.T) {
			test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{allocate.New()})
			err := test.CheckAll(i)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
