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
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestOvercommitPlugin(t *testing.T) {
	n1 := util.BuildNode("n1", api.BuildResourceList("2", "4Gi"), make(map[string]string))
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

func TestQueueScopedOvercommitAdmission(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.QueueScopedOvercommit, true)

	node := util.BuildNode("n1", api.BuildResourceList("10", "16Gi"), map[string]string{})
	queue := util.BuildQueue("batch", 1, nil)
	queue.Spec.Deserved = api.BuildResourceList("2", "2Gi")
	queue.Annotations = map[string]string{
		schedulingv1.QueueOvercommitFactorAnnotationKey: "2",
	}

	minResources := api.BuildResourceList("5", "1Gi")
	podGroup := util.BuildPodGroup("queue-limited", "default", queue.Name, 1, nil, schedulingv1.PodGroupPending)
	podGroup.Spec.MinResources = &minResources

	trueValue := true
	tiers := []conf.Tier{{Plugins: []conf.PluginOption{{
		Name:               PluginName,
		EnabledJobEnqueued: &trueValue,
		Arguments: framework.Arguments{
			overCommitFactor:         2.0,
			maxQueueOverCommitFactor: 1.0,
		},
	}}}}

	test := uthelper.TestCommonStruct{
		Name:      "queue-scoped-overcommit",
		Plugins:   map[string]framework.PluginBuilder{PluginName: New},
		PodGroups: []*schedulingv1.PodGroup{podGroup},
		Queues:    []*schedulingv1.Queue{queue},
		Nodes:     []*v1.Node{node},
	}
	ssn := test.RegisterSession(tiers, nil)
	defer test.Close()

	for _, job := range ssn.Jobs {
		if ssn.JobEnqueueable(job) {
			t.Fatalf("expected queue-scoped overcommit to reject job %s", job.Name)
		}
	}
}

func TestQueueScopedOvercommitDisabledPreservesGlobalAdmission(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.QueueScopedOvercommit, false)

	node := util.BuildNode("n1", api.BuildResourceList("10", "16Gi"), map[string]string{})
	queue := util.BuildQueue("batch", 1, nil)
	queue.Spec.Deserved = api.BuildResourceList("2", "2Gi")
	queue.Annotations = map[string]string{
		schedulingv1.QueueOvercommitFactorAnnotationKey: "1",
	}

	minResources := api.BuildResourceList("5", "1Gi")
	podGroup := util.BuildPodGroup("global-only", "default", queue.Name, 1, nil, schedulingv1.PodGroupPending)
	podGroup.Spec.MinResources = &minResources

	trueValue := true
	tiers := []conf.Tier{{Plugins: []conf.PluginOption{{
		Name:               PluginName,
		EnabledJobEnqueued: &trueValue,
		Arguments: framework.Arguments{
			overCommitFactor: 2.0,
		},
	}}}}

	test := uthelper.TestCommonStruct{
		Name:      "queue-scoped-overcommit-disabled",
		Plugins:   map[string]framework.PluginBuilder{PluginName: New},
		PodGroups: []*schedulingv1.PodGroup{podGroup},
		Queues:    []*schedulingv1.Queue{queue},
		Nodes:     []*v1.Node{node},
	}
	ssn := test.RegisterSession(tiers, nil)
	defer test.Close()

	for _, job := range ssn.Jobs {
		if !ssn.JobEnqueueable(job) {
			t.Fatalf("expected global overcommit admission to ignore queue annotation when feature gate is disabled")
		}
	}
}

func TestQueueScopedOvercommitChecksAnnotatedAncestors(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.QueueScopedOvercommit, true)

	node := util.BuildNode("n1", api.BuildResourceList("10", "16Gi"), map[string]string{})
	root := util.BuildQueue("root", 1, nil)
	parent := util.BuildQueue("research", 1, nil)
	parent.Spec.Parent = root.Name
	parent.Spec.Deserved = api.BuildResourceList("2", "2Gi")
	parent.Annotations = map[string]string{
		schedulingv1.QueueOvercommitFactorAnnotationKey: "1",
	}
	leaf := util.BuildQueue("batch", 1, nil)
	leaf.Spec.Parent = parent.Name

	minResources := api.BuildResourceList("3", "1Gi")
	podGroup := util.BuildPodGroup("parent-limited", "default", leaf.Name, 1, nil, schedulingv1.PodGroupPending)
	podGroup.Spec.MinResources = &minResources

	trueValue := true
	tiers := []conf.Tier{{Plugins: []conf.PluginOption{{
		Name:               PluginName,
		EnabledJobEnqueued: &trueValue,
		EnabledHierarchy:   &trueValue,
		Arguments: framework.Arguments{
			overCommitFactor:         2.0,
			maxQueueOverCommitFactor: 1.0,
		},
	}}}}

	test := uthelper.TestCommonStruct{
		Name:      "queue-scoped-overcommit-ancestor",
		Plugins:   map[string]framework.PluginBuilder{PluginName: New},
		PodGroups: []*schedulingv1.PodGroup{podGroup},
		Queues:    []*schedulingv1.Queue{root, parent, leaf},
		Nodes:     []*v1.Node{node},
	}
	ssn := test.RegisterSession(tiers, nil)
	defer test.Close()

	for _, job := range ssn.Jobs {
		if ssn.JobEnqueueable(job) {
			t.Fatalf("expected annotated ancestor to reject job %s", job.Name)
		}
	}
}
