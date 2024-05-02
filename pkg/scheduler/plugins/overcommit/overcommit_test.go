package overcommit

import (
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// buildResourceList builds resource list object
func buildResourceList(cpu, memory string) *v1.ResourceList {
	resourceList := &v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	}
	return resourceList
}

func TestOvercommitPlugin(t *testing.T) {
	n1 := util.BuildNode("n1", api.BuildResourceList("2", "4Gi"), make(map[string]string))
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "16Gi"), make(map[string]string))
	hugeResource := buildResourceList("20000m", "20G")
	normalResource := buildResourceList("2000m", "2G")
	smallResource := buildResourceList("200m", "0.5G")

	// pg that requires normal resources
	pg1 := util.BuildPodGroup("pg1", "test-namespace", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))
	pg1.Spec.MinResources = normalResource
	// pg that requires small resources
	pg2 := util.BuildPodGroup("pg2", "test-namespace", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))
	pg2.Spec.MinResources = hugeResource
	// pg that no requires resources
	pg3 := util.BuildPodGroup("pg2", "test-namespace", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))

	queue1 := util.BuildQueue("c1", 1, nil)
	queue2 := util.BuildQueue("c1", 1, *smallResource)

	tests := []struct {
		name string
		uthelper.TestCommonStruct
		arguments           framework.Arguments
		expectedEnqueueAble bool
	}{
		{
			name: "",
			TestCommonStruct: uthelper.TestCommonStruct{
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
			name: "overCommitFactor is less than 0",
			TestCommonStruct: uthelper.TestCommonStruct{
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
			name: "when the required resources of pg are too large",
			TestCommonStruct: uthelper.TestCommonStruct{
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
			name: "when pg does not fill MinResources",
			TestCommonStruct: uthelper.TestCommonStruct{
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

	trueValue := true
	for _, test := range tests {
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
		for _, job := range ssn.Jobs {
			ssn.JobEnqueued(job)
			isEnqueue := ssn.JobEnqueueable(job)
			if !reflect.DeepEqual(test.expectedEnqueueAble, isEnqueue) {
				t.Errorf("case: %s error,  expect %v, but get %v", test.name, test.expectedEnqueueAble, isEnqueue)
			}
		}
		test.Close()
	}
}
