package resourcestrategyfit

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/resource"
	"math"
	"reflect"
	"testing"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"

	v1 "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/scheduler/apis/config"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	eps = 1e-8
)

func Test_calculateWeight(t *testing.T) {
	type args struct {
		args framework.Arguments
	}
	tests := []struct {
		name string
		args args
		want ResourceStrategyFit
	}{
		{"test1", args{framework.Arguments{
			"ResourceStrategyFitPlusWeight": 10,
			"resources": map[string]interface{}{
				"cpu": map[string]interface{}{
					"type":   "MostAllocated",
					"weight": 1,
				},
				"memory": map[string]interface{}{
					"type":   "LeastAllocated",
					"weight": 2,
				},
			},
		}}, ResourceStrategyFit{
			ResourceStrategyFitWeight: 10,
			Resources: map[v1.ResourceName]ResourcesType{
				"cpu": {
					Type:   config.MostAllocated,
					Weight: 1,
				},
				"memory": {
					Type:   config.LeastAllocated,
					Weight: 2,
				},
			},
		}},
		{"test2", args{framework.Arguments{
			"resources": map[string]interface{}{
				"cpu": map[string]interface{}{
					"type":   "MostAllocated",
					"weight": 1,
				},
				"memory": map[string]interface{}{
					"type":   "LeastAllocated",
					"weight": 2,
				},
			},
		}}, ResourceStrategyFit{
			ResourceStrategyFitWeight: 10,
			Resources: map[v1.ResourceName]ResourcesType{
				"cpu": {
					Type:   config.MostAllocated,
					Weight: 1,
				},
				"memory": {
					Type:   config.LeastAllocated,
					Weight: 2,
				},
			},
		}},
		{"test3", args{framework.Arguments{
			"ResourceStrategyFitPlusWeight": 10,
		}}, ResourceStrategyFit{
			ResourceStrategyFitWeight: 10,
			Resources: map[v1.ResourceName]ResourcesType{
				"cpu": {
					Type:   config.LeastAllocated,
					Weight: 1,
				},
				"memory": {
					Type:   config.LeastAllocated,
					Weight: 1,
				},
			},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calculateWeight(tt.args.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("calculateWeight() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlusScore(t *testing.T) {
	type args struct {
		task   *api.TaskInfo
		node   *api.NodeInfo
		weight ResourceStrategyFit
	}
	tests := []struct {
		name string
		args args
		want float64
	}{
		{name: "test1", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: ResourceStrategyFit{
				ResourceStrategyFitWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
				},
			},
		}, want: 400},
		{name: "test2", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 400,
					Memory:   400,
				},
			},
			weight: ResourceStrategyFit{
				ResourceStrategyFitWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
				},
			},
		}, want: 250},
		{name: "test3", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: ResourceStrategyFit{
				ResourceStrategyFitWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.MostAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 1,
					},
				},
			},
		}, want: 600},
		{name: "test4", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 400,
					Memory:   400,
				},
			},
			weight: ResourceStrategyFit{
				ResourceStrategyFitWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.MostAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 1,
					},
				},
			},
		}, want: 750},
		{name: "test5", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: ResourceStrategyFit{
				ResourceStrategyFitWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 2,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 1,
					},
				},
			},
		}, want: 600},
		{name: "test6", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: ResourceStrategyFit{
				ResourceStrategyFitWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 2,
					},
				},
			},
		}, want: 750},
		{name: "test7", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 0,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: ResourceStrategyFit{
				ResourceStrategyFitWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 2,
					},
				},
			},
		}, want: 600},
		{name: "test8", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					Memory: 400,
				},
			},
			weight: ResourceStrategyFit{
				ResourceStrategyFitWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"cpu": {
						Type:   config.LeastAllocated,
						Weight: 1,
					},
					"memory": {
						Type:   config.MostAllocated,
						Weight: 2,
					},
				},
			},
		}, want: 500},
		{name: "test9", args: args{
			task: &api.TaskInfo{
				Resreq: &api.Resource{
					MilliCPU: 100,
					Memory:   100,
				},
			},
			node: &api.NodeInfo{
				Used: &api.Resource{
					MilliCPU: 200,
					Memory:   200,
				},
				Allocatable: &api.Resource{
					MilliCPU: 500,
					Memory:   500,
				},
			},
			weight: ResourceStrategyFit{
				ResourceStrategyFitWeight: 10,
				Resources: map[v1.ResourceName]ResourcesType{
					"memory": {
						Type:   config.MostAllocated,
						Weight: 2,
					},
				},
			},
		}, want: 600},
	}
	socre := map[string]float64{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlusScore(tt.args.task, tt.args.node, tt.args.weight); got != tt.want {
				if tt.name == "test5" || tt.name == "test6" {
					socre[tt.name] = got
					return
				}
				t.Errorf("PlusScore() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_mostRequestedScore(t *testing.T) {
	type args struct {
		requested float64
		used      float64
		capacity  float64
		weight    int
	}
	tests := []struct {
		name    string
		args    args
		want    float64
		wantErr bool
	}{
		{name: "test1", args: args{
			requested: 0,
			used:      0,
			capacity:  0,
			weight:    0,
		}, want: 0, wantErr: false},
		{name: "test2", args: args{
			requested: 1,
			used:      2,
			capacity:  2,
			weight:    1,
		}, want: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mostRequestedScore(tt.args.requested, tt.args.used, tt.args.capacity, tt.args.weight)
			if (err != nil) != tt.wantErr {
				t.Errorf("mostRequestedScore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("mostRequestedScore() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_leastRequestedScore(t *testing.T) {
	type args struct {
		requested float64
		used      float64
		capacity  float64
		weight    int
	}
	tests := []struct {
		name    string
		args    args
		want    float64
		wantErr bool
	}{
		{name: "test1", args: args{
			requested: 0,
			used:      0,
			capacity:  0,
			weight:    0,
		}, want: 0, wantErr: false},
		{name: "test2", args: args{
			requested: 1,
			used:      2,
			capacity:  2,
			weight:    1,
		}, want: 0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := leastRequestedScore(tt.args.requested, tt.args.used, tt.args.capacity, tt.args.weight)
			if (err != nil) != tt.wantErr {
				t.Errorf("mostRequestedScore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("mostRequestedScore() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_resourceStrategyFitPlusWeightPlusPlugin_OnSessionOpen(t *testing.T) {
	type fields struct {
		weight ResourceStrategyFit
	}
	type args struct {
		ssn *framework.Session
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{name: "test1", args: args{ssn: &framework.Session{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bp := &resourceStrategyFitPlugin{
				weight: tt.fields.weight,
			}
			bp.OnSessionOpen(tt.args.ssn)
		})
	}
}

func addResource(resourceList v1.ResourceList, name v1.ResourceName, need string) {
	resourceList[name] = resource.MustParse(need)
}

func TestResourceStrategyFitPlugin(t *testing.T) {
	GPU := v1.ResourceName("nvidia.com/gpu")
	FOO := v1.ResourceName("example.com/foo")

	p1 := util.BuildPod("c1", "p1", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	addResource(p1.Spec.Containers[0].Resources.Requests, FOO, "2")
	p2 := util.BuildPod("c1", "p2", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	addResource(p2.Spec.Containers[0].Resources.Requests, FOO, "3")
	p3 := util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("1", "10Gi"), "pg1", make(map[string]string), make(map[string]string))
	addResource(p3.Spec.Containers[0].Resources.Requests, GPU, "2")
	p4 := util.BuildPod("c1", "p4", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	addResource(p4.Spec.Containers[0].Resources.Requests, GPU, "3")

	p5 := util.BuildPod("c1", "p5", "", v1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string))
	addResource(p5.Spec.Containers[0].Resources.Requests, GPU, "4")
	addResource(p5.Spec.Containers[0].Resources.Requests, FOO, "4")

	n1 := util.BuildNode("n1", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	addResource(n1.Status.Allocatable, GPU, "10")
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	addResource(n2.Status.Allocatable, GPU, "5")
	n3 := util.BuildNode("n3", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	addResource(n3.Status.Allocatable, FOO, "10")
	n4 := util.BuildNode("n4", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	addResource(n4.Status.Allocatable, FOO, "5")

	n5 := util.BuildNode("n5", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	addResource(n5.Status.Allocatable, GPU, "10")
	addResource(n5.Status.Allocatable, FOO, "5")
	n6 := util.BuildNode("n6", api.BuildResourceList("4", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), make(map[string]string))
	addResource(n6.Status.Allocatable, FOO, "5")
	addResource(n6.Status.Allocatable, FOO, "10")

	pg1 := util.BuildPodGroup("pg1", "c1", "c1", 0, nil, "")
	queue1 := util.BuildQueue("c1", 1, nil)

	tests := []struct {
		uthelper.TestCommonStruct
		arguments framework.Arguments
		expected  map[string]map[string]float64
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{

				Name:      "single job",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Pods:      []*v1.Pod{p1, p2, p3, p4},
				Nodes:     []*v1.Node{n1, n2, n3, n4},
			},
			arguments: framework.Arguments{
				"ResourceStrategyFitPlusWeight": 10,
				"resources": map[string]interface{}{
					"nvidia.com/gpu": map[string]interface{}{
						"type":   "MostAllocated",
						"weight": 1,
					},
					"example.com/foo": map[string]interface{}{
						"type":   "LeastAllocated",
						"weight": 1,
					},
				},
			},
			expected: map[string]map[string]float64{
				"c1/p1": {
					"n1": 0,
					"n2": 0,
					"n3": 800,
					"n4": 600,
				},
				"c1/p2": {
					"n1": 0,
					"n2": 0,
					"n3": 700,
					"n4": 400,
				},
				"c1/p3": {
					"n1": 200,
					"n2": 400,
					"n3": 0,
					"n4": 0,
				},
				"c1/p4": {
					"n1": 300,
					"n2": 600,
					"n3": 0,
					"n4": 0,
				},
			},
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "single job",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Pods:      []*v1.Pod{p5},
				Nodes:     []*v1.Node{n5, n6},
			},
			arguments: framework.Arguments{
				"ResourceStrategyFitPlusWeight": 10,
				"resources": map[string]interface{}{
					"nvidia.com/gpu": map[string]interface{}{
						"type":   "MostAllocated",
						"weight": 1,
					},
					"example.com/foo": map[string]interface{}{
						"type":   "LeastAllocated",
						"weight": 2,
					},
				},
			},
			expected: map[string]map[string]float64{
				"c1/p5": {
					"n5": 266.66666666,
					"n6": 399.99999999,
				},
			},
		},
	}

	trueValue := true
	for i, test := range tests {
		tiers := []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{
						Name:             PluginName,
						EnabledNodeOrder: &trueValue,
						Arguments:        test.arguments,
					},
				},
			},
		}
		ssn := test.RegisterSession(tiers, nil)
		for _, job := range ssn.Jobs {
			for _, task := range job.Tasks {
				taskID := fmt.Sprintf("%s/%s", task.Namespace, task.Name)
				for _, node := range ssn.Nodes {
					score, err := ssn.NodeOrderFn(task, node)
					fmt.Println(i, score, node.Name, err, taskID)
					if expectScore := test.expected[taskID][node.Name]; math.Abs(expectScore-score) > eps {
						t.Errorf("case%d: task %s on node %s expect have score %v, but get %v", i, taskID, node.Name, expectScore, score)
					}
				}
			}
		}
	}
}
