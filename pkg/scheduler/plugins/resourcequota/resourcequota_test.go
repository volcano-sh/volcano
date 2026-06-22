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

package resourcequota

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"

	"volcano.sh/apis/pkg/apis/scheduling"
	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/actions/enqueue"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestResourceQuotaPlugin(t *testing.T) {

	hugeResource := api.BuildResourceList("20000m", "20G")
	normalResource := api.BuildResourceList("2000m", "2G")

	// pg that requires normal resources
	pg1 := util.BuildPodGroup("pg1", "default", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))
	pg1.Spec.MinResources = &normalResource
	// pg that requires small resources
	pg2 := util.BuildPodGroup("pg2", "default", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))
	pg2.Spec.MinResources = &hugeResource
	// pg that no set requires
	pg3 := util.BuildPodGroup("pg3", "default", "c1", 2, nil, schedulingv1.PodGroupPhase(scheduling.PodGroupInqueue))

	queue1 := util.BuildQueue("c1", 1, nil)
	rq1 := util.BuildResourceQuota("test", "default", normalResource)

	tests := []struct {
		uthelper.TestCommonStruct
		expectedEnqueueAble bool
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:           "ResourceQuota capacity can match the needs of this pg",
				Plugins:        map[string]framework.PluginBuilder{PluginName: New},
				PodGroups:      []*schedulingv1.PodGroup{pg1},
				Queues:         []*schedulingv1.Queue{queue1},
				ResourceQuotas: []*v1.ResourceQuota{rq1},
			},
			expectedEnqueueAble: true,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:           "ResourceQuota capacity can't match the needs of this pg",
				Plugins:        map[string]framework.PluginBuilder{PluginName: New},
				PodGroups:      []*schedulingv1.PodGroup{pg2},
				Queues:         []*schedulingv1.Queue{queue1},
				ResourceQuotas: []*v1.ResourceQuota{rq1},
			},
			expectedEnqueueAble: false,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:      "No ResourceQuota resource object",
				Plugins:   map[string]framework.PluginBuilder{PluginName: New},
				PodGroups: []*schedulingv1.PodGroup{pg2},
				Queues:    []*schedulingv1.Queue{queue1},
			},
			expectedEnqueueAble: true,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:           "MinResources field of pg is not set",
				Plugins:        map[string]framework.PluginBuilder{PluginName: New},
				PodGroups:      []*schedulingv1.PodGroup{pg3},
				Queues:         []*schedulingv1.Queue{queue1},
				ResourceQuotas: []*v1.ResourceQuota{rq1},
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
						},
					},
				},
			}
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()
			for _, job := range ssn.Jobs {
				isEnqueue := ssn.JobEnqueueable(job)
				if !equality.Semantic.DeepEqual(test.expectedEnqueueAble, isEnqueue) {
					t.Errorf("case: %s error,  expect %v, but get %v", test.Name, test.expectedEnqueueAble, isEnqueue)
				}
			}
		})
	}
}

// TestJobEnqueuedOnOtherPluginsReject verifies that the resourcequota plugin
// adds to a namespace's pending usage only when a job is fully admitted by all plugins
// in the tier. A job rejected by a co-tier plugin must not consume namespace quota,
// leaving room for subsequent jobs that fit within the hard limit.
func TestJobEnqueuedOnOtherPluginsReject(t *testing.T) {
	trueValue := true
	res1CPU := api.BuildResourceList("1", "1G")

	n1 := util.BuildNode("n1", api.BuildResourceList("4", "4G", []api.ScalarResource{{Name: "pods", Value: "10"}}...), nil)

	// Namespace quota: exactly 1 CPU — only one job can be admitted per session.
	rq1 := util.BuildResourceQuota("rq1", "ns1", res1CPU)

	podA := util.BuildPod("ns1", "podA", "", v1.PodPending, res1CPU, "pgA", nil, nil)
	podB := util.BuildPod("ns1", "podB", "", v1.PodPending, res1CPU, "pgB", nil, nil)

	pgA := util.BuildPodGroup("pgA", "ns1", "q1", 1, nil, schedulingv1.PodGroupPending)
	pgB := util.BuildPodGroup("pgB", "ns1", "q1", 1, nil, schedulingv1.PodGroupPending)
	pgA.Spec.MinResources = &res1CPU
	pgB.Spec.MinResources = &res1CPU

	queue1 := util.BuildQueue("q1", 1, nil)

	const rejecterName = "test-rejecter"
	plugins := map[string]framework.PluginBuilder{
		PluginName:   New,
		rejecterName: func(_ framework.Arguments) framework.Plugin { return &uthelper.RejecterPlugin{RejectJob: "pgA"} },
	}
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{Name: PluginName, EnabledJobEnqueued: &trueValue},
				{Name: rejecterName, EnabledJobEnqueued: &trueValue},
			},
		},
	}

	test := uthelper.TestCommonStruct{
		Name:           "pgA rejected by co-tier plugin must not consume ns1 quota; pgB must be admitted",
		Plugins:        plugins,
		Pods:           []*v1.Pod{podA, podB},
		Nodes:          []*v1.Node{n1},
		PodGroups:      []*schedulingv1.PodGroup{pgA, pgB},
		Queues:         []*schedulingv1.Queue{queue1},
		ResourceQuotas: []*v1.ResourceQuota{rq1},
		ExpectStatus: map[api.JobID]scheduling.PodGroupPhase{
			"ns1/pgB": scheduling.PodGroupInqueue,
		},
	}
	test.RegisterSession(tiers, nil)
	defer test.Close()
	test.Run([]framework.Action{enqueue.New()})
	if err := test.CheckAll(0); err != nil {
		t.Fatal(err)
	}
}
