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
					util.MakePodGroup("pg1", "default").Queue("root-sci").MinMember(0).Phase(schedulingv1.PodGroupInqueue).Obj(),
					util.MakePodGroup("pg21", "default").Queue("root-eng-devi").MinMember(0).Phase(schedulingv1.PodGroupInqueue).Obj(),
					util.MakePodGroup("pg22", "default").Queue("root-eng-prod").MinMember(0).Phase(schedulingv1.PodGroupInqueue).Obj(),
				},
				Pods: mergePods(
					makePods(10, "1", "1G", "pg1"),
					makePods(10, "1", "0G", "pg21"),
					makePods(10, "0", "1G", "pg22"),
				),
				Queues: []*schedulingv1.Queue{
					util.MakeQueue("root-sci").Weight(1).Capability(nil).Annotations(
						map[string]string{
							schedulingv1.KubeHierarchyAnnotationKey:       "root/sci",
							schedulingv1.KubeHierarchyWeightAnnotationKey: "100/50",
						}).Obj(),
					util.MakeQueue("root-eng-dev").Weight(1).Capability(nil).Annotations(
						map[string]string{
							schedulingv1.KubeHierarchyAnnotationKey:       "root/eng/dev",
							schedulingv1.KubeHierarchyWeightAnnotationKey: "100/50/50",
						}).Obj(),
					util.MakeQueue("root-eng-prod").Weight(1).Capability(nil).Annotations(
						map[string]string{
							schedulingv1.KubeHierarchyAnnotationKey:       "root/eng/prod",
							schedulingv1.KubeHierarchyWeightAnnotationKey: "100/50/50",
						}).Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode("n").
						Allocatable(api.BuildResourceList("10", "10Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
						Capacity(api.BuildResourceList("10", "10Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...)).
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
					util.MakePodGroup("pg1", "default").Queue("root-pg1").MinMember(0).Phase(schedulingv1.PodGroupInqueue).Obj(),
					util.MakePodGroup("pg2", "default").Queue("root-pg2").MinMember(0).Phase(schedulingv1.PodGroupInqueue).Obj(),
					util.MakePodGroup("pg31", "default").Queue("root-pg3-pg31").MinMember(0).Phase(schedulingv1.PodGroupInqueue).Obj(),
					util.MakePodGroup("pg32", "default").Queue("root-pg3-pg31").MinMember(0).Phase(schedulingv1.PodGroupInqueue).Obj(),
					util.MakePodGroup("pg4", "default").Queue("root-pg4").MinMember(0).Phase(schedulingv1.PodGroupInqueue).Obj(),
				},
				Pods: mergePods(
					makePods(30, "1", "0G", "pg1"),
					makePods(30, "1", "0G", "pg2"),
					makePods(30, "1", "0G", "pg31"),
					makePods(30, "0", "1G", "pg32"),
					makePods(30, "0", "1G", "pg4"),
				),
				Queues: []*schedulingv1.Queue{
					util.MakeQueue("root-pg1").Weight(1).Capability(nil).Annotations(
						map[string]string{
							schedulingv1.KubeHierarchyAnnotationKey:       "root/pg1",
							schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25",
						}).Obj(),
					util.MakeQueue("root-pg2").Weight(1).Capability(nil).Annotations(
						map[string]string{
							schedulingv1.KubeHierarchyAnnotationKey:       "root/pg2",
							schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25",
						}).Obj(),
					util.MakeQueue("root-pg3-pg31").Weight(1).Capability(nil).Annotations(
						map[string]string{
							schedulingv1.KubeHierarchyAnnotationKey:       "root/pg3/pg31",
							schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25/50",
						}).Obj(),
					util.MakeQueue("root-pg3-pg32").Weight(1).Capability(nil).Annotations(
						map[string]string{
							schedulingv1.KubeHierarchyAnnotationKey:       "root/pg3/pg32",
							schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25/50",
						}).Obj(),
					util.MakeQueue("root-pg4").Weight(1).Capability(nil).Annotations(
						map[string]string{
							schedulingv1.KubeHierarchyAnnotationKey:       "root/pg4",
							schedulingv1.KubeHierarchyWeightAnnotationKey: "100/25",
						}).Obj(),
				},
				Nodes: []*v1.Node{
					util.MakeNode("n").
						Allocatable(api.BuildResourceList("30", "30Gi", []api.ScalarResource{{Name: "pods", Value: "500"}}...)).
						Capacity(api.BuildResourceList("30", "30Gi", []api.ScalarResource{{Name: "pods", Value: "500"}}...)).
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
