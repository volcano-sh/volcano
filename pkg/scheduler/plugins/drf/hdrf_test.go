/*
Copyright 2020 The Volcano Authors.

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

package drf

import (
	"fmt"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/proportion"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func makePods(num int, cpu, mem, podGroupName string) []*v1.Pod {
	pods := []*v1.Pod{}
	for i := 0; i < num; i++ {
		pods = append(pods, 
			util.MakePod().
				Namespace("default").
				Name(fmt.Sprintf("%s-p%d", podGroupName, i)).
				NodeName("").
				PodPhase(v1.PodPending).
				ResourceList(api.BuildResourceList(cpu, mem)).
				GroupName(podGroupName).
				Labels(make(map[string]string)).
				NodeSelector(make(map[string]string)).
				Obj(),
		)
	}
	return pods
}

func mergePods(pods ...[]*v1.Pod) []*v1.Pod {
	ret := make([]*v1.Pod, 0)
	for _, items := range pods {
		ret = append(ret, items...)
	}
	return ret
}

func TestHDRF(t *testing.T) {
	options.Default()

	plugins := map[string]framework.PluginBuilder{PluginName: New, proportion.PluginName: proportion.New}
	tests := []struct {
		uthelper.TestCommonStruct
		expected map[string]*api.Resource
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "rescaling test",
				Plugins: plugins,
				PodGroups: []*schedulingv1.PodGroup{
					util.MakePodGroup().
						Name("pg1").
						Namespace("default").
						Queue("root-sci").
						MinMember(0).
						MinTaskMember(nil).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
					util.MakePodGroup().
						Name("pg1").
						Namespace("default").
						Queue("root-eng-dev").
						MinMember(0).
						MinTaskMember(nil).
						Mode("soft").
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
					util.MakePodGroup().
						Name("pg1").
						Namespace("default").
						Queue("root-eng-prod").
						MinMember(0).
						MinTaskMember(nil).
						Mode("soft").
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: mergePods(
					makePods(10, "1", "1G", "pg1"),
					makePods(10, "1", "0G", "pg21"),
					makePods(10, "0", "1G", "pg22"),
				),
				Queues: []*schedulingv1.Queue{
					util.MakeQueue().Name("root-sci").Weight(1).Capability(nil).Annotations(map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/sci",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/50",
					}).Obj(),
					util.MakeQueue().Name("root-eng-dev").Weight(1).Capability(nil).Annotations(map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/eng/dev",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/50/50",
					}).Obj(),
					util.MakeQueue().Name("root-sci").Weight(1).Capability(nil).Annotations(map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/eng/prod",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/50/50",
					}).Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("n").
						Allocatable(api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "50"}}...)).
						Capacity(api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "50"}}...)).
						Annotations(map[string]string{}).
						Labels(make(map[string]string)).
						Obj(),
				},
			},
			expected: map[string]*api.Resource{
				"pg1": {
					MilliCPU:        5000,
					Memory:          5000000000,
					ScalarResources: map[v1.ResourceName]float64{"pods": 5},
				},
				"pg21": {
					MilliCPU:        5000,
					Memory:          0,
					ScalarResources: map[v1.ResourceName]float64{"pods": 5},
				},
				"pg22": {
					MilliCPU:        0,
					Memory:          5000000000,
					ScalarResources: map[v1.ResourceName]float64{"pods": 5},
				},
			},
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "blocking nodes test",
				Plugins: plugins,
				PodGroups: []*schedulingv1.PodGroup{
					util.MakePodGroup().
						Name("pg1").
						Namespace("default").
						Queue("root-pg1").
						MinMember(0).
						MinTaskMember(nil).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
					util.MakePodGroup().
						Name("pg2").
						Namespace("default").
						Queue("root-pg2").
						MinMember(0).
						MinTaskMember(nil).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
					util.MakePodGroup().
						Name("pg31").
						Namespace("default").
						Queue("root-pg3-pg31").
						MinMember(0).
						MinTaskMember(nil).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
					util.MakePodGroup().
						Name("pg32").
						Namespace("default").
						Queue("root-pg3-pg31").
						MinMember(0).
						MinTaskMember(nil).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
					util.MakePodGroup().
						Name("pg4").
						Namespace("default").
						Queue("root-pg4").
						MinMember(0).
						MinTaskMember(nil).
						Phase(schedulingv1.PodGroupInqueue).
						Obj(),
				},
				Pods: mergePods(
					makePods(30, "1", "0G", "pg1"),
					makePods(30, "1", "0G", "pg2"),
					makePods(30, "1", "0G", "pg31"),
					makePods(30, "0", "1G", "pg32"),
					makePods(30, "0", "1G", "pg4"),
				),
				Queues: []*schedulingv1.Queue{
					util.MakeQueue().Name("root-pg1").Weight(1).Capability(nil).Annotations(map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/pg1",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25",
					}).Obj(),
					util.MakeQueue().Name("root-pg2").Weight(1).Capability(nil).Annotations(map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/pg2",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25",
					}).Obj(),
					util.MakeQueue().Name("root-pg3-pg31").Weight(1).Capability(nil).Annotations(map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/pg3/pg31",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25/50",
					}).Obj(),
					util.MakeQueue().Name("root-pg3-pg32").Weight(1).Capability(nil).Annotations(map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/pg3/pg32",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25/50",
					}).Obj(),
					util.MakeQueue().Name("root-pg4").Weight(1).Capability(nil).Annotations(map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/pg4",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25",
					}).Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode().
						Name("n").
						Allocatable(api.BuildResourceList("30", "30G", []api.ScalarResource{{Name: "pods", Value: "500"}}...)).
						Capacity(api.BuildResourceList("30", "30G", []api.ScalarResource{{Name: "pods", Value: "500"}}...)).
						Annotations(map[string]string{}).
						Labels(make(map[string]string)).
						Obj(),
				},
			},

			expected: map[string]*api.Resource{
				"pg1": {
					MilliCPU:        10000,
					Memory:          0,
					ScalarResources: map[v1.ResourceName]float64{"pods": 10},
				},
				"pg2": {
					MilliCPU:        10000,
					Memory:          0,
					ScalarResources: map[v1.ResourceName]float64{"pods": 10},
				},
				"pg31": {
					MilliCPU:        10000,
					Memory:          0,
					ScalarResources: map[v1.ResourceName]float64{"pods": 10},
				},
				"pg32": {
					MilliCPU:        0,
					Memory:          15000000000,
					ScalarResources: map[v1.ResourceName]float64{"pods": 15},
				},
				"pg4": {
					MilliCPU:        0,
					Memory:          15000000000,
					ScalarResources: map[v1.ResourceName]float64{"pods": 15},
				},
			},
		},
	}
	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:              PluginName,
					EnabledHierarchy:  &trueValue,
					EnabledQueueOrder: &trueValue,
					EnabledJobOrder:   &trueValue,
				},
				{
					Name:               "proportion",
					EnabledJobEnqueued: &trueValue,
					EnabledQueueOrder:  &trueValue,
					EnabledReclaimable: &trueValue,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(t.Name(), func(t *testing.T) {
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()
			test.Run([]framework.Action{allocate.New()})
			for _, job := range ssn.Jobs {
				if equality.Semantic.DeepEqual(test.expected, job.Allocated) {
					t.Fatalf("%s: job %s expected resource %s, but got %s", test.Name, job.Name, test.expected[job.Name], job.Allocated)
				}
			}
		})
	}
}
