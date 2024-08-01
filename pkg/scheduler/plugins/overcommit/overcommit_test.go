package overcommit

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
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
	n1 := util.BuildNode("n1", api.BuildResourceList("2", "4Gi", api.ScalarResource{Name: "ephemeral-storage", Value: "32Gi"}, api.ScalarResource{Name: "nvidia.com/gpu", Value: "8"}), make(map[string]string))
	n2 := util.BuildNode("n2", api.BuildResourceList("4", "16Gi"), make(map[string]string))
	hugeResource := api.BuildResourceList("20000m", "20G")
	normalResource := api.BuildResourceList("2000m", "2G")
	smallResource := api.BuildResourceList("200m", "0.5G")

	// pg that requires normal resources
	pg1 := util.BuildPodGroup("pg1", "test-namespace", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))
	pg1.Spec.MinResources = &normalResource
	// pg that requires small resources
	pg2 := util.BuildPodGroup("pg2", "test-namespace", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))
	pg2.Spec.MinResources = &hugeResource
	// pg that no requires resources
	pg3 := util.BuildPodGroup("pg2", "test-namespace", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))

	queue1 := util.BuildQueue("c1", 1, nil)
	queue2 := util.BuildQueue("c1", 1, smallResource)

	tests := []struct {
		uthelper.TestCommonStruct
		arguments           framework.Arguments
		expectedEnqueueAble bool
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "overCommitFactor is more than 1",
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
				Name:      "overCommitFactor is less than 1",
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
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "overCommitFactor is more than 1 with different overcommit factors",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg1},
				Queues:    []*schedulingv1.Queue{queue1},
				Nodes:     []*v1.Node{n1, n2},
			},
			arguments: framework.Arguments{
				"overcommit-factor.cpu":               1.3,
				"overcommit-factor.memory":            1.4,
				"overcommit-factor.ephemeral-storage": 1.4,
				"overcommit-factor.nvidia.com/gpu":    1.3,
			},
			expectedEnqueueAble: true,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "overCommitFactor is not set",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg3},
				Queues:    []*schedulingv1.Queue{queue2},
				Nodes:     []*v1.Node{n1, n2},
			},
			arguments:           framework.Arguments{},
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

func TestParseFactor(t *testing.T) {
	tests := []struct {
		name         string
		arguments    framework.Arguments
		expectedMaps map[string]float64
	}{
		{
			name: "overCommitFactor with float64 type",
			arguments: framework.Arguments{
				"overcommit-factor.cpu":               1.3,
				"overcommit-factor.memory":            1.4,
				"overcommit-factor.ephemeral-storage": 1.4,
				"overcommit-factor.nvidia.com/gpu":    1.3,
			},
			expectedMaps: map[string]float64{
				// default value
				"overcommit-factor": 1.2,
				"cpu":               1.3,
				"memory":            1.4,
				"ephemeral-storage": 1.4,
				"nvidia.com/gpu":    1.3,
			},
		},
		{
			name: "overCommitFactor with int type",
			arguments: framework.Arguments{
				"overcommit-factor.cpu":               2,
				"overcommit-factor.memory":            2,
				"overcommit-factor.ephemeral-storage": 2,
				"overcommit-factor.nvidia.com/gpu":    2,
			},
			expectedMaps: map[string]float64{
				// default value
				"overcommit-factor": 1.2,
				"cpu":               2.0,
				"memory":            2.0,
				"ephemeral-storage": 2.0,
				"nvidia.com/gpu":    2.0,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			op := New(test.arguments).(*overcommitPlugin)
			op.parseFactor(test.arguments, op.overCommitFactors.factorMaps)
			// Sort expected and resulting maps by keys for comparison
			expectedKeys := sortMapByKey(test.expectedMaps)
			resultKeys := sortMapByKey(op.overCommitFactors.factorMaps)

			// Check if the sorted keys match
			if diff := cmp.Diff(expectedKeys, resultKeys); diff != "" {
				t.Errorf("sorted keys mismatch: %s", diff)
			}

			// Check if the values match after sorting by keys
			for _, key := range expectedKeys {
				if test.expectedMaps[key] != op.overCommitFactors.factorMaps[key] {
					t.Errorf("value mismatch for key %s: expected %f, got %f",
						key, test.expectedMaps[key], op.overCommitFactors.factorMaps[key])
				}
			}
		})
	}
}

func sortMapByKey(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
