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
		pods = append(pods, util.BuildPod("default",
			fmt.Sprintf("%s-p%d", podGroupName, i), "",
			v1.PodPending, api.BuildResourceList(cpu, mem),
			podGroupName, make(map[string]string), make(map[string]string)))
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
					util.BuildPodGroup("pg1", "default", "root-sci", 0, nil, schedulingv1.PodGroupInqueue),
					util.BuildPodGroup("pg21", "default", "root-eng-dev", 0, nil, schedulingv1.PodGroupInqueue),
					util.BuildPodGroup("pg22", "default", "root-eng-prod", 0, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: mergePods(
					makePods(10, "1", "1G", "pg1"),
					makePods(10, "1", "0G", "pg21"),
					makePods(10, "0", "1G", "pg22"),
				),
				Queues: []*schedulingv1.Queue{
					util.BuildQueueWithAnnos("root-sci", 1, nil, map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/sci",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/50",
					}),
					util.BuildQueueWithAnnos("root-eng-dev", 1, nil, map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/eng/dev",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/50/50",
					}),
					util.BuildQueueWithAnnos("root-eng-prod", 1, nil, map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/eng/prod",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/50/50",
					}),
				},
				Nodes: []*v1.Node{util.BuildNode("n", api.BuildResourceList("10", "10G", []api.ScalarResource{{Name: "pods", Value: "50"}}...), make(map[string]string))},
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
					util.BuildPodGroup("pg1", "default", "root-pg1", 0, nil, schedulingv1.PodGroupInqueue),
					util.BuildPodGroup("pg2", "default", "root-pg2", 0, nil, schedulingv1.PodGroupInqueue),
					util.BuildPodGroup("pg31", "default", "root-pg3-pg31", 0, nil, schedulingv1.PodGroupInqueue),
					util.BuildPodGroup("pg32", "default", "root-pg3-pg31", 0, nil, schedulingv1.PodGroupInqueue),
					util.BuildPodGroup("pg4", "default", "root-pg4", 0, nil, schedulingv1.PodGroupInqueue),
				},
				Pods: mergePods(
					makePods(30, "1", "0G", "pg1"),
					makePods(30, "1", "0G", "pg2"),
					makePods(30, "1", "0G", "pg31"),
					makePods(30, "0", "1G", "pg32"),
					makePods(30, "0", "1G", "pg4"),
				),
				Queues: []*schedulingv1.Queue{
					util.BuildQueueWithAnnos("root-pg1", 1, nil, map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/pg1",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25",
					}),
					util.BuildQueueWithAnnos("root-pg2", 1, nil, map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/pg2",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25",
					}),
					util.BuildQueueWithAnnos("root-pg3-pg31", 1, nil, map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/pg3/pg31",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25/50",
					}),
					util.BuildQueueWithAnnos("root-pg3-pg32", 1, nil, map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/pg3/pg32",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25/50",
					}),
					util.BuildQueueWithAnnos("root-pg4", 1, nil, map[string]string{
						schedulingv1.KubeHierarchyAnnotationKey:       "root/pg4",
						schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25",
					}),
				},
				Nodes: []*v1.Node{util.BuildNode("n", api.BuildResourceList("30", "30G", []api.ScalarResource{{Name: "pods", Value: "500"}}...), make(map[string]string))},
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
